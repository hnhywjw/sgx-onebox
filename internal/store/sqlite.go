package store

import (
	"database/sql"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/monkeycode-ai/sgx-onebox-platform/internal/domain"
	_ "modernc.org/sqlite"
)

type SQLiteStore struct {
	mu       sync.RWMutex
	db       *sql.DB
	snapshot Snapshot
}

const currentSchemaVersion = 7

var testStoreCleanupOnce sync.Once

func NewSQLiteStore() (*SQLiteStore, error) {
	dataPath := projectDataPath()
	dbPath := projectDBPath()
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, err
	}
	if os.Getenv("GO_TEST_MODE") != "" {
		testStoreCleanupOnce.Do(func() {
			_ = os.Remove(dataPath)
			_ = os.Remove(dbPath)
		})
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	db.Exec("PRAGMA journal_mode=WAL")
	db.Exec("PRAGMA busy_timeout=5000")
	store := &SQLiteStore{db: db}
	if err := store.initSchema(); err != nil {
		db.Close()
		return nil, err
	}
	if err := store.load(); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func ForceRemigrate() error {
	dbPath := projectDBPath()
	_ = os.Remove(dbPath)
	st, err := NewSQLiteStore()
	if err != nil {
		return err
	}
	return st.Close()
}

func (s *SQLiteStore) initSchema() error {
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS schema_version (version INTEGER PRIMARY KEY)`); err != nil {
		return err
	}
	var existingVersion int
	if err := s.db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_version`).Scan(&existingVersion); err != nil {
		existingVersion = 0
	}
	if existingVersion < currentSchemaVersion {
		tx, txErr := s.db.Begin()
		if txErr != nil {
			return txErr
		}
		if err := s.migrateToTx(tx, existingVersion, currentSchemaVersion); err != nil {
			tx.Rollback()
			return err
		}
		tx.Exec(`DELETE FROM schema_version`)
		tx.Exec(`INSERT INTO schema_version(version) VALUES (?)`, currentSchemaVersion)
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLiteStore) migrateToTx(tx *sql.Tx, from, to int) error {
	for v := from; v < to; v++ {
		switch v + 1 {
		case 1:
			fallthrough
		case 2:
			fallthrough
		case 3:
			fallthrough
		case 4:
			fallthrough
		case 5:
			fallthrough
		case 6:
			fallthrough
		case 7:
			if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS snapshots (id TEXT PRIMARY KEY, payload TEXT NOT NULL, updated_at TEXT NOT NULL)`); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *SQLiteStore) load() error {
	var payload string
	if err := s.db.QueryRow(`SELECT payload FROM snapshots WHERE id = ?`, "current").Scan(&payload); err == nil {
		var snap Snapshot
		if unmarshalErr := json.Unmarshal([]byte(payload), &snap); unmarshalErr != nil {
			log.Printf("[store] ERROR: failed to unmarshal sqlite snapshot, resetting to seed: %v", unmarshalErr)
			s.snapshot = seed()
			return s.persistLocked()
		}
		s.snapshot = hydrateSnapshot(snap)
		s.normalize()
		return nil
	} else if err != sql.ErrNoRows {
		return err
	}

	dataPath := projectDataPath()
	raw, err := os.ReadFile(dataPath)
	if err != nil {
		if os.IsNotExist(err) {
			s.snapshot = seed()
			return s.persistLocked()
		}
		return err
	}
	var snap Snapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		log.Printf("[store] ERROR: failed to unmarshal platform data, resetting to seed: %v", err)
		s.snapshot = seed()
		return s.persistLocked()
	}
	s.snapshot = hydrateSnapshot(snap)
	s.normalize()
	return s.persistLocked()
}

func hydrateSnapshot(snap Snapshot) Snapshot {
	if snap.DataVersion == 0 {
		snap.DataVersion = currentSchemaVersion
	}
	if snap.LoginFailures == nil {
		snap.LoginFailures = map[string]int{}
	}
	if snap.LoginLockedUntil == nil {
		snap.LoginLockedUntil = map[string]string{}
	}
	if snap.ProvisioningTasks == nil {
		snap.ProvisioningTasks = []domain.ProvisioningTask{}
	}
	return snap
}

func (s *SQLiteStore) normalize() {
	for i, node := range s.snapshot.ClusterNodes {
		if node.NicName == "" {
			s.snapshot.ClusterNodes[i].NicName = "eth0"
		}
		if node.ProvisionMode == "" {
			s.snapshot.ClusterNodes[i].ProvisionMode = domain.ProvisionModeOnline
		}
		if node.K3sRole == "" {
			s.snapshot.ClusterNodes[i].K3sRole = domain.NodeK3sRole(node.Role)
		}
		if node.SGXStatus == "" {
			s.snapshot.ClusterNodes[i].SGXStatus = domain.SGXUnknown
		}
		if node.RuntimeStatus == "" {
			s.snapshot.ClusterNodes[i].RuntimeStatus = domain.RuntimeUnknown
		}
		if node.ProvisionStatus == "" {
			s.snapshot.ClusterNodes[i].ProvisionStatus = string(domain.ProvisionPending)
		}
		if node.CPUUsage < 0 {
			s.snapshot.ClusterNodes[i].CPUUsage = 0
		}
		if node.MemoryUsage < 0 {
			s.snapshot.ClusterNodes[i].MemoryUsage = 0
		}
		if node.DiskUsage < 0 {
			s.snapshot.ClusterNodes[i].DiskUsage = 0
		}
		if node.RxBytes < 0 {
			s.snapshot.ClusterNodes[i].RxBytes = 0
		}
		if node.TxBytes < 0 {
			s.snapshot.ClusterNodes[i].TxBytes = 0
		}
	}
}

func cloneSnapshot(snap Snapshot) Snapshot {
	raw, err := json.Marshal(snap)
	if err != nil {
		return newEmptySnapshot()
	}
	var out Snapshot
	if err := json.Unmarshal(raw, &out); err != nil {
		return newEmptySnapshot()
	}
	if out.LoginFailures == nil {
		out.LoginFailures = map[string]int{}
	}
	if out.LoginLockedUntil == nil {
		out.LoginLockedUntil = map[string]string{}
	}
	if out.ProvisioningTasks == nil {
		out.ProvisioningTasks = []domain.ProvisioningTask{}
	}
	if out.Users == nil {
		out.Users = []domain.User{}
	}
	if out.ClusterNodes == nil {
		out.ClusterNodes = []domain.ClusterNode{}
	}
	return out
}

func newEmptySnapshot() Snapshot {
	return Snapshot{
		LoginFailures:    map[string]int{},
		LoginLockedUntil: map[string]string{},
		ProvisioningTasks: []domain.ProvisioningTask{},
		Users:             []domain.User{},
		ClusterNodes:      []domain.ClusterNode{},
	}
}
func (s *SQLiteStore) persistLocked() error {
	if s.snapshot.LoginFailures == nil {
		s.snapshot.LoginFailures = map[string]int{}
	}
	if s.snapshot.LoginLockedUntil == nil {
		s.snapshot.LoginLockedUntil = map[string]string{}
	}
	if s.snapshot.ProvisioningTasks == nil {
		s.snapshot.ProvisioningTasks = []domain.ProvisioningTask{}
	}
	s.snapshot.DataVersion = currentSchemaVersion
	raw, err := json.MarshalIndent(s.snapshot, "", "  ")
	if err != nil {
		return err
	}
	if _, err := s.db.Exec(`INSERT INTO snapshots(id, payload, updated_at) VALUES (?, ?, ?) ON CONFLICT(id) DO UPDATE SET payload = excluded.payload, updated_at = excluded.updated_at`, "current", string(raw), time.Now().UTC().Format(time.RFC3339)); err != nil {
		return err
	}
	return nil
}

func (s *SQLiteStore) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneSnapshot(s.snapshot)
}

func (s *SQLiteStore) Replace(snapshot Snapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.persistOnly(snapshot); err != nil {
		return err
	}
	s.snapshot = cloneSnapshot(snapshot)
	s.normalize()
	return nil
}

func (s *SQLiteStore) persistOnly(snapshot Snapshot) error {
	if snapshot.LoginFailures == nil {
		snapshot.LoginFailures = map[string]int{}
	}
	if snapshot.LoginLockedUntil == nil {
		snapshot.LoginLockedUntil = map[string]string{}
	}
	if snapshot.ProvisioningTasks == nil {
		snapshot.ProvisioningTasks = []domain.ProvisioningTask{}
	}
	snapshot.DataVersion = currentSchemaVersion
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO snapshots(id, payload, updated_at) VALUES (?, ?, ?) ON CONFLICT(id) DO UPDATE SET payload = excluded.payload, updated_at = excluded.updated_at`, "current", string(raw), time.Now().UTC().Format(time.RFC3339))
	return err
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

func (s *SQLiteStore) Ping() error {
	return s.db.Ping()
}

func (s *SQLiteStore) Checkpoint() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil
	}
	_, err := s.db.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	return err
}

func (s *SQLiteStore) CleanupAudit() {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := time.Now().UTC().AddDate(-1, 0, 0)
	kept := s.snapshot.AuditEvents[:0]
	for _, event := range s.snapshot.AuditEvents {
		createdAt, parseErr := time.Parse(time.RFC3339, event.CreatedAt)
		if parseErr != nil || createdAt.After(cutoff) {
			kept = append(kept, event)
		}
	}
	s.snapshot.AuditEvents = kept
	_ = s.persistLocked()
}
