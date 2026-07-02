package domain

type IsolationLevel string

type Role string

type UserStatus string

type ComponentStatus string

type MarketplaceAppStatus string

type InstallMode string

type ProvisioningStatus string

type ProvisioningStepStatus string

type NodeProvisionMode string

type NodeK3sRole string

type SGXStatus string

type RuntimeStatus string

const (
	IsolationStandard    IsolationLevel       = "standard"
	IsolationHardened    IsolationLevel       = "hardened"
	IsolationEnclave     IsolationLevel       = "enclave"
	RolePlatformAdmin    Role                 = "platform_admin"
	RoleSecurityAdmin    Role                 = "security_admin"
	RoleAuditor          Role                 = "auditor"
	RoleOperator         Role                 = "operator"
	UserActive           UserStatus           = "active"
	UserDisabled         UserStatus           = "disabled"
	ComponentDraft       ComponentStatus      = "draft"
	ComponentDeploying   ComponentStatus      = "deployment_pending"
	ComponentDeployed    ComponentStatus      = "deployed"
	ComponentDegraded    ComponentStatus      = "degraded"
	MarketplaceDraft     MarketplaceAppStatus = "draft"
	MarketplaceOnShelf   MarketplaceAppStatus = "published"
	MarketplaceOffShelf  MarketplaceAppStatus = "unpublished"
	InstallModeISO       InstallMode          = "iso"
	InstallModePackage   InstallMode          = "incremental"
	InstallModeOffline   InstallMode          = "offline_bundle"
	ProvisionModeOnline  NodeProvisionMode    = "online"
	ProvisionModeOffline NodeProvisionMode    = "offline"
	OfflineBundleBuiltin string               = "built-in"

	K3sRoleControlPlane NodeK3sRole            = "control-plane"
	K3sRoleWorker       NodeK3sRole            = "worker"
	ProvisionPending    ProvisioningStatus     = "pending"
	ProvisionRunning    ProvisioningStatus     = "running"
	ProvisionSucceeded  ProvisioningStatus     = "succeeded"
	ProvisionFailed     ProvisioningStatus     = "failed"
	ProvisionCancelled  ProvisioningStatus     = "cancelled"
	StepPending         ProvisioningStepStatus = "pending"
	StepRunning         ProvisioningStepStatus = "running"
	StepSucceeded       ProvisioningStepStatus = "succeeded"
	StepFailed          ProvisioningStepStatus = "failed"
	StepSkipped         ProvisioningStepStatus = "skipped"
	SGXUnknown          SGXStatus              = "unknown"
	SGXPending          SGXStatus              = "sgx_pending"
	SGXReady            SGXStatus              = "sgx_ready"
	SGXFailed           SGXStatus              = "sgx_failed"
	RuntimeUnknown      RuntimeStatus          = "unknown"
	RuntimePending      RuntimeStatus          = "runtime_pending"
	RuntimeReady        RuntimeStatus          = "runtime_ready"
	RuntimeFailed       RuntimeStatus          = "runtime_failed"
)

type ComponentCatalogItem struct {
	ID                 string         `json:"id"`
	Name               string         `json:"name"`
	Category           string         `json:"category"`
	Vendor             string         `json:"vendor"`
	Version            string         `json:"version"`
	TemplateVersion    string         `json:"templateVersion"`
	IsolationRecommend IsolationLevel `json:"isolationRecommend"`
	LanguageSupport    []string       `json:"languageSupport"`
	Dependencies       []string       `json:"dependencies"`
	Description        string         `json:"description"`
}

type ImageAsset struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Registry      string `json:"registry"`
	Repository    string `json:"repository"`
	Tag           string `json:"tag"`
	Digest        string `json:"digest"`
	Signed        bool   `json:"signed"`
	SBOM          bool   `json:"sbom"`
	Vulnerability string `json:"vulnerability"`
	EnclaveReady  bool   `json:"enclaveReady"`
	LastScanAt    string `json:"lastScanAt"`
}

type ImageBuildRequest struct {
	Name             string `json:"name"`
	Registry         string `json:"registry"`
	Repository       string `json:"repository"`
	Tag              string `json:"tag"`
	SourcePackage    string `json:"sourcePackage"`
	DockerfilePath   string `json:"dockerfilePath"`
	BuildArgs        string `json:"buildArgs"`
	EnableSignature  bool   `json:"enableSignature"`
	GenerateSBOM     bool   `json:"generateSbom"`
	EnableSGXRuntime bool   `json:"enableSgxRuntime"`
}

type ImageBuildResult struct {
	Image ImageAsset `json:"image"`
	Log   string     `json:"log"`
}

type ComponentDefinition struct {
	ID                 string          `json:"id"`
	Name               string          `json:"name"`
	Image              string          `json:"image"`
	Version            string          `json:"version"`
	Namespace          string          `json:"namespace"`
	Isolation          IsolationLevel  `json:"isolation"`
	Replicas           int             `json:"replicas"`
	Status             ComponentStatus `json:"status"`
	NetworkAttachments []string        `json:"networkAttachments"`
	PackagePath        string          `json:"packagePath"`
	PackageSize        int64           `json:"packageSize"`
	MtlsEnabled        bool            `json:"mtlsEnabled"`
}

type EnclaveProfile struct {
	ID            string   `json:"id"`
	ComponentID   string   `json:"componentId"`
	MREnclave     string   `json:"mrEnclave"`
	MRSigner      string   `json:"mrSigner"`
	QuotePolicy   string   `json:"quotePolicy"`
	SecretsPolicy string   `json:"secretsPolicy"`
	SGXDevices    []string `json:"sgxDevices"`
	SgxEnabled    bool     `json:"sgxEnabled"`
}

type IsolationPolicy struct {
	Level                    IsolationLevel `json:"level"`
	RuntimeClass             string         `json:"runtimeClass"`
	ReadonlyRootFS           bool           `json:"readonlyRootFs"`
	DropCapabilities         []string       `json:"dropCapabilities"`
	RequireAttestation       bool           `json:"requireAttestation"`
	RequireSignature         bool           `json:"requireSignature"`
	ConfidentialMaterial     bool           `json:"confidentialMaterial"`
	RunAsNonRoot             bool           `json:"runAsNonRoot"`
	AllowPrivilegeEscalation *bool          `json:"allowPrivilegeEscalation,omitempty"`
	SeccompProfile           string         `json:"seccompProfile,omitempty"`
	AppArmorProfile          string         `json:"appArmorProfile,omitempty"`
	SELinuxOptions           string         `json:"seLinuxOptions,omitempty"`
}

type NetworkAttachment struct {
	ID                 string   `json:"id"`
	Name               string   `json:"name"`
	Bridge             string   `json:"bridge"`
	ParentNIC          string   `json:"parentNic"`
	VLANID             int      `json:"vlanId"`
	Subnet             string   `json:"subnet"`
	Gateway            string   `json:"gateway"`
	AttachedComponents []string `json:"attachedComponents"`
}

type TopologyNode struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	Label      string `json:"label"`
	Zone       string `json:"zone"`
	RefID      string `json:"refId"`
	HostNodeID string `json:"hostNodeId,omitempty"`
	NicName    string `json:"nicName,omitempty"`
}

type TopologyLink struct {
	ID     string `json:"id"`
	Source string `json:"source"`
	Target string `json:"target"`
	Kind   string `json:"kind"`
}

type TopologyGraph struct {
	Nodes []TopologyNode `json:"nodes"`
	Links []TopologyLink `json:"links"`
}

type AttestationRecord struct {
	ID              string `json:"id"`
	ComponentID     string `json:"componentId"`
	Measurement     string `json:"measurement"`
	Verifier        string `json:"verifier"`
	Status          string `json:"status"`
	VerifiedAt      string `json:"verifiedAt"`
	Standard        string `json:"standard"`
	ControlID       string `json:"controlId"`
	ControlName     string `json:"controlName"`
	PolicyResult    string `json:"policyResult"`
	Evidence        string `json:"evidence"`
	SecretsReleased bool   `json:"secretsReleased"`
}

type AttestationResultPayload struct {
	Status          string `json:"status"`
	PolicyResult    string `json:"policyResult"`
	Evidence        string `json:"evidence"`
	SecretsReleased bool   `json:"secretsReleased"`
}

type ComplianceFinding struct {
	Category       string `json:"category"`
	Level          string `json:"level"`
	Message        string `json:"message"`
	ControlID      string `json:"controlId"`
	ControlName    string `json:"controlName"`
	Recommendation string `json:"recommendation"`
}

type ComplianceReport struct {
	ID          string              `json:"id"`
	Title       string              `json:"title"`
	Score       int                 `json:"score"`
	GeneratedAt string              `json:"generatedAt"`
	Status      string              `json:"status"`
	Standard    string              `json:"standard"`
	Findings    []ComplianceFinding `json:"findings"`
}

type InstallPackage struct {
	ID         string                `json:"id"`
	Name       string                `json:"name"`
	Mode       InstallMode           `json:"mode"`
	Version    string                `json:"version"`
	Signed     bool                  `json:"signed"`
	Offline    bool                  `json:"offline"`
	ImportedAt string                `json:"importedAt"`
	FilePath   string                `json:"filePath"`
	FileSize   int64                 `json:"fileSize"`
	Manifest   OfflineBundleManifest `json:"manifest"`
}

type OfflineBundleManifest struct {
	K3sVersion       string            `json:"k3sVersion"`
	RuntimeVersion   string            `json:"runtimeVersion"`
	KubectlVersion   string            `json:"kubectlVersion"`
	SGXDriverVersion string            `json:"sgxDriverVersion"`
	DCAPVersion      string            `json:"dcapVersion"`
	OSFamily         []string          `json:"osFamily"`
	SHA256           map[string]string `json:"sha256"`
}

type MarketplaceApp struct {
	ID             string               `json:"id"`
	Name           string               `json:"name"`
	Category       string               `json:"category"`
	CurrentVersion string               `json:"currentVersion"`
	VersionHistory []string             `json:"versionHistory"`
	PackageName    string               `json:"packageName"`
	Vendor         string               `json:"vendor"`
	Description    string               `json:"description"`
	Dependencies   []string             `json:"dependencies"`
	CompatibleEnv  []string             `json:"compatibleEnv"`
	Status         MarketplaceAppStatus `json:"status"`
	UpdatedAt      string               `json:"updatedAt"`
	PackageFile    string               `json:"packageFile"`
	PackageSize    int64                `json:"packageSize"`
}

type ClusterNode struct {
	ID                    string            `json:"id"`
	Name                  string            `json:"name"`
	Role                  string            `json:"role"`
	Status                string            `json:"status"`
	Version               string            `json:"version"`
	InternalIP            string            `json:"internalIp"`
	ManagementIP          string            `json:"managementIp"`
	OS                    string            `json:"os"`
	Arch                  string            `json:"arch"`
	Kernel                string            `json:"kernel"`
	ContainerRuntime      string            `json:"containerRuntime"`
	RuntimeClass          string            `json:"runtimeClass"`
	CapacityCPU           string            `json:"capacityCpu"`
	CapacityMemory        string            `json:"capacityMemory"`
	DiskCapacity          string            `json:"diskCapacity"`
	Labels                []string          `json:"labels"`
	Taints                []string          `json:"taints"`
	CPUUsage              int               `json:"cpuUsage"`
	MemoryUsage           int               `json:"memoryUsage"`
	DiskUsage             int               `json:"diskUsage"`
	PodCount              int               `json:"podCount"`
	LastHeartbeat         string            `json:"lastHeartbeat"`
	JoinedAt              string            `json:"joinedAt"`
	JoinMode              string            `json:"joinMode"`
	JoinStatus            string            `json:"joinStatus"`
	JoinCommand           string            `json:"joinCommand"`
	LastJoinAttemptAt     string            `json:"lastJoinAttemptAt"`
	LastJoinMessage       string            `json:"lastJoinMessage"`
	ProvisionMode         NodeProvisionMode `json:"provisionMode"`
	AutoProvision         bool              `json:"autoProvision"`
	EnableSGX             bool              `json:"enableSgx"`
	ProvisionStatus       string            `json:"provisionStatus"`
	ProvisionTaskID       string            `json:"provisionTaskId"`
	SGXStatus             SGXStatus         `json:"sgxStatus"`
	RuntimeStatus         RuntimeStatus     `json:"runtimeStatus"`
	K3sRole               NodeK3sRole       `json:"k3sRole"`
	OfflineBundleID       string            `json:"offlineBundleId"`
	ControlPlaneEndpoint  string            `json:"controlPlaneEndpoint"`
	InstallChannel        string            `json:"installChannel"`
	SSHHost               string            `json:"sshHost"`
	SSHPort               int               `json:"sshPort"`
	SSHUsername           string            `json:"sshUsername"`
	SSHPassword           string            `json:"sshPassword,omitempty"`
	SSHPasswordCiphertext string            `json:"sshPasswordCiphertext"`
	SSHPasswordConfigured bool              `json:"sshPasswordConfigured"`
	SSHKnownHostKey       string            `json:"sshKnownHostKey"`
	RxBytes               int64             `json:"rxBytes"`
	TxBytes               int64             `json:"txBytes"`
	NicName               string            `json:"nicName"`
	RxRate                float64           `json:"rxRate"`
	TxRate                float64           `json:"txRate"`
}

type ProvisioningTask struct {
	ID              string             `json:"id"`
	NodeID          string             `json:"nodeId"`
	Actor           string             `json:"actor"`
	Mode            NodeProvisionMode  `json:"mode"`
	Role            NodeK3sRole        `json:"role"`
	EnableSGX       bool               `json:"enableSgx"`
	OfflineBundleID string             `json:"offlineBundleId"`
	Status          ProvisioningStatus `json:"status"`
	CurrentStep     string             `json:"currentStep"`
	Steps           []ProvisioningStep `json:"steps"`
	CreatedAt       string             `json:"createdAt"`
	UpdatedAt       string             `json:"updatedAt"`
	StartedAt       string             `json:"startedAt"`
	CompletedAt     string             `json:"completedAt"`
	Message         string             `json:"message"`
}

type ProvisioningStep struct {
	Name       string                 `json:"name"`
	Status     ProvisioningStepStatus `json:"status"`
	StartedAt  string                 `json:"startedAt"`
	FinishedAt string                 `json:"finishedAt"`
	Message    string                 `json:"message"`
	Evidence   string                 `json:"evidence"`
}

type ClusterQuota struct {
	ID          string `json:"id"`
	Scope       string `json:"scope"`
	CPULimit    string `json:"cpuLimit"`
	MemoryLimit string `json:"memoryLimit"`
	PodLimit    int    `json:"podLimit"`
	UpdatedAt   string `json:"updatedAt"`
}

type ClusterAlert struct {
	ID        string `json:"id"`
	Level     string `json:"level"`
	Source    string `json:"source"`
	Message   string `json:"message"`
	Status    string `json:"status"`
	CreatedAt string `json:"createdAt"`
}

type ClusterLog struct {
	ID         string `json:"id"`
	NodeID     string `json:"nodeId"`
	Category   string `json:"category"`
	Level      string `json:"level"`
	Message    string `json:"message"`
	RecordedAt string `json:"recordedAt"`
}

type ClusterUpgradeStatus struct {
	Status         string `json:"status"`
	CurrentVersion string `json:"currentVersion"`
	TargetVersion  string `json:"targetVersion"`
	Progress       int    `json:"progress"`
	StartedAt      string `json:"startedAt"`
	CompletedAt    string `json:"completedAt"`
	Message        string `json:"message"`
}

type ClusterUpgradeRequest struct {
	Version string `json:"version"`
	Channel string `json:"channel"`
}

type AlertThresholdConfig struct {
	CPU int `json:"cpuThreshold"`
	Mem int `json:"memThreshold"`
	Pod int `json:"podThreshold"`
}

type EnclaveResource struct {
	ID           string `json:"id"`
	NodeID       string `json:"nodeId"`
	EPCSizeMB    int    `json:"epcSizeMb"`
	EPCUsedMB    int    `json:"epcUsedMb"`
	EnclaveCount int    `json:"enclaveCount"`
	Status       string `json:"status"`
}

type EnclaveKeyMaterial struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ComponentID string `json:"componentId"`
	Algorithm   string `json:"algorithm"`
	Status      string `json:"status"`
	RotatedAt   string `json:"rotatedAt"`
}

type EnclaveInspection struct {
	ID        string `json:"id"`
	Target    string `json:"target"`
	Status    string `json:"status"`
	Summary   string `json:"summary"`
	CheckedAt string `json:"checkedAt"`
}

type SecurityPolicyRule struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Category       string   `json:"category"`
	Scope          string   `json:"scope"`
	Mode           string   `json:"mode"`
	Status         string   `json:"status"`
	Targets        []string `json:"targets"`
	IsolationLevel string   `json:"isolationLevel,omitempty"`
	UpdatedAt      string   `json:"updatedAt"`
}

type ComplianceTask struct {
	ID          string `json:"id"`
	ControlID   string `json:"controlId"`
	ControlName string `json:"controlName"`
	Owner       string `json:"owner"`
	Status      string `json:"status"`
	DueAt       string `json:"dueAt"`
	Note        string `json:"note"`
}

type SystemSetting struct {
	ID        string `json:"id"`
	Category  string `json:"category"`
	Name      string `json:"name"`
	Value     string `json:"value"`
	UpdatedAt string `json:"updatedAt"`
}

type AuditEvent struct {
	ID        string `json:"id"`
	Actor     string `json:"actor"`
	Action    string `json:"action"`
	Target    string `json:"target"`
	Result    string `json:"result"`
	CreatedAt string `json:"createdAt"`
}

type User struct {
	ID           string     `json:"id"`
	Username     string     `json:"username"`
	DisplayName  string     `json:"displayName"`
	Role         Role       `json:"role"`
	Status       UserStatus `json:"status"`
	PasswordHash string     `json:"passwordHash"`
	CreatedAt    string     `json:"createdAt"`
	LastLoginAt  string     `json:"lastLoginAt"`
}

type UserView struct {
	ID          string     `json:"id"`
	Username    string     `json:"username"`
	DisplayName string     `json:"displayName"`
	Role        Role       `json:"role"`
	Status      UserStatus `json:"status"`
	CreatedAt   string     `json:"createdAt"`
	LastLoginAt string     `json:"lastLoginAt"`
}

type UserPayload struct {
	ID          string     `json:"id"`
	Username    string     `json:"username"`
	DisplayName string     `json:"displayName"`
	Role        Role       `json:"role"`
	Status      UserStatus `json:"status"`
	Password    string     `json:"password"`
}

type ChangePasswordPayload struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

type LoginRequest struct {
	Username      string `json:"username"`
	Password      string `json:"password"`
	CaptchaID     string `json:"captchaId"`
	CaptchaAnswer string `json:"captchaAnswer"`
}

type LoginResponse struct {
	Token string   `json:"token"`
	User  UserView `json:"user"`
}

type Session struct {
	Token       string `json:"token"`
	UserID      string `json:"userId"`
	CreatedAt   string `json:"createdAt"`
	ExpiresAt   string `json:"expiresAt"`
	InitiatedAt string `json:"initiatedAt"`
}

type BatchResultItem struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Success bool   `json:"success"`
	Message string `json:"message"`
}

type BatchDeployRequest struct {
	IDs []string `json:"ids"`
}

type BatchScaleRequest struct {
	IDs      []string `json:"ids"`
	Replicas int      `json:"replicas"`
}

type PluginType string

const (
	PluginTypeMonitoring   PluginType = "monitoring"
	PluginTypeCompliance   PluginType = "compliance"
	PluginTypeNotification PluginType = "notification"
	PluginTypeRuntime      PluginType = "runtime"
	PluginTypeCustom       PluginType = "custom"
)

type PluginStatus string

const (
	PluginEnabled  PluginStatus = "enabled"
	PluginDisabled PluginStatus = "disabled"
)

type PluginDefinition struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Type        PluginType        `json:"type"`
	Version     string            `json:"version"`
	Status      PluginStatus      `json:"status"`
	Endpoint    string            `json:"endpoint"`
	Config      map[string]string `json:"config"`
	Description string            `json:"description"`
	CreatedAt   string            `json:"createdAt"`
	UpdatedAt   string            `json:"updatedAt"`
}

type DashboardSummary struct {
	TotalNodes        int                  `json:"totalNodes"`
	ReadyNodes        int                  `json:"readyNodes"`
	TotalComponents   int                  `json:"totalComponents"`
	HealthyComponents int                  `json:"healthyComponents"`
	TotalImages       int                  `json:"totalImages"`
	SignedImages      int                  `json:"signedImages"`
	ActiveAlerts      int                  `json:"activeAlerts"`
	ComplianceScore   int                  `json:"complianceScore"`
	ClusterCPUUsage   int                  `json:"clusterCpuUsage"`
	ClusterMemUsage   int                  `json:"clusterMemUsage"`
	ClusterDiskUsage  int                  `json:"clusterDiskUsage"`
	K3sVersion        string               `json:"k3sVersion"`
	NetworkThroughput float64              `json:"networkThroughput"`
	SGXReadyNodes     int                  `json:"sgxReadyNodes"`
	Nodes             []DashboardNodeInfo  `json:"nodes"`
}

type DashboardNodeInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Role      string `json:"role"`
	Status    string `json:"status"`
	CPUUsage  int    `json:"cpuUsage"`
	MemUsage  int    `json:"memUsage"`
	DiskUsage int    `json:"diskUsage"`
	SGXStatus string `json:"sgxStatus"`
}
