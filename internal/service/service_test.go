package service

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monkeycode-ai/sgx-onebox-platform/internal/domain"
	"github.com/monkeycode-ai/sgx-onebox-platform/internal/store"
)

func init() {
	os.Setenv("GO_TEST_MODE", "1")
}

func newTestService(t *testing.T) *PlatformService {
	t.Helper()
	st, err := store.NewSQLiteStore()
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return NewPlatformService(st)
}

func TestServiceHealth(t *testing.T) {
	svc := newTestService(t)
	if err := svc.Health(); err != nil {
		t.Fatalf("Health: %v", err)
	}
}

func TestSnapshotCollectionsMarshalAsArrays(t *testing.T) {
	svc := newTestService(t)
	snapshot := svc.Snapshot()
	payload, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("Marshal snapshot: %v", err)
	}
	for _, field := range []string{"topoEgress", "topoLinks", "provisioningTasks", "installPackages", "systemSettings"} {
		if bytes.Contains(payload, []byte(`"`+field+`":null`)) {
			t.Fatalf("expected %s to marshal as array, got %s", field, string(payload))
		}
	}
	if bytes.Contains(payload, []byte("\"sessions\":[")) || bytes.Contains(payload, []byte("\"loginFailures\":{")) {
		t.Fatalf("snapshot leaks internal session state: %s", string(payload))
	}
}

func TestSaveNetworkVLANConflictDoesNotAppendExecutorLogs(t *testing.T) {
	svc := newTestService(t)
	before := svc.Snapshot()
	logCount := len(before.ClusterLogs)
	alertCount := len(before.ClusterAlerts)
	err := svc.SaveNetwork(domain.NetworkAttachment{
		Name:      "冲突网络",
		Bridge:    "br-conflict",
		ParentNIC: "eno1.120",
		VLANID:    120,
		Subnet:    "172.16.121.0/24",
		Gateway:   "172.16.121.1",
	})
	if err == nil || !strings.Contains(err.Error(), "VLAN") {
		t.Fatalf("expected VLAN conflict, got %v", err)
	}
	after := svc.Snapshot()
	if len(after.ClusterLogs) != logCount || len(after.ClusterAlerts) != alertCount {
		t.Fatalf("expected conflict validation before executor side effects, logs %d->%d alerts %d->%d", logCount, len(after.ClusterLogs), alertCount, len(after.ClusterAlerts))
	}
}

func TestServiceFindUser(t *testing.T) {
	svc := newTestService(t)
	user, err := svc.FindUser("usr-admin")
	if err != nil {
		t.Fatalf("FindUser: %v", err)
	}
	if user.Username != "admin" {
		t.Fatalf("expected admin, got %s", user.Username)
	}
}

func TestServiceFindUserNotFound(t *testing.T) {
	svc := newTestService(t)
	_, err := svc.FindUser("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent user")
	}
}

func TestServiceListUsers(t *testing.T) {
	svc := newTestService(t)
	users := svc.ListUsers()
	if len(users) == 0 {
		t.Fatal("expected at least one user")
	}
	for _, u := range users {
		if u.Username == "" || u.DisplayName == "" {
			t.Fatalf("user view incomplete: %+v", u)
		}
	}
}

func TestServiceChangePasswordBadCurrent(t *testing.T) {
	svc := newTestService(t)
	err := svc.ChangePassword("usr-admin", domain.ChangePasswordPayload{
		CurrentPassword: "wrong-pass",
		NewPassword:     "newpass123",
	})
	if err == nil {
		t.Fatal("expected error for wrong current password")
	}
}

func TestServiceChangePasswordShortNew(t *testing.T) {
	svc := newTestService(t)
	err := svc.ChangePassword("usr-admin", domain.ChangePasswordPayload{
		CurrentPassword: "admin123",
		NewPassword:     "1234567",
	})
	if err == nil {
		t.Fatal("expected error for short new password")
	}
}

func TestDeployComponentWithoutExecutorStaysPending(t *testing.T) {
	svc := newTestService(t)
	if err := svc.DeployComponent("cmp-key", "test-user"); err != nil {
		t.Fatalf("DeployComponent: %v", err)
	}
	snapshot := svc.Snapshot()
	for _, component := range snapshot.Components {
		if component.ID == "cmp-key" {
			if component.Status != domain.ComponentDeploying {
				t.Fatalf("expected deployment_pending, got %s", component.Status)
			}
			return
		}
	}
	t.Fatal("component cmp-key not found")
}

func TestRunEnclaveInspectionWithoutExecutorStaysPending(t *testing.T) {
	svc := newTestService(t)
	records, err := svc.RunEnclaveInspection()
	if err != nil {
		t.Fatalf("RunEnclaveInspection: %v", err)
	}
	if len(records) == 0 {
		t.Fatal("expected inspection records")
	}
	for _, record := range records {
		if record.Status != "pending" {
			t.Fatalf("expected pending inspection, got %+v", record)
		}
	}
}

func TestScaleComponentWithoutExecutorStaysPendingAndKeepsReplicas(t *testing.T) {
	svc := newTestService(t)
	before := svc.Snapshot()
	originalReplicas := 0
	for _, component := range before.Components {
		if component.ID == "cmp-key" {
			originalReplicas = component.Replicas
			break
		}
	}
	if err := svc.ScaleComponent("cmp-key", originalReplicas+2, "test-user"); err != nil {
		t.Fatalf("ScaleComponent: %v", err)
	}
	snapshot := svc.Snapshot()
	for _, component := range snapshot.Components {
		if component.ID == "cmp-key" {
			if component.Replicas != originalReplicas {
				t.Fatalf("expected replicas to remain %d, got %d", originalReplicas, component.Replicas)
			}
			if component.Status != domain.ComponentDeploying {
				t.Fatalf("expected deployment_pending, got %s", component.Status)
			}
			return
		}
	}
	t.Fatal("component cmp-key not found")
}

func TestUpgradeComponentWithoutExecutorStaysPendingAndKeepsVersion(t *testing.T) {
	svc := newTestService(t)
	before := svc.Snapshot()
	originalVersion := ""
	for _, component := range before.Components {
		if component.ID == "cmp-key" {
			originalVersion = component.Version
			break
		}
	}
	if err := svc.UpgradeComponent("cmp-key", "9.9.9", "admin"); err != nil {
		t.Fatalf("UpgradeComponent: %v", err)
	}
	snapshot := svc.Snapshot()
	for _, component := range snapshot.Components {
		if component.ID == "cmp-key" {
			if component.Version != originalVersion {
				t.Fatalf("expected version to remain %s, got %s", originalVersion, component.Version)
			}
			if component.Status != domain.ComponentDeploying {
				t.Fatalf("expected deployment_pending, got %s", component.Status)
			}
			return
		}
	}
	t.Fatal("component cmp-key not found")
}

func TestSaveClusterNodeJoinCommandDoesNotLeakToken(t *testing.T) {
	t.Setenv("K3S_TOKEN", "test-token")
	svc := newTestService(t)
	if err := svc.SaveClusterNode(domain.ClusterNode{
		Name:         "worker-test",
		Role:         "worker",
		InternalIP:   "10.0.0.20",
		ManagementIP: "10.0.0.20",
		SSHHost:      "10.0.0.20",
		SSHPort:      22,
		SSHUsername:  "root",
		SSHPassword:  "password123",
	}); err != nil {
		t.Fatalf("SaveClusterNode: %v", err)
	}
	snapshot := svc.Snapshot()
	for _, node := range snapshot.ClusterNodes {
		if node.Name == "worker-test" {
			if node.JoinStatus != "join_command_ready" {
				t.Fatalf("expected join_command_ready, got %s", node.JoinStatus)
			}
			if node.JoinCommand == "" {
				t.Fatal("expected join command")
			}
			if strings.Contains(node.JoinCommand, "cluster.example") {
				t.Fatalf("join command contains placeholder host: %s", node.JoinCommand)
			}
			if strings.Contains(node.JoinCommand, "test-token") {
				t.Fatalf("join command leaks token: %s", node.JoinCommand)
			}
			return
		}
	}
	t.Fatal("worker-test not found")
}

func TestSaveImageWithoutExecutorMarksPending(t *testing.T) {
	svc := newTestService(t)
	image := domain.ImageAsset{ID: "image-test-pending", Name: "pending-image", Registry: "registry.local", Repository: "security/test", Tag: "1.0.0"}
	if err := svc.SaveImage(image); err != nil {
		t.Fatalf("SaveImage: %v", err)
	}
	for _, item := range svc.store.Snapshot().Images {
		if item.ID == image.ID {
			if item.Vulnerability != "pending_executor" {
				t.Fatalf("expected pending_executor, got %s", item.Vulnerability)
			}
			return
		}
	}
	t.Fatal("image not found")
}

func TestBuildImageCreatesImageAssetAndLog(t *testing.T) {
	svc := newTestService(t)
	result, err := svc.BuildImage(domain.ImageBuildRequest{Name: "secure-waf", Registry: "registry.local", Repository: "security/secure-waf", Tag: "1.0.0", SourcePackage: "/uploads/images/secure-waf.tar.gz", DockerfilePath: "Dockerfile", EnableSignature: true, GenerateSBOM: true, EnableSGXRuntime: true})
	if err != nil {
		t.Fatalf("BuildImage: %v", err)
	}
	if result.Image.ID == "" || !result.Image.Signed || !result.Image.SBOM || !result.Image.EnclaveReady {
		t.Fatalf("unexpected image build result: %+v", result)
	}
	if !strings.Contains(result.Log, "secure-waf") {
		t.Fatalf("expected build log, got %q", result.Log)
	}
	foundLog := false
	for _, item := range svc.Snapshot().ClusterLogs {
		if item.Category == "image" && strings.Contains(item.Message, "镜像构建任务已登记") {
			foundLog = true
			break
		}
	}
	if !foundLog {
		t.Fatal("expected image build cluster log")
	}
}

func TestBuildImageRequiresSourcePackage(t *testing.T) {
	svc := newTestService(t)
	_, err := svc.BuildImage(domain.ImageBuildRequest{Name: "secure-waf", Registry: "registry.local", Repository: "security/secure-waf", Tag: "1.0.0"})
	if err == nil || !strings.Contains(err.Error(), "制品包") {
		t.Fatalf("expected source package error, got %v", err)
	}
}

func TestSaveInstallPackageRequiresReadableFile(t *testing.T) {
	svc := newTestService(t)
	err := svc.SaveInstallPackage(domain.InstallPackage{ID: "pkg-missing", Name: "missing", Version: "1.0.0", Mode: domain.InstallModeOffline, FilePath: "/tmp/missing-package-file.tgz"})
	if err == nil {
		t.Fatal("expected missing package file error")
	}
}

func TestSaveInstallPackageLoadsOfflineBundleManifest(t *testing.T) {
	svc := newTestService(t)
	bundlePath := writeOfflineBundle(t, map[string][]byte{"bin/k3s": []byte("k3s-binary")}, domain.OfflineBundleManifest{
		K3sVersion:     "v1.30.2+k3s1",
		RuntimeVersion: "containerd 1.7.0",
		KubectlVersion: "v1.30.2",
		DCAPVersion:    "1.20",
		OSFamily:       []string{"ubuntu-22.04", "linux"},
	})
	err := svc.SaveInstallPackage(domain.InstallPackage{ID: "pkg-offline-ok", Name: "offline ok", Version: "1.0.0", Mode: domain.InstallModeOffline, FilePath: bundlePath})
	if err != nil {
		t.Fatalf("SaveInstallPackage: %v", err)
	}
	var saved domain.InstallPackage
	for _, item := range svc.Snapshot().InstallPackages {
		if item.ID == "pkg-offline-ok" {
			saved = item
			break
		}
	}
	if !saved.Offline || saved.Manifest.K3sVersion != "v1.30.2+k3s1" || len(saved.Manifest.SHA256) == 0 {
		t.Fatalf("expected offline manifest persisted, got %+v", saved)
	}
}

func TestSaveInstallPackageRejectsOfflineBundleWithoutManifest(t *testing.T) {
	svc := newTestService(t)
	bundlePath := writeTarGz(t, map[string][]byte{"bin/k3s": []byte("k3s-binary")})
	err := svc.SaveInstallPackage(domain.InstallPackage{ID: "pkg-no-manifest", Name: "missing manifest", Version: "1.0.0", Mode: domain.InstallModeOffline, FilePath: bundlePath})
	if err == nil || !strings.Contains(err.Error(), "manifest") {
		t.Fatalf("expected manifest error, got %v", err)
	}
}

func TestSaveInstallPackageRejectsOfflineBundleHashMismatch(t *testing.T) {
	svc := newTestService(t)
	bundlePath := writeOfflineBundleWithHashes(t, map[string][]byte{"bin/k3s": []byte("k3s-binary")}, domain.OfflineBundleManifest{
		K3sVersion:     "v1.30.2+k3s1",
		RuntimeVersion: "containerd 1.7.0",
		KubectlVersion: "v1.30.2",
		OSFamily:       []string{"linux"},
		SHA256:         map[string]string{"bin/k3s": strings.Repeat("0", 64)},
	})
	err := svc.SaveInstallPackage(domain.InstallPackage{ID: "pkg-bad-hash", Name: "bad hash", Version: "1.0.0", Mode: domain.InstallModeOffline, FilePath: bundlePath})
	if err == nil || !strings.Contains(err.Error(), "SHA256") {
		t.Fatalf("expected SHA256 error, got %v", err)
	}
}

func TestSaveInstallPackageRejectsOfflineBundleIncompatibleOS(t *testing.T) {
	svc := newTestService(t)
	bundlePath := writeOfflineBundle(t, map[string][]byte{"bin/k3s": []byte("k3s-binary")}, domain.OfflineBundleManifest{
		K3sVersion:     "v1.30.2+k3s1",
		RuntimeVersion: "containerd 1.7.0",
		KubectlVersion: "v1.30.2",
		OSFamily:       []string{"windows"},
	})
	err := svc.SaveInstallPackage(domain.InstallPackage{ID: "pkg-bad-os", Name: "bad os", Version: "1.0.0", Mode: domain.InstallModeOffline, FilePath: bundlePath})
	if err == nil || !strings.Contains(err.Error(), "兼容") {
		t.Fatalf("expected compatibility error, got %v", err)
	}
}

func writeOfflineBundle(t *testing.T, files map[string][]byte, manifest domain.OfflineBundleManifest) string {
	t.Helper()
	manifest.SHA256 = map[string]string{}
	for name, content := range files {
		sum := sha256.Sum256(content)
		manifest.SHA256[name] = hex.EncodeToString(sum[:])
	}
	return writeOfflineBundleWithHashes(t, files, manifest)
}

func writeOfflineBundleWithHashes(t *testing.T, files map[string][]byte, manifest domain.OfflineBundleManifest) string {
	t.Helper()
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("Marshal manifest: %v", err)
	}
	archiveFiles := map[string][]byte{"manifest.json": manifestBytes}
	for name, content := range files {
		archiveFiles[name] = content
	}
	return writeTarGz(t, archiveFiles)
}

func writeTarGz(t *testing.T, files map[string][]byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "offline-bundle.tgz")
	var buffer bytes.Buffer
	gzipWriter := gzip.NewWriter(&buffer)
	tarWriter := tar.NewWriter(gzipWriter)
	for name, content := range files {
		header := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(content))}
		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatalf("WriteHeader: %v", err)
		}
		if _, err := tarWriter.Write(content); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatalf("tar Close: %v", err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatalf("gzip Close: %v", err)
	}
	if err := os.WriteFile(path, buffer.Bytes(), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

func TestSaveSecurityPolicyWithoutExecutorStaysStaged(t *testing.T) {
	svc := newTestService(t)
	rule := domain.SecurityPolicyRule{ID: "policy-test", Name: "test policy", Category: "network", Scope: "default", Mode: "enforce", Status: "active"}
	if err := svc.SaveSecurityPolicyRule(rule); err != nil {
		t.Fatalf("SaveSecurityPolicyRule: %v", err)
	}
	for _, item := range svc.store.Snapshot().SecurityPolicies {
		if item.ID == rule.ID {
			if item.Status != "staged" {
				t.Fatalf("expected staged, got %s", item.Status)
			}
			return
		}
	}
	t.Fatal("policy not found")
}

func TestSaveClusterNodeStatusPreservesConcurrentFields(t *testing.T) {
	svc := newTestService(t)
	adapter := &nodeStoreAdapter{svc: svc}
	snapshot := svc.store.Snapshot()
	if len(snapshot.ClusterNodes) == 0 {
		t.Fatal("expected seeded cluster node")
	}
	node := snapshot.ClusterNodes[0]
	node.Name = "renamed-by-user"
	snapshot.ClusterNodes[0] = node
	if err := svc.store.Replace(snapshot); err != nil {
		t.Fatalf("Replace: %v", err)
	}
	stale := node
	stale.Name = "stale-name"
	stale.Status = "unreachable"
	stale.CPUUsage = 77
	stale.LastJoinMessage = "poll update"
	if err := adapter.SaveClusterNodeStatus(stale); err != nil {
		t.Fatalf("SaveClusterNodeStatus: %v", err)
	}
	updated := svc.store.Snapshot().ClusterNodes[0]
	if updated.Name != "renamed-by-user" {
		t.Fatalf("expected name to be preserved, got %s", updated.Name)
	}
	if updated.Status != "unreachable" || updated.CPUUsage != 77 || updated.LastJoinMessage != "poll update" {
		t.Fatalf("expected status fields to update, got %+v", updated)
	}
}

func TestSaveClusterNodeAutoProvisionControlPlaneCreatesTask(t *testing.T) {
	svc := newTestService(t)
	err := svc.SaveClusterNode(domain.ClusterNode{
		Name:          "auto-control",
		Role:          "control-plane",
		InternalIP:    "10.0.1.10",
		ManagementIP:  "10.0.1.10",
		SSHHost:       "10.0.1.10",
		SSHPort:       22,
		SSHUsername:   "root",
		SSHPassword:   "password123",
		AutoProvision: true,
		ProvisionMode: domain.ProvisionModeOnline,
		K3sRole:       domain.K3sRoleControlPlane,
	})
	if err != nil {
		t.Fatalf("SaveClusterNode: %v", err)
	}
	snapshot := svc.Snapshot()
	task := findProvisioningTaskByNode(t, snapshot, "node-auto-control")
	if task.Role != domain.K3sRoleControlPlane || task.CurrentStep != "preflight" {
		t.Fatalf("unexpected task: %+v", task)
	}
	if !hasProvisioningStep(task, "k3s_server_install") {
		t.Fatalf("expected k3s_server_install step, got %+v", task.Steps)
	}
	if hasProvisioningStep(task, "sgx_dcap_install") {
		t.Fatalf("unexpected sgx step for non SGX node")
	}
	for _, node := range snapshot.ClusterNodes {
		if node.Name == "auto-control" {
			if node.ProvisionTaskID != task.ID || node.ProvisionStatus != string(domain.ProvisionPending) || node.RuntimeStatus != domain.RuntimePending {
				t.Fatalf("unexpected node provisioning fields: %+v", node)
			}
			return
		}
	}
	t.Fatal("auto-control not found")
}

func TestSaveClusterNodeAutoProvisionWorkerWithSGXCreatesTask(t *testing.T) {
	t.Setenv("K3S_TOKEN", "worker-token")
	svc := newTestService(t)
	bundlePath := writeOfflineBundle(t, map[string][]byte{"bin/k3s": []byte("k3s-binary")}, domain.OfflineBundleManifest{
		K3sVersion:       "v1.30.2+k3s1",
		RuntimeVersion:   "containerd 1.7.0",
		KubectlVersion:   "v1.30.2",
		SGXDriverVersion: "2.20",
		DCAPVersion:      "1.20",
		OSFamily:         []string{"linux"},
	})
	if err := svc.SaveInstallPackage(domain.InstallPackage{ID: "pkg-offline-230", Name: "offline 230", Version: "1.0.0", Mode: domain.InstallModeOffline, FilePath: bundlePath}); err != nil {
		t.Fatalf("SaveInstallPackage: %v", err)
	}
	err := svc.SaveClusterNode(domain.ClusterNode{
		Name:                 "auto-worker-sgx",
		Role:                 "worker",
		InternalIP:           "10.0.1.20",
		ManagementIP:         "10.0.1.20",
		SSHHost:              "10.0.1.20",
		SSHPort:              22,
		SSHUsername:          "root",
		SSHPassword:          "password123",
		AutoProvision:        true,
		EnableSGX:            true,
		ProvisionMode:        domain.ProvisionModeOffline,
		K3sRole:              domain.K3sRoleWorker,
		OfflineBundleID:      "pkg-offline-230",
		ControlPlaneEndpoint: "10.0.0.11:6443",
	})
	if err != nil {
		t.Fatalf("SaveClusterNode: %v", err)
	}
	snapshot := svc.Snapshot()
	task := findProvisioningTaskByNode(t, snapshot, "node-auto-worker-sgx")
	if task.Role != domain.K3sRoleWorker || task.Mode != domain.ProvisionModeOffline || !task.EnableSGX || task.OfflineBundleID != "pkg-offline-230" {
		t.Fatalf("unexpected task: %+v", task)
	}
	for _, stepName := range []string{"preflight", "k3s_agent_install", "runtime_verify", "sgx_dcap_install", "final_verify"} {
		if !hasProvisioningStep(task, stepName) {
			t.Fatalf("expected step %s in %+v", stepName, task.Steps)
		}
	}
	for _, node := range snapshot.ClusterNodes {
		if node.Name == "auto-worker-sgx" {
			if node.SGXStatus != domain.SGXPending {
				t.Fatalf("expected sgx_pending, got %s", node.SGXStatus)
			}
			if strings.Contains(node.JoinCommand, "worker-token") {
				t.Fatalf("join command leaks token: %s", node.JoinCommand)
			}
			return
		}
	}
	t.Fatal("auto-worker-sgx not found")
}

func TestSaveClusterNodeAutoProvisionRejectsMissingOfflineBundle(t *testing.T) {
	svc := newTestService(t)
	err := svc.SaveClusterNode(domain.ClusterNode{
		Name:            "auto-missing-bundle",
		Role:            "worker",
		InternalIP:      "10.0.1.35",
		ManagementIP:    "10.0.1.35",
		SSHHost:         "10.0.1.35",
		SSHPort:         22,
		SSHUsername:     "root",
		SSHPassword:     "password123",
		AutoProvision:   true,
		ProvisionMode:   domain.ProvisionModeOffline,
		K3sRole:         domain.K3sRoleWorker,
		OfflineBundleID: "pkg-not-exist",
	})
	if err == nil || !strings.Contains(err.Error(), "离线资源包") {
		t.Fatalf("expected missing offline bundle error, got %v", err)
	}
}

func TestSaveClusterNodeAutoProvisionRequiresSSHCredentials(t *testing.T) {
	svc := newTestService(t)
	err := svc.SaveClusterNode(domain.ClusterNode{
		Name:          "auto-missing-ssh",
		Role:          "worker",
		InternalIP:    "10.0.1.30",
		ManagementIP:  "10.0.1.30",
		SSHPort:       22,
		SSHUsername:   "root",
		SSHPassword:   "password123",
		AutoProvision: true,
	})
	if err == nil {
		t.Fatal("expected missing SSH host error")
	}
	if !strings.Contains(err.Error(), "SSH 主机") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRetryProvisioningTaskResetsFailedStep(t *testing.T) {
	svc := newTestService(t)
	task, err := svc.CreateProvisioningTask(domain.ClusterNode{ID: "node-retry", Role: "worker", K3sRole: domain.K3sRoleWorker, ProvisionMode: domain.ProvisionModeOnline}, "operator")
	if err != nil {
		t.Fatalf("CreateProvisioningTask: %v", err)
	}
	task.Status = domain.ProvisionFailed
	task.CurrentStep = "runtime_verify"
	task.Steps[0].Status = domain.StepSucceeded
	task.Steps[1].Status = domain.StepFailed
	task.Steps[1].Message = "failed"
	if err := svc.SaveProvisioningTaskStatus(task); err != nil {
		t.Fatalf("SaveProvisioningTaskStatus: %v", err)
	}
	if err := svc.RetryProvisioningTask(task.ID, "operator"); err != nil {
		t.Fatalf("RetryProvisioningTask: %v", err)
	}
	updated := findProvisioningTask(t, svc.Snapshot(), task.ID)
	if updated.Status != domain.ProvisionPending || updated.Steps[1].Status != domain.StepPending || updated.Steps[0].Status != domain.StepSucceeded {
		t.Fatalf("unexpected retry state: %+v", updated)
	}
}

func TestCancelProvisioningTaskSkipsRemainingSteps(t *testing.T) {
	svc := newTestService(t)
	task, err := svc.CreateProvisioningTask(domain.ClusterNode{ID: "node-cancel", Role: "control-plane", K3sRole: domain.K3sRoleControlPlane, ProvisionMode: domain.ProvisionModeOnline}, "operator")
	if err != nil {
		t.Fatalf("CreateProvisioningTask: %v", err)
	}
	task.Status = domain.ProvisionRunning
	task.Steps[0].Status = domain.StepRunning
	if err := svc.SaveProvisioningTaskStatus(task); err != nil {
		t.Fatalf("SaveProvisioningTaskStatus: %v", err)
	}
	if err := svc.CancelProvisioningTask(task.ID, "operator"); err != nil {
		t.Fatalf("CancelProvisioningTask: %v", err)
	}
	updated := findProvisioningTask(t, svc.Snapshot(), task.ID)
	if updated.Status != domain.ProvisionCancelled {
		t.Fatalf("expected cancelled, got %s", updated.Status)
	}
	for _, step := range updated.Steps {
		if step.Status == domain.StepRunning || step.Status == domain.StepPending {
			t.Fatalf("expected no pending/running step after cancel, got %+v", updated.Steps)
		}
	}
}

func TestProvisioningEvidenceIncludedInComplianceReport(t *testing.T) {
	svc := newTestService(t)
	snapshot := svc.Snapshot()
	snapshot.ClusterNodes = append(snapshot.ClusterNodes, domain.ClusterNode{ID: "node-evidence", Name: "evidence-node"})
	snapshot.ProvisioningTasks = append(snapshot.ProvisioningTasks, domain.ProvisioningTask{
		ID:        "task-evidence",
		NodeID:    "node-evidence",
		Actor:     "operator",
		Role:      domain.K3sRoleControlPlane,
		EnableSGX: true,
		Status:    domain.ProvisionSucceeded,
		Steps: []domain.ProvisioningStep{
			{Name: "k3s_server_install", Status: domain.StepSucceeded, Evidence: "k3s version v1.30.2+k3s1 token=secret-token"},
			{Name: "runtime_verify", Status: domain.StepSucceeded, Evidence: "containerd 1.7.0"},
			{Name: "sgx_dcap_install", Status: domain.StepSucceeded, Evidence: "dcap-verify-quote 1.20 password:abc"},
			{Name: "final_verify", Status: domain.StepSucceeded, Evidence: "evidence-node Ready"},
		},
	})
	if err := svc.store.Replace(snapshot); err != nil {
		t.Fatalf("Replace: %v", err)
	}
	report, err := svc.RunCompliance("auditor")
	if err != nil {
		t.Fatalf("RunCompliance: %v", err)
	}
	messages := make([]string, 0, len(report.Findings))
	for _, finding := range report.Findings {
		messages = append(messages, finding.Message)
	}
	joined := strings.Join(messages, "\n")
	if !strings.Contains(joined, "K3s/runtime 已完成真实验证") || !strings.Contains(joined, "SGX/DCAP 已完成真实验证") {
		t.Fatalf("expected provisioning evidence findings, got %s", joined)
	}
	if strings.Contains(joined, "secret-token") || strings.Contains(joined, "password:abc") {
		t.Fatalf("compliance findings leak sensitive evidence: %s", joined)
	}
}

func TestSaveProvisioningTaskStatusWritesStepAuditEvents(t *testing.T) {
	svc := newTestService(t)
	task, err := svc.CreateProvisioningTask(domain.ClusterNode{ID: "node-audit", Role: "worker", K3sRole: domain.K3sRoleWorker, ProvisionMode: domain.ProvisionModeOnline}, "operator")
	if err != nil {
		t.Fatalf("CreateProvisioningTask: %v", err)
	}
	task.Status = domain.ProvisionRunning
	task.Steps[0].Status = domain.StepSucceeded
	task.Steps[0].Message = "preflight ok"
	task.Steps[0].Evidence = "token=secret-token password:abc"
	if err := svc.SaveProvisioningTaskStatus(task); err != nil {
		t.Fatalf("SaveProvisioningTaskStatus: %v", err)
	}
	var stepAuditFound bool
	for _, event := range svc.Snapshot().AuditEvents {
		if event.Action == "provisioning-step-succeeded" && event.Target == task.ID+":preflight" {
			stepAuditFound = true
			if strings.Contains(event.Result, "secret-token") || strings.Contains(event.Result, "password:abc") {
				t.Fatalf("audit event leaks sensitive data: %+v", event)
			}
		}
	}
	if !stepAuditFound {
		t.Fatal("expected provisioning step audit event")
	}
}

func hasProvisioningStep(task domain.ProvisioningTask, name string) bool {
	for _, step := range task.Steps {
		if step.Name == name {
			return true
		}
	}
	return false
}

func findProvisioningTask(t *testing.T, snapshot store.Snapshot, id string) domain.ProvisioningTask {
	t.Helper()
	for _, task := range snapshot.ProvisioningTasks {
		if task.ID == id {
			return task
		}
	}
	t.Fatalf("task %s not found", id)
	return domain.ProvisioningTask{}
}

func findProvisioningTaskByNode(t *testing.T, snapshot store.Snapshot, nodeID string) domain.ProvisioningTask {
	t.Helper()
	for _, task := range snapshot.ProvisioningTasks {
		if task.NodeID == nodeID {
			return task
		}
	}
	t.Fatalf("task for node %s not found", nodeID)
	return domain.ProvisioningTask{}
}
