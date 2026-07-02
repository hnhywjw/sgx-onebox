import { Bar, BarChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts';
import type { ClusterNode, ComponentDefinition, ComplianceReport, EnclaveResource, SecurityPolicyRule } from '../types';

interface DashboardTabProps {
  clusterNodes: ClusterNode[];
  components: ComponentDefinition[];
  policies: SecurityPolicyRule[];
  complianceReports: ComplianceReport[];
  enclaveResources: EnclaveResource[];
  clusterUpgrade: { status: string; progress?: number; currentVersion?: string } | null;
  mergedAlerts: Array<{ id: string; level: string; source: string; message: string; createdAt: string; status: string }>;
  alertLevelLabel: Record<string, string>;
  alertLevelColor: Record<string, string>;
  loadData: (silentOrTab?: boolean | string) => Promise<void>;
  switchTab: (tabKey: string) => void;
  setActiveSubTab: (key: string) => void;
  dataLoading: boolean;
  formatTime: (value: string) => string;
}

export default function DashboardTab({
  clusterNodes,
  components,
  policies,
  complianceReports,
  enclaveResources,
  clusterUpgrade,
  mergedAlerts,
  alertLevelLabel,
  alertLevelColor,
  loadData,
  switchTab,
  setActiveSubTab,
  dataLoading,
  formatTime,
}: DashboardTabProps) {
  return (
    <>
      <div className="panel top-banner hero">
        <div className="hero-main">
          <h2>控制面概览</h2>
          <p className="muted">SGX 集群整体运行状态</p>
        </div>
        <div className="hero-badges">
          {clusterUpgrade && clusterUpgrade.status === 'running' && (
            <span className="badge badge-warning">升级中 {clusterUpgrade.progress}%</span>
          )}
          <span className="badge badge-primary">版本 {clusterUpgrade?.currentVersion ?? '-'}</span>
          <button
            className="secondary-button"
            style={{ minHeight: 36, padding: '6px 14px', fontSize: 13 }}
            onClick={() => loadData()}
            disabled={dataLoading}
          >
            刷新数据
          </button>
        </div>
      </div>

      <div className="stats-grid">
        <button className="panel summary-card stat-card stat-button" onClick={() => switchTab('deployment')}>
          <span>集群管理</span>
          <strong>{clusterNodes.length}</strong>
          <span className="muted">{clusterNodes.filter(n => n.status === 'ready').length} 在线</span>
        </button>
        <button
          className="panel summary-card stat-card stat-button"
          onClick={() => { switchTab('deployment'); setActiveSubTab('components'); }}
        >
          <span>运行组件</span>
          <strong>{components.filter(c => c.status === 'deployed').length}</strong>
          <span className="muted">共 {components.length} 定义</span>
        </button>
        <button className="panel summary-card stat-card stat-button" onClick={() => switchTab('security')}>
          <span>安全策略</span>
          <strong>{policies.filter(p => p.status === 'active').length}</strong>
          <span className="muted">{policies.length} 条规则</span>
        </button>
        <button
          className="panel summary-card stat-card stat-button"
          onClick={() => { switchTab('security'); setActiveSubTab('compliance-reports'); }}
        >
          <span>合规分数</span>
          <strong>{complianceReports.length > 0 ? `${complianceReports[0].score}%` : '-'}</strong>
          <span className="muted">{complianceReports.length} 份报告</span>
        </button>
        <button className="panel summary-card stat-card stat-button" onClick={() => switchTab('trusted')}>
          <span>Enclave 总数</span>
          <strong>{enclaveResources.reduce((s, r) => s + r.enclaveCount, 0)}</strong>
          <span className="muted">{enclaveResources.filter(r => r.status === 'healthy' || r.status === 'active').length} 活跃</span>
        </button>
        <button className="panel summary-card stat-card stat-button" onClick={() => { setActiveSubTab('nodes'); switchTab('deployment'); }}>
          <span>系统健康度</span>
          <strong>{clusterNodes.length > 0 ? `${Math.round((clusterNodes.filter(n => n.status === 'ready').length / clusterNodes.length) * 100)}%` : '-'}</strong>
          <span className="muted">{clusterNodes.filter(n => n.status === 'ready').length}/{clusterNodes.length} 就绪</span>
        </button>
      </div>

      {enclaveResources.length > 0 && (() => {
        const epcTotal = enclaveResources.reduce((s, r) => s + r.epcSizeMb, 0);
        const epcUsed = enclaveResources.reduce((s, r) => s + r.epcUsedMb, 0);
        const epcPct = epcTotal > 0 ? Math.round((epcUsed / epcTotal) * 100) : 0;
        const totalLabel = epcTotal > 1024 ? `${(epcTotal / 1024).toFixed(1)} GB` : `${epcTotal} MB`;
        return (
          <div className="panel" style={{ marginBottom: 16 }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 8 }}>
              <h3 style={{ margin: 0, fontSize: 15 }}>EPC 可信资源</h3>
              <span className="muted" style={{ fontSize: 13 }}>{epcUsed} MB / {totalLabel}</span>
            </div>
            <div className="progress-track">
              <div className="progress-fill progress-ok" style={{ width: `${epcPct}%` }} />
            </div>
          </div>
        );
      })()}

      <div className="charts-section">
        <div className="panel" style={{ minHeight: 240 }}>
          <h3 style={{ margin: '0 0 12px', fontSize: 15 }}>节点 CPU 使用率</h3>
          <ResponsiveContainer width="100%" height={200}>
            <BarChart
              data={clusterNodes.map(n => ({
                name: n.name.length > 8 ? n.name.slice(0, 8) + '\u2026' : n.name,
                cpu: n.cpuUsage,
              }))}
              maxBarSize={24}
            >
              <CartesianGrid strokeDasharray="3 3" stroke="rgba(148,163,184,0.14)" />
              <XAxis dataKey="name" tick={{ fontSize: 11 }} />
              <YAxis unit="%" tick={{ fontSize: 11 }} />
              <Tooltip />
              <Bar dataKey="cpu" fill="#3b82f6" radius={[6, 6, 0, 0]} name="CPU %" />
            </BarChart>
          </ResponsiveContainer>
        </div>
        <div className="panel" style={{ minHeight: 240 }}>
          <h3 style={{ margin: '0 0 12px', fontSize: 15 }}>节点内存使用率</h3>
          <ResponsiveContainer width="100%" height={200}>
            <BarChart
              data={clusterNodes.map(n => ({
                name: n.name.length > 8 ? n.name.slice(0, 8) + '\u2026' : n.name,
                memory: n.memoryUsage,
              }))}
              maxBarSize={24}
            >
              <CartesianGrid strokeDasharray="3 3" stroke="rgba(148,163,184,0.14)" />
              <XAxis dataKey="name" tick={{ fontSize: 11 }} />
              <YAxis unit="%" tick={{ fontSize: 11 }} />
              <Tooltip />
              <Bar dataKey="memory" fill="#f59e0b" radius={[6, 6, 0, 0]} name="内存 %" />
            </BarChart>
          </ResponsiveContainer>
        </div>
        <div className="panel" style={{ minHeight: 240 }}>
          <h3 style={{ margin: '0 0 12px', fontSize: 15 }}>节点磁盘使用率</h3>
          <ResponsiveContainer width="100%" height={200}>
            <BarChart
              data={clusterNodes.map(n => ({
                name: n.name.length > 8 ? n.name.slice(0, 8) + '\u2026' : n.name,
                disk: n.diskUsage,
              }))}
              maxBarSize={24}
            >
              <CartesianGrid strokeDasharray="3 3" stroke="rgba(148,163,184,0.14)" />
              <XAxis dataKey="name" tick={{ fontSize: 11 }} />
              <YAxis unit="%" tick={{ fontSize: 11 }} />
              <Tooltip />
              <Bar dataKey="disk" fill="#8b5cf6" radius={[6, 6, 0, 0]} name="磁盘 %" />
            </BarChart>
          </ResponsiveContainer>
        </div>
        <div className="panel" style={{ minHeight: 240 }}>
          <h3 style={{ margin: '0 0 12px', fontSize: 15 }}>Pod 分布</h3>
          <ResponsiveContainer width="100%" height={200}>
            <BarChart
              data={clusterNodes.map(n => ({
                name: n.name.length > 8 ? n.name.slice(0, 8) + '\u2026' : n.name,
                pods: n.podCount,
              }))}
              maxBarSize={24}
            >
              <CartesianGrid strokeDasharray="3 3" stroke="rgba(148,163,184,0.14)" />
              <XAxis dataKey="name" tick={{ fontSize: 11 }} />
              <YAxis tick={{ fontSize: 11 }} />
              <Tooltip />
              <Bar dataKey="pods" fill="#10b981" radius={[6, 6, 0, 0]} name="Pod 数" />
            </BarChart>
          </ResponsiveContainer>
        </div>
      </div>

      <div className="panel" style={{ marginBottom: 16 }}>
        <div className="section-head" style={{ marginBottom: 12 }}>
          <h3 style={{ margin: 0, fontSize: 16 }}>最近集群告警</h3>
          <button
            className="secondary-button"
            style={{ minHeight: 36, padding: '6px 14px', fontSize: 13 }}
            onClick={() => { switchTab('system'); setActiveSubTab('alert-events'); }}
          >
            查看全部
          </button>
        </div>
        {mergedAlerts.length === 0 && <p className="muted">暂无告警</p>}
        <div className="grid-two" style={{ gap: 10 }}>
          {mergedAlerts.slice(0, 6).map(alert => (
            <button
              key={alert.id}
              className="panel stat-button"
              style={{ width: '100%', display: 'block' }}
              onClick={() => { switchTab('system'); setActiveSubTab('alert-events'); }}
            >
              <div className="inline-card">
                <div>
                  <span className={`badge ${alertLevelColor[alert.level]}`} style={{ fontSize: 11, marginRight: 8 }}>
                    {alertLevelLabel[alert.level]}
                  </span>
                  <strong style={{ fontSize: 14 }}>{alert.source}</strong>
                </div>
                <span className="muted" style={{ fontSize: 12, whiteSpace: 'nowrap' }}>
                  {formatTime(alert.createdAt)}
                </span>
              </div>
              <p className="muted" style={{ margin: '6px 0 0', fontSize: 13, lineHeight: 1.4 }}>
                {alert.message}
              </p>
            </button>
          ))}
        </div>
      </div>
    </>
  );
}
