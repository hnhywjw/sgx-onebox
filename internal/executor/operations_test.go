package executor

import (
	"testing"

	"github.com/monkeycode-ai/sgx-onebox-platform/internal/domain"
)

func TestFindSSHNodesMatchesK3sRoleAndSkipsMissingCredentials(t *testing.T) {
	store := &fakeProvisioningStore{nodes: []domain.ClusterNode{
		{ID: "cp-no-secret", Role: "control-plane", K3sRole: domain.K3sRoleControlPlane, SSHHost: "10.0.0.10", SSHUsername: "root"},
		{ID: "cp-ready", Role: "", K3sRole: domain.K3sRoleControlPlane, SSHHost: "10.0.0.11", SSHUsername: "root", SSHPasswordCiphertext: "configured"},
		{ID: "worker-ready", Role: "worker", K3sRole: domain.K3sRoleWorker, SSHHost: "10.0.0.21", SSHUsername: "root", SSHPasswordCiphertext: "configured"},
	}}
	exec := New(Config{Store: store})
	nodes := exec.findSSHNodes("control-plane")
	if len(nodes) != 1 || nodes[0].ID != "cp-ready" {
		t.Fatalf("expected one control-plane node with credentials, got %+v", nodes)
	}
}

func TestCommandValidatorsRejectUnsafeInput(t *testing.T) {
	if isValidKubernetesName("default;touch-x") {
		t.Fatal("expected unsafe Kubernetes name to be rejected")
	}
	if isValidK3sVersion("v1.30.2+k3s1;curl-x") {
		t.Fatal("expected unsafe K3s version to be rejected")
	}
	if isSafeImageRef("registry.local/app:1.0;touch-x") {
		t.Fatal("expected unsafe image reference to be rejected")
	}
	if isValidMeasurement("abc;touch-x") {
		t.Fatal("expected unsafe measurement to be rejected")
	}
	if isValidLinuxInterface("eth0;touch-x") {
		t.Fatal("expected unsafe interface name to be rejected")
	}
}

func TestCommandValidatorsAcceptExpectedInput(t *testing.T) {
	if !isValidKubernetesName("default") {
		t.Fatal("expected namespace to be accepted")
	}
	if !isValidK3sVersion("v1.30.2+k3s1") {
		t.Fatal("expected K3s version to be accepted")
	}
	if !isSafeImageRef("registry.local/security/waf:2.3.1") {
		t.Fatal("expected image reference to be accepted")
	}
	if !isValidMeasurement("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef") {
		t.Fatal("expected measurement to be accepted")
	}
	if !isValidLinuxInterface("eno1.100") {
		t.Fatal("expected interface name to be accepted")
	}
}
