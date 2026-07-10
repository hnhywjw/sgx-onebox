# SGX-OneBox 部署说明

## 系统要求

- **操作系统**: Ubuntu 22.04 (jammy) amd64
- **CPU**: 支持 Intel SGX 的处理器
- **内存**: 最低 4GB，推荐 8GB+
- **磁盘**: 最低 20GB 可用空间

## 快速部署

### 1. 下载软件包

```bash
wget https://github.com/hnhywjw/sgx-onebox/releases/latest/download/sgx-onebox-v20260710-r2.tar.gz
tar -xzf sgx-onebox-v20260710-r2.tar.gz
cd sgx-onebox-v20260710-r2
```

### 2. 安装 SGX 运行时 (如需离线部署)

```bash
cd runtime-bundles/linux-amd64
chmod +x install.sh install-sgx.sh
sudo ./install.sh
sudo ./install-sgx.sh
```

### 3. 配置环境变量

必须设置 `PLATFORM_SECRET` 作为 Token 签名和数据加密的主密钥：

```bash
export PLATFORM_SECRET=$(openssl rand -hex 32)
```

### 4. 启动服务

```bash
chmod +x bin/sgx-onebox
./bin/sgx-onebox
```

服务默认监听 `0.0.0.0:8080`，同时提供 API 接口和前端页面。

### 5. 验证服务

```bash
curl http://localhost:8080/api/v1/health
```

预期返回: `{"db":"connected","status":"ok"}`

## 访问前端

浏览器打开 `http://<服务器IP>:8080`

### 默认账户

| 角色 | 用户名 | 密码 |
|------|--------|------|
| 平台管理员 | admin | admin123 |
| 安全管理员 | security-admin | secure123 |
| 操作员 | operator | ops123 |
| 审计员 | auditor | audit123 |

## 数据存储

数据存储在运行目录下的 `platform.db` SQLite 文件中，首次启动自动初始化。

## 安全配置

### PLATFORM_SECRET

用于 Token 签名和数据加密的主密钥，长度需 >= 16 字符。生产环境请使用强随机值。
一旦设定不可变更，否则所有已签发的 Token 将失效。

### CORS 配置

设置 `CORS_ORIGIN` 环境变量可配置允许的跨域来源：

```bash
export CORS_ORIGIN="https://your-frontend.example.com"
```

### 测试模式

开发测试时可启用以简化验证（跳过验证码、放宽 Origin 检查）：

```bash
export GO_TEST_MODE=1
```

### 可选环境变量

| 变量 | 默认值 | 说明 |
|------|--------|------|
| PLATFORM_STATIC_DIR | apps/web/dist | 前端静态文件目录 |
| EXECUTOR_POLL_SECONDS | 10 | 节点巡检间隔(秒) |
| EXECUTOR_SSH_TIMEOUT_SECONDS | 8 | SSH 连接超时(秒) |
| SSH_STRICT_HOST_KEY_CHECKING | 1 | SSH 主机密钥检查(设为 0 禁用) |
| K3S_TOKEN | (无) | K3s 集群 join token |

## 服务管理

### 使用 systemd 管理

```bash
sudo cp deploy/sgx-onebox.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable sgx-onebox
sudo systemctl start sgx-onebox
```

## 注意事项

1. 首次启动后立即修改所有默认账户密码
2. PLATFORM_SECRET 生产环境务必使用随机强密钥
3. 生产环境不要启用 GO_TEST_MODE
4. 定期备份 platform.db 文件
5. 组件删除前请确认已停止运行
