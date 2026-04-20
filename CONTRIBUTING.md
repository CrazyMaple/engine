# 贡献指南

感谢对 Engine 项目的关注！本指南描述了代码规范、PR 流程、测试要求和提交信息约定。所有贡献者（内部或社区）都应遵循本指南。

## 开发环境

- Go 1.24+
- 推荐 IDE：GoLand 或 VS Code + Go 扩展
- OS：Linux / macOS（Windows 可用 WSL2）

```bash
git clone https://github.com/engine/engine.git
cd engine
go mod tidy
go build ./...
go test ./...
```

## 仓库结构概览

| 目录 | 模块 | 负责人 |
|------|------|--------|
| `actor/` | Actor 核心（Cell/Context/Props/Mailbox） | core-team |
| `remote/` | 分布式通信（TCP、加密、端点） | core-team |
| `cluster/`, `cluster/canary/` | 集群 Gossip、灰度、A/B 测试 | infra-team |
| `grain/` | 虚拟 Actor | core-team |
| `scene/`, `ecs/` | 游戏场景 + ECS | game-team |
| `room/`, `matcher` | 房间/匹配 | game-team |
| `inventory/`, `quest/`, `leaderboard/`, `mail/` | 游戏业务模块 | game-team |
| `syncx/`, `replay/` | 状态同步、回放 | game-team |
| `network/`, `gate/` | 传输层（TCP/WebSocket/KCP）+ 网关 | infra-team |
| `dashboard/` | 运维面板 + REST API | infra-team |
| `persistence/` | Storage 接口 + Memory/MongoDB | infra-team |
| `log/`, `middleware/` | 日志聚合、中间件链 | infra-team |
| `codegen/`, `codec/` | 代码生成、编解码 | infra-team |
| `config/`, `hotreload/` | 配置加载、热重载 | infra-team |
| `timer/`, `saga/`, `pubsub/`, `router/` | 通用基础设施 | infra-team |
| `stress/`, `bench/`, `testkit/` | 压测、基准、测试工具 | qa-team |
| `deploy/helm`, `deploy/k8s`, `deploy/operator` | 部署 | devops-team |
| `cmd/engine/` | CLI 入口 | core-team |
| `better/` | vendored 参考实现（只读） | — |

## 代码规范

### 格式化

- 所有 Go 文件必须通过 `go fmt ./...`
- `go vet ./...` 不允许有警告
- 推荐在本地安装 `golangci-lint` 并启用 `govet / errcheck / ineffassign / staticcheck`

### 命名

- 包名：小写、无下划线（`canary`、`syncx`、`statesync`）
- 导出符号：大驼峰，首字母大写（`ExperimentAnalysis`、`NewArchiver`）
- 内部符号：小驼峰（`assignVariant`、`stdNormalCDF`）
- 测试文件：`xxx_test.go`
- 基准测试：`BenchmarkXxx`

### 注释

- 每个导出符号必须有一行中文或英文注释（以符号名开头）
- 注释应说明 **Why**，不要重复说 **What**（代码已经说了）
- 不要保留历史遗留注释（"old version"、"// todo fix before v1.6"），用 issue 跟踪

### 文件大小

- 单个 `.go` 文件控制在 **500 行以内**，超过则按职责拆分
- 单个函数控制在 **80 行以内**（合理例外：状态机、长 switch）

### 依赖原则

- **核心模块零外部依赖**：`actor/`、`remote/`、`network/`、`internal/`、`cluster/` 不得引入第三方库
- 外部依赖仅允许出现在 `persistence/mongodb`、`cluster/provider/*`、`deploy/*` 等边缘模块
- 不允许直接 import `better/` 下的代码

### 错误处理

- 内部调用链：尽量返回 `error`，让调用者决定
- 对外 API：在 `errors/` 下定义统一错误类型（`ConnectError`、`TimeoutError` 等）
- 不要用 `panic` 表达业务错误；只有程序 bug（如 nil Pointer、数组越界）才允许 panic

## PR 流程

1. **先开 issue 讨论**（除非是明显的 bug fix 或文档改动）
2. Fork 仓库 → 建立 feature 分支：`feature/xxx` 或 `fix/xxx`
3. 提交前自测：
   ```bash
   go build ./...
   go vet ./...
   go test ./...
   ```
4. PR 标题使用 commit message 约定（见下）
5. PR 描述需包含：
   - 要解决的问题 / 要达成的目标
   - 实现思路（为什么选择 A 方案而不是 B）
   - 测试覆盖情况
   - 关联的 issue / 设计文档（如有）
6. 至少 1 名模块负责人 review 后方可合并
7. 合并方式：**Squash and merge**（保留单条清洁的 commit）

## 测试要求

| 变更类型 | 最低测试要求 |
|---------|-------------|
| 新模块 | 包内部 > 60% 行覆盖率；关键路径必须有端到端测试 |
| bug fix | 提供一个能复现原 bug 的失败测试 + 修复后变绿 |
| 重构 | 测试只调整结构，行为不变，整体覆盖率不降 |
| 性能优化 | 在 `bench/` 下新增基准测试 + 在 PR 描述中给出 before/after 数据 |

- 测试文件与被测文件位于同一个包（除非特殊原因）
- 外部依赖（MongoDB、Redis、对象存储）必须有接口抽象 + 内存 Mock 实现；单元测试不得连接真实服务
- `stress/` 测试默认 `t.Skip` 降级，不阻塞主流程；CI 中用独立 Job 运行

## 提交信息（Commit Message）

使用 [Conventional Commits](https://www.conventionalcommits.org/) 格式：

```
<type>(<scope>): <subject>

<body 可选>

<footer 可选>
```

### type 列表

| type | 用途 |
|------|------|
| `feat` | 新功能 |
| `fix` | bug 修复 |
| `perf` | 性能优化 |
| `refactor` | 重构（不改变行为） |
| `docs` | 文档 |
| `test` | 测试 |
| `build` | 构建脚本、依赖 |
| `ci` | CI 配置 |
| `chore` | 其他杂项（不影响源码） |

### scope

模块名，与仓库目录对齐：`actor`、`remote`、`cluster`、`canary`、`replay`、`dashboard`、`stress`、`helm`、`docs` 等。跨多模块时可省略或写 `*`。

### 示例

```
feat(canary): add Welch's t-test and Wilson CI for A/B analysis
fix(stress): switch to event-driven wait for member convergence
docs(helm): add Chart README with preset examples
perf(remote): reduce envelope allocation via pool on hot path
refactor(syncx): split delta encoder into dedicated file
```

### PR 标题 = 顶部 commit

PR 会 squash 成一条 commit，PR 标题将成为最终 commit subject，请遵循上述格式。

## 模块负责人（Module Owners）

模块负责人对该模块的 PR 有最终审批权。无模块负责人时默认由 core-team 审批。

| 模块 | Owner |
|------|-------|
| Actor 核心 | @core-team |
| Remote/Network | @infra-team |
| Cluster/Canary | @infra-team |
| 游戏业务层 | @game-team |
| Dashboard/运维 | @infra-team |
| 部署（Helm/Operator） | @devops-team |
| 测试基建 | @qa-team |
| 文档/社区 | @docs-team |

## 发布流程（仅供 maintainer）

1. `main` 分支打 `vX.Y.Z` tag
2. `codegen/stability.go` 维护 API 稳定性分级（Stable / Beta / Experimental）
3. CHANGELOG 由 `codegen/changelog.go` 自动生成并附加到 release

## 行为规范

本项目采用 [Contributor Covenant](https://www.contributor-covenant.org/version/2/1/code_of_conduct/) 行为规范。遇到违规请邮件至 maintainers 或在 issue 中 @core-team。

## 许可

提交 PR 即视为你同意以本仓库 LICENSE（见根目录）授权你的贡献。
