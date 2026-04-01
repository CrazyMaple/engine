# Proto.Actor (Go) 源码分析

> 版本: dev | 语言: Go | 作者: Asynkron | 包名: github.com/asynkron/protoactor-go

## 一、框架总览

Proto.Actor 是一个跨平台的 Actor 模型框架（支持 Go / .NET / Kotlin），Go 版本提供了完整的 Actor 系统实现，包括本地 Actor、远程通信、集群、持久化等能力。

核心设计理念：**一切皆 Actor，消息驱动，位置透明**。

### 目录结构

```
protoactor-go-dev/
├── actor/              # 核心 Actor 系统（PID/Props/Context/Mailbox/Supervision）
│   └── middleware/     # 中间件示例
├── cluster/            # 集群（Grain/Gossip/PubSub/成员管理）
│   ├── clusterproviders/  # 集群提供者（Consul/etcd/K8s/ZK/AutoManaged）
│   └── identitylookup/   # 身份查找（分布式哈希）
├── remote/             # 远程通信（gRPC 传输）
├── router/             # 路由器（Broadcast/RoundRobin/Random/ConsistentHash）
├── persistence/        # 持久化（事件溯源 + 快照）
├── eventstream/        # 发布-订阅事件流
├── scheduler/          # 定时调度器
├── stream/             # Actor → Channel 桥接
├── plugin/             # 插件系统
├── extensions/         # 扩展注册表
├── metrics/            # OpenTelemetry 指标
├── log/                # 日志辅助
├── internal/           # 内部数据结构（无锁队列）
├── protobuf/           # protoc-gen-go-grain 代码生成
├── ctxext/             # Context 扩展
└── testkit/            # 测试工具
```

---

## 二、Actor 核心系统 (`actor/`)

### 2.1 ActorSystem — 运行时容器 (`actor_system.go`)

ActorSystem 是整个框架的入口，管理所有 Actor 的生命周期：

```go
type ActorSystem struct {
    ProcessRegistry *ProcessRegistryValue   // 进程注册表（PID → Process 映射）
    Root            *RootContext            // 根上下文（用于 Spawn/Send/Request）
    EventStream     *eventstream.EventStream // 全局事件流
    Guardians       *guardiansValue         // 守护者进程管理
    DeadLetter      *deadLetterProcess      // 死信处理
    Extensions      *extensions.Extensions  // 扩展注册表
    Config          *Config                 // 配置
    ID              string                  // 系统唯一 ID (shortuuid)
}
```

创建流程 (`NewActorSystem`):
```
NewActorSystem(options...)
  ├─ 生成唯一 ID (shortuuid)
  ├─ 创建 ProcessRegistry (地址: "nonhost")
  ├─ 创建 RootContext
  ├─ 创建 Guardians (守护者)
  ├─ 创建 EventStream
  ├─ 创建 DeadLetter 进程
  ├─ 注册 Metrics 扩展
  └─ 注册 EventStream 进程
```

### 2.2 核心接口关系

```
Process (进程接口)          Actor (业务接口)
┌─────────────────────┐    ┌──────────────────┐
│ SendUserMessage()   │    │ Receive(Context)  │
│ SendSystemMessage() │    └──────────────────┘
│ Stop()              │
└─────────────────────┘
         ▲
         │ 实现
    ┌────┴────────────┐
    │  ActorProcess   │ ← 标准 Actor 进程
    │  futureProcess  │ ← Future 进程
    │  deadLetterProc │ ← 死信进程
    │  guardianProc   │ ← 守护者进程
    │  router.process │ ← 路由器进程
    │  remote.process │ ← 远程进程
    └─────────────────┘
```

### 2.3 PID — Actor 的地址 (`pid.go`)

PID 是 Actor 的唯一标识，包含地址和 ID：

```go
type PID struct {
    Address   string  // "nonhost" (本地) 或 "host:port" (远程)
    Id        string  // Actor 路径标识
    RequestId uint32  // 请求 ID
    p         *Process // 缓存的 Process 引用（性能优化）
}
```

关键方法：
- `ref(actorSystem)` — 解析 PID 到 Process，优先使用缓存指针，未命中则查 ProcessRegistry
- `sendUserMessage()` — 通过 Process 发送用户消息
- `sendSystemMessage()` — 通过 Process 发送系统消息

Process 引用缓存机制：使用 `atomic.LoadPointer` 无锁读取缓存，检测到 `dead==1` 时清除缓存重新查找。

### 2.4 Props — Actor 配置 (`props.go`)

Props 定义了如何创建和配置一个 Actor：

```go
type Props struct {
    spawner                 SpawnFunc              // 自定义 Spawn 逻辑
    producer                ProducerWithActorSystem // Actor 工厂函数
    mailboxProducer         MailboxProducer        // 邮箱工厂
    guardianStrategy        SupervisorStrategy     // 守护者监督策略
    supervisionStrategy     SupervisorStrategy     // 子 Actor 监督策略
    dispatcher              Dispatcher             // 调度器
    receiverMiddleware      []ReceiverMiddleware   // 接收中间件链
    senderMiddleware        []SenderMiddleware     // 发送中间件链
    spawnMiddleware         []SpawnMiddleware      // Spawn 中间件链
    contextDecorator        []ContextDecorator     // Context 装饰器
    onInit                  []func(ctx Context)    // 初始化回调
}
```

默认 Spawn 流程 (`defaultSpawner`):
```
1. newActorContext(system, props, parent) → 创建 actorContext
2. props.produceMailbox() → 创建 Mailbox
3. NewActorProcess(mailbox) → 创建 ActorProcess
4. ProcessRegistry.Add(proc, id) → 注册进程，获得 PID
5. initialize(props, ctx) → 执行 onInit 回调
6. mailbox.RegisterHandlers(ctx, dispatcher) → 绑定消息处理器
7. mailbox.PostSystemMessage(Started) → 发送启动消息
8. mailbox.Start() → 启动邮箱
```

### 2.5 Context 系统 (`context.go` / `actor_context.go`)

Context 采用**接口分解**设计，将功能拆分为多个小接口组合：

```
Context (完整接口)
├── infoPart        → Parent() / Self() / Actor() / ActorSystem() / Logger()
├── basePart        → Respond / Stash / Watch / Unwatch / SetReceiveTimeout / Forward / ReenterAfter
├── messagePart     → Message() / MessageHeader()
├── senderPart      → Sender() / Send / Request / RequestWithCustomSender / RequestFuture
├── receiverPart    → Receive(envelope)
├── spawnerPart     → Spawn / SpawnPrefix / SpawnNamed
├── stopperPart     → Stop / StopFuture / Poison / PoisonFuture
└── extensionPart   → Get / Set (Context 扩展)
```

组合出的子接口：
- `SenderContext` = infoPart + senderPart + messagePart
- `ReceiverContext` = infoPart + receiverPart + messagePart + extensionPart
- `SpawnerContext` = infoPart + spawnerPart

#### actorContext — 核心实现

```go
type actorContext struct {
    actor             Actor
    actorSystem       *ActorSystem
    extras            *actorContextExtras  // 懒初始化（children/watchers/stash/timer/extensions）
    props             *Props
    parent            *PID
    self              *PID
    receiveTimeout    time.Duration
    messageOrEnvelope interface{}          // 当前正在处理的消息
    state             int32                // stateAlive/stateRestarting/stateStopping/stateStopped
}
```

actorContext 同时实现了 `MessageInvoker` 接口（被 Mailbox 回调）：

```
InvokeUserMessage(msg)
  ├─ 处理 ReceiveTimeout 计时器（暂停/重置）
  ├─ 采集 Metrics（消息处理耗时）
  └─ processMessage(msg)
       ├─ 有 receiverMiddleware → 走中间件链
       ├─ 有 contextDecorator → 走装饰器
       └─ 否则 → defaultReceive()
            ├─ PoisonPill → Stop(self)
            ├─ AutoRespond → actor.Receive + 自动回复
            └─ 默认 → actor.Receive(ctx)

InvokeSystemMessage(msg)
  ├─ *continuation → 恢复 await 上下文并执行回调
  ├─ *Started      → 转发为 UserMessage 处理
  ├─ *Watch        → 添加 watcher（已停止则立即通知 Terminated）
  ├─ *Unwatch      → 移除 watcher
  ├─ *Stop         → handleStop()
  ├─ *Terminated   → 移除子 Actor + 尝试完成停止/重启
  ├─ *Failure      → 委托给监督策略
  └─ *Restart      → handleRestart()
```

#### extras 懒初始化

`actorContextExtras` 仅在需要时创建（调用 `ensureExtras()`），包含：
- `children PIDSet` — 子 Actor 集合
- `watchers PIDSet` — 监视者集合
- `stash *linkedliststack.Stack` — 消息暂存栈
- `receiveTimeoutTimer` — 接收超时定时器
- `rs *RestartStatistics` — 重启统计
- `extensions` — Context 扩展

---

## 三、Mailbox 邮箱系统 (`actor/mailbox.go`)

### 3.1 接口定义

```go
type Mailbox interface {
    PostUserMessage(message interface{})   // 投递用户消息
    PostSystemMessage(message interface{}) // 投递系统消息
    RegisterHandlers(invoker MessageInvoker, dispatcher Dispatcher)
    Start()
    UserMessageCount() int
}

type MessageInvoker interface {
    InvokeSystemMessage(interface{})
    InvokeUserMessage(interface{})
    EscalateFailure(reason interface{}, message interface{})
}
```

### 3.2 defaultMailbox 实现

```go
type defaultMailbox struct {
    userMailbox     queue          // 用户消息队列（可选 bounded/unbounded/lock-free）
    systemMailbox   *mpsc.Queue   // 系统消息队列（始终使用 MPSC 无锁队列）
    schedulerStatus int32         // idle / running（CAS 原子切换）
    userMessages    int32         // 用户消息计数
    sysMessages     int32         // 系统消息计数
    suspended       int32         // 挂起标志
    invoker         MessageInvoker
    dispatcher      Dispatcher
    middlewares     []MailboxMiddleware
}
```

### 3.3 调度机制

```
PostUserMessage / PostSystemMessage
  └─ push 到队列 + 原子递增计数
  └─ schedule()
       └─ CAS(idle → running) 成功 → dispatcher.Schedule(processMessages)

processMessages()
  └─ run()  ← 实际消息处理循环
  └─ 设置 idle
  └─ 检查是否有新消息到达 → 有则 CAS 回 running 继续处理

run() 循环:
  for {
    if i > throughput → runtime.Gosched() 让出 CPU
    1. 优先处理 systemMailbox（SuspendMailbox/ResumeMailbox 特殊处理）
    2. 检查 suspended → 是则 return
    3. 处理 userMailbox
    4. 两个队列都空 → return
  }
  defer recover → EscalateFailure（panic 容错）
```

关键设计：
- **系统消息优先**：始终先处理系统消息，确保生命周期控制及时响应
- **吞吐量控制**：每处理 `throughput` 条消息后 `Gosched()` 让出 CPU
- **挂起机制**：`SuspendMailbox` 暂停用户消息处理（系统消息不受影响）
- **CAS 调度**：无锁的 idle/running 状态切换，避免重复调度

### 3.4 邮箱类型

| 类型 | 文件 | 队列实现 | 特点 |
|------|------|----------|------|
| Unbounded | `unbounded.go` | `goring.Queue` | 无界，基于环形缓冲区 |
| UnboundedLockfree | `unbounded_lock_free.go` | `mpsc.Queue` | 无界，MPSC 无锁队列 |
| Bounded | `bounded.go` | `RingBuffer` | 有界，可选丢弃策略 |

### 3.5 Dispatcher 调度器

```go
type Dispatcher interface {
    Schedule(fn func())  // 调度执行
    Throughput() int     // 每轮处理消息数
}
```

| 实现 | 行为 |
|------|------|
| `goroutineDispatcher` | `go fn()` — 每次调度启动新 goroutine |
| `synchronizedDispatcher` | `fn()` — 同步执行（当前 goroutine） |

---

## 四、消息系统 (`actor/messages.go`)

### 4.1 消息分类体系

Proto.Actor 将消息分为四大类，通过 marker interface 区分：

```
SystemMessage (系统消息 — 生命周期控制，不经过用户 Receive)
├── *Started       → Actor 启动完成（特殊：转发为 UserMessage 处理）
├── *Stop          → 立即停止
├── *Watch         → 注册监视
├── *Unwatch       → 取消监视
├── *Terminated    → 被监视者已终止
├── *Failure       → 子 Actor 失败上报
├── *Restart       → 重启指令
└── *continuation  → ReenterAfter 回调

AutoReceiveMessage (自动处理消息 — 经过用户 Receive 但有特殊语义)
├── *Restarting    → 正在重启
├── *Stopping      → 正在停止
├── *Stopped       → 已停止
└── *PoisonPill    → 毒丸（处理完当前消息后停止）

MailboxMessage (邮箱控制消息 — 不经过 InvokeSystemMessage)
├── *SuspendMailbox  → 挂起用户消息处理
└── *ResumeMailbox   → 恢复用户消息处理

用户消息 (业务消息 — 完全由用户 Receive 处理)
└── 任意类型
```

### 4.2 MessageEnvelope

```go
type MessageEnvelope struct {
    Header  messageHeader    // 元数据 map[string]string
    Message interface{}      // 实际消息体
    Sender  *PID             // 发送者 PID
}
```

- `WrapEnvelope(msg)` — 如果已经是 Envelope 则直接返回，否则包装
- `UnwrapEnvelope(msg)` — 解构出 Header/Message/Sender
- 支持 `MessageBatch` 接口批量投递

### 4.3 特殊消息接口

| 接口 | 作用 |
|------|------|
| `AutoRespond` | 处理后自动回复（如 `Touch` → `Touched`） |
| `NotInfluenceReceiveTimeout` | 不重置接收超时计时器 |
| `IgnoreDeadLetterLogging` | 进入死信时不记录日志 |

---

## 五、监督策略 (`actor/supervision.go`)

### 5.1 核心接口

```go
type SupervisorStrategy interface {
    HandleFailure(actorSystem *ActorSystem, supervisor Supervisor, child *PID,
                  rs *RestartStatistics, reason interface{}, message interface{})
}

type Supervisor interface {
    Children() []*PID
    EscalateFailure(reason interface{}, message interface{})
    RestartChildren(pids ...*PID)
    StopChildren(pids ...*PID)
    ResumeChildren(pids ...*PID)
}

type DeciderFunc func(reason interface{}) Directive
```

### 5.2 Directive 指令

```go
const (
    ResumeDirective   // 恢复子 Actor，忽略错误
    RestartDirective  // 重启子 Actor
    StopDirective     // 停止子 Actor
    EscalateDirective // 向上级传递错误
)
```

### 5.3 四种内置策略

#### OneForOneStrategy（默认）

只影响失败的子 Actor 本身：

```
HandleFailure(child)
  ├─ Resume  → supervisor.ResumeChildren(child)
  ├─ Restart → shouldStop? → StopChildren : RestartChildren(child)
  ├─ Stop    → supervisor.StopChildren(child)
  └─ Escalate → supervisor.EscalateFailure()

shouldStop: maxNrOfRetries==0 || 在 withinDuration 内失败次数 > maxNrOfRetries
```

默认策略：`OneForOne(maxRetries=10, within=10s, decider=AlwaysRestart)`

#### AllForOneStrategy

一个子 Actor 失败时，**所有子 Actor** 都受影响（Restart/Stop 作用于全部 children）。适用于子 Actor 之间有强依赖的场景。

#### ExponentialBackoffStrategy

指数退避重启，带随机抖动：

```go
delay = failureCount * initialBackoff + rand(0~500ns)
time.AfterFunc(delay, func() { supervisor.RestartChildren(child) })
```

每次失败在 `backoffWindow` 内累计，窗口外重置计数。

#### RestartingStrategy

最简单的策略 — 无条件立即重启。

### 5.4 失败处理流程

```
Actor panic
  └─ mailbox.run() defer recover
       └─ invoker.EscalateFailure(reason, msg)
            ├─ 发送 SuspendMailbox 给自己（暂停用户消息）
            ├─ 构造 Failure{Who, Reason, RestartStats, Message}
            ├─ parent == nil → handleRootFailure (使用默认策略)
            └─ parent != nil → parent.sendSystemMessage(Failure)
                 └─ parent.handleFailure()
                      ├─ actor 实现了 SupervisorStrategy → 用 actor 自己的策略
                      └─ 否则 → 用 props.supervisionStrategy
```

---

## 六、Future 异步结果 (`actor/future.go`)

### 6.1 接口定义

```go
type Future interface {
    PID() *PID                          // 背后的临时 Actor PID
    PipeTo(pids ...*PID)                // 结果转发给其他 Actor
    Result() (interface{}, error)       // 阻塞等待结果
    Wait() error                        // 阻塞等待完成
}
```

### 6.2 实现原理

Future 本质是一个**临时 Process**（`futureProcess`），注册到 ProcessRegistry：

```
NewFuture(system, timeout)
  ├─ 创建 futureProcess（内嵌 sync.Cond）
  ├─ 注册到 ProcessRegistry，ID 前缀 "future"
  ├─ 设置超时定时器 time.AfterFunc(timeout, ...)
  │    └─ 超时触发 → err = ErrTimeout → Stop
  └─ 返回 &future

收到响应:
  SendUserMessage(pid, msg)
    ├─ DeadLetterResponse → err = ErrDeadLetter
    └─ 其他 → result = msg
    └─ Stop(pid)

Stop(pid):
  ├─ done = true
  ├─ 停止超时定时器
  ├─ ProcessRegistry.Remove(pid)
  ├─ sendToPipes() → 转发结果给 PipeTo 目标
  ├─ runCompletions() → 执行 continueWith 回调
  └─ cond.Signal() → 唤醒 Result()/Wait() 阻塞者
```

### 6.3 ReenterAfter 机制

`Context.ReenterAfter(future, callback)` 实现非阻塞 await：

```go
// 当 future 完成时，发送 continuation 系统消息给自己
concrete.continueWith(func(_ interface{}, _ error) {
    ctx.self.sendSystemMessage(&continuation{f: wrapper, message: currentMsg})
})
```

continuation 在 `InvokeSystemMessage` 中处理，恢复原始消息上下文后执行回调，保证回调在 Actor 的消息循环中串行执行。

---

## 七、Behavior 行为切换 (`actor/behavior.go`)

Behavior 是一个 `ReceiveFunc` 栈，实现 Actor 状态机模式：

```go
type Behavior []ReceiveFunc

func (b *Behavior) Become(receive ReceiveFunc)         // 清空栈，压入新行为
func (b *Behavior) BecomeStacked(receive ReceiveFunc)  // 压栈（保留旧行为）
func (b *Behavior) UnbecomeStacked()                   // 弹栈（恢复上一个行为）
func (b *Behavior) Receive(context Context)            // 执行栈顶行为
```

典型用法：
```go
type myActor struct { behavior actor.Behavior }

func (a *myActor) Receive(ctx actor.Context) {
    a.behavior.Receive(ctx)  // 委托给当前行为
}

// 在某个行为中切换状态
func (a *myActor) connected(ctx actor.Context) {
    switch ctx.Message().(type) {
    case *Disconnect:
        a.behavior.Become(a.disconnected)  // 切换到 disconnected 状态
    }
}
```

---

## 八、ProcessRegistry 进程注册表 (`actor/process_registry.go`)

### 8.1 结构

```go
type ProcessRegistryValue struct {
    SequenceID     uint64            // 自增 ID 计数器（原子操作）
    ActorSystem    *ActorSystem
    Address        string            // "nonhost"（本地）或 "host:port"（远程）
    LocalPIDs      *SliceMap         // 分片并发 Map
    RemoteHandlers []AddressResolver // 远程地址解析器链
}
```

### 8.2 SliceMap — 1024 分片并发 Map

```go
type SliceMap struct {
    LocalPIDs []cmap.ConcurrentMap  // 1024 个分片
}

func (s *SliceMap) GetBucket(key string) cmap.ConcurrentMap {
    hash := murmur32.Sum32([]byte(key))
    return s.LocalPIDs[hash % 1024]
}
```

使用 murmur3 哈希将 PID ID 映射到 1024 个分片之一，每个分片是独立的 `ConcurrentMap`，大幅降低锁竞争。

### 8.3 ID 生成

```go
func (pr *ProcessRegistryValue) NextID() string {
    counter := atomic.AddUint64(&pr.SequenceID, 1)
    return uint64ToID(counter)  // base64 编码，前缀 '$'
}
```

生成格式：`$<base64编码的自增数字>`，如 `$1`, `$a`, `$1B`。

### 8.4 Get 查找流程

```
Get(pid)
  ├─ pid == nil → 返回 DeadLetter
  ├─ pid.Address != localAddress && != 本机地址
  │    └─ 遍历 RemoteHandlers 尝试解析 → 失败返回 DeadLetter
  └─ 本地查找 → LocalPIDs.GetBucket(pid.Id).Get(pid.Id)
       └─ 未找到 → 返回 DeadLetter
```

---

## 九、RootContext 根上下文 (`actor/root_context.go`)

RootContext 是 Actor 系统的外部入口，用于从非 Actor 代码与 Actor 交互：

```go
type RootContext struct {
    actorSystem      *ActorSystem
    senderMiddleware SenderFunc         // 发送中间件链
    spawnMiddleware  SpawnFunc          // Spawn 中间件链
    headers          messageHeader      // 消息头
    guardianStrategy SupervisorStrategy // 守护者策略
}
```

主要能力：
- `Send(pid, msg)` — 单向发送
- `Request(pid, msg)` — 发送（无 Sender，不期望回复）
- `RequestFuture(pid, msg, timeout)` — 发送并返回 Future
- `Spawn(props)` / `SpawnNamed(props, name)` — 创建顶级 Actor
- `Stop(pid)` / `Poison(pid)` — 停止 Actor
- `WithGuardian(strategy)` — 设置守护者策略（顶级 Actor 的监督者）

与 `actorContext` 的区别：
- `Parent()` 返回 nil（或 Guardian PID）
- `Self()` 返回 Guardian PID（如果设置了 guardianStrategy）
- 不处理消息，不参与生命周期

---

## 十、EventStream 事件流 (`eventstream/eventstream.go`)

线程安全的发布-订阅消息总线：

```go
type EventStream struct {
    sync.RWMutex
    subscriptions []*Subscription
    counter       int32
}

type Subscription struct {
    id      int32
    handler Handler          // func(interface{})
    p       Predicate        // 过滤函数（可选）
    active  uint32           // 原子激活标志
}
```

API：
- `Subscribe(handler)` → 返回 Subscription
- `SubscribeWithPredicate(handler, predicate)` → 带过滤的订阅
- `Unsubscribe(sub)` → 取消订阅（swap-remove 优化）
- `Publish(evt)` → 广播给所有活跃且通过 Predicate 的订阅者

框架内部使用场景：
- `DeadLetterEvent` — 死信通知
- `SupervisorEvent` — 监督事件（子 Actor 失败/重启/停止）
- 集群成员变更事件

---

## 十一、Router 路由器 (`router/`)

### 11.1 架构概览

Router 是一个特殊的 Process，拦截消息并按策略分发给一组 routee Actor：

```
调用者 → router.process (代理进程)
              ├─ 用户消息 → State.RouteMessage(msg) → 按策略选择 routee
              └─ 管理消息 → 转发给内部 routerActor 处理
```

### 11.2 State 接口

```go
type State interface {
    RouteMessage(message interface{})       // 路由消息到 routee
    SetRoutees(routees *actor.PIDSet)       // 设置 routee 集合
    GetRoutees() *actor.PIDSet
    SetSender(sender actor.SenderContext)   // 设置发送上下文
}
```

### 11.3 四种路由策略

| 策略 | 路由逻辑 | 适用场景 |
|------|----------|----------|
| Broadcast | 发送给**所有** routee | 广播通知 |
| RoundRobin | 原子自增 index % routee 数量 | 均匀负载分配 |
| Random | `rand.Intn(routeeCount)` | 简单随机分配 |
| ConsistentHash | 消息实现 `Hasher` 接口，hashring 选节点 | 有状态路由（同 key 同 routee） |

### 11.4 两种部署模式

| 模式 | 说明 | 创建方式 |
|------|------|----------|
| Pool | Router 自己 Spawn 子 Actor 作为 routee | `NewXxxPool(size)` |
| Group | 使用外部已存在的 Actor 作为 routee | `NewXxxGroup(pids...)` |

### 11.5 process 代理

`router.process` 实现 `actor.Process` 接口，作为路由器的外部代理：

- `SendUserMessage` → 普通消息直接 `state.RouteMessage()`，管理消息转发给 routerActor
- `SendSystemMessage` → Watch/Unwatch 自行管理，Stop 先停 routerActor 再清理
- `Stop/Poison` → 原子标记 stopping，等待 routerActor 停止后移除注册

Spawn 流程（`router.spawn`）：
```
1. 创建 router.process 并注册到 ProcessRegistry
2. 创建 routerActor（groupRouterActor 或 poolRouterActor）
3. routerActor 启动时调用 config.OnStarted() 初始化 routee
4. WaitGroup 等待 routerActor 就绪
5. 返回代理 PID
```

---

## 十二、Remote 远程通信 (`remote/`)

### 12.1 架构概览

Remote 模块基于 gRPC 实现跨进程 Actor 通信，核心思想是**位置透明** — 发送消息给远程 PID 与本地 PID 使用相同 API。

```
本地 Actor                                          远程 Actor
    │                                                   │
    ├─ Send(remotePID, msg)                             │
    │   └─ ProcessRegistry.Get(remotePID)               │
    │       └─ AddressResolver → remote.process         │
    │           └─ remote.SendMessage()                  │
    │               └─ endpointManager.remoteDeliver()   │
    │                   └─ endpointWriter Actor          │
    │                       └─ gRPC Stream ─────────────→│ endpointReader
    │                                                    │   └─ 本地投递
```

### 12.2 核心组件

| 组件 | 文件 | 职责 |
|------|------|------|
| `remote.process` | `remote_process.go` | 远程 PID 的 Process 代理，拦截消息转发给 endpointManager |
| `endpointManager` | `endpoint_manager.go` | 管理所有远程连接，懒初始化 endpoint |
| `endpointWriter` | `endpoint_writer.go` | 每个远程地址一个，通过 gRPC 流批量发送消息 |
| `endpointWatcher` | `endpoint_watcher.go` | 管理远程 Watch/Unwatch，连接断开时通知 Terminated |
| `endpointReader` | `endpoint_reader.go` | gRPC 服务端，接收远程消息并本地投递 |
| `activatorActor` | `activator_actor.go` | 处理远程 Spawn 请求 |

### 12.3 序列化

```go
type Serializer interface {
    Serialize(msg interface{}) ([]byte, error)
    Deserialize(typeName string, bytes []byte) (interface{}, error)
    GetTypeName(msg interface{}) (string, error)
}
```

内置两种序列化器：
- `protoSerializer`（ID=0，默认）— Protocol Buffers
- `jsonSerializer`（ID=1）— JSON

### 12.4 连接管理

`endpointManager` 使用懒连接模式：
```
ensureConnected(address)
  ├─ connections.Load(address) → 已有则返回
  └─ 创建 endpointLazy → sync.Once 首次访问时连接
       └─ RequestFuture(endpointSupervisor, address)
            └─ 创建 endpointWriter + endpointWatcher Actor 对
```

---

## 十三、Cluster 集群 (`cluster/`)

### 13.1 架构概览

Cluster 在 Remote 之上构建分布式 Virtual Actor（Grain）系统：

```
Cluster
├── Remote              → gRPC 远程通信基础
├── ClusterProvider     → 成员发现（Consul/etcd/K8s/ZK/AutoManaged）
├── IdentityLookup      → 身份查找（将 identity+kind 映射到具体节点 PID）
├── MemberList          → 成员列表管理 + 拓扑变更事件
├── Gossip              → Gossip 协议（成员间状态同步）
├── PubSub              → 分布式发布-订阅
├── PidCache            → PID 缓存（避免重复查找）
└── kinds               → 注册的 Grain 类型
```

### 13.2 核心结构

```go
type Cluster struct {
    ActorSystem    *actor.ActorSystem
    Config         *Config
    Gossip         *Gossiper          // Gossip 协议
    PubSub         *PubSub            // 分布式 Pub/Sub
    Remote         *remote.Remote     // 远程通信
    PidCache       *PidCacheValue     // PID 缓存
    MemberList     *MemberList        // 成员管理
    IdentityLookup IdentityLookup     // 身份查找
    kinds          map[string]*ActivatedKind  // Grain 类型注册
    context        Context            // 集群请求上下文
}
```

### 13.3 Grain（Virtual Actor）

Grain 是集群级别的虚拟 Actor，特点：
- 通过 `identity + kind` 全局唯一标识
- 按需激活，无需手动 Spawn
- 位置透明，自动路由到正确节点
- 节点故障时自动迁移

调用方式：
```go
// 通过 identity + kind 请求 Grain
result, err := cluster.Request("player-123", "PlayerGrain", &GetState{})
```

### 13.4 启动流程

```
cluster.StartMember()
  ├─ 创建 Remote 并启动 gRPC
  ├─ 初始化所有注册的 Kind
  ├─ 设置 IdentityLookup
  ├─ 启动 Gossip 协议
  ├─ 启动 PubSub
  ├─ 初始化拓扑共识
  └─ 启动 ClusterProvider（注册到服务发现）
```

### 13.5 集群提供者

| 提供者 | 目录 | 适用场景 |
|--------|------|----------|
| Consul | `clusterproviders/consul/` | 生产环境 |
| etcd | `clusterproviders/etcd/` | K8s 生态 |
| Kubernetes | `clusterproviders/k8s/` | K8s 原生 |
| ZooKeeper | `clusterproviders/zk/` | Java 生态 |
| AutoManaged | `clusterproviders/automanaged/` | 开发/测试 |

---

## 十四、Persistence 持久化 (`persistence/`)

### 14.1 设计模式：事件溯源 + 快照

```go
type Mixin struct {
    eventIndex    int           // 当前事件索引
    providerState ProviderState // 存储后端
    name          string        // Actor 名称（= PID.Id）
    receiver      receiver      // 消息接收器
    recovering    bool          // 是否正在恢复中
}
```

### 14.2 恢复流程

```
Actor 启动 → persistence.init(provider, context)
  ├─ providerState.Restart()
  ├─ 加载快照: GetSnapshot(name)
  │    └─ 有快照 → eventIndex = snapshotIndex, Receive(snapshot)
  ├─ 重放事件: GetEvents(name, eventIndex, 0)
  │    └─ 逐条 Receive(event), eventIndex++
  ├─ recovering = false
  └─ Receive(ReplayComplete)  ← 通知恢复完成
```

### 14.3 持久化操作

```go
// 持久化事件
mixin.PersistReceive(event)
  ├─ providerState.PersistEvent(name, eventIndex, event)
  ├─ eventIndex % snapshotInterval == 0 → 触发 RequestSnapshot
  └─ eventIndex++

// 持久化快照
mixin.PersistSnapshot(snapshot)
  └─ providerState.PersistSnapshot(name, eventIndex, snapshot)
```

### 14.4 ProviderState 接口

```go
type ProviderState interface {
    GetEvents(actorName string, eventIndexStart int, eventIndexEnd int, callback func(e interface{}))
    GetSnapshot(actorName string) (snapshot interface{}, eventIndex int, ok bool)
    PersistEvent(actorName string, eventIndex int, event proto.Message)
    PersistSnapshot(actorName string, eventIndex int, snapshot proto.Message)
    Restart()
    GetSnapshotInterval() int
}
```

---

## 十五、Scheduler 定时调度 (`scheduler/`)

基于 `time.AfterFunc` 的定时消息发送器：

```go
type TimerScheduler struct {
    ctx actor.SenderContext
}
```

| 方法 | 行为 |
|------|------|
| `SendOnce(delay, pid, msg)` | 延迟后发送一次 |
| `SendRepeatedly(initial, interval, pid, msg)` | 延迟后周期发送 |
| `RequestOnce(delay, pid, msg)` | 延迟后 Request 一次 |
| `RequestRepeatedly(delay, interval, pid, msg)` | 延迟后周期 Request |

所有方法返回 `CancelFunc` 用于取消。

周期发送实现（`startTimer`）：
```
time.AfterFunc(delay, func() {
    fn()           // 执行发送
    t.Reset(interval)  // 重置定时器实现周期
})
```

使用原子状态（init/ready/done）避免 Cancel 与 Reset 的竞态。

---

## 十六、Stream 流桥接 (`stream/`)

将 Actor 消息桥接到 Go Channel：

### TypedStream[T]（泛型版）

```go
type TypedStream[T any] struct {
    C   <-chan T       // 只读 channel
    pid *actor.PID     // 内部临时 Actor
}
```

创建一个临时 Actor，收到消息后写入 channel。调用 `Close()` 停止 Actor 并关闭 channel。

### UntypedStream

非泛型版本，channel 类型为 `chan interface`。

---

## 十七、辅助模块

### 17.1 Plugin 插件系统 (`plugin/`)

通过中间件机制将插件注入 Actor 生命周期：

```go
func Use(plugin ...actor.ReceiverMiddleware) actor.PropsOption {
    return actor.WithReceiverMiddleware(plugin...)
}
```

插件本质是 `ReceiverMiddleware`，在消息到达 Actor 前/后执行逻辑。Persistence 模块就是通过 Plugin 机制注入的。

### 17.2 Extensions 扩展注册表 (`extensions/`)

类型安全的扩展注册机制：

```go
type ExtensionID int32

func NextExtensionID() ExtensionID  // 全局自增 ID

type Extensions struct {
    extensions []Extension  // 按 ExtensionID 索引
}

func (e *Extensions) Get(id ExtensionID) Extension
func (e *Extensions) Register(extension Extension)
```

每个扩展通过 `ExtensionID()` 返回唯一 ID，注册后可通过 `ActorSystem.Extensions.Get(id)` 快速获取。Cluster、Metrics 等都作为扩展注册。

### 17.3 Metrics 指标 (`metrics/`)

基于 OpenTelemetry 的可观测性：

```go
type ActorMetrics struct {
    ActorMailboxLength           metric.Int64Histogram
    ActorMessageReceiveDuration  metric.Float64Histogram
    ActorSpawnCount              metric.Int64Counter
    ActorStoppedCount            metric.Int64Counter
    ActorRestartedCount          metric.Int64Counter
    ActorFailureCount            metric.Int64Counter
    FuturesStartedCount          metric.Int64Counter
    FuturesCompletedCount        metric.Int64Counter
    FuturesTimedOutCount         metric.Int64Counter
    // ... 更多指标
}
```

通过 `ActorSystem.Config.MetricsEnabled` 开关，在 actorContext 的关键路径（Spawn/Stop/Restart/Failure/InvokeUserMessage）中采集。

---

## 十八、Actor 生命周期深度分析

### 18.1 完整生命周期状态机

```
                    ┌──────────────────────────────────────┐
                    │                                      │
  Spawn ──→ [stateAlive] ──Stop──→ [stateStopping] ──→ [stateStopped]
                │                        │
                │ Failure+Restart        │ 等待子 Actor 停止
                │                        │ → finalizeStop()
                ▼                        │   ├─ Remove from Registry
         [stateRestarting]               │   ├─ Receive(Stopped)
                │                        │   ├─ 通知 watchers Terminated
                │ 等待子 Actor 停止       │   └─ 通知 parent Terminated
                │ → restart()            │
                │   ├─ incarnateActor()  │
                │   ├─ ResumeMailbox     │
                │   ├─ Receive(Started)  │
                │   └─ 重放 stash 消息    │
                │                        │
                └──→ [stateAlive] ◄──────┘
```

### 18.2 消息处理时序

```
外部调用 Send(pid, msg)
  │
  ▼
PID.sendUserMessage(system, msg)
  │
  ▼
PID.ref(system) → Process
  ├─ 缓存命中 → atomic.LoadPointer
  └─ 缓存未命中 → ProcessRegistry.Get(pid)
  │
  ▼
ActorProcess.SendUserMessage(pid, msg)
  │
  ▼
defaultMailbox.PostUserMessage(msg)
  ├─ userMailbox.Push(msg)
  ├─ atomic.AddInt32(&userMessages, 1)
  └─ schedule()
       └─ CAS(idle→running) → dispatcher.Schedule(processMessages)
  │
  ▼
processMessages() → run()
  │
  ▼
actorContext.InvokeUserMessage(msg)
  ├─ 暂停 receiveTimeout
  ├─ processMessage(msg)
  │    └─ middleware chain → defaultReceive()
  │         └─ actor.Receive(ctx)  ← 用户代码
  └─ 重置 receiveTimeout
```

---

## 十九、Goroutine 模型分析

### 19.1 每个 Actor 的 Goroutine 使用

Proto.Actor 不为每个 Actor 分配固定 goroutine，而是**按需调度**：

```
消息到达 → mailbox.schedule()
  └─ CAS(idle→running) 成功时才启动新 goroutine
  └─ goroutineDispatcher.Schedule(fn) → go fn()
```

- 空闲 Actor 不占用 goroutine
- 消息到达时通过 CAS 竞争启动权，保证同一时刻只有一个 goroutine 处理该 Actor 的消息
- 处理完一批消息后回到 idle，新消息到达再次调度

### 19.2 并发安全保证

```
Actor 消息处理的串行性保证：
1. mailbox.schedulerStatus 使用 CAS 原子操作
2. 同一时刻只有一个 goroutine 执行 run()
3. run() 内顺序处理 system + user 消息
4. Actor.Receive() 始终在单一 goroutine 中执行

跨 Actor 通信的并发安全：
1. PID.sendUserMessage → mailbox.PostUserMessage（线程安全的队列 Push）
2. mpsc.Queue / goring.Queue 都是并发安全的
3. 消息投递和消息处理完全解耦
```

### 19.3 吞吐量调优

`Dispatcher.Throughput()` 控制每轮处理的消息数：
- 值越大 → 单次处理更多消息，减少 goroutine 切换开销
- 值越小 → 更公平的调度，避免单个 Actor 长时间占用 CPU
- 超过 throughput 后调用 `runtime.Gosched()` 主动让出

---

## 二十、中间件链机制

### 20.1 三种中间件

| 类型 | 触发时机 | 签名 |
|------|----------|------|
| ReceiverMiddleware | Actor 接收消息时 | `func(next ReceiverFunc) ReceiverFunc` |
| SenderMiddleware | Actor 发送消息时 | `func(next SenderFunc) SenderFunc` |
| SpawnMiddleware | Spawn 子 Actor 时 | `func(next SpawnFunc) SpawnFunc` |

### 20.2 链式构建

中间件采用**洋葱模型**，从外到内包裹：

```
配置: WithReceiverMiddleware(A, B, C)

构建链: C(B(A(defaultReceive)))

执行顺序:
  C.before → B.before → A.before → defaultReceive → A.after → B.after → C.after
```

### 20.3 ContextDecorator

`ContextDecorator` 包装 Context 本身，可以拦截所有 Context 方法调用：

```go
type ContextDecorator func(ctx Context) Context

// 使用时，actor.Receive 收到的是装饰后的 Context
ctx.actor.Receive(ctx.ensureExtras().context)  // 装饰后的 context
```

---

## 二十一、错误处理与容错

### 21.1 panic 恢复

```
mailbox.run()
  defer func() {
      if r := recover(); r != nil {
          invoker.EscalateFailure(r, msg)  // 捕获 panic，上报为 Failure
      }
  }()
```

Actor 中的 panic 不会导致进程崩溃，而是被 mailbox 捕获并转化为监督事件。

### 21.2 监督决策链

```
Actor panic
  → EscalateFailure
    → SuspendMailbox（暂停用户消息）
    → 发送 Failure 给 parent
      → parent.handleFailure
        → DeciderFunc(reason) → Directive
          ├─ Resume   → ResumeMailbox，继续处理
          ├─ Restart  → 检查重试限制 → Restart 或 Stop
          ├─ Stop     → 停止子 Actor
          └─ Escalate → 继续向上传递
```

### 21.3 RestartStatistics

```go
type RestartStatistics struct {
    failureTimes []time.Time
}

func (rs *RestartStatistics) NumberOfFailures(within time.Duration) int
// 统计 within 时间窗口内的失败次数，用于判断是否超过重试限制
```

---

## 二十二、核心数据结构速查表

| 数据结构 | 位置 | 用途 |
|----------|------|------|
| `mpsc.Queue` | `internal/queue/mpsc/` | 多生产者单消费者无锁队列（系统邮箱） |
| `goring.Queue` | 外部依赖 | 无界环形缓冲区（默认用户邮箱） |
| `RingBuffer` | `internal/queue/` | 有界环形缓冲区（Bounded 邮箱） |
| `SliceMap` | `actor/process_registry.go` | 1024 分片并发 Map（进程注册表） |
| `PIDSet` | `actor/pidset.go` | PID 集合（children/watchers/routees） |
| `Behavior` | `actor/behavior.go` | ReceiveFunc 栈（状态机） |
| `linkedliststack` | 外部依赖 | 链表栈（消息 stash） |
| `hashring` | 外部依赖 | 一致性哈希环（ConsistentHash 路由） |

---

## 二十三、框架优缺点分析

### 优点

1. **位置透明**：本地/远程/集群 Actor 使用统一 API，PID 屏蔽位置细节
2. **高性能邮箱**：无锁队列 + CAS 调度，避免不必要的 goroutine 创建
3. **完善的监督树**：四种策略 + 可自定义 Decider，故障隔离能力强
4. **中间件体系**：Receiver/Sender/Spawn 三层中间件 + ContextDecorator，扩展性好
5. **Virtual Actor（Grain）**：集群级虚拟 Actor，按需激活，自动迁移
6. **丰富的集群支持**：Consul/etcd/K8s/ZK 多种服务发现，Gossip 状态同步
7. **事件溯源持久化**：内置 Event Sourcing + Snapshot 模式

### 不足

1. **学习曲线**：概念多（Process/Props/Context/Mailbox/Dispatcher），入门门槛较高
2. **集群复杂度**：Gossip/IdentityLookup/ClusterProvider 层次多，调试困难
3. **错误处理**：依赖 panic/recover 机制，不如显式 error 返回直观
4. **文档不足**：Go 版本文档相对 .NET 版本较少
5. **序列化耦合**：远程通信强依赖 Protocol Buffers

---

## 二十四、与 MapleWish 框架对比参考

| 维度 | Proto.Actor | MapleWish |
|------|-------------|-----------|
| Actor 模型 | 标准 Actor（Mailbox + Dispatcher） | 单线程事件驱动（三邮箱） |
| 消息投递 | 直接 PID 发送 | Kafka 消息队列 |
| 并发模型 | 每 Actor 按需 goroutine | 严格单线程 |
| RPC 模式 | Request/RequestFuture | Call/Read/Send |
| 监督策略 | OneForOne/AllForOne/Backoff | 无内置监督 |
| 远程通信 | gRPC | Kafka |
| 集群 | Virtual Actor + 服务发现 | 无内置集群 |
| 持久化 | Event Sourcing + Snapshot | 无内置持久化 |
| 路由 | Broadcast/RoundRobin/Random/Hash | 无内置路由 |
| 定时器 | Scheduler（Send/Request） | 系统事件（周变/日变/秒变） |
| 复杂度 | 高（功能全面） | 低（专注游戏场景） |
