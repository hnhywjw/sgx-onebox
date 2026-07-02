# Requirements Document: 内置 SSH 执行器

## Introduction

为平台构建进程内 SSH 执行器，替代当前所有模拟/随机数据，实现真实的集群节点管理：指标采集、节点纳管（k3s join）、静默回退。

## Glossary

- **Executor**: 嵌入 platform-api 的 Go 协程组件，负责 SSH 连接到集群节点并执行运维操作
- **Node Agent**: 目标节点上无需安装额外 agent，执行器通过 SSH 标准协议远程执行命令
- **Silent Fallback**: SSH 连接失败时不中断平台服务，自动回退为 `normalize()` 生成的估算指标
- **Poll Cycle**: 执行器按固定周期（默认 10 秒）对所有注册节点执行一次指标采集

## Requirements

### R1: SSH 指标采集

**User Story:** AS 平台运维员, I want 平台自动采集节点的真实 CPU /内存/磁盘/网络指标, so that 仪表板展示的是真实运行数据而非随机数。

#### Acceptance Criteria

1. WHEN 执行器 Poll Cycle 到达, the system SHALL 通过 SSH 连接到每个 `sshHost`:`sshPort` 不为空的节点
2. WHEN 连接成功后, the system SHALL 执行远程命令获取 CPU 使用率、内存使用率、磁盘使用率、网卡接收/发送字节数和速率
3. WHEN 指标采集成功, the system SHALL 将采集值写入对应 `ClusterNode` 的 `cpuUsage`/`memoryUsage`/`diskUsage`/`rxBytes`/`txBytes`/`rxRate`/`txRate` 字段并持久化
4. WHILE 一个 Poll Cycle 运行中, the system SHALL 并行采集所有节点指标（不串行等待）
5. WHEN 指标采集成功, the system SHALL 更新 `lastHeartbeat` 为当前时间戳

### R2: 节点纳管执行

**User Story:** AS 平台运维员, I want 添加的新节点能自动完成 k3s join, so that 节点注册后自动加入集群而非停留在 "credential_ready" 空状态。

#### Acceptance Criteria

1. WHEN 用户通过 API 创建新节点且 `sshPassword` 已配置, the system SHALL 生成 k3s join 命令并写入 `joinCommand` 字段
2. WHEN 节点状态为 `joinStatus: "credential_ready"` 且 Poll Cycle 到达, the system SHALL 通过 SSH 在目标节点上执行 join 命令
3. WHEN join 命令执行成功（进程返回码为 0）, the system SHALL 将 `joinStatus` 更新为 `"active"`, 将 `lastJoinMessage` 更新为 "节点已通过 SSH 加入集群"
4. IF join 命令执行失败（返回码非 0 或超时）, the system SHALL 将 `joinStatus` 更新为 `"failed"`, 将错误消息写入 `lastJoinMessage`, 且不阻塞其他节点的采集

### R3: 静默回退

**User Story:** AS 平台运维员, I want SSH 连接失败时平台不发生报错和数据中断, so that 网络波动或节点离线不影响平台可用性。

#### Acceptance Criteria

1. IF SSH 连接任一节点失败（认证失败、超时、网络不可达）, the system SHALL 跳过该节点本次采集, 保留该节点上次成功采集的指标值
2. IF 节点从未成功采集过指标, the system SHALL 使用 `normalize()` 生成的估算值作为初始数据
3. WHEN 任何回退发生, the system SHALL 在日志中记录 `WARN` 级别信息（包含节点 ID 和失败原因）
4. IF 节点连续 3 个 Poll Cycle 连接失败, the system SHALL 将 `status` 更新为 `"unreachable"`

### R4: 密码安全

**User Story:** AS 安全管理员, I want SSH 密码以密文存储, so that 数据文件泄露时攻击者无法直接获取节点凭据。

#### Acceptance Criteria

1. WHEN 密码被写入存储, the system SHALL 使用 `PLATFORM_SECRET` 环境变量作为密钥, 通过 AES-256-GCM 加密后存入 `sshPasswordCiphertext`
2. WHEN 执行器需要 SSH 连接, the system SHALL 从 `sshPasswordCiphertext` 解密获取明文密码
3. WHEN `PLATFORM_SECRET` 环境变量未设置, the system SHALL 拒绝启动并输出 `FATAL: PLATFORM_SECRET must be set` 错误

### R5: 配置可控

**User Story:** AS 平台管理员, I want 控制执行器的采集周期和超时设置, so that 执行器不会过度占用节点资源。

#### Acceptance Criteria

1. WHILE 执行器运行, the system SHALL 默认每 10 秒执行一次 Poll Cycle
2. WHILE 执行单次 SSH 操作, the system SHALL 默认超时为 8 秒
3. WHEN 环境变量 `EXECUTOR_POLL_SECONDS` 已设置, the system SHALL 使用该值作为 Poll Cycle 间隔
4. WHEN 环境变量 `EXECUTOR_SSH_TIMEOUT_SECONDS` 已设置, the system SHALL 使用该值作为单次 SSH 超时时间
