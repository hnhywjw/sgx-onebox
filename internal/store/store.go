package store

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/monkeycode-ai/sgx-onebox-platform/internal/domain"
)

type Snapshot struct {
	DataVersion        int                           `json:"dataVersion"`
	Images             []domain.ImageAsset           `json:"images"`
	Components         []domain.ComponentDefinition  `json:"components"`
	Enclaves           []domain.EnclaveProfile       `json:"enclaves"`
	Networks           []domain.NetworkAttachment    `json:"networks"`
	Attestations       []domain.AttestationRecord    `json:"attestations"`
	Reports            []domain.ComplianceReport     `json:"reports"`
	Users              []domain.User                 `json:"users"`
	ManifestHints      []string                      `json:"manifestHints"`
	ClusterNodes       []domain.ClusterNode          `json:"clusterNodes"`
	ClusterQuotas      []domain.ClusterQuota         `json:"clusterQuotas"`
	ClusterAlerts      []domain.ClusterAlert         `json:"clusterAlerts"`
	ClusterLogs        []domain.ClusterLog           `json:"clusterLogs"`
	ProvisioningTasks  []domain.ProvisioningTask     `json:"provisioningTasks"`
	ClusterUpgrade     domain.ClusterUpgradeStatus   `json:"clusterUpgrade"`
	AlertThreshold     domain.AlertThresholdConfig   `json:"alertThreshold"`
	InstallPackages    []domain.InstallPackage       `json:"installPackages"`
	MarketplaceApps    []domain.MarketplaceApp       `json:"marketplaceApps"`
	CatalogItems       []domain.ComponentCatalogItem `json:"catalogItems"`
	IsolationPolicies  []domain.IsolationPolicy      `json:"isolationPolicies"`
	EnclaveResources   []domain.EnclaveResource      `json:"enclaveResources"`
	EnclaveKeys        []domain.EnclaveKeyMaterial   `json:"enclaveKeys"`
	EnclaveInspections []domain.EnclaveInspection    `json:"enclaveInspections"`
	SecurityPolicies   []domain.SecurityPolicyRule   `json:"securityPolicies"`
	ComplianceTasks    []domain.ComplianceTask       `json:"complianceTasks"`
	SystemSettings     []domain.SystemSetting        `json:"systemSettings"`
	AuditEvents        []domain.AuditEvent           `json:"auditEvents"`
	LoginFailures      map[string]int                `json:"loginFailures"`
	LoginLockedUntil   map[string]string             `json:"loginLockedUntil"`
	Sessions           []domain.Session              `json:"sessions"`
	TopoLinks          []domain.TopologyLink         `json:"topoLinks"`
	TopoEgress         []domain.TopologyNode         `json:"topoEgress"`
	Plugins            []domain.PluginDefinition     `json:"plugins"`
}

type Store interface {
	Snapshot() Snapshot
	Replace(snapshot Snapshot) error
	Close() error
	Ping() error
	Checkpoint() error
	CleanupAudit()
}

func projectDataPath() string {
	if p := os.Getenv("PLATFORM_JSON_PATH"); p != "" {
		return p
	}
	if os.Getenv("GO_TEST_MODE") != "" {
		return filepath.Join("/tmp", fmt.Sprintf("platform-data-test-%d.json", os.Getpid()))
	}
	cwd, err := os.Getwd()
	if err != nil {
		return filepath.Join("data", "platform-data.json")
	}
	current := cwd
	for {
		if _, statErr := os.Stat(filepath.Join(current, "go.mod")); statErr == nil {
			return filepath.Join(current, "data", "platform-data.json")
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return filepath.Join(cwd, "data", "platform-data.json")
}

func projectDBPath() string {
	if os.Getenv("GO_TEST_MODE") != "" {
		return filepath.Join("/tmp", fmt.Sprintf("platform-data-test-%d.db", os.Getpid()))
	}
	cwd, err := os.Getwd()
	if err != nil {
		return filepath.Join("data", "platform.db")
	}
	current := cwd
	for {
		if _, statErr := os.Stat(filepath.Join(current, "go.mod")); statErr == nil {
			return filepath.Join(current, "data", "platform.db")
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return filepath.Join(cwd, "data", "platform.db")
}
