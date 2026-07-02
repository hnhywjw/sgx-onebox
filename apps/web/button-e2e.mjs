import { chromium } from '/usr/local/lib/node_modules/playwright/index.mjs';

const base = process.env.E2E_BASE_URL || process.env.SMOKE_BASE_URL || 'http://localhost:8080';
const headless = process.env.E2E_HEADLESS !== '0';
const slowMo = Number(process.env.E2E_SLOWMO || 0);
const runId = `e2e-${Date.now()}`;
const clicked = [];
const errors = [];

function assert(condition, message) {
  if (!condition) throw new Error(message);
}

async function waitReady(page) {
  await page.waitForLoadState('domcontentloaded');
  await page.waitForTimeout(250);
}

async function clickByRole(page, name, options = {}) {
  const locator = page.getByRole('button', { name, exact: options.exact ?? true }).first();
  await locator.waitFor({ state: 'visible', timeout: options.timeout || 8000 });
  await locator.click();
  clicked.push(options.label || name);
  await waitReady(page);
}

async function clickText(page, text, options = {}) {
  const locator = page.getByText(text, { exact: options.exact ?? true }).first();
  await locator.waitFor({ state: 'visible', timeout: options.timeout || 8000 });
  await locator.click();
  clicked.push(options.label || text);
  await waitReady(page);
}

async function closeOverlay(page) {
  const close = page.locator('.modal-close').first();
  if (await close.isVisible().catch(() => false)) {
    await close.click();
    await waitReady(page);
    return;
  }
  if (await page.locator('.modal-backdrop').first().isVisible().catch(() => false)) {
    await page.keyboard.press('Escape');
    await waitReady(page);
  }
}

async function fill(page, selector, value) {
  const locator = page.locator(selector).first();
  await locator.waitFor({ state: 'visible', timeout: 8000 });
  await locator.fill(value);
}

async function select(page, selector, value) {
  const locator = page.locator(selector).first();
  await locator.waitFor({ state: 'visible', timeout: 8000 });
  await locator.selectOption(value);
}

async function dashboard(page) {
  return await page.evaluate(async () => {
    const res = await fetch('/api/v1/dashboard');
    if (!res.ok) throw new Error(`dashboard ${res.status}`);
    return await res.json();
  });
}

async function cleanup(page) {
  const snap = await dashboard(page);
  const requests = [];
  for (const item of snap.enclaveKeys || []) if (item.name?.includes(runId) || item.id?.includes(runId)) requests.push(fetch(`/api/v1/enclave-keys/${item.id}`, { method: 'DELETE' }));
  for (const item of snap.enclaves || []) if (item.id?.includes(runId) || item.componentId?.includes(runId)) requests.push(fetch(`/api/v1/enclaves/${item.id}`, { method: 'DELETE' }));
  for (const item of snap.enclaveResources || []) if (item.id?.includes(runId) || item.nodeId?.includes(runId)) requests.push(fetch(`/api/v1/enclave-resources/${item.id}`, { method: 'DELETE' }));
  for (const item of snap.networks || []) if (item.name?.includes(runId) || item.id?.includes(runId)) requests.push(fetch(`/api/v1/networks/${item.id}`, { method: 'DELETE' }));
  for (const item of snap.securityPolicies || []) if (item.name?.includes(runId) || item.id?.includes(runId)) requests.push(fetch(`/api/v1/security-policies/${item.id}`, { method: 'DELETE' }));
  for (const item of snap.complianceTasks || []) if (item.controlId?.includes(runId)) requests.push(fetch(`/api/v1/compliance-tasks/${item.id}`, { method: 'DELETE' }));
  for (const item of snap.systemSettings || []) if (item.name?.includes(runId)) requests.push(fetch(`/api/v1/system-settings/${item.id}`, { method: 'DELETE' }));
  for (const item of snap.clusterQuotas || []) if (item.scope?.includes(runId)) requests.push(fetch(`/api/v1/cluster/quotas/${item.id}`, { method: 'DELETE' }));
  for (const item of snap.components || []) if (item.name?.includes(runId)) requests.push(fetch(`/api/v1/components/${item.id}`, { method: 'DELETE' }));
  for (const item of snap.images || []) if (item.name?.includes(runId)) requests.push(fetch(`/api/v1/images/${item.id}`, { method: 'DELETE' }));
  for (const item of snap.marketplaceApps || []) if (item.name?.includes(runId)) requests.push(fetch(`/api/v1/marketplace/apps/${item.id}`, { method: 'DELETE' }));
  await Promise.allSettled(requests);
}

async function expectNoErrorToast(page, context) {
  const error = page.locator('.toast-error, .login-message-error').first();
  if (await error.isVisible().catch(() => false)) {
    const text = await error.textContent();
    throw new Error(`${context}: ${text}`);
  }
}

async function login(page) {
  await page.goto(base, { waitUntil: 'domcontentloaded' });
  await fill(page, 'input[placeholder^="用户名"]', 'admin');
  await fill(page, 'input[placeholder^="密码"]', 'admin123');
  const answer = await page.locator('.captcha-code img').evaluate(img => {
    const src = img.getAttribute('src') || '';
    const raw = atob(src.split('base64,')[1] || '');
    const match = raw.match(/<text[^>]*>([^<]+)<\/text>/);
    return match ? match[1].trim() : '';
  });
  await fill(page, 'input[placeholder="4位数字"]', answer);
  await clickByRole(page, '登录');
  await page.getByText('仪表盘', { exact: true }).waitFor({ state: 'visible', timeout: 10000 });
}

async function verifyNavigation(page) {
  for (const tab of ['仪表盘', '交付与部署', '安全合规', '可信计算', '系统管理']) {
    await clickText(page, tab);
  }
}

async function createImage(page) {
  await clickText(page, '交付与部署');
  await clickText(page, '镜像资产');
  await clickByRole(page, '新增镜像');
  await fill(page, 'input[placeholder="nginx-enclave"]', `img-${runId}`);
  await fill(page, 'input[placeholder="registry.example.com"]', 'registry.local');
  await fill(page, 'input[placeholder="secure/nginx"]', `secure/${runId}`);
  await fill(page, 'input[placeholder="1.0.0"]', '1.0.0');
  await clickByRole(page, '保存');
  await expectNoErrorToast(page, '新增镜像');
  const snap = await dashboard(page);
  const image = snap.images.find(item => item.name === `img-${runId}`);
  assert(image, '镜像创建后未出现在 dashboard');
  return image;
}

async function createComponent(page, image) {
  await clickText(page, '组件定义');
  await clickByRole(page, '新增组件');
  await fill(page, 'input[placeholder="secure-api"]', `component-${runId}`);
  await fill(page, 'input[placeholder="选择或填写已登记镜像 ID"]', image.id);
  await fill(page, 'input[placeholder="1.0.0"]', '1.0.0');
  await select(page, 'select[title="创建飞地配置前，组件隔离级别需选择 enclave"]', 'enclave');
  await clickByRole(page, '保存');
  await expectNoErrorToast(page, '新增组件');
  const snap = await dashboard(page);
  const component = snap.components.find(item => item.name === `component-${runId}`);
  assert(component, '组件创建后未出现在 dashboard');
  return component;
}

async function verifyDeployment(page) {
  await clickText(page, '交付与部署');
  await clickText(page, '集群节点');
  await clickByRole(page, '注册节点');
  await fill(page, 'input[placeholder="sgx-node-01"]', `node-${runId}`);
  await fill(page, 'input[placeholder="10.0.0.11"]', '10.0.0.11');
  await fill(page, 'input[title="填写平台可连通的 SSH 主机名或 IPv4 地址"]', '10.0.0.11');
  await clickByRole(page, '取消');
  const image = await createImage(page);
  const component = await createComponent(page, image);
  await page.once('dialog', async dialog => await dialog.accept('2'));
  await page.getByRole('row').filter({ hasText: `component-${runId}` }).getByRole('button', { name: '扩缩容' }).click();
  clicked.push('组件扩缩容');
  await waitReady(page);
  await page.once('dialog', async dialog => await dialog.accept('1.0.1'));
  await page.getByRole('row').filter({ hasText: `component-${runId}` }).getByRole('button', { name: '升级' }).click();
  clicked.push('组件升级');
  await waitReady(page);
  await page.getByRole('row').filter({ hasText: `component-${runId}` }).getByRole('button', { name: 'Manifest' }).click();
  clicked.push('组件 Manifest');
  await waitReady(page);
  await closeOverlay(page);
  await clickText(page, '安装包');
  await clickByRole(page, '导入安装包');
  await fill(page, 'input[placeholder="k3s-offline-bundle"]', `pkg-${runId}`);
  await fill(page, 'input[placeholder="v1.30.4+k3s1"]', 'v1.30.4+k3s1');
  await clickByRole(page, '取消');
  await clickText(page, '应用市场');
  await clickByRole(page, '新增应用');
  await fill(page, 'input[placeholder="secure-redis"]', `app-${runId}`);
  await fill(page, 'input[placeholder="database"]', 'database');
  await fill(page, 'input[placeholder="1.0.0"]', '1.0.0');
  await clickByRole(page, '取消');
  const appRow = page.locator('tbody tr').first();
  if (await appRow.isVisible().catch(() => false)) {
    const publish = appRow.getByRole('button', { name: '发布' });
    if (await publish.isVisible().catch(() => false)) {
      await publish.click();
      clicked.push('应用发布');
      await waitReady(page);
    }
    const unpublish = appRow.getByRole('button', { name: '下架' });
    if (await unpublish.isVisible().catch(() => false)) {
      await unpublish.click();
      clicked.push('应用下架');
      await waitReady(page);
    }
    const addVersion = appRow.getByRole('button', { name: '加版本' });
    if (await addVersion.isVisible().catch(() => false)) {
      await page.once('dialog', async dialog => await dialog.accept('1.0.1'));
      await addVersion.click();
      clicked.push('应用加版本');
      await waitReady(page);
    }
  }
  return { image, component };
}

async function verifySecurity(page) {
  await clickText(page, '安全合规');
  await clickText(page, '安全策略');
  await clickByRole(page, '新增策略');
  await fill(page, 'input[placeholder="deny-egress-public"]', `policy-${runId}`);
  await fill(page, 'input[placeholder="network"]', 'network');
  await fill(page, 'input[placeholder="namespace/default"]', `namespace/${runId}`);
  await select(page, 'select[title="active 会尝试发布；staged 仅保存草稿；disabled 不下发"]', 'disabled');
  await clickByRole(page, '保存');
  await expectNoErrorToast(page, '新增策略');
  await page.getByRole('row').filter({ hasText: `policy-${runId}` }).getByRole('button', { name: '发布' }).click();
  clicked.push('策略发布');
  await waitReady(page);
  await clickText(page, '网络隔离');
  const netSnap = await dashboard(page);
  const usedVlans = new Set((netSnap.networks || []).filter(item => item.parentNic === 'eth0').map(item => Number(item.vlanId)));
  let vlan = 3000;
  while (usedVlans.has(vlan) && vlan < 4094) vlan += 1;
  await clickByRole(page, '新增网络');
  await fill(page, 'input[placeholder="prod-external"]', `net-${runId}`);
  await select(page, 'select[title="选择所有节点上统一存在的物理网卡"]', 'eth0');
  await fill(page, 'input[placeholder="10.0.0.0/24"]', '10.250.0.0/24');
  await fill(page, 'input[placeholder="10.0.0.1"]', '10.250.0.1');
  await fill(page, 'input[placeholder="120"]', String(vlan));
  await clickByRole(page, '保存');
  await expectNoErrorToast(page, '新增网络');
  await clickText(page, '拓扑视图');
  await page.getByRole('button', { name: '添加物理出口' }).waitFor({ state: 'visible', timeout: 8000 });
  await clickText(page, '合规扫描');
  await clickByRole(page, '开始扫描');
  await waitReady(page);
  await clickText(page, '合规分析');
  await clickText(page, '合规任务');
  await clickByRole(page, '新增任务');
  await fill(page, 'input[placeholder="ISO27001-A.8.9"]', `CTRL-${runId}`);
  await fill(page, 'input[placeholder="配置管理"]', '配置管理');
  await fill(page, 'input[placeholder="张三"]', 'ops-team');
  await fill(page, 'input[placeholder="补充整改说明"]', '浏览器 E2E 验证任务');
  await clickByRole(page, '保存');
  await expectNoErrorToast(page, '新增合规任务');
  await clickText(page, '合规报告');
}

async function verifyTrusted(page, component) {
  await clickText(page, '可信计算');
  await clickText(page, '可信资源');
  await clickByRole(page, '添加可信资源');
  const hasNode = await page.locator('select[title="选择已登记且未绑定可信资源的 SGX 节点"] option').count();
  if (hasNode > 1) {
    const value = await page.locator('select[title="选择已登记且未绑定可信资源的 SGX 节点"] option').nth(1).getAttribute('value');
    await select(page, 'select[title="选择已登记且未绑定可信资源的 SGX 节点"]', value || '');
    await fill(page, 'input[placeholder="256"]', '256');
    await fill(page, 'input[placeholder="0"]', '0');
    await clickByRole(page, '取消');
  } else {
    await clickByRole(page, '取消');
  }
  await clickText(page, '飞地配置');
  await clickByRole(page, '添加飞地配置');
  await fill(page, 'input[placeholder="已登记的 enclave 组件 ID"]', component.id);
  await fill(page, 'input[placeholder="64位十六进制度量值"]', 'abcdef0123456789');
  await fill(page, 'input[placeholder="64位十六进制签名者值"]', '0123456789abcdef');
  await fill(page, 'input[placeholder="require-dcap-verified"]', 'require-dcap-verified');
  await clickByRole(page, '保存');
  await expectNoErrorToast(page, '新增飞地配置');
  await clickText(page, '密钥管理');
  await clickByRole(page, '添加密钥');
  await fill(page, 'input[placeholder="api-signing-key"]', `key-${runId}`);
  await fill(page, 'input[placeholder="已登记组件 ID"]', component.id);
  await select(page, 'select[title="选择密钥算法"]', 'ECDSA');
  await clickByRole(page, '保存');
  await expectNoErrorToast(page, '新增密钥');
  await clickText(page, '远程证明');
  await clickByRole(page, '运行证明');
  await waitReady(page);
  await clickText(page, '飞地巡检');
  await clickByRole(page, '执行巡检');
  await waitReady(page);
}

async function verifySystem(page) {
  await clickText(page, '系统管理');
  await clickText(page, '系统设置');
  await clickByRole(page, '添加配置');
  await select(page, 'select[title="选择配置所属类别"]', 'general');
  await fill(page, 'input[placeholder="session_timeout_minutes"]', `setting_${runId}`);
  await fill(page, 'input[placeholder="30"]', '30');
  await clickByRole(page, '保存');
  await expectNoErrorToast(page, '添加系统设置');
  await clickText(page, '资源配额');
  await clickByRole(page, '新增配额');
  await fill(page, 'input[placeholder="namespace/default"]', `namespace/${runId}`);
  await fill(page, 'input[placeholder="2"]', '2');
  await fill(page, 'input[placeholder="4Gi"]', '4Gi');
  await fill(page, 'input[placeholder="20"]', '20');
  await clickByRole(page, '保存');
  await expectNoErrorToast(page, '新增配额');
  await clickText(page, '账号管理');
  await clickByRole(page, '创建账号');
  await fill(page, 'input[placeholder="ops-user"]', `user-${runId}`);
  await fill(page, 'input[placeholder="运维操作员"]', 'E2E 操作员');
  await fill(page, 'input[placeholder="至少 8 位"]', 'Passw0rd123');
  await clickByRole(page, '取消');
  await clickText(page, '版本升级');
  await page.getByRole('button', { name: '执行升级' }).waitFor({ state: 'visible', timeout: 8000 });
  await clickByRole(page, '执行升级');
  await expectNoErrorToast(page, '执行升级');
  await page.getByRole('button', { name: '取消升级' }).waitFor({ state: 'visible', timeout: 8000 });
  await clickByRole(page, '取消升级');
  await expectNoErrorToast(page, '取消升级');
  await page.getByRole('button', { name: '执行升级' }).waitFor({ state: 'visible', timeout: 8000 });
  await clickText(page, '告警事件');
  await fill(page, 'input[title="CPU 使用率告警阈值，单位 %，示例 80"]', '81');
  await fill(page, 'input[title="内存使用率告警阈值，单位 %，示例 80"]', '82');
  await fill(page, 'input[title="Pod 数量告警阈值，示例 100"]', '101');
  await clickByRole(page, '保存');
  await expectNoErrorToast(page, '保存告警阈值');
  await clickText(page, '集群日志');
  await clickText(page, '审计日志');
}

const browser = await chromium.launch({ headless, slowMo });
const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
page.on('pageerror', error => errors.push(`pageerror: ${error.message}`));
page.on('console', message => {
  if (message.type() === 'error' && !message.text().includes('401 (Unauthorized)')) errors.push(`console: ${message.text()}`);
});
try {
  await login(page);
  await cleanup(page);
  await verifyNavigation(page);
  const { component } = await verifyDeployment(page);
  await verifySecurity(page);
  await verifyTrusted(page, component);
  await verifySystem(page);
  await cleanup(page);
  if (errors.length > 0) throw new Error(errors.join('\n'));
  console.log(`button-e2e ok: clicked ${clicked.length} controls`);
  console.log(clicked.join('\n'));
} finally {
  await browser.close();
}
