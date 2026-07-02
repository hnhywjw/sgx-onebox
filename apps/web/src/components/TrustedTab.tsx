import { useState } from 'react';
import type { AttestationRecord, ClusterNode, EnclaveInspection, EnclaveKeyMaterial, EnclaveProfile, EnclaveResource } from '../types';
import { api } from '../api';
import { BulkDeleteControls } from './BulkDeleteControls';
import { rowCheckboxClassName, toggleSelected } from './bulkSelection';

interface TrustedTabProps {
  enclaveResources: EnclaveResource[];
  enclaveProfiles: EnclaveProfile[];
  enclaveKeys: EnclaveKeyMaterial[];
  attestations: AttestationRecord[];
  enclaveInspections: EnclaveInspection[];
  clusterNodes: ClusterNode[];
  activeSubTab: string;
  setActiveSubTab: (key: string) => void;
  loadData: (silentOrTab?: boolean | string) => Promise<void>;
  handleSave: (action: () => Promise<unknown>, successMsg: string) => Promise<boolean>;
  handleDelete: (action: () => Promise<unknown>, successMsg: string) => Promise<boolean>;
  handleAttestationResult: (id: string, status: 'verified' | 'failed') => Promise<boolean>;
  setError: (msg: string) => void;
  setSuccess: (msg: string) => void;
  dataLoading: boolean;
  submitting: boolean;
  formatTime: (value: string) => string;
}

function FieldHint({ children }: { children: string }) {
  return <span className="field-hint">{children}</span>;
}

const initialEnclaveResource = { id: '', nodeId: '', epcSizeMb: 256, epcUsedMb: 0, enclaveCount: 0 };
const initialEnclaveProfile = { id: '', componentId: '', mrEnclave: '', mrSigner: '', quotePolicy: '', sgxEnabled: true };
const initialEnclaveKey = { id: '', name: '', componentId: '', algorithm: 'ECDSA' };

function toEnclaveResourcePayload(resource: typeof initialEnclaveResource, original?: EnclaveResource): EnclaveResource {
  return { ...(original ?? { id: '', status: 'standby' }), ...resource };
}

function toEnclaveProfilePayload(profile: typeof initialEnclaveProfile, original?: EnclaveProfile): EnclaveProfile {
  return { ...(original ?? { id: '', secretsPolicy: '', sgxDevices: [] }), ...profile };
}

function toEnclaveKeyPayload(key: typeof initialEnclaveKey, original?: EnclaveKeyMaterial): EnclaveKeyMaterial {
  return { ...(original ?? { id: '', status: 'active', rotatedAt: '' }), ...key };
}

const ALGORITHMS = ['ECDSA', 'RSA', 'Ed25519', 'AES'];

export function TrustedTab({
  enclaveResources,
  enclaveProfiles,
  enclaveKeys,
  attestations,
  enclaveInspections,
  clusterNodes,
  activeSubTab,
  setActiveSubTab,
  loadData,
  handleSave,
  handleDelete,
  handleAttestationResult,
  setError,
  setSuccess,
  dataLoading,
  submitting,
  formatTime,
}: TrustedTabProps) {
  const [newResource, setNewResource] = useState(initialEnclaveResource);
  const [showNewResourceForm, setShowNewResourceForm] = useState(false);
  const [newProfile, setNewProfile] = useState(initialEnclaveProfile);
  const [showNewProfileForm, setShowNewProfileForm] = useState(false);
  const [newKey, setNewKey] = useState(initialEnclaveKey);
  const [showNewKeyForm, setShowNewKeyForm] = useState(false);
  const [selectedProfiles, setSelectedProfiles] = useState<Set<string>>(new Set());
  const [selectedKeys, setSelectedKeys] = useState<Set<string>>(new Set());

  const startEditResource = (resource: EnclaveResource) => {
    setNewResource({
      id: resource.id,
      nodeId: resource.nodeId,
      epcSizeMb: resource.epcSizeMb,
      epcUsedMb: resource.epcUsedMb,
      enclaveCount: resource.enclaveCount,
    });
    setShowNewResourceForm(true);
  };

  const startEditProfile = (profile: EnclaveProfile) => {
    setNewProfile({
      id: profile.id,
      componentId: profile.componentId,
      mrEnclave: profile.mrEnclave,
      mrSigner: profile.mrSigner,
      quotePolicy: profile.quotePolicy,
      sgxEnabled: profile.sgxEnabled,
    });
    setShowNewProfileForm(true);
  };

  const startEditKey = (key: EnclaveKeyMaterial) => {
    setNewKey({
      id: key.id,
      name: key.name,
      componentId: key.componentId,
      algorithm: key.algorithm,
    });
    setShowNewKeyForm(true);
  };

  const runBulkDelete = (ids: string[], action: (id: string) => Promise<unknown>, label: string, clear: () => void) => {
    if (ids.length === 0) return;
    if (!confirm(`确认删除选中的 ${ids.length} 个${label}？`)) return;
    void handleSave(async () => {
      await Promise.all(ids.map(id => action(id)));
      clear();
    }, `已删除 ${ids.length} 个${label}`);
  };

  const assignedNodeIds = new Set(enclaveResources.filter(r => r.id !== newResource.id).map(r => r.nodeId));
  const availableNodes = clusterNodes.filter(n => !assignedNodeIds.has(n.id));

  const subTabs = [
    { key: 'resources', label: '可信资源' },
    { key: 'profiles', label: '飞地配置' },
    { key: 'keys', label: '密钥管理' },
    { key: 'attestations', label: '远程证明' },
    { key: 'inspections', label: '飞地巡检' },
  ];

  return (
    <>
      <div className="panel top-banner top-banner-soft hero">
        <div className="hero-main">
          <h2>可信计算</h2>
          <p className="muted">可信资源、飞地配置、密钥管理与远程证明</p>
        </div>
        <div className="hero-badges">
          <button className="secondary-button" style={{ minHeight: 36, padding: '6px 14px', fontSize: 13 }} onClick={() => loadData('trusted')} disabled={dataLoading}>刷新数据</button>
        </div>
      </div>

      <div className="sub-tab-bar">
        {subTabs.map(st => (
          <button key={st.key} className={`sub-tab-button${activeSubTab === st.key ? ' sub-tab-active' : ''}`} onClick={() => { setActiveSubTab(st.key); setError(''); setSuccess(''); }}>
            {st.label}
          </button>
        ))}
      </div>

      {/* Resources */}
      {activeSubTab === 'resources' && (
        <div className="panel">
          <div className="section-head" style={{ marginBottom: 12 }}>
            <h3 style={{ margin: 0, fontSize: 16 }}>可信资源 ({enclaveResources.length})</h3>
            <button className="primary-button" onClick={() => { setNewResource(initialEnclaveResource); setShowNewResourceForm(true); }}>添加可信资源</button>
          </div>
          {showNewResourceForm && (
            <div className="panel" style={{ marginBottom: 14, background: 'rgba(239,246,255,0.6)' }}>
              <h4 style={{ margin: '0 0 10px' }}>{newResource.id ? '编辑可信资源' : '添加可信资源'}</h4>
              <div className="form-grid">
                <div className="field-block">
                  <span className="field-label required">节点</span>
                  <select value={newResource.nodeId} title="选择已登记且未绑定可信资源的 SGX 节点" onChange={e => setNewResource(r => ({ ...r, nodeId: e.target.value }))}>
                    <option value="">请选择节点</option>
                    {availableNodes.map(n => (
                      <option key={n.id} value={n.id}>{n.name}</option>
                    ))}
                  </select>
                  <FieldHint>请选择已完成节点登记的 SGX 主机。</FieldHint>
                </div>
                <div className="field-block">
                  <span className="field-label required">EPC 大小 (MB)</span>
                  <input type="number" min={64} value={newResource.epcSizeMb} placeholder="256" title="填写节点 EPC 总容量，单位 MB，最小 64" onChange={e => setNewResource(r => ({ ...r, epcSizeMb: Number(e.target.value) }))} />
                  <FieldHint>示例：256，单位 MB，最小 64。</FieldHint>
                </div>
                <div className="field-block">
                  <span className="field-label">EPC 已用 (MB)</span>
                  <input type="number" value={newResource.epcUsedMb} placeholder="0" title="填写当前 EPC 已使用容量，单位 MB" onChange={e => setNewResource(r => ({ ...r, epcUsedMb: Number(e.target.value) }))} />
                  <FieldHint>新增时通常填写 0。</FieldHint>
                </div>
                <div className="field-block">
                  <span className="field-label">飞地数量</span>
                  <input type="number" value={newResource.enclaveCount} placeholder="0" title="填写当前运行中的飞地数量" onChange={e => setNewResource(r => ({ ...r, enclaveCount: Number(e.target.value) }))} />
                  <FieldHint>新增时通常填写 0。</FieldHint>
                </div>
              </div>
              <div className="action-cell" style={{ marginTop: 12 }}>
                <button className="primary-button" onClick={() => { if (!newResource.nodeId) { setError('请选择节点'); return; } if (!Number.isFinite(newResource.epcSizeMb) || newResource.epcSizeMb < 64) { setError('EPC 大小不能小于 64 MB'); return; } if (newResource.epcUsedMb < 0) { setError('EPC 已用不能小于 0'); return; } if (newResource.enclaveCount < 0) { setError('飞地数量不能小于 0'); return; }               const original = enclaveResources.find(resource => resource.id === newResource.id); const payload = toEnclaveResourcePayload(newResource, original); handleSave(() => newResource.id ? api.updateEnclaveResource(newResource.id, payload) : api.saveEnclaveResource(payload), '可信资源已保存').then(ok => ok && setShowNewResourceForm(false)); }} disabled={submitting || dataLoading}>保存</button>
                <button className="secondary-button" onClick={() => setShowNewResourceForm(false)}>取消</button>
              </div>
            </div>
          )}
          {enclaveResources.length === 0 ? (
            <p className="muted" style={{ textAlign: 'center', padding: '24px 0' }}>暂无可信资源</p>
          ) : (
            <div className="table-wrap">
              <table>
                <thead><tr><th>节点 ID</th><th>EPC 大小 (MB)</th><th>EPC 已用 (MB)</th><th>飞地数量</th><th>状态</th><th>操作</th></tr></thead>
                <tbody>
                  {enclaveResources.map(res => (
                    <tr key={res.id}>
                      <td><strong>{res.nodeId}</strong></td>
                      <td>{res.epcSizeMb}</td>
                      <td>{res.epcUsedMb}</td>
                      <td>{res.enclaveCount}</td>
                      <td><span className={`badge ${res.status === 'healthy' ? 'badge-success' : res.status === 'warning' ? 'badge-warning' : 'badge-info'}`}>{res.status}</span></td>
                      <td><button className="secondary-button" style={{ minHeight: 30, padding: '3px 8px', fontSize: 11 }} onClick={() => startEditResource(res)}>编辑</button> <button className="danger-button" style={{ minHeight: 30, padding: '3px 8px', fontSize: 11 }} onClick={() => handleDelete(() => api.deleteEnclaveResource(res.id), '可信资源已移除')}>移除</button></td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      )}

      {/* Profiles */}
      {activeSubTab === 'profiles' && (
        <div className="panel">
          <div className="section-head" style={{ marginBottom: 12 }}>
            <h3 style={{ margin: 0, fontSize: 16 }}>飞地配置 ({enclaveProfiles.length})</h3>
            <button className="primary-button" onClick={() => { setNewProfile(initialEnclaveProfile); setShowNewProfileForm(true); }}>添加飞地配置</button>
          </div>
          {showNewProfileForm && (
            <div className="panel" style={{ marginBottom: 14, background: 'rgba(239,246,255,0.6)' }}>
              <h4 style={{ margin: '0 0 10px' }}>{newProfile.id ? '编辑飞地配置' : '添加飞地配置'}</h4>
              <div className="form-grid">
                <div className="field-block"><span className="field-label required">组件 ID</span><input value={newProfile.componentId} placeholder="已登记的 enclave 组件 ID" title="填写安全组件部署中隔离级别为 enclave 的组件 ID" onChange={e => setNewProfile(p => ({ ...p, componentId: e.target.value }))} /><FieldHint>需填写隔离级别为 enclave 的组件 ID。</FieldHint></div>
                <div className="field-block"><span className="field-label required">MR Enclave</span><input value={newProfile.mrEnclave} placeholder="64位十六进制度量值" title="填写 SGX 远程证明中的 MRENCLAVE 度量值" onChange={e => setNewProfile(p => ({ ...p, mrEnclave: e.target.value }))} /><FieldHint>填写真实 MRENCLAVE，通常为十六进制字符串。</FieldHint></div>
                <div className="field-block"><span className="field-label required">MR Signer</span><input value={newProfile.mrSigner} placeholder="64位十六进制签名者值" title="填写 SGX 远程证明中的 MRSIGNER 度量值" onChange={e => setNewProfile(p => ({ ...p, mrSigner: e.target.value }))} /><FieldHint>填写真实 MRSIGNER，通常为十六进制字符串。</FieldHint></div>
                <div className="field-block"><span className="field-label">Quote Policy</span><input value={newProfile.quotePolicy} placeholder="require-dcap-verified" title="填写 Quote 验证策略名称或说明" onChange={e => setNewProfile(p => ({ ...p, quotePolicy: e.target.value }))} /><FieldHint>示例：require-dcap-verified。</FieldHint></div>
                <div className="field-block">
                  <label style={{ display: 'flex', alignItems: 'center', gap: 8, cursor: 'pointer' }}>
                    <input className="row-select-checkbox" type="checkbox" checked={newProfile.sgxEnabled} onChange={e => setNewProfile(p => ({ ...p, sgxEnabled: e.target.checked }))} />
                    <span className="field-label" style={{ margin: 0 }}>启用 sgx.enabled 注解</span>
                  </label>
                  <FieldHint>勾选后，组件部署时将注入 sgx.enabled: 'true' annotation，由 K3s 调度器将 Pod 调度到 SGX 节点并启用硬件可信能力。</FieldHint>
                </div>
              </div>
              <div className="action-cell" style={{ marginTop: 12 }}>
                <button className="primary-button" onClick={() => { if (!newProfile.componentId.trim()) { setError('组件 ID 不能为空'); return; } if (!newProfile.mrEnclave.trim()) { setError('MR Enclave 不能为空'); return; } if (!newProfile.mrSigner.trim()) { setError('MR Signer 不能为空'); return; }               const original = enclaveProfiles.find(profile => profile.id === newProfile.id); const payload = toEnclaveProfilePayload(newProfile, original); handleSave(() => newProfile.id ? api.updateEnclave(newProfile.id, payload) : api.createEnclave(payload), '飞地配置已保存').then(ok => ok && setShowNewProfileForm(false)); }} disabled={submitting || dataLoading}>保存</button>
                <button className="secondary-button" onClick={() => setShowNewProfileForm(false)}>取消</button>
              </div>
            </div>
          )}
          {enclaveProfiles.length > 0 && <BulkDeleteControls total={enclaveProfiles.length} selected={selectedProfiles} setSelected={setSelectedProfiles} ids={enclaveProfiles.map(profile => profile.id)} label="飞地配置" onDelete={() => runBulkDelete(Array.from(selectedProfiles), api.deleteEnclave, '飞地配置', () => setSelectedProfiles(new Set()))} />}
          {enclaveProfiles.length === 0 ? (
            <p className="muted" style={{ textAlign: 'center', padding: '24px 0' }}>暂无飞地配置</p>
          ) : (
            <div className="table-wrap">
              <table>
                <thead><tr><th className="row-select-cell"></th><th>组件 ID</th><th>MR Enclave</th><th>MR Signer</th><th>Quote Policy</th><th>SGX</th><th>操作</th></tr></thead>
                <tbody>
                  {enclaveProfiles.map(profile => (
                    <tr key={profile.id}>
                      <td className="row-select-cell"><input className={rowCheckboxClassName} type="checkbox" checked={selectedProfiles.has(profile.id)} onChange={() => toggleSelected(setSelectedProfiles, profile.id)} /></td>
                      <td><strong>{profile.componentId}</strong></td>
                      <td className="muted" style={{ fontFamily: 'monospace', fontSize: 11 }}>{profile.mrEnclave}</td>
                      <td className="muted" style={{ fontFamily: 'monospace', fontSize: 11 }}>{profile.mrSigner}</td>
                      <td>{profile.quotePolicy || '-'}</td>
                      <td>{profile.sgxEnabled !== false ? <span className="status-enabled">enabled</span> : <span className="status-disabled">disabled</span>}</td>
                      <td><button className="secondary-button" style={{ minHeight: 30, padding: '3px 8px', fontSize: 11 }} onClick={() => startEditProfile(profile)}>编辑</button> <button className="danger-button" style={{ minHeight: 30, padding: '3px 8px', fontSize: 11 }} onClick={() => handleDelete(() => api.deleteEnclave(profile.id), '飞地配置已删除')}>删除</button></td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      )}

      {/* Keys */}
      {activeSubTab === 'keys' && (
        <div className="panel">
          <div className="section-head" style={{ marginBottom: 12 }}>
            <h3 style={{ margin: 0, fontSize: 16 }}>密钥管理 ({enclaveKeys.length})</h3>
            <button className="primary-button" onClick={() => { setNewKey(initialEnclaveKey); setShowNewKeyForm(true); }}>添加密钥</button>
          </div>
          {showNewKeyForm && (
            <div className="panel" style={{ marginBottom: 14, background: 'rgba(239,246,255,0.6)' }}>
              <h4 style={{ margin: '0 0 10px' }}>{newKey.id ? '编辑密钥' : '添加密钥'}</h4>
              <div className="form-grid">
                <div className="field-block"><span className="field-label required">名称</span><input value={newKey.name} placeholder="api-signing-key" title="填写密钥名称" onChange={e => setNewKey(k => ({ ...k, name: e.target.value }))} /><FieldHint>示例：api-signing-key。</FieldHint></div>
                <div className="field-block"><span className="field-label required">组件 ID</span><input value={newKey.componentId} placeholder="已登记组件 ID" title="填写需要绑定密钥的组件 ID" onChange={e => setNewKey(k => ({ ...k, componentId: e.target.value }))} /><FieldHint>需填写已登记组件 ID。</FieldHint></div>
                <div className="field-block">
                  <span className="field-label required">算法</span>
                  <select value={newKey.algorithm} title="选择密钥算法" onChange={e => setNewKey(k => ({ ...k, algorithm: e.target.value }))}>
                    {ALGORITHMS.map(a => <option key={a} value={a}>{a}</option>)}
                  </select>
                  <FieldHint>签名类场景可选 ECDSA，通用加密可选 AES。</FieldHint>
                </div>
              </div>
              <div className="action-cell" style={{ marginTop: 12 }}>
                <button className="primary-button" onClick={() => { if (!newKey.name.trim()) { setError('名称不能为空'); return; } if (!newKey.componentId.trim()) { setError('组件 ID 不能为空'); return; } if (!newKey.algorithm.trim()) { setError('算法不能为空'); return; }               const original = enclaveKeys.find(key => key.id === newKey.id); const payload = toEnclaveKeyPayload(newKey, original); handleSave(() => newKey.id ? api.updateEnclaveKey(newKey.id, payload) : api.saveEnclaveKey(payload), '密钥已保存').then(ok => ok && setShowNewKeyForm(false)); }} disabled={submitting || dataLoading}>保存</button>
                <button className="secondary-button" onClick={() => setShowNewKeyForm(false)}>取消</button>
              </div>
            </div>
          )}
          {enclaveKeys.length > 0 && <BulkDeleteControls total={enclaveKeys.length} selected={selectedKeys} setSelected={setSelectedKeys} ids={enclaveKeys.map(key => key.id)} label="密钥" onDelete={() => runBulkDelete(Array.from(selectedKeys), api.deleteEnclaveKey, '密钥', () => setSelectedKeys(new Set()))} />}
          {enclaveKeys.length === 0 ? (
            <p className="muted" style={{ textAlign: 'center', padding: '24px 0' }}>暂无密钥</p>
          ) : (
            <div className="table-wrap">
              <table>
                <thead><tr><th className="row-select-cell"></th><th>名称</th><th>组件 ID</th><th>算法</th><th>状态</th><th>轮换时间</th><th>操作</th></tr></thead>
                <tbody>
                  {enclaveKeys.map(key => (
                    <tr key={key.id}>
                      <td className="row-select-cell"><input className={rowCheckboxClassName} type="checkbox" checked={selectedKeys.has(key.id)} onChange={() => toggleSelected(setSelectedKeys, key.id)} /></td>
                      <td><strong>{key.name}</strong></td>
                      <td className="muted">{key.componentId}</td>
                      <td><span className="badge badge-primary">{key.algorithm}</span></td>
                      <td><span className={`badge ${key.status === 'active' ? 'badge-success' : key.status === 'revoked' ? 'badge-danger' : 'badge-warning'}`}>{key.status}</span></td>
                      <td className="muted">{key.rotatedAt ? formatTime(key.rotatedAt) : '-'}</td>
                      <td><button className="secondary-button" style={{ minHeight: 30, padding: '3px 8px', fontSize: 11 }} onClick={() => startEditKey(key)}>编辑</button> <button className="danger-button" style={{ minHeight: 30, padding: '3px 8px', fontSize: 11 }} onClick={() => handleDelete(() => api.deleteEnclaveKey(key.id), '密钥已删除')}>删除</button></td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      )}

      {/* Attestations */}
      {activeSubTab === 'attestations' && (
        <div className="panel">
          <div className="section-head" style={{ marginBottom: 12 }}>
            <h3 style={{ margin: 0, fontSize: 16 }}>远程证明 ({attestations.length})</h3>
            <button className="primary-button" onClick={() => handleSave(() => api.runAttestation(), '远程证明已发起')} disabled={submitting || dataLoading}>运行证明</button>
          </div>
          {attestations.length === 0 ? (
            <p className="muted" style={{ textAlign: 'center', padding: '24px 0' }}>暂无证明记录</p>
          ) : (
            <div className="table-wrap">
              <table>
                <thead><tr><th>组件 ID</th><th>验证方</th><th>状态</th><th>验证时间</th><th>控制项 ID</th><th>操作</th></tr></thead>
                <tbody>
                  {attestations.map(att => (
                    <tr key={att.id}>
                      <td><strong>{att.componentId}</strong></td>
                      <td className="muted">{att.verifier}</td>
                      <td>
                        <span className={`badge ${att.status === 'verified' ? 'badge-success' : att.status === 'failed' ? 'badge-danger' : 'badge-warning'}`}>
                          {att.status === 'verified' ? '已通过' : att.status === 'failed' ? '未通过' : '待验证'}
                        </span>
                      </td>
                      <td className="muted">{att.verifiedAt ? formatTime(att.verifiedAt) : '-'}</td>
                      <td className="muted">{att.controlId || '-'}</td>
                      <td>
                        {att.status === 'pending' ? (
                          <div style={{ display: 'flex', gap: 4, flexWrap: 'wrap' }}>
                            <button className="primary-button" style={{ minHeight: 30, padding: '3px 8px', fontSize: 11 }} onClick={() => handleAttestationResult(att.id, 'verified')} disabled={submitting || dataLoading}>通过</button>
                            <button className="danger-button" style={{ minHeight: 30, padding: '3px 8px', fontSize: 11 }} onClick={() => handleAttestationResult(att.id, 'failed')} disabled={submitting || dataLoading}>失败</button>
                          </div>
                        ) : (
                          <span className="muted">-</span>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      )}

      {/* Inspections */}
      {activeSubTab === 'inspections' && (
        <div className="panel">
          <div className="section-head" style={{ marginBottom: 12 }}>
            <h3 style={{ margin: 0, fontSize: 16 }}>飞地巡检 ({enclaveInspections.length})</h3>
            <button className="primary-button" onClick={() => handleSave(() => api.runEnclaveInspection(), '飞地巡检已执行')} disabled={submitting || dataLoading}>执行巡检</button>
          </div>
          {enclaveInspections.length === 0 ? (
            <p className="muted" style={{ textAlign: 'center', padding: '24px 0' }}>暂无巡检记录</p>
          ) : (
            <div className="table-wrap">
              <table>
                <thead><tr><th>巡检目标</th><th>状态</th><th>摘要</th><th>检查时间</th></tr></thead>
                <tbody>
                  {enclaveInspections.map(insp => (
                    <tr key={insp.id}>
                      <td><strong>{insp.target}</strong></td>
                      <td>
                        <span className={`badge ${insp.status === 'healthy' ? 'badge-success' : insp.status === 'error' ? 'badge-danger' : insp.status === 'pending' ? 'badge-info' : 'badge-warning'}`}>
                          {insp.status === 'healthy' ? '健康' : insp.status === 'pending' ? '等待执行器' : insp.status === 'error' ? '异常' : '告警'}
                        </span>
                      </td>
                      <td className="muted">{insp.summary}</td>
                      <td className="muted">{insp.checkedAt ? formatTime(insp.checkedAt) : '-'}</td>
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
