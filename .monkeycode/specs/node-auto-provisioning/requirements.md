# Requirements Document

## Introduction

本需求定义 SGX OneBox Platform 的节点自动装机与自动纳管能力。平台管理员在现有页面风格下添加目标节点后，平台通过 SSH 在目标主机上完成前置检查、K3s 控制面或工作节点安装、container runtime 与 kubectl 准备、SGX 硬件检测、SGX 驱动与 DCAP 工具链安装、验证与审计闭环。目标是把平台从“管理已准备好的节点”升级为“自动准备并纳管目标节点”。

## Glossary

- **管理控制面**：运行 SGX OneBox Platform 的主机与 `platform-api` 进程。
- **目标节点**：由平台通过 SSH 自动准备并纳管的 Linux 主机。
- **控制面节点**：运行 K3s server 角色的目标节点。
- **工作节点**：运行 K3s agent 角色的目标节点。
- **SGX 节点**：具备 SGX 硬件能力并需要安装 SGX/DCAP 工具链的目标节点。
- **自动装机任务**：平台为目标节点执行的一组检查、安装、验证、回滚与审计步骤。
- **离线资源包**：包含 K3s、container runtime、kubectl、SGX 驱动、DCAP 包、脚本与校验文件的本地资源包。
- **阶段状态**：自动装机任务的步骤级状态，包括 `pending`、`running`、`succeeded`、`failed`、`skipped`。

## Requirements

### Requirement 1

**User Story:** AS 平台管理员, I want 添加目标节点时选择节点角色和能力, so that 平台可以自动完成对应软件安装与纳管。

#### Acceptance Criteria

1. WHEN 平台管理员打开节点新增表单, the system SHALL 使用现有页面视觉风格展示节点基础信息、SSH 凭据、节点角色、SGX 能力和安装模式字段。
2. WHEN 平台管理员选择控制面节点角色, the system SHALL 创建包含 K3s server、kubectl、container runtime 和健康检查步骤的自动装机任务。
3. WHEN 平台管理员选择工作节点角色, the system SHALL 创建包含 K3s agent、container runtime、join token 注入和健康检查步骤的自动装机任务。
4. WHEN 平台管理员启用 SGX 能力, the system SHALL 创建包含 SGX 硬件检测、驱动安装、DCAP 工具链安装和证明工具验证步骤的自动装机任务。
5. IF 节点表单缺少 SSH 主机、端口、用户名或认证凭据, the system SHALL 拒绝创建自动装机任务并展示可修复字段。

### Requirement 2

**User Story:** AS 运维员, I want 平台在目标节点上自动完成环境前置检查, so that 不满足条件的主机可以在安装前给出明确原因。

#### Acceptance Criteria

1. WHEN 自动装机任务启动, the system SHALL 通过 SSH 采集目标节点的操作系统、内核、CPU 架构、磁盘空间、内存、网络连通性和已有 K3s 状态。
2. WHEN 目标节点需要 SGX 能力, the system SHALL 检测 CPU SGX flag、BIOS SGX 可用性、`/dev/sgx_enclave`、`/dev/sgx_provision` 和 EPC 信息。
3. IF 前置检查未通过, the system SHALL 将任务状态更新为 `failed` 并记录失败检查项、节点、命令摘要和修复建议。
4. WHEN 前置检查通过, the system SHALL 将任务推进到安装阶段并记录审计事件。

### Requirement 3

**User Story:** AS 运维员, I want 平台自动安装 K3s 控制面与工作节点, so that 新主机可以直接加入平台管理的集群。

#### Acceptance Criteria

1. WHEN 目标节点角色为控制面节点, the system SHALL 在目标节点安装 K3s server 并生成或读取集群 token。
2. WHEN 目标节点角色为工作节点, the system SHALL 从平台记录的控制面地址和 token 生成 K3s agent join 命令并在目标节点执行。
3. WHEN K3s 安装完成, the system SHALL 验证 `kubectl get nodes` 或 `k3s kubectl get nodes` 返回目标节点 Ready 状态。
4. IF K3s 安装或 join 失败, the system SHALL 保留失败阶段、远程输出摘要和下一步建议。
5. WHILE K3s 安装运行中, the system SHALL 在节点列表展示阶段进度和当前步骤。

### Requirement 4

**User Story:** AS 运维员, I want 平台自动准备 kubectl 与 container runtime, so that 后续镜像校验、部署和策略下发可以真实执行。

#### Acceptance Criteria

1. WHEN K3s 安装完成, the system SHALL 验证 `kubectl` 或 `k3s kubectl` 可用性。
2. WHEN container runtime 检查执行, the system SHALL 验证 `containerd`、`crictl` 或 `ctr` 的可用性。
3. IF 目标节点缺少所需运行时工具, the system SHALL 通过在线仓库或离线资源包安装对应工具。
4. WHEN 工具安装完成, the system SHALL 记录工具版本、安装来源和验证结果。

### Requirement 5

**User Story:** AS 安全管理员, I want 平台自动安装和验证 SGX/DCAP 工具链, so that SGX 节点可以执行远程证明和飞地巡检。

#### Acceptance Criteria

1. WHEN SGX 节点通过硬件检查, the system SHALL 安装目标系统匹配的 SGX 驱动、PSW、DCAP Quote Provider Library 和 DCAP 验证工具。
2. WHEN SGX/DCAP 安装完成, the system SHALL 执行 SGX 设备文件检查、EPC 信息检查和 DCAP 验证工具检查。
3. WHEN DCAP 验证工具可用, the system SHALL 将 SGX 节点状态更新为 `sgx_ready` 并记录工具版本。
4. IF 目标节点缺少 SGX 硬件能力, the system SHALL 将 SGX 阶段状态更新为 `failed` 并保留 K3s 纳管结果。
5. IF DCAP 安装失败, the system SHALL 保留节点基础纳管状态并将 SGX 能力标记为 `sgx_pending`。

### Requirement 6

**User Story:** AS 交付工程师, I want 平台支持在线和离线两种安装来源, so that 无互联网现场也能完成节点自动装机。

#### Acceptance Criteria

1. WHEN 平台管理员选择在线安装模式, the system SHALL 使用配置的官方或私有仓库地址下载 K3s、runtime 与 SGX/DCAP 依赖。
2. WHEN 平台管理员选择离线安装模式, the system SHALL 从已导入的离线资源包读取安装文件、脚本和校验文件。
3. WHEN 离线资源包被导入, the system SHALL 校验包格式、SHA256、组件清单、目标 OS 兼容性和版本信息。
4. IF 安装源不可用或校验失败, the system SHALL 阻止自动装机任务进入安装阶段并展示失败来源。

### Requirement 7

**User Story:** AS 平台管理员, I want 查看每个节点自动装机任务的阶段状态, so that 可以定位安装失败点并重试。

#### Acceptance Criteria

1. WHEN 自动装机任务创建, the system SHALL 为任务生成唯一 ID、目标节点 ID、计划步骤、当前阶段和创建人。
2. WHILE 自动装机任务运行, the system SHALL 展示步骤进度、当前命令摘要、开始时间、更新时间和最近输出摘要。
3. WHEN 某阶段失败, the system SHALL 支持从失败阶段重试任务。
4. WHEN 自动装机任务完成, the system SHALL 将节点状态更新为 `ready` 或 `sgx_ready` 并记录完整审计事件。
5. IF 管理员取消任务, the system SHALL 停止后续阶段并把任务状态更新为 `cancelled`。

### Requirement 8

**User Story:** AS 审计员, I want 自动装机过程产生可审计证据, so that 后续合规报告可以引用真实安装和验证记录。

#### Acceptance Criteria

1. WHEN 自动装机任务执行每个阶段, the system SHALL 记录阶段名称、执行结果、输出摘要、操作者和时间戳。
2. WHEN 节点完成 K3s 纳管, the system SHALL 记录 K3s 版本、节点角色、runtime 版本和 Ready 验证证据。
3. WHEN SGX/DCAP 验证完成, the system SHALL 记录 SGX 设备、EPC 信息、DCAP 工具版本和验证命令结果摘要。
4. WHEN 合规报告生成, the system SHALL 引用节点自动装机任务中的真实证据来源。

### Requirement 9

**User Story:** AS 安全管理员, I want 自动装机过程保护凭据和 token, so that SSH 密码和 K3s token 不会暴露在页面、快照或日志中。

#### Acceptance Criteria

1. WHEN 平台保存 SSH 凭据, the system SHALL 加密存储凭据并在 API 响应中仅返回配置状态。
2. WHEN 平台使用 K3s token, the system SHALL 在执行时注入 token 并在存储、日志和页面中脱敏显示。
3. WHEN 远程命令输出包含敏感字段, the system SHALL 在持久化前对敏感字段进行脱敏。
4. IF 凭据解密失败, the system SHALL 停止自动装机任务并记录安全失败事件。
