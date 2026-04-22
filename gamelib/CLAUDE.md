# gamelib/CLAUDE.md — B 类 module：游戏侧引擎功能

> module `gamelib`（Go 1.24+）。承载围绕「场景 / 战斗 / 背包 / 任务 / 同步 / 持久化 / 网关 / 热更 / 中间件 / 遥测」等**通用玩法运行时**。业务项目通过 `require gamelib` 直接接入。

## 定位与边界

- **只做**：游戏侧可复用的**功能层**运行时与抽象。
- **不做**：Actor 调度、集群基座、跨节点传输（这些在 engine/）；不做开发期工具（这些在 tool/）。
- **依赖方向**：
  - 允许 `gamelib → engine`（正向）；
  - **禁止** `gamelib → tool`（反向）；
  - **禁止** import `better/*`。
- **注意**：`middleware/` 内的「Actor Interceptor 注入点接口」位于 `engine/actor/`，本层只放**具体实现**（OTel/RBAC/metrics/ratelimit 等）。

## 目录清单（v1.12 修正后，共 19 项）

| 目录 | 职责 |
|---|---|
| `config/` | RecordFile（Tab 分隔）/ JSON / Excel / YAML 配置加载，热重载，`engine.yaml` 全量 |
| `timer/` | AfterFunc / CronFunc / 分布式定时 |
| `gate/` | 客户端网关（TCP/WS/KCP）+ 安全链（token / 防重放 / 签名 / 异常检测），反向 import `engine/network` |
| `scene/` | 场景管理 + Grid AOI + A\* 寻路 + 跨场景迁移 |
| `ecs/` | Entity / Component / System，与战斗 / 技能集成 |
| `bt/` | 行为树（黑板 + LOD） |
| `syncx/` | 帧同步 / 状态同步 / Delta / 预测回滚（依赖本层 `fixedpoint/`） |
| `fixedpoint/` | 定点数（确定性数学） |
| `room/` | 房间 + 匹配（ELO） |
| `skill/` | 技能 / Buff / 冷却 |
| `inventory/` | 背包 / 道具 / 交易 |
| `quest/` | 任务 / 成就 / 分支 / 跨模块 |
| `mail/` | 邮件（Memory / Mongo） |
| `leaderboard/` | 跳表排行榜 + 赛季 |
| `replay/` | 战斗回放（录播 + 归档） |
| `saga/` | Saga 分布式事务 + 跨节点 |
| `persistence/` | EventSourcing / Storage 接口（MemoryStorage + MongoStorage） |
| `hotreload/` | Go Plugin + Lua 热更 |
| `middleware/` | Actor 消息管道装饰器：OTel / tracing / metrics / ACL / RBAC / signing / ratelimit / logging / profiler |

> **2026-04-21 勘误**：原计划 §2.2 的 `log/` 与 `telemetry/` 因被 engine 骨架大量直接引用，已修正归属到 engine 白名单。gamelib 若需扩展日志能力（聚合 / 脱敏 / 广播 sink），可新增 `gamelib/logger/`，作为 `engine/log` 之上的增强层。

## 构建与测试

```bash
cd code/engine/gamelib
go build ./...
go test ./...
```

## 外部依赖

`go.mod` direct require：
- `engine v0.0.0` （`replace engine => ../engine`）— 骨架层；
- `github.com/xuri/excelize/v2` — `config/excel_loader.go` 加载策划 Excel；
- `gopkg.in/mgo.v2` — `persistence/` 的 MongoStorage 实现；
- `gopkg.in/yaml.v3` — `config/manager.go` 的 YAML 解析。

新增外部依赖需评审是否属于"所有游戏都会用到"的通用能力；否则放项目层（demo_game / server）即可。

## 相关文档

- `doc/` —— 游戏层设计文档（场景/战斗/同步协议等）。
- 仓库根 `engine/CLAUDE.md` —— 骨架层白名单与依赖方向。
