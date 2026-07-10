# 版本说明

## v20260710

**发布日期**: 2026-07-10

### 安全修复 (第六轮审计)

- **SSRF 防护**: 修复 `isSafeEndpoint` 检查逻辑反转（私有 IP 原被错误放行），新增 IPv4 映射 IPv6 地址处理和 `IsUnspecified` 检查
- **CSRF 防护**: `requireUser` 同源检查改为仅对 Cookie 认证生效，Bearer token 客户端不再被误拦截
- **命令注入防护**: 新增 `isSafeBuildArgs` 白名单过滤，阻止镜像构建参数注入
- **路径遍历**: `SaveInstallPackage` 增加 `filepath.Clean` 防绝对路径绕过
- **上传响应**: 恢复 `path` 字段，修复前端所有上传功能
- **镜像构建**: panic 恢复时更新镜像状态为 critical 并记录审计日志
- **任务恢复**: provisioning 任务 30 分钟超时自动恢复停滞状态

### v20260702

**发布日期**: 2026-07-02

### 安全修复 (第三至五轮审计)

- **认证安全**: BearerToken 强制同源校验，Login 全流程写锁内完成（消除 TOCTOU），Token 增加创建时间戳 14 天过期，登录判定改用 `errors.Is` sentinel error
- **CSP/安全头**: 移除 `unsafe-inline`，新增 Cross-Origin-Resource-Policy/Opener-Policy/X-Permitted-Cross-Domain-Policies
- **密钥管理**: `deriveKey()` 做 HMAC/AES 密钥分离，`crypto/rand.Read` 失败降级策略
- **SQLite**: WAL/busy_timeout PRAGMA 错误检查，cloneSnapshot 序列化失败返回原快照
- **SSH 安全**: 默认 `SSH_STRICT_HOST_KEY_CHECKING=1`，命令注入黑名单扩展，文件上传 500MB 上限+路径清理
- **其他**: DeleteUser 自删阻止，SSHPassword/Ciphertext 响应中清除，登录限流定期清理，decodeJSON 1MB 限制

### v20260701

**发布日期**: 2026-07-01

### 功能

- 初始版本发布
- 集群节点管理（K3s）
- 组件部署与编排
- 镜像构建（Docker）
- 安装包管理（ISO/Package/Offline）
- 插件系统（Webhook）
- 审计日志
- 基于角色的访问控制（RBAC）
- 离线部署支持（air-gap）
