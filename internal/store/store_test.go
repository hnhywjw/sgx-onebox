package store

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/monkeycode-ai/sgx-onebox-platform/internal/domain"
)

func init() {
	os.Setenv("GO_TEST_MODE", "1")
}

func TestNewSQLiteStore(t *testing.T) {
	st, err := NewSQLiteStore()
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer st.Close()

	snap := st.Snapshot()
	if len(snap.Users) == 0 {
		t.Fatal("expected seeded users")
	}
	if len(snap.Images) == 0 {
		t.Fatal("expected seeded images")
	}
	if len(snap.Components) == 0 {
		t.Fatal("expected seeded components")
	}
	if snap.LoginFailures == nil {
		t.Fatal("LoginFailures map should be initialized")
	}
	if snap.LoginLockedUntil == nil {
		t.Fatal("LoginLockedUntil map should be initialized")
	}
}

func TestSQLitePing(t *testing.T) {
	st, err := NewSQLiteStore()
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer st.Close()

	if err := st.Ping(); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

func TestSQLiteReplaceAndRead(t *testing.T) {
	st, err := NewSQLiteStore()
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer st.Close()

	snap := st.Snapshot()
	originalCount := len(snap.Users)

	snap.Users = snap.Users[:0]
	if err := st.Replace(snap); err != nil {
		t.Fatalf("Replace: %v", err)
	}

	newSnap := st.Snapshot()
	if len(newSnap.Users) != 0 {
		t.Fatalf("expected 0 users after clear, got %d", len(newSnap.Users))
	}

	original := seed()
	original.Users = original.Users[:originalCount]
	if err := st.Replace(original); err != nil {
		t.Fatalf("Replace restore: %v", err)
	}
}

func TestSQLiteLoginFailuresPersistence(t *testing.T) {
	st, err := NewSQLiteStore()
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer st.Close()

	snap := st.Snapshot()
	snap.LoginFailures = map[string]int{"testuser": 3}
	snap.LoginLockedUntil = map[string]string{"testuser": "2026-06-21T12:00:00Z"}
	if err := st.Replace(snap); err != nil {
		t.Fatalf("Replace: %v", err)
	}

	newSnap := st.Snapshot()
	if newSnap.LoginFailures["testuser"] != 3 {
		t.Fatalf("expected 3 login failures, got %d", newSnap.LoginFailures["testuser"])
	}
	if newSnap.LoginLockedUntil["testuser"] != "2026-06-21T12:00:00Z" {
		t.Fatalf("expected locked_until, got %s", newSnap.LoginLockedUntil["testuser"])
	}
}

func TestSQLiteClose(t *testing.T) {
	st, err := NewSQLiteStore()
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := st.Ping(); err == nil {
		t.Fatal("Ping should fail after Close")
	}
}

func TestSchemaVersionWritten(t *testing.T) {
	st, err := NewSQLiteStore()
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer st.Close()
	var version int
	if err := st.db.QueryRow("SELECT version FROM schema_version LIMIT 1").Scan(&version); err != nil {
		t.Fatalf("query schema_version: %v", err)
	}
	if version != currentSchemaVersion {
		t.Fatalf("expected schema version %d, got %d", currentSchemaVersion, version)
	}
}

func TestSQLiteSnapshotTablePersistence(t *testing.T) {
	st, err := NewSQLiteStore()
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer st.Close()

	snap := st.Snapshot()
	snap.ManifestHints = append(snap.ManifestHints, "sqlite-snapshot-test")
	if err := st.Replace(snap); err != nil {
		t.Fatalf("Replace: %v", err)
	}

	var payload string
	if err := st.db.QueryRow("SELECT payload FROM snapshots WHERE id = ?", "current").Scan(&payload); err != nil {
		t.Fatalf("query snapshots: %v", err)
	}
	if !strings.Contains(payload, "sqlite-snapshot-test") {
		t.Fatalf("expected snapshot payload to include marker")
	}
}

func TestSQLiteStoreDoesNotWriteJSONDataFile(t *testing.T) {
	st, err := NewSQLiteStore()
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer st.Close()

	snap := st.Snapshot()
	snap.ManifestHints = append(snap.ManifestHints, "sqlite-only-test")
	if err := st.Replace(snap); err != nil {
		t.Fatalf("Replace: %v", err)
	}
	if _, err := os.Stat(projectDataPath()); !os.IsNotExist(err) {
		t.Fatalf("expected no JSON data file, stat err=%v", err)
	}
}

func TestProvisioningTaskSnapshotPersistence(t *testing.T) {
	st, err := NewSQLiteStore()
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer st.Close()

	snap := st.Snapshot()
	snap.ProvisioningTasks = []domain.ProvisioningTask{
		{
			ID:              "prov-test-1",
			NodeID:          "node-test-1",
			Actor:           "admin",
			Mode:            domain.ProvisionModeOnline,
			Role:            domain.K3sRoleControlPlane,
			EnableSGX:       true,
			OfflineBundleID: "",
			Status:          domain.ProvisionPending,
			CurrentStep:     "preflight",
			Steps: []domain.ProvisioningStep{
				{Name: "preflight", Status: domain.StepPending, Message: "等待执行"},
			},
			CreatedAt: "2026-06-27T00:00:00Z",
			UpdatedAt: "2026-06-27T00:00:00Z",
		},
	}
	if err := st.Replace(snap); err != nil {
		t.Fatalf("Replace: %v", err)
	}

	loaded := st.Snapshot()
	if len(loaded.ProvisioningTasks) != 1 {
		t.Fatalf("expected 1 provisioning task, got %d", len(loaded.ProvisioningTasks))
	}
	task := loaded.ProvisioningTasks[0]
	if task.ID != "prov-test-1" || task.Steps[0].Name != "preflight" {
		t.Fatalf("unexpected provisioning task: %+v", task)
	}

	raw, err := json.Marshal(loaded)
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	if !strings.Contains(string(raw), "provisioningTasks") {
		t.Fatal("expected provisioningTasks in serialized snapshot")
	}
}
