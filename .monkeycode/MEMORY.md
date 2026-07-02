# 用户指令记忆

本文件记录了用户的指令、偏好和教导，用于在未来的交互中提供参考。

## 格式

### 用户指令条目
用户指令条目应遵循以下格式：

[用户指令摘要]
- Date: [YYYY-MM-DD]
- Context: [提及的场景或时间]
- Instructions:
  - [用户教导或指示的内容，逐行描述]

### 项目知识条目
Agent 在任务执行过程中发现的条目应遵循以下格式：

[项目知识摘要]
- Date: [YYYY-MM-DD]
- Context: Agent 在执行 [具体任务描述] 时发现
- Category: [运维部署|构建方法|测试方法|排错调试|工作流协作|环境配置]
- Instructions:
  - [具体的知识点，逐行描述]

## 去重策略
- 添加新条目前，检查是否存在相似或相同的指令
- 若发现重复，跳过新条目或与已有条目合并
- 合并时，更新上下文或日期信息
- 这有助于避免冗余条目，保持记忆文件整洁

## 条目

[项目知识摘要]
- Date: 2026-06-17
- Context: Agent 在执行平台初始化时发现
- Category: 环境配置
- Instructions:
  - 所有回复使用简体中文
  - Web 项目开发后需要补充本地预览配置

[项目验证命令]
- Date: 2026-06-27
- Context: Agent 在执行平台功能梳理与可用性验证时发现
- Category: 构建方法
- Instructions:
  - 后端测试使用 `GO_TEST_MODE=1 go test ./...`
  - 前端类型检查使用 `npm run typecheck:web`
  - 前端 lint 使用 `npm run lint:web`
  - 前端生产构建使用 `npm run build:web`
  - 前端 smoke 验证需先有 `apps/web/dist/index.html`，再运行 `SMOKE_BASE_URL=http://localhost:8080 npm run smoke:e2e --workspace apps/web`
  - 浏览器逐按钮 E2E 验证使用 `E2E_BASE_URL=http://localhost:8080 npm run button:e2e --workspace apps/web`
