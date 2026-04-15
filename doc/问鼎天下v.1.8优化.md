# 问鼎天下 v1.8 优化计划

> 基于 v1.7 需求审核，识别遗留项与下一阶段迭代方向
> 审核日期：2026-04-09
> 当前代码量：~45,400 行 Go 代码（不含 better/ 参考实现，302 个文件）

---

## 〇、项目约定

### better/ 目录规则

**`better/` 为参考实现目录（vendored Leaf 和 ProtoActor 源码），遵循以下规则：**

1. **不参与项目编译**：不直接编译进引擎产物，仅作为设计参考和对比基准
2. **不参与需求审核**：审核功能完成度时，不将 `better/` 下的代码计入已完成项
3. **不参与测试统计**：`better/` 下的测试失败不影响引擎整体测试状态评估
4. **不计入代码量**：统计项目代码量时排除 `better/` 目录
5. **只读参考**：可查阅实现思路，但新代码应在引擎对应模块中独立实现，不得直接复制或 import

---

## 一、v1.7 需求完成度审核

### v1.6 遗留项 — ✅ 全部清零

| 需求项 | 状态 | 说明 |
|--------|------|------|
| ActorPool MetricsRegistry 集成 | ✅ | actor/actor_pool_metrics.go 实现 WithMetrics() 注入，注册 engine_pool_active_count/idle_count/scale_up_total/scale_down_total 四个指标；middleware/metrics_pool_adapter.go 完成适配层 |
| Excel 直读支持 | ✅ | config/excel_impl.go（`//go:build xlsx`）集成 excelize/v2，支持 Read() 单 Sheet + ReadMultiSheet() 多 Sheet 读取；config/excel_stub.go 提供无依赖时的友好错误提示 |

**关键里程碑**：连续四版遗留的所有技术债务已全部清零，无遗留项。

---

### 方向一：引擎内核深度优化 — ✅ 完全完成（4/4）

| 需求项 | 状态 | 说明 |
|--------|------|------|
| Actor 生命周期钩子扩展 | ✅ | actor/lifecycle_hooks.go 实现 PreStart/PostStop/PreRestart/PostRestart 四个钩子 + WithHookTimeout() 超时保护（默认 5s）+ Panic 隔离 + Props 链式配置 |
| Actor 优先级 Mailbox | ✅ | actor/mailbox_priority.go 实现 PriorityMailbox（High/Normal/Low 三级队列）+ MessagePrioritizer 接口 + 饥饿检测机制（低优消息自动提升）+ 背压协同（低优先丢弃）+ WithPriorityMailbox() 配置 |
| Actor Stash/Unstash 消息暂存 | ✅ | actor/stash.go 实现 messageStash 暂存栈 + actor/context.go 定义 Stash()/UnstashAll()/StashSize() 接口 + DefaultStashCapacity=1000 容量限制 + ErrStashFull 错误 + FIFO 重投递 + WithStashCapacity() 配置；含完整测试 stash_test.go |
| Dead Letter 增强 | ✅ | actor/deadletter_enhanced.go 实现 DeadLetterMetrics 接口（engine_deadletter_total 计数器按类型分类）+ 频率告警（AlertThreshold/AlertWindow + EventStream 事件发布）+ 持久化存储（DeadLetterRecord 最近 N 条，默认 500，自动驱逐）+ Stats()/RecentRecords() 查询；middleware/metrics_deadletter_adapter.go 适配层；含完整测试 |

---

### 方向二：分布式能力增强 — ✅ 完全完成（3/3）

| 需求项 | 状态 | 说明 |
|--------|------|------|
| 分布式事务（Saga 模式） | ✅ | saga/ 目录实现编排式 Saga：SagaBuilder 链式定义 Step{Action, Compensate} + Coordinator 顺序执行 + 失败反向补偿（含重试 MaxRetries/RetryDelay）+ SagaStore 持久化接口 + MemorySagaStore 实现 + 全局/步骤级超时 + 同步/异步执行模式；含完整测试 |
| Actor 消息持久化（Event Sourcing） | ✅ | persistence/eventsourced.go 实现 EventSourced 接口（PersistenceID/ApplyEvent/SnapshotState/RestoreSnapshot）+ EventJournal/SnapshotStore 存储接口 + MemoryJournal/MemorySnapshotStore 实现 + EventSourcedContext（Persist/PersistAll/Recover/SaveSnapshot）+ 自动快照策略（每 N 条事件，默认 100）；eventsourced_middleware.go 与现有中间件共存；含完整测试 |
| 跨节点 Request-Response | ✅ | remote/request_response.go 实现 RemoteFutureRegistry + RemoteRequestMessage/RemoteResponseMessage + crypto/rand 唯一 RequestID + 响应路由回发起节点 + Future 超时自动清理（5 分钟 cleanupLoop）+ 错误传播；含完整测试 |

---

### 方向三：游戏业务能力 — ✅ 完全完成（4/4）

| 需求项 | 状态 | 说明 |
|--------|------|------|
| 房间/匹配系统框架 | ✅ | room/ 目录实现 RoomInstance 状态机（Waiting→Running→Finished→Stopped）+ RoomManager（创建/查找/销毁）+ Matcher 接口 + EloMatcher（动态范围扩展）+ QueueMatcher（FIFO）+ ConditionMatcher（自定义条件）+ MatchService 整合 + WaitTimeout/GameTimeout 超时 + RoomEvent 事件系统；含 17 个测试用例 |
| 排行榜服务 | ✅ | leaderboard/ 目录实现 SkipList 跳表（O(log N) 插入）+ Board 排行榜（UpdateScore/GetRank/GetTopN/GetAroundMe）+ LeaderboardActor 多排行榜管理 + ResetPolicy（Daily/Weekly/None）+ Snapshot/RestoreFromSnapshot 快照持久化 + MaxSize 容量限制；含 9 个测试用例 |
| 邮件/通知系统框架 | ✅ | mail/ 目录实现 Mailbox（收发/已读/附件领取/过期清理/容量管理）+ MailActor 实现 Actor 接口 + NotificationService（在线推送/离线队列/上线拉取）+ Template 消息模板（参数化渲染）+ Attachment 附件系统（二次领取防护）+ Broadcast 广播；含 9 个测试用例 |
| 定时任务调度器增强 | ✅ | timer/distributed.go 实现 DistributedScheduler + IsLeaderFn 选主执行（非 Leader 跳过）+ TaskStore 持久化接口 + MemoryTaskStore 实现 + 启动时自动恢复已注册任务 + TaskLog 执行日志（Success/Failed/Timeout/Canceled）+ Panic 恢复 + 超时控制 + CronExpr 集成；含 7 个测试用例 |

---

### 方向四：可观测性与运维 — ✅ 完全完成（3/3）

| 需求项 | 状态 | 说明 |
|--------|------|------|
| OpenTelemetry 追踪集成 | ✅ | middleware/otel.go 定义 Span/Tracer/SpanExporter 抽象接口 + otel_adapter.go（`//go:build otel`）OTel SDK 适配 + otel_default.go（`//go:build !otel`）NoOp 本地追踪 + W3C Trace Context 传播（formatTraceParent/parseTraceParent）+ AlwaysSampler/NeverSampler/RatioSampler 三种采样策略 + otel_middleware.go Actor 中间件自动创建 Span；含测试 |
| Dashboard v3 实时监控 | ✅ | dashboard/live_push.go 实现 LivePushServer WebSocket 实时推送（多主题：runtime/metrics/cluster/hotactors + 客户端订阅过滤 + 异步写缓冲）+ v3_report.go 实现运行报告（ExportReport JSON/CSV 导出 + Heatmap Actor 热力图 + graphNode/graphEdge 集群拓扑图）；含测试 |
| 健康检查端点 | ✅ | dashboard/healthcheck.go 实现 /healthz（Liveness）+ /readyz（Readiness）+ AddLivenessCheck()/AddReadinessCheck() 自定义检查注册 + runChecks() 聚合（全通过返回 200/UP，任一失败返回 503/DOWN）+ Panic 恢复；含测试 |

---

### 方向五：开发体验与生态 — ✅ 完全完成（3/3）

| 需求项 | 状态 | 说明 |
|--------|------|------|
| 热更新框架 | ✅ | hotreload/ 目录实现 Plugin 接口（Name/Version/Load/Reload/Unload/Health）+ GoPluginLoader（.so 动态加载 + ScanDir 目录扫描）+ ScriptPlugin/ScriptWatcher（脚本文件监听自动热更）+ 版本管理（PluginInfo + ReloadEvent 历史记录）+ Rollback 回滚（保存前一版本）+ SetEventListener 事件通知；含测试 |
| 协议文档自动化 | ✅ | codegen/protocol_doc.go 实现 GenerateOpenAPIDoc（OpenAPI JSON）+ GenerateHTMLDoc（带搜索的 HTML 页面）+ GenerateChangelog（版本对比：Added/Removed/Changed + 字段级变更）+ MockResponse（根据定义生成 Mock）+ GenerateMockServer（Go Mock 代码生成）；含测试 |
| 示例项目 — 完整小游戏 Demo | ✅ | example/demo_game/ 实现完整的多人猜数对战游戏：PlayerActor（模拟客户端）+ GameRoomActor（状态机：Waiting→Playing→Finished）+ MatchmakerActor（队列匹配）+ LeaderboardActor 集成 + 完整消息定义（C2S/S2C/内部）+ 回合超时（30s）+ 得分计算 + `go run` 直接运行 |

---

## 二、当前构建与测试状态

```
构建状态：✅ go build ./... 通过（零错误）

测试状态（34 个包全部通过）：
✅ engine/actor                      — 通过
✅ engine/cluster                     — 通过
✅ engine/cluster/federation          — 通过
✅ engine/cluster/provider/consul     — 通过
✅ engine/cluster/provider/etcd       — 通过
✅ engine/cluster/provider/k8s        — 通过
✅ engine/codec                       — 通过
✅ engine/codegen                     — 通过
✅ engine/config                      — 通过
✅ engine/console                     — 通过
✅ engine/dashboard                   — 通过
✅ engine/ecs                         — 通过
✅ engine/errors                      — 通过
✅ engine/gate                        — 通过
✅ engine/grain                       — 通过
✅ engine/hotreload                   — 通过
✅ engine/internal                    — 通过
✅ engine/leaderboard                 — 通过
✅ engine/log                         — 通过
✅ engine/mail                        — 通过
✅ engine/middleware                   — 通过
✅ engine/network                     — 通过
✅ engine/persistence                 — 通过
✅ engine/proto                       — 通过
✅ engine/pubsub                      — 通过
✅ engine/remote                      — 通过
✅ engine/room                        — 通过
✅ engine/router                      — 通过
✅ engine/saga                        — 通过
✅ engine/scene                       — 通过
✅ engine/stress                      — 通过
✅ engine/timer                       — 通过

无测试文件（6 个非核心包）：
⚠️ engine/cmd/engine                 — CLI 工具入口，无测试
⚠️ engine/codegen/cmd/msggen         — 代码生成 CLI，无测试
⚠️ engine/codegen/cmd/msgversion     — 版本管理 CLI，无测试
⚠️ engine/example                    — 示例代码，无需测试
⚠️ engine/example/client             — 客户端示例，无需测试
⚠️ engine/example/demo_game          — 游戏 Demo，无需测试

❌ engine/better/.../mongodb          — MongoDB 未连接（参考实现，不计入）
```

---

## 三、v1.7 遗留问题

**无遗留项。** v1.7 规划的 19 项需求全部完成，完成率 100%。

### 待改善项（非阻塞，可在 v1.8 中迭代优化）

| 项目 | 说明 | 优先级 |
|------|------|--------|
| 排行榜 GetRank 性能 | 当前 O(N) 遍历查排名，可优化为 O(log N) 通过跳表直接计算 | 🟢 低 |
| 邮件持久化后端 | mail/ 当前纯内存，生产需接入数据库存储 | 🟡 中 |
| Saga 分布式集成 | Saga 当前为进程内协调，未与 Remote 层打通跨节点 Saga | 🟡 中 |
| Demo 游戏客户端 | demo_game 仅服务端模拟，缺少 TypeScript Web 客户端 | 🟢 低 |

---

## 四、v1.8 新增优化方向

### 方向一：性能极致优化

#### 1.1 Actor Zero-Alloc 消息通道

**优先级**：🔴 高（万人同服场景下 GC 是首要瓶颈）

**现状**：Envelope 对象池已实现基础池化，但消息传递链路中仍有大量短生命周期对象分配（消息封装、Sender 捕获、Context 临时状态）。

**目标**：
- [ ] 消息传递全链路 Zero-Alloc 审计：用 `go test -benchmem` 识别 Send/Request 路径的所有分配点
- [ ] Context 对象池化：actorCell 的 Context 复用（每次 Receive 不再创建新 Context）
- [ ] 消息批处理 Ring Buffer：替代 slice append，本地消息批量投递零拷贝
- [ ] PID 缓存优化：高频 Send 目标 PID 查找结果缓存（避免重复 ProcessRegistry 查询）
- [ ] Benchmark 基线建立：核心路径基准测试 + CI 回归守护

**预期效果**：Send 路径 allocs/op 从当前值降低 70%+

**预期代码量**：~300-400 行

---

#### 1.2 Remote 层 Zero-Copy 序列化

**优先级**：🔴 高（跨节点通信序列化开销显著）

**现状**：Remote 层消息经过 Codec.Marshal → []byte → 长度前缀 → TCP Write 多次内存拷贝。

**目标**：
- [ ] 实现 StreamCodec 接口：直接向 Writer 编码，避免中间 []byte 分配
- [ ] 使用 `io.WriterTo` 模式：消息对象实现 WriteTo(w io.Writer)，直接写入 TCP 缓冲区
- [ ] 接收端零拷贝解码：从 Reader 直接解码，避免读取到临时 buffer
- [ ] 大消息分片传输：超过阈值的消息自动分片，减少单次内存分配峰值
- [ ] Benchmark 对比：JSON/Protobuf/Binary 三种 Codec 的序列化吞吐和内存消耗

**预期代码量**：~300-400 行

---

#### 1.3 Mailbox 自适应调度策略

**优先级**：🟡 中（不同负载模式下的最优调度）

**现状**：Mailbox 使用固定吞吐量调度（每次取 N 条消息处理），无法适应突发流量和空闲场景。

**目标**：
- [ ] 自适应吞吐量：根据队列深度动态调整每次处理消息数（空闲时少取降延迟，积压时多取提吞吐）
- [ ] 协作式调度：处理时间超阈值时主动让出 goroutine（防止单个 Actor 独占调度器）
- [ ] Dispatcher 工作窃取：空闲 Dispatcher 从忙碌队列窃取任务（提升 CPU 利用率）
- [ ] 调度指标暴露：每个 Mailbox 的平均处理延迟、队列深度、调度频率

**预期代码量**：~300-400 行

---

### 方向二：集群高可用增强

#### 2.1 Actor 状态迁移（Live Migration）

**优先级**：🔴 高（滚动升级和节点缩容的核心能力）

**现状**：节点下线时 Actor 被直接停止，状态丢失。Grain 虚拟 Actor 可在新节点重新激活，但非 Grain 的有状态 Actor 无法迁移。

**目标**：
- [ ] Migratable 接口：`MarshalState() ([]byte, error)` / `UnmarshalState([]byte) error`
- [ ] 迁移协议：源节点 Pause（暂停处理）→ Serialize → Transfer → 目标节点 Spawn + Restore → Resume
- [ ] 消息转发：迁移过程中消息自动转发到新 PID（通过 Redirect 中间层）
- [ ] 迁移进度回调：`OnMigrationStart` / `OnMigrationComplete` 事件
- [ ] 与 Cluster Singleton 联动：Singleton 迁移时自动触发 Live Migration
- [ ] 迁移超时保护：超时后自动回滚到源节点

**使用场景**：
```go
type PlayerActor struct {
    state PlayerState
}

func (p *PlayerActor) MarshalState() ([]byte, error) {
    return json.Marshal(p.state)
}

func (p *PlayerActor) UnmarshalState(data []byte) error {
    return json.Unmarshal(data, &p.state)
}

// 触发迁移
cluster.Migrate(playerPID, targetNodeAddr)
```

**预期代码量**：~500-700 行

---

#### 2.2 集群滚动升级支持

**优先级**：🔴 高（零停机部署是生产环境刚需）

**现状**：节点更新需要逐个手动停止-启动，缺乏协调机制。

**目标**：
- [ ] 滚动升级协调器：按顺序逐个升级节点，确保集群始终有足够的健康节点
- [ ] 节点排空（Drain）：升级前将节点标记为 Draining，不再接受新请求，等待存量请求完成
- [ ] 版本兼容检查：节点上线时检查协议版本兼容性（向前兼容 N-1 版本）
- [ ] 金丝雀发布：支持部分节点先升级，验证通过后再滚动全部
- [ ] 升级回滚：检测到新版本异常自动回滚
- [ ] 升级状态广播：通过 Gossip 同步升级进度

**预期代码量**：~400-600 行

---

#### 2.3 多数据中心支持

**优先级**：🟡 中（全球化部署场景）

**现状**：cluster/federation 实现了跨集群网关，但未考虑网络延迟和数据本地性。

**目标**：
- [ ] 数据中心感知路由：优先路由到同数据中心的 Actor（减少跨 IDC 延迟）
- [ ] 数据中心标签：节点加入集群时声明所属 DC（如 `dc=us-east-1`）
- [ ] 就近读取策略：状态查询优先本地副本，写入路由到主节点
- [ ] 跨 DC 心跳优化：跨数据中心心跳间隔更长，避免跨 IDC 流量过大
- [ ] DC 故障转移：整个数据中心不可达时，自动将流量切换到备用 DC

**预期代码量**：~400-500 行

---

### 方向三：游戏引擎进阶

#### 3.1 帧同步/状态同步框架

**优先级**：🔴 高（多人实时对战核心基础设施）

**现状**：引擎支持 ECS 帧驱动和场景管理，但缺乏标准化的同步框架。

**目标**：
- [ ] 帧同步（Lockstep）模式：
  - FrameSyncRoom Actor：收集所有玩家输入 → 广播确定性帧数据
  - 输入缓冲：提前缓冲 N 帧输入，平滑网络抖动
  - 帧校验：服务端运行相同逻辑，定期校验哈希防作弊
- [ ] 状态同步（State Sync）模式：
  - StateSyncRoom Actor：服务端权威运算 → 增量状态下发
  - 状态差分压缩：仅发送变化部分（Delta Compression）
  - 客户端预测 + 服务端校正：预测本地输入，收到权威状态后回滚校正
- [ ] 同步模式抽象接口：`SyncStrategy`，业务层选择适合的同步方式
- [ ] 网络延迟补偿：Round-Trip Time 测量 + 服务端回退（Lag Compensation）

**预期代码量**：~600-800 行

---

#### 3.2 AI 行为树框架

**优先级**：🟡 中（NPC 智能行为是中重度游戏的基础需求）

**现状**：无 AI 相关模块，NPC 行为需要业务层手动实现。

**目标**：
- [ ] 行为树核心节点：
  - 组合节点：Sequence（顺序）、Selector（选择）、Parallel（并行）
  - 装饰节点：Inverter（取反）、Repeater（重复）、Limiter（限次）
  - 叶子节点：Action（执行动作）、Condition（条件判断）
- [ ] 黑板（Blackboard）：行为树共享数据上下文
- [ ] 可视化定义：行为树 JSON 配置加载（支持配表热重载）
- [ ] 与 ECS 集成：AISystem 驱动所有 NPC 的行为树 Tick
- [ ] 调试工具：Dashboard 中行为树执行路径可视化

**使用场景**：
```go
tree := bt.NewTree(
    bt.Selector(
        bt.Sequence(
            bt.Condition("IsEnemyInRange", checkRange),
            bt.Action("Attack", attackEnemy),
        ),
        bt.Sequence(
            bt.Condition("IsHealthLow", checkHealth),
            bt.Action("Flee", fleeFromEnemy),
        ),
        bt.Action("Patrol", patrol),
    ),
)
```

**预期代码量**：~500-600 行

---

#### 3.3 战斗回放系统

**优先级**：🟡 中（PVP 举证、赛事回放、AI 训练数据采集）

**现状**：persistence/eventsourced.go 已实现 Event Sourcing 基础设施，可复用。

**目标**：
- [ ] 回放记录器（ReplayRecorder）：基于 Event Sourcing，记录战斗全程输入事件
- [ ] 回放播放器（ReplayPlayer）：按时间线重放事件序列，还原战斗过程
- [ ] 回放数据格式：紧凑二进制编码（时间戳 + 事件类型 + 事件数据）
- [ ] 回放倍速控制：1x / 2x / 4x 加速播放 + 暂停/跳转
- [ ] 回放存储策略：热数据本地 + 冷数据归档到对象存储（可选）
- [ ] 与 Room 系统集成：RoomActor 可选开启回放记录

**预期代码量**：~400-500 行

---

#### 3.4 背包/道具系统框架

**优先级**：🟢 低（通用游戏功能，作为引擎可选模块）

**目标**：
- [ ] Item 定义：道具模板（ID、类型、叠加上限、属性效果）
- [ ] InventoryActor：背包 Actor（增删查改 + 容量管理 + 排序）
- [ ] 消耗/使用管线：UseItem → 前置检查 → 执行效果 → 数量扣减 → 通知
- [ ] 堆叠与拆分：可叠加道具的合并和拆分逻辑
- [ ] 交易安全：双方背包原子操作（借助 Saga 保证一致性）
- [ ] 配表驱动：道具模板从 RecordFile/Excel 加载

**预期代码量**：~400-500 行

---

### 方向四：运维与部署

#### 4.1 Kubernetes Operator 支持

**优先级**：🔴 高（云原生部署标配）

**现状**：引擎支持 K8s Provider 做服务发现，但缺少 K8s 原生运维集成。

**目标**：
- [ ] CRD 定义：`EngineCluster` 自定义资源（集群规模、版本、配置）
- [ ] Pod 标签与注解规范：引擎节点自动设置标准 K8s 标签（app、version、role）
- [ ] 健康检查探针集成：/healthz → livenessProbe、/readyz → readinessProbe 的 K8s 部署模板
- [ ] HPA 自动伸缩指标：暴露自定义 Metrics（连接数、Actor 数）供 HPA 决策
- [ ] Helm Chart：标准化部署包（单节点/集群/全球多区域三种模式）
- [ ] ConfigMap 热重载：引擎配置通过 K8s ConfigMap 注入，变更自动生效

**预期代码量**：~400-500 行（Go 代码）+ ~300 行（YAML 模板）

---

#### 4.2 灰度发布与流量控制

**优先级**：🟡 中（大规模运营的安全发布手段）

**目标**：
- [ ] 流量标签路由：消息携带灰度标签（如用户 ID 分桶），路由到指定版本节点
- [ ] 权重路由：按比例将流量分配到新旧版本（如 5% 新版 / 95% 旧版）
- [ ] 灰度规则引擎：支持按用户 ID、地区、渠道等条件匹配灰度策略
- [ ] 灰度指标对比：新旧版本的错误率、延迟、资源消耗自动对比
- [ ] 一键全量/回滚：灰度验证通过后一键全量发布，异常时一键回滚

**预期代码量**：~400-500 行

---

#### 4.3 链路级性能分析（Continuous Profiling）

**优先级**：🟡 中（生产环境性能诊断）

**目标**：
- [ ] 按需 Profiling：通过 Dashboard 或 API 触发 CPU/Memory/Goroutine/Block Profile
- [ ] 自动告警 Profiling：CPU 使用率/GC 暂停时间超阈值时自动采集 Profile
- [ ] Profile 存储与对比：历史 Profile 归档，支持两个时间点的 diff 分析
- [ ] 与 OTel Span 关联：特定 TraceID 的请求自动关联 Profile 数据
- [ ] Actor 级别 Profiling：单个 Actor 的消息处理耗时分布

**预期代码量**：~300-400 行

---

### 方向五：生态与互操作

#### 5.1 Unity/Unreal 客户端 SDK

**优先级**：🔴 高（商业游戏客户端必需）

**现状**：codegen 可生成 TypeScript SDK 和 C# 类型定义，但缺少完整的客户端连接库。

**目标**：
- [ ] C# SDK（Unity 适配）：
  - TCP/WebSocket 连接管理 + 自动重连
  - 消息序列化/反序列化（JSON/Protobuf 双模式）
  - 消息路由分发（注册处理器，按消息类型自动分发）
  - 网络状态回调（Connected/Disconnected/Reconnecting）
- [ ] Codegen 增强：
  - 生成完整的 C# SDK 客户端代码（含连接管理，不仅仅是类型定义）
  - 生成 Unity Package 目录结构（可直接导入 Unity 工程）
- [ ] TypeScript SDK 增强：
  - 添加消息路由分发器（替代 switch-case 手动分发）
  - 支持 Protobuf 编解码（当前仅 JSON）

**预期代码量**：~600-800 行（模板代码 + 生成逻辑）

---

#### 5.2 GM 管理后台框架

**优先级**：🟡 中（游戏运营管理的标准需求）

**目标**：
- [ ] GM 命令系统：注册 GM 命令（如修改玩家属性、发放道具、踢人等）
- [ ] 权限模型：角色权限控制（管理员 / 运营 / 客服 / 只读）
- [ ] 操作审计：所有 GM 操作记录审计日志（操作人、操作内容、影响对象、时间）
- [ ] 批量操作：支持批量发送邮件、批量封禁/解封、批量发放奖励
- [ ] 与 Dashboard 整合：GM 功能嵌入现有 Dashboard Web UI
- [ ] REST API：所有 GM 操作通过 REST API 暴露，支持自动化脚本

**预期代码量**：~500-600 行

---

#### 5.3 自动化压测框架

**优先级**：🟡 中（CI 集成压测，防止性能回归）

**现状**：stress/ 目录有基础压力测试，但缺乏系统化的压测方案。

**目标**：
- [ ] 压测场景 DSL：用配置文件定义压测场景（并发数、持续时间、消息频率、玩家行为模型）
- [ ] 虚拟玩家（Bot）：可编程的模拟客户端，按配置执行游戏行为序列
- [ ] 指标收集：压测过程中自动收集 TPS、P99 延迟、错误率、资源使用率
- [ ] 报告生成：压测结束后生成 HTML 报告（含图表和对比基线）
- [ ] CI 集成：每次发版前自动运行压测，性能低于基线则阻止合并
- [ ] 分布式压测：支持多台机器协同发压（解决单机并发上限）

**预期代码量**：~500-700 行

---

#### 5.4 插件体系标准化

**优先级**：🟢 低（引擎生态长期发展基础）

**现状**：hotreload/ 实现了 Plugin 接口和动态加载，但缺乏标准化的插件生命周期和分发机制。

**目标**：
- [ ] 插件清单文件（plugin.yaml）：声明插件元数据（名称、版本、依赖、兼容引擎版本）
- [ ] 依赖解析：插件间依赖自动解析和加载顺序排列
- [ ] 插件隔离：每个插件独立的日志命名空间和指标前缀
- [ ] 生命周期钩子：Init → Start → HealthCheck → Stop → Cleanup
- [ ] `engine plugin install/remove/list` CLI 命令

**预期代码量**：~300-400 行

---

## 五、优先级排序与迭代计划

### 第一批（v1.8-alpha）— 性能 + 高可用

| 序号 | 任务 | 优先级 | 预期工作量 |
|------|------|--------|-----------|
| 1 | Actor Zero-Alloc 消息通道 | 🔴 高 | ~350 行 |
| 2 | Remote 层 Zero-Copy 序列化 | 🔴 高 | ~350 行 |
| 3 | Actor 状态迁移（Live Migration） | 🔴 高 | ~600 行 |
| 4 | 集群滚动升级支持 | 🔴 高 | ~500 行 |
| 5 | 帧同步/状态同步框架 | 🔴 高 | ~700 行 |

### 第二批（v1.8-beta）— 游戏进阶 + 运维

| 序号 | 任务 | 优先级 | 预期工作量 |
|------|------|--------|-----------|
| 6 | Kubernetes Operator 支持 | 🔴 高 | ~500 行 |
| 7 | Unity/Unreal 客户端 SDK | 🔴 高 | ~700 行 |
| 8 | AI 行为树框架 | 🟡 中 | ~550 行 |
| 9 | 战斗回放系统 | 🟡 中 | ~450 行 |
| 10 | Mailbox 自适应调度策略 | 🟡 中 | ~350 行 |

### 第三批（v1.8-rc）— 生态 + 深化

| 序号 | 任务 | 优先级 | 预期工作量 |
|------|------|--------|-----------|
| 11 | GM 管理后台框架 | 🟡 中 | ~550 行 |
| 12 | 灰度发布与流量控制 | 🟡 中 | ~450 行 |
| 13 | 自动化压测框架 | 🟡 中 | ~600 行 |
| 14 | 链路级性能分析 | 🟡 中 | ~350 行 |
| 15 | 多数据中心支持 | 🟡 中 | ~450 行 |
| 16 | 背包/道具系统框架 | 🟢 低 | ~450 行 |
| 17 | 插件体系标准化 | 🟢 低 | ~350 行 |

### 总预期新增代码量：~7,500-9,000 行

---

## 六、技术决策要点

### 6.1 Zero-Alloc 策略

**推荐**：渐进优化，Benchmark 驱动

```bash
# 建立基线
go test ./actor/... -bench=BenchmarkSend -benchmem -count=5

# 优化后对比
benchstat old.txt new.txt
```

**理由**：
- 不盲目优化，用 pprof 和 benchmem 精确定位分配热点
- 优先优化 Send/Request 热路径（占总分配 80%+）
- 保持代码可读性：仅在性能关键路径使用 unsafe/对象池

### 6.2 Actor 状态迁移方案

**推荐**：两阶段迁移（Pause-Transfer-Resume）

```
源节点                          目标节点
  │                                │
  ├── 1. Pause (暂停消息处理)      │
  ├── 2. MarshalState()           │
  ├── 3. ──── state bytes ────→   │
  │                                ├── 4. Spawn + UnmarshalState()
  │                                ├── 5. Resume (开始处理消息)
  ├── 6. 设置 Redirect(新PID)     │
  ├── 7. 转发积压消息 → ──────→   │
  └── 8. Stop (销毁源 Actor)       │
```

**理由**：
- 暂停期间消息不丢失（Stash 暂存）
- Redirect 保证迁移对调用方透明
- 迁移超时自动回滚，不会卡死

### 6.3 帧同步 vs 状态同步选择

**推荐**：框架同时支持，由业务层选择

| 方案 | 适用场景 | 特点 |
|------|---------|------|
| 帧同步 (Lockstep) | 格斗、MOBA、RTS | 带宽低，但要求确定性计算 |
| 状态同步 (State Sync) | MMO、FPS、IO 游戏 | 容错好，但带宽消耗较大 |

**理由**：
- 两种方案在不同游戏类型下各有优势
- 统一 SyncStrategy 接口，RoomActor 按需切换
- 帧同步需要配合确定性物理引擎，状态同步需要 Delta Compression

### 6.4 行为树实现方案

**推荐**：数据驱动（JSON 配置）+ 代码扩展

```json
{
  "type": "selector",
  "children": [
    {
      "type": "sequence",
      "children": [
        {"type": "condition", "name": "IsEnemyInRange"},
        {"type": "action", "name": "Attack"}
      ]
    },
    {"type": "action", "name": "Patrol"}
  ]
}
```

**理由**：
- JSON 配置可热重载（策划无需重新编译）
- Action/Condition 函数由 Go 代码注册，保持性能
- 配合 Dashboard 可实现在线行为树编辑器

### 6.5 K8s 部署策略

**推荐**：Helm Chart + CRD（渐进式）

- **Phase 1**（v1.8）：Helm Chart 模板化部署 + 标准 K8s 探针
- **Phase 2**（v1.9）：CRD + Operator 实现自动化运维（自动扩缩、滚动升级）

**理由**：
- Helm Chart 成本低、适配所有 K8s 环境
- CRD/Operator 开发成本高，但运维自动化价值大
- 渐进式避免一次性引入过多复杂度

### 6.6 GM 管理后台权限模型

**推荐**：RBAC（角色权限控制）

```
角色定义：
├── admin      — 全部权限
├── operator   — 运营操作（发邮件、发公告、活动管理）
├── support    — 客服操作（查看玩家、发道具、解封）
└── viewer     — 只读查看
```

**理由**：
- RBAC 模型简单清晰，覆盖游戏运营的权限需求
- 与 Dashboard 现有 Auth 机制对接
- 所有操作审计日志已有基础（dashboard/audit.go）

---

## 七、风险评估

| 风险 | 影响 | 应对 |
|------|------|------|
| Zero-Alloc 优化引入 unsafe | 内存安全风险 | 限制 unsafe 使用范围 + 竞态检测（go test -race）+ 模糊测试 |
| Actor 状态迁移期间消息丢失 | 玩家操作丢失 | Stash 暂存 + Redirect 转发 + 迁移超时回滚 |
| 帧同步确定性不一致 | 客户端表现分裂 | 定点数运算替代浮点数 + 服务端校验哈希 + 不一致自动重同步 |
| 滚动升级新旧版本协议不兼容 | 通信失败 | 协议版本号 + 向前兼容 N-1 版本 + 握手阶段协商 |
| K8s Operator 维护成本 | 新增基础设施代码 | Phase 1 仅 Helm Chart，CRD 延后到 v1.9 |
| 行为树性能开销 | 大量 NPC Tick 导致 CPU 飙升 | 行为树 LOD（远处 NPC 降低 Tick 频率）+ 对象池化节点 |
| GM 权限漏洞 | 线上资产风险 | 所有 GM 操作双重确认 + 审计日志不可删除 + 敏感操作需二次鉴权 |
| 压测框架本身的性能瓶颈 | 压测数据不准确 | 压测客户端用轻量协程，避免 GC 抖动影响结果 |

---

## 八、v1.3 → v1.8 演进总览

```
v1.3（架构奠基）
├── Phase 1-4 核心功能全部完成
├── Actor 引擎 + 分布式 + 集群 + 游戏层
└── 初始架构代码

v1.4（补齐加固）— ✅ 完成
├── WebSocket Gate + Protobuf Codec 框架 + 配置热重载
├── 测试/基准测试补齐 + 错误处理规范化
├── Dashboard 增强 + AllForOne 监管 + 外部服务发现
└── 遗留：Remote 层仍硬编码 JSON

v1.5（生产就绪）— ✅ 完成
├── Graceful Shutdown + 消息追踪 + Rate Limiter
├── 场景转移 + ECS 调度器 + 战斗框架
├── 结构化日志 + Prometheus 指标 + 客户端 SDK
├── Gate 握手 + ECS-Actor 融合（超额）
└── 遗留：Remote Codec/Proto 定义/Codegen Proto

v1.6（深度优化）— ✅ 完成（94.1%）
├── Protobuf 全链路打通（三版遗留清零）
├── Actor Pool 弹性伸缩 + Mailbox 背压 + 批处理
├── Split-Brain 检测 + 集群单例 + 跨集群网关
├── AOI 多算法 + A* 寻路 + Gate 安全 + 敏感数据保护
├── engine-cli + TestKit + 连接池动态扩缩
└── 遗留：ActorPool 指标 ⚠️、Excel 直读 ⚠️

v1.7（业务能力 + 生态）— ✅ 完成（100%）
├── 内核：生命周期钩子 + 优先级 Mailbox + Stash/Unstash + Dead Letter 增强
├── 分布式：Saga 事务 + Event Sourcing + 跨节点 Request-Response
├── 游戏业务：房间/匹配 + 排行榜 + 邮件通知 + 分布式定时任务
├── 可观测：OpenTelemetry + Dashboard v3 + 健康检查
├── 生态：热更新框架 + 协议文档自动化 + 完整小游戏 Demo
└── 遗留：无

v1.8（极致性能 + 商业就绪）— 规划中
├── 性能：Zero-Alloc 消息通道 + Zero-Copy 序列化 + 自适应调度
├── 高可用：Actor 状态迁移 + 滚动升级 + 多数据中心
├── 游戏进阶：帧同步/状态同步 + AI 行为树 + 战斗回放 + 背包道具
├── 运维：K8s Operator + 灰度发布 + 持续 Profiling
├── 生态：Unity/UE SDK + GM 后台 + 自动化压测 + 插件标准化
└── 目标：可商业化运营的高性能游戏引擎
```

---

## 九、v1.7 完成度总结

| 分类 | 完成 | 未完成 | 完成度 |
|------|------|--------|--------|
| v1.6 遗留项 | 2/2 | 0 | 100% |
| 方向一：引擎内核深度优化 | 4/4 | 0 | 100% |
| 方向二：分布式能力增强 | 3/3 | 0 | 100% |
| 方向三：游戏业务能力 | 4/4 | 0 | 100% |
| 方向四：可观测性与运维 | 3/3 | 0 | 100% |
| 方向五：开发体验与生态 | 3/3 | 0 | 100% |
| **总计** | **19/19** | **0** | **100%** |

**关键结论**：
- v1.7 规划的 19 项需求 **全部完成**（100%），无遗留项
- 新增 7 个模块（saga/、room/、leaderboard/、mail/、hotreload/、persistence/eventsourced、remote/request_response）
- 构建零错误，34 个测试包全部通过
- 代码量从 v1.6 的 ~34,400 行增长到 ~45,400 行（+32.0%）
- 文件数从 249 增长到 302（+53 个新文件）
- 引擎已具备完整的游戏业务能力、分布式事务支持、可观测体系和开发者工具链
- v1.8 的重点从"功能补齐"转向"性能极致优化"和"商业化就绪"

---

*文档版本：v1.8*
*生成时间：2026-04-09*
*基于 v1.7 需求审核生成*
*当前代码量：~45,400 行 Go 代码（不含 better/ 参考实现，302 个文件）*
