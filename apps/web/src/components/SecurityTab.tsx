import { useState, useMemo, useRef, useEffect, useCallback } from 'react';
import type { ClusterNode, ComplianceFinding, ComplianceReport, ComplianceTask, ComponentDefinition, NetworkAttachment, SecurityPolicyRule, TopologyLink, TopologyNode } from '../types';
import { api } from '../api';
import { BulkDeleteControls } from './BulkDeleteControls';
import { rowCheckboxClassName, toggleSelected } from './bulkSelection';

interface SecurityTabProps {
  policies: SecurityPolicyRule[];
  networks: NetworkAttachment[];
  complianceTasks: ComplianceTask[];
  complianceReports: ComplianceReport[];
  clusterNodes: ClusterNode[];
  components: ComponentDefinition[];
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
  topoLinks: TopologyLink[];
  topoEgress: TopologyNode[];
}

function FieldHint({ children }: { children: string }) {
  return <span className="field-hint">{children}</span>;
}

const initialPolicy = { id: '', name: '', category: '', scope: '', status: 'active' as SecurityPolicyRule['status'], isolationLevel: '' };
const initialNetwork = { id: '', name: '', parentNic: '', subnet: '', gateway: '', vlan: '' };
const initialComplianceTask = { id: '', controlId: '', controlName: '', owner: '', note: '', status: 'open' as ComplianceTask['status'], dueAt: '' };
const CLUSTER_NICS = ['eth0', 'eth1', 'eth2', 'ens192', 'ens224', 'eno1', 'eno2', 'bond0', 'bond1', 'bond4', 'flannel.1', 'calieee', 'cni0'];

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

function isValidCIDR(value: string): boolean {
  const [ip, prefixRaw] = value.trim().split('/');
  const prefix = Number(prefixRaw);
  return isValidIPv4(ip) && /^\d+$/.test(prefixRaw ?? '') && Number.isInteger(prefix) && prefix >= 0 && prefix <= 32;
}

type PolicyForm = typeof initialPolicy;
type ComplianceTaskForm = typeof initialComplianceTask;

function toSecurityPolicyPayload(policy: PolicyForm, original?: SecurityPolicyRule): SecurityPolicyRule {
  return { ...(original ?? { id: '', mode: 'enforce', targets: [], updatedAt: '' }), ...policy };
}

function toNetworkPayload(network: Omit<NetworkAttachment, 'id'> & Partial<Pick<NetworkAttachment, 'id'>>, original?: NetworkAttachment): NetworkAttachment {
  return { ...(original ?? { id: '', attachedComponents: [] }), ...network } as NetworkAttachment;
}

function toTopologyNodePayload(node: Omit<TopologyNode, 'refId'> & Partial<Pick<TopologyNode, 'refId'>>): TopologyNode {
  return { refId: node.id, ...node };
}

function toComplianceTaskPayload(task: ComplianceTaskForm): ComplianceTask {
  return { ...task };
}

interface TopoNode {
  id: string;
  kind: 'node' | 'component' | 'egress' | 'network';
  label: string;
  zone: string;
  hostNodeId?: string;
  nicName?: string;
}

interface TopoLink {
  id: string;
  source: string;
  target: string;
}

const computeLayout = (nodes: TopoNode[], links: TopoLink[], width = 600, height = 450) => {
  const positions = nodes.map(() => ({
    x: width / 2 + (Math.random() - 0.5) * 200,
    y: height / 2 + (Math.random() - 0.5) * 200,
  }));

  for (let iter = 0; iter < 100; iter++) {
    for (let i = 0; i < nodes.length; i++) {
      let fx = 0, fy = 0;
      for (let j = 0; j < nodes.length; j++) {
        if (i === j) continue;
        const dx = positions[i].x - positions[j].x;
        const dy = positions[i].y - positions[j].y;
        const dist = Math.max(Math.sqrt(dx * dx + dy * dy), 1);
        const force = 500 / (dist * dist);
        fx += (dx / dist) * force;
        fy += (dy / dist) * force;
      }
      positions[i].x += fx * 0.1;
      positions[i].y += fy * 0.1;
    }

    for (const link of links) {
      const si = nodes.findIndex(n => n.id === link.source);
      const ti = nodes.findIndex(n => n.id === link.target);
      if (si < 0 || ti < 0) continue;
      const dx = positions[ti].x - positions[si].x;
      const dy = positions[ti].y - positions[si].y;
      const dist = Math.max(Math.sqrt(dx * dx + dy * dy), 1);
      const force = (dist - 100) * 0.01;
      positions[si].x += (dx / dist) * force * 0.5;
      positions[si].y += (dy / dist) * force * 0.5;
      positions[ti].x -= (dx / dist) * force * 0.5;
      positions[ti].y -= (dy / dist) * force * 0.5;
    }

    for (let i = 0; i < nodes.length; i++) {
      positions[i].x += (width / 2 - positions[i].x) * 0.01;
      positions[i].y += (height / 2 - positions[i].y) * 0.01;
    }
  }

  return positions;
};

interface NodePosition { x: number; y: number; }

function shortenEdge(x1: number, y1: number, x2: number, y2: number, r: number) {
  const dx = x2 - x1;
  const dy = y2 - y1;
  const dist = Math.sqrt(dx * dx + dy * dy);
  if (dist < 1) return { x: x2, y: y2 };
  return { x: x2 - (dx / dist) * r, y: y2 - (dy / dist) * r };
}

function formatTraffic(bytesPerSec: number): string {
  if (bytesPerSec >= 1_000_000_000) return `${(bytesPerSec / 1_000_000_000).toFixed(1)} Gb/s`;
  if (bytesPerSec >= 1_000_000) return `${(bytesPerSec / 1_000_000).toFixed(1)} Mb/s`;
  if (bytesPerSec >= 1_000) return `${(bytesPerSec / 1_000).toFixed(1)} kb/s`;
  if (bytesPerSec <= 0) return '0 b/s';
  return `${bytesPerSec.toFixed(1)} b/s`;
}

function TopologyGraph({ 
  nodes, links, clusterNodes, selectedLinkId, onSelectLink, onDeleteLink 
}: { 
  nodes: TopoNode[]; 
  links: TopoLink[];
  clusterNodes: ClusterNode[];
  selectedLinkId: string | null;
  onSelectLink: (linkId: string | null) => void;
  onDeleteLink?: (linkId: string) => void;
}) {
  const nodeTraffic = useMemo(() => {
    const m = new Map<string, number>();
    for (const cn of clusterNodes) {
      m.set(cn.id, cn.txRate || 0);
    }
    return m;
  }, [clusterNodes]);

  const resolveLinkTraffic = useCallback((link: TopoLink): number => {
    const src = nodes.find(n => n.id === link.source);
    if (src?.kind === 'egress' && src.hostNodeId) return nodeTraffic.get(src.hostNodeId) ?? 0;
    const tgt = nodes.find(n => n.id === link.target);
    if (tgt?.kind === 'egress' && tgt.hostNodeId) return nodeTraffic.get(tgt.hostNodeId) ?? 0;
    const srcNet = nodes.find(n => n.id === link.source);
    if (srcNet?.kind === 'network') {
      for (const cn of clusterNodes) {
        if (cn.nicName && nodes.some(n => n.id === link.target)) return nodeTraffic.get(cn.id) ?? 0;
      }
    }
    return nodeTraffic.get(link.source) ?? nodeTraffic.get(link.target) ?? 0;
  }, [nodes, nodeTraffic, clusterNodes]);
  const [positions, setPositions] = useState<NodePosition[]>(() => {
    try {
      const saved = localStorage.getItem('topo-positions');
      if (saved) {
        const savedMap = JSON.parse(saved) as Record<string, { x: number; y: number }>;
        const restored = nodes.map(n => savedMap[n.id] || { x: 0, y: 0 });
        if (restored.some(p => p.x !== 0 || p.y !== 0)) return restored;
      }
    } catch { /* ignore */ }
    return computeLayout(nodes, links);
  });
  const [dragIdx, setDragIdx] = useState<number | null>(null);
  const [tooltip, setTooltip] = useState<{ x: number; y: number; node: TopoNode } | null>(null);
  const [contextMenu, setContextMenu] = useState<{ x: number; y: number; linkId: string } | null>(null);
  const [hoveredLinkId, setHoveredLinkId] = useState<string | null>(null);
  const svgRef = useRef<SVGSVGElement>(null);

  useEffect(() => {
    setPositions(prev => {
      const savedMap: Record<string, { x: number; y: number }> = {};
      try {
        const saved = localStorage.getItem('topo-positions');
        if (saved) Object.assign(savedMap, JSON.parse(saved));
      } catch { /* ignore */ }
      const existing = nodes.every(n => savedMap[n.id]);
      if (existing && nodes.length === prev.length) return prev;
      const fresh = computeLayout(nodes, links);
      const result = nodes.map((n, i) => {
        if (savedMap[n.id]) return savedMap[n.id];
        return fresh[i] || { x: 300, y: 225 };
      });
      return result;
    });
  }, [nodes, links]);

  const positionsRef = useRef(positions);
  positionsRef.current = positions;

  const persistPositions = useCallback(() => {
    const map: Record<string, { x: number; y: number }> = {};
    nodes.forEach((n, i) => { map[n.id] = positionsRef.current[i] || { x: 0, y: 0 }; });
    localStorage.setItem('topo-positions', JSON.stringify(map));
  }, [nodes]);

  const handleMouseDown = (idx: number) => (e: React.MouseEvent) => {
    e.stopPropagation();
    setDragIdx(idx);
  };

  const handleMouseMove = (e: React.MouseEvent) => {
    if (dragIdx === null || !svgRef.current) return;
    const rect = svgRef.current.getBoundingClientRect();
    const x = ((e.clientX - rect.left) / rect.width) * 600;
    const y = ((e.clientY - rect.top) / rect.height) * 450;
    setPositions(prev => prev.map((p, i) => i === dragIdx ? { x, y } : p));
  };

  const handleMouseUp = () => {
    if (dragIdx !== null) persistPositions();
    setDragIdx(null);
  };

  const nodeColor = (kind: TopoNode['kind']) => 
    kind === 'egress' ? '#f59e0b' : kind === 'node' ? '#3b82f6' : kind === 'network' ? '#06b6d4' : '#22c55e';
  const nodeRadius = (kind: TopoNode['kind']) => kind === 'egress' ? 14 : kind === 'network' ? 13 : 11;

  const getNodeRadius = (id: string) => {
    const n = nodes.find(nd => nd.id === id);
    return n ? nodeRadius(n.kind) : 11;
  };

  return (
    <div style={{ position: 'relative' }}>
      <svg ref={svgRef} viewBox="0 0 600 450" className="topology-svg"
        style={{ width: '100%', height: 450, border: '1px solid #e2e8f0', borderRadius: 8, background: '#f8fafc', cursor: dragIdx !== null ? 'grabbing' : 'default' }}
        onMouseMove={handleMouseMove} onMouseUp={handleMouseUp} onMouseLeave={() => { handleMouseUp(); setHoveredLinkId(null); }}
        onClick={() => { onSelectLink(null); setContextMenu(null); }}>
        <defs>
        </defs>
        {links.map((link) => {
          const si = nodes.findIndex(n => n.id === link.source);
          const ti = nodes.findIndex(n => n.id === link.target);
          if (si < 0 || ti < 0) return null;
          const x1 = positions[si].x, y1 = positions[si].y;
          const x2 = positions[ti].x, y2 = positions[ti].y;
          const tr = getNodeRadius(link.target);
          const end = shortenEdge(x1, y1, x2, y2, tr + 2);
          const isSelected = selectedLinkId === link.id;
          const isHovered = hoveredLinkId === link.id;
          const dx = end.x - x1, dy = end.y - y1;
          const len = Math.sqrt(dx * dx + dy * dy);
          const ux = len > 0 ? dx / len : 0, uy = len > 0 ? dy / len : 0;
          const arrowSize = 8, arrowWing = 3.5;
          const ax = end.x, ay = end.y;
          const lx = end.x - ux * arrowSize + uy * arrowWing;
          const ly = end.y - uy * arrowSize - ux * arrowWing;
          const rx = end.x - ux * arrowSize - uy * arrowWing;
          const ry = end.y - uy * arrowSize + ux * arrowWing;
          const mx = (x1 + end.x) / 2 + uy * 8;
          const my = (y1 + end.y) / 2 - ux * 8;
          const traffic = resolveLinkTraffic(link);
          const edgeColor = isSelected ? '#3b82f6' : isHovered ? '#ef4444' : '#94a3b8';
          const edgeWidth = isSelected ? 2.8 : isHovered ? 2.5 : 1.8;
          return (
            <g key={link.id}>
              <line x1={x1} y1={y1} x2={ax} y2={ay}
                stroke={edgeColor} strokeWidth={edgeWidth}
                style={{ transition: 'stroke 0.15s, stroke-width 0.15s' }} />
              <polygon points={`${ax},${ay} ${lx},${ly} ${rx},${ry}`}
                fill={edgeColor}
                style={{ transition: 'fill 0.15s' }} />
              <line className="topo-flow" x1={x1} y1={y1} x2={ax} y2={ay} />
              <text x={mx} y={my} textAnchor="middle" fontSize={10}
                stroke="#fff" strokeWidth={3} paintOrder="stroke" fill="#334155"
                style={{ pointerEvents: 'none', userSelect: 'none', fontWeight: 600 }}>
                {formatTraffic(traffic)}
              </text>
              <line x1={x1} y1={y1} x2={end.x} y2={end.y}
                stroke="transparent" strokeWidth={14} style={{ cursor: 'pointer' }}
                onMouseEnter={() => setHoveredLinkId(link.id)}
                onMouseLeave={() => setHoveredLinkId(null)}
                onClick={(e) => { e.stopPropagation(); onSelectLink(link.id); }}
                onContextMenu={(e) => { e.preventDefault(); e.stopPropagation(); setContextMenu({ x: e.clientX, y: e.clientY, linkId: link.id }); }} />
            </g>
          );
        })}
        {nodes.map((node, i) => (
          <g key={node.id}>
            <circle className="topo-node" cx={positions[i].x} cy={positions[i].y}
              r={nodeRadius(node.kind)} fill={nodeColor(node.kind)} stroke="#fff" strokeWidth={2}
              style={{ cursor: 'grab' }}
              onMouseDown={handleMouseDown(i)}
              onMouseEnter={() => {
                const rect = svgRef.current!.getBoundingClientRect();
                setTooltip({ x: positions[i].x * (rect.width / 600) + rect.left,
                  y: positions[i].y * (rect.height / 450) + rect.top - 40, node });
              }}
              onMouseLeave={() => setTooltip(null)} />
            <text className="topo-label" x={positions[i].x + nodeRadius(node.kind) + 4} y={positions[i].y + 4}>
              {node.kind === 'egress' ? `${node.label}` : node.label}
            </text>
            {node.kind === 'egress' && node.nicName && (
              <text x={positions[i].x + nodeRadius(node.kind) + 4} y={positions[i].y + 16}
                fontSize={10} fill="#94a3b8">{node.nicName}</text>
            )}
          </g>
        ))}
      </svg>
      {tooltip && (
        <div style={{ position: 'fixed', left: tooltip.x, top: tooltip.y,
          background: '#1e293b', color: '#f1f5f9', padding: '6px 10px', borderRadius: 6,
          fontSize: 12, pointerEvents: 'none', zIndex: 9999, whiteSpace: 'nowrap',
          transform: 'translate(-50%, -100%)' }}>
          <div><strong>{tooltip.node.label}</strong></div>
          <div>类型: {tooltip.node.kind === 'egress' ? '物理出口' : tooltip.node.kind === 'node' ? '集群节点' : tooltip.node.kind === 'network' ? '集群网络' : '组件'} | 状态: {tooltip.node.zone}</div>
          {tooltip.node.nicName && <div>网卡: {tooltip.node.nicName}</div>}
        </div>
      )}
      {contextMenu && (
        <div style={{ position: 'fixed', left: contextMenu.x, top: contextMenu.y, zIndex: 10000,
          background: '#fff', border: '1px solid #e2e8f0', borderRadius: 8, boxShadow: '0 8px 24px rgba(15,23,42,0.15)',
          padding: 4, minWidth: 120 }}
          onClick={(e) => e.stopPropagation()}>
          <button style={{ display: 'block', width: '100%', padding: '6px 12px', border: 'none', background: 'none',
            fontSize: 13, textAlign: 'left', cursor: 'pointer', borderRadius: 4, color: '#ef4444' }}
            onClick={() => { onDeleteLink?.(contextMenu.linkId); setContextMenu(null); onSelectLink(null); }}>
            删除连线
          </button>
        </div>
      )}
      {/* Close context menu on outside click */}
      {contextMenu && <div style={{ position: 'fixed', inset: 0, zIndex: 9999 }} onClick={() => setContextMenu(null)} />}
    </div>
  );
}

export function SecurityTab({
  policies,
  networks,
  complianceTasks,
  complianceReports,
  clusterNodes,
  components,
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
  topoLinks,
  topoEgress,
}: SecurityTabProps) {
  const [newPolicy, setNewPolicy] = useState(initialPolicy);
  const [showNewPolicyForm, setShowNewPolicyForm] = useState(false);
  const [newNetwork, setNewNetwork] = useState(initialNetwork);
  const [showNewNetworkForm, setShowNewNetworkForm] = useState(false);
  const [newComplianceTask, setNewComplianceTask] = useState(initialComplianceTask);
  const [showNewTaskForm, setShowNewTaskForm] = useState(false);
  const [selectedReport, setSelectedReport] = useState<ComplianceReport | null>(null);
  const [displayedReports, setDisplayedReports] = useState<ComplianceReport[]>([]);
  const prevReportIdsRef = useRef<Set<string>>(new Set());
  useEffect(() => {
    if (complianceReports.length === 0) return;
    const prevIds = prevReportIdsRef.current;
    const newIds = new Set(complianceReports.map(r => r.id));
    if (prevIds.size !== newIds.size || ![...newIds].every(id => prevIds.has(id))) {
      setDisplayedReports(complianceReports);
    }
    prevReportIdsRef.current = newIds;
  }, [complianceReports]);
  const [selectedPolicies, setSelectedPolicies] = useState<Set<string>>(new Set());
  const [selectedNetworks, setSelectedNetworks] = useState<Set<string>>(new Set());
  const [selectedComplianceTasks, setSelectedComplianceTasks] = useState<Set<string>>(new Set());
  const [networkEditingAttached, setNetworkEditingAttached] = useState<string[]>([]);

  const runBulkDelete = (ids: string[], action: (id: string) => Promise<unknown>, label: string, clear: () => void) => {
    if (ids.length === 0) return;
    if (!confirm(`确认删除选中的 ${ids.length} 个${label}？`)) return;
    void handleSave(async () => {
      await Promise.all(ids.map(id => action(id)));
      clear();
    }, `已删除 ${ids.length} 个${label}`);
  };

  const exportReport = async (reportId: string, format: 'csv' | 'html') => {
    try {
      await api.exportComplianceReport(reportId, format);
      setSuccess(format === 'csv' ? 'CSV 已开始下载' : '报表已开始下载');
    } catch (err) {
      setError((err as Error).message || '导出失败');
    }
  };

  const deleteLocalReport = (id: string) => {
    setDisplayedReports(prev => prev.filter(r => r.id !== id));
    setSuccess('报告已删除');
  };

  const startEditPolicy = (policy: SecurityPolicyRule) => {
    setNewPolicy({
      id: policy.id,
      name: policy.name,
      category: policy.category,
      scope: policy.scope,
      status: policy.status,
      isolationLevel: policy.isolationLevel || '',
    });
    setShowNewPolicyForm(true);
  };

  const startEditNetwork = (network: NetworkAttachment) => {
    setNewNetwork({
      id: network.id,
      name: network.name,
      parentNic: network.parentNic || '',
      subnet: network.subnet,
      gateway: network.gateway,
      vlan: String(network.vlanId),
    });
    setNetworkEditingAttached(network.attachedComponents || []);
    setShowNewNetworkForm(true);
  };

  const startEditComplianceTask = (task: ComplianceTask) => {
    setNewComplianceTask({
      id: task.id,
      controlId: task.controlId,
      controlName: task.controlName,
      owner: task.owner || '',
      note: task.note || '',
      status: task.status,
      dueAt: task.dueAt || '',
    });
    setShowNewTaskForm(true);
  };

  const allTopoNodes = [
    ...components.map(c => ({ id: c.id, kind: 'component' as const, label: c.name, zone: c.status })),
  ];

  const networkTopoNodes: TopoNode[] = useMemo(() =>
    networks.map(n => ({ id: n.id, kind: 'network' as const, label: n.name, zone: n.bridge || 'active' })),
  [networks]);

  const topologyGraphLinks: { id: string; source: string; target: string }[] = useMemo(() => {
    const networkLinks: { id: string; source: string; target: string }[] = [];
    for (const net of networks) {
      for (const compId of net.attachedComponents || []) {
        networkLinks.push({ id: `net-auto-${net.id}-${compId}`, source: net.id, target: compId });
      }
    }
    return networkLinks;
  }, [networks]);

  // Topology state (from backend props)
  const [egressNodes, setEgressNodes] = useState<TopoNode[]>(() =>
    topoEgress.map(n => ({ ...n, kind: n.kind as TopoNode['kind'], hostNodeId: n.hostNodeId, nicName: n.nicName })),
  );
  const [topologyLinks, setTopologyLinks] = useState<TopoLink[]>(() => {
    const saved: TopoLink[] = topoLinks.map(l => ({ id: l.id, source: l.source, target: l.target }));
    const merged = [...saved];
    for (const al of topologyGraphLinks) {
      if (!saved.some(sl => sl.id === al.id)) merged.push(al);
    }
    return merged;
  });
  useEffect(() => {
    setEgressNodes(topoEgress.map(n => ({ ...n, kind: n.kind as TopoNode['kind'], hostNodeId: n.hostNodeId, nicName: n.nicName })));
  }, [topoEgress]);
  useEffect(() => {
    const saved = topoLinks.map(l => ({ id: l.id, source: l.source, target: l.target }));
    const merged = new Map<string, TopoLink>();
    for (const sl of saved) merged.set(sl.id, sl);
    for (const al of topologyGraphLinks) {
      if (!merged.has(al.id)) merged.set(al.id, al);
    }
    setTopologyLinks([...merged.values()]);
  }, [topoLinks, topologyGraphLinks]);
  const [egressForm, setEgressForm] = useState({ hostNodeId: '', nicName: '' });
  const [linkForm, setLinkForm] = useState({ source: '', target: '' });
  const [selectedLinkId, setSelectedLinkId] = useState<string | null>(null);

  const scoreColor = (score: number) => (score >= 80 ? 'badge-success' : score >= 60 ? 'badge-warning' : 'badge-danger');

  const findingsDistribution = useMemo(() => {
    const counts = { high: 0, medium: 0, low: 0 };
    let total = 0;
    displayedReports.forEach(r => {
      r.findings?.forEach(f => {
        counts[f.level]++;
        total++;
      });
    });
    return { ...counts, total };
  }, [displayedReports]);

  const avgScore = useMemo(() => {
    if (displayedReports.length === 0) return 0;
    const sum = displayedReports.reduce((acc, r) => acc + r.score, 0);
    return Math.round(sum / displayedReports.length);
  }, [displayedReports]);

  return (
    <>
      <div className="panel top-banner top-banner-soft hero">
        <div className="hero-main">
          <h2>安全合规</h2>
          <p className="muted">安全策略、网络隔离、合规扫描与报告管理</p>
        </div>
        <div className="hero-badges">
          <button className="secondary-button" style={{ minHeight: 36, padding: '6px 14px', fontSize: 13 }} onClick={() => loadData('security')} disabled={dataLoading}>刷新数据</button>
        </div>
      </div>

      <div className="sub-tab-bar">
        {[
          { key: 'policies', label: '安全策略' },
          { key: 'networks', label: '网络隔离' },
          { key: 'topology-viz', label: '拓扑视图' },
          { key: 'compliance-run', label: '合规扫描' },
          { key: 'compliance-focus', label: '合规分析' },
          { key: 'compliance-tasks', label: '合规任务' },
          { key: 'compliance-reports', label: '合规报告' },
        ].map(st => (
          <button key={st.key} className={`sub-tab-button${activeSubTab === st.key ? ' sub-tab-active' : ''}`} onClick={() => { setActiveSubTab(st.key); setError(''); setSuccess(''); }}>
            {st.label}
          </button>
        ))}
      </div>

      {/* Policies */}
      {activeSubTab === 'policies' && (
        <div className="panel">
          <div className="section-head" style={{ marginBottom: 12 }}>
            <h3 style={{ margin: 0, fontSize: 16 }}>安全策略 ({policies.length})</h3>
            <button className="primary-button" onClick={() => { setNewPolicy(initialPolicy); setShowNewPolicyForm(true); }}>新增策略</button>
          </div>
          {showNewPolicyForm && (
            <div className="panel" style={{ marginBottom: 14, background: 'rgba(239,246,255,0.6)' }}>
              <h4 style={{ margin: '0 0 10px' }}>{newPolicy.id ? '编辑安全策略' : '新增安全策略'}</h4>
              <div className="form-grid">
                <div className="field-block"><span className="field-label required">名称</span><input value={newPolicy.name} placeholder="deny-egress-public" title="填写安全策略名称" onChange={e => setNewPolicy(p => ({ ...p, name: e.target.value }))} /><FieldHint>示例：deny-egress-public。</FieldHint></div>
                <div className="field-block"><span className="field-label required">分类</span><input value={newPolicy.category} placeholder="network" title="填写策略分类，如 network、runtime、attestation" onChange={e => setNewPolicy(p => ({ ...p, category: e.target.value }))} /><FieldHint>示例：network、runtime、attestation。</FieldHint></div>
                <div className="field-block"><span className="field-label required">作用范围</span><input value={newPolicy.scope} placeholder="namespace/default" title="填写策略生效范围" onChange={e => setNewPolicy(p => ({ ...p, scope: e.target.value }))} /><FieldHint>示例：namespace/default 或 component/secure-api。</FieldHint></div>
                <div className="field-block"><span className="field-label">状态</span><select value={newPolicy.status} title="active 会尝试发布；staged 仅保存草稿；disabled 不下发" onChange={e => setNewPolicy(p => ({ ...p, status: e.target.value as SecurityPolicyRule['status'] }))}><option value="active">active</option><option value="staged">staged</option><option value="disabled">disabled</option></select><FieldHint>首次录入建议 staged，确认后再发布为 active。</FieldHint></div>
                <div className="field-block"><span className="field-label">隔离级别管控</span><select value={newPolicy.isolationLevel} title="设置后，NetworkPolicy 将按隔离级别限制入站流量" onChange={e => setNewPolicy(p => ({ ...p, isolationLevel: e.target.value }))}><option value="">不限制</option><option value="enclave">enclave（仅允许同级别入站）</option><option value="hardened">hardened（允许 enclave + hardened 入站）</option><option value="standard">standard（允许所有级别入站）</option></select><FieldHint>按组件隔离级别控制入站流量。</FieldHint></div>
              </div>
              <div className="action-cell" style={{ marginTop: 12 }}>
                <button className="primary-button" disabled={submitting || dataLoading} onClick={async () => {
                  if (!newPolicy.name.trim()) { setError('请输入策略名称'); return; }
                  if (!newPolicy.category.trim()) { setError('请输入策略分类'); return; }
                  if (!newPolicy.scope.trim()) { setError('请输入策略作用范围'); return; }
                  const original = policies.find(policy => policy.id === newPolicy.id);
                  const ok = await handleSave(
                    () => newPolicy.id
                      ? api.updateSecurityPolicy(newPolicy.id, toSecurityPolicyPayload(newPolicy, original))
                      : api.saveSecurityPolicy(toSecurityPolicyPayload(newPolicy, original)),
                    '安全策略已保存',
                  );
                  if (ok) setShowNewPolicyForm(false);
                }}>保存</button>
                <button className="secondary-button" onClick={() => setShowNewPolicyForm(false)}>取消</button>
              </div>
            </div>
          )}
          {policies.length > 0 && <BulkDeleteControls total={policies.length} selected={selectedPolicies} setSelected={setSelectedPolicies} ids={policies.map(policy => policy.id)} label="策略" onDelete={() => runBulkDelete(Array.from(selectedPolicies), api.deleteSecurityPolicy, '策略', () => setSelectedPolicies(new Set()))} />}
          {policies.length === 0 ? (
            <p className="muted" style={{ textAlign: 'center', padding: '24px 0' }}>暂无策略</p>
          ) : (
            <div className="table-wrap">
              <table>
                <thead><tr><th className="row-select-cell"></th><th>名称</th><th>分类</th><th>范围</th><th>模式</th><th>状态</th><th>更新时间</th><th>操作</th></tr></thead>
                <tbody>
                  {policies.map(policy => (
                    <tr key={policy.id}>
                      <td className="row-select-cell"><input className={rowCheckboxClassName} type="checkbox" checked={selectedPolicies.has(policy.id)} onChange={() => toggleSelected(setSelectedPolicies, policy.id)} /></td>
                      <td><strong>{policy.name}</strong></td>
                      <td><span className="badge badge-info">{policy.category}</span></td>
                      <td className="muted">{policy.scope}</td>
                      <td className="muted">{policy.mode}</td>
                      <td><span className={`badge ${policy.status === 'active' ? 'badge-success' : policy.status === 'disabled' ? 'badge-danger' : 'badge-warning'}`}>{policy.status}</span></td>
                      <td className="muted">{formatTime(policy.updatedAt)}</td>
                      <td>
                        <div style={{ display: 'flex', gap: 4, flexWrap: 'wrap' }}>
                          <button className="secondary-button" style={{ minHeight: 30, padding: '3px 8px', fontSize: 11 }} disabled={submitting || dataLoading} onClick={() => startEditPolicy(policy)}>编辑</button>
                          {policy.status === 'active' ? (
                            <button className="secondary-button" style={{ minHeight: 30, padding: '3px 8px', fontSize: 11 }} disabled={submitting || dataLoading} onClick={() => handleSave(() => api.saveSecurityPolicy({ ...policy, status: 'disabled' }), '策略已禁用')}>禁用</button>
                          ) : (
                            <button className="primary-button" style={{ minHeight: 30, padding: '3px 8px', fontSize: 11 }} disabled={submitting || dataLoading} onClick={() => handleSave(() => api.saveSecurityPolicy({ ...policy, status: 'active' }), '策略发布请求已提交')}>发布</button>
                          )}
                          <button className="danger-button" style={{ minHeight: 30, padding: '3px 8px', fontSize: 11 }} disabled={submitting || dataLoading} onClick={() => handleDelete(() => api.deleteSecurityPolicy(policy.id), '策略已删除')}>删除</button>
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

      {/* Networks */}
      {activeSubTab === 'networks' && (
        <div className="panel">
          <div className="section-head" style={{ marginBottom: 12 }}>
            <h3 style={{ margin: 0, fontSize: 16 }}>网络隔离 ({networks.length})</h3>
            <button className="primary-button" onClick={() => { setNewNetwork(initialNetwork); setNetworkEditingAttached([]); setShowNewNetworkForm(true); }}>新增网络</button>
          </div>
          {showNewNetworkForm && (
            <div className="panel" style={{ marginBottom: 14, background: 'rgba(239,246,255,0.6)' }}>
              <h4 style={{ margin: '0 0 10px' }}>{newNetwork.id ? '编辑集群网络' : '新增集群网络'}</h4>
              <div className="form-grid">
                <div className="field-block">
                  <span className="field-label required">网络名称</span>
                  <input placeholder="prod-external" title="填写网络名称，会用于生成网桥名称" value={newNetwork.name} onChange={e => setNewNetwork(n => ({ ...n, name: e.target.value }))} />
                  <FieldHint>示例：prod-external，会生成 br-prod-external。</FieldHint>
                </div>
                <div className="field-block">
                  <span className="field-label required">集群网卡</span>
                  <select value={newNetwork.parentNic} title="选择所有节点上统一存在的物理网卡" onChange={e => setNewNetwork(n => ({ ...n, parentNic: e.target.value }))}>
                    <option value="">选择集群统一网卡</option>
                    {CLUSTER_NICS.map(nic => <option key={nic} value={nic}>{nic}</option>)}
                  </select>
                  <span className="muted" style={{ fontSize: 10, marginTop: 2 }}>集群所有节点共用此物理网卡</span>
                </div>
                <div className="field-block">
                  <span className="field-label required">子网 (CIDR)</span>
                  <input placeholder="10.0.0.0/24" title="填写 CIDR 子网，例如 10.0.0.0/24" value={newNetwork.subnet} onChange={e => setNewNetwork(n => ({ ...n, subnet: e.target.value }))} />
                  <FieldHint>必须是 CIDR 格式，示例：10.0.0.0/24。</FieldHint>
                </div>
                <div className="field-block">
                  <span className="field-label required">网关</span>
                  <input placeholder="10.0.0.1" title="填写子网网关 IPv4 地址" value={newNetwork.gateway} onChange={e => setNewNetwork(n => ({ ...n, gateway: e.target.value }))} />
                  <FieldHint>示例：10.0.0.1，需位于所填子网内。</FieldHint>
                </div>
                <div className="field-block">
                  <span className="field-label required">VLAN ID</span>
                  <input placeholder="120" title="填写 1 到 4094 之间的 VLAN ID" value={newNetwork.vlan} onChange={e => setNewNetwork(n => ({ ...n, vlan: e.target.value.replace(/\D/g, '').slice(0, 4) }))} />
                  <FieldHint>范围 1-4094，示例：120。</FieldHint>
                </div>
              </div>
              <div className="action-cell" style={{ marginTop: 12 }}>
                <button className="primary-button" disabled={submitting || dataLoading} onClick={async () => {
                  if (!newNetwork.name.trim()) { setError('请输入网络名称'); return; }
                  if (!newNetwork.parentNic.trim()) { setError('请选择集群网卡'); return; }
                  if (!newNetwork.subnet.trim()) { setError('请输入子网'); return; }
                  if (!isValidCIDR(newNetwork.subnet)) { setError('请输入正确的 CIDR 格式，如 10.0.0.0/24'); return; }
                  if (!newNetwork.gateway.trim()) { setError('请输入网关'); return; }
                  if (!isValidIPv4(newNetwork.gateway)) { setError('请输入正确的网关 IP 地址'); return; }
                  const vlanId = Number(newNetwork.vlan);
                  if (!Number.isInteger(vlanId) || vlanId < 1 || vlanId > 4094) { setError('VLAN ID 必须在 1 到 4094 之间'); return; }
                  const original = networks.find(network => network.id === newNetwork.id);
                  const payload = toNetworkPayload({
                    id: newNetwork.id,
                    name: newNetwork.name,
                    bridge: original?.bridge || `br-${newNetwork.name || 'net'}`,
                    parentNic: newNetwork.parentNic,
                    vlanId,
                    subnet: newNetwork.subnet,
                    gateway: newNetwork.gateway,
                    attachedComponents: networkEditingAttached,
                  }, original);
                  const ok = await handleSave(
                    () => newNetwork.id ? api.updateNetwork(newNetwork.id, payload) : api.createNetwork(payload),
                    '集群网络已保存',
                  );
                  if (ok) {
                    setNetworkEditingAttached([]);
                    setShowNewNetworkForm(false);
                  }
              }}>保存</button>
                <button className="secondary-button" onClick={() => setShowNewNetworkForm(false)}>取消</button>
              </div>
            </div>
          )}
          {networks.length > 0 && <BulkDeleteControls total={networks.length} selected={selectedNetworks} setSelected={setSelectedNetworks} ids={networks.map(net => net.id)} label="网络" onDelete={() => runBulkDelete(Array.from(selectedNetworks), api.deleteNetwork, '网络', () => setSelectedNetworks(new Set()))} />}
          {networks.length === 0 ? (
            <p className="muted" style={{ textAlign: 'center', padding: '24px 0' }}>暂无网络</p>
          ) : (
            <div className="table-wrap">
              <table>
                <thead><tr><th className="row-select-cell"></th><th>名称</th><th>网桥</th><th>VLAN</th><th>子网</th><th>网关</th><th>关联组件</th><th>操作</th></tr></thead>
                <tbody>
                  {networks.map(net => (
                    <tr key={net.id}>
                      <td className="row-select-cell"><input className={rowCheckboxClassName} type="checkbox" checked={selectedNetworks.has(net.id)} onChange={() => toggleSelected(setSelectedNetworks, net.id)} /></td>
                      <td><strong>{net.name}</strong></td>
                      <td className="muted">{net.bridge}</td>
                      <td>{net.vlanId}</td>
                      <td className="muted">{net.subnet}</td>
                      <td className="muted">{net.gateway}</td>
                      <td><span className="badge badge-info">{net.attachedComponents?.length ?? 0} 个</span></td>
                      <td><button className="secondary-button" style={{ minHeight: 30, padding: '3px 8px', fontSize: 11 }} disabled={submitting || dataLoading} onClick={() => startEditNetwork(net)}>编辑</button><button className="danger-button" style={{ minHeight: 30, padding: '3px 8px', fontSize: 11, marginLeft: 4 }} onClick={() => handleDelete(() => api.deleteNetwork(net.id), '网络已删除')}>删除</button></td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      )}

      {/* Topology Viz */}
      {activeSubTab === 'topology-viz' && (
        <div className="panel" style={{ height: 'auto', minHeight: 520 }}>
          <h3 style={{ margin: '0 0 12px', fontSize: 16 }}>
            拓扑视图 ({allTopoNodes.length + egressNodes.length} 节点, {topologyLinks.length} 连接)
          </h3>
          
          <div style={{ display: 'flex', gap: 12, marginBottom: 12, flexWrap: 'wrap', alignItems: 'flex-end' }}>
            {/* Add Egress Node */}
            <div style={{ display: 'flex', gap: 6, alignItems: 'flex-end', flexWrap: 'wrap' }}>
              <div>
                <span className="tiny-label" style={{ display: 'block', marginBottom: 2 }}>物理出口绑定的集群主机</span>
                <select style={{ minHeight: 32, minWidth: 140, fontSize: 12 }} title="选择承载物理出口网卡的集群节点"
                  value={egressForm.hostNodeId} onChange={e => setEgressForm(f => ({ ...f, hostNodeId: e.target.value }))}>
                  <option value="">选择主机</option>
                  {clusterNodes.map(n => <option key={n.id} value={n.id}>{n.name}</option>)}
                </select>
              </div>
              <div>
                <span className="tiny-label" style={{ display: 'block', marginBottom: 2 }}>主机的物理网卡名</span>
                <input style={{ width: 100, minHeight: 32, fontSize: 12 }} placeholder="eth0" title="填写该主机上真实存在的物理网卡名，例如 eth0 或 bond0"
                  value={egressForm.nicName} onChange={e => setEgressForm(f => ({ ...f, nicName: e.target.value }))} />
              </div>
              <button className="secondary-button" disabled={submitting || dataLoading} style={{ minHeight: 32, padding: '4px 10px', fontSize: 12 }}
                onClick={async () => {
                  if (!egressForm.hostNodeId || !egressForm.nicName) { setError('请选择主机并填写物理网卡名'); return; }
                  const hostNode = clusterNodes.find(n => n.id === egressForm.hostNodeId);
                  const id = `egress-${egressForm.hostNodeId}-${egressForm.nicName}`;
                  if (egressNodes.some(n => n.id === id)) { setError('该网卡出口已存在'); return; }
                  await handleSave(() => api.createTopoEgress(toTopologyNodePayload({
                    id, kind: 'egress', label: `${egressForm.nicName}@${hostNode?.name}`,
                    zone: hostNode?.status ?? '', refId: egressForm.hostNodeId, hostNodeId: egressForm.hostNodeId, nicName: egressForm.nicName,
                  })), '物理出口已添加');
                  setEgressForm({ hostNodeId: '', nicName: '' });
                  await loadData('security');
                }}>添加物理出口</button>
            </div>

            {/* Add Link */}
            <div style={{ display: 'flex', gap: 6, alignItems: 'flex-end', flexWrap: 'wrap' }}>
              <div>
                <span className="tiny-label" style={{ display: 'block', marginBottom: 2 }}>流量发起方（源）</span>
                <select style={{ minHeight: 32, minWidth: 120, fontSize: 12 }}
                  value={linkForm.source} onChange={e => setLinkForm(f => ({ ...f, source: e.target.value }))}>
                  <option value="">选择源节点</option>
                  {[...allTopoNodes, ...networkTopoNodes, ...egressNodes].map(n => <option key={n.id} value={n.id}>{n.label}{n.kind === 'egress' ? ' (出口)' : n.kind === 'network' ? ' (网络)' : ''}</option>)}
                </select>
              </div>
              <span className="muted" style={{ fontSize: 12, marginBottom: 8 }}>→</span>
              <div>
                <span className="tiny-label" style={{ display: 'block', marginBottom: 2 }}>流量接收方（目标）</span>
                <select style={{ minHeight: 32, minWidth: 120, fontSize: 12 }}
                  value={linkForm.target} onChange={e => setLinkForm(f => ({ ...f, target: e.target.value }))}>
                  <option value="">选择目标节点</option>
                  {[...allTopoNodes, ...networkTopoNodes, ...egressNodes].map(n => <option key={n.id} value={n.id}>{n.label}{n.kind === 'egress' ? ' (出口)' : n.kind === 'network' ? ' (网络)' : ''}</option>)}
                </select>
              </div>
              <button className="secondary-button" disabled={submitting || dataLoading} style={{ minHeight: 32, padding: '4px 10px', fontSize: 12, marginBottom: 0 }}
                onClick={async () => {
                  if (!linkForm.source || !linkForm.target) { setError('请选择源节点和目标节点'); return; }
                  if (linkForm.source === linkForm.target) { setError('源节点和目标节点不能相同'); return; }
                  const srcNode = [...allTopoNodes, ...networkTopoNodes, ...egressNodes].find(n => n.id === linkForm.source);
                  const tgtNode = [...allTopoNodes, ...networkTopoNodes, ...egressNodes].find(n => n.id === linkForm.target);
                  const id = `${linkForm.source}-${linkForm.target}`;
                  if (topologyLinks.some(l => l.id === id)) { setError('该连线已存在'); return; }
                  const syncToNetwork = async (netId: string, peer: typeof srcNode) => {
                    if (!peer || peer.kind !== 'component') return;
                    const net = networks.find(n => n.id === netId);
                    if (!net) return;
                    const attached = Array.from(new Set([...(net.attachedComponents || []), peer.id]));
                    await api.createNetwork(toNetworkPayload({ ...net, attachedComponents: attached, bridge: net.bridge, parentNic: net.parentNic }));
                  };
                  if (srcNode?.kind === 'network') {
                    await syncToNetwork(linkForm.source, tgtNode);
                  }
                  if (tgtNode?.kind === 'network') {
                    await syncToNetwork(linkForm.target, srcNode);
                  }
                  const saved = await handleSave(() => api.createTopoLink({ id, source: linkForm.source, target: linkForm.target, kind: '' }), '拓扑连线已添加');
                  if (!saved) return;
                  setLinkForm({ source: '', target: '' });
                  await loadData('security');
                }}>添加连线</button>
              <button className="secondary-button" disabled={submitting || dataLoading} style={{ minHeight: 32, padding: '4px 10px', fontSize: 12, color: '#ef4444' }}
                onClick={async () => {
                  if (!confirm('确认删除所有自定义连线和出口节点？此操作不可撤销。')) return;
                  await handleSave(async () => {
                    const userLinks = topologyLinks.filter(l => !topologyGraphLinks.some(al => al.id === l.id));
                    await Promise.all([
                      ...userLinks.map(l => api.deleteTopoLink(l.id)),
                      ...egressNodes.map(n => api.deleteTopoEgress(n.id)),
                    ]);
                    setSelectedLinkId(null);
                  }, '已重置拓扑');
                  await loadData('security');
                }}>重置</button>
              {selectedLinkId && (
                <button className="secondary-button" style={{ minHeight: 32, padding: '4px 10px', fontSize: 12, color: '#dc2626', borderColor: '#fca5a5' }}
                  onClick={async () => {
                    const link = topologyLinks.find(l => l.id === selectedLinkId);
                    if (!link) { setSelectedLinkId(null); return; }
                    const srcNode = [...allTopoNodes, ...networkTopoNodes, ...egressNodes].find(n => n.id === link.source);
                    const tgtNode = [...allTopoNodes, ...networkTopoNodes, ...egressNodes].find(n => n.id === link.target);
                    const syncRemove = async (netId: string, peer: typeof srcNode) => {
                      if (!peer || peer.kind !== 'component') return;
                      const net = networks.find(n => n.id === netId);
                      if (!net) return;
                      const attached = (net.attachedComponents || []).filter(c => c !== peer.id);
                      await api.createNetwork(toNetworkPayload({ ...net, attachedComponents: attached, bridge: net.bridge, parentNic: net.parentNic }));
                    };
                    if (srcNode?.kind === 'network') await syncRemove(link.source, tgtNode);
                    if (tgtNode?.kind === 'network') await syncRemove(link.target, srcNode);
                    const linkId = selectedLinkId;
                    const deleted = await handleDelete(() => api.deleteTopoLink(linkId), '拓扑连线已删除');
                    if (!deleted) return;
                    setSelectedLinkId(null);
                    await loadData('security');
                  }}>删除选中连线</button>
              )}
            </div>
          </div>
          
          <div style={{ display: 'flex', gap: 12, alignItems: 'flex-start' }}>
            <div style={{ flex: 1 }}>
              <TopologyGraph nodes={[...allTopoNodes, ...networkTopoNodes, ...egressNodes]} links={topologyLinks}
            clusterNodes={clusterNodes}
            selectedLinkId={selectedLinkId}
            onSelectLink={setSelectedLinkId}
            onDeleteLink={async (linkId) => {
              const link = topologyLinks.find(l => l.id === linkId);
              if (!link) return;
              const srcNode = [...allTopoNodes, ...networkTopoNodes, ...egressNodes].find(n => n.id === link.source);
              const tgtNode = [...allTopoNodes, ...networkTopoNodes, ...egressNodes].find(n => n.id === link.target);
              const syncRemove = async (netId: string, peer: typeof srcNode) => {
                if (!peer || peer.kind !== 'component') return;
                const net = networks.find(n => n.id === netId);
                if (!net) return;
                const attached = (net.attachedComponents || []).filter(c => c !== peer.id);
                await api.createNetwork(toNetworkPayload({ ...net, attachedComponents: attached, bridge: net.bridge, parentNic: net.parentNic }));
              };
              if (srcNode?.kind === 'network') await syncRemove(link.source, tgtNode);
              if (tgtNode?.kind === 'network') await syncRemove(link.target, srcNode);
              const deleted = await handleDelete(() => api.deleteTopoLink(linkId), '拓扑连线已删除');
              if (!deleted) return;
              setSelectedLinkId(null);
              await loadData('security');
            }} />
            </div>
            {egressNodes.length > 0 && (
              <div style={{ width: 220, flexShrink: 0, borderLeft: '1px solid #e2e8f0', paddingLeft: 12 }}>
                <h4 style={{ margin: '0 0 8px', fontSize: 13, color: '#64748b' }}>物理出口 ({egressNodes.length})</h4>
                {egressNodes.map(node => (
                  <div key={node.id} style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '4px 0', borderBottom: '1px solid #f1f5f9' }}>
                    <div>
                      <span style={{ fontSize: 12, fontWeight: 600 }}>{node.label}</span>
                      {node.nicName && <span style={{ fontSize: 10, color: '#94a3b8', display: 'block' }}>{node.nicName}</span>}
                    </div>
                    <button className="danger-button" style={{ minHeight: 24, padding: '2px 6px', fontSize: 10 }} disabled={submitting || dataLoading} onClick={async () => {
                      if (!confirm('确认删除该出口节点？')) return;
                      const ok = await handleDelete(() => api.deleteTopoEgress(node.id), '出口节点已删除');
                      if (ok) await loadData('security');
                    }}>删除</button>
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>
      )}

      {/* Compliance Run */}
      {activeSubTab === 'compliance-run' && (
        <div className="panel">
          <h3 style={{ margin: '0 0 12px', fontSize: 16 }}>合规扫描</h3>
          <p className="muted" style={{ marginBottom: 16 }}>点击下方按钮发起一次合规扫描，系统将对所有已部署组件进行合规性检查并生成报告。</p>
          <button className="primary-button" disabled={submitting || dataLoading} onClick={() => handleSave(() => api.runCompliance(), '合规扫描已发起')}>开始扫描</button>
        </div>
      )}

      {/* Compliance Focus */}
      {activeSubTab === 'compliance-focus' && (
        <div className="panel">
          <h3 style={{ margin: '0 0 12px', fontSize: 16 }}>合规分析</h3>
          {displayedReports.length === 0 ? (
            <p className="muted">暂无合规报告数据</p>
          ) : (
            <>
              <div className="grid-three" style={{ marginBottom: 16 }}>
                <div className="panel stat-card">
                  <span className="tiny-label" style={{ display: 'block', marginBottom: 4 }}>报告总数</span>
                  <strong style={{ fontSize: 36, margin: '4px 0' }}>{displayedReports.length}</strong>
                </div>
                <div className="panel stat-card">
                  <span className="tiny-label" style={{ display: 'block', marginBottom: 4 }}>平均评分</span>
                  <strong style={{ fontSize: 36, margin: '4px 0', color: avgScore >= 80 ? 'var(--success)' : avgScore >= 60 ? 'var(--warning)' : 'var(--danger)' }}>{avgScore}%</strong>
                </div>
                <div className="panel stat-card">
                  <span className="tiny-label" style={{ display: 'block', marginBottom: 4 }}>最新报告</span>
                  <strong style={{ fontSize: 28, margin: '4px 0' }}>{displayedReports[0].title}</strong>
                  <span className={`badge ${displayedReports[0].status === 'ready' ? 'badge-success' : 'badge-warning'}`} style={{ marginTop: 4 }}>{displayedReports[0].status === 'ready' ? '就绪' : '运行中'}</span>
                </div>
              </div>

              {findingsDistribution.total > 0 && (
                <div className="panel" style={{ marginBottom: 16 }}>
                  <span className="tiny-label" style={{ display: 'block', marginBottom: 6 }}>发现项分布 ({findingsDistribution.total} 项)</span>
                  <div className="findings-bar">
                    <div style={{ width: `${findingsDistribution.high / Math.max(findingsDistribution.total, 1) * 100}%` }} className="findings-bar-high" title={`高: ${findingsDistribution.high}`} />
                    <div style={{ width: `${findingsDistribution.medium / Math.max(findingsDistribution.total, 1) * 100}%` }} className="findings-bar-medium" title={`中: ${findingsDistribution.medium}`} />
                    <div style={{ width: `${findingsDistribution.low / Math.max(findingsDistribution.total, 1) * 100}%` }} className="findings-bar-low" title={`低: ${findingsDistribution.low}`} />
                  </div>
                  <div style={{ display: 'flex', gap: 16, marginTop: 4, fontSize: 12 }}>
                    <span className="muted" style={{ display: 'flex', alignItems: 'center', gap: 4 }}><span style={{ display: 'inline-block', width: 10, height: 10, borderRadius: 2, background: '#ef4444' }} /> 高: {findingsDistribution.high}</span>
                    <span className="muted" style={{ display: 'flex', alignItems: 'center', gap: 4 }}><span style={{ display: 'inline-block', width: 10, height: 10, borderRadius: 2, background: '#f59e0b' }} /> 中: {findingsDistribution.medium}</span>
                    <span className="muted" style={{ display: 'flex', alignItems: 'center', gap: 4 }}><span style={{ display: 'inline-block', width: 10, height: 10, borderRadius: 2, background: '#3b82f6' }} /> 低: {findingsDistribution.low}</span>
                  </div>
                </div>
              )}

              <div className="table-wrap">
                <table>
                  <thead><tr><th>报告</th><th>评分</th><th>标准</th><th>发现项</th><th>生成时间</th><th>操作</th></tr></thead>
                  <tbody>
                    {displayedReports.map(report => (
                      <tr key={report.id} style={{ cursor: 'pointer' }} onClick={() => setSelectedReport(report)}>
                        <td><strong>{report.title}</strong></td>
                        <td><span className={`badge ${scoreColor(report.score)}`}>{report.score}%</span></td>
                        <td><span className="badge badge-info">{report.standard}</span></td>
                        <td>{report.findings.length}</td>
                        <td className="muted">{formatTime(report.generatedAt)}</td>
                        <td>
                          <button className="danger-button" style={{ minHeight: 30, padding: '3px 8px', fontSize: 11 }} disabled={submitting || dataLoading} onClick={(e) => { e.stopPropagation(); if (confirm('确认删除该报告？')) deleteLocalReport(report.id); }}>删除</button>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </>
          )}
        </div>
      )}

      {/* Compliance Tasks */}
      {activeSubTab === 'compliance-tasks' && (
        <div className="panel">
          <div className="section-head" style={{ marginBottom: 12 }}>
            <h3 style={{ margin: 0, fontSize: 16 }}>合规任务 ({complianceTasks.length})</h3>
            <button className="primary-button" onClick={() => { setNewComplianceTask(initialComplianceTask); setShowNewTaskForm(true); }}>新增任务</button>
          </div>
          {showNewTaskForm && (
            <div className="panel" style={{ marginBottom: 14, background: 'rgba(239,246,255,0.6)' }}>
              <h4 style={{ margin: '0 0 10px' }}>{newComplianceTask.id ? '编辑合规任务' : '新增合规任务'}</h4>
              <div className="form-grid">
                <div className="field-block"><span className="field-label required">控制项 ID</span><input value={newComplianceTask.controlId} placeholder="ISO27001-A.8.9" title="填写合规控制项编号" onChange={e => setNewComplianceTask(t => ({ ...t, controlId: e.target.value }))} /><FieldHint>示例：ISO27001-A.8.9。</FieldHint></div>
                <div className="field-block"><span className="field-label required">控制项名称</span><input value={newComplianceTask.controlName} placeholder="配置管理" title="填写控制项名称" onChange={e => setNewComplianceTask(t => ({ ...t, controlName: e.target.value }))} /><FieldHint>示例：配置管理。</FieldHint></div>
                <div className="field-block"><span className="field-label">负责人</span><input value={newComplianceTask.owner} placeholder="张三" title="填写任务负责人姓名或账号" onChange={e => setNewComplianceTask(t => ({ ...t, owner: e.target.value }))} /><FieldHint>示例：张三 或 ops-team。</FieldHint></div>
                <div className="field-block"><span className="field-label">备注</span><input value={newComplianceTask.note} placeholder="补充整改说明" title="填写任务备注" onChange={e => setNewComplianceTask(t => ({ ...t, note: e.target.value }))} /><FieldHint>可填写整改要求或证据位置。</FieldHint></div>
                <div className="field-block"><span className="field-label">截止日期</span><input type="date" value={newComplianceTask.dueAt} title="选择任务截止日期" onChange={e => setNewComplianceTask(t => ({ ...t, dueAt: e.target.value }))} /><FieldHint>选择计划完成日期。</FieldHint></div>
              </div>
              <div className="action-cell" style={{ marginTop: 12 }}>
                <button className="primary-button" disabled={submitting || dataLoading} onClick={async () => {
                  if (!newComplianceTask.controlId.trim()) { setError('请输入控制项 ID'); return; }
                  if (!newComplianceTask.controlName.trim()) { setError('请输入控制项名称'); return; }
                  const payload = toComplianceTaskPayload(newComplianceTask);
                  const ok = await handleSave(
                    () => newComplianceTask.id
                      ? api.updateComplianceTask(newComplianceTask.id, payload)
                      : api.saveComplianceTask(payload),
                    '合规任务已保存',
                  );
                  if (ok) setShowNewTaskForm(false);
                }}>保存</button>
                <button className="secondary-button" onClick={() => setShowNewTaskForm(false)}>取消</button>
              </div>
            </div>
          )}
          {complianceTasks.length > 0 && <BulkDeleteControls total={complianceTasks.length} selected={selectedComplianceTasks} setSelected={setSelectedComplianceTasks} ids={complianceTasks.map(task => task.id)} label="任务" onDelete={() => runBulkDelete(Array.from(selectedComplianceTasks), api.deleteComplianceTask, '任务', () => setSelectedComplianceTasks(new Set()))} />}
          {complianceTasks.length === 0 ? (
            <p className="muted" style={{ textAlign: 'center', padding: '24px 0' }}>暂无任务</p>
          ) : (
            <div className="table-wrap">
              <table>
                <thead><tr><th className="row-select-cell"></th><th>控制项</th><th>负责人</th><th>状态</th><th>截止日期</th><th>备注</th><th>操作</th></tr></thead>
                <tbody>
                  {complianceTasks.map(task => (
                    <tr key={task.id}>
                      <td className="row-select-cell"><input className={rowCheckboxClassName} type="checkbox" checked={selectedComplianceTasks.has(task.id)} onChange={() => toggleSelected(setSelectedComplianceTasks, task.id)} /></td>
                      <td><strong>{task.controlId}</strong><br /><span className="muted" style={{ fontSize: 11 }}>{task.controlName}</span></td>
                      <td className="muted">{task.owner}</td>
                      <td><span className={`badge ${task.status === 'open' ? 'badge-warning' : 'badge-success'}`}>{task.status}</span></td>
                      <td className="muted">{task.dueAt || '-'}</td>
                      <td className="muted">{task.note || '-'}</td>
                      <td><button className="secondary-button" style={{ minHeight: 30, padding: '3px 8px', fontSize: 11 }} disabled={submitting || dataLoading} onClick={() => startEditComplianceTask(task)}>编辑</button><button className="danger-button" style={{ minHeight: 30, padding: '3px 8px', fontSize: 11, marginLeft: 4 }} onClick={() => handleDelete(() => api.deleteComplianceTask(task.id), '任务已删除')}>删除</button></td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      )}

      {/* Compliance Reports */}
      {activeSubTab === 'compliance-reports' && (
        <div className="panel">
          <div className="section-head" style={{ marginBottom: 12 }}>
            <h3 style={{ margin: 0, fontSize: 16 }}>合规报告 ({displayedReports.length})</h3>
          </div>
          {displayedReports.length === 0 ? (
            <p className="muted" style={{ textAlign: 'center', padding: '24px 0' }}>暂无报告</p>
          ) : (
            <div className="table-wrap">
              <table>
                <thead><tr><th>标题</th><th>评分</th><th>标准</th><th>状态</th><th>生成时间</th><th>操作</th></tr></thead>
                <tbody>
                  {displayedReports.map(report => (
                    <tr key={report.id}>
                      <td><strong>{report.title}</strong></td>
                      <td><span className={`badge ${scoreColor(report.score)}`}>{report.score}%</span></td>
                      <td><span className="badge badge-info">{report.standard}</span></td>
                      <td><span className={`badge ${report.status === 'ready' ? 'badge-success' : 'badge-warning'}`}>{report.status === 'ready' ? '就绪' : '运行中'}</span></td>
                      <td className="muted">{formatTime(report.generatedAt)}</td>
                      <td>
                        <button className="secondary-button" style={{ minHeight: 30, padding: '3px 8px', fontSize: 11 }} disabled={submitting || dataLoading} onClick={() => setSelectedReport(report)}>查看</button>
                        <button className="secondary-button" style={{ minHeight: 30, padding: '3px 8px', fontSize: 11, marginLeft: 4 }} onClick={() => void exportReport(report.id, 'csv')}>CSV</button>
                        <button className="secondary-button" style={{ minHeight: 30, padding: '3px 8px', fontSize: 11, marginLeft: 4 }} onClick={() => void exportReport(report.id, 'html')}>报表</button>
                        <button className="danger-button" style={{ minHeight: 30, padding: '3px 8px', fontSize: 11, marginLeft: 4 }} disabled={submitting || dataLoading} onClick={() => { if (confirm('确认删除该报告？')) deleteLocalReport(report.id); }}>删除</button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      )}

      {/* Report Detail Modal */}
      {selectedReport && (
        <div className="modal-backdrop" onClick={e => { if (e.target === e.currentTarget) setSelectedReport(null); }}>
          <div className="panel password-modal">
            <div className="modal-head">
              <div>
                <h4>{selectedReport.title}</h4>
                <p className="muted">{selectedReport.standard} &middot; 评分 {selectedReport.score}%</p>
              </div>
              <button className="modal-close" onClick={() => setSelectedReport(null)}>&times;</button>
            </div>
            <div style={{ marginBottom: 14 }}>
              <span className={`badge ${selectedReport.status === 'ready' ? 'badge-success' : 'badge-warning'}`} style={{ marginRight: 8 }}>{selectedReport.status === 'ready' ? '就绪' : '运行中'}</span>
              <span className="muted">生成于 {formatTime(selectedReport.generatedAt)}</span>
              <div style={{ marginTop: 8, display: 'flex', gap: 6 }}>
                <button className="primary-button" style={{ minHeight: 30, padding: '4px 14px', fontSize: 12 }} disabled={submitting || dataLoading} onClick={() => void exportReport(selectedReport.id, 'csv')}>导出 CSV</button>
                <button className="primary-button" style={{ minHeight: 30, padding: '4px 14px', fontSize: 12 }} disabled={submitting || dataLoading} onClick={() => void exportReport(selectedReport.id, 'html')}>导出报表 (HTML/PDF)</button>
              </div>
            </div>
            {selectedReport.findings.length > 0 && (() => {
              const mHigh = selectedReport.findings.filter(f => f.level === 'high').length;
              const mMedium = selectedReport.findings.filter(f => f.level === 'medium').length;
              const mLow = selectedReport.findings.filter(f => f.level === 'low').length;
              return (
                <div className="panel" style={{ marginBottom: 14, padding: '10px 16px', display: 'flex', gap: 20, fontSize: 13 }}>
                  <span>发现项: <strong>{selectedReport.findings.length}</strong></span>
                  <span style={{ color: '#b91c1c' }}>高: <strong>{mHigh}</strong></span>
                  <span style={{ color: '#b45309' }}>中: <strong>{mMedium}</strong></span>
                  <span style={{ color: '#1d4ed8' }}>低: <strong>{mLow}</strong></span>
                </div>
              );
            })()}
            <div className="table-wrap">
              <table>
                <thead><tr><th>类别</th><th>等级</th><th>描述</th><th>控制项 ID</th><th>控制项名称</th><th>建议</th></tr></thead>
                <tbody>
                  {selectedReport.findings.map((finding: ComplianceFinding, idx: number) => (
                    <tr key={idx}>
                      <td><span className="badge badge-info">{finding.category}</span></td>
                      <td><span className={`badge ${finding.level === 'high' ? 'badge-danger' : finding.level === 'medium' ? 'badge-warning' : 'badge-info'}`}>{finding.level === 'high' ? '高' : finding.level === 'medium' ? '中' : '低'}</span></td>
                      <td>{finding.message}</td>
                      <td className="muted">{finding.controlId}</td>
                      <td>{finding.controlName}</td>
                      <td className="muted">{finding.recommendation}</td>
                    </tr>
                  ))}
                  {selectedReport.findings.length === 0 && (
                    <tr><td colSpan={6} className="muted" style={{ textAlign: 'center' }}>无发现项</td></tr>
                  )}
                </tbody>
              </table>
            </div>
            <div className="action-cell password-modal-actions">
              <button className="secondary-button" onClick={() => setSelectedReport(null)}>关闭</button>
            </div>
          </div>
        </div>
      )}
    </>
  );
}
