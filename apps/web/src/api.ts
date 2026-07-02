import type {
  AlertThresholdConfig,
  AuditEvent,
  AttestationRecord,
  AttestationResultPayload,
  ChangePasswordPayload,
  ClusterNode,
  ClusterQuota,
  ClusterUpgradeRequest,
  ClusterUpgradeStatus,
  ComplianceReport,
  ComplianceTask,
  ComponentDefinition,
  DashboardSnapshot,
  EnclaveInspection,
  EnclaveKeyMaterial,
  EnclaveProfile,
  EnclaveResource,
  ImageAsset,
  ImageBuildRequest,
  ImageBuildResult,
  InstallPackage,
  LoginResponse,
  MarketplaceApp,
  NetworkAttachment,
  ProvisioningTask,
  SecurityPolicyRule,
  SystemSetting,
  TopologyLink,
  TopologyNode,
  UserPayload,
  UserView,
} from './types';

export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
  }
}

async function request<T>(path: string, options?: RequestInit): Promise<T> {
  const method = options?.method ?? 'GET';
  const headers: Record<string, string> = {
    ...(options?.headers as Record<string, string> ?? {}),
  };
  if (method !== 'GET' && method !== 'HEAD') {
    headers['Content-Type'] = 'application/json';
  }
  const response = await fetch(path, {
    credentials: 'include' as RequestCredentials,
    headers,
    ...options,
  });

  if (!response.ok) {
    const errorBody = (await response.json().catch(() => ({ error: '请求失败' }))) as { error?: string };
    throw new ApiError(response.status, errorBody.error ?? '请求失败');
  }

  return response.json() as Promise<T>;
}

export const api = {
  captcha: () => request<{ id: string; image: string; expiresAt: string }>('/api/v1/auth/captcha'),
  login: (username: string, password: string, captchaId: string, captchaAnswer: string) => request<LoginResponse>('/api/v1/auth/login', { method: 'POST', body: JSON.stringify({ username, password, captchaId, captchaAnswer }) }),
  logout: () => request<{ status: string }>('/api/v1/auth/logout', { method: 'POST' }),
  me: () => request<UserView>('/api/v1/auth/me'),
  changePassword: (payload: ChangePasswordPayload) => request<{ status: string }>('/api/v1/auth/password', { method: 'POST', body: JSON.stringify(payload) }),
  snapshot: () => request<DashboardSnapshot>('/api/v1/dashboard'),
  listUsers: () => request<UserView[]>('/api/v1/users'),
  saveUser: (payload: UserPayload) => request<UserPayload>('/api/v1/users', { method: 'POST', body: JSON.stringify(payload) }),
  updateUser: (id: string, payload: UserPayload) => request<UserPayload>(`/api/v1/users/${id}`, { method: 'PUT', body: JSON.stringify(payload) }),
  deleteUser: (id: string) => request<{ status: string }>(`/api/v1/users/${id}`, { method: 'DELETE' }),
  createImage: (payload: ImageAsset) => request<ImageAsset>('/api/v1/images', { method: 'POST', body: JSON.stringify(payload) }),
  updateImage: (id: string, payload: ImageAsset) => request<ImageAsset>(`/api/v1/images/${id}`, { method: 'PUT', body: JSON.stringify(payload) }),
  buildImage: (payload: ImageBuildRequest) => request<ImageBuildResult>('/api/v1/images/build', { method: 'POST', body: JSON.stringify(payload) }),
  deleteImage: (id: string) => request<{ status: string }>(`/api/v1/images/${id}`, { method: 'DELETE' }),
  createComponent: (payload: ComponentDefinition) => request<ComponentDefinition>('/api/v1/components', { method: 'POST', body: JSON.stringify(payload) }),
  updateComponent: (id: string, payload: ComponentDefinition) => request<ComponentDefinition>(`/api/v1/components/${id}`, { method: 'PUT', body: JSON.stringify(payload) }),
  deleteComponent: (id: string) => request<{ status: string }>(`/api/v1/components/${id}`, { method: 'DELETE' }),
  deployComponent: (id: string) => request<{ status: string }>(`/api/v1/components/${id}/deploy`, { method: 'POST' }),
  scaleComponent: (id: string, replicas: number) => request<{ status: string }>(`/api/v1/components/${id}/scale`, { method: 'POST', body: JSON.stringify({ replicas }) }),
  upgradeComponent: (id: string, version: string) => request<{ status: string }>(`/api/v1/components/${id}/upgrade`, { method: 'POST', body: JSON.stringify({ version }) }),
  getManifest: (id: string) => request<{ manifest: string }>(`/api/v1/components/${id}/manifest`),
  createNetwork: (payload: NetworkAttachment) => request<NetworkAttachment>('/api/v1/networks', { method: 'POST', body: JSON.stringify(payload) }),
  updateNetwork: (id: string, payload: NetworkAttachment) => request<NetworkAttachment>(`/api/v1/networks/${id}`, { method: 'PUT', body: JSON.stringify(payload) }),
  deleteNetwork: (id: string) => request<{ status: string }>(`/api/v1/networks/${id}`, { method: 'DELETE' }),
  saveSecurityPolicy: (payload: SecurityPolicyRule) => request<SecurityPolicyRule>('/api/v1/security-policies', { method: 'POST', body: JSON.stringify(payload) }),
  updateSecurityPolicy: (id: string, payload: SecurityPolicyRule) => request<SecurityPolicyRule>(`/api/v1/security-policies/${id}`, { method: 'PUT', body: JSON.stringify(payload) }),
  deleteSecurityPolicy: (id: string) => request<{ status: string }>(`/api/v1/security-policies/${id}`, { method: 'DELETE' }),
  createInstallPackage: (payload: InstallPackage) => request<InstallPackage>('/api/v1/install-packages', { method: 'POST', body: JSON.stringify(payload) }),
  updateInstallPackage: (id: string, payload: InstallPackage) => request<InstallPackage>(`/api/v1/install-packages/${id}`, { method: 'PUT', body: JSON.stringify(payload) }),
  deleteInstallPackage: (id: string) => request<{ status: string }>(`/api/v1/install-packages/${id}`, { method: 'DELETE' }),
  createMarketplaceApp: (payload: MarketplaceApp) => request<MarketplaceApp>('/api/v1/marketplace-apps', { method: 'POST', body: JSON.stringify(payload) }),
  updateMarketplaceApp: (id: string, payload: MarketplaceApp) => request<MarketplaceApp>(`/api/v1/marketplace-apps/${id}`, { method: 'PUT', body: JSON.stringify(payload) }),
  deleteMarketplaceApp: (id: string) => request<{ status: string }>(`/api/v1/marketplace-apps/${id}`, { method: 'DELETE' }),
  publishMarketplaceApp: (id: string) => request<{ status: string }>(`/api/v1/marketplace-apps/${id}/publish`, { method: 'POST' }),
  unpublishMarketplaceApp: (id: string) => request<{ status: string }>(`/api/v1/marketplace-apps/${id}/unpublish`, { method: 'POST' }),
  addMarketplaceAppVersion: (id: string, version: string, packageName: string) => request<{ status: string }>(`/api/v1/marketplace-apps/${id}/versions`, { method: 'POST', body: JSON.stringify({ version, packageName }) }),
  createClusterNode: (payload: ClusterNode) => request<ClusterNode>('/api/v1/cluster/nodes', { method: 'POST', body: JSON.stringify(payload) }),
  updateClusterNode: (id: string, payload: ClusterNode) => request<ClusterNode>(`/api/v1/cluster/nodes/${id}`, { method: 'PUT', body: JSON.stringify(payload) }),
  getProvisioningTask: (id: string) => request<ProvisioningTask>(`/api/v1/provisioning-tasks/${id}`),
  listProvisioningTasks: () => request<ProvisioningTask[]>('/api/v1/provisioning-tasks'),
  retryProvisioningTask: (id: string) => request<ProvisioningTask>(`/api/v1/provisioning-tasks/${id}/retry`, { method: 'POST' }),
  cancelProvisioningTask: (id: string) => request<ProvisioningTask>(`/api/v1/provisioning-tasks/${id}/cancel`, { method: 'POST' }),
  saveClusterQuota: (payload: ClusterQuota) => request<ClusterQuota>('/api/v1/cluster/quotas', { method: 'POST', body: JSON.stringify(payload) }),
  updateClusterQuota: (id: string, payload: ClusterQuota) => request<ClusterQuota>(`/api/v1/cluster/quotas/${id}`, { method: 'PUT', body: JSON.stringify(payload) }),
  deleteClusterNode: (id: string) => request<{ status: string }>(`/api/v1/cluster/nodes/${id}`, { method: 'DELETE' }),
  deleteClusterQuota: (id: string) => request<{ status: string }>(`/api/v1/cluster/quotas/${id}`, { method: 'DELETE' }),
	getClusterUpgrade: () => request<ClusterUpgradeStatus>('/api/v1/cluster/upgrade'),
	upgradeCluster: (payload: ClusterUpgradeRequest) => request<{ status: string }>('/api/v1/cluster/upgrade', { method: 'POST', body: JSON.stringify(payload) }),
	downloadClusterUpgrade: (payload: ClusterUpgradeRequest) => request<{ status: string }>('/api/v1/cluster/upgrade/download', { method: 'POST', body: JSON.stringify(payload) }),
	resetClusterUpgrade: () => request<{ status: string }>('/api/v1/cluster/upgrade', { method: 'PUT' }),
	getAlertThreshold: () => request<AlertThresholdConfig>('/api/v1/cluster/alert-threshold'),
	saveAlertThreshold: (payload: AlertThresholdConfig) => request<AlertThresholdConfig>('/api/v1/cluster/alert-threshold', { method: 'PUT', body: JSON.stringify(payload) }),
	fetchK3sVersions: async (channel: string): Promise<string[]> => {
		const res = await fetch(`/api/v1/cluster/k3s-versions?channel=${encodeURIComponent(channel)}`, { credentials: 'include' });
		if (!res.ok) throw new Error('获取 K3s 版本失败');
		return res.json() as Promise<string[]>;
	},
	uploadFile: (file: File, uploadType: string = 'packages', onProgress?: (percent: number) => void) => new Promise<{ path: string; name: string; size: number }>((resolve, reject) => {
		const formData = new FormData();
		formData.append('file', file);
		formData.append('type', uploadType);
		const xhr = new XMLHttpRequest();
		xhr.open('POST', '/api/v1/upload');
		xhr.withCredentials = true;
		xhr.timeout = 600_000;
		xhr.upload.onprogress = event => {
			if (event.lengthComputable && onProgress) {
				const pct = Math.round((event.loaded / event.total) * 100);
				onProgress(Math.max(1, Math.min(100, pct)));
			}
		};
		xhr.onreadystatechange = () => {
			if (xhr.readyState !== XMLHttpRequest.DONE) return;
			let body: { path?: string; name?: string; size?: number; error?: string } = {};
			try {
				body = xhr.responseText ? JSON.parse(xhr.responseText) as typeof body : {};
			} catch {
				body = { error: xhr.responseText || '上传失败' };
			}
			if (xhr.status >= 200 && xhr.status < 300 && body.path && body.name && typeof body.size === 'number') {
				onProgress?.(100);
				resolve({ path: body.path, name: body.name, size: body.size });
				return;
			}
			reject(new ApiError(xhr.status, body.error ?? '上传失败'));
		};
		xhr.ontimeout = () => reject(new ApiError(408, '上传超时，请检查网络后重试'));
		xhr.onerror = () => reject(new ApiError(0, '网络错误，上传失败'));
		xhr.send(formData);
	}),
  createEnclave: (payload: EnclaveProfile) => request<EnclaveProfile>('/api/v1/enclaves', { method: 'POST', body: JSON.stringify(payload) }),
  updateEnclave: (id: string, payload: EnclaveProfile) => request<EnclaveProfile>(`/api/v1/enclaves/${id}`, { method: 'PUT', body: JSON.stringify(payload) }),
  deleteEnclave: (id: string) => request<{ status: string }>(`/api/v1/enclaves/${id}`, { method: 'DELETE' }),
  saveEnclaveResource: (payload: EnclaveResource) => request<EnclaveResource>('/api/v1/enclave-resources', { method: 'POST', body: JSON.stringify(payload) }),
  updateEnclaveResource: (id: string, payload: EnclaveResource) => request<EnclaveResource>(`/api/v1/enclave-resources/${id}`, { method: 'PUT', body: JSON.stringify(payload) }),
  deleteEnclaveResource: (id: string) => request<{ status: string }>(`/api/v1/enclave-resources/${id}`, { method: 'DELETE' }),
  saveEnclaveKey: (payload: EnclaveKeyMaterial) => request<EnclaveKeyMaterial>('/api/v1/enclave-keys', { method: 'POST', body: JSON.stringify(payload) }),
  updateEnclaveKey: (id: string, payload: EnclaveKeyMaterial) => request<EnclaveKeyMaterial>(`/api/v1/enclave-keys/${id}`, { method: 'PUT', body: JSON.stringify(payload) }),
  deleteEnclaveKey: (id: string) => request<{ status: string }>(`/api/v1/enclave-keys/${id}`, { method: 'DELETE' }),
  saveComplianceTask: (payload: ComplianceTask) => request<ComplianceTask>('/api/v1/compliance-tasks', { method: 'POST', body: JSON.stringify(payload) }),
  updateComplianceTask: (id: string, payload: ComplianceTask) => request<ComplianceTask>(`/api/v1/compliance-tasks/${id}`, { method: 'PUT', body: JSON.stringify(payload) }),
  deleteComplianceTask: (id: string) => request<{ status: string }>(`/api/v1/compliance-tasks/${id}`, { method: 'DELETE' }),
  saveSystemSetting: (payload: SystemSetting) => request<SystemSetting>('/api/v1/system-settings', { method: 'POST', body: JSON.stringify(payload) }),
  updateSystemSetting: (id: string, payload: SystemSetting) => request<SystemSetting>(`/api/v1/system-settings/${id}`, { method: 'PUT', body: JSON.stringify(payload) }),
  deleteSystemSetting: (id: string) => request<{ status: string }>(`/api/v1/system-settings/${id}`, { method: 'DELETE' }),
  listAuditEvents: (keyword = '') => request<AuditEvent[]>(`/api/v1/audit${keyword ? `?keyword=${encodeURIComponent(keyword)}` : ''}`),
  runAttestation: () => request('/api/v1/attestations/run', { method: 'POST' }),
  listAttestations: () => request<AttestationRecord[]>('/api/v1/attestations'),
  getAttestation: (id: string) => request<AttestationRecord>(`/api/v1/attestations/${id}`),
  submitAttestationResult: (id: string, payload: AttestationResultPayload) => request<AttestationRecord>(`/api/v1/attestations/${id}/result`, { method: 'POST', body: JSON.stringify(payload) }),
  runEnclaveInspection: () => request<EnclaveInspection[]>('/api/v1/enclave-inspections/run', { method: 'POST' }),
  listEnclaveInspections: () => request<EnclaveInspection[]>('/api/v1/enclave-inspections'),
  getEnclaveInspection: (id: string) => request<EnclaveInspection>(`/api/v1/enclave-inspections/${id}`),
  runCompliance: () => request<ComplianceReport>('/api/v1/compliance/run', { method: 'POST' }),
  listTopoLinks: () => request<TopologyLink[]>('/api/v1/topo/links'),
  createTopoLink: (payload: TopologyLink) => request<TopologyLink>('/api/v1/topo/links', { method: 'POST', body: JSON.stringify(payload) }),
  updateTopoLink: (id: string, payload: TopologyLink) => request<TopologyLink>(`/api/v1/topo/links/${id}`, { method: 'PUT', body: JSON.stringify(payload) }),
  deleteTopoLink: (id: string) => request<{ status: string }>(`/api/v1/topo/links/${id}`, { method: 'DELETE' }),
  listTopoEgress: () => request<TopologyNode[]>('/api/v1/topo/egress'),
  createTopoEgress: (payload: TopologyNode) => request<TopologyNode>('/api/v1/topo/egress', { method: 'POST', body: JSON.stringify(payload) }),
  updateTopoEgress: (id: string, payload: TopologyNode) => request<TopologyNode>(`/api/v1/topo/egress/${id}`, { method: 'PUT', body: JSON.stringify(payload) }),
  deleteTopoEgress: (id: string) => request<{ status: string }>(`/api/v1/topo/egress/${id}`, { method: 'DELETE' }),
  exportComplianceReport: async (reportId: string, format: 'csv' | 'html'): Promise<void> => {
    const res = await fetch(`/api/v1/compliance/export?id=${encodeURIComponent(reportId)}&format=${format}`, { credentials: 'include' });
    if (!res.ok) {
      const text = await res.text();
      try {
        const j = JSON.parse(text);
        throw new ApiError(res.status, j.error || text);
      } catch (e) {
        if (e instanceof ApiError) throw e;
        throw new ApiError(res.status, text);
      }
    }
    const blob = await res.blob();
    const disposition = res.headers.get('Content-Disposition');
    const match = disposition?.match(/filename="(.+)"/);
    const filename = match ? match[1] : `compliance-report.${format}`;
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = filename;
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);
  },
  batchDeployComponents: (ids: string[]) => request<{ id: string; name: string; success: boolean; message: string }[]>('/api/v1/components/batch/deploy', { method: 'POST', body: JSON.stringify({ ids }) }),
  batchScaleComponents: (ids: string[], replicas: number) => request<{ id: string; name: string; success: boolean; message: string }[]>('/api/v1/components/batch/scale', { method: 'POST', body: JSON.stringify({ ids, replicas }) }),
  getPlugins: () => request<import('./types').PluginDefinition[]>('/api/v1/plugins'),
  savePlugin: (payload: import('./types').PluginDefinition) => request<import('./types').PluginDefinition>('/api/v1/plugins', { method: 'POST', body: JSON.stringify(payload) }),
  updatePlugin: (id: string, payload: import('./types').PluginDefinition) => request<import('./types').PluginDefinition>(`/api/v1/plugins/${id}`, { method: 'PUT', body: JSON.stringify(payload) }),
  deletePlugin: (id: string) => request<{ status: string }>(`/api/v1/plugins/${id}`, { method: 'DELETE' }),
  enablePlugin: (id: string) => request<{ status: string }>(`/api/v1/plugins/${id}/enable`, { method: 'POST' }),
  disablePlugin: (id: string) => request<{ status: string }>(`/api/v1/plugins/${id}/disable`, { method: 'POST' }),
};
