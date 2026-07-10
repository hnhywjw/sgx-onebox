package executor

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/monkeycode-ai/sgx-onebox-platform/internal/domain"
	"github.com/monkeycode-ai/sgx-onebox-platform/internal/runtimebundle"
	"github.com/monkeycode-ai/sgx-onebox-platform/internal/security"
	"golang.org/x/crypto/ssh"
)

type provisioningCommandRunner interface {
	Run(node domain.ClusterNode, command string, timeout time.Duration) (string, error)
}

type provisioningOfflineRunner interface {
	RunOfflineK3s(node domain.ClusterNode, pkg domain.InstallPackage, role domain.NodeK3sRole, endpoint string, token string, timeout time.Duration) (string, error)
}

type realProvisioningCommandRunner struct {
	timeout time.Duration
}

func (r realProvisioningCommandRunner) Run(node domain.ClusterNode, command string, timeout time.Duration) (string, error) {
	if timeout <= 0 {
		timeout = r.timeout
	}
	client, err := connectProvisioningSSH(node, timeout)
	if err != nil {
		return "", err
	}
	defer client.Close()
	return sshExec(client, command, timeout)
}

func (r realProvisioningCommandRunner) RunOfflineK3s(node domain.ClusterNode, pkg domain.InstallPackage, role domain.NodeK3sRole, endpoint string, token string, timeout time.Duration) (string, error) {
	if timeout <= 0 {
		timeout = r.timeout * time.Duration(offlineTimeoutMultiplier())
	}
	if strings.TrimSpace(pkg.FilePath) == "" {
		return "", errors.New("离线资源包文件路径为空")
	}
	if info, err := os.Stat(pkg.FilePath); err != nil || info.IsDir() {
		return "", errors.New("离线资源包文件不可读取")
	}
	client, err := connectProvisioningSSH(node, timeout)
	if err != nil {
		return "", err
	}
	defer client.Close()
	remoteDir := "/tmp/sgx-onebox-provisioning-" + safeRemoteName(node.ID)
	defer func() {
		sshExec(client, "rm -rf "+shellQuote(remoteDir), timeout/2)
	}()
	remoteBundle := remoteDir + "/" + safeRemoteName(filepath.Base(pkg.FilePath))
	if _, err := sshExec(client, "mkdir -p "+shellQuote(remoteDir), timeout); err != nil {
		return "", err
	}
	if _, err := sshUploadFileBase64(client, pkg.FilePath, remoteBundle, timeout); err != nil {
		return "", err
	}
	return sshExec(client, offlineK3sInstallCommand(remoteBundle, role, endpoint, token), timeout)
}

func connectProvisioningSSH(node domain.ClusterNode, timeout time.Duration) (*ssh.Client, error) {
	if strings.TrimSpace(node.SSHHost) == "" || strings.TrimSpace(node.SSHUsername) == "" || strings.TrimSpace(node.SSHPasswordCiphertext) == "" {
		return nil, errors.New("目标节点缺少 SSH 凭据")
	}
	password, err := security.DecryptString(node.SSHPasswordCiphertext)
	if err != nil || password == "" {
		return nil, fmt.Errorf("解密 SSH 凭据失败: %v", err)
	}
	defer zeroPassword([]byte(password))
	return sshConnect(node.SSHHost, node.SSHPort, node.SSHUsername, []byte(password), node.SSHKnownHostKey, timeout)
}

type stepResult struct {
	message  string
	evidence string
	node     domain.ClusterNode
}

func (e *Executor) pollProvisioningTasks() {
	tasks := e.store.ListProvisioningTasks()
	if len(tasks) == 0 {
		return
	}
	nodes := e.store.ListClusterNodes()
	sem := make(chan struct{}, e.provisioningConcurrency)
	var wg sync.WaitGroup
	for _, task := range tasks {
		if task.Status != domain.ProvisionPending {
			continue
		}
		node, ok := findProvisioningNode(nodes, task.NodeID)
		if !ok {
			task.Status = domain.ProvisionFailed
			task.Message = "目标节点不存在"
			task.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
			if err := e.store.SaveProvisioningTaskStatus(task); err != nil {
				logWarn(fmt.Sprintf("persist missing-node provisioning task failed: %v", err))
			}
			continue
		}
		sem <- struct{}{}
		wg.Add(1)
		go func(t domain.ProvisioningTask, n domain.ClusterNode) {
			defer wg.Done()
			defer func() { <-sem }()
			e.runProvisioningTask(t, n, nodes)
		}(task, node)
	}
	wg.Wait()
}

func (e *Executor) runProvisioningTask(task domain.ProvisioningTask, node domain.ClusterNode, nodes []domain.ClusterNode) {
	if latest, ok := findProvisioningTask(e.store.ListProvisioningTasks(), task.ID); ok && latest.Status == domain.ProvisionCancelled {
		return
	}
	if task.StartedAt == "" {
		task.StartedAt = time.Now().UTC().Format(time.RFC3339)
	}
	task.Status = domain.ProvisionRunning
	node.ProvisionStatus = string(domain.ProvisionRunning)
	node.ProvisionTaskID = task.ID
	_ = e.store.SaveClusterNodeStatus(node)
	if err := e.store.SaveProvisioningTaskStatus(task); err != nil {
		logWarn(fmt.Sprintf("persist running provisioning task failed: %v", err))
	}
	for {
		if latest, ok := findProvisioningTask(e.store.ListProvisioningTasks(), task.ID); ok && latest.Status == domain.ProvisionCancelled {
			return
		}
		stepIndex := nextProvisioningStepIndex(task)
		if stepIndex < 0 {
			now := time.Now().UTC().Format(time.RFC3339)
			task.Status = domain.ProvisionSucceeded
			task.CurrentStep = ""
			task.UpdatedAt = now
			task.CompletedAt = now
			task.Message = "自动装机任务已完成"
			node.Status = "ready"
			node.ProvisionStatus = string(domain.ProvisionSucceeded)
			node.RuntimeStatus = domain.RuntimeReady
			if task.EnableSGX {
				node.SGXStatus = domain.SGXReady
			}
			_ = e.store.SaveClusterNodeStatus(node)
			if err := e.store.SaveProvisioningTaskStatus(task); err != nil {
				logWarn(fmt.Sprintf("persist succeeded provisioning task failed: %v", err))
			}
			return
		}
		step := task.Steps[stepIndex]
		now := time.Now().UTC().Format(time.RFC3339)
		step.Status = domain.StepRunning
		step.StartedAt = now
		step.Message = "步骤执行中"
		task.Status = domain.ProvisionRunning
		task.CurrentStep = step.Name
		task.UpdatedAt = now
		task.Steps[stepIndex] = step
		if err := e.store.SaveProvisioningTaskStatus(task); err != nil {
			logWarn(fmt.Sprintf("persist running provisioning task failed: %v", err))
		}
		result, err := e.runProvisioningStep(step.Name, node, task, nodes)
		if latest, ok := findProvisioningTask(e.store.ListProvisioningTasks(), task.ID); ok && latest.Status == domain.ProvisionCancelled {
			return
		}
		node = result.node
		finishedAt := time.Now().UTC().Format(time.RFC3339)
		step.FinishedAt = finishedAt
		step.Evidence = redactProvisioningEvidence(result.evidence)
		if err != nil {
			step.Status = domain.StepFailed
			step.Message = result.message
			task.Status = domain.ProvisionFailed
			task.CurrentStep = step.Name
			task.UpdatedAt = finishedAt
			task.CompletedAt = finishedAt
			task.Message = result.message
			task.Steps[stepIndex] = step
			node.ProvisionStatus = string(domain.ProvisionFailed)
			if step.Name == "sgx_dcap_install" {
				node.Status = "ready"
				node.RuntimeStatus = domain.RuntimeReady
				node.SGXStatus = domain.SGXPending
			}
			_ = e.store.SaveClusterNodeStatus(node)
			if saveErr := e.store.SaveProvisioningTaskStatus(task); saveErr != nil {
				logWarn(fmt.Sprintf("persist failed provisioning task failed: %v", saveErr))
			}
			return
		}
		step.Status = domain.StepSucceeded
		step.Message = result.message
		task.Steps[stepIndex] = step
		task.UpdatedAt = finishedAt
		task.Message = result.message
		if err := e.store.SaveProvisioningTaskStatus(task); err != nil {
			logWarn(fmt.Sprintf("persist provisioning step success failed: %v", err))
		}
		if latest, ok := findProvisioningTask(e.store.ListProvisioningTasks(), task.ID); ok && latest.Status == domain.ProvisionCancelled {
			return
		}
		_ = e.store.SaveClusterNodeStatus(node)
	}
}

func (e *Executor) runProvisioningStep(name string, node domain.ClusterNode, task domain.ProvisioningTask, nodes []domain.ClusterNode) (stepResult, error) {
	result := stepResult{node: node}
	var command string
	var timeout time.Duration
	switch name {
	case "preflight":
		command = preflightCommand(task.EnableSGX)
		timeout = e.sshTimeout * 3
	case "k3s_server_install":
		endpoint := controlPlaneEndpoint(node, nodes)
		token := strings.TrimSpace(os.Getenv("K3S_TOKEN"))
		joiningExistingControlPlane := endpoint != "" && hasOtherControlPlane(node, nodes)
		if joiningExistingControlPlane && token == "" {
			return resultWithMessage(node, "缺少 K3S_TOKEN，无法加入已有控制面", "K3S_TOKEN missing"), errors.New("缺少 K3S_TOKEN")
		}
		if task.Mode == domain.ProvisionModeOffline {
			return e.runOfflineK3sStep(node, task, domain.K3sRoleControlPlane, endpoint, token)
		}
		command = k3sServerInstallCommand(node.InstallChannel, endpoint, token, joiningExistingControlPlane)
		timeout = e.sshTimeout * 30
	case "k3s_agent_install":
		endpoint := controlPlaneEndpoint(node, nodes)
		if endpoint == "" {
			return resultWithMessage(node, "缺少控制面地址，无法执行 K3s agent join", "control plane endpoint missing"), errors.New("缺少控制面地址")
		}
		token := strings.TrimSpace(os.Getenv("K3S_TOKEN"))
		if token == "" {
			return resultWithMessage(node, "缺少 K3S_TOKEN，无法执行 K3s agent join", "K3S_TOKEN missing"), errors.New("缺少 K3S_TOKEN")
		}
		if task.Mode == domain.ProvisionModeOffline {
			return e.runOfflineK3sStep(node, task, domain.K3sRoleWorker, endpoint, token)
		}
		command = k3sAgentInstallCommand(endpoint, token, node.InstallChannel)
		timeout = e.sshTimeout * 30
	case "runtime_verify":
		command = runtimeVerifyCommand()
		timeout = e.sshTimeout * 5
	case "sgx_dcap_install":
		command = sgxDCAPVerifyCommand()
		timeout = e.sshTimeout * 5
	case "final_verify":
		command = finalVerifyCommand()
		timeout = e.sshTimeout * 5
	default:
		return resultWithMessage(node, "未知自动装机步骤", name), errors.New("未知自动装机步骤")
	}
	output, err := e.provisioningRunner.Run(node, command, timeout)
	if err != nil {
		return resultWithMessage(node, fmt.Sprintf("步骤 %s 执行失败", name), output), err
	}
	result.message = fmt.Sprintf("步骤 %s 执行成功", name)
	result.evidence = output
	applyProvisioningStepNodeState(&result.node, name, output, task.EnableSGX)
	return result, nil
}

func (e *Executor) runOfflineK3sStep(node domain.ClusterNode, task domain.ProvisioningTask, role domain.NodeK3sRole, endpoint string, token string) (stepResult, error) {
	runner, ok := e.provisioningRunner.(provisioningOfflineRunner)
	if !ok {
		return resultWithMessage(node, "当前执行器不支持离线资源包传输", "offline runner unsupported"), errors.New("执行器不支持离线资源包传输")
	}
	var pkg domain.InstallPackage
	if task.OfflineBundleID == domain.OfflineBundleBuiltin {
		var found bool
		pkg, found = resolveBuiltInBundle(node.Arch)
		if !found {
			return resultWithMessage(node, "内置运行时资源包未找到，请先执行 runtime-bundles/fetch.sh 下载组件", "built-in bundle missing"), errors.New("内置运行时资源包未找到")
		}
		defer func() {
			if pkg.ID == domain.OfflineBundleBuiltin && strings.TrimSpace(pkg.FilePath) != "" {
				_ = os.Remove(pkg.FilePath)
			}
		}()
	} else {
		var ok bool
		pkg, ok = findOfflineInstallPackage(e.store.ListInstallPackages(), task.OfflineBundleID)
		if !ok {
			return resultWithMessage(node, "离线资源包不存在或未校验", "offline bundle missing"), errors.New("离线资源包不存在或未校验")
		}
	}
	output, err := runner.RunOfflineK3s(node, pkg, role, endpoint, token, 0)
	result := stepResult{node: node, evidence: output}
	if err != nil {
		result.message = "离线 K3s 安装执行失败"
		return result, err
	}
	result.message = "离线 K3s 安装执行成功"
	if role == domain.K3sRoleControlPlane {
		applyProvisioningStepNodeState(&result.node, "k3s_server_install", output, task.EnableSGX)
	} else {
		applyProvisioningStepNodeState(&result.node, "k3s_agent_install", output, task.EnableSGX)
	}
	return result, nil
}

func findOfflineInstallPackage(packages []domain.InstallPackage, id string) (domain.InstallPackage, bool) {
	for _, pkg := range packages {
		if pkg.ID == id && pkg.Mode == domain.InstallModeOffline && pkg.Offline && strings.TrimSpace(pkg.FilePath) != "" {
			return pkg, true
		}
	}
	return domain.InstallPackage{}, false
}

func resolveBuiltInBundle(nodeArch string) (domain.InstallPackage, bool) {
	baseDir := runtimebundle.ResolveBuiltInBundleDir()
	arch := normalizeArchForBundle(nodeArch)
	if arch == "" {
		return domain.InstallPackage{}, false
	}
	candidates := []string{
		filepath.Join(baseDir, "linux-"+arch),
	}
	for _, dir := range candidates {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			manifestPath := filepath.Join(dir, "manifest.json")
			if _, err := os.Stat(manifestPath); err != nil {
				continue
			}
			tarball := filepath.Join(os.TempDir(), fmt.Sprintf("sgx-onebox-builtin-bundle-%s-%d.tar.gz", filepath.Base(dir), time.Now().UnixNano()))
			if err := createBundleTarball(dir, tarball); err == nil {
				return domain.InstallPackage{
					ID:       domain.OfflineBundleBuiltin,
					Name:     "built-in-" + filepath.Base(dir),
					Mode:     domain.InstallModeOffline,
					Offline:  true,
					FilePath: tarball,
				}, true
			}
		}
	}
	return domain.InstallPackage{}, false
}

func offlineTimeoutMultiplier() int {
	if v := os.Getenv("PLATFORM_OFFLINE_UPLOAD_TIMEOUT_MULTIPLIER"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return 60
}

func normalizeArchForBundle(arch string) string {
	switch strings.TrimSpace(strings.ToLower(arch)) {
	case "x86_64", "amd64", "linux-amd64":
		return "amd64"
	case "aarch64", "arm64", "linux-arm64":
		return "arm64"
	default:
		return ""
	}
}

func createBundleTarball(dir string, dest string) error {
	cmd := exec.Command("tar", "-czf", dest, "-C", dir, ".")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("创建内置资源包 tarball 失败: %v: %s", err, string(output))
	}
	return nil
}

func offlineK3sInstallCommand(remoteBundle string, role domain.NodeK3sRole, endpoint string, token string) string {
	workDir := remoteBundle + "-extracted"
	installEnv := "INSTALL_K3S_SKIP_DOWNLOAD=true"
	if role == domain.K3sRoleWorker {
		installEnv += " K3S_URL=" + shellQuote("https://"+endpoint) + " K3S_TOKEN=" + shellQuote(token) + " INSTALL_K3S_EXEC=agent"
	} else if strings.TrimSpace(endpoint) != "" && strings.TrimSpace(token) != "" {
		installEnv += " K3S_URL=" + shellQuote("https://"+endpoint) + " K3S_TOKEN=" + shellQuote(token) + " INSTALL_K3S_EXEC=server"
	} else {
		installEnv += " INSTALL_K3S_EXEC=\"server --cluster-init\""
	}
	return fmt.Sprintf("set -e; work=%s; bundle=%s; mkdir -p \"$work\"; case \"$bundle\" in *.tar.gz|*.tgz) tar -xzf \"$bundle\" -C \"$work\" ;; *.tar) tar -xf \"$bundle\" -C \"$work\" ;; *.zip) command -v unzip >/dev/null 2>&1 || { echo unzip is required but not installed; exit 2; }; unzip -oq \"$bundle\" -d \"$work\" ;; *) echo unsupported offline bundle format; exit 2 ;; esac; if [ -x \"$work/install.sh\" ]; then cd \"$work\" && %s ./install.sh; elif [ -x \"$work/bin/install.sh\" ]; then cd \"$work/bin\" && %s ./install.sh; else echo offline bundle missing executable install.sh; exit 2; fi; if command -v k3s >/dev/null 2>&1; then k3s --version; fi; if command -v kubectl >/dev/null 2>&1; then kubectl version --client=true; elif command -v k3s >/dev/null 2>&1; then k3s kubectl version --client=true; fi 2>&1", shellQuote(workDir), shellQuote(remoteBundle), installEnv, installEnv)
}

func safeRemoteName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "bundle"
	}
	builder := strings.Builder{}
	for _, ch := range value {
		if ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '.' || ch == '-' || ch == '_' {
			builder.WriteRune(ch)
		} else {
			builder.WriteRune('-')
		}
	}
	return strings.Trim(builder.String(), "-")
}

func resultWithMessage(node domain.ClusterNode, message string, evidence string) stepResult {
	return stepResult{node: node, message: message, evidence: evidence}
}

func nextProvisioningStepIndex(task domain.ProvisioningTask) int {
	for index, step := range task.Steps {
		if step.Status == domain.StepPending || step.Status == domain.StepRunning || step.Status == domain.StepFailed {
			return index
		}
	}
	return -1
}

func findProvisioningTask(tasks []domain.ProvisioningTask, taskID string) (domain.ProvisioningTask, bool) {
	for _, task := range tasks {
		if task.ID == taskID {
			return task, true
		}
	}
	return domain.ProvisioningTask{}, false
}

func findProvisioningNode(nodes []domain.ClusterNode, nodeID string) (domain.ClusterNode, bool) {
	for _, node := range nodes {
		if node.ID == nodeID {
			return node, true
		}
	}
	return domain.ClusterNode{}, false
}

func controlPlaneEndpoint(node domain.ClusterNode, nodes []domain.ClusterNode) string {
	if endpoint := strings.TrimSpace(node.ControlPlaneEndpoint); endpoint != "" {
		return strings.TrimPrefix(strings.TrimPrefix(endpoint, "https://"), "http://")
	}
	for _, item := range nodes {
		if item.ID == node.ID || !isControlPlaneNode(item) {
			continue
		}
		if item.Status != "" && item.Status != "ready" {
			continue
		}
		if item.InternalIP != "" {
			return item.InternalIP + ":6443"
		}
		if item.ManagementIP != "" {
			return item.ManagementIP + ":6443"
		}
		if item.SSHHost != "" {
			return item.SSHHost + ":6443"
		}
	}
	return ""
}

func hasOtherControlPlane(node domain.ClusterNode, nodes []domain.ClusterNode) bool {
	for _, item := range nodes {
		if item.ID != node.ID && isControlPlaneNode(item) {
			return true
		}
	}
	return false
}

func isControlPlaneNode(node domain.ClusterNode) bool {
	return node.Role == "control-plane" || node.K3sRole == domain.K3sRoleControlPlane
}

func preflightCommand(enableSGX bool) string {
	cmd := "set -e; uname -m; cat /etc/os-release; df -BG /; free -m; command -v curl || command -v wget; command -v k3s || true"
	if enableSGX {
		cmd += "; grep -i sgx /proc/cpuinfo || true; ls /dev/sgx_* 2>/dev/null || true"
	}
	return cmd + " 2>&1"
}

func k3sServerInstallCommand(channel string, endpoint string, token string, joiningExistingControlPlane bool) string {
	versionEnv := k3sInstallVersionEnv(channel)
	if joiningExistingControlPlane {
		return fmt.Sprintf("set -e; if command -v k3s >/dev/null 2>&1; then k3s --version; else curl -sfL https://get.k3s.io | K3S_URL=%s K3S_TOKEN=%s %s sh -s - server; fi; k3s kubectl get nodes 2>&1", shellQuote("https://"+endpoint), shellQuote(token), versionEnv)
	}
	return fmt.Sprintf("set -e; if command -v k3s >/dev/null 2>&1; then k3s --version; else curl -sfL https://get.k3s.io | %s sh -s - server --cluster-init; fi; k3s kubectl get nodes 2>&1", versionEnv)
}

func k3sAgentInstallCommand(endpoint string, token string, channel string) string {
	versionEnv := k3sInstallVersionEnv(channel)
	return fmt.Sprintf("set -e; if command -v k3s >/dev/null 2>&1; then k3s --version; else curl -sfL https://get.k3s.io | K3S_URL=%s K3S_TOKEN=%s %s sh -s - agent; fi; systemctl is-active k3s-agent || systemctl is-active k3s 2>&1", shellQuote("https://"+endpoint), shellQuote(token), versionEnv)
}

func k3sInstallVersionEnv(channel string) string {
	value := strings.TrimSpace(channel)
	if isValidK3sVersion(value) {
		return "INSTALL_K3S_VERSION=" + shellQuote(value)
	}
	return ""
}

func runtimeVerifyCommand() string {
	return "set -e; if command -v crictl >/dev/null 2>&1; then crictl --version; elif command -v ctr >/dev/null 2>&1; then ctr version; elif command -v k3s >/dev/null 2>&1; then k3s ctr version; else exit 127; fi; if command -v kubectl >/dev/null 2>&1; then kubectl version --client=true; elif command -v k3s >/dev/null 2>&1; then k3s kubectl version --client=true; else exit 127; fi 2>&1"
}

func sgxDCAPVerifyCommand() string {
	return "ok=true; if ! grep -qi sgx /proc/cpuinfo 2>/dev/null; then echo SGX_PREFLIGHT: CPU does not support SGX; ok=false; fi; if [ ! -e /dev/sgx_enclave ]; then echo SGX_PREFLIGHT: /dev/sgx_enclave missing; ok=false; fi; if [ ! -e /dev/sgx_provision ]; then echo SGX_PREFLIGHT: /dev/sgx_provision missing; ok=false; fi; if [ \"$ok\" = false ]; then exit 1; fi; if command -v dcap-verify-quote >/dev/null 2>&1; then dcap-verify-quote --version 2>&1 || { echo SGX_DCAP: dcap-verify-quote exists but --version failed; exit 1; }; echo SGX_DCAP: dcap-verify-quote available; elif command -v quote_verify >/dev/null 2>&1; then quote_verify --version 2>&1 || { echo SGX_DCAP: quote_verify exists but --version failed; exit 1; }; echo SGX_DCAP: quote_verify available; else echo SGX_DCAP: neither dcap-verify-quote nor quote_verify found; exit 127; fi 2>&1"
}

func finalVerifyCommand() string {
	return "set -e; if command -v kubectl >/dev/null 2>&1; then kubectl get nodes; else k3s kubectl get nodes; fi 2>&1"
}

func applyProvisioningStepNodeState(node *domain.ClusterNode, stepName string, output string, enableSGX bool) {
	now := time.Now().UTC().Format(time.RFC3339)
	node.LastHeartbeat = now
	node.LastJoinAttemptAt = now
	switch stepName {
	case "preflight":
		node.ProvisionStatus = string(domain.ProvisionRunning)
		node.LastJoinMessage = "前置检查已通过"
		if arch := extractArch(output); arch != "" {
			node.Arch = arch
		} else if detected := rawArch(output); detected != "" {
			log.Printf("节点 %s 架构 %s 不在支持列表中(x86_64/aarch64),自动装机可能失败", node.Name, detected)
			node.LastJoinMessage = fmt.Sprintf("不支持的架构: %s,仅支持 x86_64 和 aarch64", detected)
		}
		if osInfo := extractOS(output); osInfo != "" {
			node.OS = osInfo
		}
	case "k3s_server_install", "k3s_agent_install":
		node.JoinStatus = "active"
		node.JoinedAt = now
		node.Status = "ready"
		node.LastJoinMessage = "K3s 安装或纳管已完成"
	case "runtime_verify":
		node.RuntimeStatus = domain.RuntimeReady
		node.LastJoinMessage = "runtime 与 kubectl 验证已通过"
	case "sgx_dcap_install":
		if enableSGX {
			node.SGXStatus = domain.SGXReady
			node.LastJoinMessage = "SGX/DCAP 验证已通过"
		}
	case "final_verify":
		node.Status = "ready"
		node.ProvisionStatus = string(domain.ProvisionSucceeded)
		node.RuntimeStatus = domain.RuntimeReady
		if enableSGX && (node.SGXStatus == domain.SGXUnknown || node.SGXStatus == "") {
			node.SGXStatus = domain.SGXReady
		}
		node.LastJoinMessage = "节点最终健康检查已通过"
	}
	if version := extractK3sVersion(output); version != "" {
		node.Version = version
	}
}

func extractK3sVersion(output string) string {
	for _, field := range strings.Fields(output) {
		field = strings.Trim(field, ",;()[]")
		if strings.HasPrefix(field, "v") && strings.Contains(field, "+k3s") && isValidK3sVersion(field) {
			return field
		}
	}
	return ""
}

func extractArch(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		switch line {
		case "x86_64":
			return "x86_64"
		case "aarch64", "arm64":
			return "aarch64"
		}
	}
	return ""
}

func rawArch(output string) string {
	known := map[string]bool{"x86_64": true, "aarch64": true, "arm64": true, "amd64": true, "armv7l": true, "armv8l": true}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if known[line] {
			return line
		}
	}
	return ""
}

func extractOS(output string) string {
	var name, version string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "ID=") && !strings.Contains(line, "ID_LIKE") {
			name = strings.Trim(line[3:], "\"'")
		}
		if strings.HasPrefix(line, "VERSION_ID=") {
			version = strings.Trim(line[12:], "\"'")
		}
	}
	if name != "" && version != "" {
		return name + " " + version
	}
	if name != "" {
		return name
	}
	return ""
}

func redactProvisioningEvidence(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 2000 {
		value = value[:2000]
	}
	replacers := []string{"K3S_TOKEN", "token", "password", "passwd", "secret"}
	for _, key := range replacers {
		value = redactKeyValue(value, key)
	}
	return value
}

func redactKeyValue(value string, key string) string {
	for _, sep := range []string{"=", ":"} {
		needle := key + sep
		for {
			lowerValue := strings.ToLower(value)
			lowerNeedle := strings.ToLower(needle)
			idx := strings.Index(lowerValue, lowerNeedle)
			if idx < 0 {
				break
			}
			start := idx + len(needle)
			end := start
			for end < len(value) && !strings.ContainsRune(" \n\r\t;,&", rune(value[end])) {
				end++
			}
			if value[start:end] == "***" {
				break
			}
			value = value[:start] + "***" + value[end:]
		}
	}
	return value
}
