package executor

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monkeycode-ai/sgx-onebox-platform/internal/domain"
)

type fakeProvisioningStore struct {
	mu       sync.Mutex
	nodes    []domain.ClusterNode
	packages []domain.InstallPackage
	tasks    []domain.ProvisioningTask
}

func (s *fakeProvisioningStore) ListClusterNodes() []domain.ClusterNode {
	s.mu.Lock()
	defer s.mu.Unlock()
	nodes := make([]domain.ClusterNode, len(s.nodes))
	copy(nodes, s.nodes)
	return nodes
}

func (s *fakeProvisioningStore) SaveClusterNodes(nodes []domain.ClusterNode) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nodes = append([]domain.ClusterNode{}, nodes...)
	return nil
}

func (s *fakeProvisioningStore) SaveClusterNodeStatus(node domain.ClusterNode) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for index := range s.nodes {
		if s.nodes[index].ID == node.ID {
			s.nodes[index] = node
			return nil
		}
	}
	return errors.New("node not found")
}

func (s *fakeProvisioningStore) ListInstallPackages() []domain.InstallPackage {
	s.mu.Lock()
	defer s.mu.Unlock()
	packages := make([]domain.InstallPackage, len(s.packages))
	copy(packages, s.packages)
	return packages
}

func (s *fakeProvisioningStore) ListProvisioningTasks() []domain.ProvisioningTask {
	s.mu.Lock()
	defer s.mu.Unlock()
	tasks := make([]domain.ProvisioningTask, len(s.tasks))
	copy(tasks, s.tasks)
	return tasks
}

func (s *fakeProvisioningStore) SaveProvisioningTaskStatus(task domain.ProvisioningTask) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for index := range s.tasks {
		if s.tasks[index].ID == task.ID {
			s.tasks[index] = task
			return nil
		}
	}
	return errors.New("task not found")
}

type fakeProvisioningRunner struct {
	outputs map[string]string
	failOn  string
	calls   []string
}

func (r *fakeProvisioningRunner) Run(_ domain.ClusterNode, command string, _ time.Duration) (string, error) {
	r.calls = append(r.calls, command)
	for key, output := range r.outputs {
		if strings.Contains(command, key) {
			if r.failOn != "" && strings.Contains(command, r.failOn) {
				return output, errors.New("remote command failed")
			}
			return output, nil
		}
	}
	if r.failOn != "" && strings.Contains(command, r.failOn) {
		return "failed output", errors.New("remote command failed")
	}
	return "ok", nil
}

func (r *fakeProvisioningRunner) RunOfflineK3s(_ domain.ClusterNode, _ domain.InstallPackage, role domain.NodeK3sRole, _ string, _ string, _ time.Duration) (string, error) {
	command := "offline-" + string(role)
	r.calls = append(r.calls, command)
	if r.failOn != "" && strings.Contains(command, r.failOn) {
		return "offline failed", errors.New("remote command failed")
	}
	return "k3s version v1.30.2+k3s1", nil
}

func TestProvisioningWorkerCompletesControlPlaneTask(t *testing.T) {
	store := &fakeProvisioningStore{
		nodes: []domain.ClusterNode{{ID: "node-provision-ok", Name: "provision-ok", Role: "control-plane", K3sRole: domain.K3sRoleControlPlane, ProvisionTaskID: "task-ok", SSHHost: "10.0.0.10", SSHPort: 22, SSHUsername: "root", SSHPasswordCiphertext: "configured", RuntimeStatus: domain.RuntimePending}},
		tasks: []domain.ProvisioningTask{{ID: "task-ok", NodeID: "node-provision-ok", Role: domain.K3sRoleControlPlane, Mode: domain.ProvisionModeOnline, Status: domain.ProvisionPending, CurrentStep: "preflight", Steps: []domain.ProvisioningStep{{Name: "preflight", Status: domain.StepPending}, {Name: "k3s_server_install", Status: domain.StepPending}, {Name: "runtime_verify", Status: domain.StepPending}, {Name: "final_verify", Status: domain.StepPending}}}},
	}
	runner := &fakeProvisioningRunner{outputs: map[string]string{"k3s kubectl get nodes": "k3s version v1.30.2+k3s1", "kubectl get nodes": "node Ready"}}
	exec := New(Config{Store: store, ProvisioningConcurrency: 3})
	exec.provisioningRunner = runner
	exec.pollProvisioningTasks()

	task := store.tasks[0]
	if task.Status != domain.ProvisionSucceeded {
		t.Fatalf("expected task succeeded, got %+v", task)
	}
	for _, step := range task.Steps {
		if step.Status != domain.StepSucceeded {
			t.Fatalf("expected all steps succeeded, got %+v", task.Steps)
		}
	}
	node := store.nodes[0]
	if node.Status != "ready" || node.ProvisionStatus != string(domain.ProvisionSucceeded) || node.RuntimeStatus != domain.RuntimeReady {
		t.Fatalf("unexpected node state: %+v", node)
	}
	if node.Version != "v1.30.2+k3s1" {
		t.Fatalf("expected version extracted, got %s", node.Version)
	}
}

func TestProvisioningWorkerSGXFailurePreservesK3sReady(t *testing.T) {
	store := &fakeProvisioningStore{
		nodes: []domain.ClusterNode{{ID: "node-sgx-fail", Name: "sgx-fail", Role: "worker", K3sRole: domain.K3sRoleWorker, ProvisionTaskID: "task-sgx", SSHHost: "10.0.0.20", SSHPort: 22, SSHUsername: "root", SSHPasswordCiphertext: "configured", ControlPlaneEndpoint: "10.0.0.10:6443", RuntimeStatus: domain.RuntimePending, SGXStatus: domain.SGXPending}},
		tasks: []domain.ProvisioningTask{{ID: "task-sgx", NodeID: "node-sgx-fail", Role: domain.K3sRoleWorker, Mode: domain.ProvisionModeOnline, EnableSGX: true, Status: domain.ProvisionPending, CurrentStep: "preflight", Steps: []domain.ProvisioningStep{{Name: "preflight", Status: domain.StepPending}, {Name: "k3s_agent_install", Status: domain.StepPending}, {Name: "runtime_verify", Status: domain.StepPending}, {Name: "sgx_dcap_install", Status: domain.StepPending}, {Name: "final_verify", Status: domain.StepPending}}}},
	}
	t.Setenv("K3S_TOKEN", "secret-token")
	runner := &fakeProvisioningRunner{failOn: "/dev/sgx_enclave", outputs: map[string]string{"K3S_URL": "joined K3S_TOKEN=secret-token", "crictl": "runtime ok", "/dev/sgx_enclave": "password:abc token=secret-token"}}
	exec := New(Config{Store: store, ProvisioningConcurrency: 3})
	exec.provisioningRunner = runner
	exec.pollProvisioningTasks()

	task := store.tasks[0]
	if task.Status != domain.ProvisionFailed || task.CurrentStep != "sgx_dcap_install" {
		t.Fatalf("expected task failed at sgx step, got %+v", task)
	}
	if !strings.Contains(task.Steps[3].Evidence, "password:***") || !strings.Contains(task.Steps[3].Evidence, "token=***") {
		t.Fatalf("expected redacted evidence, got %q", task.Steps[3].Evidence)
	}
	if strings.Contains(task.Steps[3].Evidence, "secret-token") || strings.Contains(task.Steps[3].Evidence, "abc") {
		t.Fatalf("evidence leaks sensitive value: %q", task.Steps[3].Evidence)
	}
	node := store.nodes[0]
	if node.Status != "ready" || node.RuntimeStatus != domain.RuntimeReady || node.SGXStatus != domain.SGXPending {
		t.Fatalf("expected K3s ready preserved and SGX pending, got %+v", node)
	}
}

func TestProvisioningWorkerOfflineTaskFailsWithoutVerifiedBundle(t *testing.T) {
	store := &fakeProvisioningStore{
		nodes: []domain.ClusterNode{{ID: "node-offline", Name: "offline", Role: "control-plane", K3sRole: domain.K3sRoleControlPlane, ProvisionTaskID: "task-offline", SSHHost: "10.0.0.30", SSHPort: 22, SSHUsername: "root", SSHPasswordCiphertext: "configured"}},
		tasks: []domain.ProvisioningTask{{ID: "task-offline", NodeID: "node-offline", Role: domain.K3sRoleControlPlane, Mode: domain.ProvisionModeOffline, OfflineBundleID: "pkg-missing", Status: domain.ProvisionPending, Steps: []domain.ProvisioningStep{{Name: "preflight", Status: domain.StepPending}, {Name: "k3s_server_install", Status: domain.StepPending}}}},
	}
	exec := New(Config{Store: store, ProvisioningConcurrency: 3})
	exec.provisioningRunner = &fakeProvisioningRunner{}
	exec.pollProvisioningTasks()

	task := store.tasks[0]
	if task.Status != domain.ProvisionFailed || task.CurrentStep != "k3s_server_install" {
		t.Fatalf("expected offline task to fail at k3s install, got %+v", task)
	}
	if task.Steps[1].Status != domain.StepFailed || !strings.Contains(task.Message, "离线") {
		t.Fatalf("expected offline failure message, got %+v", task)
	}
}

func TestProvisioningWorkerCompletesOfflineControlPlaneTask(t *testing.T) {
	store := &fakeProvisioningStore{
		nodes:    []domain.ClusterNode{{ID: "node-offline-ok", Name: "offline-ok", Role: "control-plane", K3sRole: domain.K3sRoleControlPlane, ProvisionTaskID: "task-offline-ok", SSHHost: "10.0.0.30", SSHPort: 22, SSHUsername: "root", SSHPasswordCiphertext: "configured"}},
		packages: []domain.InstallPackage{{ID: "pkg-offline-ok", Mode: domain.InstallModeOffline, Offline: true, FilePath: "/tmp/offline.tgz"}},
		tasks:    []domain.ProvisioningTask{{ID: "task-offline-ok", NodeID: "node-offline-ok", Role: domain.K3sRoleControlPlane, Mode: domain.ProvisionModeOffline, OfflineBundleID: "pkg-offline-ok", Status: domain.ProvisionPending, Steps: []domain.ProvisioningStep{{Name: "preflight", Status: domain.StepPending}, {Name: "k3s_server_install", Status: domain.StepPending}, {Name: "runtime_verify", Status: domain.StepPending}, {Name: "final_verify", Status: domain.StepPending}}}},
	}
	runner := &fakeProvisioningRunner{outputs: map[string]string{"crictl": "runtime ok", "kubectl get nodes": "node Ready"}}
	exec := New(Config{Store: store, ProvisioningConcurrency: 3})
	exec.provisioningRunner = runner
	exec.pollProvisioningTasks()

	task := store.tasks[0]
	if task.Status != domain.ProvisionSucceeded {
		t.Fatalf("expected offline task succeeded, got %+v", task)
	}
	if !containsCall(runner.calls, "offline-control-plane") {
		t.Fatalf("expected offline install call, got %+v", runner.calls)
	}
}

func TestProvisioningWorkerDoesNotOverwriteCancelledTask(t *testing.T) {
	store := &fakeProvisioningStore{
		nodes: []domain.ClusterNode{{ID: "node-cancelled", Name: "cancelled", Role: "control-plane", K3sRole: domain.K3sRoleControlPlane, ProvisionTaskID: "task-cancelled", SSHHost: "10.0.0.40", SSHPort: 22, SSHUsername: "root", SSHPasswordCiphertext: "configured"}},
		tasks: []domain.ProvisioningTask{{ID: "task-cancelled", NodeID: "node-cancelled", Role: domain.K3sRoleControlPlane, Mode: domain.ProvisionModeOnline, Status: domain.ProvisionCancelled, Steps: []domain.ProvisioningStep{{Name: "preflight", Status: domain.StepSkipped}}}},
	}
	exec := New(Config{Store: store, ProvisioningConcurrency: 3})
	exec.provisioningRunner = &fakeProvisioningRunner{}
	exec.runProvisioningTask(domain.ProvisioningTask{ID: "task-cancelled", NodeID: "node-cancelled", Role: domain.K3sRoleControlPlane, Mode: domain.ProvisionModeOnline, Status: domain.ProvisionRunning, Steps: []domain.ProvisioningStep{{Name: "preflight", Status: domain.StepPending}}}, store.nodes[0], store.nodes)

	if store.tasks[0].Status != domain.ProvisionCancelled {
		t.Fatalf("expected cancelled task preserved, got %+v", store.tasks[0])
	}
	if store.nodes[0].ProvisionStatus == string(domain.ProvisionRunning) {
		t.Fatalf("expected cancelled node status not overwritten, got %+v", store.nodes[0])
	}
}

func TestControlPlaneEndpointSkipsSelfAndUsesExistingControlPlane(t *testing.T) {
	node := domain.ClusterNode{ID: "cp-2", Role: "control-plane", K3sRole: domain.K3sRoleControlPlane}
	nodes := []domain.ClusterNode{
		{ID: "cp-2", Role: "control-plane", K3sRole: domain.K3sRoleControlPlane, InternalIP: "10.0.0.12"},
		{ID: "cp-1", Role: "control-plane", K3sRole: domain.K3sRoleControlPlane, InternalIP: "10.0.0.11", Status: "ready"},
	}
	if got := controlPlaneEndpoint(node, nodes); got != "10.0.0.11:6443" {
		t.Fatalf("expected existing control plane endpoint, got %q", got)
	}
}

func TestK3sServerInstallCommandSupportsClusterInitAndServerJoin(t *testing.T) {
	initCommand := k3sServerInstallCommand("v1.30.2+k3s1", "", "", false)
	if !strings.Contains(initCommand, "server --cluster-init") {
		t.Fatalf("expected cluster init command, got %s", initCommand)
	}
	joinCommand := k3sServerInstallCommand("v1.30.2+k3s1", "10.0.0.11:6443", "secret-token", true)
	if !strings.Contains(joinCommand, "K3S_URL='https://10.0.0.11:6443'") || !strings.Contains(joinCommand, "sh -s - server") || strings.Contains(joinCommand, "--cluster-init") {
		t.Fatalf("expected server join command, got %s", joinCommand)
	}
}

func TestOfflineK3sInstallCommandSupportsControlPlaneJoin(t *testing.T) {
	command := offlineK3sInstallCommand("/tmp/bundle.tar.gz", domain.K3sRoleControlPlane, "10.0.0.11:6443", "secret-token")
	if !strings.Contains(command, "K3S_URL='https://10.0.0.11:6443'") || !strings.Contains(command, "INSTALL_K3S_EXEC=server") || strings.Contains(command, "--cluster-init") {
		t.Fatalf("expected offline control-plane join command, got %s", command)
	}
}

func containsCall(calls []string, needle string) bool {
	for _, call := range calls {
		if strings.Contains(call, needle) {
			return true
		}
	}
	return false
}
