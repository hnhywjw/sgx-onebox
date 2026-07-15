import { useCallback, useEffect, useMemo, useRef, useState, Component } from 'react';
import { api, ApiError } from './api';
import type {
  DashboardSnapshot,
  LoginResponse,
  UserView,
} from './types';
import { ChangePasswordModal } from './components/ChangePasswordModal';
import DashboardTab from './components/DashboardTab';
import { DeploymentTab } from './components/DeploymentTab';
import { SecurityTab } from './components/SecurityTab';
import { TrustedTab } from './components/TrustedTab';
import { SystemTab } from './components/SystemTab';

type SubTabKey = 'nodes' | 'quotas' | 'logs' | 'upgrade' | 'images' | 'components' | 'packages' | 'app-market' | 'policies' | 'networks' | 'topology-viz' | 'resources' | 'profiles' | 'keys' | 'attestations' | 'inspections' | 'compliance-run' | 'compliance-focus' | 'compliance-tasks' | 'compliance-reports' | 'accounts' | 'alert-events' | 'audit' | 'settings' | 'plugins';

/* ── SVG Icons ── */
const IconDashboard = <svg xmlns="http://www.w3.org/2000/svg" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><rect x="3" y="3" width="7" height="7" rx="1" /><rect x="14" y="3" width="7" height="7" rx="1" /><rect x="3" y="14" width="7" height="7" rx="1" /><rect x="14" y="14" width="7" height="7" rx="1" /></svg>;

const IconDelivery = <svg xmlns="http://www.w3.org/2000/svg" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M21 15v4a2 2 0 01-2 2H5a2 2 0 01-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>;

const IconCompliance = <svg xmlns="http://www.w3.org/2000/svg" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/><polyline points="9 12 11 14 15 10"/></svg>;

const IconResources = <svg xmlns="http://www.w3.org/2000/svg" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><rect x="4" y="4" width="16" height="16" rx="2" ry="2"/><rect x="9" y="9" width="6" height="6"/><line x1="9" y1="1" x2="9" y2="4"/><line x1="15" y1="1" x2="15" y2="4"/><line x1="9" y1="20" x2="9" y2="23"/><line x1="15" y1="20" x2="15" y2="23"/><line x1="20" y1="9" x2="23" y2="9"/><line x1="20" y1="14" x2="23" y2="14"/><line x1="1" y1="9" x2="4" y2="9"/><line x1="1" y1="14" x2="4" y2="14"/></svg>;

const IconSettings = <svg xmlns="http://www.w3.org/2000/svg" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 00.33 1.82l.06.06a2 2 0 010 2.83 2 2 0 01-2.83 0l-.06-.06a1.65 1.65 0 00-1.82-.33 1.65 1.65 0 00-1 1.51V21a2 2 0 01-4 0v-.09A1.65 1.65 0 009 19.4a1.65 1.65 0 00-1.82.33l-.06.06a2 2 0 01-2.83-2.83l.06-.06A1.65 1.65 0 004.6 15a1.65 1.65 0 00-1.51-1H3a2 2 0 010-4h.09A1.65 1.65 0 004.6 9a1.65 1.65 0 00-.33-1.82l-.06-.06a2 2 0 012.83-2.83l.06.06A1.65 1.65 0 009 4.6a1.65 1.65 0 001-1.51V3a2 2 0 014 0v.09a1.65 1.65 0 001 1.51 1.65 1.65 0 001.82-.33l.06-.06a2 2 0 012.83 2.83l-.06.06A1.65 1.65 0 0019.4 9a1.65 1.65 0 001.51 1H21a2 2 0 010 4h-.09a1.65 1.65 0 00-1.51 1z"/></svg>;

/* ── Login feature icons ── */
const IconControlPlane = <svg xmlns="http://www.w3.org/2000/svg" width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="#2563eb" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><circle cx="12" cy="12" r="3"/><circle cx="5" cy="6" r="2"/><circle cx="19" cy="6" r="2"/><circle cx="5" cy="18" r="2"/><circle cx="19" cy="18" r="2"/><path d="M10 10L6.5 7.5M14 10l3.5-2.5M10 14l-3.5 2.5M14 14l3.5 2.5"/></svg>;

const IconIsolation = <svg xmlns="http://www.w3.org/2000/svg" width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="#4f46e5" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><rect x="4" y="4" width="16" height="16" rx="3"/><rect x="8" y="8" width="8" height="8" rx="2"/><path d="M12 2v2M12 20v2M2 12h2M20 12h2"/></svg>;

const IconComplianceFeature = <svg xmlns="http://www.w3.org/2000/svg" width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="#16a34a" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M22 11.08V12a10 10 0 11-5.93-9.14"/><polyline points="22 4 12 14.01 9 11.01"/></svg>;

const IconUser = <svg xmlns="http://www.w3.org/2000/svg" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="#94a3b8" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M20 21v-2a4 4 0 00-4-4H8a4 4 0 00-4 4v2"/><circle cx="12" cy="7" r="4"/></svg>;

const IconLock = <svg xmlns="http://www.w3.org/2000/svg" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="#94a3b8" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><rect x="3" y="11" width="18" height="11" rx="2" ry="2"/><path d="M7 11V7a5 5 0 0110 0v4"/></svg>;

const IconLoginLock = <svg xmlns="http://www.w3.org/2000/svg" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="#2563eb" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round"><rect x="4" y="10" width="16" height="10" rx="2"/><path d="M8 10V7a4 4 0 018 0v3"/><path d="M12 14v2"/></svg>;

function BrandLogo({ compact = false }: { compact?: boolean }) {
  return <div className={`brand-logo${compact ? ' brand-logo-compact' : ''}`} aria-label="SGX OneBOX">
    <span className="brand-mark">SGX</span>
    <span className="brand-word">OneBOX</span>
  </div>;
}

/* ── Tab / SubTab configs ── */
interface TabDef { key: string; label: string; icon: JSX.Element; }

const tabs: TabDef[] = [
  { key: 'dashboard', label: '仪表盘', icon: IconDashboard },
  { key: 'deployment', label: '交付与部署', icon: IconDelivery },
  { key: 'trusted', label: '可信计算', icon: IconResources },
  { key: 'security', label: '安全合规', icon: IconCompliance },
  { key: 'system', label: '系统管理', icon: IconSettings },
];

/* ── Helpers ── */
function formatTime(value: string) {
  return new Date(value).toLocaleString('zh-CN');
}
const roleLabels: Record<string, string> = {
  platform_admin: '平台管理员',
  security_admin: '安全管理员',
  auditor: '审计员',
  operator: '运维操作员',
};

/* ── Error Boundary ── */
export class ErrorBoundary extends Component<{ children: React.ReactNode }, { hasError: boolean }> {
  constructor(props: { children: React.ReactNode }) {
    super(props);
    this.state = { hasError: false };
  }
  static getDerivedStateFromError() { return { hasError: true }; }
  render() {
    if (this.state.hasError) {
      return <div className="panel" style={{ maxWidth: 400, margin: '80px auto', textAlign: 'center', padding: 40 }}>
        <h3>页面出现异常</h3>
        <p className="muted">请刷新页面重试，或联系管理员</p>
        <button className="primary-button" style={{ marginTop: 12 }} onClick={() => { this.setState({ hasError: false }); window.location.reload(); }}>刷新页面</button>
      </div>;
    }
    return this.props.children;
  }
}

/* ── App ── */
export default function App() {
  // Auth
  const [currentUser, setCurrentUser] = useState<UserView | null>(null);
  const [sessionExpired, setSessionExpired] = useState(false);
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [remember, setRemember] = useState(false);
  const [loginError, setLoginError] = useState('');
  const [loginLoading, setLoginLoading] = useState(false);
  const [captchaId, setCaptchaId] = useState('');
  const [captchaImage, setCaptchaImage] = useState('');
  const [captchaAnswer, setCaptchaAnswer] = useState('');
  const [captchaExpiresAt, setCaptchaExpiresAt] = useState(0);

  // Navigation
  const [activeTab, setActiveTab] = useState('dashboard');
  const [activeSubTab, setActiveSubTab] = useState<SubTabKey>('nodes');

  // Data
  const [snapshot, setSnapshot] = useState<DashboardSnapshot | null>(null);
  const [dataLoading, setDataLoading] = useState(false);
  const loadingRef = useRef(false);
  const submittingRef = useRef(false);
  const mountedRef = useRef(false);
  const [users, setUsers] = useState<UserView[]>([]);

  // UI state
  const [error, setError] = useState('');
  const [success, setSuccess] = useState('');
  const [toastLeaving, setToastLeaving] = useState(false);

  useEffect(() => {
    if (success) {
      setToastLeaving(false);
      const leaveTimer = setTimeout(() => setToastLeaving(true), 4200);
      const clearTimer = setTimeout(() => setSuccess(''), 5000);
      return () => { clearTimeout(leaveTimer); clearTimeout(clearTimer); };
    }
  }, [success]);
  useEffect(() => {
    if (error) {
      setToastLeaving(false);
      const leaveTimer = setTimeout(() => setToastLeaving(true), 4200);
      const clearTimer = setTimeout(() => setError(''), 5000);
      return () => { clearTimeout(leaveTimer); clearTimeout(clearTimer); };
    }
  }, [error]);

  // Cluster upgrade
  const [upgradeVersion, setUpgradeVersion] = useState('');
  const [upgradeChannel, setUpgradeChannel] = useState('stable');
  const [k3sVersions, setK3sVersions] = useState<string[]>([]);
  const [k3sVersionsLoading, setK3sVersionsLoading] = useState(false);
  const [manifestContent, setManifestContent] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  // Alert thresholds
  const [alertThresholdCpu, setAlertThresholdCpu] = useState('80');
  const [alertThresholdMem, setAlertThresholdMem] = useState('80');
  const [alertThresholdPod, setAlertThresholdPod] = useState('100');
  const [savedAlertThresholdCpu, setSavedAlertThresholdCpu] = useState('80');
  const [savedAlertThresholdMem, setSavedAlertThresholdMem] = useState('80');
  const [savedAlertThresholdPod, setSavedAlertThresholdPod] = useState('100');
  const [lastRefreshTime, setLastRefreshTime] = useState(() => new Date().toISOString());

  // Change password
  const [showChangePassword, setShowChangePassword] = useState(false);
  const [changePwdCurrent, setChangePwdCurrent] = useState('');
  const [changePwdNew, setChangePwdNew] = useState('');
  const [changePwdConfirm, setChangePwdConfirm] = useState('');

  /* ── Captcha ── */
  const refreshCaptcha = useCallback(async () => {
    try {
      const captcha = await api.captcha();
      setCaptchaId(captcha.id);
      setCaptchaImage(captcha.image);
      setCaptchaAnswer('');
      setCaptchaExpiresAt(new Date(captcha.expiresAt).getTime());
    } catch {
      setCaptchaId('');
      setCaptchaImage('');
      setCaptchaAnswer('');
      setCaptchaExpiresAt(0);
    }
  }, []);

  /* ── Session check ── */
  useEffect(() => {
    api.me().then(u => { setCurrentUser(u); }).catch(() => { /** not logged in */ });
  }, []);

  useEffect(() => {
    if (!currentUser) return;
    setK3sVersionsLoading(true);
    api.fetchK3sVersions(upgradeChannel)
      .then(versions => { setK3sVersions(versions); if (versions.length > 0) setUpgradeVersion(versions[0]); })
      .catch(() => { if (upgradeChannel !== 'stable') setError('获取 K3s 版本列表失败，请检查网络连接'); })
      .finally(() => setK3sVersionsLoading(false));
  }, [currentUser, upgradeChannel]);

  useEffect(() => {
    if (!currentUser) return;
    setSessionExpired(false);
    setDataLoading(true);
    mountedRef.current = true;
    const requests: [Promise<DashboardSnapshot>, Promise<UserView[]>] = [
      api.snapshot(),
      currentUser.role === 'platform_admin' ? api.listUsers() : Promise.resolve([]),
    ];
    Promise.all(requests)
      .then(([snap, userList]) => {
        if (!mountedRef.current) return;
        setSnapshot(snap);
        setUsers(userList);
        const t = snap.alertThreshold;
        if (t) {
          setAlertThresholdCpu(String(t.cpuThreshold));
          setAlertThresholdMem(String(t.memThreshold));
          setAlertThresholdPod(String(t.podThreshold));
          setSavedAlertThresholdCpu(String(t.cpuThreshold));
          setSavedAlertThresholdMem(String(t.memThreshold));
          setSavedAlertThresholdPod(String(t.podThreshold));
        }
      })
      .catch(e => { if (!mountedRef.current) return; if (e instanceof ApiError && e.status === 401) { setCurrentUser(null); setSessionExpired(true); } else { setError((e as Error).message); } })
      .finally(() => { if (mountedRef.current) setDataLoading(false); });
    return () => { mountedRef.current = false; };
  }, [currentUser]);

  /* ── Login ── */
  const handleLogin = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!captchaId || Date.now() > captchaExpiresAt) { setLoginError('验证码已过期，请点击验证码刷新'); void refreshCaptcha(); return; }
    setLoginLoading(true);
    setLoginError('');
    try {
      const res: LoginResponse = await api.login(username, password, captchaId, captchaAnswer);
      setCurrentUser(res.user);
      setSessionExpired(false);
      if (remember) {
        window.localStorage.setItem('sgx-remember-login', JSON.stringify({ username }));
      } else {
        window.localStorage.removeItem('sgx-remember-login');
      }
      setUsername('');
      setPassword('');
      setCaptchaAnswer('');
      void refreshCaptcha();
    } catch (err) {
      setLoginError((err as Error).message);
      void refreshCaptcha();
    } finally {
      setLoginLoading(false);
    }
  };

  /* ── Logout ── */
  const handleLogout = async () => {
    try {
      await api.logout();
    } finally {
      setCurrentUser(null);
      setActiveTab('dashboard');
      setSessionExpired(false);
    }
  };

  /* ── Change password ── */
  const handleChangePassword = async (e: React.FormEvent) => {
    e.preventDefault();
    if (changePwdNew !== changePwdConfirm) { setError('两次输入的新密码不一致'); return; }
    try {
      await api.changePassword({ currentPassword: changePwdCurrent, newPassword: changePwdNew });
      setSuccess('密码修改成功，请使用新密码重新登录');
      setShowChangePassword(false);
      setChangePwdCurrent(''); setChangePwdNew(''); setChangePwdConfirm('');
      setCurrentUser(null);
      setActiveTab('dashboard');
      setError('');
    } catch (err) { setError((err as Error).message); }
  };

  const closeChangePassword = () => {
    setShowChangePassword(false);
    setChangePwdCurrent('');
    setChangePwdNew('');
    setChangePwdConfirm('');
    setError('');
  };

  /* ── Data refresh ── */
  const loadData = useCallback(async (silentOrTab: boolean | string = false) => {
    const tab: string | undefined = typeof silentOrTab === 'string' ? silentOrTab : undefined;
    const silent: boolean = typeof silentOrTab === 'boolean' ? silentOrTab : false;
    if (!currentUser || loadingRef.current) return;
    loadingRef.current = true;
    setDataLoading(true);
    try {
      const shouldLoadUsers = (!tab || tab === 'system') && currentUser.role === 'platform_admin';
      const userRequest = shouldLoadUsers ? api.listUsers() : Promise.resolve<UserView[] | null>(null);
      const [snapResult, userResult] = await Promise.allSettled([api.snapshot(), userRequest]);
        if (snapResult.status === 'fulfilled') {
        const snap = snapResult.value;
        setSnapshot(snap);
        setLastRefreshTime(new Date().toISOString());
        if (!tab || tab === 'system') {
          const t = snap.alertThreshold;
          if (t) {
            setAlertThresholdCpu(String(t.cpuThreshold));
            setAlertThresholdMem(String(t.memThreshold));
            setAlertThresholdPod(String(t.podThreshold));
            setSavedAlertThresholdCpu(String(t.cpuThreshold));
            setSavedAlertThresholdMem(String(t.memThreshold));
            setSavedAlertThresholdPod(String(t.podThreshold));
          }
        }
      }
      if (snapResult.status === 'rejected') {
        if (snapResult.reason instanceof ApiError && snapResult.reason.status === 401) {
          setCurrentUser(null);
          setSessionExpired(true);
          return;
        }
        throw snapResult.reason;
      }
      if (shouldLoadUsers) {
        if (userResult.status === 'fulfilled' && userResult.value) setUsers(userResult.value);
        if (userResult.status === 'rejected') throw userResult.reason;
      }
      if (!silent) {
        setError('');
        setSuccess('数据已刷新');
      }
    } catch (err) {
      if (!silent) {
        setSuccess('');
        setError((err as Error).message || '数据刷新失败');
      }
    } finally {
      setDataLoading(false);
      loadingRef.current = false;
    }
  }, [currentUser]);

  /* ── Auto-poll dashboard ── */
  useEffect(() => {
    if (!currentUser) return;
    const interval = setInterval(() => loadData(true), 30000);
    return () => clearInterval(interval);
  }, [currentUser, loadData]);

  /* ── Tab switching ── */
  const switchTab = (tabKey: string) => {
    setActiveTab(tabKey);
    setError(''); setSuccess('');
    if (tabKey === 'deployment') setActiveSubTab('nodes');
    else if (tabKey === 'security') setActiveSubTab('policies');
    else if (tabKey === 'trusted') setActiveSubTab('resources');
    else if (tabKey === 'system') setActiveSubTab('settings');
  };

  const handleSetActiveSubTab = (key: string) => setActiveSubTab(key as SubTabKey);

  /* ── Entity CRUD helpers ── */
  const handleSave = async (action: () => Promise<unknown>, successMsg: string) => {
    if (submittingRef.current) return false;
    submittingRef.current = true;
    try { setError(''); setSubmitting(true); await action(); setSuccess(successMsg); await loadData(); return true; }
    catch (err) {
      const e = err as ApiError;
      if (e.status === 401) { setSessionExpired(true); setCurrentUser(null); } else setError(e.message);
      return false;
    }
    finally { submittingRef.current = false; setSubmitting(false); }
  };

  const handleAttestationResult = (id: string, status: 'verified' | 'failed') => {
    return handleSave(
      () => api.submitAttestationResult(id, {
        status,
        secretsReleased: status === 'verified',
        policyResult: status === 'verified' ? '远程证明核验通过' : '远程证明核验未通过',
        evidence: status === 'verified' ? '远程证明证据校验通过' : '远程证明证据校验失败',
      }),
      status === 'verified' ? '证明结果已通过' : '证明结果已标记失败'
    );
  };

  const handleDelete = async (action: () => Promise<unknown>, successMsg: string) => {
    if (submittingRef.current) return false;
    if (!confirm('确认删除？')) return false;
    submittingRef.current = true;
    try { setError(''); setSubmitting(true); await action(); setSuccess(successMsg); await loadData(); return true; } catch (err) { if (err instanceof ApiError && err.status === 401) { setCurrentUser(null); setSessionExpired(true); } else { setError((err as Error).message); } return false; } finally { submittingRef.current = false; setSubmitting(false); }
  };

  /* ── Dashboard metrics ── */
  const alertLevelLabel: Record<string, string> = { critical: '严重', warning: '警告', info: '信息' };
  const alertLevelColor: Record<string, string> = { critical: 'badge-danger', warning: 'badge-warning', info: 'badge-info' };

  /* ── Derived data ── */
  const clusterNodes = snapshot?.clusterNodes ?? [];
  const clusterQuotas = snapshot?.clusterQuotas ?? [];
  const clusterLogs = snapshot?.clusterLogs ?? [];
  const clusterAlerts = snapshot?.clusterAlerts ?? [];
  const clusterUpgrade = snapshot?.clusterUpgrade ?? null;
  const images = snapshot?.images ?? [];
  const components = snapshot?.components ?? [];
  const packages = snapshot?.installPackages ?? [];
  const provisioningTasks = snapshot?.provisioningTasks ?? [];
  const marketplaceApps = snapshot?.marketplaceApps ?? [];
  const policies = snapshot?.securityPolicies ?? [];
  const networks = snapshot?.networks ?? [];
  const enclaveResources = snapshot?.enclaveResources ?? [];
  const enclaveProfiles = snapshot?.enclaves ?? [];
  const enclaveKeys = snapshot?.enclaveKeys ?? [];
  const enclaveInspections = snapshot?.enclaveInspections ?? [];
  const attestations = snapshot?.attestations ?? [];
  const complianceTasks = snapshot?.complianceTasks ?? [];
  const complianceReports = snapshot?.reports ?? [];
  const systemSettings = snapshot?.systemSettings ?? [];
  const auditEvents = snapshot?.auditEvents ?? [];
  const topoLinks = snapshot?.topoLinks ?? [];
  const topoEgress = snapshot?.topoEgress ?? [];
  const plugins = snapshot?.plugins ?? [];
  const manifestHints = snapshot?.manifestHints ?? [];

  const cpuThreshold = Number(savedAlertThresholdCpu) || 80;
  const memThreshold = Number(savedAlertThresholdMem) || 80;
  const podThreshold = Number(savedAlertThresholdPod) || 100;

  const dynamicAlerts = useMemo(() => {
    const nodes = snapshot?.clusterNodes ?? [];
    const alerts: typeof clusterAlerts = [];
    for (const node of nodes) {
      if (node.cpuUsage > cpuThreshold) {
        alerts.push({ id: `alert-dyn-cpu-${node.id}`, level: node.cpuUsage > cpuThreshold + 10 ? 'critical' : 'warning' as const, source: node.name, message: `节点 CPU 使用率 ${node.cpuUsage.toFixed(1)}%，超过阈值 ${cpuThreshold}%`, status: 'open', createdAt: lastRefreshTime });
      }
      if (node.memoryUsage > memThreshold) {
        alerts.push({ id: `alert-dyn-mem-${node.id}`, level: node.memoryUsage > memThreshold + 10 ? 'critical' : 'warning' as const, source: node.name, message: `节点内存使用率 ${node.memoryUsage.toFixed(1)}%，超过阈值 ${memThreshold}%`, status: 'open', createdAt: lastRefreshTime });
      }
      if (node.podCount > podThreshold) {
        alerts.push({ id: `alert-dyn-pod-${node.id}`, level: node.podCount > podThreshold + 10 ? 'critical' : 'warning' as const, source: node.name, message: `节点 Pod 数量 ${node.podCount}，超过阈值 ${podThreshold} 个`, status: 'open', createdAt: lastRefreshTime });
      }
    }
    return alerts;
  }, [snapshot?.clusterNodes, cpuThreshold, memThreshold, podThreshold, lastRefreshTime]);

  const mergedAlerts = [...dynamicAlerts, ...clusterAlerts];

  /* ── Init captcha & restore remember-password ── */
  useEffect(() => {
    void refreshCaptcha();
    const saved = window.localStorage.getItem('sgx-remember-login');
    if (saved) {
      try {
        const creds = JSON.parse(saved) as { username: string };
        setUsername(creds.username ?? '');
        setRemember(true);
      } catch { /* ignore */ }
    }
  }, [refreshCaptcha]);

  /* ── Login Page ── */
  if (!currentUser) {
    return (
      <div className="login-shell">
        <div className="login-stage">
          <div className="login-aside">
            <div className="brand-block">
              <div className="brand-heading">
                <BrandLogo />
                <strong className="brand-title">全新一代等保一体机</strong>
              </div>
              <p className="brand-subtitle">——轻量化+可信执行环境统一管理平台</p>
            </div>
            <div className="feature-list">
              <div className="login-feature">
                <div className="feature-icon">{IconControlPlane}</div>
                <div>
                  <strong>轻量化统一控制面</strong>
                  <span className="muted" style={{ display: 'block', marginTop: 4 }}>基于 K3s 轻量化架构，统一纳管节点、镜像、组件与应用交付，降低 SGX 集群部署与运维复杂度</span>
                </div>
              </div>
              <div className="login-feature">
                <div className="feature-icon">{IconIsolation}</div>
                <div>
                  <strong>可信隔离编排</strong>
                  <span className="muted" style={{ display: 'block', marginTop: 4 }}>支持标准、硬件加固与飞地隔离策略，按业务敏感等级灵活配置可信运行环境</span>
                </div>
              </div>
              <div className="login-feature">
                <div className="feature-icon">{IconComplianceFeature}</div>
                <div>
                  <strong>合规闭环管理</strong>
                  <span className="muted" style={{ display: 'block', marginTop: 4 }}>集成安全策略、远程证明、审计记录与合规报告，支撑等保与密评场景落地</span>
                </div>
              </div>
            </div>
          </div>
          <div className="login-panel">
            <h2 className="login-title">{IconLoginLock}<span>平台登录</span></h2>
            <form className="login-form" onSubmit={handleLogin}>
              <div className="login-input-wrap">
                <span className="login-input-icon">{IconUser}</span>
                <input className="login-input" type="text" placeholder="用户名，如 admin" title="输入平台账号用户名，例如 admin" value={username} onChange={e => setUsername(e.target.value)} autoComplete="username" />
              </div>
              <div className="login-input-wrap">
                <span className="login-input-icon">{IconLock}</span>
                <input className="login-input" type="password" placeholder="密码" title="输入账号密码" value={password} onChange={e => setPassword(e.target.value)} autoComplete="current-password" />
              </div>
              <div className="section-head" style={{ marginTop: 2 }}>
                <div className="inline-card" style={{ flex: 1, justifyContent: 'flex-start', gap: 10 }}>
                  <input className="captcha-input" placeholder="4位数字" title="输入右侧图片中的 4 位数字验证码" value={captchaAnswer} maxLength={4} inputMode="numeric" onChange={e => setCaptchaAnswer(e.target.value.replace(/\D/g, '').slice(0, 4))} />
                  <button type="button" className="captcha-code" onClick={() => { setLoginError(''); void refreshCaptcha(); }} title="点击刷新验证码">{captchaImage ? <img src={captchaImage} alt="验证码" style={{ height: 32, display: 'block' }} /> : '----'}</button>
                </div>
                <div className="checkbox">
                  <input type="checkbox" id="remember" checked={remember} onChange={e => setRemember(e.target.checked)} />
                  <label htmlFor="remember" style={{ fontSize: 13, color: '#64748b' }}>记住用户名</label>
                </div>
              </div>
              <button type="submit" className="login-submit" disabled={loginLoading || !username || !password || captchaAnswer.length !== 4}>
                {loginLoading ? '登录中...' : '登录'}
              </button>
              <p className="login-copyright">© 2026 SGX OneBOX. All rights reserved.</p>
              {sessionExpired && <p className="login-message login-message-error">会话已过期，请重新登录</p>}
              {loginError && <p className="login-message login-message-error">{loginError}</p>}
            </form>
          </div>
        </div>
      </div>
    );
  }

  /* ── Main App ── */
  return (
    <div className={`app-shell${currentUser.role === 'auditor' ? ' app-shell-readonly' : ''}`}>
      {/* Sidebar */}
      <aside className="sidebar">
        <div className="brand-block">
          <BrandLogo compact />
          <strong style={{ fontSize: 14, color: '#1e293b', lineHeight: 1.2 }}>全新一代等保一体机</strong>
          <p className="muted" style={{ fontSize: 12, margin: 0, lineHeight: 1.4 }}>轻量化+可信执行环境统一管理平台</p>
          <div className="sidebar-brand-divider" />
        </div>
        <div className="sidebar-user-identity">
          <small>{roleLabels[currentUser.role] ?? currentUser.role}</small>
          <strong>[{currentUser.username}]</strong>
        </div>
        <div className="sidebar-soft-divider" />
        <nav className="nav-list">
          {tabs.map(tab => (
            <button key={tab.key} className={`nav-button${activeTab === tab.key ? ' nav-button-active' : ''}`} onClick={() => switchTab(tab.key)}>
              <span className="nav-icon">{tab.icon}</span> {tab.label}
            </button>
          ))}
        </nav>
        <div className="sidebar-soft-divider" />
        <div className="sidebar-user-actions">
          <button className="sidebar-action-button" onClick={() => { setError(''); setSuccess(''); setShowChangePassword(true); }}>修改密码</button>
          <button className="sidebar-action-button sidebar-action-danger" onClick={handleLogout}>退出</button>
        </div>
      </aside>

      {/* ChangePassword modal */}
      <ChangePasswordModal
        show={showChangePassword}
        currentPwd={changePwdCurrent}
        newPwd={changePwdNew}
        confirmPwd={changePwdConfirm}
        onCurrentPwdChange={setChangePwdCurrent}
        onNewPwdChange={setChangePwdNew}
        onConfirmPwdChange={setChangePwdConfirm}
        onSubmit={handleChangePassword}
        onClose={closeChangePassword}
      />

      {/* Manifest modal */}
      {manifestContent && (
        <div className="modal-backdrop" onClick={() => setManifestContent(null)}>
          <div className="password-modal" style={{ maxWidth: 720, maxHeight: '80vh', overflow: 'auto' }} onClick={e => e.stopPropagation()}>
            <div className="modal-head"><h3>Manifest</h3><button className="modal-close" onClick={() => setManifestContent(null)}>×</button></div>
            <pre style={{ background: '#f1f5f9', padding: 16, borderRadius: 8, fontSize: 12, lineHeight: 1.6, overflow: 'auto', maxHeight: '60vh', whiteSpace: 'pre-wrap', wordBreak: 'break-all' }}>{manifestContent}</pre>
          </div>
        </div>
      )}

      {/* Toast */}
      {(error || success) && (
        <div className={`toast-container${toastLeaving ? ' toast-leaving' : ''}`}>
          {error && <div className="toast toast-error" onClick={() => { setToastLeaving(true); setTimeout(() => setError(''), 600); }}>{error}</div>}
          {success && <div className="toast toast-success" onClick={() => { setToastLeaving(true); setTimeout(() => setSuccess(''), 600); }}>{success}</div>}
        </div>
      )}

      <main className="content">
        {activeTab === 'dashboard' && (
          <DashboardTab
            clusterNodes={clusterNodes}
            components={components}
            policies={policies}
            complianceReports={complianceReports}
            enclaveResources={enclaveResources}
            clusterUpgrade={clusterUpgrade}
            mergedAlerts={mergedAlerts}
            alertLevelLabel={alertLevelLabel}
            alertLevelColor={alertLevelColor}
            loadData={loadData}
            switchTab={switchTab}
            setActiveSubTab={handleSetActiveSubTab}
            dataLoading={dataLoading}
            formatTime={formatTime}
          />
        )}
        {activeTab === 'deployment' && (
          <DeploymentTab
            clusterNodes={clusterNodes}
            images={images}
            components={components}
            packages={packages}
            provisioningTasks={provisioningTasks}
            marketApps={marketplaceApps}
            k3sVersions={k3sVersions}
            activeSubTab={activeSubTab}
            setActiveSubTab={handleSetActiveSubTab}
            loadData={loadData}
            handleSave={handleSave}
            handleDelete={handleDelete}
            setError={setError}
            setSuccess={setSuccess}
            setManifestContent={setManifestContent}
            manifestHints={manifestHints}
            dataLoading={dataLoading}
            submitting={submitting}
            formatTime={formatTime}
          />
        )}
        {activeTab === 'security' && (
          <SecurityTab
            policies={policies}
            networks={networks}
            complianceTasks={complianceTasks}
            complianceReports={complianceReports}
            clusterNodes={clusterNodes}
            components={components}
            topoLinks={topoLinks}
            topoEgress={topoEgress}
            activeSubTab={activeSubTab}
            setActiveSubTab={handleSetActiveSubTab}
            loadData={loadData}
            handleSave={handleSave}
            handleDelete={handleDelete}
            setError={setError}
            setSuccess={setSuccess}
            dataLoading={dataLoading}
            submitting={submitting}
            formatTime={formatTime}
          />
        )}
        {activeTab === 'trusted' && (
          <TrustedTab
            enclaveResources={enclaveResources}
            enclaveProfiles={enclaveProfiles}
            enclaveKeys={enclaveKeys}
            attestations={attestations}
            enclaveInspections={enclaveInspections}
            clusterNodes={clusterNodes}
            activeSubTab={activeSubTab}
            setActiveSubTab={handleSetActiveSubTab}
            loadData={loadData}
            handleSave={handleSave}
            handleDelete={handleDelete}
            handleAttestationResult={handleAttestationResult}
            setError={setError}
            setSuccess={setSuccess}
            dataLoading={dataLoading}
            submitting={submitting}
            formatTime={formatTime}
          />
        )}
        {activeTab === 'system' && (
          <SystemTab
            clusterQuotas={clusterQuotas}
            clusterLogs={clusterLogs}
            clusterUpgrade={clusterUpgrade}
            systemSettings={systemSettings}
            users={users}
            auditEvents={auditEvents}
            mergedAlerts={mergedAlerts}
            alertLevelLabel={alertLevelLabel}
            alertLevelColor={alertLevelColor}
            savedAlertThresholdCpu={savedAlertThresholdCpu}
            savedAlertThresholdMem={savedAlertThresholdMem}
            savedAlertThresholdPod={savedAlertThresholdPod}
            alertThresholdCpu={alertThresholdCpu}
            alertThresholdMem={alertThresholdMem}
            alertThresholdPod={alertThresholdPod}
            setAlertThresholdCpu={setAlertThresholdCpu}
            setAlertThresholdMem={setAlertThresholdMem}
            setAlertThresholdPod={setAlertThresholdPod}
            setSavedAlertThresholdCpu={setSavedAlertThresholdCpu}
            setSavedAlertThresholdMem={setSavedAlertThresholdMem}
            setSavedAlertThresholdPod={setSavedAlertThresholdPod}
            k3sVersions={k3sVersions}
            k3sVersionsLoading={k3sVersionsLoading}
            upgradeVersion={upgradeVersion}
            setUpgradeVersion={setUpgradeVersion}
            upgradeChannel={upgradeChannel}
            setUpgradeChannel={setUpgradeChannel}
            activeSubTab={activeSubTab}
            setActiveSubTab={handleSetActiveSubTab}
            loadData={loadData}
            handleSave={handleSave}
            handleDelete={handleDelete}
            setError={setError}
            setSuccess={setSuccess}
            dataLoading={dataLoading}
            submitting={submitting}
            formatTime={formatTime}
            roleLabels={roleLabels}
            plugins={plugins}
          />
        )}
      </main>
    </div>
  );
}