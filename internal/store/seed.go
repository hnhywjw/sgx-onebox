package store

import (
	"errors"
	"time"

	"github.com/monkeycode-ai/sgx-onebox-platform/internal/domain"
	"github.com/monkeycode-ai/sgx-onebox-platform/internal/security"
)

func mustHash(pw string) string {
	h, err := security.HashPassword(pw)
	if err != nil {
		panic("seed password hashing failed: " + err.Error())
	}
	return h
}

func boolPtr(v bool) *bool { return &v }

var ErrNotFound = errors.New("not found")

var ErrConflict = errors.New("resource conflict")

func seed() Snapshot {
	now := time.Now().UTC().Format(time.RFC3339)
	return Snapshot{
		DataVersion: currentSchemaVersion,
		Images: []domain.ImageAsset{
			{ID: "img-waf", Name: "WAF 网关", Registry: "registry.local", Repository: "security/waf", Tag: "2.3.1", Digest: "sha256:waf231", Signed: true, SBOM: true, Vulnerability: "low", EnclaveReady: false, LastScanAt: now},
			{ID: "img-key", Name: "密钥托管代理", Registry: "registry.local", Repository: "security/key-keeper", Tag: "1.4.0", Digest: "sha256:key140", Signed: true, SBOM: true, Vulnerability: "low", EnclaveReady: true, LastScanAt: now},
		},
		Components: []domain.ComponentDefinition{
			{ID: "cmp-waf", Name: "WAF 网关", Image: "img-waf", Version: "2.3.1", Namespace: "northbound", Isolation: domain.IsolationStandard, Replicas: 2, Status: domain.ComponentDeployed, NetworkAttachments: []string{"net-outside"}},
			{ID: "cmp-key", Name: "密钥托管代理", Image: "img-key", Version: "1.4.0", Namespace: "core-secure", Isolation: domain.IsolationEnclave, Replicas: 1, Status: domain.ComponentDraft, NetworkAttachments: []string{"net-mgmt"}},
		},
		Enclaves: []domain.EnclaveProfile{
			{ID: "enc-key-keeper", ComponentID: "cmp-key", MREnclave: "0x72cdaf12be90", MRSigner: "0x5abf87239011", QuotePolicy: "strict-production", SecretsPolicy: "attestation-gated", SGXDevices: []string{"/dev/sgx_enclave", "/dev/sgx_provision"}, SgxEnabled: true},
		},
		Networks: []domain.NetworkAttachment{
			{ID: "net-outside", Name: "外联区", Bridge: "br-external", ParentNIC: "eno1.120", VLANID: 120, Subnet: "172.16.120.0/24", Gateway: "172.16.120.1", AttachedComponents: []string{"cmp-waf"}},
			{ID: "net-mgmt", Name: "管理区", Bridge: "br-mgmt", ParentNIC: "eno2.10", VLANID: 10, Subnet: "192.168.10.0/24", Gateway: "192.168.10.1", AttachedComponents: []string{"cmp-key"}},
		},
		Attestations: []domain.AttestationRecord{
			{ID: "att-cmp-key", ComponentID: "cmp-key", Measurement: "MRENCLAVE:cmp-key", Verifier: "dcap-verifier", Status: "pending", VerifiedAt: now, Standard: "等保2.0", ControlID: "SM-2.0-CA-01", ControlName: "可信计算环境核验", PolicyResult: "等待外部证明结果", Evidence: "初始化飞地配置已登记，等待真实 DCAP Quote 证据", SecretsReleased: false},
		},
		Reports: []domain.ComplianceReport{},
		Users: []domain.User{
			{ID: "usr-admin", Username: "admin", DisplayName: "平台管理员", Role: domain.RolePlatformAdmin, Status: domain.UserActive, PasswordHash: mustHash("admin123"), CreatedAt: now},
			{ID: "usr-security-admin", Username: "security-admin", DisplayName: "安全管理员", Role: domain.RoleSecurityAdmin, Status: domain.UserActive, PasswordHash: mustHash("secure123"), CreatedAt: now},
			{ID: "usr-auditor", Username: "auditor", DisplayName: "审计员", Role: domain.RoleAuditor, Status: domain.UserActive, PasswordHash: mustHash("audit123"), CreatedAt: now},
			{ID: "usr-operator", Username: "operator", DisplayName: "运维员", Role: domain.RoleOperator, Status: domain.UserActive, PasswordHash: mustHash("ops123"), CreatedAt: now},
		},
		ManifestHints: []string{
			"远程证明和合规报表按等保 2.0 控制项生成证据",
			"enclave 组件追加 SGX device plugin、证明 sidecar 与密钥注入策略",
			"bridge 网络通过 Multus attachment 指向 Linux bridge 与 VLAN 子接口",
			"cmp-key 组件处于 draft 状态是因为 TEE/SGX 飞地组件需要先完成远程证明和飞地配置绑定后才能部署",
		},
		ClusterNodes: []domain.ClusterNode{
			{ID: "node-master-1", Name: "master-1", Role: "control-plane", Status: "ready", Version: "v1.30.2+k3s1", InternalIP: "10.0.0.11", ManagementIP: "192.168.10.11", OS: "Ubuntu 22.04", Arch: "amd64", Kernel: "5.15.0-112-generic", ContainerRuntime: "containerd://1.7.15-k3s1", RuntimeClass: "system", ProvisionMode: domain.ProvisionModeOnline, K3sRole: domain.K3sRoleControlPlane, SGXStatus: domain.SGXUnknown, RuntimeStatus: domain.RuntimeUnknown, CapacityCPU: "8 vCPU", CapacityMemory: "16 GiB", DiskCapacity: "100 GB", Labels: []string{"node-role.kubernetes.io/control-plane=true", "topology.kubernetes.io/zone=zone-a", "sgx.capable=true"}, Taints: []string{"CriticalAddonsOnly=true:NoExecute"}, CPUUsage: 41, MemoryUsage: 58, DiskUsage: 62, PodCount: 23, LastHeartbeat: now, JoinedAt: now, JoinMode: "preprovisioned", JoinStatus: "active", JoinCommand: "curl -sfL https://get.k3s.io | sh -", LastJoinAttemptAt: now, LastJoinMessage: "节点已纳管", SSHHost: "192.168.10.11", SSHPort: 22, SSHUsername: "ubuntu", SSHPasswordConfigured: false, NicName: "eno1", RxBytes: 12000000000, TxBytes: 8000000000, RxRate: 15.5, TxRate: 12.3},
			{ID: "node-worker-1", Name: "worker-1", Role: "worker", Status: "ready", Version: "v1.30.2+k3s1", InternalIP: "10.0.0.21", ManagementIP: "192.168.10.21", OS: "Ubuntu 22.04", Arch: "amd64", Kernel: "5.15.0-112-generic", ContainerRuntime: "containerd://1.7.15-k3s1", RuntimeClass: "standard", ProvisionMode: domain.ProvisionModeOnline, K3sRole: domain.K3sRoleWorker, SGXStatus: domain.SGXUnknown, RuntimeStatus: domain.RuntimeUnknown, CapacityCPU: "16 vCPU", CapacityMemory: "32 GiB", DiskCapacity: "200 GB", Labels: []string{"node-role.kubernetes.io/worker=true", "topology.kubernetes.io/zone=zone-b"}, Taints: []string{}, CPUUsage: 36, MemoryUsage: 44, DiskUsage: 51, PodCount: 18, LastHeartbeat: now, JoinedAt: now, JoinMode: "preprovisioned", JoinStatus: "active", JoinCommand: "K3S_URL=https://master-1:6443 K3S_TOKEN=*** sh -", LastJoinAttemptAt: now, LastJoinMessage: "节点已纳管", SSHHost: "192.168.10.21", SSHPort: 22, SSHUsername: "ubuntu", SSHPasswordConfigured: false, NicName: "eno2", RxBytes: 6500000000, TxBytes: 4300000000, RxRate: 8.2, TxRate: 5.9},
		},
		ClusterQuotas: []domain.ClusterQuota{
			{ID: "quota-core", Scope: "core-secure", CPULimit: "8", MemoryLimit: "16Gi", PodLimit: 40, UpdatedAt: now},
			{ID: "quota-northbound", Scope: "northbound", CPULimit: "6", MemoryLimit: "12Gi", PodLimit: 30, UpdatedAt: now},
		},
		ClusterUpgrade: domain.ClusterUpgradeStatus{Status: "idle", CurrentVersion: "v1.30.2+k3s1", TargetVersion: "", Progress: 0, Message: "当前无升级任务"},
		AlertThreshold: domain.AlertThresholdConfig{CPU: 80, Mem: 80, Pod: 100},
		ClusterAlerts: []domain.ClusterAlert{
			{ID: "alert-node-heartbeat", Level: "warning", Source: "node-worker-1", Message: "节点心跳抖动已恢复，请持续关注负载峰值。", Status: "open", CreatedAt: now},
		},
		ClusterLogs: []domain.ClusterLog{
			{ID: "log-cluster-1", NodeID: "node-master-1", Category: "scheduler", Level: "info", Message: "已完成安全组件副本调度刷新。", RecordedAt: now},
			{ID: "log-cluster-2", NodeID: "node-worker-1", Category: "runtime", Level: "warning", Message: "发现瞬时 CPU 峰值，已触发资源巡检。", RecordedAt: now},
		},
		ProvisioningTasks: []domain.ProvisioningTask{},
		InstallPackages: []domain.InstallPackage{
			{ID: "pkg-iso-230", Name: "等保一体机标准安装 ISO", Mode: domain.InstallModeISO, Version: "2.3.0", Signed: true, Offline: false, ImportedAt: now},
			{ID: "pkg-offline-230", Name: "离线组件资源包", Mode: domain.InstallModeOffline, Version: "2.3.0", Signed: true, Offline: true, ImportedAt: now},
		},
		MarketplaceApps: []domain.MarketplaceApp{
			{ID: "app-waf-suite", Name: "WAF 网关套件", Category: "边界防护", CurrentVersion: "2.3.1", VersionHistory: []string{"2.1.0", "2.2.0", "2.3.1"}, PackageName: "waf-suite-2.3.1.tgz", Vendor: "MonkeyCode Security", Description: "用于南北向流量治理、访问控制与防护规则编排。", Dependencies: []string{"net-outside", "日志审计"}, CompatibleEnv: []string{"K3s", "x86_64", "SGX 可选"}, Status: domain.MarketplaceOnShelf, UpdatedAt: now},
			{ID: "app-key-keeper", Name: "密钥托管代理", Category: "密钥管理", CurrentVersion: "1.4.0", VersionHistory: []string{"1.2.0", "1.3.0", "1.4.0"}, PackageName: "key-keeper-1.4.0.sbom.tar.gz", Vendor: "MonkeyCode Security", Description: "提供远程证明联动的密钥托管与发放能力。", Dependencies: []string{"net-mgmt", "DCAP Quote Verify"}, CompatibleEnv: []string{"K3s", "SGX", "离线包"}, Status: domain.MarketplaceOnShelf, UpdatedAt: now},
		},
		CatalogItems: []domain.ComponentCatalogItem{
			{ID: "catalog-waf", Name: "WAF 网关", Category: "边界防护", Vendor: "MonkeyCode Security", Version: "2.3.1", TemplateVersion: "2026.06", IsolationRecommend: domain.IsolationStandard, LanguageSupport: []string{"Go", "Lua"}, Dependencies: []string{"net-outside"}, Description: "面向南北向流量治理的标准化安全组件模板。"},
			{ID: "catalog-key-keeper", Name: "密钥托管代理", Category: "密钥管理", Vendor: "MonkeyCode Security", Version: "1.4.0", TemplateVersion: "2026.06", IsolationRecommend: domain.IsolationEnclave, LanguageSupport: []string{"Go"}, Dependencies: []string{"net-mgmt"}, Description: "面向飞地环境的密钥托管与证明联动组件模板。"},
		},
		IsolationPolicies: []domain.IsolationPolicy{
			{Level: domain.IsolationStandard, RuntimeClass: "runc", ReadonlyRootFS: false, DropCapabilities: []string{"NET_RAW"}, RequireAttestation: false, RequireSignature: true, ConfidentialMaterial: false, RunAsNonRoot: false},
			{Level: domain.IsolationHardened, RuntimeClass: "gvisor", ReadonlyRootFS: true, DropCapabilities: []string{"ALL"}, RequireAttestation: false, RequireSignature: true, ConfidentialMaterial: true, RunAsNonRoot: true, AllowPrivilegeEscalation: boolPtr(false), SeccompProfile: "RuntimeDefault"},
			{Level: domain.IsolationEnclave, RuntimeClass: "rune", ReadonlyRootFS: true, DropCapabilities: []string{"ALL"}, RequireAttestation: true, RequireSignature: true, ConfidentialMaterial: true, RunAsNonRoot: true, AllowPrivilegeEscalation: boolPtr(false), SeccompProfile: "RuntimeDefault"},
		},
		EnclaveResources: []domain.EnclaveResource{
			{ID: "sgx-node-master-1", NodeID: "node-master-1", EPCSizeMB: 256, EPCUsedMB: 104, EnclaveCount: 1, Status: "healthy"},
			{ID: "sgx-node-worker-1", NodeID: "node-worker-1", EPCSizeMB: 256, EPCUsedMB: 0, EnclaveCount: 0, Status: "standby"},
		},
		EnclaveKeys: []domain.EnclaveKeyMaterial{
			{ID: "key-kms-1", Name: "核心密钥托管主密钥", ComponentID: "cmp-key", Algorithm: "AES-256-GCM", Status: "active", RotatedAt: now},
		},
		EnclaveInspections: []domain.EnclaveInspection{
			{ID: "inspection-initial", Target: "cmp-key", Status: "pending", Summary: "初始化飞地配置已登记，等待真实执行器巡检", CheckedAt: now},
		},
		SecurityPolicies: []domain.SecurityPolicyRule{
			{ID: "policy-runtime-default", Name: "运行时行为基线", Category: "runtime", Scope: "core-secure", Mode: "enforce", Status: "active", Targets: []string{"cmp-waf", "cmp-key"}, UpdatedAt: now},
			{ID: "policy-network-core", Name: "核心域访问控制", Category: "network", Scope: "net-mgmt", Mode: "monitor", Status: "active", Targets: []string{"net-mgmt"}, UpdatedAt: now},
		},
		ComplianceTasks: []domain.ComplianceTask{
			{ID: "task-sign-waf", ControlID: "SM-2.0-SC-01", ControlName: "软件完整性保护", Owner: "security-admin", Status: "tracking", DueAt: now, Note: "持续复核镜像签名链路和导入校验。"},
		},
		SystemSettings: []domain.SystemSetting{
			{ID: "setting-session-timeout", Category: "auth", Name: "会话超时时间", Value: "30m", UpdatedAt: now},
			{ID: "setting-audit-retention", Category: "audit", Name: "审计保留周期", Value: "180d", UpdatedAt: now},
		},
		AuditEvents: []domain.AuditEvent{
			{ID: "audit-bootstrap-1", Actor: "系统", Action: "平台初始化", Target: "platform", Result: "success", CreatedAt: now},
			{ID: "audit-bootstrap-2", Actor: "系统", Action: "安全策略加载", Target: "security-policies", Result: "success", CreatedAt: now},
		},
		TopoLinks: []domain.TopologyLink{
			{ID: "tl-cmp-waf-outside", Source: "cmp-waf", Target: "net-outside", Kind: "attached"},
			{ID: "tl-cmp-key-mgmt", Source: "cmp-key", Target: "net-mgmt", Kind: "attached"},
		},
		Plugins: []domain.PluginDefinition{
			{ID: "plugin-default-monitor", Name: "内置监控插件", Type: domain.PluginTypeMonitoring, Version: "1.0.0", Status: domain.PluginEnabled, Endpoint: "", Config: map[string]string{"interval": "30s"}, Description: "平台内置节点与组件运行状态采集插件", CreatedAt: now, UpdatedAt: now},
			{ID: "plugin-default-compliance", Name: "内置合规插件", Type: domain.PluginTypeCompliance, Version: "1.0.0", Status: domain.PluginEnabled, Endpoint: "", Config: map[string]string{"standard": "等保2.0"}, Description: "等保 2.0 控制项自动化检查插件", CreatedAt: now, UpdatedAt: now},
		},
		LoginFailures:    map[string]int{},
		LoginLockedUntil: map[string]string{},
	}
}
