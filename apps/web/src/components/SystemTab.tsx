import { useState, useEffect } from 'react';
import type {
  AlertThresholdConfig,
  AuditEvent,
  ClusterAlert,
  ClusterLog,
  ClusterQuota,
  ClusterUpgradeStatus,
  PluginDefinition,
  SystemSetting,
  UserPayload,
  UserView,
} from '../types';
import { api } from '../api';
import { BulkDeleteControls } from './BulkDeleteControls';
import { rowCheckboxClassName, toggleSelected } from './bulkSelection';

interface SystemTabProps {
  clusterQuotas: ClusterQuota[];
  clusterLogs: ClusterLog[];
  clusterUpgrade: ClusterUpgradeStatus | null;
  systemSettings: SystemSetting[];
  users: UserView[];
  auditEvents: AuditEvent[];
  mergedAlerts: ClusterAlert[];
  alertLevelLabel: Record<string, string>;
  alertLevelColor: Record<string, string>;
  savedAlertThresholdCpu: string;
  savedAlertThresholdMem: string;
  savedAlertThresholdPod: string;
  alertThresholdCpu: string;
  alertThresholdMem: string;
  alertThresholdPod: string;
  setAlertThresholdCpu: (v: string) => void;
  setAlertThresholdMem: (v: string) => void;
  setAlertThresholdPod: (v: string) => void;
  setSavedAlertThresholdCpu: (v: string) => void;
  setSavedAlertThresholdMem: (v: string) => void;
  setSavedAlertThresholdPod: (v: string) => void;
  k3sVersions: string[];
  k3sVersionsLoading: boolean;
  upgradeVersion: string;
  setUpgradeVersion: (v: string) => void;
  upgradeChannel: string;
  setUpgradeChannel: (v: string) => void;
  activeSubTab: string;
  setActiveSubTab: (key: string) => void;
  loadData: (silentOrTab?: boolean | string) => Promise<void>;
  handleSave: (action: () => Promise<unknown>, successMsg: string) => Promise<boolean>;
  handleDelete: (action: () => Promise<unknown>, successMsg: string) => Promise<boolean>;
  setError: (msg: string) => void;
  setSuccess: (msg: string) => void;
  dataLoading: boolean;
  submitting: boolean;
  formatTime: (value: string) => string;
  roleLabels: Record<string, string>;
  plugins: PluginDefinition[];
}

const ALERT_PAGE_SIZE = 8;
const LOG_PAGE_SIZE = 15;
const AUDIT_PAGE_SIZE = 15;

function FieldHint({ children }: { children: string }) {
  return <span className="field-hint">{children}</span>;
}

const initialQuota = { id: '', scope: '', cpuLimit: '', memoryLimit: '', podLimit: 0 };
const initialUser = { id: '', username: '', displayName: '', password: '', role: 'operator' as UserPayload['role'], status: 'active' as UserPayload['status'] };
const initialSystemSetting = { id: '', category: 'general' as SystemSetting['category'], name: '', value: '' };

export function SystemTab({
  clusterQuotas,
  clusterLogs,
  clusterUpgrade,
  systemSettings,
  users,
  auditEvents,
  mergedAlerts,
  alertLevelLabel,
  alertLevelColor,
  savedAlertThresholdCpu,
  savedAlertThresholdMem,
  savedAlertThresholdPod,
  alertThresholdCpu,
  alertThresholdMem,
  alertThresholdPod,
  setAlertThresholdCpu,
  setAlertThresholdMem,
  setAlertThresholdPod,
  setSavedAlertThresholdCpu,
  setSavedAlertThresholdMem,
  setSavedAlertThresholdPod,
  k3sVersions,
  k3sVersionsLoading,
  upgradeVersion,
  setUpgradeVersion,
  upgradeChannel,
  setUpgradeChannel,
  activeSubTab,
  setActiveSubTab,
  loadData,
  handleSave,
  handleDelete,
  setError,
  setSuccess,
  dataLoading,
  submitting,
  formatTime,
  roleLabels,
  plugins,
}: SystemTabProps) {
  const [newQuota, setNewQuota] = useState(initialQuota);
  const [showNewQuotaForm, setShowNewQuotaForm] = useState(false);
  const [editUser, setEditUser] = useState<UserPayload>(initialUser);
  const [showEditUserForm, setShowEditUserForm] = useState(false);
  const [newSystemSetting, setNewSystemSetting] = useState(initialSystemSetting);
  const [showNewSystemSettingForm, setShowNewSystemSettingForm] = useState(false);
  const [alertPage, setAlertPage] = useState(1);
  const [logPage, setLogPage] = useState(1);
  const [auditPage, setAuditPage] = useState(1);
  const [logFilter, setLogFilter] = useState('');
  const [logLevelFilter, setLogLevelFilter] = useState('');
  const [auditFilter, setAuditFilter] = useState('');
  const [showPluginForm, setShowPluginForm] = useState(false);
  const [newPlugin, setNewPlugin] = useState<PluginDefinition>({ id: '', name: '', type: 'monitoring', version: '1.0.0', status: 'enabled', endpoint: '', config: {}, description: '', createdAt: '', updatedAt: '' });
  const [selectedSettings, setSelectedSettings] = useState<Set<string>>(new Set());
  const [selectedQuotas, setSelectedQuotas] = useState<Set<string>>(new Set());
  const [alertStatuses, setAlertStatuses] = useState<Record<string, string>>({});

  const startEditSetting = (setting: SystemSetting) => {
    setNewSystemSetting({ id: setting.id, category: setting.category, name: setting.name, value: setting.value });
    setShowNewSystemSettingForm(true);
  };
  const startEditQuota = (quota: ClusterQuota) => {
    setNewQuota({ id: quota.id, scope: quota.scope, cpuLimit: quota.cpuLimit, memoryLimit: quota.memoryLimit, podLimit: quota.podLimit });
    setShowNewQuotaForm(true);
  };
  const startEditPlugin = (plugin: PluginDefinition) => {
    setNewPlugin({ ...plugin });
    setShowPluginForm(true);
  };

  const runBulkDelete = (ids: string[], action: (id: string) => Promise<unknown>, label: string, clear: () => void) => {
    if (ids.length === 0) return;
    if (!confirm(`确认删除选中的 ${ids.length} 个${label}？`)) return;
    void handleSave(async () => {
      await Promise.all(ids.map(id => action(id)));
      clear();
    }, `已删除 ${ids.length} 个${label}`);
  };

  const downloadCSV = (filename: string, headers: string[], rows: string[][]) => {
    const bom = '\uFEFF';
    const esc = (v: string) => {
      const s = String(v);
      if (s.startsWith('=') || s.startsWith('+') || s.startsWith('-') || s.startsWith('@')) {
        return `"'${s.replace(/"/g, '""')}"`;
      }
      return `"${s.replace(/"/g, '""')}"`;
    };
    const csv = bom + [headers.join(','), ...rows.map(r => r.map(esc).join(','))].join('\n');
    const blob = new Blob([csv], { type: 'text/csv;charset=utf-8' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = filename;
    a.click();
    URL.revokeObjectURL(url);
  };

  const dismissAlert = (alertId: string) => {
    setAlertStatuses(prev => ({ ...prev, [alertId]: 'closed' }));
    setSuccess('告警已标记为已读');
  };

  useEffect(() => { setAlertPage(1); }, [activeSubTab]);
  useEffect(() => { setLogPage(1); setLogFilter(''); setLogLevelFilter(''); }, [activeSubTab]);
  useEffect(() => { setAuditPage(1); setAuditFilter(''); }, [activeSubTab]);

  return (
    <>
      <div className="panel top-banner top-banner-soft hero">
        <div className="hero-main">
          <h2>系统管理</h2>
          <p className="muted">告警、配额、账号、审计与系统设置</p>
        </div>
        <div className="hero-badges">
          <button className="secondary-button" style={{ minHeight: 36, padding: '6px 14px', fontSize: 13 }} onClick={() => loadData('system')} disabled={dataLoading}>刷新数据</button>
        </div>
      </div>

      <div className="sub-tab-bar">
        {[
          { key: 'settings', label: '系统设置' },
          { key: 'quotas', label: '资源配额' },
          { key: 'accounts', label: '账号管理' },
          { key: 'upgrade', label: '版本升级' },
          { key: 'alert-events', label: '告警事件' },
          { key: 'logs', label: '集群日志' },
          { key: 'audit', label: '审计日志' },
          { key: 'plugins', label: '插件管理' },
        ].map(st => (
          <button key={st.key} className={`sub-tab-button${activeSubTab === st.key ? ' sub-tab-active' : ''}`} onClick={() => { setActiveSubTab(st.key); setError(''); setSuccess(''); }}>
            {st.label}
          </button>
        ))}
      </div>

      {/* Settings */}
      {activeSubTab === 'settings' && (
        <div className="panel">
          <div className="section-head" style={{ marginBottom: 12 }}>
            <h3 style={{ margin: 0, fontSize: 16 }}>系统设置 ({systemSettings.length})</h3>
            <button className="primary-button" onClick={() => { setNewSystemSetting(initialSystemSetting); setShowNewSystemSettingForm(true); }}>添加配置</button>
          </div>
          {showNewSystemSettingForm && (
            <div className="panel" style={{ marginBottom: 14, background: 'rgba(239,246,255,0.6)' }}>
              <h4 style={{ margin: '0 0 10px' }}>{newSystemSetting.id ? '编辑系统设置' : '添加系统设置'}</h4>
              <div className="form-grid">
                <div className="field-block"><span className="field-label required">类别</span><select value={newSystemSetting.category} title="选择配置所属类别" onChange={e => setNewSystemSetting(s => ({ ...s, category: e.target.value }))}><option value="general">通用</option><option value="security">安全</option><option value="network">网络</option><option value="notification">通知</option></select><FieldHint>按配置用途选择类别。</FieldHint></div>
                <div className="field-block"><span className="field-label required">名称</span><input value={newSystemSetting.name} placeholder="session_timeout_minutes" title="填写配置键名" onChange={e => setNewSystemSetting(s => ({ ...s, name: e.target.value }))} /><FieldHint>示例：session_timeout_minutes。</FieldHint></div>
                <div className="field-block"><span className="field-label required">值</span><input value={newSystemSetting.value} placeholder="30" title="填写配置值" onChange={e => setNewSystemSetting(s => ({ ...s, value: e.target.value }))} /><FieldHint>示例：30、true、https://example.internal。</FieldHint></div>
              </div>
              <div className="action-cell" style={{ marginTop: 12 }}>
                <button className="primary-button" disabled={submitting || dataLoading} onClick={() => {
                  if (!newSystemSetting.category || !newSystemSetting.name.trim() || !newSystemSetting.value.trim()) {
                    setError('请填写所有必填字段');
                    return;
                  }
                  handleSave(() => newSystemSetting.id ? api.updateSystemSetting(newSystemSetting.id, newSystemSetting as SystemSetting) : api.saveSystemSetting(newSystemSetting as SystemSetting), '设置已保存').then(ok => ok && setShowNewSystemSettingForm(false));
                }}>保存</button>
                <button className="secondary-button" onClick={() => setShowNewSystemSettingForm(false)}>取消</button>
              </div>
            </div>
          )}
          {systemSettings.length > 0 && <BulkDeleteControls total={systemSettings.length} selected={selectedSettings} setSelected={setSelectedSettings} ids={systemSettings.map(setting => setting.id)} label="配置" onDelete={() => runBulkDelete(Array.from(selectedSettings), api.deleteSystemSetting, '配置', () => setSelectedSettings(new Set()))} />}
          {systemSettings.length === 0 ? (
            <p className="muted" style={{ textAlign: 'center', padding: '24px 0' }}>暂无数据</p>
          ) : (
            <div className="table-wrap">
              <table>
                <thead><tr><th className="row-select-cell"></th><th>类别</th><th>名称</th><th>值</th><th>更新</th><th>操作</th></tr></thead>
                <tbody>
                  {systemSettings.map(setting => (
                    <tr key={setting.id}>
                      <td className="row-select-cell"><input className={rowCheckboxClassName} type="checkbox" checked={selectedSettings.has(setting.id)} onChange={() => toggleSelected(setSelectedSettings, setting.id)} /></td>
                      <td><span className="badge badge-info" style={{ fontSize: 11 }}>{setting.category}</span></td>
                      <td><strong style={{ fontSize: 13 }}>{setting.name}</strong></td>
                      <td className="muted">{setting.value}</td>
                      <td className="muted" style={{ fontSize: 12 }}>{formatTime(setting.updatedAt)}</td>
                      <td>
                        <div style={{ display: 'flex', gap: 4 }}>
                          <button className="secondary-button" style={{ minHeight: 30, padding: '3px 8px', fontSize: 11 }} disabled={submitting || dataLoading} onClick={() => startEditSetting(setting)}>编辑</button>
                          <button className="danger-button" style={{ minHeight: 30, padding: '3px 8px', fontSize: 11 }} disabled={submitting || dataLoading} onClick={() => handleDelete(() => api.deleteSystemSetting(setting.id), '系统设置已删除')}>删除</button>
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      )}

      {/* Quotas */}
      {activeSubTab === 'quotas' && (
        <div className="panel">
          <div className="section-head" style={{ marginBottom: 12 }}>
            <h3 style={{ margin: 0, fontSize: 16 }}>资源配额 ({clusterQuotas.length})</h3>
            <button className="primary-button" onClick={() => { setNewQuota(initialQuota); setShowNewQuotaForm(true); }}>新增配额</button>
          </div>
          {showNewQuotaForm && (
            <div className="panel" style={{ marginBottom: 14, background: 'rgba(239,246,255,0.6)' }}>
              <h4 style={{ margin: '0 0 10px' }}>{newQuota.id ? '编辑资源配额' : '新增资源配额'}</h4>
              <div className="form-grid">
                <div className="field-block"><span className="field-label required">作用域</span><input value={newQuota.scope} placeholder="namespace/default" title="填写配额生效范围" onChange={e => setNewQuota(v => ({ ...v, scope: e.target.value }))} /><FieldHint>示例：namespace/default 或 cluster。</FieldHint></div>
                <div className="field-block"><span className="field-label required">CPU 限制</span><input value={newQuota.cpuLimit} placeholder="2" title="填写 Kubernetes CPU 资源格式" onChange={e => setNewQuota(v => ({ ...v, cpuLimit: e.target.value }))} /><FieldHint>示例：2、500m。</FieldHint></div>
                <div className="field-block"><span className="field-label required">内存限制</span><input value={newQuota.memoryLimit} placeholder="4Gi" title="填写 Kubernetes 内存资源格式" onChange={e => setNewQuota(v => ({ ...v, memoryLimit: e.target.value }))} /><FieldHint>示例：4Gi、1024Mi。</FieldHint></div>
                <div className="field-block"><span className="field-label required">Pod 上限</span><input value={newQuota.podLimit} placeholder="20" title="填写允许创建的 Pod 最大数量" onChange={e => setNewQuota(v => ({ ...v, podLimit: Number(e.target.value) || 0 }))} /><FieldHint>示例：20，必须为数字。</FieldHint></div>
              </div>
              <div className="action-cell" style={{ marginTop: 12 }}>
                <button className="primary-button" disabled={submitting || dataLoading} onClick={() => {
                  if (!newQuota.scope.trim() || !newQuota.cpuLimit.trim() || !newQuota.memoryLimit.trim() || !Number.isInteger(newQuota.podLimit) || newQuota.podLimit <= 0) {
                    setError('请填写所有必填字段，Pod 上限必须为大于 0 的整数');
                    return;
                  }
                  handleSave(() => newQuota.id ? api.updateClusterQuota(newQuota.id, newQuota as ClusterQuota) : api.saveClusterQuota(newQuota as ClusterQuota), '配额已保存').then(ok => ok && setShowNewQuotaForm(false));
                }}>保存</button>
                <button className="secondary-button" onClick={() => setShowNewQuotaForm(false)}>取消</button>
              </div>
            </div>
          )}
          {clusterQuotas.length > 0 && <BulkDeleteControls total={clusterQuotas.length} selected={selectedQuotas} setSelected={setSelectedQuotas} ids={clusterQuotas.map(quota => quota.id)} label="配额" onDelete={() => runBulkDelete(Array.from(selectedQuotas), api.deleteClusterQuota, '配额', () => setSelectedQuotas(new Set()))} />}
          {clusterQuotas.length === 0 ? (
            <p className="muted" style={{ textAlign: 'center', padding: '24px 0' }}>暂无数据</p>
          ) : (
            <div className="table-wrap">
              <table>
                <thead><tr><th className="row-select-cell"></th><th>作用域</th><th>CPU 限制</th><th>内存限制</th><th>Pod 上限</th><th>更新时间</th><th>操作</th></tr></thead>
                <tbody>
                  {clusterQuotas.map(quota => (
                    <tr key={quota.id}>
                      <td className="row-select-cell"><input className={rowCheckboxClassName} type="checkbox" checked={selectedQuotas.has(quota.id)} onChange={() => toggleSelected(setSelectedQuotas, quota.id)} /></td>
                      <td><span className="badge badge-info">{quota.scope}</span></td>
                      <td>{quota.cpuLimit}</td>
                      <td>{quota.memoryLimit}</td>
                      <td>{quota.podLimit}</td>
                      <td className="muted">{formatTime(quota.updatedAt)}</td>
                      <td>
                        <div style={{ display: 'flex', gap: 4 }}>
                          <button className="secondary-button" style={{ minHeight: 30, padding: '3px 8px', fontSize: 11 }} disabled={submitting || dataLoading} onClick={() => startEditQuota(quota)}>编辑</button>
                          <button className="danger-button" style={{ minHeight: 30, padding: '3px 8px', fontSize: 11 }} disabled={submitting || dataLoading} onClick={() => handleDelete(() => api.deleteClusterQuota(quota.id), '配额已删除')}>删除</button>
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      )}

      {/* Accounts */}
      {activeSubTab === 'accounts' && (
        <div className="panel">
          <div className="section-head" style={{ marginBottom: 12 }}>
            <h3 style={{ margin: 0, fontSize: 16 }}>账号管理 ({users.length})</h3>
            <button className="primary-button" onClick={() => { setEditUser(initialUser); setShowEditUserForm(true); }}>创建账号</button>
          </div>
          {showEditUserForm && (
            <div className="panel" style={{ marginBottom: 14, background: 'rgba(239,246,255,0.6)' }}>
              <h4 style={{ margin: '0 0 10px' }}>{editUser.id ? '编辑账号' : '创建账号'}</h4>
              <div className="form-grid">
                <div className="field-block"><span className="field-label required">用户名</span><input value={editUser.username} placeholder="ops-user" title="填写登录用户名" onChange={e => setEditUser(u => ({ ...u, username: e.target.value }))} /><FieldHint>示例：ops-user，建议使用小写字母、数字和短横线。</FieldHint></div>
                <div className="field-block"><span className="field-label required">显示名称</span><input value={editUser.displayName} placeholder="运维操作员" title="填写页面展示名称" onChange={e => setEditUser(u => ({ ...u, displayName: e.target.value }))} /><FieldHint>示例：运维操作员。</FieldHint></div>
                <div className="field-block"><span className="field-label required">密码</span><input type="password" value={editUser.password} placeholder={editUser.id ? '留空表示不修改密码' : '至少 8 位'} title="创建账号时填写初始密码；编辑账号时留空表示不修改" onChange={e => setEditUser(u => ({ ...u, password: e.target.value }))} /><FieldHint>{editUser.id ? '编辑账号时可留空保留原密码。' : '建议至少 8 位，包含字母和数字。'}</FieldHint></div>
                <div className="field-block"><span className="field-label required">角色</span><select value={editUser.role} title="选择账号权限角色" onChange={e => setEditUser(u => ({ ...u, role: e.target.value as UserPayload['role'] }))}>{Object.entries(roleLabels).map(([k, v]) => <option key={k} value={k}>{v}</option>)}</select><FieldHint>按职责选择最小权限角色。</FieldHint></div>
                <div className="field-block"><span className="field-label required">状态</span><select value={editUser.status} title="启用/禁用账号" onChange={e => setEditUser(u => ({ ...u, status: e.target.value as UserPayload['status'] }))}><option value="active">启用</option><option value="disabled">禁用</option></select><FieldHint>禁用后该账号将无法登录。</FieldHint></div>
              </div>
              <div className="action-cell" style={{ marginTop: 12 }}>
                <button className="primary-button" disabled={submitting || dataLoading} onClick={() => {
                  if (!editUser.username.trim() || !editUser.displayName.trim() || !editUser.role || !editUser.status) {
                    setError('请填写所有必填字段');
                    return;
                  }
                  if (!editUser.id && !editUser.password.trim()) {
                    setError('创建账号时必须填写密码');
                    return;
                  }
                  handleSave(() => editUser.id ? api.updateUser(editUser.id, editUser) : api.saveUser(editUser), '账号已保存').then(ok => ok && setShowEditUserForm(false));
                }}>保存</button>
                <button className="secondary-button" onClick={() => setShowEditUserForm(false)}>取消</button>
              </div>
            </div>
          )}
          {users.length === 0 ? (
            <p className="muted" style={{ textAlign: 'center', padding: '24px 0' }}>暂无数据</p>
          ) : (
            <div className="table-wrap">
              <table>
                <thead><tr><th>用户名</th><th>显示名</th><th>角色</th><th>状态</th><th>创建时间</th><th>最后登录</th><th>操作</th></tr></thead>
                <tbody>
                  {users.map(user => (
                    <tr key={user.id}>
                      <td><strong>{user.username}</strong></td>
                      <td>{user.displayName}</td>
                      <td><span className="badge badge-info" style={{ fontSize: 11 }}>{roleLabels[user.role] ?? user.role}</span></td>
                      <td><span className={`badge ${user.status === 'active' ? 'badge-success' : 'badge-danger'}`} style={{ fontSize: 11 }}>{user.status}</span></td>
                      <td className="muted" style={{ fontSize: 12 }}>{formatTime(user.createdAt)}</td>
                      <td className="muted" style={{ fontSize: 12 }}>{user.lastLoginAt ? formatTime(user.lastLoginAt) : '从未登录'}</td>
                      <td>
                        <div style={{ display: 'flex', gap: 4 }}>
                          <button className="secondary-button" style={{ minHeight: 30, padding: '3px 8px', fontSize: 11 }} disabled={submitting || dataLoading} onClick={() => { setEditUser({ id: user.id, username: user.username, displayName: user.displayName, role: user.role, status: user.status, password: '' }); setShowEditUserForm(true); }}>编辑</button>
                          <button className="danger-button" style={{ minHeight: 30, padding: '3px 8px', fontSize: 11 }} disabled={submitting || dataLoading} onClick={() => handleDelete(() => api.deleteUser(user.id), '账号已删除')}>删除</button>
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      )}

      {/* Upgrade */}
      {activeSubTab === 'upgrade' && (
        <div className="panel">
          <h3 style={{ margin: '0 0 12px', fontSize: 16 }}>版本升级</h3>
          {clusterUpgrade && (
            <div className="inline-card" style={{ marginBottom: 14 }}>
              <div>
                <span className="tiny-label">当前版本</span>
                <strong style={{ display: 'block', fontSize: 16 }}>{clusterUpgrade.currentVersion}</strong>
              </div>
            {clusterUpgrade && (clusterUpgrade.status === 'running' || clusterUpgrade.status === 'pending_executor') && (
                <div>
                  <span className="tiny-label">升级进度</span>
                  <div className="progress-track" style={{ width: 160, display: 'inline-block' }}>
                    <div className={`progress-fill ${clusterUpgrade.status === 'running' ? 'progress-ok' : 'progress-pending'}`} style={{ width: `${clusterUpgrade.progress || 0}%` }} />
                  </div>
                  <span style={{ fontSize: 13, marginLeft: 8 }}>{clusterUpgrade.progress || 0}%</span>
                </div>
              )}
              <div style={{ textAlign: 'right' }}>
                <span className="tiny-label">状态</span>
                <span className={`badge ${clusterUpgrade.status === 'running' ? 'badge-warning' : clusterUpgrade.status === 'completed' || clusterUpgrade.status === 'idle' ? 'badge-success' : 'badge-info'}`} style={{ display: 'block', marginTop: 2, fontSize: 11 }}>{clusterUpgrade.status}</span>
              </div>
            </div>
          )}
          <div className="inline-card" style={{ flexWrap: 'wrap', gap: 8 }}>
            <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
              <span className="field-label" style={{ fontSize: 13, whiteSpace: 'nowrap' }}>目标版本</span>
              {k3sVersionsLoading ? (
                <span className="muted" style={{ fontSize: 12 }}>加载中...</span>
              ) : (
                <select style={{ minHeight: 34, minWidth: 160 }} value={upgradeVersion} title="选择目标 K3s 版本，提交后会创建真实升级请求" onChange={e => setUpgradeVersion(e.target.value)}>
                  {k3sVersions.map(v => <option key={v} value={v}>{v}</option>)}
                </select>
              )}
            </div>
            <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
              <span className="field-label" style={{ fontSize: 13, whiteSpace: 'nowrap' }}>通道</span>
              <select style={{ minHeight: 34 }} value={upgradeChannel} title="选择 K3s 版本通道，生产环境建议 stable" onChange={e => setUpgradeChannel(e.target.value)}>
                <option value="stable">Stable</option>
                <option value="beta">Beta</option>
                <option value="preview">Preview</option>
              </select>
            </div>
            <button className="secondary-button" style={{ minHeight: 34, padding: '6px 16px', fontSize: 13 }} disabled={submitting || dataLoading || k3sVersionsLoading || !upgradeVersion} onClick={() => { if (!upgradeVersion) { setError('请选择目标版本'); return; } return handleSave(() => api.downloadClusterUpgrade({ version: upgradeVersion, channel: upgradeChannel }), '目标版本已下载，网络恢复后可直接执行升级'); }}>手动下载</button>
            <button className="primary-button" style={{ minHeight: 34, padding: '6px 16px', fontSize: 13 }} disabled={submitting || dataLoading || k3sVersionsLoading || !upgradeVersion} onClick={() => { if (!upgradeVersion) { setError('请选择目标版本'); return; } return handleSave(() => api.upgradeCluster({ version: upgradeVersion, channel: upgradeChannel }), '升级请求已提交，请查看升级状态和运行日志确认结果'); }}>执行升级</button>
            {clusterUpgrade && (clusterUpgrade.status === 'running' || clusterUpgrade.status === 'pending_executor') && (
              <button className="danger-button" style={{ minHeight: 34, padding: '6px 14px', fontSize: 13 }} disabled={submitting || dataLoading} onClick={() => handleSave(() => api.resetClusterUpgrade(), '升级已取消')}>取消升级</button>
            )}
          </div>
          {clusterUpgrade && clusterUpgrade.message && <p className="muted" style={{ margin: '10px 0 0' }}>{clusterUpgrade.message}</p>}
        </div>
      )}

      {/* Alert Events */}
      {activeSubTab === 'alert-events' && (
        <>
          <div className="panel" style={{ marginBottom: 16 }}>
            <div className="section-head" style={{ marginBottom: 10 }}><h3 style={{ margin: 0, fontSize: 16 }}>告警阈值配置</h3></div>
            <div className="inline-card" style={{ flexWrap: 'wrap' }}>
              <div style={{ display: 'flex', gap: 8, alignItems: 'center', flexWrap: 'wrap' }}>
                <span style={{ fontWeight: 600, fontSize: 13, color: '#334155' }}>当前阈值：</span>
                <span style={{ color: 'var(--accent)', fontWeight: 700, fontSize: 13 }}>CPU {savedAlertThresholdCpu}%</span>
                <span className="muted">/</span>
                <span style={{ color: 'var(--accent)', fontWeight: 700, fontSize: 13 }}>内存 {savedAlertThresholdMem}%</span>
                <span className="muted">/</span>
                <span style={{ color: 'var(--accent)', fontWeight: 700, fontSize: 13 }}>Pod {savedAlertThresholdPod} 个</span>
              </div>
              <div style={{ display: 'flex', gap: 8, alignItems: 'center', flexWrap: 'wrap' }}>
                <input style={{ width: 70, minHeight: 34, padding: '4px 8px', fontSize: 13 }} value={alertThresholdCpu} placeholder="80" title="CPU 使用率告警阈值，单位 %，示例 80" onChange={e => setAlertThresholdCpu(e.target.value)} />
                <input style={{ width: 70, minHeight: 34, padding: '4px 8px', fontSize: 13 }} value={alertThresholdMem} placeholder="80" title="内存使用率告警阈值，单位 %，示例 80" onChange={e => setAlertThresholdMem(e.target.value)} />
                <input style={{ width: 70, minHeight: 34, padding: '4px 8px', fontSize: 13 }} value={alertThresholdPod} placeholder="100" title="Pod 数量告警阈值，示例 100" onChange={e => setAlertThresholdPod(e.target.value)} />
                <button className="primary-button" style={{ minHeight: 34, padding: '6px 14px', fontSize: 13 }} disabled={submitting || dataLoading} onClick={() => handleSave(async () => {
                  const cpu = Number(alertThresholdCpu);
                  const mem = Number(alertThresholdMem);
                  const pod = Number(alertThresholdPod);
                  if (!Number.isFinite(cpu) || cpu <= 0 || cpu > 100) throw new Error('CPU 阈值必须为 1-100 的数字');
                  if (!Number.isFinite(mem) || mem <= 0 || mem > 100) throw new Error('内存阈值必须为 1-100 的数字');
                  if (!Number.isFinite(pod) || pod <= 0) throw new Error('Pod 阈值必须为大于 0 的数字');
                  const cfg: AlertThresholdConfig = { cpuThreshold: cpu, memThreshold: mem, podThreshold: pod };
                  await api.saveAlertThreshold(cfg);
                  setSavedAlertThresholdCpu(String(cfg.cpuThreshold));
                  setSavedAlertThresholdMem(String(cfg.memThreshold));
                  setSavedAlertThresholdPod(String(cfg.podThreshold));
                }, '告警阈值已更新')}>保存</button>
              </div>
            </div>
          </div>
          <div className="panel">
            <h3 style={{ margin: '0 0 12px', fontSize: 16 }}>告警事件 ({mergedAlerts.length})</h3>
            <div style={{ display: 'flex', gap: 10, marginBottom: 12, flexWrap: 'wrap', alignItems: 'center' }}>
              <button className="secondary-button" style={{ minHeight: 30, padding: '4px 12px', fontSize: 12, whiteSpace: 'nowrap' }} onClick={() => downloadCSV(`cluster-alerts-${new Date().toISOString().slice(0, 10)}.csv`, ['来源','级别','消息','状态','时间'], mergedAlerts.map(a => [a.source, a.level, a.message, a.status, a.createdAt]))} disabled={mergedAlerts.length === 0}>导出 CSV</button>
            </div>
            {mergedAlerts.length === 0 ? (
              <p className="muted">暂无告警事件</p>
            ) : (
              <>
                <div className="feature-list alert-event-list">
                  {mergedAlerts
                    .slice((alertPage - 1) * ALERT_PAGE_SIZE, alertPage * ALERT_PAGE_SIZE)
                    .map(alert => {
                      const effectiveStatus = alertStatuses[alert.id] ?? alert.status;
                      return (
                      <div key={alert.id} className="detail-card alert-event-card">
                        <div className="alert-event-main">
                          <div className="alert-event-title">
                            <span className={`badge ${alertLevelColor[alert.level]}`}>{alertLevelLabel[alert.level]}</span>
                            <strong>{alert.source}</strong>
                          </div>
                          <p className="muted alert-event-message">{alert.message}</p>
                        </div>
                        <div className="alert-event-meta">
                          <span className="muted">{formatTime(alert.createdAt)}</span>
                          <span className={`badge ${effectiveStatus === 'open' ? 'badge-warning' : 'badge-success'}`}>{effectiveStatus}</span>
                          {effectiveStatus === 'open' && (
                            <button className="secondary-button" style={{ minHeight: 26, padding: '2px 8px', fontSize: 11, whiteSpace: 'nowrap' }} disabled={submitting || dataLoading} onClick={() => dismissAlert(alert.id)}>已读</button>
                          )}
                        </div>
                      </div>
                      );
                    })}
                </div>
                <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'center', gap: 12, marginTop: 16 }}>
                  <button className="secondary-button" style={{ minHeight: 30, padding: '4px 12px', fontSize: 12 }} disabled={alertPage <= 1} onClick={() => setAlertPage(Math.max(1, alertPage - 1))}>上一页</button>
                  <span style={{ fontSize: 13, color: '#64748b' }}>第 {alertPage} / {Math.ceil(mergedAlerts.length / ALERT_PAGE_SIZE)} 页</span>
                  <button className="secondary-button" style={{ minHeight: 30, padding: '4px 12px', fontSize: 12 }} disabled={alertPage >= Math.ceil(mergedAlerts.length / ALERT_PAGE_SIZE)} onClick={() => setAlertPage(Math.min(Math.ceil(mergedAlerts.length / ALERT_PAGE_SIZE), alertPage + 1))}>下一页</button>
                </div>
              </>
            )}
          </div>
        </>
      )}

      {/* Logs */}
      {activeSubTab === 'logs' && (
        <div className="panel">
          <h3 style={{ margin: '0 0 12px', fontSize: 16 }}>集群日志 ({clusterLogs.length})</h3>
          <div style={{ display: 'flex', gap: 10, marginBottom: 12, flexWrap: 'wrap', alignItems: 'center' }}>
            <input style={{ flex: 1, minWidth: 160 }} placeholder="搜索消息关键字..." value={logFilter} onChange={e => { setLogFilter(e.target.value); setLogPage(1); }} />
            <select value={logLevelFilter} onChange={e => { setLogLevelFilter(e.target.value); setLogPage(1); }}><option value="">全部级别</option><option value="info">info</option><option value="warning">warning</option><option value="error">error</option></select>
            <button className="secondary-button" style={{ minHeight: 30, padding: '4px 12px', fontSize: 12, whiteSpace: 'nowrap' }} onClick={() => downloadCSV(`cluster-logs-${new Date().toISOString().slice(0, 10)}.csv`, ['节点','类别','级别','消息','时间'], clusterLogs.map(l => [l.nodeId, l.category, l.level, l.message, l.recordedAt]))} disabled={clusterLogs.length === 0}>导出 CSV</button>
          </div>
          {(() => {
            const filtered = clusterLogs.filter(log => {
              if (logFilter && !log.message.toLowerCase().includes(logFilter.toLowerCase())) return false;
              if (logLevelFilter && log.level !== logLevelFilter) return false;
              return true;
            });
            if (filtered.length === 0) return <p className="muted" style={{ textAlign: 'center', padding: '24px 0' }}>{clusterLogs.length === 0 ? '暂无数据' : '没有匹配的日志'}</p>;
            const totalPages = Math.ceil(filtered.length / LOG_PAGE_SIZE);
            const page = Math.min(logPage, totalPages);
            return (
              <>
                <div className="table-wrap">
                  <table>
                    <thead><tr><th>节点</th><th>类别</th><th>级别</th><th>消息</th><th>时间</th></tr></thead>
                    <tbody>
                      {filtered.slice((page - 1) * LOG_PAGE_SIZE, page * LOG_PAGE_SIZE).map(log => (
                        <tr key={log.id}>
                          <td className="muted" style={{ fontSize: 12 }}>{log.nodeId}</td>
                          <td><span className="badge badge-info" style={{ fontSize: 11 }}>{log.category}</span></td>
                          <td><span className={`badge ${log.level === 'error' ? 'badge-danger' : log.level === 'warning' ? 'badge-warning' : 'badge-info'}`} style={{ fontSize: 11 }}>{log.level}</span></td>
                          <td className="muted" style={{ fontSize: 13 }}>{log.message}</td>
                          <td className="muted" style={{ fontSize: 12 }}>{formatTime(log.recordedAt)}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
                <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'center', gap: 12, marginTop: 16 }}>
                  <button className="secondary-button" style={{ minHeight: 30, padding: '4px 12px', fontSize: 12 }} disabled={page <= 1} onClick={() => setLogPage(Math.max(1, page - 1))}>上一页</button>
                  <span style={{ fontSize: 13, color: '#64748b' }}>第 {page} / {totalPages} 页（已筛选 {filtered.length} 条）</span>
                  <button className="secondary-button" style={{ minHeight: 30, padding: '4px 12px', fontSize: 12 }} disabled={page >= totalPages} onClick={() => setLogPage(Math.min(totalPages, page + 1))}>下一页</button>
                </div>
              </>
            );
          })()}
        </div>
      )}

      {/* Audit */}
      {activeSubTab === 'audit' && (
        <div className="panel">
          <h3 style={{ margin: '0 0 12px', fontSize: 16 }}>审计日志 ({auditEvents.length})</h3>
          <div style={{ display: 'flex', gap: 10, marginBottom: 12, flexWrap: 'wrap', alignItems: 'center' }}>
            <input style={{ flex: 1, minWidth: 160 }} placeholder="搜索操作人或目标..." value={auditFilter} onChange={e => { setAuditFilter(e.target.value); setAuditPage(1); }} />
            <button className="secondary-button" style={{ minHeight: 30, padding: '4px 12px', fontSize: 12, whiteSpace: 'nowrap' }} onClick={() => downloadCSV(`audit-logs-${new Date().toISOString().slice(0, 10)}.csv`, ['时间','操作人','操作','目标','结果'], auditEvents.map(e => [e.createdAt, e.actor, e.action, e.target, e.result]))} disabled={auditEvents.length === 0}>导出 CSV</button>
          </div>
          {(() => {
            const filtered = auditEvents.filter(event => {
              if (auditFilter) {
                const kw = auditFilter.toLowerCase();
                if (!event.actor.toLowerCase().includes(kw) && !event.target.toLowerCase().includes(kw) && !event.action.toLowerCase().includes(kw) && !event.result.toLowerCase().includes(kw)) return false;
              }
              return true;
            });
            if (filtered.length === 0) return <p className="muted" style={{ textAlign: 'center', padding: '24px 0' }}>{auditEvents.length === 0 ? '暂无数据' : '没有匹配的审计记录'}</p>;
            const totalPages = Math.ceil(filtered.length / AUDIT_PAGE_SIZE);
            const page = Math.min(auditPage, totalPages);
            return (
              <>
                <div className="table-wrap">
                  <table>
                    <thead><tr><th>时间</th><th>操作人</th><th>操作</th><th>目标</th><th>结果</th></tr></thead>
                    <tbody>
                      {filtered.slice((page - 1) * AUDIT_PAGE_SIZE, page * AUDIT_PAGE_SIZE).map(event => (
                        <tr key={event.id}>
                          <td className="muted" style={{ fontSize: 12, whiteSpace: 'nowrap' }}>{formatTime(event.createdAt)}</td>
                          <td>{event.actor}</td>
                          <td><span className="badge badge-primary" style={{ fontSize: 11 }}>{event.action}</span></td>
                          <td className="muted">{event.target}</td>
                          <td><span className={`badge ${event.result === 'success' ? 'badge-success' : 'badge-danger'}`} style={{ fontSize: 11 }}>{event.result}</span></td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
                <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'center', gap: 12, marginTop: 16 }}>
                  <button className="secondary-button" style={{ minHeight: 30, padding: '4px 12px', fontSize: 12 }} disabled={page <= 1} onClick={() => setAuditPage(Math.max(1, page - 1))}>上一页</button>
                  <span style={{ fontSize: 13, color: '#64748b' }}>第 {page} / {totalPages} 页（已筛选 {filtered.length} 条）</span>
                  <button className="secondary-button" style={{ minHeight: 30, padding: '4px 12px', fontSize: 12 }} disabled={page >= totalPages} onClick={() => setAuditPage(Math.min(totalPages, page + 1))}>下一页</button>
                </div>
              </>
            );
          })()}
        </div>
      )}

      {/* Plugins */}
      {activeSubTab === 'plugins' && (
        <div className="panel">
          <div className="section-head" style={{ marginBottom: 12 }}>
            <h3 style={{ margin: 0, fontSize: 16 }}>插件管理 ({plugins.length})</h3>
            <button className="primary-button" onClick={() => { setNewPlugin({ id: '', name: '', type: 'monitoring', version: '1.0.0', status: 'enabled', endpoint: '', config: {}, description: '', createdAt: '', updatedAt: '' }); setShowPluginForm(true); }}>注册插件</button>
          </div>
          {showPluginForm && (
            <div className="panel" style={{ marginBottom: 14, background: 'rgba(239,246,255,0.6)' }}>
              <h4 style={{ margin: '0 0 10px' }}>{newPlugin.id ? '编辑插件' : '注册新插件'}</h4>
              <div className="form-grid">
                <div className="field-block"><span className="field-label required">名称</span><input value={newPlugin.name} placeholder="示例：Prometheus 监控插件" onChange={e => setNewPlugin(p => ({ ...p, name: e.target.value }))} /><FieldHint>插件标识名称，如 Prometheus 监控插件。</FieldHint></div>
                <div className="field-block"><span className="field-label required">类型</span><select value={newPlugin.type} onChange={e => setNewPlugin(p => ({ ...p, type: e.target.value as PluginDefinition['type'] }))}><option value="monitoring">monitoring</option><option value="compliance">compliance</option><option value="notification">notification</option><option value="runtime">runtime</option><option value="custom">custom</option></select><FieldHint>插件类型决定 Hook 分发策略。</FieldHint></div>
                <div className="field-block"><span className="field-label">版本</span><input value={newPlugin.version} placeholder="1.0.0" onChange={e => setNewPlugin(p => ({ ...p, version: e.target.value }))} /><FieldHint>语义化版本号。</FieldHint></div>
                <div className="field-block"><span className="field-label">接口地址</span><input value={newPlugin.endpoint} placeholder="https://monitor.example.com/webhook" onChange={e => setNewPlugin(p => ({ ...p, endpoint: e.target.value }))} /><FieldHint>插件 Webhook 回调 URL，留空表示仅本地执行。</FieldHint></div>
                <div className="field-block"><span className="field-label">描述</span><input value={newPlugin.description} placeholder="插件功能简述" onChange={e => setNewPlugin(p => ({ ...p, description: e.target.value }))} /><FieldHint>简要说明插件用途。</FieldHint></div>
              </div>
              <div className="action-cell" style={{ marginTop: 12 }}>
                <button className="primary-button" disabled={submitting || dataLoading} onClick={() => {
                  if (!newPlugin.name.trim() || !newPlugin.type.trim()) {
                    setError('请填写所有必填字段');
                    return;
                  }
                  handleSave(async () => { const res = newPlugin.id ? await api.updatePlugin(newPlugin.id, newPlugin) : await api.savePlugin({ ...newPlugin, id: 'plugin-' + Date.now() }); setShowPluginForm(false); return res; }, '插件已注册').then(ok => { if (ok) loadData('system'); });
                }}>保存</button>
                <button className="secondary-button" onClick={() => setShowPluginForm(false)}>取消</button>
              </div>
            </div>
          )}
          {plugins.length === 0 ? (
            <p className="muted" style={{ textAlign: 'center', padding: '24px 0' }}>暂无已注册插件</p>
          ) : (
            <div className="table-wrap">
              <table>
                <thead><tr><th>名称</th><th>类型</th><th>版本</th><th>状态</th><th>接口地址</th><th>描述</th><th>操作</th></tr></thead>
                <tbody>
                  {plugins.map(plugin => (
                    <tr key={plugin.id}>
                      <td><strong>{plugin.name}</strong></td>
                      <td><span className="badge badge-primary">{plugin.type}</span></td>
                      <td className="muted">{plugin.version}</td>
                      <td><span className={`badge ${plugin.status === 'enabled' ? 'badge-success' : 'badge-danger'}`}>{plugin.status === 'enabled' ? '已启用' : '已禁用'}</span></td>
                      <td className="muted" style={{ maxWidth: 180, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{plugin.endpoint || '-'}</td>
                      <td className="muted" style={{ maxWidth: 160, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{plugin.description || '-'}</td>
                      <td>
                        <div style={{ display: 'flex', gap: 4, flexWrap: 'wrap' }}>
                          <button className="secondary-button" style={{ minHeight: 30, padding: '3px 8px', fontSize: 11 }} disabled={submitting || dataLoading} onClick={() => startEditPlugin(plugin)}>编辑</button>
                          {plugin.status === 'enabled' ? (
                            <button className="secondary-button" style={{ minHeight: 28, padding: '2px 8px', fontSize: 11 }} disabled={submitting || dataLoading} onClick={() => handleSave(() => api.disablePlugin(plugin.id), '插件已禁用').then(ok => { if (ok) loadData('system'); })}>禁用</button>
                          ) : (
                            <button className="primary-button" style={{ minHeight: 28, padding: '2px 8px', fontSize: 11 }} disabled={submitting || dataLoading} onClick={() => handleSave(() => api.enablePlugin(plugin.id), '插件已启用').then(ok => { if (ok) loadData('system'); })}>启用</button>
                          )}
                          <button className="danger-button" style={{ minHeight: 28, padding: '2px 8px', fontSize: 11 }} disabled={submitting || dataLoading} onClick={() => handleDelete(() => api.deletePlugin(plugin.id), '插件已删除').then(ok => { if (ok) loadData('system'); })}>删除</button>
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      )}
    </>
  );
}
