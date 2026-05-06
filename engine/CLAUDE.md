# engine/CLAUDE.md — A 类 module：Actor + 分布式骨架

> module `engine`（Go 1.24+，最小外部依赖）。对标 Proto.Actor + Leaf 的原生骨架规模；**不含任何游戏业务**。

## 定位与边界

- **只做**：Actor 生命周期（创建·激活·寻址·消息传递·监管·停止·死信）、位置透明通信、虚拟 Actor 激活、最小集群成员感知、跨节点消息契约。
- **不做**：消息分发模式（Router / PubSub）、集群运维（RollingUpgrade / Canary / MultiDC / Federation / SplitBrain / Migration）、传输优化与安全实现（Encryption / Signer / Fragment / ZeroCopy）、日志聚合管道（Aggregator / BroadcastSink / RingBufferSink）、可观测性增强（Profile / SchedulingMetrics 上报层）、服务发现具体实现（Consul / Etcd / K8s / Gossip）、KCP 等具体网关传输；游戏玩法（场景/战斗/背包/任务/邮件/排行榜/回放/…）。
- **依赖方向**：engine **不得** import `gamelib/*` 或 `tool/*`。

## 白名单目录（v1.13 收缩后，共 11 项）

| 目录 | 保留内容 |
|---|---|
| `actor/` | PID、Context、Props、Mailbox 接口（默认 + backpressure 两种内置实现）、Dispatcher（单一默认）、Supervisor、Future、DeadLetter、Shutdown、EventStream、Behavior、Stash、LifecycleHooks、ActorPool、testkit、Mailbox 扩展契约（`SchedulerAware` / `OwnerAware` / `EventStreamAware` / `BatchAwareMailbox`） |
| `remote/` | RemoteProcess、Endpoint、TypeRegistry、Request/Response、ConnPool、HealthCheck、RetryQueue、TraceContext 传播、`MessageCipher` / `MessageSigner` 接口（实现外迁 gamelib/remote/security/） |
| `cluster/` | Member、MemberList、`Provider` 接口、`StaticProvider`、ConsistentHash、Singleton、`ClusterTopologyEvent`、cluster.go 协调入口、`testkit/FakeProvider` |
| `grain/` | Kind、IdentityLookup、PlacementActor、自动激活 |
| `proto/` | actor / remote / cluster / system 四份 .proto 与注册表 — 跨节点消息契约 |
| `codec/` | JSON / Binary / Protobuf / Processor — `remote` 的直接依赖 |
| `network/` | TCP server/client/conn、WS server/conn、TLS、MsgParser、Conn/Agent 抽象（KCP 已外迁 gamelib） |
| `telemetry/` | TraceContext / W3C traceparent — 跨 Actor 跨节点的追踪"元"契约 |
| `log/` | 薄日志层（`Debug/Info/Warn/Error` + Logger / TextHandler / JSONHandler / Redact），engine 内部依赖；不含聚合 / Sink / 多通道分发（已外迁 tool/logstore） |
| `errors/` | 引擎级错误分类（ConnectError / TimeoutError / ClusterError / CodecError） |
| `internal/` | MPSC 无锁队列 — mailbox 底层 |

> **2026-04-21 勘误**：v1.12 §2.2 原将 `log` 和 `telemetry` 划入 gamelib，但 engine 骨架内部已大量依赖这两个包。为遵守「engine 不得 import gamelib」铁律，二者保留在本白名单。
> **2026-05-06 v1.13 收口**：白名单从 13 项收缩到 11 项；`router/`、`pubsub/` 已迁 gamelib，`cluster/canary/` / `federation/` / `rolling_upgrade*` / `multi_dc*` / `splitbrain*` / `migration*` 已删除，KCP 与日志聚合管道、加解密 / 签名实现、actor 高级 mailbox 与剖析 / 工作窃取等下迁或删除。

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
- **Props 构建模式**：Actor 配置蓝图（dispatcher、mailbox、supervisor strategy）；高级 mailbox（Adaptive / Batch / Priority / RingBuffer）通过 `Props.WithMailbox(gamelib_mailbox.NewXxx(...))` 由 gamelib 注入。
- **Envelope 对象池**：消息封装复用，减少 GC。
- **Middleware 注入点**：`actor/` 提供 Interceptor 接口；具体实现（OTel / RBAC / metrics / ratelimit）在 gamelib 层。
- **集群骨架**：`cluster.Provider` 是唯一的成员发现接口；StaticProvider 兜底，Gossip / Consul / Etcd / K8s 等具体实现由 gamelib 注入；engine 不再内嵌 gossip / splitbrain / federation / canary / rolling upgrade / multi DC / migration 任一实现。
- **Remote 安全**：`MessageCipher` / `MessageSigner` 仅留接口；具体实现（AES-GCM / CipherRing / X25519 派生 / HMAC）由 gamelib/remote/security 注入；engine 核心路径不保留 `hmacSize` 等算法固定常量。

## 构建与测试

```bash
cd code/engine/engine
go build ./...
go test ./...
go test ./... -race                       # Phase 4 收口要求
go test ./actor/... -bench=. -benchmem    # 本目录下的基准
```

## 外部依赖

`go.mod` direct require：
- `github.com/gorilla/websocket` — `network/ws_*` 与 `remote` 健康检查使用；
- `google.golang.org/protobuf` — `proto/` 包下四份 .proto 的运行时。

新增外部依赖需评审：对引擎骨架而言，零依赖 > 最少依赖 > 丰富依赖。

## 相关文档

- `doc/v2.1_执行手册.md` —— v1.13 / v2.1 收缩唯一执行依据。
- `doc/adr/v1.13_001_cluster_provider.md` —— Provider 最小接口拍板。
- `doc/adr/v1.13_002_remote_security_interface.md` —— Cipher / Signer 接口拍板。
- `doc/adr/v1.13_003_engine_boundary.md` —— engine 边界与 mailbox 扩展契约拍板。
- `doc/v1.13_完成度审核.md` —— v1.13 收口完成度审核（含 LOC / 符号 / 依赖对比）。
