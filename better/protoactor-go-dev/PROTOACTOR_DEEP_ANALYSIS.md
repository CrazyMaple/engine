# ProtoActor-Go 深度架构分析

> 本文档对 `better/protoactor-go-dev` 进行深度分析，涵盖模块划分、函数调用链、优劣势评估，
> 方便后续按模块替换或迭代到 MapleWish 框架中。

---

## 目录

- [一、整体架构总览](#一整体架构总览)
- [二、模块依赖关系图](#二模块依赖关系图)
- [三、核心模块详解](#三核心模块详解)
  - [M1. Actor 核心模块](#m1-actor-核心模块)
  - [M2. Remote 远程通信模块](#m2-remote-远程通信模块)
  - [M3. Cluster 集群模块](#m3-cluster-集群模块)
  - [M4. Router 路由模块](#m4-router-路由模块)
  - [M5. EventStream 事件流模块](#m5-eventstream-事件流模块)
  - [M6. Scheduler 调度器模块](#m6-scheduler-调度器模块)
  - [M7. Persistence 持久化模块](#m7-persistence-持久化模块)
  - [M8. Internal 内部工具模块](#m8-internal-内部工具模块)
  - [M9. 辅助模块（Stream/Plugin/TestKit/Metrics/Extensions）](#m9-辅助模块)
- [四、关键调用链](#四关键调用链)
- [五、各模块优劣势分析](#五各模块优劣势分析)
- [六、模块化替换建议](#六模块化替换建议maplewish-集成路线)

---

## 一、整体架构总览

ProtoActor-Go 是一个 **分布式 Actor 框架**，429 个 Go 文件，分为以下核心层次：

```
┌─────────────────────────────────────────────────────────────┐
│                    Cluster 层（集群管理）                      │
│  Grain(Virtual Actor) · Gossip · PubSub · ClusterProvider   │
├─────────────────────────────────────────────────────────────┤
│                    Remote 层（跨节点通信）                     │
│  gRPC双向流 · EndpointManager · Serializer · Activator      │
├─────────────────────────────────────────────────────────────┤
│                    Actor 层（核心执行引擎）                    │
│  ActorSystem · Context · PID · Process · Mailbox · Future   │
│  Props · Supervision · Behavior · Middleware · Dispatcher    │
├─────────────────────────────────────────────────────────────┤
│                    基础设施层                                 │
│  EventStream · Router · Scheduler · Persistence             │
│  Internal(MPSC/Ring Queue) · Metrics · Extensions           │
└─────────────────────────────────────────────────────────────┘
```

**文件统计**：

| 模块 | 目录 | Go文件数 | 核心职责 |
|------|------|----------|----------|
| Actor | `actor/` | ~111 | Actor 生命周期、消息处理、监督 |
| Cluster | `cluster/` | ~109 | 集群管理、Grain、Gossip、PubSub |
| Remote | `remote/` | ~31 | gRPC 远程通信、序列化 |
| Router | `router/` | ~19 | 消息路由策略 |
| EventStream | `eventstream/` | ~3 | 发布-订阅事件总线 |
| Scheduler | `scheduler/` | ~2 | 定时任务调度 |
| Persistence | `persistence/` | ~10 | 事件溯源、快照 |
| Internal | `internal/` | ~5 | 无锁队列、调试工具 |
| 其他辅助 | `stream/plugin/testkit/metrics/...` | ~20 | 流、插件、测试、指标 |

---

## 二、模块依赖关系图

```
                         ┌──────────┐
                         │ Cluster  │
                         └────┬─────┘
                              │ 依赖
                    ┌─────────┴──────────┐
                    ▼                    ▼
              ┌──────────┐        ┌───────────┐
              │  Remote  │        │  Router   │
              └────┬─────┘        └─────┬─────┘
                   │                    │
                   └────────┬───────────┘
                            ▼
                     ┌─────────────┐
                     │ Actor (核心) │
                     └──────┬──────┘
                            │
            ┌───────────────┼───────────────┐
            ▼               ▼               ▼
     ┌─────────────┐ ┌───────────┐  ┌────────────┐
     │ EventStream │ │  Internal │  │ Extensions │
     └─────────────┘ │ (Queues)  │  │  / ctxext  │
                     └───────────┘  └────────────┘

  独立模块（仅依赖 Actor 接口）：
  ┌────────────┐  ┌───────────┐  ┌──────────┐  ┌─────────┐
  │ Persistence│  │ Scheduler │  │  Stream  │  │ Plugin  │
  └────────────┘  └───────────┘  └──────────┘  └─────────┘
```

---

## 三、核心模块详解

### M1. Actor 核心模块

**目录**: `actor/`

#### 1.1 核心类型层次

```
ActorSystem
├── ProcessRegistry (PID → Process 的注册表，1024分片Map)
├── RootContext     (系统级操作入口，无邮箱)
├── Guardians       (Guardian策略缓存)
├── EventStream    (全局事件总线)
├── DeadLetter     (死信处理)
└── Extensions     (扩展注册)

Props (Actor配置蓝图)
├── producer        → 创建 Actor 实例
├── mailboxProducer → 创建 Mailbox
├── dispatcher      → 调度策略
├── supervisionStrategy → 监督策略
├── receiverMiddleware  → 接收中间件链
├── senderMiddleware    → 发送中间件链
└── spawnMiddleware     → 生成中间件链

actorContext (Actor运行时上下文)
├── actor       → 业务 Actor 实例
├── self/parent → PID 引用
├── extras      → children/watchers/stash/timer
├── state       → alive/restarting/stopping/stopped
└── messageOrEnvelope → 当前消息
```

#### 1.2 接口体系（组合接口模式）

```go
// 最小接口 - 用户只需实现这一个
type Actor interface {
    Receive(c Context)
}

// Context 接口 = 多个功能接口组合
Context = infoPart + basePart + messagePart + senderPart
        + receiverPart + spawnerPart + stopperPart + extensionPart

// 衍生接口用于中间件
SenderContext   = infoPart + senderPart + messagePart
ReceiverContext = infoPart + receiverPart + messagePart + extensionPart
SpawnerContext  = infoPart + spawnerPart
```

#### 1.3 Actor 生命周期状态机

```
                    ┌──────────┐
         Spawn ───▶│  Alive   │◀── Restart (incarnateActor + Started)
                    └────┬─────┘
                         │
              ┌──────────┴──────────┐
              ▼                     ▼
       ┌─────────────┐     ┌──────────────┐
       │  Stopping   │     │  Restarting  │
       │ (Stopping)  │     │ (Restarting) │
       └──────┬──────┘     └──────┬───────┘
              │                    │
              │  stopAllChildren   │ stopAllChildren
              │                    │
              ▼                    ▼
       ┌─────────────┐     ┌──────────────┐
       │  finalizeStop│     │   restart()  │
       │  (Stopped)  │     │  (Started)   │
       └──────┬──────┘     └──────────────┘
              │
              ▼
       ┌─────────────┐
       │   Stopped   │ → 通知 watchers + parent
       └─────────────┘
```

**关键生命周期消息**：
- `Started` → Actor 初始化（用户可在此注册资源）
- `Stopping` → 即将停止（用户释放资源）
- `Stopped` → 已停止
- `Restarting` → 即将重启
- `ReceiveTimeout` → 超时未收到消息
- `Terminated` → 被监视的 Actor 已停止

#### 1.4 消息处理调用链

```
ctx.Send(pid, msg)
  │
  ├─[有 senderMiddleware]─▶ middleware chain ──▶ pid.sendUserMessage()
  └─[无 middleware]────────────────────────────▶ pid.sendUserMessage()
      │
      ▼
  pid.ref(system) → Process 查找（atomic缓存 + ProcessRegistry）
      │
      ▼
  Process.SendUserMessage(pid, msg)
      │
      ├─ ActorProcess  → mailbox.PostUserMessage(msg)
      ├─ futureProcess → 解析响应，完成 Future
      └─ deadLetterProcess → 发布 DeadLetterEvent
      │
      ▼ (ActorProcess 路径)
  mailbox.schedule()
      │
      ▼
  dispatcher.Schedule(mailbox.processMessages)
      │  ┌────── goroutineDispatcher: go fn()
      │  └────── synchronizedDispatcher: fn()
      ▼
  mailbox.run()
      │
      ├─ 优先: systemMailbox.Pop() → InvokeSystemMessage()
      │    ├─ *Started    → 转为 InvokeUserMessage
      │    ├─ *Stop       → handleStop()
      │    ├─ *Restart    → handleRestart()
      │    ├─ *Watch      → 记录 watcher
      │    ├─ *Terminated → 移除 child + 通知用户
      │    ├─ *Failure    → SupervisorStrategy.HandleFailure()
      │    └─ *continuation → ReenterAfter 回调
      │
      └─ 次要: userMailbox.Pop() → InvokeUserMessage()
           │
           ├─[有 receiverMiddleware]─▶ middleware chain ──▶ Receive()
           └─[无 middleware]──────────────────────────────▶ defaultReceive()
                │
                ├─ PoisonPill  → ctx.Stop(self)
                ├─ AutoRespond → actor.Receive(ctx) + Respond()
                └─ default     → actor.Receive(ctx) ← 用户代码
```

#### 1.5 Mailbox 系统

| 类型 | 文件 | 底层实现 | 特点 |
|------|------|---------|------|
| Unbounded | `unbounded.go` | `goring.Queue` | 无限容量，自动扩容 |
| UnboundedLockFree | `unbounded_lock_free.go` | `mpsc.Queue` | 无锁 MPSC |
| Bounded | `bounded.go` | `RingBuffer` | 固定容量，满时阻塞 |
| BoundedDropping | `bounded.go` | `RingBuffer` | 固定容量，满时丢弃旧消息 |
| Priority | `unbounded_priority.go` | `PriorityQueue` | 优先级排序 |

**Mailbox 调度核心逻辑**：
```
系统消息优先 → SuspendMailbox/ResumeMailbox 控制暂停
→ 每处理 Throughput(默认300) 条消息后让出 CPU
→ 无消息时退出循环，等待下次 schedule()
```

#### 1.6 Future 系统

```
NewFuture(system, timeout)
  │
  ├─ 创建 futureProcess，注册到 ProcessRegistry
  ├─ 启动超时定时器
  │
  ▼ 用于 RequestFuture
ctx.RequestFuture(pid, msg, timeout)
  │
  ├─ envelope.Sender = future.PID()  ← 关键：响应会发回 Future
  └─ sendUserMessage(pid, envelope)
      │
      ▼ 目标 Actor 处理后
  ctx.Respond(result) → Send(Sender=future.PID(), result)
      │
      ▼
  futureProcess.SendUserMessage() → 解析结果
      │
      ├─ future.result = result
      ├─ future.done = true
      ├─ cond.Broadcast() → 唤醒等待者
      ├─ PipeTo → 转发到指定 PID
      └─ completions → 执行所有回调
```

#### 1.7 监督策略

| 策略 | 文件 | 行为 | 适用场景 |
|------|------|------|---------|
| OneForOne | `strategy_one_for_one.go` | 只重启失败的子 Actor | 子 Actor 独立 |
| AllForOne | `strategy_all_for_one.go` | 所有子 Actor 一起重启 | 子 Actor 有强依赖 |
| ExponentialBackoff | `strategy_exponential_backoff.go` | 指数退避延迟重启 | 外部资源依赖 |
| Restarting | `strategy_restarting.go` | 无条件立即重启 | 简单重试 |

**监督流程**：
```
Actor panic → mailbox defer 捕获
  → EscalateFailure(reason, msg)
    → SuspendMailbox (挂起邮箱)
    → Failure 消息发给 parent
      → parent.handleFailure()
        → SupervisorStrategy.HandleFailure()
          → 四种决策:
            Resume    → ResumeMailbox (继续处理)
            Restart   → Restart 系统消息 → incarnateActor()
            Stop      → Stop 系统消息 → finalizeStop()
            Escalate  → 继续上报给更高层
```

#### 1.8 Middleware 系统

```go
// 四种中间件类型
ReceiverMiddleware  func(next ReceiverFunc) ReceiverFunc   // 接收链
SenderMiddleware    func(next SenderFunc) SenderFunc       // 发送链
SpawnMiddleware     func(next SpawnFunc) SpawnFunc         // 生成链
ContextDecorator    func(next ContextDecoratorFunc) ContextDecoratorFunc // Context装饰

// 编译为链：middleware[0] → middleware[1] → ... → middleware[n] → 最终处理
```

**已有中间件实现**：
- `middleware/logging.go` - 日志记录
- `middleware/opentelemetry/` - OpenTelemetry 追踪
- `middleware/opentracing/` - OpenTracing 追踪
- `middleware/propagator/` - 中间件传播
- `middleware/protozip/` - 消息压缩

---

### M2. Remote 远程通信模块

**目录**: `remote/`

#### 2.1 架构设计

```
节点A                                            节点B
┌─────────────────┐     gRPC双向流      ┌─────────────────┐
│  EndpointWriter │ ◀══════════════▶ │ EndpointReader  │
│  (批处理发送)    │                  │ (接收+投递)      │
├─────────────────┤                  ├─────────────────┤
│ EndpointManager │                  │ EndpointManager │
│  (连接管理)      │                  │  (连接管理)      │
├─────────────────┤                  ├─────────────────┤
│ EndpointWatcher │                  │ EndpointWatcher │
│  (生命周期监听)  │                  │  (生命周期监听)  │
├─────────────────┤                  ├─────────────────┤
│   Activator     │                  │   Activator     │
│  (远程Spawn)    │                  │  (远程Spawn)    │
└─────────────────┘                  └─────────────────┘
```

#### 2.2 核心类型

```go
type Remote struct {
    actorSystem    *actor.ActorSystem
    s              *grpc.Server         // gRPC 服务器
    edpReader      *endpointReader      // 接收端
    edpManager     *endpointManager     // 连接管理
    config         *Config
    kinds          map[string]*actor.Props  // 可远程 Spawn 的类型
    blocklist      *BlockList           // 黑名单
}

type Config struct {
    Host, Port              string, int
    AdvertisedHost          string        // NAT/LB 地址
    EndpointWriterBatchSize int           // 默认 1000
    EndpointWriterQueueSize int           // 默认 1000000
    MaxRetryCount           int           // 默认 5
    ServerOptions           []grpc.ServerOption
    DialOptions             []grpc.DialOption
    Kinds                   map[string]*actor.Props
}
```

#### 2.3 消息发送完整链路

```
Root.Send(remotePID, msg)
  │
  ▼
ProcessRegistry.Get(remotePID)
  → 地址不是本地 → remoteHandler(pid)
    → newProcess(pid, remote) ← 创建远程 Process 代理
  │
  ▼
remote.process.SendUserMessage(pid, msg)
  → UnwrapEnvelope(msg)
  → remote.SendMessage(pid, header, msg, sender, serializerID)
    │
    ▼
  edpManager.remoteDeliver(remoteDeliver{...})
    → ensureConnected(address) → endpointLazy.Get()
      → sync.Once → 创建 EndpointWriter + EndpointWatcher
    → Root.Send(endpoint.writer, remoteDeliver)
      │
      ▼
  EndpointWriter 邮箱（特殊批处理邮箱）
    → PopMany(batchSize) → 批量出队
    → sendEnvelopes(messages)
      ├─ 逐条序列化: Serialize(msg, serializerID)
      ├─ 构建去重查找表: typeNames[], targets[], senders[]
      └─ 构建 MessageBatch (protobuf)
        │
        ▼
  gRPC stream.Send(RemoteMessage{MessageBatch})
        │
        ▼
  ═══════════ 网络传输 ═══════════
        │
        ▼
  EndpointReader.Receive(stream)
    → stream.Recv()
    → onMessageBatch(batch)
      ├─ 反序列化: Deserialize(data, typeName, serializerID)
      ├─ 重建 PID: target=本地地址, sender=远程地址
      └─ Root.Send(targetPID, message) → 投递到本地 Actor
```

#### 2.4 序列化体系

```go
type Serializer interface {
    Serialize(msg interface{}) ([]byte, error)
    Deserialize(typeName string, bytes []byte) (interface{}, error)
    GetTypeName(msg interface{}) (string, error)
}

// 内置序列化器（按ID索引）
// 0: ProtoSerializer  → Protocol Buffers
// 1: JSONSerializer   → JSON
```

#### 2.5 消息协议（protobuf）

```protobuf
message RemoteMessage {
  oneof message_type {
    MessageBatch     message_batch = 1;     // 消息批次
    ConnectRequest   connect_request = 2;   // 连接请求
    ConnectResponse  connect_response = 3;  // 连接响应
    DisconnectRequest disconnect_request = 4; // 断开连接
  }
}

message MessageBatch {
  repeated string type_names = 1;          // 类型名去重数组
  repeated string targets = 2;            // 目标ID去重数组
  repeated MessageEnvelope envelopes = 3; // 消息信封
  repeated actor.PID senders = 4;         // 发送者去重数组
}
```

---

### M3. Cluster 集群模块

**目录**: `cluster/`

#### 3.1 核心架构

```
┌───────────────────────────────────────────────────┐
│                    Cluster                         │
│                                                   │
│  ┌───────────┐ ┌──────────┐ ┌──────────────────┐ │
│  │  Gossiper │ │  PubSub  │ │  IdentityLookup  │ │
│  │ (状态同步) │ │(发布订阅) │ │  (Grain 定位)    │ │
│  └─────┬─────┘ └────┬─────┘ └────────┬─────────┘ │
│        │            │                │            │
│  ┌─────┴─────┐      │       ┌────────┴─────────┐ │
│  │ Informer  │      │       │  disthash        │ │
│  │(Gossip实现)│      │       │  ├ Manager       │ │
│  └───────────┘      │       │  ├ PlacementActor│ │
│                     │       │  └ Rendezvous    │ │
│  ┌──────────────────┴───┐   └──────────────────┘ │
│  │      MemberList       │                        │
│  │   (成员管理+拓扑变化)  │                        │
│  └───────────┬───────────┘                        │
│              │                                    │
│  ┌───────────┴───────────┐                        │
│  │   ClusterProvider     │                        │
│  │  (服务发现抽象)        │                        │
│  └───────────────────────┘                        │
└───────────────────────────────────────────────────┘
```

#### 3.2 Grain（Virtual Actor）调用链

```
cluster.Request("player123", "Player", &GetBalance{})
  │
  ▼
DefaultContext.Request()
  │
  ├─ getPid("player123", "Player")
  │   ├─ PidCache 缓存命中 → 直接使用
  │   └─ 缓存未命中 → IdentityLookup.Get(ClusterIdentity)
  │       │
  │       ▼
  │     disthash.Manager.Get(identity)
  │       │
  │       ├─ Rendezvous.GetByClusterIdentity(identity)
  │       │   → FNV-1a 一致性哈希 → 定位负责节点地址
  │       │
  │       ├─ PidOfActivatorActor(ownerAddress)
  │       │   → 构建远程 PlacementActor 的 PID
  │       │
  │       └─ RequestFuture(placementPID, ActivationRequest{identity})
  │           │
  │           ▼ (远程通信)
  │         PlacementActor.onActivationRequest()
  │           ├─ 已激活 → 返回现有 PID
  │           └─ 未激活 → SpawnPrefix(Props, identity)
  │               → 返回 ActivationResponse{Pid}
  │
  ▼ 获得 PID 后
  RequestFuture(pid, message, timeout)
  │
  ├─ 成功 → 返回 response
  ├─ 超时/死信 → 清除 PidCache → 重试
  └─ 超过 RetryCount → 返回 error
```

#### 3.3 ClusterProvider 实现

```go
type ClusterProvider interface {
    StartMember(cluster *Cluster) error
    StartClient(cluster *Cluster) error
    Shutdown(graceful bool) error
}
```

| 提供者 | 目录 | 后端 | 适用场景 |
|--------|------|------|---------|
| AutoManaged | `clusterproviders/automanaged/` | 内存+HTTP | 开发/测试 |
| Consul | `clusterproviders/consul/` | Consul API | 生产环境 |
| etcd | `clusterproviders/etcd/` | etcd Lease | 生产环境 |
| Kubernetes | `clusterproviders/k8s/` | K8s API | 容器化部署 |
| ZooKeeper | `clusterproviders/zk/` | ZK Session | 传统基础设施 |

#### 3.4 Gossip 协议

```
gossipLoop() 每 300ms:
  ├─ blockExpiredHeartbeats()  → 阻止超时节点
  ├─ blockGracefullyLeft()     → 阻止已离开节点
  ├─ SetState(HeartbeatKey, heartbeat)
  └─ SendState()
      → Informer.SendState()
        → 选择 FanOut(默认3) 个随机成员
        → 发送 GossipRequest{state}
        → 对方 ReceiveState() → 合并状态 → 返回 GossipResponse

关键 Gossip Key:
  - "topology"  → 集群拓扑
  - "heartbeat" → 心跳 + Actor 统计
  - "left"      → 优雅离开标记
```

#### 3.5 PubSub 系统

```
Publisher.Publish(topic, message)
  │
  ▼
cluster.Request(topic, "protoact-topic", PubSubBatch{msg})
  │
  ▼
TopicActor.onPubSubBatch()
  │
  ├─ 收集所有 subscribers
  ├─ 按成员分组 → DeliverBatchRequest
  └─ 发送给各成员的 PubSubMemberDeliveryActor
      │
      ▼
  PubSubMemberDeliveryActor
    → 为每个 subscriber 投递消息
    → 收集失败的 subscriber
    → 通知 TopicActor 移除失效订阅
```

---

### M4. Router 路由模块

**目录**: `router/`

#### 4.1 路由策略

```go
type State interface {
    RouteMessage(message interface{})
    SetRoutees(routees *actor.PIDSet)
    GetRoutees() *actor.PIDSet
    SetSender(sender actor.SenderContext)
}
```

| 策略 | 文件 | 算法 | 适用场景 |
|------|------|------|---------|
| Broadcast | `broadcast_router.go` | 发给所有 routee | 全局通知 |
| Random | `random_router.go` | 随机选择一个 | 简单负载均衡 |
| RoundRobin | `roundrobin_router.go` | 轮询（原子计数器） | 均匀分布 |
| ConsistentHash | `consistent_hash_router.go` | 一致性哈希 | 会话保持/分片 |

#### 4.2 两种路由模式

```
Pool 模式（PoolRouter）：
  Router 自动创建并管理一组子 Actor
  → 适合计算密集型任务

Group 模式（GroupRouter）：
  Router 路由到已存在的 Actor 集合
  → 适合已有 Actor 的负载分发
```

---

### M5. EventStream 事件流模块

**目录**: `eventstream/`

```go
type EventStream struct {
    sync.RWMutex
    subscriptions []*Subscription
    counter       int32
}

// 核心 API
Subscribe(handler Handler) *Subscription
SubscribeWithPredicate(handler Handler, predicate Predicate) *Subscription
Unsubscribe(sub *Subscription)
Publish(evt interface{})
```

**特点**：
- 线程安全（RWMutex）
- 支持谓词过滤
- 原子操作管理订阅状态
- 高效取消订阅（swap策略避免遍历）

---

### M6. Scheduler 调度器模块

**目录**: `scheduler/`

```go
type TimerScheduler struct {
    ctx actor.SenderContext
}

// 核心 API
SendOnce(delay, pid, msg) CancelFunc       // 延迟发送一次
SendRepeatedly(initial, interval, pid, msg) CancelFunc  // 周期发送
RequestOnce(delay, pid, msg) CancelFunc     // 延迟请求一次
RequestRepeatedly(initial, interval, pid, msg) CancelFunc
```

**实现**: 基于 `time.AfterFunc()` + 状态机（Init → Ready → Done）+ 原子操作

---

### M7. Persistence 持久化模块

**目录**: `persistence/`

#### 7.1 接口体系

```go
type ProviderState interface {
    SnapshotStore    // GetSnapshot/PersistSnapshot/DeleteSnapshots
    EventStore       // GetEvents/PersistEvent/DeleteEvents
    Restart()
    GetSnapshotInterval() int
}
```

#### 7.2 恢复流程

```
Actor.Started
  → Using(provider) 中间件拦截
    → mixin.init(provider, ctx)
      → GetSnapshot(actorName)           // 加载最新快照
      → GetEvents(actorName, fromIndex)  // 重放事件
        → actor.Receive(replayEvent)     // 每个事件触发 Receive
      → ReplayComplete 信号
```

#### 7.3 持久化实现

| 实现 | 目录 | 后端 |
|------|------|------|
| InMemory | `persistence/` | Map（开发测试） |
| Couchbase | `persistence/protocb/` | Couchbase |

---

### M8. Internal 内部工具模块

**目录**: `internal/`

#### MPSC 队列 (`internal/queue/mpsc/`)

```go
type Queue struct { head, tail *node }

// 无锁设计：多生产者单消费者
Push(x interface{})  // 使用 atomic.StorePointer + CAS
Pop() interface{}    // 单线程消费
Empty() bool
```

#### Ring 队列 (`internal/queue/goring/`)

```go
type Queue struct {
    len     int64
    content *ringBuffer
    lock    sync.Mutex
}

// 支持批量操作
Push(item interface{})
Pop() (interface{}, bool)
PopMany(count int64) ([]interface{}, bool)  // 批量出队
```

---

### M9. 辅助模块

#### Stream 模块 (`stream/`)
Actor ↔ Go Channel 桥接：
```go
TypedStream[T]  // 泛型版
UntypedStream   // interface{} 版
```

#### Plugin 模块 (`plugin/`)
- `plugin.go` - 插件接口（OnStart/OnOtherMessage）
- `passivation.go` - 空闲超时自动停止 Actor

#### TestKit 模块 (`testkit/`)
- `TestProbe` - 测试探针（发送/接收/断言）
- 泛型辅助：`GetNextMessageOf[T]`, `FishForMessage[T]`

#### Metrics 模块 (`metrics/`)
- OpenTelemetry 集成
- 指标：Spawn/Restart/Stop/Failure/Mailbox/Duration/DeadLetter/Future

#### Extensions 模块 (`extensions/` + `ctxext/`)
```
extensions/ → ActorSystem 级别扩展（Remote, Cluster, Metrics）
ctxext/     → Actor Context 级别扩展（ClusterIdentity 等）
```

---

## 四、关键调用链

### 4.1 本地消息发送

```
ctx.Send(pid, msg)
  → actorContext.sendUserMessage()
    → [senderMiddleware?] → pid.sendUserMessage()
      → pid.ref() → ActorProcess
        → mailbox.PostUserMessage(msg)
          → dispatcher.Schedule(processMessages)
            → systemMailbox 优先 → userMailbox
              → InvokeUserMessage(msg)
                → [receiverMiddleware?] → defaultReceive()
                  → actor.Receive(ctx)
```

### 4.2 远程消息发送

```
ctx.Send(remotePID, msg)
  → ProcessRegistry → remoteHandler → remote.process
    → remote.SendMessage()
      → edpManager.remoteDeliver()
        → endpointLazy.Get() → [Lazy连接]
          → EndpointWriter.sendEnvelopes()
            → Serialize + 批处理
              → gRPC stream.Send(MessageBatch)

[远程节点]
EndpointReader.Receive()
  → stream.Recv()
    → Deserialize
      → Root.Send(localPID, msg) → 本地投递
```

### 4.3 Grain 调用

```
cluster.Request(identity, kind, msg)
  → DefaultContext.Request()
    → getPid() → [PidCache / IdentityLookup]
      → Rendezvous 一致性哈希 → 定位节点
        → PlacementActor.ActivationRequest
          → SpawnPrefix(props, identity) [如需激活]
    → RequestFuture(pid, msg, timeout)
      → [Remote 通信]
        → Actor.Receive(ctx) → ctx.Respond(result)
```

### 4.4 监督恢复

```
actor.Receive() panic
  → mailbox.defer → EscalateFailure()
    → SuspendMailbox
    → Failure → parent
      → handleFailure()
        → Strategy.HandleFailure()
          → [Resume]  ResumeMailbox
          → [Restart] Restart → incarnateActor() → Started
          → [Stop]    Stop → finalizeStop() → Terminated
          → [Escalate] → 上报 parent 的 parent
```

---

## 五、各模块优劣势分析

### M1. Actor 核心模块

| 维度 | 评价 |
|------|------|
| **优势** | |
| 接口设计 | 组合接口模式，高度灵活，中间件支持完善 |
| 生命周期 | 完整的状态机，支持 Stash/ReenterAfter |
| 邮箱系统 | 多种实现，系统消息优先，挂起/恢复机制 |
| 监督策略 | 四种策略可选，支持自定义 Decider |
| 性能 | 无锁 PID 缓存、分片 ProcessRegistry、吞吐量控制 |
| **劣势** | |
| 复杂度 | 代码量大（111文件），概念多，学习曲线陡峭 |
| Context 膨胀 | actorContext 职责过多（消息/生命周期/监督/子管理全部混在一起） |
| Panic 依赖 | 错误传播依赖 panic-recover，不够 Go 风格 |
| 配置分散 | Props 选项太多，容易遗漏关键配置 |

### M2. Remote 远程通信模块

| 维度 | 评价 |
|------|------|
| **优势** | |
| 批处理 | 消息合并+去重查找表，高吞吐 |
| Lazy 连接 | 按需建立，避免资源浪费 |
| 透明代理 | remote.process 实现 Process 接口，对用户透明 |
| 序列化可扩展 | 支持自定义 Serializer |
| **劣势** | |
| gRPC 强绑定 | 传输层和 gRPC 深度耦合，无法替换为其他协议 |
| 优雅关闭不完善 | 源码有 TODO 注释，存在超时 workaround |
| 重连逻辑简单 | MaxRetryCount 后直接失败，无指数退避 |
| 无连接池 | 每个远程地址只有一个 gRPC 连接 |

### M3. Cluster 集群模块

| 维度 | 评价 |
|------|------|
| **优势** | |
| Virtual Actor | Grain 模式，自动激活/定位，开发体验好 |
| 多 Provider | 支持 Consul/etcd/K8s/ZK，灵活部署 |
| Gossip 共识 | 最终一致性 + 拓扑共识检查 |
| PubSub | 内置集群级发布订阅 |
| **劣势** | |
| 复杂度极高 | 109 个文件，概念最多的模块 |
| 代码生成依赖 | Grain 需要 protoc 插件生成代码 |
| 单点隐患 | PlacementActor 如果挂了需要等待重新分配 |
| 配置项过多 | Gossip/Heartbeat/Timeout 参数需要精细调优 |
| 启动延迟 | `time.Sleep(1 * time.Second)` 硬编码等待 |

### M4. Router 路由模块

| 维度 | 评价 |
|------|------|
| **优势** | |
| 策略丰富 | 4种路由策略覆盖常见场景 |
| Pool/Group | 两种模式灵活应对不同需求 |
| **劣势** | |
| 无权重路由 | 缺少加权轮询/加权随机 |
| 动态性不足 | ConsistentHash 的 routee 变化时需要重建哈希环 |

### M5-M9. 辅助模块

| 模块 | 优势 | 劣势 |
|------|------|------|
| EventStream | 简洁高效、线程安全 | 无持久化、无 backpressure |
| Scheduler | 简单易用 | 仅支持固定间隔，无 Cron 表达式 |
| Persistence | 事件溯源 + 快照经典模式 | 仅 InMemory 和 Couchbase，缺少通用 DB |
| Internal | MPSC 无锁队列性能优秀 | 无文档，边界场景未充分测试 |
| Stream | Go Channel 桥接方便 | 功能单一 |
| Plugin | Passivation 实用 | 插件体系不够完善 |
| TestKit | 泛型探针方便测试 | 集群测试工具复杂 |
| Metrics | OpenTelemetry 标准 | 指标定义分散 |

---

## 六、模块化替换建议（MapleWish 集成路线）

### 第一优先级：核心引擎替换

| 目标 | 来源模块 | MapleWish 现状 | 替换策略 |
|------|---------|---------------|---------|
| **Actor 模型** | M1.Actor | `engine/skeleton/actor.go` | 渐进替换：先引入 Props/Context 接口，再迁移消息处理 |
| **监督策略** | M1.Supervision | `engine/skeleton/supervision.go` | 直接采用，替换现有简单实现 |
| **Future** | M1.Future | `engine/skeleton/future.go` | 直接采用，ProtoActor 的 Future 更成熟 |
| **Behavior** | M1.Behavior | `engine/skeleton/behavior.go` | 直接采用，栈式行为切换 |
| **Mailbox** | M1.Mailbox | 三邮箱模型 | 重点参考：改造为系统+用户分离，引入 Unbounded/Bounded |

### 第二优先级：通信层替换

| 目标 | 来源模块 | MapleWish 现状 | 替换策略 |
|------|---------|---------------|---------|
| **节点间通信** | M2.Remote | Kafka(mkafka/) | **不直接采用 gRPC**，但参考其 EndpointManager 架构，保留 Kafka 作为传输层 |
| **序列化** | M2.Serializer | Protobuf | 参考其接口设计，统一序列化抽象 |
| **远程 Spawn** | M2.Activator | 无 | 可选引入，支持跨节点创建 Actor |

### 第三优先级：集群能力

| 目标 | 来源模块 | MapleWish 现状 | 替换策略 |
|------|---------|---------------|---------|
| **Virtual Actor** | M3.Grain | Space 管理 | 参考 Grain 模式改造 Space，但保持简单 |
| **一致性哈希** | M3.Rendezvous | 无 | 可直接采用，用于 Actor 分布 |
| **成员管理** | M3.MemberList | 无（依赖配置） | 参考设计，但简化（游戏服务器节点数有限） |

### 第四优先级：辅助功能

| 目标 | 来源模块 | 替换策略 |
|------|---------|---------|
| **EventStream** | M5 | 直接采用，替代自建事件系统 |
| **Scheduler** | M6 | 结合现有 `engine/timer/`，补充 Actor 级定时 |
| **Router** | M4 | 按需引入（如房间广播用 Broadcast） |
| **Dead Letter** | M1.DeadLetter | 直接采用 |
| **Metrics** | M9.Metrics | 参考设计，集成 Prometheus |

### 模块独立性评分

```
可独立替换（低耦合）:
  ★★★★★ EventStream  - 完全独立，零依赖
  ★★★★★ Internal     - 纯数据结构，零依赖
  ★★★★☆ Scheduler    - 仅依赖 SenderContext 接口
  ★★★★☆ Stream       - 仅依赖 ActorSystem
  ★★★★☆ Router       - 仅依赖 Actor 接口

需要适配（中等耦合）:
  ★★★☆☆ Persistence  - 依赖 ReceiverMiddleware 机制
  ★★★☆☆ Supervision  - 依赖 Actor 生命周期
  ★★★☆☆ Future       - 依赖 ProcessRegistry

核心引擎（高耦合，需整体替换）:
  ★★☆☆☆ Actor Core   - Context/Process/Mailbox 强耦合
  ★★☆☆☆ Remote       - 深度依赖 Actor + gRPC
  ★☆☆☆☆ Cluster      - 依赖 Remote + Actor + Gossip
```

### 推荐迭代路线

```
Phase 1: 基础设施（1-2周）
  └─ 引入 EventStream、Internal Queue、Dead Letter

Phase 2: Actor 引擎升级（2-3周）
  └─ 重构 Actor 接口 → Props/Context/Process 三件套
  └─ 引入 Unbounded Mailbox + Dispatcher
  └─ 完善 Supervision（OneForOne + ExponentialBackoff）
  └─ 引入 Future + Behavior

Phase 3: 通信层增强（2-3周）
  └─ 保留 Kafka 传输，参考 Remote 架构设计消息路由
  └─ 统一序列化接口
  └─ 引入 Router（Broadcast 用于房间广播）

Phase 4: 集群能力（可选，3-4周）
  └─ 引入一致性哈希定位
  └─ 简化版 MemberList
  └─ 参考 Grain 改造 Space 系统
```

---

> 生成时间: 2026-03-03
> 基于 ProtoActor-Go dev 分支源码分析
