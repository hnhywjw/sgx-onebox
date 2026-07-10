# 版本说明

## v20260710-r2

**发布日期**: 2026-07-10

### 第八轮审计修复

- **DeleteComponent**: 阻止删除已部署/部署中的组件，防止误操作导致运行中断
- **UpgradeComponent**: 修复审计日志 actor 硬编码，正确记录操作用户

### 累计安全加固 (52项修复)

#### 认证与授权
- BearerToken 同源校验 (仅 Cookie 认证时生效)
- Login 全流程写锁防 TOCTOU
- Token 14天过期 + 创建时间戳
- 密码修改自动撤销所有会话
- 登录锁定 + 定时缓解

#### 防护
- SSRF: isSafeEndpoint 完整私有IP检测 (含 IPv4-mapped IPv6)
- 命令注入: buildArgs 白名单过滤
- 路径遍历: filepath.Clean 防绝对路径绕过
- CSRF: Cookie 认证强制同源检查
- CSP: 移除 unsafe-inline + 4个安全响应头

#### 数据库与并发
- WAL/busy_timeout 错误检查
- cloneSnapshot 健壮性
- runImageBuild panic 恢复 + 审计
- provisioning 30分钟超时恢复

#### 密钥与加密
- HMAC/AES 密钥分离 (SHA-256 派生)
- crypto/rand 降级策略
- SSH 默认严格主机密钥检查

## v20260710

**发布日期**: 2026-07-10 (初始发布于 GitHub)

### 核心功能
- 集群节点管理 (K3s)
- 组件部署与编排
- 镜像构建 (Docker)
- 安装包管理 (ISO/Package/Offline)
- 插件系统 (Webhook)
- 审计日志
- RBAC 角色访问控制
- 离线部署支持
