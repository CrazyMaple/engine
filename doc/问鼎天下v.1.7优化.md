# 问鼎天下 v1.7 优化计划

> 基于 v1.6 需求审核，识别遗留项与下一阶段迭代方向
> 审核日期：2026-04-08
> 当前代码量：~34,400 行 Go 代码（不含 better/ 参考实现，249 个文件）

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

## 一、v1.6 需求完成度审核

### v1.4/v1.5 遗留项 — ✅ 全部清零

| 需求项 | 状态 | 说明 |
|--------|------|------|
| Remote 层 Codec 可插拔化 | ✅ | remote/remote_codec.go 实现 RemoteCodec 抽象层，endpoint.go 全量替换硬编码 JSON，支持 JSON/Protobuf/Binary 三种编解码，含完整测试（remote_codec_test.go） |
| 核心消息 Proto 定义 | ✅ | proto/ 目录含 remote.proto、system.proto、cluster.proto 三个定义文件 + messages.go（400+ 行）Go 消息实现 + encoding.go 辅助编解码 |
| Codegen 支持 Proto 输入 | ✅ | codegen/proto_parser.go（316 行）实现完整 proto3 解析器，msggen CLI 支持 `-proto` 参数，可生成 Go/TS/C#/文档/TypeRegistry 代码 |

**关键里程碑**：连续三版遗留的 Protobuf 全链路打通问题已彻底解决。

---

### 方向一：性能优化与生产打磨 — ⚠️ 基本完成（3.5/4）

| 需求项 | 状态 | 说明 |
|--------|------|------|
| Actor Pool 弹性伸缩 | ⚠️ | actor/actor_pool.go 实现 MinSize/MaxSize/ScaleThreshold 弹性伸缩 + IdleTimeout 空闲回收 + RoundRobin 路由，**但 MetricsRegistry 未接入**（Stats() 方法仅内部维护，未暴露给指标系统） |
| Mailbox 背压机制 | ✅ | actor/backpressure.go + mailbox_backpressure.go 完整实现 HighWatermark/LowWatermark + 四种策略（DropOldest/DropNewest/Block/Notify）+ EventStream 背压事件发布 |
| 消息批处理优化（本地层） | ✅ | actor/mailbox_batch.go 实现 BatchActor 接口 + BatchMailbox + props.go 提供 WithBatchMailbox(batchSize, batchTimeout) 便利配置 |
| 连接池与网络层优化 | ✅ | remote/health_check.go（Ping/Pong 健康检查）+ conn_pool.go（MinConns/MaxConns 动态扩缩容）+ retry_queue.go（消息重发队列，支持指数退避） |

---

### 方向二：集群增强 — ✅ 完全完成（含超额）

| 需求项 | 状态 | 说明 |
|--------|------|------|
| Split-Brain 检测与修复 | ✅ | cluster/splitbrain.go 实现 Quorum 投票检测 + 稳定窗口防误判；splitbrain_resolver.go 提供 KeepOldest/KeepMajority/ShutdownAll 三种策略 + 自定义 SplitBrainResolver 接口 |
| 集群单例（Cluster Singleton） | ✅ | cluster/singleton.go 实现基于一致性哈希的确定性选主 + 拓扑变更自动迁移 + Activated/Deactivated 事件 |
| 跨集群网关（Federation） | ✅ | cluster/federation/ 目录实现 ClusterGateway + FederatedMessage + 心跳探活 + `cluster://clusterID/actor_path` 联邦 PID 寻址（原 v1.6 优先级为"低"，已超额完成） |

---

### 方向三：游戏引擎深化 — ⚠️ 大部分完成（2.5/3）

| 需求项 | 状态 | 说明 |
|--------|------|------|
| AOI 优化 — 十字链表 + 灯塔 | ✅ | scene/aoi.go 定义统一 AOI 接口；aoi_crosslink.go 实现十字链表 AOI（密集移动场景优）；aoi_lighthouse.go 实现灯塔 AOI（大地图稀疏场景优）；grid.go 保留九宫格作为默认 |
| 寻路框架 | ✅ | scene/pathfinder.go 实现 Pathfinder 接口 + A* 算法（4/8 方向 + 权重 + 防穿角）+ PathCache LRU 缓存 + NavMesh 接口预留 |
| 数据表增强 | ⚠️ | config/schema.go 实现 Schema 校验（必填/类型/范围/正则/外键）✅；**Excel 直读仅有 Stub**（config/excel_stub.go 返回 "build with -tags xlsx" 错误），无实际 Excel 库集成 ❌ |

---

### 方向四：安全加固 — ✅ 完全完成

| 需求项 | 状态 | 说明 |
|--------|------|------|
| Gate 安全增强 | ✅ | gate/security.go 责任链框架 + security_ip_limit.go（IP 限流）+ security_msg_validator.go（消息校验 + 白名单）+ security_token.go（Token 验证）+ security_replay.go（防重放）+ security_anomaly.go（异常封禁）+ security_config.go（统一配置开关） |
| 敏感数据保护 | ✅ | log/redact.go（日志脱敏 + RedactingLogger）+ config/encrypt.go（AES-256-GCM 配置加密）+ dashboard/auth.go（Basic Auth + Bearer Token 鉴权）+ dashboard/audit.go（操作审计日志 + 来源 IP） |

---

### 方向五：开发效率工具 — ✅ 完全完成

| 需求项 | 状态 | 说明 |
|--------|------|------|
| 引擎 CLI 工具（engine-cli） | ✅ | cmd/engine/ 实现 5 个子命令：init（项目脚手架）、gen（多语言代码生成）、run（热重载开发模式）、dashboard（独立面板）、bench（基准测试报告） |
| 集成测试框架（TestKit） | ✅ | actor/testkit.go 实现 TestKit（隔离 ActorSystem + 自动清理）+ TestProbe（ExpectMsg/ExpectMsgType/ExpectNoMsg/IgnoreMsg/RequestAndExpect）+ BlackholeActor/EchoActor/ForwardActor 辅助 Actor |

---

## 二、当前构建与测试状态

```
构建状态：✅ go build ./... 通过（零错误）

测试状态（27 个包全部通过）：
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
✅ engine/internal                    — 通过
✅ engine/log                         — 通过
✅ engine/middleware                   — 通过
✅ engine/network                     — 通过
✅ engine/persistence                 — 通过
✅ engine/proto                       — 通过
✅ engine/pubsub                      — 通过
✅ engine/remote                      — 通过
✅ engine/router                      — 通过
✅ engine/scene                       — 通过
✅ engine/stress                      — 通过（63.5s）
✅ engine/timer                       — 通过

无测试文件（5 个非核心包）：
⚠️ engine/cmd/engine                 — CLI 工具入口，无测试
⚠️ engine/codegen/cmd/msggen         — 代码生成 CLI，无测试
⚠️ engine/codegen/cmd/msgversion     — 版本管理 CLI，无测试
⚠️ engine/example                    — 示例代码，无需测试
⚠️ engine/example/client             — 客户端示例，无需测试

❌ engine/better/.../mongodb          — MongoDB 未连接（参考实现，不计入）
```

---

## 三、v1.6 遗留问题（建议在 v1.7 解决）

### 遗留一：ActorPool MetricsRegistry 集成

**问题**：ActorPool 已实现弹性伸缩和 Stats() 统计接口，但未接入全局 MetricsRegistry（middleware/metrics_registry.go），指标仅内部维护。

**目标**：
- [ ] ActorPool 在创建时可选注入 MetricsRegistry
- [ ] 注册指标：`engine_pool_active_count`、`engine_pool_idle_count`、`engine_pool_scale_up_total`、`engine_pool_scale_down_total`
- [ ] checkScale() 和 RouteMessage() 中自动更新指标

**预期代码量**：~50-80 行

---

### 遗留二：Excel 直读支持

**问题**：config/excel_stub.go 仅有 Stub 实现（返回 "build with -tags xlsx" 错误），无实际 Excel 库集成。

**目标**：
- [ ] 添加可选依赖 `github.com/xuri/excelize/v2`（build tag: `xlsx`）
- [ ] 实现 `config/excel_impl.go`（`//go:build xlsx`），ExcelReader.Read() 读取 .xlsx 文件并填充 RecordFile
- [ ] 支持多 Sheet 读取（Sheet 名映射配置表名）

**预期代码量**：~150-200 行

---

## 四、v1.7 新增优化方向

### 方向一：引擎内核深度优化

#### 1.1 Actor 生命周期钩子扩展

**优先级**：🔴 高（Actor 行为定制化刚需）

**现状**：Actor 仅有 Started/Stopping/Stopped/Restarting 四种生命周期消息，缺少细粒度的扩展点。

**目标**：
- [ ] `PreStart` 钩子：Actor 启动前执行，可用于依赖检查、资源预分配
- [ ] `PostStop` 钩子：Actor 停止后执行，可用于资源清理确认、通知外部系统
- [ ] `PreRestart` / `PostRestart` 钩子：重启前后执行，可用于状态迁移
- [ ] Props 配置：`WithPreStart(fn)` / `WithPostStop(fn)` 函数式注入
- [ ] 钩子执行超时保护（防止钩子阻塞 Actor 生命周期）

**预期代码量**：~200-300 行

---

#### 1.2 Actor 优先级 Mailbox

**优先级**：🔴 高（关键消息不应被普通消息堆积阻塞）

**现状**：Mailbox 仅区分系统消息和用户消息两个队列，用户消息内部无优先级区分。

**目标**：
- [ ] 实现 PriorityMailbox，支持消息优先级分级（High/Normal/Low）
- [ ] 优先级判定接口：`MessagePrioritizer` — 根据消息类型返回优先级
- [ ] 多级队列：高优先级消息总是先于低优先级处理
- [ ] Props 配置：`WithPriorityMailbox(prioritizer)` 便利方法
- [ ] 与背压机制协同（低优先级消息优先被丢弃）

**预期代码量**：~200-300 行

---

#### 1.3 Actor Stash/Unstash 消息暂存

**优先级**：🟡 中（复杂状态机场景需要暂存消息）

**现状**：Actor 使用 BehaviorStack 实现状态切换，但无法在状态切换过程中暂存未处理消息。

**目标**：
- [ ] Context 增加 `Stash()` 方法：将当前消息放入暂存栈
- [ ] Context 增加 `UnstashAll()` 方法：将暂存栈中的消息重新投递到 Mailbox 头部
- [ ] 暂存栈容量限制（防止无限堆积）
- [ ] 与 Behavior 切换联动（UnbecomeStacked 时可自动 Unstash）

**使用场景示例**：
```go
// 玩家 Actor 在加载数据期间暂存所有业务消息
func (p *PlayerActor) Loading(ctx actor.Context) {
    switch ctx.Message().(type) {
    case *DataLoaded:
        ctx.UnstashAll()
        ctx.Become(p.Ready)
    default:
        ctx.Stash() // 暂存，数据加载完成后重新处理
    }
}
```

**预期代码量**：~150-200 行

---

#### 1.4 Dead Letter 增强

**优先级**：🟡 中（生产环境死信分析和告警）

**现状**：DeadLetter 仅记录日志，缺乏监控和自动响应能力。

**目标**：
- [ ] DeadLetter 接入 MetricsRegistry（`engine_deadletter_total` 计数器，按消息类型分类）
- [ ] DeadLetter 频率告警（单位时间内死信数超阈值触发 EventStream 事件）
- [ ] DeadLetter 持久化存储（可选，最近 N 条死信落库供排查）
- [ ] Dashboard 死信查询页面（最近死信列表 + 目标 Actor + 发送者）

**预期代码量**：~200-300 行

---

### 方向二：分布式能力增强

#### 2.1 分布式事务支持（Saga 模式）

**优先级**：🔴 高（跨 Actor 操作一致性是游戏后端核心痛点）

**目标**：
- [ ] Saga 协调器 Actor（SagaCoordinator）：编排多步操作
- [ ] Saga 步骤定义：`Step{Action, Compensate}` — 正向操作 + 补偿操作
- [ ] 执行策略：顺序执行、失败后反向补偿
- [ ] Saga 状态持久化（断电恢复后继续执行或补偿）
- [ ] 超时控制（每步超时 + 全局超时）
- [ ] Saga 执行日志（便于排查分布式一致性问题）

**使用场景**：
```
交易 Saga: 扣买家金币 → 加卖家金币 → 转移道具
  失败补偿: 回退道具 → 退卖家金币 → 退买家金币
```

**预期代码量**：~400-600 行

---

#### 2.2 Actor 消息持久化（Event Sourcing 可选）

**优先级**：🟡 中（关键 Actor 状态可恢复）

**现状**：persistence/ 模块实现了快照式持久化，但不支持事件溯源模式。

**目标**：
- [ ] EventSourced 接口：`Persist(event)` / `Recover(event)` / `Snapshot()`
- [ ] EventJournal 存储接口：顺序写入事件日志
- [ ] MemoryJournal 内存实现（开发测试用）
- [ ] 恢复流程：先加载最近快照，再重放快照之后的事件
- [ ] 快照策略：每 N 条事件自动创建快照（防止恢复时间过长）
- [ ] 与现有 PersistenceMiddleware 共存，不破坏现有 API

**预期代码量**：~400-500 行

---

#### 2.3 跨节点 Request-Response（分布式 Future）

**优先级**：🟡 中（当前 Remote 仅支持 Send，不支持跨节点 Request/Response）

**目标**：
- [ ] 远程 Request：发送消息到远程 Actor 并等待响应
- [ ] 分布式 Future：FutureProcess 支持远程回调地址
- [ ] 响应路由：远程 Actor Respond 时自动路由回发起节点
- [ ] 超时机制：远程 Request 超时后自动清理 Future
- [ ] 消息关联：RequestID 全局唯一，防止响应混淆

**预期代码量**：~300-400 行

---

### 方向三：游戏业务能力

#### 3.1 房间/匹配系统框架

**优先级**：🔴 高（多人游戏核心基础设施）

**目标**：
- [ ] RoomActor 抽象：房间生命周期管理（创建→等待→运行→结算→销毁）
- [ ] RoomManager Actor：房间创建、查找、列表、销毁
- [ ] 匹配器接口 `Matcher`：基于条件匹配玩家
- [ ] 内置匹配策略：
  - `EloMatcher` — ELO 分数范围匹配
  - `QueueMatcher` — 先到先得队列匹配
  - `ConditionMatcher` — 自定义条件匹配（等级、段位等）
- [ ] 匹配超时处理（超时后放宽条件或取消匹配）
- [ ] 匹配队列持久化（防止服务重启丢失排队玩家）

**预期代码量**：~500-700 行

---

#### 3.2 排行榜服务

**优先级**：🟡 中（游戏常见功能，适合作为引擎内置服务）

**目标**：
- [ ] LeaderboardActor：排行榜 Actor 抽象（可作为 Cluster Singleton 运行）
- [ ] 数据结构：跳表（SkipList）或有序集合，支持 O(log N) 插入和排名查询
- [ ] 核心操作：UpdateScore / GetRank / GetTopN / GetAroundMe
- [ ] 多维排行：支持同时按不同维度排序（总分、日排、周排）
- [ ] 定期快照持久化（防止排名数据丢失）
- [ ] 重置策略（日重置、周重置、赛季重置）

**预期代码量**：~400-500 行

---

#### 3.3 邮件/通知系统框架

**优先级**：🟡 中（游戏内玩家间异步通信基础设施）

**目标**：
- [ ] MailboxActor（游戏邮箱）：收发邮件、附件领取、已读标记、过期删除
- [ ] NotificationActor：系统通知推送（全服广播、个人通知）
- [ ] 离线消息队列：玩家下线期间的消息暂存，上线后推送
- [ ] 消息模板：支持参数化消息内容（如 "恭喜你在{活动名}中获得{奖品}"）
- [ ] 附件系统：邮件可携带道具/金币等物品

**预期代码量**：~400-500 行

---

#### 3.4 定时任务调度器增强

**优先级**：🟢 低（当前 timer/ 仅支持 AfterFunc/CronFunc，缺少分布式调度）

**目标**：
- [ ] 分布式定时任务：任务注册到集群，仅在一个节点执行（避免重复执行）
- [ ] 任务持久化：重启后自动恢复已注册任务
- [ ] 任务日志：执行结果记录（成功/失败/超时）
- [ ] 与 Cluster Singleton 协同：定时任务 Actor 作为集群单例运行

**预期代码量**：~300-400 行

---

### 方向四：可观测性与运维

#### 4.1 分布式追踪集成（OpenTelemetry）

**优先级**：🔴 高（生产环境跨节点问题排查必备）

**现状**：middleware/tracing.go 实现了本地 TraceContext/Span，但不支持导出到外部追踪系统。

**目标**：
- [ ] OpenTelemetry Span 适配器：将引擎内部 Span 转换为 OTel Span
- [ ] 上下文传播：Remote 消息自动携带 TraceContext（W3C Trace Context 格式）
- [ ] 导出器接口：支持 Jaeger/Zipkin/OTLP 后端
- [ ] Build Tag 隔离（`-tags otel`），默认构建不引入 OTel 依赖
- [ ] 采样策略配置（全量/比例/尾部采样）

**预期代码量**：~400-600 行

---

#### 4.2 Dashboard v3 — 实时监控大屏

**优先级**：🟡 中（提升运维可视化体验）

**目标**：
- [ ] WebSocket 实时推送（替代当前 5 秒轮询刷新）
- [ ] 消息流量实时折线图（按消息类型分组）
- [ ] 集群拓扑关系图（节点状态着色：绿/黄/红）
- [ ] Actor 消息热力图（哪些 Actor 消息最密集）
- [ ] 一键导出运行报告（JSON/CSV 格式）

**预期代码量**：~500-700 行

---

#### 4.3 健康检查端点（Liveness / Readiness）

**优先级**：🟡 中（K8s/Docker 部署标配）

**目标**：
- [ ] `/healthz` — Liveness 探针：进程存活检测
- [ ] `/readyz` — Readiness 探针：服务就绪检测（ActorSystem 启动完成 + Remote 连接就绪 + Cluster 成员加入）
- [ ] 自定义健康检查注册（业务层可插入自定义检查项）
- [ ] 健康状态聚合（所有检查项通过才返回 200）
- [ ] 与 Dashboard HTTP Server 复用端口

**预期代码量**：~150-200 行

---

### 方向五：开发体验与生态

#### 5.1 热更新框架

**优先级**：🔴 高（游戏运营期间不停服更新逻辑）

**目标**：
- [ ] Plugin 接口：定义可热更新的逻辑单元（Load/Reload/Unload）
- [ ] Go Plugin 方案（`plugin` 包）：编译为 .so 动态加载
- [ ] Lua 脚本方案（可选）：轻量级逻辑热更（适合战斗公式、活动规则）
- [ ] 版本管理：同时运行新旧版本，灰度切换
- [ ] 与 Dashboard 集成：在线上传和触发热更新
- [ ] 回滚机制：热更失败自动回退到上一版本

**预期代码量**：~400-600 行

---

#### 5.2 协议文档自动化

**优先级**：🟡 中（降低前后端对接成本）

**现状**：Codegen 可生成 Markdown 文档，但缺乏交互式文档和协议变更追踪。

**目标**：
- [ ] 生成 OpenAPI/Swagger 风格的协议文档（HTTP 可浏览）
- [ ] 协议变更日志自动生成（对比两版本消息定义，列出新增/删除/修改）
- [ ] 在线协议调试器（Dashboard 内嵌，输入消息 JSON → 发送到 Actor → 查看响应）
- [ ] 客户端 Mock Server（根据协议定义自动生成 Mock 响应，前端可独立开发）

**预期代码量**：~500-700 行

---

#### 5.3 示例项目 — 完整小游戏 Demo

**优先级**：🟢 低（但对引擎推广和新人上手极有价值）

**目标**：
- [ ] 一个可运行的多人在线小游戏 Demo（如简易 IO 游戏或卡牌对战）
- [ ] 展示引擎核心能力：Actor 模型、场景管理、AOI、匹配、房间、持久化
- [ ] 包含服务端 + TypeScript Web 客户端
- [ ] 支持单节点开发运行 + 多节点集群部署
- [ ] 附带详细注释和架构说明文档

**预期代码量**：~1000-1500 行（含客户端）

---

## 五、优先级排序与迭代计划

### 第一批（v1.7-alpha）— 遗留清零 + 内核增强

| 序号 | 任务 | 优先级 | 预期工作量 |
|------|------|--------|-----------|
| 1 | ActorPool MetricsRegistry 集成（v1.6 遗留） | 🔴 高 | ~70 行 |
| 2 | Excel 直读支持（v1.6 遗留） | 🟡 中 | ~180 行 |
| 3 | Actor 生命周期钩子扩展 | 🔴 高 | ~250 行 |
| 4 | Actor 优先级 Mailbox | 🔴 高 | ~250 行 |
| 5 | 分布式事务（Saga 模式） | 🔴 高 | ~500 行 |
| 6 | 房间/匹配系统框架 | 🔴 高 | ~600 行 |

### 第二批（v1.7-beta）— 分布式 + 可观测性

| 序号 | 任务 | 优先级 | 预期工作量 |
|------|------|--------|-----------|
| 7 | OpenTelemetry 追踪集成 | 🔴 高 | ~500 行 |
| 8 | 热更新框架 | 🔴 高 | ~500 行 |
| 9 | Actor Stash/Unstash | 🟡 中 | ~180 行 |
| 10 | 跨节点 Request-Response | 🟡 中 | ~350 行 |
| 11 | 健康检查端点 | 🟡 中 | ~180 行 |
| 12 | Dead Letter 增强 | 🟡 中 | ~250 行 |

### 第三批（v1.7-rc）— 游戏业务 + 生态

| 序号 | 任务 | 优先级 | 预期工作量 |
|------|------|--------|-----------|
| 13 | 排行榜服务 | 🟡 中 | ~450 行 |
| 14 | 邮件/通知系统 | 🟡 中 | ~450 行 |
| 15 | Event Sourcing 持久化 | 🟡 中 | ~450 行 |
| 16 | Dashboard v3 实时大屏 | 🟡 中 | ~600 行 |
| 17 | 协议文档自动化 | 🟡 中 | ~600 行 |
| 18 | 定时任务调度器增强 | 🟢 低 | ~350 行 |
| 19 | 示例项目 — 完整小游戏 | 🟢 低 | ~1200 行 |

### 总预期新增代码量：~7,500-9,000 行

---

## 六、技术决策要点

### 6.1 Saga 实现方案

**推荐**：编排式（Orchestrator）而非编舞式（Choreography）

```go
saga := NewSaga("trade").
    Step("deduct_buyer", deductAction, deductCompensate).
    Step("add_seller", addAction, addCompensate).
    Step("transfer_item", transferAction, transferCompensate).
    Build()

ctx.Send(sagaCoordinatorPID, &ExecuteSaga{Saga: saga, Context: tradeCtx})
```

**理由**：
- 编排式流程清晰、易于调试和监控
- 游戏事务通常步骤明确、顺序固定，适合编排模式
- 补偿操作可明确绑定到每一步，回滚路径确定

### 6.2 热更新技术选型

**推荐**：Go Plugin + Lua 双轨方案

| 方案 | 适用场景 | 特点 |
|------|---------|------|
| Go Plugin (.so) | 重逻辑变更（新系统、新功能） | 性能好，类型安全，但需重新编译 |
| Lua 脚本 | 轻逻辑调整（数值公式、活动规则） | 即时生效，无需编译，但性能稍低 |

**理由**：
- Go Plugin 适合大版本更新（如新增战斗系统）
- Lua 适合运营期快速调整（如修改掉落率、活动规则）
- 两者互补，覆盖不同热更场景

### 6.3 优先级 Mailbox 设计

**推荐**：三级队列 + 优先级判定接口

```go
type MessagePrioritizer interface {
    Priority(msg interface{}) MessagePriority // High / Normal / Low
}

// 处理顺序：SystemMessages > High > Normal > Low
```

**理由**：
- 三级足够覆盖绝大多数场景（心跳/关键业务 → 常规消息 → 低优日志类）
- 判定接口可插拔，业务层自定义优先级规则
- 与背压协同：背压触发时优先丢弃 Low 消息

### 6.4 房间系统设计

**推荐**：RoomActor 状态机 + RoomManager 注册中心

```
RoomManager (Cluster Singleton)
├── CreateRoom → spawn RoomActor
├── FindRoom → lookup by ID/condition
├── ListRooms → iterate active rooms
└── DestroyRoom → stop RoomActor

RoomActor 状态机：
  Waiting → (满员) → Running → (结算) → Finished → (销毁) → Stopped
```

**理由**：
- RoomActor 天然契合 Actor 模型（每个房间一个 Actor，独立状态）
- RoomManager 作为 Cluster Singleton 保证全局唯一注册中心
- 状态机模式（BehaviorStack）清晰表达房间生命周期

### 6.5 OpenTelemetry 集成策略

**推荐**：Build Tag 隔离（`-tags otel`）+ 接口适配

**理由**：
- OTel SDK 是较重依赖（~20+ 包），默认不引入
- 通过 Adapter 模式将引擎内部 Span 转为 OTel Span
- 无 tag 时 tracing 仍工作（仅本地 TraceID 传播），不影响功能

---

## 七、风险评估

| 风险 | 影响 | 应对 |
|------|------|------|
| Saga 补偿操作失败 | 数据永久不一致 | 补偿重试 + 人工干预告警 + Saga 日志审计 |
| Go Plugin 跨版本兼容 | 插件加载失败 | Plugin 接口版本号校验 + 自动回滚 |
| Lua 脚本安全性 | 恶意脚本破坏系统 | Lua 沙箱限制（禁止 IO/OS 调用）+ 执行超时 |
| 优先级 Mailbox 饥饿 | 低优先级消息永远得不到处理 | 饥饿检测：低优消息等待超阈值后提升优先级 |
| OTel 依赖更新频繁 | 编译问题 | 固定 OTel SDK 版本 + CI 每周验证 |
| 房间系统集群一致性 | 玩家被路由到不存在的房间 | RoomManager Singleton + 房间 PID 缓存 + 重试 |
| 完整 Demo 维护成本 | Demo 与引擎版本脱节 | CI 集成 Demo 编译测试，引擎变更时同步更新 |

---

## 八、v1.3 → v1.7 演进总览

```
v1.3（架构奠基）
├── Phase 1-4 核心功能全部完成
├── Actor 引擎 + 分布式 + 集群 + 游戏层
└── 初始架构代码

v1.4（补齐加固）— ✅ 完成
├── WebSocket Gate ✅
├── Protobuf Codec 框架 ✅
├── 配置热重载 ✅
├── 测试/基准测试补齐 ✅
├── 错误处理规范化 ✅
├── Dashboard 增强 ✅
└── AllForOne 监管策略 + 外部服务发现（超额） ✅

v1.5（生产就绪）— ✅ 完成
├── Graceful Shutdown + 信号处理 ✅
├── 消息追踪与链路 ID ✅
├── Rate Limiter + 场景转移 + ECS 调度器 ✅
├── 结构化日志 + Prometheus 指标 ✅
├── 客户端 SDK 增强（TS/C#/文档） ✅
├── 消息版本兼容 ✅
└── Gate 握手 + ECS-Actor 融合（超额） ✅

v1.6（深度优化）— ✅ 基本完成（95%+）
├── Protobuf 全链路打通（三版遗留清零） ✅
├── Actor Pool 弹性伸缩 + Mailbox 背压 + 批处理 ✅
├── Split-Brain 检测 + 集群单例 + 跨集群网关 ✅
├── AOI 多算法（十字链表/灯塔）+ A* 寻路 ✅
├── Gate 安全（IP限流/Token/防重放/异常封禁） ✅
├── 敏感数据保护（日志脱敏/配置加密/Dashboard鉴权） ✅
├── engine-cli 5 个子命令 + TestKit 测试框架 ✅
├── 连接池动态扩缩 + 健康检查 + 消息重发 ✅
├── Schema 校验 ✅
└── 遗留：ActorPool 指标集成 ⚠️、Excel 直读 ⚠️

v1.7（业务能力 + 生态）— 规划中
├── 内核：生命周期钩子 + 优先级 Mailbox + Stash/Unstash + Dead Letter 增强
├── 分布式：Saga 事务 + Event Sourcing + 跨节点 Request-Response
├── 游戏业务：房间/匹配 + 排行榜 + 邮件通知 + 分布式定时任务
├── 可观测：OpenTelemetry + Dashboard v3 + 健康检查
├── 生态：热更新框架 + 协议文档自动化 + 完整小游戏 Demo
└── 目标：具备完整游戏业务能力的生产级引擎
```

---

## 九、v1.6 完成度总结

| 分类 | 完成 | 未完成 | 完成度 |
|------|------|--------|--------|
| v1.4/v1.5 遗留项 | 3/3 | 0 | 100% |
| 方向一：性能优化 | 3.5/4 | 0.5 | 87.5% |
| 方向二：集群增强 | 3/3 | 0 | 100% |
| 方向三：游戏引擎深化 | 2.5/3 | 0.5 | 83.3% |
| 方向四：安全加固 | 2/2 | 0 | 100% |
| 方向五：开发效率 | 2/2 | 0 | 100% |
| **总计** | **16/17** | **1** | **94.1%** |

**关键结论**：
- v1.6 规划的 17 项需求中完成 16 项（94.1%），另有跨集群网关超额完成
- 遗留两个小项：ActorPool 指标集成（~70 行）+ Excel 直读（~180 行），均为低风险项
- 连续三版遗留的 Protobuf 全链路问题已彻底解决
- 构建零错误，27 个测试包全部通过
- 代码量从 v1.5 的 ~24,100 行增长到 ~34,400 行（+42.7%）

---

*文档版本：v1.7*
*生成时间：2026-04-08*
*基于 v1.6 需求审核生成*
*当前代码量：~34,400 行 Go 代码（不含 better/ 参考实现，249 个文件）*
