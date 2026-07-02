export type Role = 'platform_admin' | 'security_admin' | 'auditor' | 'operator';
export type UserStatus = 'active' | 'disabled';
export type IsolationLevel = 'standard' | 'hardened' | 'enclave';
export type MarketplaceAppStatus = 'draft' | 'published' | 'unpublished';
export type NodeProvisionMode = 'online' | 'offline';
export type NodeK3sRole = 'control-plane' | 'worker';
export type ProvisioningStatus = 'pending' | 'running' | 'succeeded' | 'failed' | 'cancelled';
export type ProvisioningStepStatus = 'pending' | 'running' | 'succeeded' | 'failed' | 'skipped';

export interface UserView {
  id: string;
  username: string;
  displayName: string;
  role: Role;
  status: UserStatus;
  createdAt: string;
  lastLoginAt: string;
}

export interface UserPayload {
  id: string;
  username: string;
  displayName: string;
  role: Role;
  status: UserStatus;
  password: string;
}

export interface ChangePasswordPayload {
  currentPassword: string;
  newPassword: string;
}

export interface LoginResponse {
  token: string;
  user: UserView;
}

export interface ComponentCatalogItem {
  id: string;
  name: string;
  category: string;
  vendor: string;
  version: string;
  templateVersion: string;
  isolationRecommend: IsolationLevel;
  languageSupport: string[];
  dependencies: string[];
  description: string;
}

export interface IsolationPolicy {
  level: IsolationLevel;
  runtimeClass: string;
  readonlyRootFs: boolean;
  dropCapabilities: string[];
  requireAttestation: boolean;
  requireSignature: boolean;
  confidentialMaterial: boolean;
  runAsNonRoot: boolean;
  allowPrivilegeEscalation?: boolean;
  seccompProfile?: string;
  appArmorProfile?: string;
  seLinuxOptions?: string;
}

export interface OfflineBundleManifest {
  k3sVersion: string;
  runtimeVersion: string;
  kubectlVersion: string;
  sgxDriverVersion: string;
  dcapVersion: string;
  osFamily: string[];
  sha256: Record<string, string>;
}

export interface InstallPackage {
  id: string;
  name: string;
  mode: 'iso' | 'incremental' | 'offline_bundle';
  version: string;
  signed: boolean;
  offline: boolean;
  importedAt: string;
  filePath: string;
  fileSize: number;
  manifest: OfflineBundleManifest;
}

export interface MarketplaceApp {
  id: string;
  name: string;
  category: string;
  currentVersion: string;
  versionHistory: string[];
  packageName: string;
  vendor: string;
  description: string;
  dependencies: string[];
	compatibleEnv: string[];
	status: MarketplaceAppStatus;
	updatedAt: string;
	packageFile: string;
	packageSize: number;
}

export interface ClusterNode {
  id: string;
  name: string;
  role: string;
  status: string;
  version: string;
  internalIp: string;
  managementIp: string;
  os: string;
  arch: string;
  kernel: string;
  containerRuntime: string;
  runtimeClass: string;
  capacityCpu: string;
  capacityMemory: string;
  diskCapacity: string;
  labels: string[];
  taints: string[];
  cpuUsage: number;
  memoryUsage: number;
  diskUsage: number;
  podCount: number;
  lastHeartbeat: string;
  joinedAt: string;
  joinMode: string;
  joinStatus: string;
  joinCommand: string;
  lastJoinAttemptAt: string;
  lastJoinMessage: string;
  provisionMode: NodeProvisionMode;
  autoProvision: boolean;
  enableSgx: boolean;
  provisionStatus: string;
  provisionTaskId: string;
  sgxStatus: string;
  runtimeStatus: string;
  k3sRole: NodeK3sRole;
  offlineBundleId: string;
  controlPlaneEndpoint: string;
  installChannel: string;
  sshHost: string;
  sshPort: number;
  sshUsername: string;
  sshPassword: string;
  sshPasswordCiphertext: string;
  sshPasswordConfigured: boolean;
  rxBytes: number;
  txBytes: number;
  nicName: string;
  rxRate: number;
  txRate: number;
}

export interface ClusterQuota {
  id: string;
  scope: string;
  cpuLimit: string;
  memoryLimit: string;
  podLimit: number;
  updatedAt: string;
}

export interface ClusterAlert {
  id: string;
  level: 'info' | 'warning' | 'critical';
  source: string;
  message: string;
  status: 'open' | 'closed';
  createdAt: string;
}

export interface ClusterLog {
  id: string;
  nodeId: string;
  category: string;
  level: 'info' | 'warning' | 'error';
  message: string;
  recordedAt: string;
}

export interface ProvisioningStep {
  name: string;
  status: ProvisioningStepStatus;
  startedAt: string;
  finishedAt: string;
  message: string;
  evidence: string;
}

export interface ProvisioningTask {
  id: string;
  nodeId: string;
  actor: string;
  mode: NodeProvisionMode;
  role: NodeK3sRole;
  enableSgx: boolean;
  offlineBundleId: string;
  status: ProvisioningStatus;
  currentStep: string;
  steps: ProvisioningStep[];
  createdAt: string;
  updatedAt: string;
  startedAt: string;
  completedAt: string;
  message: string;
}

export interface ClusterUpgradeRequest {
  version: string;
  channel: string;
}

export interface ClusterUpgradeStatus {
  status: string;
  currentVersion: string;
  targetVersion: string;
  progress: number;
  startedAt: string;
  completedAt: string;
  message: string;
}

export interface EnclaveResource {
  id: string;
  nodeId: string;
  epcSizeMb: number;
  epcUsedMb: number;
  enclaveCount: number;
  status: string;
}

export interface EnclaveKeyMaterial {
  id: string;
  name: string;
  componentId: string;
  algorithm: string;
  status: string;
  rotatedAt: string;
}

export interface EnclaveInspection {
  id: string;
  target: string;
  status: 'healthy' | 'warning' | 'pending' | 'error';
  summary: string;
  checkedAt: string;
}

export interface SecurityPolicyRule {
  id: string;
  name: string;
  category: string;
  scope: string;
  mode: string;
  status: string;
  targets: string[];
  isolationLevel?: string;
  updatedAt: string;
}

export interface ComplianceTask {
  id: string;
  controlId: string;
  controlName: string;
  owner: string;
  status: string;
  dueAt: string;
  note: string;
}

export interface SystemSetting {
  id: string;
  category: string;
  name: string;
  value: string;
  updatedAt: string;
}

export interface AuditEvent {
  id: string;
  actor: string;
  action: string;
  target: string;
  result: string;
  createdAt: string;
}

export interface TopologyNode {
  id: string;
  kind: string;
  label: string;
  zone: string;
  refId: string;
  hostNodeId?: string;
  nicName?: string;
}

export interface TopologyLink {
  id: string;
  source: string;
  target: string;
  kind: string;
}

export interface TopologyGraph {
  nodes: TopologyNode[];
  links: TopologyLink[];
}

export interface ImageAsset {
  id: string;
  name: string;
  registry: string;
  repository: string;
  tag: string;
  digest: string;
  signed: boolean;
  sbom: boolean;
  vulnerability: 'low' | 'medium' | 'high' | 'pending_executor';
  enclaveReady: boolean;
  lastScanAt: string;
}

export interface ImageBuildRequest {
  name: string;
  registry: string;
  repository: string;
  tag: string;
  sourcePackage: string;
  dockerfilePath: string;
  buildArgs: string;
  enableSignature: boolean;
  generateSbom: boolean;
  enableSgxRuntime: boolean;
}

export interface ImageBuildResult {
  image: ImageAsset;
  log: string;
}

export interface ComponentDefinition {
  id: string;
  name: string;
  image: string;
  version: string;
  isolation: IsolationLevel;
  namespace: string;
  replicas: number;
	status: 'draft' | 'deployment_pending' | 'deployed' | 'degraded';
	networkAttachments: string[];
	packagePath: string;
	packageSize: number;
	mtlsEnabled: boolean;
}

export interface EnclaveProfile {
  id: string;
  componentId: string;
  mrEnclave: string;
  mrSigner: string;
  quotePolicy: string;
  secretsPolicy: string;
  sgxDevices: string[];
  sgxEnabled: boolean;
}

export interface NetworkAttachment {
  id: string;
  name: string;
  bridge: string;
  parentNic: string;
  vlanId: number;
  subnet: string;
  gateway: string;
  attachedComponents: string[];
}

export interface AttestationRecord {
  id: string;
  componentId: string;
  measurement: string;
  verifier: string;
  status: 'pending' | 'verified' | 'failed';
  verifiedAt: string;
  standard: string;
  controlId: string;
  controlName: string;
  policyResult: string;
  evidence: string;
  secretsReleased: boolean;
}

export interface AttestationResultPayload {
  status: 'pending' | 'verified' | 'failed';
  policyResult: string;
  evidence: string;
  secretsReleased: boolean;
}

export interface ComplianceFinding {
  category: string;
  level: 'high' | 'medium' | 'low';
  message: string;
  controlId: string;
  controlName: string;
  recommendation: string;
}

export interface ComplianceReport {
  id: string;
  title: string;
  score: number;
  generatedAt: string;
  status: 'ready' | 'running';
  standard: string;
  findings: ComplianceFinding[];
}

export interface DashboardSnapshot {
  images: ImageAsset[];
  components: ComponentDefinition[];
  enclaves: EnclaveProfile[];
  networks: NetworkAttachment[];
  attestations: AttestationRecord[];
  reports: ComplianceReport[];
  manifestHints: string[];
  clusterNodes: ClusterNode[];
  clusterQuotas: ClusterQuota[];
  clusterAlerts: ClusterAlert[];
  clusterLogs: ClusterLog[];
  provisioningTasks: ProvisioningTask[];
  clusterUpgrade: ClusterUpgradeStatus;
  alertThreshold: AlertThresholdConfig;
  installPackages: InstallPackage[];
  marketplaceApps: MarketplaceApp[];
  catalogItems: ComponentCatalogItem[];
  isolationPolicies: IsolationPolicy[];
  enclaveResources: EnclaveResource[];
  enclaveKeys: EnclaveKeyMaterial[];
  enclaveInspections: EnclaveInspection[];
  securityPolicies: SecurityPolicyRule[];
  complianceTasks: ComplianceTask[];
  systemSettings: SystemSetting[];
  auditEvents: AuditEvent[];
  topoLinks: TopologyLink[];
  topoEgress: TopologyNode[];
  plugins: PluginDefinition[];
}

export interface AlertThresholdConfig {
  cpuThreshold: number;
  memThreshold: number;
  podThreshold: number;
}

export interface BatchResultItem {
  id: string;
  name: string;
  success: boolean;
  message: string;
}

export interface BatchDeployRequest {
  ids: string[];
}

export interface BatchScaleRequest {
  ids: string[];
  replicas: number;
}

export interface PluginDefinition {
  id: string;
  name: string;
  type: 'monitoring' | 'compliance' | 'notification' | 'runtime' | 'custom';
  version: string;
  status: 'enabled' | 'disabled';
  endpoint: string;
  config: Record<string, string>;
  description: string;
  createdAt: string;
  updatedAt: string;
}
