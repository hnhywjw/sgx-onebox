import { useState } from 'react';
import type { ClusterNode, ComponentDefinition, ImageAsset, ImageBuildRequest, InstallPackage, MarketplaceApp, ProvisioningTask } from '../types';
import { api } from '../api';
import { BulkDeleteControls } from './BulkDeleteControls';
import { rowCheckboxClassName, toggleSelected } from './bulkSelection';

function FieldHint({ children }: { children: string }) {
  return <span className="field-hint">{children}</span>;
}

const initialNode = { id: '', name: '', role: 'control-plane', internalIp: '', managementIp: '', os: '', sshHost: '', sshPort: 22, sshUsername: 'root', sshPassword: '', version: '', provisionMode: 'online' as ClusterNode['provisionMode'], autoProvision: true, enableSgx: false, k3sRole: 'control-plane' as ClusterNode['k3sRole'], offlineBundleId: '', controlPlaneEndpoint: '', installChannel: '' };
const initialImage = { name: '', registry: '', repository: '', tag: 'latest', id: '' };
const initialImageBuild: ImageBuildRequest = { name: '', registry: '', repository: '', tag: 'latest', sourcePackage: '', dockerfilePath: 'Dockerfile', buildArgs: '', enableSignature: true, generateSbom: true, enableSgxRuntime: false };
const initialComponent = { name: '', image: '', version: '', namespace: '', isolation: 'standard' as ComponentDefinition['isolation'], replicas: 1, status: '' as ComponentDefinition['status'], packagePath: '', packageSize: 0, mtlsEnabled: false, id: '' };
const initialPackage = { name: '', version: '', mode: 'iso' as InstallPackage['mode'], filePath: '', fileSize: 0, id: '' };
const initialMarketplaceApp = { name: '', category: '', vendor: '', currentVersion: '', packageFile: '', packageSize: 0, packageName: '', id: '' };

function parseIPv4(value: string): number[] | null {
  const parts = value.trim().split('.');
  if (parts.length !== 4) return null;
  const nums = parts.map(part => Number(part));
  if (nums.some((num, index) => !/^\d+$/.test(parts[index]) || !Number.isInteger(num) || num < 0 || num > 255)) return null;
  return nums;
}

function isValidIPv4(value: string): boolean {
  return parseIPv4(value) !== null;
}

function isValidHost(value: string): boolean {
  const host = value.trim();
  return isValidIPv4(host) || /^[a-zA-Z0-9.-]+$/.test(host);
}

function isValidPort(value: number): boolean {
  return Number.isInteger(value) && value >= 1 && value <= 65535;
}

function toClusterNodePayload(node: typeof initialNode): ClusterNode {
  return {
    status: 'credential_ready', kernel: '', arch: '', containerRuntime: '', runtimeClass: '', capacityCpu: '', capacityMemory: '', diskCapacity: '', labels: [], taints: [], cpuUsage: 0, memoryUsage: 0, diskUsage: 0, podCount: 0, lastHeartbeat: '', joinedAt: '', joinMode: 'ssh', joinStatus: 'credential_ready', joinCommand: '', lastJoinAttemptAt: '', lastJoinMessage: '', provisionStatus: 'pending', provisionTaskId: '',     sgxStatus: node.enableSgx ? 'sgx_pending' : 'unknown', runtimeStatus: node.autoProvision ? 'runtime_pending' : 'unknown', sshPasswordCiphertext: '', sshPasswordConfigured: Boolean(node.sshPassword), rxBytes: 0, txBytes: 0, nicName: 'eth0', rxRate: 0, txRate: 0,
    ...node,
    role: node.k3sRole,
    k3sRole: node.k3sRole,
    installChannel: node.installChannel || node.version,
  };
}

function toClusterNodeUpdatePayload(node: typeof initialNode, original: ClusterNode): ClusterNode {
  return {
    ...original,
    ...node,
    role: node.k3sRole,
    k3sRole: node.k3sRole,
    installChannel: node.installChannel || node.version,
    sshPasswordCiphertext: original.sshPasswordCiphertext,
    sshPasswordConfigured: original.sshPasswordConfigured,
  };
}

function toImagePayload(image: typeof initialImage, original?: ImageAsset): ImageAsset {
  return { ...(original ?? { id: '', digest: '', signed: false, sbom: false, vulnerability: 'low' as ImageAsset['vulnerability'], enclaveReady: false, lastScanAt: '' }), ...image };
}

function toComponentPayload(component: typeof initialComponent, original?: ComponentDefinition): ComponentDefinition {
  return { ...(original ?? { id: '', networkAttachments: [] }), ...component, status: component.status || original?.status || 'draft' } as ComponentDefinition;
}

function toInstallPackagePayload(pkg: typeof initialPackage, original?: InstallPackage): InstallPackage {
  return { ...(original ?? { id: '', signed: false, offline: pkg.mode === 'offline_bundle', importedAt: '', manifest: { k3sVersion: '', runtimeVersion: '', kubectlVersion: '', sgxDriverVersion: '', dcapVersion: '', osFamily: [], sha256: {} } }), ...pkg, offline: pkg.mode === 'offline_bundle' };
}

function toMarketplaceAppPayload(app: typeof initialMarketplaceApp, original?: MarketplaceApp): MarketplaceApp {
  return { ...(original ?? { id: '', versionHistory: [], description: '', dependencies: [], compatibleEnv: [], status: 'draft', updatedAt: '' }), ...app };
}

const statusBadgeClass = (status: string) => {
  if (status === 'ready' || status === 'succeeded' || status === 'active') return 'badge-success';
  if (status === 'failed' || status === 'unreachable') return 'badge-danger';
  if (status === 'running' || status === 'pending' || status === 'cancelled' || status === 'warning') return 'badge-warning';
  return 'badge-info';
};

const stepLabels: Record<string, string> = {
  preflight: '前置检查',
  k3s_server_install: '安装 K3s Server',
  k3s_agent_install: '安装 K3s Agent',
  runtime_verify: '验证运行时',
  sgx_dcap_install: '验证 SGX/DCAP',
  final_verify: '最终检查',
};

const taskStatusLabels: Record<string, string> = {
  pending: '等待执行',
  running: '执行中',
  succeeded: '已完成',
  failed: '失败',
  cancelled: '已取消',
};

interface DeploymentTabProps {
  clusterNodes: ClusterNode[];
  images: ImageAsset[];
  components: ComponentDefinition[];
  packages: InstallPackage[];
  provisioningTasks: ProvisioningTask[];
  marketApps: MarketplaceApp[];
  k3sVersions: string[];
  activeSubTab: string;
  setActiveSubTab: (key: string) => void;
  loadData: (silentOrTab?: boolean | string) => Promise<void>;
  handleSave: (action: () => Promise<unknown>, successMsg: string) => Promise<boolean>;
  handleDelete: (action: () => Promise<unknown>, successMsg: string) => Promise<boolean>;
  setError: (msg: string) => void;
  setSuccess: (msg: string) => void;
  setManifestContent: (content: string | null) => void;
  manifestHints: string[];
  dataLoading: boolean;
  submitting: boolean;
  formatTime: (value: string) => string;
}

export function DeploymentTab({
  clusterNodes,
  images,
  components,
  packages,
  provisioningTasks,
  marketApps,
  k3sVersions,
  activeSubTab,
  setActiveSubTab,
  loadData,
  handleSave,
  handleDelete,
  setError,
  setSuccess,
  setManifestContent,
  manifestHints,
  dataLoading,
  submitting,
  formatTime,
}: DeploymentTabProps) {
  const [newNode, setNewNode] = useState(initialNode);
  const [newImage, setNewImage] = useState(initialImage);
  const [imageBuild, setImageBuild] = useState<ImageBuildRequest>(initialImageBuild);
  const [imageBuildLog, setImageBuildLog] = useState('');
  const [newComponent, setNewComponent] = useState(initialComponent);
  const [newPackage, setNewPackage] = useState(initialPackage);
  const [packageUploading, setPackageUploading] = useState(false);
  const [packageUploadProgress, setPackageUploadProgress] = useState(0);
  const [newMarketplaceApp, setNewMarketplaceApp] = useState(initialMarketplaceApp);
  const [detailNodeId, setDetailNodeId] = useState<string | null>(null);
  const [showNewNodeForm, setShowNewNodeForm] = useState(false);
  const [showNewImageForm, setShowNewImageForm] = useState(false);
  const [showImageBuildForm, setShowImageBuildForm] = useState(false);
  const [showNewComponentForm, setShowNewComponentForm] = useState(false);
  const [showNewPackageForm, setShowNewPackageForm] = useState(false);
  const [showNewMarketplaceAppForm, setShowNewMarketplaceAppForm] = useState(false);
  const [saving, setSaving] = useState(false);
  const offlineBundles = packages.filter(pkg => pkg.mode === 'offline_bundle' && pkg.offline);
  const builtInAvailable = manifestHints.includes('built-in-bundle-available');
  const taskForNode = (node: ClusterNode) => provisioningTasks.find(task => task.id === node.provisionTaskId) ?? provisioningTasks.find(task => task.nodeId === node.id);
  const selectedDetailNode = detailNodeId ? clusterNodes.find(node => node.id === detailNodeId) : null;
  const selectedDetailTask = selectedDetailNode ? taskForNode(selectedDetailNode) : undefined;
  const startEditNode = (node: ClusterNode) => {
    setNewNode({
      id: node.id,
      name: node.name,
      role: node.role || node.k3sRole || 'worker',
      internalIp: node.internalIp || '',
      managementIp: node.managementIp || '',
      os: node.os || '',
      sshHost: node.sshHost || '',
      sshPort: node.sshPort || 22,
      sshUsername: node.sshUsername || 'root',
      sshPassword: '',
      version: node.version || '',
      provisionMode: node.provisionMode || 'online',
      autoProvision: Boolean(node.autoProvision),
      enableSgx: Boolean(node.enableSgx),
      k3sRole: node.k3sRole || (node.role as ClusterNode['k3sRole']) || 'worker',
      offlineBundleId: node.offlineBundleId || '',
      controlPlaneEndpoint: node.controlPlaneEndpoint || '',
      installChannel: node.installChannel || node.version || '',
    });
    setShowNewNodeForm(true);
    setDetailNodeId(null);
  };
  const startEditImage = (image: ImageAsset) => {
    setNewImage({ name: image.name, registry: image.registry, repository: image.repository, tag: image.tag, id: image.id });
    setShowNewImageForm(true);
    setShowImageBuildForm(false);
  };
  const startEditPackage = (pkg: InstallPackage) => {
    setNewPackage({ name: pkg.name, version: pkg.version, mode: pkg.mode, filePath: pkg.filePath, fileSize: pkg.fileSize, id: pkg.id });
    setShowNewPackageForm(true);
  };
  const startEditMarketApp = (app: MarketplaceApp) => {
    setNewMarketplaceApp({ name: app.name, category: app.category, vendor: app.vendor, currentVersion: app.currentVersion, packageFile: app.packageFile, packageSize: app.packageSize, packageName: app.packageName, id: app.id });
    setShowNewMarketplaceAppForm(true);
  };
  const startEditComponent = (comp: ComponentDefinition) => {
    setNewComponent({ name: comp.name, image: comp.image, version: comp.version, namespace: comp.namespace, isolation: comp.isolation, replicas: comp.replicas, status: comp.status, packagePath: comp.packagePath, packageSize: comp.packageSize, mtlsEnabled: comp.mtlsEnabled, id: comp.id });
    setShowNewComponentForm(true);
  };
  const detailItem = (label: string, value: string | number | undefined) => <div className="node-detail-item"><strong>{label}</strong><span>{value === undefined || value === '' ? '-' : value}</span></div>;
  const provisioningProgress = (task?: ProvisioningTask) => {
    if (!task || task.steps.length === 0) return 0;
    const done = task.steps.filter(step => step.status === 'succeeded' || step.status === 'skipped').length;
    return Math.round((done / task.steps.length) * 100);
  };
  const controlPlaneNodes = clusterNodes.filter(node => node.k3sRole === 'control-plane' || node.role === 'control-plane');
  const workerNodes = clusterNodes.filter(node => (node.k3sRole || node.role) === 'worker');
  const imageLabel = (image: ImageAsset) => `${image.name} · ${image.repository}:${image.tag} (${image.id})`;
  const readyControlPlanes = controlPlaneNodes.filter(node => node.status === 'ready');
  const readyWorkers = workerNodes.filter(node => node.status === 'ready');
  const deploymentMode = controlPlaneNodes.length === 0 ? '未初始化' : controlPlaneNodes.length === 1 ? '单 Master 模式' : '多 Master 高可用模式';
  const deploymentModeClass = controlPlaneNodes.length === 0 ? 'badge-warning' : controlPlaneNodes.length === 1 ? 'badge-info' : 'badge-success';
  const deploymentRecommendation = controlPlaneNodes.length === 0
    ? '请先注册一个 Control Plane 节点完成平台基础部署。'
    : controlPlaneNodes.length === 1 && workerNodes.length === 0
      ? '当前一个 Master 即可承载完整功能；资源不足时增加 Worker，可靠性要求高时增加 Master。'
      : controlPlaneNodes.length === 1
        ? '当前通过 Worker 扩展计算资源；可靠性要求高时可继续增加 Master。'
        : '当前已具备多 Master 控制面形态；建议使用奇数个 Master 并配置稳定的控制面 Endpoint。';
  const nodeRoleLabel = (node: ClusterNode) => {
    const isControlPlane = node.k3sRole === 'control-plane' || node.role === 'control-plane';
    if (isControlPlane && controlPlaneNodes[0]?.id === node.id) return '首个 Master';
    if (isControlPlane) return 'HA Master';
    return 'Worker 扩容';
  };
  const [selectedComponents, setSelectedComponents] = useState<Set<string>>(new Set());
  const [selectedImages, setSelectedImages] = useState<Set<string>>(new Set());

  const runBulkDelete = (ids: string[], action: (id: string) => Promise<unknown>, label: string, clear: () => void) => {
    if (ids.length === 0) return;
    if (!confirm(`确认删除选中的 ${ids.length} 个${label}？`)) return;
    void handleSave(async () => {
      await Promise.all(ids.map(id => action(id)));
      clear();
    }, `已删除 ${ids.length} 个${label}`);
  };

  const handleFileUpload = async (file: File, uploadType: string) => {
    const maxSize = uploadType === 'packages' ? 2 * 1024 * 1024 * 1024 : 100 * 1024 * 1024;
    const maxLabel = uploadType === 'packages' ? '2GB' : '100MB';
    if (file.size > maxSize) { setError(`文件大小不能超过 ${maxLabel}`); return null; }
    try {
      const result = await api.uploadFile(file, uploadType);
      return result;
    } catch (err) { setError((err as Error).message); return null; }
  };

  const withSaving = (action: () => Promise<unknown>, successMsg: string) => {
    setSaving(true);
    return handleSave(action, successMsg).finally(() => setSaving(false));
  };

  const submitImageBuild = () => {
    if (!imageBuild.name.trim()) { setError('请输入镜像名称'); return; }
    if (!imageBuild.sourcePackage.trim()) { setError('请输入或上传安全组件源码/制品包'); return; }
    if (!imageBuild.registry.trim()) { setError('请输入 Registry'); return; }
    if (!imageBuild.repository.trim()) { setError('请输入 Repository'); return; }
    if (!imageBuild.tag.trim()) { setError('请输入 Tag'); return; }
    void withSaving(async () => {
      const res = await api.buildImage(imageBuild);
      setImageBuildLog(res.log);
    }, '镜像构建任务已登记').then(ok => ok && setShowImageBuildForm(false));
  };

  return (
    <>
      <div className="panel top-banner top-banner-soft hero">
        <div className="hero-main">
          <h2>交付与部署</h2>
          <p className="muted">集群、镜像、组件与应用生命周期管理</p>
        </div>
        <div className="hero-badges">
          <button className="secondary-button" style={{ minHeight: 36, padding: '6px 14px', fontSize: 13 }} onClick={() => loadData('deployment')} disabled={dataLoading}>刷新数据</button>
        </div>
      </div>
      <div className="sub-tab-bar">
        {[
          { key: 'nodes', label: '集群管理' },
          { key: 'images', label: '组件镜像管理' },
          { key: 'components', label: '安全组件部署' },
          { key: 'packages', label: '安装包管理' },
          { key: 'app-market', label: '应用市场' },
        ].map(st => (
          <button key={st.key} className={`sub-tab-button${activeSubTab === st.key ? ' sub-tab-active' : ''}`} onClick={() => { setActiveSubTab(st.key); setError(''); setSuccess(''); }}>
            {st.label}
          </button>
        ))}
      </div>

      {/* Nodes */}
      {activeSubTab === 'nodes' && (
        <div className="panel">
          <div className="section-head" style={{ marginBottom: 12 }}>
            <h3 style={{ margin: 0, fontSize: 16 }}>集群管理 ({clusterNodes.length})</h3>
            <button className="primary-button" onClick={() => { setNewNode(initialNode); setShowNewNodeForm(true); }}>注册节点</button>
          </div>
          <div className="node-mode-grid">
            <div className="node-mode-card node-mode-primary">
              <div className="node-mode-title"><span>当前部署模式</span><span className={`badge ${deploymentModeClass}`}>{deploymentMode}</span></div>
              <p>{deploymentRecommendation}</p>
            </div>
            <div className="node-mode-card">
              <span className="muted">控制面</span>
              <strong>{readyControlPlanes.length}/{controlPlaneNodes.length}</strong>
              <p>{controlPlaneNodes.length <= 1 ? '单 Master 可完成全部功能部署' : '多 Master 提供控制面可靠性'}</p>
            </div>
            <div className="node-mode-card">
              <span className="muted">Worker</span>
              <strong>{readyWorkers.length}/{workerNodes.length}</strong>
              <p>{workerNodes.length === 0 ? '资源充足时可保持零 Worker' : '用于横向扩展运行资源'}</p>
            </div>
            <div className="node-mode-card">
              <span className="muted">建议路径</span>
              <strong>{controlPlaneNodes.length === 0 ? '先建 Master' : controlPlaneNodes.length === 1 ? '按需扩容' : '维护 HA'}</strong>
              <p>{controlPlaneNodes.length === 0 ? '注册首个 Control Plane' : controlPlaneNodes.length === 1 ? '扩资源加 Worker，提可靠加 Master' : '保持 master endpoint 稳定可达'}</p>
            </div>
          </div>
          <div className="node-mode-flow">
            <div className="node-flow-step active"><strong>1</strong><span>首个 Control Plane</span><small>完整平台部署</small></div>
            <div className={`node-flow-step ${workerNodes.length > 0 ? 'active' : ''}`}><strong>2</strong><span>增加 Worker</span><small>资源容量扩展</small></div>
            <div className={`node-flow-step ${controlPlaneNodes.length > 1 ? 'active' : ''}`}><strong>3</strong><span>增加 Master</span><small>控制面高可用</small></div>
          </div>
          {showNewNodeForm && (
            <div className="panel" style={{ marginBottom: 14, background: 'rgba(239,246,255,0.6)' }}>
              <h4 style={{ margin: '0 0 10px' }}>{newNode.id ? '修改节点' : '注册新节点'}</h4>
              <div className="form-grid">
                <div className="field-block"><span className="field-label required">节点名称</span><input value={newNode.name} placeholder="sgx-node-01" title="填写便于识别的节点名称，如 sgx-node-01" onChange={e => setNewNode(n => ({ ...n, name: e.target.value }))} /><FieldHint>示例：sgx-node-01，建议仅使用字母、数字和短横线。</FieldHint></div>
                <div className="field-block"><span className="field-label">K3s 角色</span><select value={newNode.k3sRole} title="默认一个 Control Plane 即可部署完整平台；资源不足增加 Worker；可靠性要求高时增加 Control Plane 组成 HA" onChange={e => setNewNode(n => ({ ...n, role: e.target.value, k3sRole: e.target.value as ClusterNode['k3sRole'] }))}><option value="control-plane">Control Plane</option><option value="worker">Worker</option></select><FieldHint>默认首个节点选 Control Plane；资源扩容选 Worker；高可靠扩容选 Control Plane。</FieldHint></div>
                <div className="field-block"><span className="field-label">安装模式</span><select value={newNode.provisionMode} title="在线安装需要目标节点可访问 K3s 安装源；离线安装需先导入离线资源包" onChange={e => setNewNode(n => ({ ...n, provisionMode: e.target.value as ClusterNode['provisionMode'], offlineBundleId: e.target.value === 'online' ? '' : n.offlineBundleId }))}><option value="online">在线安装</option><option value="offline">离线资源包</option></select><FieldHint>在线模式依赖外网；离线模式需选择已校验资源包。</FieldHint></div>
                <div className="field-block"><span className="field-label">自动装机</span><select value={newNode.autoProvision ? 'true' : 'false'} title="启用后会通过 SSH 执行真实安装；仅登记凭据只保存节点信息" onChange={e => setNewNode(n => ({ ...n, autoProvision: e.target.value === 'true' }))}><option value="true">启用</option><option value="false">仅登记凭据</option></select><FieldHint>启用后提交即创建自动装机任务。</FieldHint></div>
                <div className="field-block"><span className="field-label required">内网 IP</span><input value={newNode.internalIp} placeholder="10.0.0.11" title="填写节点在集群内通信使用的 IPv4 地址" onChange={e => setNewNode(n => ({ ...n, internalIp: e.target.value }))} /><FieldHint>示例：10.0.0.11，用于 K3s 节点通信。</FieldHint></div>
                <div className="field-block"><span className="field-label">管理 IP</span><input value={newNode.managementIp} placeholder="10.0.0.11" title="填写平台访问该节点的管理网络地址，可与 SSH 地址一致" onChange={e => setNewNode(n => ({ ...n, managementIp: e.target.value }))} /><FieldHint>示例：10.0.0.11，可填写带外管理网地址。</FieldHint></div>
                <div className="field-block"><span className="field-label">目标操作系统</span><select value={newNode.os} title="选择目标节点实际操作系统，用于离线包兼容性校验" onChange={e => setNewNode(n => ({ ...n, os: e.target.value }))}><option value="">-- 请选择 --</option><option value="Ubuntu 22.04">Ubuntu 22.04</option><option value="Ubuntu 20.04">Ubuntu 20.04</option><option value="CentOS 8">CentOS 8</option><option value="Rocky Linux 9">Rocky Linux 9</option><option value="Debian 12">Debian 12</option><option value="openSUSE Leap 15">openSUSE Leap 15</option></select><FieldHint>请与目标节点系统版本保持一致。</FieldHint></div>
                <div className="field-block"><span className="field-label required">SSH 地址</span><input value={newNode.sshHost} placeholder="10.0.0.11" title="填写平台可连通的 SSH 主机名或 IPv4 地址" onChange={e => setNewNode(n => ({ ...n, sshHost: e.target.value }))} /><FieldHint>示例：10.0.0.11，需从平台所在主机可访问。</FieldHint></div>
                <div className="field-block"><span className="field-label">SSH 端口</span><input type="number" min={1} max={65535} value={newNode.sshPort} placeholder="22" title="填写 SSH 端口，默认 22" onChange={e => setNewNode(n => ({ ...n, sshPort: Number(e.target.value) }))} /><FieldHint>默认 22；范围 1-65535。</FieldHint></div>
                <div className="field-block"><span className="field-label">SSH 用户</span><input value={newNode.sshUsername} placeholder="root" title="填写具备安装 K3s、runtime、SGX/DCAP 权限的用户" onChange={e => setNewNode(n => ({ ...n, sshUsername: e.target.value }))} /><FieldHint>示例：root，需具备安装与服务管理权限。</FieldHint></div>
                <div className="field-block"><span className="field-label">SSH 用户密码</span><input type="password" value={newNode.sshPassword} placeholder={newNode.id ? '留空则保持原密码' : '输入 SSH 登录密码'} title="密码仅用于 SSH 执行，保存时会加密并在日志中脱敏" onChange={e => setNewNode(n => ({ ...n, sshPassword: e.target.value }))} /><FieldHint>{newNode.id ? '修改节点时留空会保持已有密码配置状态。' : '用于连接目标节点，日志与审计中会脱敏。'}</FieldHint></div>
                <div className="field-block"><span className="field-label">K3s 版本</span><select value={newNode.installChannel || newNode.version} title="选择要安装的 K3s 版本；为空时使用安装脚本默认版本" onChange={e => setNewNode(n => ({ ...n, version: e.target.value, installChannel: e.target.value }))}><option value="">-- 请选择 --</option>{k3sVersions.map(v => <option key={v} value={v}>{v}</option>)}</select><FieldHint>示例：v1.30.x+k3s1，建议选择稳定版本。</FieldHint></div>
                <div className="field-block"><span className="field-label">SGX/DCAP</span><select value={newNode.enableSgx ? 'true' : 'false'} title="启用后会检查 SGX 硬件、驱动和 DCAP 能力" onChange={e => setNewNode(n => ({ ...n, enableSgx: e.target.value === 'true' }))}><option value="false">不启用</option><option value="true">启用 SGX</option></select><FieldHint>仅 SGX 硬件节点选择启用。</FieldHint></div>
                <div className="field-block"><span className="field-label">离线资源包</span><select value={newNode.offlineBundleId} title="离线安装时选择已上传校验的离线包或内置运行时资源包" onChange={e => setNewNode(n => ({ ...n, offlineBundleId: e.target.value }))} disabled={newNode.provisionMode !== 'offline'}><option value="">-- 请选择 --</option>                  <option value="built-in" disabled={!builtInAvailable}>{builtInAvailable ? '内置运行时资源包' : '内置运行时资源包（不可用）'}</option>{offlineBundles.map(pkg => <option key={pkg.id} value={pkg.id}>{pkg.name} / {pkg.version}</option>)}</select><FieldHint>离线模式必选，内置包在 release tarball 中随平台分发。</FieldHint></div>
                <div className="field-block"><span className="field-label">控制面 Endpoint</span><input value={newNode.controlPlaneEndpoint} onChange={e => setNewNode(n => ({ ...n, controlPlaneEndpoint: e.target.value }))} placeholder="10.0.0.10:6443" title="Worker 或新增 Control Plane 加入已有集群时填写 Control Plane 的 host:port" /><FieldHint>首个 Control Plane 可留空；Worker 和新增 Control Plane 建议填写已有 master 或负载均衡地址。</FieldHint></div>
              </div>
              <div className="action-cell" style={{ marginTop: 12 }}>
                <button className="primary-button" onClick={() => {
                  if (!newNode.name.trim()) { setError('请输入节点名称'); return; }
                  if (!newNode.internalIp.trim()) { setError('请输入内网 IP'); return; }
                  if (!isValidIPv4(newNode.internalIp)) { setError('请输入正确的内网 IPv4 地址'); return; }
                  if (newNode.managementIp.trim() && !isValidIPv4(newNode.managementIp)) { setError('请输入正确的管理 IPv4 地址'); return; }
                  if (newNode.autoProvision && !newNode.sshHost.trim()) { setError('自动装机模式下请输入 SSH 地址'); return; }
                  if (newNode.sshHost.trim() && !isValidHost(newNode.sshHost)) { setError('请输入正确的 SSH 地址'); return; }
                  if (!isValidPort(newNode.sshPort)) { setError('SSH 端口必须是 1 到 65535 之间的整数'); return; }
                  if (newNode.autoProvision && newNode.provisionMode === 'offline' && !newNode.offlineBundleId) { setError('离线安装模式下请选择离线资源包'); return; }
                  const original = newNode.id ? clusterNodes.find(node => node.id === newNode.id) : undefined;
                  const payload = original ? toClusterNodeUpdatePayload(newNode, original) : toClusterNodePayload(newNode);
                  const action = newNode.id ? () => api.updateClusterNode(newNode.id, payload) : () => api.createClusterNode(payload);
                  void withSaving(action, newNode.id ? '节点信息已更新' : '节点注册与自动装机请求已提交').then(ok => ok && setShowNewNodeForm(false));
                }} disabled={saving || submitting || dataLoading}>{newNode.id ? '保存修改' : '提交'}</button>
                <button className="secondary-button" onClick={() => { setShowNewNodeForm(false); setNewNode(initialNode); }}>取消</button>
              </div>
            </div>
          )}
          {clusterNodes.length === 0 ? (
            <p className="muted" style={{ textAlign: 'center', padding: '24px 0' }}>暂无数据</p>
          ) : (
            <div className="table-wrap">
              <table>
                <thead><tr><th>节点</th><th>角色</th><th>状态</th><th>装机阶段</th><th>版本</th><th>IP</th><th>负载</th><th>Pod</th><th>心跳</th><th>操作</th></tr></thead>
                <tbody>
                  {clusterNodes.map(node => {
                    const task = taskForNode(node);
                    const progress = provisioningProgress(task);
                    return (
                      <tr key={node.id}>
                        <td><strong>{node.name}</strong><br /><span className="muted" style={{ fontSize: 11 }}>{node.sshPasswordConfigured ? 'SSH 已配置' : 'SSH 未配置'}</span></td>
                        <td><span className="badge badge-info" style={{ fontSize: 11 }}>{node.k3sRole || node.role}</span><br /><span className="node-role-hint">{nodeRoleLabel(node)}</span></td>
                        <td><span className={`badge ${statusBadgeClass(node.status)}`}>{node.status === 'unreachable' ? '不可达' : node.status}</span></td>
                        <td><div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}><span className={`badge ${statusBadgeClass(task?.status || node.provisionStatus || '')}`} style={{ fontSize: 11 }}>{task ? `${taskStatusLabels[task.status] || task.status} · ${stepLabels[task.currentStep] || task.currentStep || '无步骤'}` : (node.provisionStatus || '未装机')}</span>{task && <span className="muted" style={{ fontSize: 11 }}>{progress}% · SGX {node.sgxStatus || '-'}</span>}</div></td>
                        <td className="muted">{node.version || '-'}</td>
                        <td className="muted">{node.managementIp || node.internalIp || '-'}</td>
                        <td><div className="node-load-stats"><span className="badge badge-info" style={{ fontSize: 11 }}>CPU {node.cpuUsage.toFixed(1)}%</span><span className="badge badge-primary" style={{ fontSize: 11 }}>MEM {node.memoryUsage.toFixed(1)}%</span></div></td>
                        <td>{node.podCount}</td>
                        <td className="muted" style={{ fontSize: 12 }}>{node.lastHeartbeat ? formatTime(node.lastHeartbeat) : '-'}</td>
                        <td><div style={{ display: 'flex', gap: 4, flexWrap: 'wrap' }}><button className="secondary-button" style={{ minHeight: 30, padding: '3px 8px', fontSize: 11 }} disabled={submitting || dataLoading} onClick={() => setDetailNodeId(node.id)}>查看详情</button><button className="secondary-button" style={{ minHeight: 30, padding: '3px 8px', fontSize: 11 }} disabled={submitting || dataLoading} onClick={() => startEditNode(node)}>修改</button>{task && (task.status === 'failed' || task.status === 'cancelled') && <button className="secondary-button" style={{ minHeight: 30, padding: '3px 8px', fontSize: 11 }} disabled={submitting || dataLoading} onClick={() => { void handleSave(() => api.retryProvisioningTask(task.id), '自动装机任务已提交重试'); }}>重试</button>}{task && task.status !== 'succeeded' && task.status !== 'cancelled' && <button className="secondary-button" style={{ minHeight: 30, padding: '3px 8px', fontSize: 11 }} disabled={submitting || dataLoading} onClick={() => { void handleSave(() => api.cancelProvisioningTask(task.id), '自动装机任务已取消'); }}>取消</button>}<button className="danger-button" style={{ minHeight: 30, padding: '3px 8px', fontSize: 11 }} disabled={submitting || dataLoading} onClick={() => handleDelete(() => api.deleteClusterNode(node.id), '节点已删除')}>删除</button></div></td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          )}
        </div>
      )}

      {selectedDetailNode && (
        <div className="modal-backdrop" onClick={() => setDetailNodeId(null)}>
          <div className="node-detail-modal" onClick={e => e.stopPropagation()}>
            <div className="modal-head">
              <div>
                <h4>{selectedDetailNode.name}</h4>
                <p className="muted">节点 ID：{selectedDetailNode.id}</p>
              </div>
              <button type="button" className="modal-close" onClick={() => setDetailNodeId(null)}>×</button>
            </div>
            <div className="node-detail-grid">
              {detailItem('角色', selectedDetailNode.k3sRole || selectedDetailNode.role)}
              {detailItem('节点定位', nodeRoleLabel(selectedDetailNode))}
              {detailItem('拓扑角色', selectedDetailNode.k3sRole === 'control-plane' || selectedDetailNode.role === 'control-plane' ? (controlPlaneNodes.length > 1 ? '控制面高可用成员' : '单 Master 控制面') : '资源扩容节点')}
              {detailItem('状态', selectedDetailNode.status === 'unreachable' ? '不可达' : selectedDetailNode.status)}
              {detailItem('K3s 版本', selectedDetailNode.version)}
              {detailItem('操作系统', selectedDetailNode.os)}
              {detailItem('架构', selectedDetailNode.arch)}
              {detailItem('内核', selectedDetailNode.kernel)}
              {detailItem('容器运行时', selectedDetailNode.containerRuntime)}
              {detailItem('RuntimeClass', selectedDetailNode.runtimeClass)}
              {detailItem('内网 IP', selectedDetailNode.internalIp)}
              {detailItem('管理 IP', selectedDetailNode.managementIp)}
              {detailItem('SSH 地址', selectedDetailNode.sshHost ? `${selectedDetailNode.sshUsername}@${selectedDetailNode.sshHost}:${selectedDetailNode.sshPort}` : '未配置')}
              {detailItem('SSH 凭据', selectedDetailNode.sshPasswordConfigured ? '已配置' : '未配置')}
              {detailItem('安装模式', selectedDetailNode.provisionMode)}
              {detailItem('自动装机', selectedDetailNode.autoProvision ? '启用' : '关闭')}
              {detailItem('安装包', selectedDetailNode.offlineBundleId)}
              {detailItem('控制面 Endpoint', selectedDetailNode.controlPlaneEndpoint)}
              {detailItem('Join 模式', selectedDetailNode.joinMode)}
              {detailItem('Join 状态', selectedDetailNode.joinStatus)}
              {detailItem('装机状态', selectedDetailTask ? `${taskStatusLabels[selectedDetailTask.status] || selectedDetailTask.status} · ${provisioningProgress(selectedDetailTask)}%` : selectedDetailNode.provisionStatus)}
              {detailItem('当前步骤', selectedDetailTask ? (stepLabels[selectedDetailTask.currentStep] || selectedDetailTask.currentStep || '无步骤') : '-')}
              {detailItem('运行时状态', selectedDetailNode.runtimeStatus)}
              {detailItem('SGX 状态', selectedDetailNode.sgxStatus)}
              {detailItem('CPU 容量', selectedDetailNode.capacityCpu)}
              {detailItem('内存容量', selectedDetailNode.capacityMemory)}
              {detailItem('磁盘容量', selectedDetailNode.diskCapacity)}
              {detailItem('CPU 使用率', `${selectedDetailNode.cpuUsage.toFixed(1)}%`)}
              {detailItem('内存使用率', `${selectedDetailNode.memoryUsage.toFixed(1)}%`)}
              {detailItem('磁盘使用率', `${selectedDetailNode.diskUsage.toFixed(1)}%`)}
              {detailItem('Pod 数量', selectedDetailNode.podCount)}
              {detailItem('网卡', selectedDetailNode.nicName)}
              {detailItem('接收速率', `${selectedDetailNode.rxRate.toFixed(1)} MB/s`)}
              {detailItem('发送速率', `${selectedDetailNode.txRate.toFixed(1)} MB/s`)}
              {detailItem('接收流量', `${selectedDetailNode.rxBytes} bytes`)}
              {detailItem('发送流量', `${selectedDetailNode.txBytes} bytes`)}
              {detailItem('加入时间', selectedDetailNode.joinedAt ? formatTime(selectedDetailNode.joinedAt) : '-')}
              {detailItem('最后心跳', selectedDetailNode.lastHeartbeat ? formatTime(selectedDetailNode.lastHeartbeat) : '-')}
              {detailItem('最后尝试', selectedDetailNode.lastJoinAttemptAt ? formatTime(selectedDetailNode.lastJoinAttemptAt) : '-')}
              {detailItem('最后消息', selectedDetailTask?.message || selectedDetailNode.lastJoinMessage)}
            </div>
            <div className="node-detail-section">
              <strong>标签</strong>
              <div className="node-detail-tags">{selectedDetailNode.labels.length > 0 ? selectedDetailNode.labels.map(label => <span key={label} className="badge badge-info">{label}</span>) : <span className="muted">-</span>}</div>
            </div>
            <div className="node-detail-section">
              <strong>污点</strong>
              <div className="node-detail-tags">{selectedDetailNode.taints.length > 0 ? selectedDetailNode.taints.map(taint => <span key={taint} className="badge badge-warning">{taint}</span>) : <span className="muted">-</span>}</div>
            </div>
            <div className="node-detail-section">
              <strong>Join 命令</strong>
              <pre className="node-detail-pre">{selectedDetailNode.joinCommand || '-'}</pre>
            </div>
            {selectedDetailTask && (
              <div className="node-detail-section">
                <strong>装机步骤</strong>
                <div className="node-task-list">
                  {selectedDetailTask.steps.map(step => <div key={step.name} className="detail-card"><div className="section-head" style={{ marginBottom: 4 }}><strong>{stepLabels[step.name] || step.name}</strong><span className={`badge ${statusBadgeClass(step.status)}`}>{step.status}</span></div><p className="muted" style={{ margin: 0, fontSize: 12 }}>{step.message || '等待执行'}{step.finishedAt ? ` · ${formatTime(step.finishedAt)}` : ''}</p>{step.evidence && <pre className="node-detail-pre">{step.evidence}</pre>}</div>)}
                </div>
              </div>
            )}
            <div className="action-cell password-modal-actions">
              <button className="secondary-button" onClick={() => startEditNode(selectedDetailNode)}>修改</button>
              <button className="primary-button" onClick={() => setDetailNodeId(null)}>关闭</button>
            </div>
          </div>
        </div>
      )}

      {/* Images */}
      {activeSubTab === 'images' && (
        <div className="panel">
          <div className="section-head" style={{ marginBottom: 12 }}><h3 style={{ margin: 0, fontSize: 16 }}>组件镜像管理 ({images.length})</h3><div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}><button className="primary-button" onClick={() => { setImageBuild(initialImageBuild); setImageBuildLog(''); setShowImageBuildForm(true); setShowNewImageForm(false); }}>构建镜像</button><button className="secondary-button" onClick={() => { setNewImage(initialImage); setShowNewImageForm(true); setShowImageBuildForm(false); }}>导入已有镜像</button></div></div>
          <div className="image-build-guide"><strong>组件镜像管理负责安全组件镜像生命周期</strong><span>“构建镜像”用于从源码或制品包登记构建任务；“导入已有镜像”用于登记已经存在于 Registry 的镜像。安全组件部署从这里选择镜像 ID，保存后点击组件行的“部署”按钮即可下发到集群节点运行。</span></div>
          {showImageBuildForm && <div className="panel" style={{ marginBottom: 14, background: 'rgba(239,246,255,0.6)' }}><h4 style={{ margin: '0 0 10px' }}>构建安全组件镜像</h4><div className="form-grid"><div className="field-block"><span className="field-label required">镜像名称</span><input value={imageBuild.name} placeholder="secure-waf" title="填写构建后的镜像名称" onChange={e => setImageBuild(v => ({ ...v, name: e.target.value }))} /><FieldHint>示例：secure-waf，对应一个安全组件镜像。</FieldHint></div><div className="field-block"><span className="field-label required">安全组件源码/制品包</span><input value={imageBuild.sourcePackage} placeholder="上传或填写 package path" title="上传源码包或填写已经上传的制品包路径" onChange={e => setImageBuild(v => ({ ...v, sourcePackage: e.target.value }))} /><FieldHint>可先用下方文件选择上传源码包、Jar、二进制包或构建上下文压缩包。</FieldHint></div><div className="field-block"><span className="field-label">上传构建包</span><input type="file" title="上传安全组件源码或制品包，最大 100MB" onChange={async e => { const f = e.target.files?.[0]; if (!f) return; const r = await handleFileUpload(f, 'images'); if (r) setImageBuild(v => ({ ...v, sourcePackage: r.path, name: v.name || r.name.replace(/\.[^.]+$/, '') })); }} /><FieldHint>上传成功后自动回填构建包路径。</FieldHint></div><div className="field-block"><span className="field-label">Dockerfile 路径</span><input value={imageBuild.dockerfilePath} placeholder="Dockerfile" title="填写构建上下文内 Dockerfile 的相对路径" onChange={e => setImageBuild(v => ({ ...v, dockerfilePath: e.target.value }))} /><FieldHint>默认 Dockerfile，可填写 docker/Dockerfile。</FieldHint></div><div className="field-block"><span className="field-label required">Registry</span><input value={imageBuild.registry} placeholder="registry.example.com" title="填写目标镜像仓库" onChange={e => setImageBuild(v => ({ ...v, registry: e.target.value }))} /><FieldHint>示例：registry.example.com 或 10.0.0.20:5000。</FieldHint></div><div className="field-block"><span className="field-label required">Repository</span><input value={imageBuild.repository} placeholder="security/secure-waf" title="填写目标镜像仓库路径" onChange={e => setImageBuild(v => ({ ...v, repository: e.target.value }))} /><FieldHint>示例：security/secure-waf。</FieldHint></div><div className="field-block"><span className="field-label required">Tag</span><input value={imageBuild.tag} placeholder="1.0.0" title="填写目标镜像标签" onChange={e => setImageBuild(v => ({ ...v, tag: e.target.value }))} /><FieldHint>示例：1.0.0、latest。</FieldHint></div><div className="field-block"><span className="field-label">构建参数</span><input value={imageBuild.buildArgs} placeholder="HTTP_PROXY=...;FEATURE=sgx" title="填写可选构建参数" onChange={e => setImageBuild(v => ({ ...v, buildArgs: e.target.value }))} /><FieldHint>用于传入 build args，多个参数可用分号分隔。</FieldHint></div><div className="field-block"><label style={{ display: 'flex', alignItems: 'center', gap: 8, cursor: 'pointer', paddingTop: 6 }}><input className="row-select-checkbox" type="checkbox" checked={imageBuild.enableSignature} onChange={e => setImageBuild(v => ({ ...v, enableSignature: e.target.checked }))} /><span className="field-label" style={{ margin: 0 }}>生成镜像签名</span></label><FieldHint>构建完成后标记为已签名，供合规检查使用。</FieldHint></div><div className="field-block"><label style={{ display: 'flex', alignItems: 'center', gap: 8, cursor: 'pointer', paddingTop: 6 }}><input className="row-select-checkbox" type="checkbox" checked={imageBuild.generateSbom} onChange={e => setImageBuild(v => ({ ...v, generateSbom: e.target.checked }))} /><span className="field-label" style={{ margin: 0 }}>生成 SBOM</span></label><FieldHint>生成软件物料清单，支撑资产与合规审计。</FieldHint></div><div className="field-block"><label style={{ display: 'flex', alignItems: 'center', gap: 8, cursor: 'pointer', paddingTop: 6 }}><input className="row-select-checkbox" type="checkbox" checked={imageBuild.enableSgxRuntime} onChange={e => setImageBuild(v => ({ ...v, enableSgxRuntime: e.target.checked }))} /><span className="field-label" style={{ margin: 0 }}>SGX 运行时适配</span></label><FieldHint>安全组件需要可信执行环境时勾选。</FieldHint></div></div><div className="action-cell" style={{ marginTop: 12 }}><button className="primary-button" onClick={submitImageBuild} disabled={saving || submitting || dataLoading}>提交构建</button><button className="secondary-button" onClick={() => setShowImageBuildForm(false)}>取消</button></div></div>}
          {imageBuildLog && <pre className="image-build-log">{imageBuildLog}</pre>}
          {showNewImageForm && <div className="panel" style={{ marginBottom: 14, background: 'rgba(239,246,255,0.6)' }}><div className="form-grid"><div className="field-block"><span className="field-label required">名称</span><input value={newImage.name} placeholder="nginx-enclave" title="填写已有镜像的资产名称" onChange={e => setNewImage(v => ({ ...v, name: e.target.value }))} /><FieldHint>示例：nginx-enclave。</FieldHint></div><div className="field-block"><span className="field-label required">Registry</span><input value={newImage.registry} placeholder="registry.example.com" title="填写镜像仓库域名，可含端口" onChange={e => setNewImage(v => ({ ...v, registry: e.target.value }))} /><FieldHint>示例：registry.example.com 或 10.0.0.20:5000。</FieldHint></div><div className="field-block"><span className="field-label required">Repository</span><input value={newImage.repository} placeholder="secure/nginx" title="填写仓库内镜像路径" onChange={e => setNewImage(v => ({ ...v, repository: e.target.value }))} /><FieldHint>示例：secure/nginx。</FieldHint></div><div className="field-block"><span className="field-label required">Tag</span><input value={newImage.tag} placeholder="1.0.0" title="填写镜像标签" onChange={e => setNewImage(v => ({ ...v, tag: e.target.value }))} /><FieldHint>示例：latest、1.0.0。</FieldHint></div></div><div className="action-cell" style={{ marginTop: 12 }}><button className="primary-button" onClick={() => { if (!newImage.name.trim()) { setError('请输入名称'); return; } if (!newImage.registry.trim()) { setError('请输入 Registry'); return; } if (!newImage.repository.trim()) { setError('请输入 Repository'); return; } if (!newImage.tag.trim()) { setError('请输入 Tag'); return; } const isEdit = Boolean(newImage.id); const original = images.find(image => image.id === newImage.id); const payload = toImagePayload(newImage, original); const action = isEdit ? () => api.updateImage(newImage.id, payload) : () => api.createImage(payload); withSaving(action, isEdit ? '镜像已更新' : '镜像已导入').then(ok => ok && setShowNewImageForm(false)); }} disabled={saving || submitting || dataLoading}>保存</button><button className="secondary-button" onClick={() => setShowNewImageForm(false)}>取消</button></div></div>}
          {images.length > 0 && <BulkDeleteControls total={images.length} selected={selectedImages} setSelected={setSelectedImages} ids={images.map(img => img.id)} label="镜像" onDelete={() => runBulkDelete(Array.from(selectedImages), api.deleteImage, '镜像', () => setSelectedImages(new Set()))} />}
          {images.length === 0 ? <p className="muted" style={{ textAlign: 'center', padding: '24px 0' }}>暂无数据</p> : (
            <div className="table-wrap"><table><thead><tr><th className="row-select-cell"></th><th>镜像 ID</th><th>镜像</th><th>Registry</th><th>Tag</th><th>签名</th><th>SBOM</th><th>漏洞</th><th>SGX</th><th>操作</th></tr></thead><tbody>
              {images.map(img => <tr key={img.id}><td className="row-select-cell"><input className={rowCheckboxClassName} type="checkbox" checked={selectedImages.has(img.id)} onChange={() => toggleSelected(setSelectedImages, img.id)} /></td><td><code className="inline-code">{img.id}</code></td><td><strong>{img.name}</strong><br /><span className="muted" style={{ fontSize: 11 }}>{img.repository}</span></td><td className="muted">{img.registry}</td><td>{img.tag}</td><td><span className={`badge ${img.signed ? 'badge-success' : 'badge-danger'}`}>{img.signed ? '已签名' : '未签名'}</span></td><td><span className={`badge ${img.sbom ? 'badge-success' : 'badge-danger'}`}>{img.sbom ? '有' : '无'}</span></td><td><span className={`badge ${img.vulnerability === 'high' ? 'badge-danger' : img.vulnerability === 'medium' ? 'badge-warning' : 'badge-success'}`}>{img.vulnerability}</span></td><td><span className={`badge ${img.enclaveReady ? 'badge-success' : 'badge-info'}`}>{img.enclaveReady ? '就绪' : '未适配'}</span></td><td><div style={{ display: 'flex', gap: 4, flexWrap: 'wrap' }}><button className="secondary-button" style={{ minHeight: 30, padding: '3px 8px', fontSize: 11 }} disabled={submitting || dataLoading} onClick={() => startEditImage(img)}>编辑</button><button className="danger-button" style={{ minHeight: 30, padding: '3px 8px', fontSize: 11 }} disabled={submitting || dataLoading} onClick={() => handleDelete(() => api.deleteImage(img.id), '镜像已删除')}>删除</button></div></td></tr>)}
            </tbody></table></div>
          )}
        </div>
      )}

      {/* Components */}
      {activeSubTab === 'components' && (
        <div className="panel">
          <div className="section-head" style={{ marginBottom: 12 }}><h3 style={{ margin: 0, fontSize: 16 }}>安全组件部署 ({components.length})</h3><button className="primary-button" onClick={() => { setNewComponent(initialComponent); setShowNewComponentForm(true); }}>新增组件</button></div>
          <div className="image-build-guide"><strong>组件运行流程</strong><span>先在组件镜像管理中导入或构建镜像，再在安全组件部署中选择镜像并保存。保存后组件处于 draft 状态，点击操作列“部署”会生成 Kubernetes Manifest 并提交到集群，调度器会选择可用节点运行组件副本。</span></div>
          {selectedComponents.size > 0 && (
            <div className="panel" style={{ marginBottom: 12, background: '#f0f4ff', display: 'flex', alignItems: 'center', gap: 8, padding: '8px 16px' }}>
              <span style={{ fontSize: 13, fontWeight: 600 }}>已选 {selectedComponents.size} 项</span>
              <button className="primary-button" style={{ minHeight: 30, padding: '3px 12px', fontSize: 12 }} disabled={submitting || dataLoading} onClick={() => handleSave(async () => {
                const ids = Array.from(selectedComponents);
                const res = await api.batchDeployComponents(ids);
                const failed = res.filter(r => !r.success);
                if (failed.length > 0) setError(`批量部署部分失败: ${failed.map(f => f.name).join(', ')}`);
                else setSuccess(`已提交 ${res.length} 个组件部署`);
                setSelectedComponents(new Set());
              }, '批量部署已提交')}>批量部署</button>
              <button className="secondary-button" style={{ minHeight: 30, padding: '3px 12px', fontSize: 12 }} disabled={submitting || dataLoading} onClick={() => {
                const replicas = Number(window.prompt('请输入目标副本数', '2'));
                if (replicas > 0) handleSave(async () => {
                  const ids = Array.from(selectedComponents);
                  const res = await api.batchScaleComponents(ids, replicas);
                  const failed = res.filter(r => !r.success);
                  if (failed.length > 0) setError(`批量扩缩容部分失败: ${failed.map(f => f.name).join(', ')}`);
                  else setSuccess(`已提交 ${res.length} 个组件扩缩容至 ${replicas} 副本`);
                  setSelectedComponents(new Set());
                }, '批量扩缩容已提交');
              }}>批量扩缩容</button>
              <button className="secondary-button" style={{ minHeight: 30, padding: '3px 12px', fontSize: 12 }} disabled={submitting || dataLoading} onClick={() => setSelectedComponents(new Set())}>取消选择</button>
            </div>
          )}
          {showNewComponentForm && <div className="panel" style={{ marginBottom: 14, background: 'rgba(239,246,255,0.6)' }}><div className="form-grid"><div className="field-block"><span className="field-label required">名称</span><input value={newComponent.name} placeholder="secure-api" title="填写组件名称" onChange={e => setNewComponent(v => ({ ...v, name: e.target.value }))} /><FieldHint>示例：secure-api。</FieldHint></div><div className="field-block"><span className="field-label required">镜像</span><select value={newComponent.image} title="从组件镜像管理中选择已导入或已构建的镜像" onChange={e => setNewComponent(v => ({ ...v, image: e.target.value }))}><option value="">-- 请选择镜像 --</option>{images.map(image => <option key={image.id} value={image.id}>{imageLabel(image)}</option>)}</select><FieldHint>镜像 ID 来自组件镜像管理列表；先构建镜像或导入已有镜像，再创建安全组件部署。</FieldHint></div><div className="field-block"><span className="field-label required">版本</span><input value={newComponent.version} placeholder="1.0.0" title="填写组件版本号" onChange={e => setNewComponent(v => ({ ...v, version: e.target.value }))} /><FieldHint>示例：1.0.0。</FieldHint></div><div className="field-block"><span className="field-label">命名空间</span><input value={newComponent.namespace} placeholder="default" title="填写 Kubernetes 命名空间" onChange={e => setNewComponent(v => ({ ...v, namespace: e.target.value }))} /><FieldHint>留空时由后端或默认部署逻辑处理。</FieldHint></div><div className="field-block"><span className="field-label">副本数</span><input type="number" min={1} value={newComponent.replicas} placeholder="1" title="填写组件副本数" onChange={e => setNewComponent(v => ({ ...v, replicas: Math.max(1, Number(e.target.value) || 1) }))} /><FieldHint>示例：1，部署后也可通过扩缩容调整。</FieldHint></div><div className="field-block"><span className="field-label">隔离级别</span><select value={newComponent.isolation} title="创建飞地配置前，组件隔离级别需选择 enclave" onChange={e => setNewComponent(v => ({ ...v, isolation: e.target.value as ComponentDefinition['isolation'] }))}><option value="standard">standard</option><option value="hardened">hardened</option><option value="enclave">enclave</option></select><FieldHint>需要 SGX 飞地配置时请选择 enclave。</FieldHint></div><div className="field-block"><label style={{ display: 'flex', alignItems: 'center', gap: 8, cursor: 'pointer', paddingTop: 6 }}><input className="row-select-checkbox" type="checkbox" checked={newComponent.mtlsEnabled} onChange={e => setNewComponent(v => ({ ...v, mtlsEnabled: e.target.checked }))} /><span className="field-label" style={{ margin: 0 }}>启用 mTLS</span></label><FieldHint>勾选后，组件部署时注入 linkerd.io/inject: enabled 注解，由服务网格接管 mTLS 通信加密。</FieldHint></div><div className="field-block"><span className="field-label">资源包</span><input type="file" title="上传组件部署资源包，最大 100MB" onChange={async e => { const f = e.target.files?.[0]; if (!f) return; const r = await handleFileUpload(f, 'components'); if (r) setNewComponent(v => ({ ...v, packagePath: r.path, packageSize: r.size })); }} /><FieldHint>支持组件资源包，最大 100MB。</FieldHint></div></div><div className="action-cell" style={{ marginTop: 12 }}><button className="primary-button" onClick={() => { if (!newComponent.name.trim()) { setError('请输入组件名称'); return; } if (!newComponent.image) { setError('请选择镜像'); return; } if (!newComponent.version.trim()) { setError('请输入版本'); return; } if (!Number.isInteger(newComponent.replicas) || newComponent.replicas < 1) { setError('副本数必须为大于 0 的整数'); return; } const isEdit = Boolean(newComponent.id); const original = components.find(comp => comp.id === newComponent.id); const payload = toComponentPayload(newComponent, original); const action = isEdit ? () => api.updateComponent(newComponent.id, payload) : () => api.createComponent(payload); withSaving(action, isEdit ? '组件已更新' : '组件已保存').then(ok => ok && setShowNewComponentForm(false)); }} disabled={saving || submitting || dataLoading}>{newComponent.id ? '更新' : '保存'}</button><button className="secondary-button" onClick={() => setShowNewComponentForm(false)}>取消</button></div></div>}
          {components.length === 0 ? <p className="muted" style={{ textAlign: 'center', padding: '24px 0' }}>暂无数据</p> : (
            <div className="table-wrap"><table><thead><tr><th className="row-select-cell"><input className={rowCheckboxClassName} type="checkbox" checked={components.length > 0 && selectedComponents.size === components.length} onChange={e => { if (e.target.checked) setSelectedComponents(new Set(components.map(c => c.id))); else setSelectedComponents(new Set()); }} title="全选/取消全选" /></th><th>组件</th><th>镜像</th><th>隔离</th><th>副本</th><th>状态</th><th>操作</th></tr></thead><tbody>
              {components.map(comp => {
                const checked = selectedComponents.has(comp.id);
                const toggleSelect = () => {
                  const next = new Set(selectedComponents);
                  if (checked) next.delete(comp.id); else next.add(comp.id);
                  setSelectedComponents(next);
                };
                return <tr key={comp.id}><td className="row-select-cell"><input className={rowCheckboxClassName} type="checkbox" checked={checked} onChange={toggleSelect} /></td><td><strong>{comp.name}</strong><br /><span className="muted" style={{ fontSize: 11 }}>v{comp.version} / {comp.namespace}</span></td><td className="muted">{comp.image}</td><td><span className="badge badge-info">{comp.isolation}</span></td><td>{comp.replicas}</td><td><span className={`badge ${comp.status === 'deployed' ? 'badge-success' : comp.status === 'degraded' || comp.status === 'deployment_pending' ? 'badge-warning' : 'badge-info'}`}>{comp.status}</span></td><td><div style={{ display: 'flex', gap: 4, flexWrap: 'wrap' }}>{comp.status === 'draft' && <button className="primary-button" style={{ minHeight: 30, padding: '3px 8px', fontSize: 11 }} disabled={submitting || dataLoading} onClick={() => handleSave(() => api.deployComponent(comp.id), '部署请求已提交，请查看组件状态和运行日志确认结果')}>部署</button>}<button className="secondary-button" style={{ minHeight: 30, padding: '3px 8px', fontSize: 11 }} disabled={submitting || dataLoading} onClick={() => { const replicas = Number(window.prompt('请输入副本数，示例：2，必须为大于 0 的整数', String(comp.replicas))); if (replicas > 0) void handleSave(() => api.scaleComponent(comp.id, replicas), '扩缩容请求已提交，请查看组件状态和运行日志确认结果'); }}>扩缩容</button><button className="secondary-button" style={{ minHeight: 30, padding: '3px 8px', fontSize: 11 }} disabled={submitting || dataLoading} onClick={() => { const version = window.prompt('请输入目标版本，示例：1.0.1', comp.version); if (version) void handleSave(() => api.upgradeComponent(comp.id, version), '升级请求已提交，请查看组件状态和运行日志确认结果'); }}>升级</button><button className="secondary-button" style={{ minHeight: 30, padding: '3px 8px', fontSize: 11 }} disabled={submitting || dataLoading} onClick={() => startEditComponent(comp)}>编辑</button><button className="secondary-button" style={{ minHeight: 30, padding: '3px 8px', fontSize: 11 }} disabled={submitting || dataLoading} onClick={() => handleSave(async () => { const res = await api.getManifest(comp.id); setManifestContent(res.manifest); }, 'Manifest 已生成')}>Manifest</button><button className="danger-button" style={{ minHeight: 30, padding: '3px 8px', fontSize: 11 }} disabled={submitting || dataLoading} onClick={() => handleDelete(() => api.deleteComponent(comp.id), '组件已删除')}>删除</button></div></td></tr>;
              })}
            </tbody></table></div>
          )}
        </div>
      )}

      {/* Packages */}
      {activeSubTab === 'packages' && (
        <div className="panel">
          <div className="section-head" style={{ marginBottom: 12 }}><h3 style={{ margin: 0, fontSize: 16 }}>安装包管理 ({packages.length})</h3><button className="primary-button" onClick={() => { setNewPackage(initialPackage); setPackageUploading(false); setPackageUploadProgress(0); setShowNewPackageForm(true); }}>导入安装包</button></div>
          {showNewPackageForm && <div className="panel" style={{ marginBottom: 14, background: 'rgba(239,246,255,0.6)' }}><div className="form-grid"><div className="field-block"><span className="field-label required">名称</span><input value={newPackage.name} placeholder="k3s-offline-bundle" title="填写安装包名称" onChange={e => setNewPackage(v => ({ ...v, name: e.target.value }))} /><FieldHint>示例：k3s-offline-bundle。</FieldHint></div><div className="field-block"><span className="field-label required">版本</span><input value={newPackage.version} placeholder="v1.30.4+k3s1" title="填写安装包对应版本" onChange={e => setNewPackage(v => ({ ...v, version: e.target.value }))} /><FieldHint>示例：v1.30.4+k3s1。</FieldHint></div><div className="field-block"><span className="field-label">模式</span><select value={newPackage.mode} title="离线自动装机请选择离线包" onChange={e => setNewPackage(v => ({ ...v, mode: e.target.value as InstallPackage['mode'] }))}><option value="iso">ISO</option><option value="incremental">增量包</option><option value="offline_bundle">离线包</option></select><FieldHint>用于节点离线装机时选择“离线包”。</FieldHint></div><div className="field-block"><span className="field-label">文件</span><input type="file" title="上传 ISO、增量包或离线包，安装包最大 2GB" onChange={async e => { const f = e.target.files?.[0]; if (!f) return; setPackageUploading(true); setPackageUploadProgress(0); try { const r = await api.uploadFile(f, 'packages', setPackageUploadProgress); if (r) { setNewPackage(v => ({ ...v, filePath: r.path, fileSize: r.size, name: v.name || r.name.replace(/\.[^.]+$/, '') })); setSuccess(`安装包已上传: ${r.path}`); } } catch (err) { setError((err as Error).message); } finally { setPackageUploading(false); } }} /><FieldHint>{packageUploading ? (packageUploadProgress >= 100 ? '正在保存文件，请稍候...' : `正在上传 ${packageUploadProgress}%`) : newPackage.filePath ? `已上传: ${newPackage.filePath}` : '离线包需包含 manifest.json 和 install.sh，最大 2GB。'}</FieldHint>{packageUploading && <div className="package-upload-progress"><div className="package-upload-progress-bar" style={{ width: `${packageUploadProgress}%` }} /><span>{packageUploadProgress >= 100 ? '保存中' : `${packageUploadProgress}%`}</span></div>}</div></div><div className="action-cell" style={{ marginTop: 12 }}><button className="primary-button" disabled={packageUploading || saving || dataLoading || !newPackage.filePath} onClick={() => { if (!newPackage.name.trim()) { setError('请输入名称'); return; } if (!newPackage.version.trim()) { setError('请输入版本'); return; } const isEdit = Boolean(newPackage.id); const original = packages.find(pkg => pkg.id === newPackage.id); const payload = toInstallPackagePayload(newPackage, original); const action = isEdit ? () => api.updateInstallPackage(newPackage.id, payload) : () => api.createInstallPackage(payload); withSaving(action, isEdit ? '安装包已更新' : '安装包已保存').then(ok => ok && setShowNewPackageForm(false)); }}>{packageUploading ? '上传中...' : '保存'}</button><button className="secondary-button" onClick={() => setShowNewPackageForm(false)}>取消</button></div></div>}
          {packages.length === 0 ? <p className="muted" style={{ textAlign: 'center', padding: '24px 0' }}>暂无数据</p> : (
            <div className="table-wrap"><table><thead><tr><th>名称</th><th>模式</th><th>版本</th><th>签名</th><th>离线</th><th>导入时间</th><th>操作</th></tr></thead><tbody>
              {packages.map(pkg => <tr key={pkg.id}><td><strong>{pkg.name}</strong></td><td><span className="badge badge-primary">{pkg.mode}</span></td><td>{pkg.version}</td><td><span className={`badge ${pkg.signed ? 'badge-success' : 'badge-danger'}`}>{pkg.signed ? '已签名' : '未签名'}</span></td><td><span className={`badge ${pkg.offline ? 'badge-info' : 'badge-success'}`}>{pkg.offline ? '是' : '否'}</span></td><td className="muted">{formatTime(pkg.importedAt)}</td><td><div style={{ display: 'flex', gap: 4, flexWrap: 'wrap' }}><button className="secondary-button" style={{ minHeight: 30, padding: '3px 8px', fontSize: 11 }} disabled={submitting || dataLoading} onClick={() => startEditPackage(pkg)}>编辑</button><button className="danger-button" style={{ minHeight: 30, padding: '3px 8px', fontSize: 11 }} disabled={submitting || dataLoading} onClick={() => handleDelete(() => api.deleteInstallPackage(pkg.id), '安装包已删除')}>删除</button></div></td></tr>)}
            </tbody></table></div>
          )}
        </div>
      )}

      {/* App Market */}
      {activeSubTab === 'app-market' && (
        <div className="panel">
          <div className="section-head" style={{ marginBottom: 12 }}><h3 style={{ margin: 0, fontSize: 16 }}>应用市场 ({marketApps.length})</h3><button className="primary-button" onClick={() => { setNewMarketplaceApp(initialMarketplaceApp); setShowNewMarketplaceAppForm(true); }}>新增应用</button></div>
          {showNewMarketplaceAppForm && <div className="panel" style={{ marginBottom: 14, background: 'rgba(239,246,255,0.6)' }}><div className="form-grid"><div className="field-block"><span className="field-label required">名称</span><input value={newMarketplaceApp.name} placeholder="secure-redis" title="填写应用名称" onChange={e => setNewMarketplaceApp(v => ({ ...v, name: e.target.value }))} /><FieldHint>示例：secure-redis。</FieldHint></div><div className="field-block"><span className="field-label required">分类</span><input value={newMarketplaceApp.category} placeholder="database" title="填写应用分类" onChange={e => setNewMarketplaceApp(v => ({ ...v, category: e.target.value }))} /><FieldHint>示例：database、security、observability。</FieldHint></div><div className="field-block"><span className="field-label required">版本</span><input value={newMarketplaceApp.currentVersion} placeholder="1.0.0" title="填写应用当前版本" onChange={e => setNewMarketplaceApp(v => ({ ...v, currentVersion: e.target.value }))} /><FieldHint>示例：1.0.0。</FieldHint></div><div className="field-block"><span className="field-label">安装包</span><input type="file" title="上传应用安装包，最大 100MB" onChange={async e => { const f = e.target.files?.[0]; if (!f) return; const r = await handleFileUpload(f, 'marketapps'); if (r) setNewMarketplaceApp(v => ({ ...v, packageFile: r.path, packageSize: r.size, packageName: r.name })); }} /><FieldHint>上传应用包后可发布到应用市场。</FieldHint></div></div><div className="action-cell" style={{ marginTop: 12 }}><button className="primary-button" onClick={() => { if (!newMarketplaceApp.name.trim()) { setError('请输入名称'); return; } if (!newMarketplaceApp.category.trim()) { setError('请输入分类'); return; } if (!newMarketplaceApp.currentVersion.trim()) { setError('请输入版本'); return; } const isEdit = Boolean(newMarketplaceApp.id); const original = marketApps.find(app => app.id === newMarketplaceApp.id); const payload = toMarketplaceAppPayload(newMarketplaceApp, original); const action = isEdit ? () => api.updateMarketplaceApp(newMarketplaceApp.id, payload) : () => api.createMarketplaceApp(payload); withSaving(action, isEdit ? '应用已更新' : '应用已保存').then(ok => ok && setShowNewMarketplaceAppForm(false)); }} disabled={saving || submitting || dataLoading}>保存</button><button className="secondary-button" onClick={() => setShowNewMarketplaceAppForm(false)}>取消</button></div></div>}
          {marketApps.length === 0 ? <p className="muted" style={{ textAlign: 'center', padding: '24px 0' }}>暂无数据</p> : (
            <div className="table-wrap"><table><thead><tr><th>应用</th><th>分类</th><th>版本</th><th>状态</th><th>更新</th><th>操作</th></tr></thead><tbody>
              {marketApps.map(app => <tr key={app.id}><td><strong>{app.name}</strong><br /><span className="muted" style={{ fontSize: 11 }}>{app.vendor}</span></td><td><span className="badge badge-info">{app.category}</span></td><td>{app.currentVersion}</td><td><span className={`badge ${app.status === 'published' ? 'badge-success' : app.status === 'unpublished' ? 'badge-danger' : 'badge-info'}`}>{app.status}</span></td><td className="muted">{formatTime(app.updatedAt)}</td><td><div style={{ display: 'flex', gap: 4, flexWrap: 'wrap' }}>{app.status !== 'published' && <button className="primary-button" style={{ minHeight: 30, padding: '3px 8px', fontSize: 11 }} disabled={submitting || dataLoading} onClick={() => handleSave(() => api.publishMarketplaceApp(app.id), '已发布')}>发布</button>}{app.status === 'published' && <button className="secondary-button" style={{ minHeight: 30, padding: '3px 8px', fontSize: 11 }} disabled={submitting || dataLoading} onClick={() => handleSave(() => api.unpublishMarketplaceApp(app.id), '已下架')}>下架</button>}<button className="secondary-button" style={{ minHeight: 30, padding: '3px 8px', fontSize: 11 }} disabled={submitting || dataLoading} onClick={() => startEditMarketApp(app)}>编辑</button><button className="secondary-button" style={{ minHeight: 30, padding: '3px 8px', fontSize: 11 }} disabled={submitting || dataLoading} onClick={() => { const version = window.prompt('请输入新版本号，示例：1.0.1', app.currentVersion); if (version) void handleSave(() => api.addMarketplaceAppVersion(app.id, version, app.packageName), '版本已追加'); }}>加版本</button><button className="danger-button" style={{ minHeight: 30, padding: '3px 8px', fontSize: 11 }} disabled={submitting || dataLoading} onClick={() => handleDelete(() => api.deleteMarketplaceApp(app.id), '应用已删除')}>删除</button></div></td></tr>)}
            </tbody></table></div>
          )}
        </div>
      )}
    </>
  );
}
