# 问鼎天下 v1.6 优化计划

> 基于 v1.5 需求审核，识别遗留项与下一阶段迭代方向
> 审核日期：2026-04-03
> 当前代码量：~24,100 行 Go 代码（不含 better/ 参考实现）

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

## 一、v1.5 需求完成度审核

### v1.4 遗留项

| 需求项 | 状态 | 说明 |
|--------|------|------|
| Remote 层 Codec 可插拔化 | ❌ | remote/remote.go 和 remote/endpoint.go 仍硬编码 json.Marshal/Unmarshal（18处），未集成 Codec 接口 |
| 核心消息 Proto 定义 | ❌ | 项目中无 proto/ 目录，无 .proto 文件定义 |
| Codegen 支持 Proto 输入 | ❌ | codegen/ 仅支持 Go 源文件解析，不支持 .proto 输入 |

### 方向一：生产就绪加固 — ✅ 完全完成

| 需求项 | 状态 | 说明 |
|--------|------|------|
| 新增模块测试覆盖 | ✅ | errors/、console/、cluster/provider/*、log/ 均有完整测试 |
| Graceful Shutdown | ✅ | actor/shutdown.go + actor/signal.go，Shutdown()/ShutdownWithConfig() + SIGTERM/SIGINT 信号处理 |
| 消息追踪与链路 ID | ✅ | Envelope 含 TraceID 字段，middleware/tracing.go 实现 TraceContext/Span 机制 |
| Rate Limiter | ✅ | middleware/ratelimit.go 实现令牌桶限流，支持 per-Actor/per-Connection 配置 |

### 方向二：游戏引擎层增强 — ✅ 完全完成

| 需求项 | 状态 | 说明 |
|--------|------|------|
| 场景转移与跨场景通信 | ✅ | scene/transfer.go 实现 TransferEntity + 状态序列化 + 转移失败回滚 |
| ECS 系统调度器 | ✅ | ecs/system.go（System 接口 + SystemGroup 有序调度 + 并行）+ ecs/ticker.go（固定帧率驱动） |
| 战斗/技能框架抽象 | ✅ | ecs/combat.go（Attack/Defense/Buff/SkillState 组件）+ example/combat_example.go |

### 方向三：DevOps 与可观测性 — ✅ 完全完成

| 需求项 | 状态 | 说明 |
|--------|------|------|
| 结构化日志体系 | ✅ | log/logger.go（Logger 接口）+ json_handler.go + text_handler.go + slog_adapter.go，支持 slog 集成 |
| Prometheus 指标导出 | ✅ | middleware/metrics_registry.go 实现 Counter/Gauge + Prometheus text 格式导出，零外部依赖 |
| Dashboard v2 增强 | ✅ | dashboard/metrics_history.go（5分钟趋势）+ dashboard/audit.go（操作审计日志） |

### 方向四：协议与互操作 — ✅ 完全完成

| 需求项 | 状态 | 说明 |
|--------|------|------|
| 客户端 SDK 生成增强 | ✅ | codegen/templates_csharp.go（C# 类型生成）+ templates_sdk.go（TS SDK 含 WebSocket + 自动重连）+ templates_doc.go（Markdown API 文档）|
| 消息版本兼容 | ✅ | codegen/version.go（版本清单 + 差异对比）+ migration.go（兼容性检查 + 迁移工具） |

### 超额完成项（v1.5 未规划但已实现）

| 需求项 | 状态 | 说明 |
|--------|------|------|
| Gate 握手协议与版本协商 | ✅ | gate/handshake.go 实现 HandshakeRequest/Response + VersionNegotiator |
| ECS-Actor 融合层 | ✅ | ecs/scene_integration.go 实现 SceneWorld + ECSSceneActor，Timer 驱动帧循环 |
| Go↔C# 类型映射 | ✅ | codegen/type_mapping.go 完整的 Go→C#/TS/JSON 类型转换 |
| 消息版本 CLI 工具 | ✅ | codegen/cmd/msgversion/ 版本管理命令行工具 |

---

## 二、当前构建与测试状态

```
构建状态：✅ go build ./... 通过（零错误）

测试状态（25 个包全部通过）：
✅ engine/actor           — 通过
✅ engine/cluster          — 通过
✅ engine/cluster/provider/consul — 通过
✅ engine/cluster/provider/etcd   — 通过
✅ engine/cluster/provider/k8s    — 通过
✅ engine/codec            — 通过
✅ engine/codegen          — 通过
✅ engine/config           — 通过
✅ engine/console          — 通过
✅ engine/dashboard        — 通过
✅ engine/ecs              — 通过
✅ engine/errors           — 通过
✅ engine/gate             — 通过
✅ engine/grain            — 通过
✅ engine/internal         — 通过
✅ engine/log              — 通过
✅ engine/middleware        — 通过
✅ engine/network          — 通过
✅ engine/persistence      — 通过
✅ engine/pubsub           — 通过
✅ engine/remote           — 通过
✅ engine/router           — 通过
✅ engine/scene            — 通过
✅ engine/stress           — 通过
✅ engine/timer            — 通过

❌ engine/better/.../mongodb — MongoDB 未连接（参考实现，不计入）
```

---

## 三、v1.5 遗留问题（必须在 v1.6 解决）

### 遗留一：Remote 层 Codec 可插拔化

**问题**：Remote 层序列化仍硬编码 `json.Marshal/json.Unmarshal`（18处调用），与 codec/ 包的 Codec 接口完全未打通。

**现状代码位置**：
- `remote/remote.go` — 消息编解码硬编码 JSON
- `remote/endpoint.go` — 批量发送硬编码 JSON

**目标**：
- [ ] Remote 层引入 Codec 接口抽象，替换所有硬编码 JSON 调用
- [ ] RemoteConfig 增加 Codec 配置项（默认 JSON，可选 Protobuf）
- [ ] 端点握手阶段声明编解码方式，支持混合部署
- [ ] 保持向后兼容（JSON 仍为默认值）

**预期代码量**：~200-300 行

---

### 遗留二：核心消息 Proto 定义

**问题**：ProtobufCodec 框架已实现，但项目无 `.proto` 文件定义核心消息结构，Protobuf 编解码无法实际投入使用。

**目标**：
- [ ] 创建 `proto/` 目录，定义核心消息 .proto 文件：
  - `remote.proto` — RemoteMessage、RemoteMessageBatch
  - `system.proto` — Started、Stopping、Stopped、Restarting、Watch、Unwatch
  - `cluster.proto` — GossipState、MemberStatus、ClusterTopologyEvent
- [ ] 使用 `protoc` 生成对应 Go 代码
- [ ] Remote 层使用 Protobuf 消息结构（配合遗留一的 Codec 可插拔化）

**预期代码量**：~300-500 行（含 .proto 定义 + 生成代码）

---

### 遗留三：Codegen 支持 Proto 输入

**问题**：Codegen 仅支持从 Go 源文件（`//msggen:message`）生成代码，不支持 `.proto` 文件作为输入源。

**目标**：
- [ ] Codegen 增加 `.proto` 文件解析能力（parseProtoFile）
- [ ] 从 .proto 消息定义生成 TypeRegistry 注册代码
- [ ] 从 .proto 生成 TypeScript / C# 类型定义（与现有生成对齐）
- [ ] msggen CLI 增加 `-proto` 参数

**预期代码量**：~300-400 行

---

## 四、v1.6 新增优化方向

### 方向一：性能优化与生产打磨

#### 1.1 Actor Pool（Actor 池化）

**优先级**：🔴 高（高并发场景下 Actor 创建/销毁开销大）

**现状**：actor/pool.go 存在但功能有限，缺乏弹性伸缩和自动回收机制。

**目标**：
- [ ] Actor Pool 支持弹性伸缩（MinSize / MaxSize / ScaleThreshold）
- [ ] 空闲 Actor 自动回收（IdleTimeout 超时后缩容）
- [ ] Pool Router 集成（Pool 内部自动 RoundRobin 分发）
- [ ] Pool 指标接入 MetricsRegistry（活跃数、等待数、创建/销毁计数）
- [ ] 与 Grain 虚拟 Actor 的 Pool 化协同（Pool 中的 Grain 去激活管理）

**预期代码量**：~300-400 行

---

#### 1.2 Mailbox 背压机制

**优先级**：🔴 高（防止 Actor 消息堆积导致内存溢出）

**目标**：
- [ ] Mailbox 高水位标记（HighWatermark），达到阈值触发背压
- [ ] 背压策略可配置：
  - `DropOldest` — 丢弃最旧消息
  - `DropNewest` — 丢弃新到消息
  - `Block` — 阻塞发送方
  - `Notify` — 发送 MailboxOverflow 系统消息通知 Actor
- [ ] 背压事件接入 EventStream（可全局监听 Mailbox 压力）
- [ ] Dashboard 展示 Mailbox 水位趋势

**预期代码量**：~200-300 行

---

#### 1.3 消息批处理优化

**优先级**：🟡 中（Remote 层已有批处理，本地层缺失）

**目标**：
- [ ] 本地 Actor 间消息批处理（BatchMailbox 模式：攒够 N 条或超时后批量投递）
- [ ] 批处理 Actor 接口 `BatchReceive(messages []interface{})` 
- [ ] 适用场景：DB 写入合并、日志聚合、指标汇总
- [ ] 可通过 Props 配置启用（`WithBatchSize(n)` / `WithBatchTimeout(d)`）

**预期代码量**：~200-300 行

---

#### 1.4 连接池与网络层优化

**优先级**：🟡 中（长连接场景下的稳定性）

**目标**：
- [ ] Remote 连接健康检查（定期 Ping，检测半开连接）
- [ ] 连接池动态扩缩容（根据流量自适应）
- [ ] 网络层 Zero-Copy 优化（减少消息传输中的内存拷贝）
- [ ] 连接断开后消息重发队列（At-Least-Once 语义可选）

**预期代码量**：~300-400 行

---

### 方向二：集群增强

#### 2.1 Split-Brain 检测与自动修复

**优先级**：🔴 高（分布式集群生产环境最危险的问题）

**目标**：
- [ ] Split-Brain 检测算法（基于 Quorum 投票）
- [ ] 脑裂恢复策略：
  - `KeepOldest` — 保留运行最久的分区
  - `KeepMajority` — 保留成员最多的分区
  - `ShutdownAll` — 全部关闭（最安全）
- [ ] 自定义 SplitBrainResolver 接口
- [ ] 脑裂事件通知（SplitBrainDetected / SplitBrainResolved）
- [ ] Dashboard 展示脑裂状态

**预期代码量**：~400-600 行

---

#### 2.2 集群单例（Cluster Singleton）

**优先级**：🟡 中（全局唯一 Actor 场景，如排行榜、全服广播）

**目标**：
- [ ] 集群内保证某种 Actor 只有一个实例运行
- [ ] 节点下线时自动迁移到其他节点
- [ ] 基于 Grain 机制扩展实现（Kind="singleton:xxx"）
- [ ] Leader Election 算法（基于 Gossip 状态的确定性选主）

**预期代码量**：~200-300 行

---

#### 2.3 跨集群网关（Federation）

**优先级**：🟢 低（多集群互联场景，如跨服战）

**目标**：
- [ ] 集群间消息转发网关（ClusterGateway Actor）
- [ ] 集群注册表（ClusterID → 网关地址映射）
- [ ] 跨集群 PID 寻址（`cluster://clusterB/actor/xxx`）
- [ ] 跨集群消息路由（透明转发）

**预期代码量**：~500-700 行

---

### 方向三：游戏引擎深化

#### 3.1 AOI 优化 — 十字链表 + 灯塔混合方案

**优先级**：🟡 中（大规模场景下九宫格 AOI 性能不足）

**现状**：scene/ 使用九宫格 Grid 空间索引，适合中小规模场景。

**目标**：
- [ ] 实现十字链表 AOI（CrossLinkedList），适合实体密集型场景
- [ ] 灯塔方案（Lighthouse），适合大地图稀疏分布场景
- [ ] AOI 策略可配置，通过 SceneConfig 选择算法
- [ ] 统一 AOI 查询接口（GetNearby / OnEnter / OnLeave）
- [ ] 基准测试对比三种方案在不同实体密度下的性能

**预期代码量**：~500-700 行

---

#### 3.2 寻路框架接入

**优先级**：🟡 中（MMO/ARPG 核心功能）

**目标**：
- [ ] Pathfinder 接口抽象（FindPath(from, to) → []Point）
- [ ] A* 算法实现（Grid-based，支持障碍物和权重）
- [ ] NavMesh 数据结构预留（3D 寻路，后续扩展）
- [ ] 与 ECS MovementSystem 集成（寻路结果驱动移动组件）
- [ ] 路径缓存（相同起终点复用路径）

**预期代码量**：~400-600 行

---

#### 3.3 数据表增强 — 多格式支持与校验

**优先级**：🟢 低（提升策划表导入体验）

**现状**：config/ 仅支持 TSV（RecordFile）和 JSON。

**目标**：
- [ ] 支持 Excel 直读（`.xlsx`，可选依赖，build tag 隔离）
- [ ] 数据表 Schema 校验（定义字段类型、范围、外键引用）
- [ ] 配置表变更 Diff 工具（对比两版本配置差异）
- [ ] 配置表预编译（启动时转为二进制缓存，加速加载）

**预期代码量**：~400-500 行

---

### 方向四：安全加固

#### 4.1 Gate 安全增强

**优先级**：🔴 高（面向公网的客户端入口必须加固）

**目标**：
- [ ] 连接频率限制（同一 IP 每秒最大连接数）
- [ ] 消息合法性校验（消息 ID 白名单、包大小上限）
- [ ] 登录态验证中间件（Token 校验 + 过期检测）
- [ ] 防重放攻击（消息序列号 + 时间戳窗口）
- [ ] 异常连接自动踢出（短时间大量非法消息 → 封禁）

**预期代码量**：~300-500 行

---

#### 4.2 敏感数据保护

**优先级**：🟡 中（合规要求）

**目标**：
- [ ] 日志脱敏（Logger 支持 Redact 标记，自动遮蔽敏感字段）
- [ ] 配置加密存储（数据库密码、API Key 等配置项支持加密）
- [ ] Dashboard 访问鉴权（HTTP Basic Auth / Token）
- [ ] 审计日志增强（记录配置变更操作人 + 来源 IP）

**预期代码量**：~300-400 行

---

### 方向五：开发效率工具

#### 5.1 引擎 CLI 工具（engine-cli）

**优先级**：🟡 中（标准化开发流程）

**目标**：
- [ ] `engine init` — 初始化项目脚手架（目录结构 + go.mod + 示例代码）
- [ ] `engine gen` — 统一代码生成入口（消息注册 + SDK + Proto）
- [ ] `engine run` — 带热重载的开发模式（文件变更自动重启）
- [ ] `engine dashboard` — 独立启动 Dashboard（不依赖 ActorSystem）
- [ ] `engine bench` — 一键运行全部基准测试并生成报告

**预期代码量**：~500-700 行

---

#### 5.2 集成测试框架

**优先级**：🟡 中（端到端测试太难写）

**目标**：
- [ ] TestKit — Actor 测试工具包（类似 Akka TestKit）
  - `TestProbe` — 测试用 Actor，可断言接收到的消息
  - `ExpectMsg(timeout)` — 等待并断言消息
  - `ExpectNoMsg(duration)` — 断言无消息
  - `IgnoreMsg(filter)` — 过滤不关心的消息
- [ ] `TestActorSystem` — 轻量测试用 ActorSystem（内存化，快速启停）
- [ ] 场景测试辅助（快速创建 Grid + 实体 + AOI 验证）

**预期代码量**：~400-500 行

---

## 五、优先级排序与迭代计划

### 第一批（v1.6-alpha）— 遗留清零 + 安全加固

| 序号 | 任务 | 优先级 | 预期工作量 |
|------|------|--------|-----------|
| 1 | Remote 层 Codec 可插拔化 | 🔴 高 | ~250 行 |
| 2 | 核心消息 Proto 定义 | 🔴 高 | ~400 行 |
| 3 | Gate 安全增强 | 🔴 高 | ~400 行 |
| 4 | Split-Brain 检测与修复 | 🔴 高 | ~500 行 |
| 5 | Mailbox 背压机制 | 🔴 高 | ~250 行 |

### 第二批（v1.6-beta）— 性能优化 + 游戏引擎

| 序号 | 任务 | 优先级 | 预期工作量 |
|------|------|--------|-----------|
| 6 | Actor Pool 弹性伸缩 | 🔴 高 | ~350 行 |
| 7 | AOI 十字链表/灯塔方案 | 🟡 中 | ~600 行 |
| 8 | 寻路框架（A* 实现） | 🟡 中 | ~500 行 |
| 9 | 集群单例 | 🟡 中 | ~250 行 |
| 10 | 集成测试框架（TestKit） | 🟡 中 | ~450 行 |

### 第三批（v1.6-rc）— 工具链 + 扩展

| 序号 | 任务 | 优先级 | 预期工作量 |
|------|------|--------|-----------|
| 11 | Codegen 支持 Proto 输入 | 🟡 中 | ~350 行 |
| 12 | 连接池与网络层优化 | 🟡 中 | ~350 行 |
| 13 | 消息批处理优化 | 🟡 中 | ~250 行 |
| 14 | 敏感数据保护 | 🟡 中 | ~350 行 |
| 15 | 引擎 CLI 工具 | 🟡 中 | ~600 行 |
| 16 | 数据表增强 | 🟢 低 | ~450 行 |
| 17 | 跨集群网关（Federation） | 🟢 低 | ~600 行 |

### 总预期新增代码量：~6,000-7,500 行

---

## 六、技术决策要点

### 6.1 Remote Codec 切换策略

**推荐**：接口抽象 + 配置注入（最小改动方案）

```go
// RemoteConfig 新增 Codec 配置
type RemoteConfig struct {
    // ...existing fields...
    Codec codec.Codec  // nil 使用 JSON 兜底
}

// endpoint.go 中替换硬编码调用
func (e *Endpoint) serialize(msg interface{}) ([]byte, error) {
    if e.config.Codec != nil {
        return e.config.Codec.Marshal(msg)
    }
    return json.Marshal(msg)
}
```

**理由**：
- 仅需修改 remote/remote.go 和 remote/endpoint.go 中的序列化调用点
- 默认行为不变（JSON），零破坏性
- 端点握手阶段声明编解码方式，允许渐进式升级

### 6.2 Split-Brain 检测方案

**推荐**：Quorum + Phi Accrual Failure Detector

**理由**：
- Phi Accrual 比固定超时更适应网络抖动（Akka/Cassandra 验证过）
- Quorum 投票天然契合 Gossip 协议的状态同步机制
- 恢复策略做成可插拔接口，不同业务选择不同策略

### 6.3 Mailbox 背压模型

**推荐**：高水位标记 + 策略模式

```go
type MailboxConfig struct {
    HighWatermark int                // 触发背压的阈值，0 = 不启用
    Strategy      BackpressureStrategy // DropOldest / DropNewest / Block / Notify
}
```

**理由**：
- 与现有 Mailbox 实现兼容（只是在投递前多一次长度检查）
- 策略模式可扩展，不同 Actor 使用不同策略
- Notify 模式最推荐：Actor 自己决定如何降级，引擎不做强制丢弃

### 6.4 AOI 算法选型

**推荐**：保留九宫格作为默认，新增十字链表作为可选

**理由**：
- 九宫格实现简单、内存紧凑，在实体数 < 1000 的场景下性能最优
- 十字链表在实体频繁移动的大规模场景下（MMORPG 主城）更优
- 灯塔方案适合超大地图低密度（开放世界），优先级最低
- 通过接口统一，业务层无感知切换

### 6.5 Gate 安全分层

**推荐**：责任链模式，每层独立启用/禁用

```
连接 → IP 限流 → 握手校验 → Token 验证 → 消息合法性 → 业务处理
```

**理由**：
- 每一层都可以通过配置开关独立启停
- 开发环境可关闭所有安全层（方便调试）
- 生产环境逐层启用（灰度上线安全策略）

---

## 七、风险评估

| 风险 | 影响 | 应对 |
|------|------|------|
| Remote Codec 切换后新旧节点不兼容 | 混合部署时通信失败 | 握手阶段 Codec 协商 + JSON 兜底 |
| Split-Brain 检测误判 | 正常节点被错误隔离 | Phi Accrual 自适应阈值 + 恢复窗口期 |
| Mailbox 背压丢消息 | 关键消息丢失 | 系统消息不受背压限制；Notify 模式不丢消息 |
| AOI 算法切换引入 bug | 玩家可见性异常 | 新旧算法并行运行、比对结果，通过后再切换 |
| Gate 安全层增加延迟 | 登录/消息处理变慢 | IP 限流和消息校验都是 O(1) 操作；Token 校验做本地缓存 |
| Proto 依赖引入工具链复杂度 | CI/CD 需安装 protoc | Makefile 封装 + 预生成 .pb.go 提交到仓库 |
| TestKit 设计不当 | 测试脆弱/难维护 | 参考 Akka TestKit 成熟设计，先实现最小核心 |

---

## 八、v1.3 → v1.4 → v1.5 → v1.6 演进总览

```
v1.3（架构奠基）
├── Phase 1-4 核心功能全部完成
├── Actor 引擎 + 分布式 + 集群 + 游戏层
└── 初始架构代码

v1.4（补齐加固）— ✅ 基本完成
├── WebSocket Gate ✅
├── Protobuf Codec 框架 ✅（Remote 集成遗留）
├── 配置热重载 ✅
├── MongoStorage 完善 ✅
├── 测试/基准测试补齐 ✅
├── 错误处理规范化 ✅
├── Dashboard 增强 ✅
├── AllForOne 监管策略 ✅
├── 外部服务发现（超额） ✅
└── 遗留：Remote 层仍硬编码 JSON

v1.5（生产就绪）— ✅ 大部分完成（12/15 完成，80%）
├── Graceful Shutdown + 信号处理 ✅
├── 消息追踪与链路 ID ✅
├── Rate Limiter 限流 ✅
├── 场景转移与跨场景通信 ✅
├── ECS 系统调度器 + Scene 融合 ✅
├── 战斗/技能框架 ✅
├── 结构化日志体系 ✅
├── Prometheus 指标导出 ✅
├── Dashboard v2 增强 ✅
├── 客户端 SDK 增强（TS/C#/文档） ✅
├── 消息版本兼容 ✅
├── Gate 握手协议（超额） ✅
├── ECS-Actor 融合层（超额） ✅
└── 遗留：Remote Codec 可插拔化 ❌、Proto 定义 ❌、Codegen Proto ❌

v1.6（深度优化）— 规划中
├── 彻底打通 Remote + Protobuf 全链路（三个遗留项清零）
├── 性能优化：Actor Pool 弹性伸缩 + Mailbox 背压 + 批处理
├── 集群增强：Split-Brain 检测 + 集群单例
├── 游戏深化：AOI 多算法 + 寻路框架
├── 安全加固：Gate 安全 + 数据保护
├── 工具链：engine-cli + TestKit
└── 目标：高性能、高可靠、易用的生产级游戏引擎
```

---

## 九、v1.5 完成度总结

| 分类 | 完成 | 未完成 | 完成度 |
|------|------|--------|--------|
| v1.4 遗留项 | 0 | 3 | 0% |
| 方向一：生产就绪 | 4/4 | 0 | 100% |
| 方向二：游戏引擎 | 3/3 | 0 | 100% |
| 方向三：DevOps | 3/3 | 0 | 100% |
| 方向四：协议互操作 | 2/2 | 0 | 100% |
| 超额完成 | 4 | - | - |
| **总计** | **12+4** | **3** | **80%** |

**关键结论**：v1.5 规划的 15 项需求中完成 12 项（80%），另有 4 项超额完成。未完成的 3 项均为 Protobuf 全链路打通相关，已连续两版遗留，v1.6 必须优先清零。

---

*文档版本：v1.6*
*生成时间：2026-04-03*
*基于 v1.5 需求审核生成*
*当前代码量：~24,100 行 Go 代码（不含 better/ 参考实现）*
