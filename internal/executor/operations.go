package executor

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/monkeycode-ai/sgx-onebox-platform/internal/domain"
	"github.com/monkeycode-ai/sgx-onebox-platform/internal/security"
	"golang.org/x/crypto/ssh"
)

func (e *Executor) findSSHNodes(role string) []domain.ClusterNode {
	nodes := e.store.ListClusterNodes()
	eligible := make([]domain.ClusterNode, 0, len(nodes))
	for _, n := range nodes {
		if n.SSHHost == "" || n.SSHUsername == "" || n.SSHPasswordCiphertext == "" {
			continue
		}
		if role != "" && n.Role != role && string(n.K3sRole) != role {
			continue
		}
		eligible = append(eligible, n)
	}
	return eligible
}

func (e *Executor) findSSHNode(role string) *domain.ClusterNode {
	nodes := e.findSSHNodes(role)
	if len(nodes) == 0 {
		return nil
	}
	return &nodes[0]
}

func (e *Executor) sshConnectClient(node *domain.ClusterNode) (*ssh.Client, error) {
	password, err := security.DecryptString(node.SSHPasswordCiphertext)
	if err != nil || password == "" {
		return nil, fmt.Errorf("解密 SSH 凭据失败: %v", err)
	}
	defer zeroPassword([]byte(password))

	client, err := sshConnect(node.SSHHost, node.SSHPort, node.SSHUsername, []byte(password), node.SSHKnownHostKey, e.sshTimeout)
	if err != nil {
		return nil, fmt.Errorf("SSH 连接节点 %s 失败: %v", node.Name, err)
	}
	return client, nil
}

func (e *Executor) withSSHNode(role string, fn func(*ssh.Client, *domain.ClusterNode) (string, error)) (bool, string) {
	nodes := e.findSSHNodes(role)
	if len(nodes) == 0 {
		return false, "等待真实执行器连接"
	}
	lastResult := "等待真实执行器连接"
	for i := range nodes {
		node := &nodes[i]
		client, err := e.sshConnectClient(node)
		if err != nil {
			logWarn(fmt.Sprintf("ssh client for %s: %v", node.Name, err))
			lastResult = "等待真实执行器连接"
			continue
		}
		out, fnErr := func() (string, error) {
			defer client.Close()
			return fn(client, node)
		}()
		if fnErr != nil {
			logWarn(fmt.Sprintf("operation on %s failed: %v", node.Name, fnErr))
			lastResult = out
			continue
		}
		return true, out
	}
	return false, lastResult
}

func (e *Executor) ExecuteManifest(manifest string) (bool, string) {
	encoded := base64.StdEncoding.EncodeToString([]byte(manifest))
	return e.withSSHNode("control-plane", func(client *ssh.Client, node *domain.ClusterNode) (string, error) {
		cmd := fmt.Sprintf("printf %%s %s | base64 -d | kubectl apply -f - 2>&1", encoded)
		output, err := sshExec(client, cmd, e.sshTimeout*5)
		if err != nil {
			return fmt.Sprintf("Kubernetes 下发失败: %s", strings.TrimSpace(output)), err
		}
		return fmt.Sprintf("Manifest 已通过 kubectl 下发至节点 %s: %s", node.Name, strings.TrimSpace(output)), nil
	})
}

func (e *Executor) ExecuteScale(namespace, componentID string, replicas int) (bool, string) {
	if !isValidKubernetesName(namespace) || !isValidKubernetesName(componentID) || replicas < 1 || replicas > 1000 {
		return false, "扩缩容参数未通过安全校验"
	}
	return e.withSSHNode("control-plane", func(client *ssh.Client, node *domain.ClusterNode) (string, error) {
		cmd := fmt.Sprintf("kubectl scale deployment/%s --replicas=%d -n %s 2>&1", shellQuote(componentID), replicas, shellQuote(namespace))
		output, err := sshExec(client, cmd, e.sshTimeout*5)
		if err != nil {
			return fmt.Sprintf("Kubernetes 扩缩容失败: %s", strings.TrimSpace(output)), err
		}
		return fmt.Sprintf("组件 %s 已通过节点 %s 执行扩缩容: %s", componentID, node.Name, strings.TrimSpace(output)), nil
	})
}

func (e *Executor) ExecuteComponentUpgrade(namespace, componentID, imageRef string) (bool, string) {
	if !isValidKubernetesName(namespace) || !isValidKubernetesName(componentID) || !isSafeImageRef(imageRef) {
		return false, "组件升级参数未通过安全校验"
	}
	return e.withSSHNode("control-plane", func(client *ssh.Client, node *domain.ClusterNode) (string, error) {
		cmd := fmt.Sprintf("kubectl set image deployment/%s main=%s -n %s 2>&1", shellQuote(componentID), shellQuote(imageRef), shellQuote(namespace))
		output, err := sshExec(client, cmd, e.sshTimeout*5)
		if err != nil {
			return fmt.Sprintf("Kubernetes 组件升级失败: %s", strings.TrimSpace(output)), err
		}
		return fmt.Sprintf("组件 %s 已通过节点 %s 执行镜像升级: %s", componentID, node.Name, strings.TrimSpace(output)), nil
	})
}

func (e *Executor) ExecuteImageValidation(image domain.ImageAsset) (bool, string) {
	imageRef := fmt.Sprintf("%s/%s:%s", strings.TrimSpace(image.Registry), strings.TrimSpace(image.Repository), strings.TrimSpace(image.Tag))
	if !isSafeImageRef(imageRef) {
		return false, "镜像引用未通过安全校验"
	}
	return e.withSSHNode("control-plane", func(client *ssh.Client, node *domain.ClusterNode) (string, error) {
		cmd := fmt.Sprintf("if command -v crictl >/dev/null 2>&1; then crictl pull %s; elif command -v ctr >/dev/null 2>&1; then ctr images pull %s; else exit 127; fi 2>&1", shellQuote(imageRef), shellQuote(imageRef))
		output, err := sshExec(client, cmd, e.sshTimeout*10)
		if err != nil {
			return fmt.Sprintf("镜像拉取或校验失败: %s", strings.TrimSpace(output)), err
		}
		return fmt.Sprintf("镜像已通过节点 %s 拉取校验: %s", node.Name, strings.TrimSpace(output)), nil
	})
}

func (e *Executor) ExecuteSecurityPolicy(rule domain.SecurityPolicyRule) (bool, string) {
	name := normalizeResourceName(rule.ID)
	namespace := normalizeResourceName(rule.Scope)
	if namespace == "" {
		namespace = "default"
	}
	if !isValidKubernetesName(name) || !isValidKubernetesName(namespace) {
		return false, "安全策略参数未通过安全校验"
	}
	manifest := fmt.Sprintf("apiVersion: networking.k8s.io/v1\nkind: NetworkPolicy\nmetadata:\n  name: %s\n  namespace: %s\nspec:\n  podSelector: {}\n  policyTypes:\n    - Ingress\n", name, namespace)
	if strings.TrimSpace(rule.IsolationLevel) != "" {
		manifest = buildIsolationNetworkPolicy(name, namespace, rule.IsolationLevel, rule.Targets)
	}
	return e.ExecuteManifest(manifest)
}

func buildIsolationNetworkPolicy(name, namespace, isolationLevel string, targets []string) string {
	lines := []string{
		"apiVersion: networking.k8s.io/v1",
		"kind: NetworkPolicy",
		"metadata:",
		fmt.Sprintf("  name: %s", name),
		fmt.Sprintf("  namespace: %s", namespace),
		"spec:",
	}
	if len(targets) > 0 {
		lines = append(lines, "  podSelector:")
		lines = append(lines, "    matchExpressions:")
		lines = append(lines, "      - key: app")
		lines = append(lines, "        operator: In")
		lines = append(lines, "        values:")
		for _, t := range targets {
			lines = append(lines, fmt.Sprintf("          - %s", t))
		}
	} else {
		lines = append(lines, "  podSelector: {}")
	}
	lines = append(lines, "  policyTypes:", "    - Ingress")
	switch isolationLevel {
	case "enclave":
		lines = append(lines,
			"  ingress:",
			"    - from:",
			"        - podSelector:",
			"            matchLabels:",
			"              isolation: enclave",
		)
	case "hardened":
		lines = append(lines,
			"  ingress:",
			"    - from:",
			"        - podSelector:",
			"            matchExpressions:",
			"              - key: isolation",
			"                operator: In",
			"                values:",
			"                  - enclave",
			"                  - hardened",
		)
	default:
		lines = append(lines,
			"  ingress:",
			"    - from:",
			"        - podSelector: {}",
		)
	}
	return strings.Join(lines, "\n")
}

func (e *Executor) ExecuteNetwork(network domain.NetworkAttachment) (bool, string) {
	name := normalizeResourceName(network.ID)
	if !isValidKubernetesName(name) || !isValidLinuxInterface(network.Bridge) || !isValidLinuxInterface(network.ParentNIC) || !isSafeNetworkValue(network.Subnet) || !isSafeNetworkValue(network.Gateway) || network.VLANID < 1 || network.VLANID > 4094 {
		return false, "网络参数未通过安全校验"
	}
	manifest := fmt.Sprintf("apiVersion: k8s.cni.cncf.io/v1\nkind: NetworkAttachmentDefinition\nmetadata:\n  name: %s\n  namespace: default\nspec:\n  config: '{\"cniVersion\":\"0.3.1\",\"type\":\"bridge\",\"bridge\":\"%s\",\"vlan\":%d,\"ipam\":{\"type\":\"host-local\",\"subnet\":\"%s\",\"gateway\":\"%s\"}}'\n", name, network.Bridge, network.VLANID, network.Subnet, network.Gateway)
	return e.ExecuteManifest(manifest)
}

func normalizeResourceName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		valid := r >= 'a' && r <= 'z' || r >= '0' && r <= '9'
		if valid {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteRune('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func isValidKubernetesName(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) < 1 || len(value) > 63 {
		return false
	}
	for i, r := range value {
		valid := r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-'
		if !valid {
			return false
		}
		if (i == 0 || i == len(value)-1) && r == '-' {
			return false
		}
	}
	return true
}

func isValidK3sVersion(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) < 2 || len(value) > 64 || value[0] != 'v' {
		return false
	}
	for _, r := range value[1:] {
		if unicode.IsDigit(r) || r == '.' || r == '+' || r == '-' || r == 'k' || r == 's' {
			continue
		}
		return false
	}
	return strings.Contains(value, ".")
}

func isSafeImageRef(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) < 3 || len(value) > 255 {
		return false
	}
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("./:_@-+", r) {
			continue
		}
		return false
	}
	return !strings.Contains(value, "..")
}

func isSafeNetworkValue(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) < 3 || len(value) > 64 {
		return false
	}
	for _, r := range value {
		if unicode.IsDigit(r) || r == '.' || r == '/' || r == ':' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F' {
			continue
		}
		return false
	}
	return true
}

func isValidMeasurement(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) != 64 {
		return false
	}
	for _, r := range value {
		if unicode.IsDigit(r) || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F' {
			continue
		}
		return false
	}
	return true
}

func (e *Executor) ExecuteUpgrade(targetVersion string) (bool, string) {
	if !isValidK3sVersion(targetVersion) {
		return false, "K3s 目标版本未通过安全校验"
	}
	nodes := e.findSSHNodes("control-plane")
	if len(nodes) == 0 {
		return false, "等待真实执行器连接"
	}
	results := make([]string, 0, len(nodes))
	for i := range nodes {
		node := &nodes[i]
		client, err := e.sshConnectClient(node)
		if err != nil {
			logWarn(fmt.Sprintf("ssh client for %s: %v", node.Name, err))
			return false, fmt.Sprintf("控制面节点 %s 连接失败: %v", node.Name, err)
		}
		defer client.Close()
		dir := k3sUpgradeWorkDir(targetVersion)
		version := targetVersion
		cmd := fmt.Sprintf("set -e; script=\"%s/install.sh\"; if [ -x \"$script\" ]; then INSTALL_K3S_VERSION='%s' \"$script\"; else curl -sfL https://get.k3s.io | INSTALL_K3S_VERSION='%s' sh -; fi", dir, version, version)
		output, execErr := sshExec(client, cmd, e.sshTimeout*30)
		if execErr != nil {
			return false, fmt.Sprintf("控制面节点 %s 升级命令执行失败: %v", node.Name, execErr)
		}
		results = append(results, fmt.Sprintf("%s: %s", node.Name, strings.TrimSpace(output)))
		logDebug(fmt.Sprintf("upgrade: k3s upgrade initiated on %s, version=%s", node.Name, targetVersion))
	}
	return true, fmt.Sprintf("升级命令已通过 SSH 发送至 %d 个控制面节点: %s", len(results), strings.Join(results, " | "))
}

func (e *Executor) ExecuteUpgradeDownload(targetVersion string) (bool, string) {
	if !isValidK3sVersion(targetVersion) {
		return false, "K3s 目标版本未通过安全校验"
	}
	nodes := e.findSSHNodes("control-plane")
	if len(nodes) == 0 {
		return false, "等待真实执行器连接"
	}
	results := make([]string, 0, len(nodes))
	for i := range nodes {
		node := &nodes[i]
		client, err := e.sshConnectClient(node)
		if err != nil {
			logWarn(fmt.Sprintf("ssh client for %s: %v", node.Name, err))
			return false, fmt.Sprintf("控制面节点 %s 连接失败: %v", node.Name, err)
		}
		defer client.Close()
		dir := k3sUpgradeWorkDir(targetVersion)
		cmd := fmt.Sprintf("set -e; mkdir -p \"%s\"; if command -v curl >/dev/null 2>&1; then curl -sfL https://get.k3s.io -o \"%s/install.sh\"; elif command -v wget >/dev/null 2>&1; then wget -q https://get.k3s.io -O \"%s/install.sh\"; else echo curl or wget is required; exit 127; fi; chmod +x \"%s/install.sh\"; test -s \"%s/install.sh\"", dir, dir, dir, dir, dir)
		output, execErr := sshExec(client, cmd, e.sshTimeout*20)
		if execErr != nil {
			return false, fmt.Sprintf("控制面节点 %s 升级脚本下载失败: %v", node.Name, execErr)
		}
		results = append(results, fmt.Sprintf("%s: %s", node.Name, strings.TrimSpace(output)))
		logDebug(fmt.Sprintf("upgrade: k3s installer downloaded on %s, version=%s", node.Name, targetVersion))
	}
	return true, fmt.Sprintf("K3s %s 升级脚本已下载到 %d 个控制面节点: %s", targetVersion, len(results), strings.Join(results, " | "))
}

func k3sUpgradeWorkDir(version string) string {
	return "/var/lib/rancher/k3s/upgrade/" + strings.NewReplacer(".", "-", "+", "-", ":", "-").Replace(version)
}

func (e *Executor) ExecuteAttestation(componentID string, enclaveProfile domain.EnclaveProfile) (string, string, string) {
	status, measurement, evidence := "pending", "MRENCLAVE:"+html.EscapeString(componentID), "等待真实执行器连接"

	node := e.findSSHNode("")
	if node == nil {
		return status, measurement, evidence
	}

	client, err := e.sshConnectClient(node)
	if err != nil {
		logWarn(fmt.Sprintf("attestation ssh to %s: %v", node.Name, err))
		return status, measurement, evidence
	}
	defer client.Close()

	var cmd string
	if strings.TrimSpace(enclaveProfile.MREnclave) != "" {
		if !isValidMeasurement(enclaveProfile.MREnclave) {
			return "failed", measurement, "MRENCLAVE 未通过安全校验"
		}
		cmd = fmt.Sprintf("dcap-verify-quote --mrenclave %s 2>&1", shellQuote(enclaveProfile.MREnclave))
	} else {
		cmd = "ls /dev/sgx_* 2>&1"
	}

	output, execErr := sshExec(client, cmd, e.sshTimeout*5)
	if execErr != nil {
		logWarn(fmt.Sprintf("attestation on %s failed: %v (output: %s)", node.Name, execErr, strings.TrimSpace(output)))
		status = "failed"
		evidence = fmt.Sprintf("DCAP 验证执行失败: %s", html.EscapeString(strings.TrimSpace(output)))
		return status, measurement, evidence
	}

	output = strings.TrimSpace(output)
	switch {
	case strings.Contains(output, "VERIFIED") || strings.Contains(output, "verified"):
		status = "verified"
		measurement = enclaveProfile.MREnclave
		evidence = html.EscapeString(output)
	case strings.Contains(output, "fail") || strings.Contains(output, "error") || strings.Contains(output, "not found"):
		status = "failed"
		evidence = fmt.Sprintf("SGX DCAP 验证未通过: %s", html.EscapeString(output))
	default:
		status = "pending"
		evidence = fmt.Sprintf("SGX 设备已检测但未完成验证: %s", html.EscapeString(output))
	}

	logDebug(fmt.Sprintf("attestation for %s on %s: status=%s", componentID, node.Name, status))
	return status, measurement, evidence
}

func (e *Executor) ExecuteInspection(componentID string) (string, string) {
	node := e.findSSHNode("")
	if node == nil {
		return "pending", "等待真实执行器连接"
	}

	client, err := e.sshConnectClient(node)
	if err != nil {
		logWarn(fmt.Sprintf("inspection ssh to %s: %v", node.Name, err))
		return "pending", "等待真实执行器连接"
	}
	defer client.Close()

	cmd := "ls /dev/sgx_* 2>&1; echo '---EPC---'; cat /proc/sgx/enclaves 2>/dev/null || echo NO_PROC_SGX; echo '---CPU---'; grep sgx /proc/cpuinfo 2>/dev/null | head -5 || echo NO_SGX_CPUINFO"
	output, execErr := sshExec(client, cmd, e.sshTimeout*3)
	if execErr != nil {
		logWarn(fmt.Sprintf("inspection on %s failed: %v", node.Name, execErr))
		return "pending", "等待真实执行器连接"
	}

	output = strings.TrimSpace(output)
	if output == "" {
		return "warning", fmt.Sprintf("节点 %s SGX 设备列表为空", html.EscapeString(node.Name))
	}

	var status string
	switch {
	case strings.Contains(output, "NO_PROC_SGX") && strings.Contains(output, "NO_SGX_CPUINFO"):
		status = "error"
	case strings.Contains(output, "/dev/sgx"):
		status = "healthy"
	default:
		status = "warning"
	}

	summary := fmt.Sprintf("节点 %s SGX 巡检完成 (%s)", html.EscapeString(node.Name), html.EscapeString(status))
	logDebug(fmt.Sprintf("inspection for %s on %s: status=%s", componentID, node.Name, status))
	return status, summary
}

type kubectlPodList struct {
	Items []kubectlPod `json:"items"`
}

type kubectlPod struct {
	Metadata kubectlPodMetadata `json:"metadata"`
	Spec     kubectlPodSpec     `json:"spec"`
}

type kubectlPodMetadata struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

type kubectlPodSpec struct {
	Containers []kubectlContainer `json:"containers"`
}

type kubectlContainer struct {
	Name            string                     `json:"name"`
	Image           string                     `json:"image"`
	SecurityContext *kubectlSecurityContext    `json:"securityContext"`
	Resources       kubectlContainerResources  `json:"resources"`
}

type kubectlSecurityContext struct {
	RunAsNonRoot           *bool `json:"runAsNonRoot"`
	ReadOnlyRootFilesystem *bool `json:"readOnlyRootFilesystem"`
	Privileged             *bool `json:"privileged"`
}

type kubectlContainerResources struct {
	Limits *kubectlResourceLimits `json:"limits"`
}

type kubectlResourceLimits struct {
	CPU    string `json:"cpu"`
	Memory string `json:"memory"`
}

func parsePodListJSON(raw string) (*kubectlPodList, error) {
	var list kubectlPodList
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return nil, err
	}
	return &list, nil
}

func isK3sOutdated(version string) bool {
	version = strings.TrimPrefix(version, "v")
	parts := strings.SplitN(version, ".", 3)
	if len(parts) < 2 {
		return false
	}
	major, err1 := strconv.Atoi(parts[0])
	minor, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return false
	}
	if strings.HasPrefix(parts[len(parts)-1], "0") && len(parts[len(parts)-1]) > 1 && minor > 0 {
		parts[len(parts)-1] = strings.TrimLeft(parts[len(parts)-1], "0")
	}
	return major < 1 || (major == 1 && minor < 29)
}

func (e *Executor) ExecuteComplianceChecks() []domain.ComplianceFinding {
	node := e.findSSHNode("control-plane")
	if node == nil {
		return []domain.ComplianceFinding{
			{
				Category:       "集群连接",
				Level:          "high",
				Message:        "[executor] 无可用的控制面节点 SSH 连接",
				ControlID:      "SM-2.0-CLUSTER-01",
				ControlName:    "集群基础检查",
				Recommendation: "配置至少一个控制面节点的 SSH 凭据以启用实时合规检查",
			},
		}
	}

	client, err := e.sshConnectClient(node)
	if err != nil {
		return []domain.ComplianceFinding{
			{
				Category:       "集群连接",
				Level:          "high",
				Message:        fmt.Sprintf("[executor] SSH 连接控制面节点 %s 失败: %v", node.Name, err),
				ControlID:      "SM-2.0-CLUSTER-01",
				ControlName:    "集群基础检查",
				Recommendation: "检查控制面节点 SSH 凭据配置和网络连通性",
			},
		}
	}
	defer client.Close()

	var findings []domain.ComplianceFinding

	podCmd := "kubectl get pods --all-namespaces -o json 2>&1"
	podOutput, podErr := sshExec(client, podCmd, e.sshTimeout*15)
	if podErr != nil {
		findings = append(findings, domain.ComplianceFinding{
			Category:       "集群连接",
			Level:          "high",
			Message:        fmt.Sprintf("[executor] kubectl 查询 Pod 列表失败: %s", strings.TrimSpace(podOutput)),
			ControlID:      "SM-2.0-CLUSTER-02",
			ControlName:    "集群 API 可用性",
			Recommendation: "确认 kubectl 已正确配置且集群 API Server 可访问",
		})
	}

	pods, parseErr := parsePodListJSON(podOutput)
	if parseErr != nil && podErr == nil {
		findings = append(findings, domain.ComplianceFinding{
			Category:       "集群连接",
			Level:          "high",
			Message:        fmt.Sprintf("[executor] 解析 Pod 列表 JSON 失败: %v", parseErr),
			ControlID:      "SM-2.0-CLUSTER-02",
			ControlName:    "集群 API 可用性",
			Recommendation: "确认集群 API Server 返回有效的 JSON 格式",
		})
	}

	if pods != nil {
		for _, pod := range pods.Items {
			ns := pod.Metadata.Namespace
			podName := pod.Metadata.Name
			if ns == "kube-system" {
				continue
			}
			for _, container := range pod.Spec.Containers {
				prefix := fmt.Sprintf("[executor:%s/%s/%s]", ns, podName, container.Name)

				if container.SecurityContext != nil && container.SecurityContext.Privileged != nil && *container.SecurityContext.Privileged {
					findings = append(findings, domain.ComplianceFinding{
						Category:       "Pod Security Context",
						Level:          "high",
						Message:        fmt.Sprintf("%s 容器以特权模式运行", prefix),
						ControlID:      "SM-2.0-SC-02",
						ControlName:    "最小权限原则",
						Recommendation: "移除 privileged 配置，按需添加具体 capabilities",
					})
				}

				if container.SecurityContext == nil || container.SecurityContext.RunAsNonRoot == nil || !*container.SecurityContext.RunAsNonRoot {
					findings = append(findings, domain.ComplianceFinding{
						Category:       "Pod Security Context",
						Level:          "medium",
						Message:        fmt.Sprintf("%s 容器未设置 runAsNonRoot 或以 root 运行", prefix),
						ControlID:      "SM-2.0-SC-03",
						ControlName:    "最小权限原则",
						Recommendation: "设置 securityContext.runAsNonRoot: true 并指定非 root 用户运行容器",
					})
				}

				if container.SecurityContext == nil || container.SecurityContext.ReadOnlyRootFilesystem == nil || !*container.SecurityContext.ReadOnlyRootFilesystem {
					findings = append(findings, domain.ComplianceFinding{
						Category:       "Pod Security Context",
						Level:          "medium",
						Message:        fmt.Sprintf("%s 容器未设置只读根文件系统", prefix),
						ControlID:      "SM-2.0-SC-04",
						ControlName:    "文件系统完整性保护",
						Recommendation: "设置 securityContext.readOnlyRootFilesystem: true，使用 emptydir 或 volume 挂载写入路径",
					})
				}

				if container.Resources.Limits == nil || container.Resources.Limits.CPU == "" {
					findings = append(findings, domain.ComplianceFinding{
						Category:       "资源限制",
						Level:          "medium",
						Message:        fmt.Sprintf("%s 容器未设置 CPU 限制", prefix),
						ControlID:      "SM-2.0-RA-01",
						ControlName:    "资源访问控制",
						Recommendation: "为容器设置 resources.limits.cpu 防止资源耗尽",
					})
				}
				if container.Resources.Limits == nil || container.Resources.Limits.Memory == "" {
					findings = append(findings, domain.ComplianceFinding{
						Category:       "资源限制",
						Level:          "medium",
						Message:        fmt.Sprintf("%s 容器未设置内存限制", prefix),
						ControlID:      "SM-2.0-RA-02",
						ControlName:    "资源访问控制",
						Recommendation: "为容器设置 resources.limits.memory 防止 OOM",
					})
				}

				img := container.Image
				if strings.Contains(img, ":latest") {
					findings = append(findings, domain.ComplianceFinding{
						Category:       "镜像标签",
						Level:          "medium",
						Message:        fmt.Sprintf("%s 使用 latest 标签镜像: %s", prefix, img),
						ControlID:      "SM-2.0-SC-05",
						ControlName:    "软件完整性保护",
						Recommendation: "使用明确的版本标签替代 latest，确保部署可追溯性",
					})
				} else if !strings.Contains(img, ":") || strings.HasSuffix(img, ":") {
					findings = append(findings, domain.ComplianceFinding{
						Category:       "镜像标签",
						Level:          "low",
						Message:        fmt.Sprintf("%s 镜像缺少版本标签: %s", prefix, img),
						ControlID:      "SM-2.0-SC-06",
						ControlName:    "软件完整性保护",
						Recommendation: "为镜像添加明确的版本标签",
					})
				}
			}
		}
	}

	npCmd := "kubectl get networkpolicies --all-namespaces --no-headers 2>&1"
	npOutput, npErr := sshExec(client, npCmd, e.sshTimeout*5)
	if npErr == nil && strings.TrimSpace(npOutput) == "" {
		findings = append(findings, domain.ComplianceFinding{
			Category:       "网络安全",
			Level:          "high",
			Message:        "[executor] 集群未配置任何 NetworkPolicy",
			ControlID:      "SM-2.0-NC-01",
			ControlName:    "网络隔离与访问控制",
			Recommendation: "为各命名空间创建 NetworkPolicy 实现最小网络访问原则",
		})
	}

	verCmd := "kubectl version --short 2>/dev/null || k3s --version 2>&1"
	verOutput, verErr := sshExec(client, verCmd, e.sshTimeout*5)
	if verErr == nil {
		version := extractK3sVersion(verOutput)
		if version != "" && isK3sOutdated(version) {
			findings = append(findings, domain.ComplianceFinding{
				Category:       "集群版本",
				Level:          "medium",
				Message:        fmt.Sprintf("[executor] K3s 版本 %s 可能已过时", version),
				ControlID:      "SM-2.0-UP-01",
				ControlName:    "安全更新与补丁管理",
				Recommendation: "检查最新 K3s 版本并执行升级计划",
			})
		}
	}

	if len(findings) == 0 {
		findings = append(findings, domain.ComplianceFinding{
			Category:       "综合状态",
			Level:          "low",
			Message:        "[executor] 集群实时检查通过，未发现违规项",
			ControlID:      "SM-2.0-CLUSTER-03",
			ControlName:    "集群合规基线",
			Recommendation: "保持定期复核与证据留存",
		})
	}

	return findings
}

func (e *Executor) ExecuteImageBuild(req domain.ImageBuildRequest) (bool, string) {
	if strings.TrimSpace(req.SourcePackage) == "" {
		return false, "构建包不能为空"
	}
	return e.withSSHNode("control-plane", func(client *ssh.Client, node *domain.ClusterNode) (string, error) {
		checkOut, err := sshExec(client, "command -v docker >/dev/null 2>&1 && echo 'ok' || echo 'missing'", e.sshTimeout)
		if err != nil || strings.TrimSpace(checkOut) != "ok" {
			installOut, installErr := sshExec(client, "command -v docker >/dev/null 2>&1 || (apt-get update -qq && apt-get install -y -qq docker.io 2>&1)", e.sshTimeout*15)
			if installErr != nil {
				return fmt.Sprintf("Docker 安装失败: %s", strings.TrimSpace(installOut)), installErr
			}
			verifyOut, verifyErr := sshExec(client, "command -v docker >/dev/null 2>&1 && echo 'ok'", e.sshTimeout)
			if verifyErr != nil || strings.TrimSpace(verifyOut) != "ok" {
				return "Docker 安装后仍不可用", fmt.Errorf("docker 在安装后仍然不可用")
			}
			sshExec(client, "dockerd > /dev/null 2>&1 &", e.sshTimeout)
			time.Sleep(3 * time.Second)
		}

		dockerOK, daemonErr := sshExec(client, "docker info >/dev/null 2>&1 && echo 'ok' || echo 'down'", e.sshTimeout*5)
		if daemonErr != nil || strings.TrimSpace(dockerOK) != "ok" {
			sshExec(client, "nohup dockerd > /dev/null 2>&1 &", e.sshTimeout)
			time.Sleep(5 * time.Second)
			retryCheck, retryErr := sshExec(client, "docker info >/dev/null 2>&1 && echo 'ok' || echo 'down'", e.sshTimeout*5)
			if retryErr != nil || strings.TrimSpace(retryCheck) != "ok" {
				return "Docker 守护进程无法启动", fmt.Errorf("docker daemon 在多次尝试后仍然不可用")
			}
		}

		imageName := fmt.Sprintf("%s/%s:%s", strings.TrimSpace(req.Registry), strings.TrimSpace(req.Repository), strings.TrimSpace(req.Tag))
		dockerfile := strings.TrimSpace(req.DockerfilePath)
		if dockerfile == "" {
			dockerfile = "Dockerfile"
		}

		buildDir := fmt.Sprintf("/tmp/docker-build-%d", time.Now().UnixNano())
		_, err = sshExec(client, "mkdir -p "+shellQuote(buildDir), e.sshTimeout)
		if err != nil {
			return fmt.Sprintf("创建构建目录失败: %s", buildDir), err
		}

		sourceReader := strings.NewReader(req.SourcePackage)
		decodeOut, decodeErr := sshExecWithInput(client, fmt.Sprintf("base64 -d | tar -xz -C %s 2>&1", shellQuote(buildDir)), sourceReader, e.sshTimeout*10)
		if decodeErr != nil {
			sshExec(client, "rm -rf "+shellQuote(buildDir), e.sshTimeout)
			return fmt.Sprintf("解压构建包失败: %s", strings.TrimSpace(decodeOut)), decodeErr
		}
		defer func() {
			sshExec(client, "rm -rf "+shellQuote(buildDir), e.sshTimeout)
		}()

		buildLine := fmt.Sprintf("docker build -t %s -f %s ", shellQuote(imageName), shellQuote(dockerfile))
		if req.EnableSGXRuntime {
			buildLine += "--build-arg SGX_RUNTIME=1 "
		}
		if buildArgs := strings.TrimSpace(req.BuildArgs); buildArgs != "" {
			if !isSafeBuildArgs(buildArgs) {
				return fmt.Sprintf("构建参数包含非法字符"), fmt.Errorf("构建参数包含非法字符")
			}
			buildLine += " " + buildArgs
		}
		buildLine += fmt.Sprintf(" %s 2>&1", shellQuote(buildDir))
		buildOut, buildErr := sshExec(client, buildLine, e.sshTimeout*120)

		if buildErr != nil {
			return fmt.Sprintf("Docker 镜像构建失败: %s", strings.TrimSpace(buildOut)), buildErr
		}
		digestOut, _ := sshExec(client, fmt.Sprintf("docker image inspect --format '{{.ID}}' %s 2>&1", shellQuote(imageName)), e.sshTimeout)
		digest := strings.TrimSpace(digestOut)
		return fmt.Sprintf("镜像 %s 已通过节点 %s 构建成功\nDIGEST:%s", imageName, node.Name, digest), nil
	})
}

func isSafeBuildArgs(args string) bool {
	remain := strings.TrimSpace(args)
	for remain != "" {
		if !strings.HasPrefix(remain, "--build-arg ") {
			return false
		}
		remain = strings.TrimPrefix(remain, "--build-arg ")
		remain = strings.TrimLeft(remain, " ")
		end := strings.Index(remain, " --build-arg ")
		if end < 0 {
			end = len(remain)
		}
		pair := remain[:end]
		eq := strings.Index(pair, "=")
		if eq < 1 {
			return false
		}
		key := pair[:eq]
		if !isSafeBuildArgKey(key) {
			return false
		}
		val := pair[eq+1:]
		if !isSafeBuildArgValue(val) {
			return false
		}
		if end >= len(remain) {
			break
		}
		remain = remain[end:]
	}
	return true
}

func isSafeBuildArgKey(key string) bool {
	if len(key) == 0 || len(key) > 256 {
		return false
	}
	for i, r := range key {
		if i == 0 && !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r == '_') {
			return false
		}
		if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

func isSafeBuildArgValue(val string) bool {
	if len(val) > 4096 {
		return false
	}
	for _, r := range val {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' || r == '/' || r == ':' || r == ' ' || r == ',' || r == '+' || r == '=' {
			continue
		}
		return false
	}
	return true
}
