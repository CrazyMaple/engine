# tool/CLAUDE.md — C 类 module：开发与运维期工具

> module `tool`（Go 1.24+）。只在**开发期**与**运维期**使用的可执行程序、DSL 与示例。

## 定位与边界

- **做**：代码生成、CLI、运维控制台、Web 面板、TestKit、基准、压测、部署、样例。
- **不做**：游戏运行期功能（这些在 gamelib/）、Actor 骨架（这些在 engine/）。
- **依赖方向**：可依赖 `engine/*` 与 `gamelib/*`；**不得**被 engine/gamelib 反向 import；**不得** import `better/*`。
- **生产代码不得 `import tool/testkit`**：由 build tag `//go:build testkit` 或只在 `_test.go` 中使用保障。

## 目录清单（v1.12 后共 9 项 + migrate/）

| 目录 | 职责 |
|---|---|
| `codegen/` | `//msggen:message` 注解的消息注册生成、TypeScript 类型生成、msgversion 模板 |
| `cmd/` | 可执行入口，主力是 `cmd/engine` — engine CLI（init / gen / run / dashboard / bench / plugin / cluster / migrate / audit / doctor / doctor deps） |
| `console/` | 运维命令行 |
| `dashboard/` | Web 控制台（REST API + HotActor 热更） |
| `testkit/` | 集群 / Remote / Scenario 测试 DSL，仅供测试代码使用 |
| `bench/` | 基准回归（baseline / compare / report） |
| `stress/` | 压测 Bot + 事件驱动诊断 |
| `deploy/` | K8s / Helm / Operator 产出与代码 |
| `example/` | 全部 `*_example.go` 与 demo 客户端（actor/remote/cluster/pubsub/combat/unity/…） |
| `migrate/` | 一次性迁移脚本（v1.12 的 `split_v1.12.sh` 已落位于此） |

## 关键 CLI：`engine`（位于 `tool/cmd/engine`）

```
engine init        初始化项目脚手架
engine gen         消息注册 + SDK + Proto 统一代码生成
engine run         带热重载的开发模式
engine dashboard   独立启动 Dashboard 面板
engine bench       运行全部基准并生成报告
engine plugin      插件管理
engine cluster     集群状态查看
engine migrate     Actor 迁移管理
engine audit       API 稳定性扫描 / CHANGELOG
engine doctor      环境自检（Go 版本 / 配置 / 端口 / 服务 / 磁盘）
engine doctor deps 依赖方向体检（engine→/gamelib→/better/ 三条铁律，CI 守护）
```

### `doctor deps` 用法

```bash
# 在容器根执行（= engine/gamelib/tool 的上级目录）
cd code/engine
go run ./tool/cmd/engine doctor deps --root .             # 人类可读
go run ./tool/cmd/engine doctor deps --root . --format json

# 集成到 CI（任意违规即非零退出，PR 拦截）
go run ./tool/cmd/engine doctor deps --root . || exit 1
```

扫描原则：
- 对 `engine/ gamelib/ tool/` 下所有 `*.go` 用 `go/parser`（ImportsOnly 模式）抽取 import；
- 按 module 应用禁用前缀表（engine 禁 `gamelib/`+`tool/`，gamelib 禁 `tool/`，任何 module 禁 `better/`）；
- 违规即非零退出，适合作为 CI 门禁。

## 构建与测试

```bash
cd code/engine/tool
go build ./...
go test ./...
```

## 外部依赖

`go.mod` direct require：
- `engine`, `gamelib`（replace 到本地路径）;
- `github.com/gorilla/websocket` — dashboard / stress 直接使用。

例行依赖由 `go mod tidy` 维护；示例在 `example/` 下允许额外 require。

## 相关文档

- `doc/` —— 工具层设计与使用文档（Dashboard / TestKit / 压测方法）。
- 仓库根 `engine/CLAUDE.md`、`gamelib/CLAUDE.md` —— 被依赖层约束。
