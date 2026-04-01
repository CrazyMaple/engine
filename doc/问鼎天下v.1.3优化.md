# Application-Level Distributed Actor Model 框架深度分析

> 目标：选定游戏后端引擎初版基座框架，并规划后续迭代路线
> 分析对象：Leaf (leaf-master) vs Proto.Actor (protoactor-go-dev)
> 日期：2026-03-05

---

## 一、框架概览

| 维度 | Leaf | Proto.Actor |
|------|------|-------------|
| 定位 | 游戏服务器框架 | 通用分布式 Actor 框架 |
| 核心代码量 | ~3,700 行 / 40 文件 | ~19,300 行 / 217 文件 |
| 测试代码 | 少量 example_test | 18,378 行 / 95 文件 |
| 外部依赖 | 0（纯标准库） | 20+ 直接依赖 |
| Go 版本 | 1.13+ | 1.25+ |
| 跨平台 | Go only | Go / C# / Kotlin |
| 生产案例 | 中小型游戏 | 企业级分布式系统 |

---

## 二、核心架构对比

### 2.1 Leaf 架构

```
┌────────────────────────────────────────────────┐
│               leaf.Run(modules...)              │
│  启动顺序：Register → Init → Run → Signal → Destroy │
├────────────────────────────────────────────────┤
│  Module 系统（每个 Module = 1 goroutine）        │
│  ┌──────────────────────────────────────┐      │
│  │  Skeleton（模块骨架）                 │      │
│  │  ├── select 多路复用主循环            │      │
│  │  ├── ChanRPC Server/Client 模块间通信 │      │
│  │  ├── Timer Dispatcher 定时器          │      │
│  │  ├── Go 安全 goroutine 池             │      │
│  │  └── Console 运维命令                 │      │
│  └──────────────────────────────────────┘      │
├────────────────────────────────────────────────┤
│  网络层                                        │
│  ├── Gate：TCP + WebSocket 客户端接入          │
│  ├── Cluster：节点间 TCP 直连                  │
│  ├── Processor：JSON / Protobuf 消息编解码     │
│  └── MsgParser：自定义长度字段消息分帧          │
├────────────────────────────────────────────────┤
│  工具库：Log / Timer / Console / MongoDB / Util │
└────────────────────────────────────────────────┘
```

**设计哲学**：Module-per-goroutine，通过 Channel 隔离并发，select 事件驱动。

**Skeleton.Run 核心循环**（整个框架的灵魂，仅 24 行）：
```go
for {
    select {
    case <-closeSig:           // 关闭信号
    case ri := <-client.ChanAsynRet:  // 异步RPC回调
    case ci := <-server.ChanCall:     // RPC请求处理
    case cb := <-g.ChanCb:           // goroutine回调
    case t := <-dispatcher.ChanTimer: // 定时器触发
    }
}
```

### 2.2 Proto.Actor 架构

```
┌─────────────────────────────────────────────────────┐
│                   ActorSystem                        │
│  ProcessRegistry / EventStream / DeadLetter / Root   │
├─────────────────────────────────────────────────────┤
│  Actor 核心                                          │
│  ┌───────────────────────────────────────────┐      │
│  │  Props → spawn → PID → Process → Mailbox  │      │
│  │  ├── Actor.Receive(Context)  业务逻辑     │      │
│  │  ├── Context（8个功能接口组合）            │      │
│  │  │   info / base / message / sender       │      │
│  │  │   receiver / spawner / stopper / ext   │      │
│  │  ├── Behavior Stack 行为栈               │      │
│  │  ├── Supervision 监管策略                 │      │
│  │  └── Middleware Chain 中间件链            │      │
│  └───────────────────────────────────────────┘      │
├─────────────────────────────────────────────────────┤
│  Remote 层（gRPC + Protobuf）                        │
│  ├── EndpointManager → Writer/Reader                 │
│  ├── 自动重连 / 批量传输 / 2M+ msg/sec               │
│  └── RemoteProcess 透明代理                          │
├─────────────────────────────────────────────────────┤
│  Cluster 层（Orleans 风格虚拟 Actor）                 │
│  ├── Grain 自动激活/去激活/位置透明                    │
│  ├── Gossip 集群拓扑同步                              │
│  ├── PubSub 跨集群发布订阅                            │
│  ├── IdentityLookup 一致性哈希分片                    │
│  └── ClusterProvider: Consul/etcd/K8s/ZK/AutoManaged │
├─────────────────────────────────────────────────────┤
│  高级功能                                            │
│  ├── Router: Broadcast/RoundRobin/Random/ConsistentHash │
│  ├── Scheduler / EventStream / Persistence            │
│  └── Middleware: OpenTelemetry / Prometheus            │
└─────────────────────────────────────────────────────┘
```

**设计哲学**：经典 Actor Model（Erlang/Akka 血统），Mailbox 驱动，Process 抽象统一本地/远程。

---

## 三、关键维度深度对比

### 3.1 消息通信机制

| 特性 | Leaf (ChanRPC) | Proto.Actor (Mailbox) |
|------|----------------|----------------------|
| 通信载体 | Go Channel | MPSC 无锁队列 |
| 消息类型 | `interface{}` 弱类型 | `interface{}` + Protobuf 强类型 |
| 同步调用 | `Call0/Call1/CallN` | `RequestFuture` + `Future.Result()` |
| 异步调用 | `AsynCall` + 回调函数 | `Send` fire-and-forget |
| 请求-应答 | 手动 chanRet | `Request` + `Respond` 内置 |
| 消息转发 | 无 | `Forward(pid)` |
| 消息暂存 | 无 | `Stash()` 暂存栈 |
| 超时控制 | 无内建 | `ReceiveTimeout` 自动触发 |
| 背压控制 | Channel 容量 | Mailbox + Dispatcher 吞吐量配置 |

**分析**：
- Leaf 的 ChanRPC 简单直观，适合模块间粗粒度通信，但缺乏细粒度 Actor 间通信
- Proto.Actor 的 Mailbox 系统更完整，支持消息暂存、超时、转发等高级特性
- **游戏场景关键差异**：游戏需要大量玩家 Actor 间的细粒度通信（如同场景战斗），Proto.Actor 天然支持，Leaf 需要自行实现

### 3.2 并发模型

| 特性 | Leaf | Proto.Actor |
|------|------|-------------|
| 并发粒度 | Module 级别（粗） | Actor 级别（细） |
| 单个实体 | 1 Module = 1 goroutine | 1 Actor = 1 Mailbox（逻辑单线程） |
| 同场景多玩家 | 同一 Module goroutine 处理 | 每个玩家一个 Actor |
| 竞态安全 | Channel 隔离 | Actor 隔离（Mailbox 串行处理） |
| goroutine 使用 | 模块数 + Go池 | Dispatcher 管理（可共享 goroutine） |
| CPU 利用 | 受限于模块数 | 天然并行（每个 Actor 可并发调度） |

**分析**：
- Leaf 的粗粒度并发在**小型游戏**中足够，但在**大规模场景**（万人同服、大量 AOI 交互）时，单 Module goroutine 成为瓶颈
- Proto.Actor 每个 Actor 逻辑单线程但可并发调度，天然适合游戏中的**实体抽象**（Player、NPC、Room、Scene 各为独立 Actor）

### 3.3 容错与监管

| 特性 | Leaf | Proto.Actor |
|------|------|-------------|
| panic 恢复 | `go/go.go` 中 defer recover | Actor 内 defer recover + 监管策略 |
| 监管策略 | 无 | OneForOne / AllForOne / 自定义 |
| 故障传播 | panic → 日志 → 忽略 | panic → 上报父 Actor → 策略决策 |
| 重启统计 | 无 | RestartStatistics（窗口内重启次数） |
| 子 Actor 管理 | 无父子关系 | 完整的 Parent-Children 层级 |
| Actor 监视 | 无 | Watch/Unwatch + Terminated 通知 |

**分析**：
- Leaf 的容错等于"吞掉 panic 继续跑"，适合开发期但**不适合生产环境**
- Proto.Actor 的监管树是 Erlang "Let It Crash" 哲学的实现，能**精确控制故障边界**
- **游戏场景**：一个玩家 Actor 崩溃不应影响整个场景，Proto.Actor 的监管策略完美匹配

### 3.4 网络层与分布式

| 特性 | Leaf | Proto.Actor |
|------|------|-------------|
| 客户端协议 | TCP + WebSocket | 无内建（需自行接入） |
| 节点间协议 | TCP 直连 | gRPC 双向流 |
| 序列化 | JSON / Protobuf（可选） | Protobuf（强制） |
| 服务发现 | 手动配置地址列表 | Consul / etcd / K8s / ZK / AutoManaged |
| 位置透明 | 无 | PID 地址统一本地/远程 |
| 虚拟 Actor | 无 | Grain（自动激活/去激活） |
| 消息路由 | 自定义 | Broadcast / RoundRobin / ConsistentHash |
| 集群扩展性 | 小集群（手动） | 水平扩展（自动分片） |

**分析**：
- Leaf **原生支持**客户端接入（TCP/WS），这对游戏开发是**刚需**
- Proto.Actor **原生支持**节点间通信和集群，但**缺少**游戏客户端接入层
- 两者在这一维度**高度互补**

### 3.5 游戏开发特化功能

| 功能 | Leaf | Proto.Actor |
|------|------|-------------|
| 客户端网关 | Gate 模块（TCP+WS） | 无 |
| 消息编解码 | JSON/Protobuf Processor | Protobuf only |
| 定时器 | AfterFunc / CronFunc（Cron 表达式） | Scheduler（SendOnce/SendRepeatedly） |
| 运维控制台 | Console 模块（TCP 命令行） | 无 |
| 配置表加载 | RecordFile（CSV→struct） | 无 |
| 数据库接入 | MongoDB 连接池 | Persistence（事件溯源抽象） |
| 热更新 | 无 | 无 |

**分析**：
- Leaf 提供的"电池"更贴合游戏开发日常需求
- Proto.Actor 的功能更通用化，游戏特化需要自行补齐

---

## 四、上手难度评估

### 4.1 Leaf 上手路径

```
Day 1: 理解 Module/Skeleton/ChanRPC 三件套     → 写出第一个游戏模块
Day 2: 理解 Gate/Processor/Agent                → 客户端能连上来
Day 3: 理解 Timer/Go/Console                    → 定时任务、异步IO、运维
Day 5: 通读全部 3700 行源码                      → 完全掌握框架
```

**优势**：
- 代码量极少，通读源码仅需半天
- 概念简单：Module = goroutine，ChanRPC = 函数调用
- 游戏开发者的**思维模型**天然匹配（模块化、消息处理循环）

**劣势**：
- ChanRPC 的 `interface{}` 弱类型容易出错
- 无 IDE 自动补全支持（函数签名是 `func([]interface{})`)
- 扩展需要自己造轮子

### 4.2 Proto.Actor 上手路径

```
Day 1: 理解 Actor/PID/Props/Context              → 写出第一个 Actor
Day 2: 理解 Supervision/Lifecycle                 → 写出有容错的系统
Day 3: 理解 Remote/gRPC                           → 跨节点通信
Day 5: 理解 Cluster/Grain/Gossip                  → 分布式集群
Day 7: 理解 Router/Middleware/EventStream          → 高级功能
Day 14: 通读核心模块源码                           → 深度掌握
```

**优势**：
- 60+ 个完整示例，覆盖几乎所有使用场景
- API 设计精良，`Actor.Receive(Context)` 一个接口即可
- 概念与 Akka/Erlang 一致，社区资料丰富

**劣势**：
- 概念较多（Props/Mailbox/Dispatcher/Process/Middleware...）
- 分布式部分（Cluster/Grain）学习曲线陡峭
- 依赖较重（gRPC、Protobuf、服务发现组件）
- 游戏场景需要自行补齐客户端接入层

### 4.3 上手难度评分

| 维度 | Leaf | Proto.Actor |
|------|------|-------------|
| 概念数量 | ★★☆☆☆（5个核心概念） | ★★★★☆（15+个核心概念） |
| 源码可读性 | ★★★★★（3700行，极简） | ★★★☆☆（19300行，抽象多层） |
| 游戏场景适配 | ★★★★★（原生游戏框架） | ★★☆☆☆（需要补齐游戏层） |
| 首个可运行Demo | ★★★★★（30分钟） | ★★★☆☆（2小时） |
| 生产级开发 | ★★☆☆☆（需大量补齐） | ★★★★☆（大部分已就绪） |
| 团队新人培训 | ★★★★★（读源码即文档） | ★★★☆☆（需系统学习 Actor 模型） |

---

## 五、优势劣势总结

### 5.1 Leaf 优势

1. **极致精简**：3700 行代码，零外部依赖，编译快、二进制小
2. **游戏原生**：Gate/Agent/Processor 开箱即用的客户端接入
3. **上手极快**：概念少、源码少、半天通读
4. **并发安全**：Module-per-goroutine + Channel 隔离，无竞态
5. **定制友好**：代码足够简单，可随心改造

### 5.2 Leaf 劣势

1. **非 Actor 模型**：Module 是粗粒度并发单元，无法抽象细粒度实体
2. **无监管策略**：panic 只是被吞掉，无故障传播和恢复机制
3. **无分布式能力**：Cluster 模块仅 66 行，Run/OnClose 均为空实现
4. **弱类型通信**：ChanRPC 全程 `interface{}`，编译期无法检查
5. **无行为切换**：无法实现 Actor 的状态机模式
6. **扩展性受限**：单 Module goroutine 在大规模场景下成为瓶颈
7. **无中间件机制**：日志、追踪、监控需要侵入式添加

### 5.3 Proto.Actor 优势

1. **标准 Actor 模型**：完整的 Actor/PID/Mailbox/Supervision 实现
2. **分布式就绪**：gRPC 远程通信 + 多种集群方案 + 虚拟 Actor
3. **强大的容错**：监管策略 + 重启统计 + 故障边界控制
4. **高性能**：MPSC 无锁队列，2M+ msg/sec
5. **中间件体系**：Receiver/Sender/Spawn 三层中间件 + OpenTelemetry
6. **行为栈**：`SetBehavior/PushBehavior/PopBehavior` 状态机支持
7. **位置透明**：PID 统一本地/远程寻址
8. **路由器**：内建多种消息路由策略
9. **完善测试**：18,000+ 行测试代码

### 5.4 Proto.Actor 劣势

1. **无游戏层**：缺少客户端接入（Gate/WebSocket/TCP 消息分帧）
2. **依赖过重**：gRPC/Protobuf/Consul/etcd 等，构建和部署复杂
3. **概念门槛高**：15+ 个核心概念，新手需要系统学习
4. **过度抽象**：Props/Process/Mailbox/Dispatcher 层层嵌套
5. **游戏适配差**：需要自行设计 Room/Scene/Player 的 Actor 拓扑
6. **集群配置复杂**：需要额外部署 Consul/etcd 等服务发现组件
7. **不可控依赖**：20+ 第三方库的安全性和兼容性风险

---

## 六、框架选型决策

### 6.1 决策矩阵

| 评估指标 | 权重 | Leaf 评分 | Proto.Actor 评分 |
|----------|------|-----------|-----------------|
| 上手速度 | 20% | 9 | 5 |
| Actor 模型完整度 | 25% | 2 | 9 |
| 游戏开发适配 | 20% | 9 | 3 |
| 分布式能力 | 15% | 1 | 9 |
| 容错与监管 | 10% | 2 | 9 |
| 可扩展性 | 10% | 3 | 8 |
| **加权总分** | **100%** | **4.85** | **6.70** |

### 6.2 最终选型：Proto.Actor 作为初版基座

**理由**：

1. **目标是 Actor 模型引擎**，Proto.Actor 提供了完整的 Actor 语义（这是根本目标）
2. **补齐比替换更容易**：给 Proto.Actor 加游戏层（Gate/Agent）的工作量，远小于给 Leaf 加 Actor 模型（Mailbox/Supervision/PID/Remote）
3. **分布式是刚需**：游戏后端天然需要多节点部署，Proto.Actor 的 Remote/Cluster 体系成熟
4. **监管策略不可或缺**：生产环境必须有容错机制，从零实现不如借鉴成熟方案
5. **长期收益**：Proto.Actor 的架构天花板远高于 Leaf

**但需要从 Leaf 吸收的核心能力**：

| 从 Leaf 吸收 | 原因 |
|--------------|------|
| Gate 模块 | 客户端 TCP/WebSocket 接入是游戏刚需 |
| 消息处理器 | JSON/Protobuf 灵活编解码 |
| Skeleton 事件循环 | 定时器+RPC+goroutine 回调的多路复用模式极优雅 |
| Console 运维 | 游戏运维必备 |
| RecordFile 配置表 | 游戏配置加载的便捷方案 |
| 零依赖理念 | 尽可能减少不必要的外部依赖 |

**需要从 Proto.Actor 去掉的部分**：

| 去掉/精简 | 原因 |
|-----------|------|
| 多集群 Provider（Consul/etcd/K8s/ZK） | 初期只保留 AutoManaged，按需添加 |
| OpenTelemetry/OpenTracing 中间件 | 初期不需要，后续按需集成 |
| Persistence 模块 | 游戏持久化模式不同于事件溯源 |
| gRPC 远程层 | 替换为更轻量的自研 TCP 协议（参考 Leaf 的 MsgParser） |
| Protobuf 强制依赖 | 序列化层可插拔，支持多种编解码 |
| 大部分间接依赖 | 精简到核心必要依赖 |

---

## 七、迭代路线图

### Phase 1：核心 Actor 引擎（v0.1）

**目标**：精简的单节点 Actor 系统 + 游戏客户端接入

**从 Proto.Actor 提取**：
- [ ] Actor 接口 + Context（精简为 5 个核心接口）
- [ ] PID + Process + ProcessRegistry（本地寻址）
- [ ] Props + Spawner（Actor 工厂）
- [ ] Mailbox + MPSC 无锁队列（消息投递）
- [ ] Dispatcher（goroutine 调度）
- [ ] Supervision 策略（OneForOne + 自定义）
- [ ] 生命周期消息（Started/Stopping/Stopped/Restarting）
- [ ] Behavior Stack（状态机支持）
- [ ] Future/Promise（异步等待）
- [ ] EventStream（系统事件总线）
- [ ] DeadLetter（死信处理）

**从 Leaf 吸收**：
- [ ] Gate 模块（TCP + WebSocket 并行接入）
- [ ] MsgParser（消息分帧：长度字段 + 大小端 + 最大长度）
- [ ] Agent 模式（每连接一个 Agent Actor）
- [ ] Timer/CronExpr（定时器 + Cron 表达式）
- [ ] Console 运维模块

**自研**：
- [ ] 轻量级消息编解码层（Protobuf + JSON 可插拔）
- [ ] Actor 地址设计（为后续分布式预留）

**交付物**：
- 能运行的游戏服务器骨架
- 客户端连上来，每个玩家一个 Actor
- 支持房间/场景 Actor 管理
- 基础定时器和运维控制台

**预期代码量**：~5,000-7,000 行

---

### Phase 2：轻量级分布式（v0.2）

**目标**：多节点部署 + 跨节点消息通信

**核心工作**：
- [ ] Remote 模块（替代 gRPC，使用自研 TCP 协议）
  - 基于 Leaf 网络层的 TCPClient/TCPServer
  - 自定义消息序列化（非 gRPC，更轻量）
  - 连接池管理 + 自动重连
- [ ] RemoteProcess（远程 Actor 代理）
- [ ] PID 扩展（Address 字段支持远程寻址）
- [ ] 位置透明（Send/Request 自动路由本地/远程）
- [ ] EndpointManager（远程节点连接管理）
- [ ] 节点注册发现（AutoManaged 模式，无外部依赖）

**交付物**：
- 多个游戏节点可以互相通信
- Actor 可以跨节点发送消息
- 位置透明（代码不区分本地/远程 Actor）

**预期代码量**：新增 ~3,000-4,000 行

---

### Phase 3：集群管理（v0.3）

**目标**：自动化集群管理 + 负载均衡

**核心工作**：
- [ ] Cluster 模块
  - Gossip 协议同步成员状态
  - 心跳检测 + 故障转移
  - 集群拓扑变更通知
- [ ] Router 路由器
  - Broadcast（场景广播）
  - ConsistentHash（玩家分片）
  - RoundRobin（负载均衡）
- [ ] 虚拟 Actor（简化版 Grain）
  - 基于 ID 自动激活（玩家上线自动创建 Actor）
  - 超时自动去激活（玩家下线回收 Actor）
  - 一致性哈希定位
- [ ] PubSub 发布订阅
  - 跨节点事件广播
  - 频道订阅/取消订阅

**交付物**：
- 集群自动发现和管理
- 玩家 Actor 自动在集群中定位/创建/销毁
- 场景广播、世界频道等功能的基础设施

**预期代码量**：新增 ~5,000-6,000 行

---

### Phase 4：游戏引擎层（v0.4）

**目标**：游戏特化功能 + 开发体验优化

**核心工作**：
- [ ] 空间管理系统
  - Scene Actor（场景管理）
  - AOI（Area of Interest）兴趣区域管理
  - 空间索引（九宫格/四叉树）
- [ ] 实体组件系统（ECS 可选）
  - 与 Actor 模型融合
  - Component 作为 Actor 状态的组织方式
- [ ] 数据持久化
  - 游戏存档模式（非事件溯源）
  - 定期快照 + 增量保存
  - 多数据库后端支持
- [ ] 消息协议工具链
  - Proto 文件生成消息处理代码
  - 自动路由注册
  - 客户端 SDK 生成
- [ ] 配置管理
  - RecordFile 配置表加载（借鉴 Leaf）
  - 热重载支持
- [ ] 中间件体系
  - 日志中间件（结构化日志）
  - 指标中间件（Prometheus 可选）
  - 链路追踪中间件（按需）

**交付物**：
- 完整的游戏后端引擎
- 场景管理、AOI、实体系统
- 数据持久化方案
- 开发工具链

**预期代码量**：新增 ~8,000-10,000 行

---

### Phase 5：生产加固（v0.5）

**目标**：生产环境就绪

**核心工作**：
- [ ] 性能优化
  - 消息池化（减少 GC）
  - 对象池（sync.Pool）
  - 批量消息处理
  - 基准测试套件
- [ ] 运维工具
  - Dashboard（Web 管理面板）
  - 集群状态可视化
  - Actor 拓扑查看
  - 热点 Actor 分析
- [ ] 外部服务发现（可选）
  - Consul Provider
  - etcd Provider
  - K8s Provider
- [ ] 安全加固
  - TLS 加密通信
  - Actor 权限控制
  - 消息签名/验证
- [ ] 压力测试
  - 模拟万人同服
  - 集群故障演练
  - 网络分区测试

**交付物**：
- 生产级游戏后端引擎
- 完整的运维工具链
- 性能基准和优化报告

---

## 八、项目架构预览

```
maplewish/
├── actor/                  # 核心 Actor 系统
│   ├── actor.go           # Actor 接口定义
│   ├── context.go         # Context 接口组合
│   ├── pid.go             # Actor 进程标识
│   ├── props.go           # Actor 配置蓝图
│   ├── process.go         # 进程抽象
│   ├── mailbox.go         # 消息邮箱
│   ├── dispatcher.go      # goroutine 调度
│   ├── supervision.go     # 监管策略
│   ├── behavior.go        # 行为栈
│   ├── future.go          # Future/Promise
│   ├── deadletter.go      # 死信处理
│   ├── eventstream.go     # 事件总线
│   └── middleware.go       # 中间件链
│
├── network/                # 网络层（Leaf 风格）
│   ├── tcp_server.go      # TCP 服务器
│   ├── tcp_client.go      # TCP 客户端
│   ├── ws_server.go       # WebSocket 服务器
│   ├── msg_parser.go      # 消息分帧
│   └── conn.go            # 连接抽象
│
├── gate/                   # 客户端接入网关
│   ├── gate.go            # 网关模块
│   └── agent.go           # 玩家代理 Actor
│
├── remote/                 # 节点间通信（Phase 2）
│   ├── remote.go          # 远程通信管理
│   ├── endpoint.go        # 端点连接
│   └── remote_process.go  # 远程进程代理
│
├── cluster/                # 集群管理（Phase 3）
│   ├── cluster.go         # 集群核心
│   ├── gossip.go          # Gossip 协议
│   ├── grain.go           # 虚拟 Actor
│   └── router/            # 消息路由
│
├── engine/                 # 游戏引擎层（Phase 4）
│   ├── scene/             # 场景管理
│   ├── aoi/               # 兴趣区域
│   ├── timer/             # 定时器系统
│   └── config/            # 配置管理
│
├── console/                # 运维控制台
├── codec/                  # 编解码器（JSON/Protobuf/自定义）
├── log/                    # 日志系统
└── internal/               # 内部工具（MPSC队列等）
```

---

## 九、关键设计原则

1. **Actor-First**：一切皆 Actor，Player/Room/Scene/NPC 都是 Actor 实例
2. **消息驱动**：所有交互通过消息传递，拒绝共享状态
3. **位置透明**：代码不区分 Actor 在本地还是远程
4. **故障隔离**：一个 Actor 崩溃不影响其他 Actor
5. **渐进式分布式**：单节点开发 → 多节点部署，代码不变
6. **游戏原生**：内置客户端接入、消息编解码、场景管理
7. **最少依赖**：核心引擎零外部依赖（远程通信使用自研协议替代 gRPC）
8. **可插拔**：序列化、日志、存储、服务发现均为插件化设计

---

## 十、风险与应对

| 风险 | 影响 | 应对策略 |
|------|------|---------|
| Actor 模型对团队有学习门槛 | 开发效率下降 | 提供完整示例 + 游戏特化的 Actor 模板 |
| 自研 Remote 协议的稳定性 | 线上故障 | 初期参考 Proto.Actor 的 EndpointWriter/Reader 设计，充分测试 |
| 不用 gRPC 可能损失跨语言能力 | 未来扩展受限 | 保留编解码层接口，后续可加回 gRPC |
| 游戏层 Actor 拓扑设计复杂 | 架构混乱 | Phase 4 前完成 Actor 拓扑设计文档 |
| 集群一致性问题 | 数据不一致 | 渐进式：先 AutoManaged 后接成熟方案 |

---

## 附录 A：代码示例对比

### A.1 Leaf 风格的游戏模块

```go
// Leaf: 一个游戏模块的典型写法
type GameModule struct {
    *module.Skeleton
}

func (m *GameModule) OnInit() {
    m.Skeleton = &module.Skeleton{
        GoLen:              100,
        TimerDispatcherLen: 100,
        AsynCallLen:        100,
        ChanRPCServer:      chanrpc.NewServer(100),
    }
    m.Skeleton.Init()
    m.RegisterChanRPC("PlayerLogin", handlePlayerLogin)
}

func handlePlayerLogin(args []interface{}) {
    playerId := args[0].(int64)
    // 所有玩家共享同一个 goroutine 处理
    // 无法并行处理多个玩家的登录
}
```

### A.2 目标引擎风格的 Actor 写法

```go
// 目标：每个玩家一个 Actor，天然并行
type PlayerActor struct {
    playerId int64
    state    PlayerState
}

func (p *PlayerActor) Receive(ctx actor.Context) {
    switch msg := ctx.Message().(type) {
    case *actor.Started:
        // 加载玩家数据
    case *LoginRequest:
        // 独立 goroutine 处理，与其他玩家并行
        ctx.Respond(&LoginResponse{Success: true})
    case *EnterScene:
        // 通知场景 Actor
        ctx.Send(msg.ScenePID, &PlayerEnter{PID: ctx.Self()})
    case *actor.ReceiveTimeout:
        // 玩家超时下线
        ctx.Stop(ctx.Self())
    }
}

// 客户端连接 → 创建 PlayerActor
func onNewConnection(conn network.Conn) {
    props := actor.PropsFromProducer(func() actor.Actor {
        return &PlayerActor{}
    })
    pid := system.Root.Spawn(props)
    // 将 conn 与 pid 绑定
}
```

---

*文档版本：v1.0*
*生成时间：2026-03-05*
