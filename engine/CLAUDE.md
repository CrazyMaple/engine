# engine/CLAUDE.md — A 类 module：Actor + 分布式骨架

> module `engine`（Go 1.24+，最小外部依赖）。对标 Proto.Actor + Leaf 的原生骨架规模；**不含任何游戏业务**。

## 定位与边界

- **只做**：Actor 调度、位置透明的消息传递、集群基座、跨节点通信契约。
- **不做**：游戏玩法（场景/战斗/背包/任务/邮件/排行榜/回放/…）、日志聚合 UI、运维工具、配置热重载、持久化存储后端。
- **依赖方向**：engine **不得** import `gamelib/*` 或 `tool/*`。

## 白名单目录（v1.12 修正后，共 13 项）

| 目录 | 保留原因 |
|---|---|
| `actor/` | Actor Context / PID / Mailbox / Dispatcher / Supervisor / Behavior / Pool / Priority / Stash — 引擎核心 |
| `remote/` | Endpoint / RemoteProcess / 连接池 / 健康检查 / TraceContext 传播 — 位置透明所需 |
| `cluster/` | Gossip / 一致性哈希 / Split-Brain / Singleton / Federation / Migration / Provider — 分布式基础 |
| `grain/` | (Kind, Identity) 虚拟 Actor — Actor 基座必备 |
| `router/` | Broadcast / RoundRobin / ConsistentHash — Actor 消息路由 |
| `pubsub/` | Topic 发布订阅 — 跨 Actor 事件分发 |
| `proto/` | actor / remote / cluster / system 四份 .proto 与注册表 — 跨节点消息契约 |
| `codec/` | JSON / Binary / Stream — `remote` 的直接依赖，不可拆出 |
| `network/` | TCP / WS / KCP 与 MsgParser — `remote` 的直接依赖（gamelib.gate 反向复用） |
| `errors/` | 引擎级错误分类（ConnectError / TimeoutError / ClusterError / CodecError） |
| `internal/` | MPSC 无锁队列 — mailbox 底层 |
| `log/` | 薄日志层（`Debug/Info/Warn/Error`），engine 内部 69 处引用；注入点接口，不含游戏聚合/脱敏逻辑 |
| `telemetry/` | TraceContext / W3C traceparent — 跨 Actor 跨节点的追踪"元"契约，被 actor/remote 直接使用 |

> **2026-04-21 勘误**：计划 §2.2 原将 `log` 和 `telemetry` 划入 gamelib，但 engine 骨架内部已大量依赖这两个包。为遵守 §2.4「engine 不得 import gamelib」铁律，二者修正归属到本白名单。

## 消息流转路径

```
ctx.Send(pid, msg)
  → Envelope(含 sender + TraceContext)
    → ProcessRegistry(本地) 或 RemoteProcess(远程)
      → 目标 Actor Mailbox
        → Dispatcher 调度
          → 系统消息优先处理 → 用户消息 → Behavior 函数处理
```

## 关键设计

- **Actor-First**：一切皆 Actor，所有交互通过消息传递。
- **位置透明**：`ctx.Send(pid, msg)` 对本地/远程 Actor 使用相同 API；由 PID 寻址（本地 `id` / 远程 `address:port/id`）与 ProcessRegistry 分发。
- **监管树**：父 Actor 监管子 Actor 故障，Directive 策略（Resume / Restart / Stop / Escalate）。
- **Props 构建模式**：Actor 配置蓝图（dispatcher、mailbox、supervisor strategy）。
- **Envelope 对象池**：消息封装复用，减少 GC。
- **Middleware 注入点**：`actor/` 提供 Interceptor 接口；具体实现（OTel / RBAC / metrics / ratelimit）在 gamelib 层。

## 构建与测试

```bash
cd code/engine/engine
go build ./...
go test ./...
go test ./actor/... -bench=. -benchmem   # 本目录下的基准
```

## 外部依赖

`go.mod` 只有两条 direct require：
- `github.com/gorilla/websocket` — `network/ws_*` 与 `remote` 健康检查使用；
- `google.golang.org/protobuf` — `proto/` 包下四份 .proto 的运行时。

新增外部依赖需评审：对引擎骨架而言，零依赖 > 最少依赖 > 丰富依赖。

## 相关文档

- `doc/` —— 骨架相关设计文档（消息契约、TraceContext 规约等）。
- 仓库根 `doc/v1.12_三层拆分重构计划.md` —— 本次拆分依据。
