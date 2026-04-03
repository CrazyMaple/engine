# 问鼎天下 v1.5 优化计划

> 基于 v1.4 需求审核，识别遗留项与下一阶段迭代方向
> 审核日期：2026-04-02
> 当前代码量：~67,000+ 行 Go 代码（不含 better/ 参考实现）

---

## 〇、项目约定

### better/ 目录规则

**`better/` 为参考实现目录（vendored Leaf 和 ProtoActor 源码），遵循以下规则：**

1. **不参与项目编译**：`better/` 下的代码不直接编译进引擎产物，仅作为设计参考和对比基准
2. **不参与需求审核**：审核功能完成度时，不将 `better/` 下的代码计入已完成项
3. **不参与测试统计**：`better/` 下的测试失败（如 MongoDB 未连接）不影响引擎整体测试状态评估
4. **不计入代码量**：统计项目代码量时排除 `better/` 目录
5. **只读参考**：开发中可查阅 `better/` 下的实现思路，但新代码应在引擎对应模块中独立实现，不得直接复制或 import

---

## 一、v1.4 需求完成度审核

### 方向一：补齐功能短板

| 需求项 | 状态 | 说明 |
|--------|------|------|
| WebSocket Gate 接入 | ✅ | gate/ 支持 TCP/WS 并行，network/ws_server.go + ws_conn.go 完整实现，支持 wss://（TLS）、Ping-Pong 心跳保活 |
| Protobuf 编解码支持 | ⚠️ | codec/protobuf.go 已实现 ProtobufCodec 框架，**但 Remote 层仍硬编码 JSON**，未集成可插拔 Codec；无 .proto 文件定义；Codegen 不支持 .proto |
| 配置热重载 | ✅ | config/manager.go 实现 StartWatch/StopWatch，文件轮询 + OnReload 回调 + 原子替换 |
| MongoStorage 完善 | ✅ | 连接池（PoolLimit 默认100）、重试机制（MaxRetries 默认3）、批量操作（SaveBatch/LoadBatch）均已实现 |

### 方向二：质量加固

| 需求项 | 状态 | 说明 |
|--------|------|------|
| 测试覆盖补齐 | ✅ | gate/gate_test.go、timer/timer_test.go、codec/codec_test.go、remote/remote_test.go 均已覆盖 |
| 修复压力测试 | ✅ | stress/ 4个测试全部通过（63.8s），包括 TestStressClusterNodeFailure |
| 基准测试套件 | ✅ | actor/actor_bench_test.go、internal/queue_bench_test.go、scene/grid_bench_test.go、codec/codec_bench_test.go、remote/remote_bench_test.go |
| 错误处理规范化 | ✅ | errors/ 包已建立，定义 ConnectError/TimeoutError/AuthError/ClusterError/CodecError 等统一类型 |

### 方向三：开发体验优化

| 需求项 | 状态 | 说明 |
|--------|------|------|
| Dashboard 增强 | ✅ | 嵌入式 Web UI，运行时指标、Actor 拓扑、热点分析、5秒自动刷新 |
| 示例与文档完善 | ✅ | example/ 目录含10个示例（websocket、cluster、grain、pubsub、persistence、middleware 等） |
| AllForOne 监管策略 | ✅ | actor/supervision.go 实现 AllForOneStrategy |

### 超额完成项（v1.4 未规划但已实现）

| 需求项 | 状态 | 说明 |
|--------|------|------|
| 外部服务发现 | ✅ | cluster/provider/ 实现了 Consul、etcd、K8s 三种 Provider |

---

## 二、当前构建与测试状态

```
构建状态：✅ go build ./... 通过（零错误）

测试状态：
✅ engine/actor           — 通过
✅ engine/cluster          — 通过
✅ engine/codec            — 通过
✅ engine/codegen          — 通过
✅ engine/config           — 通过
✅ engine/dashboard        — 通过
✅ engine/ecs              — 通过
✅ engine/gate             — 通过
✅ engine/grain            — 通过
✅ engine/internal         — 通过
✅ engine/middleware        — 通过
✅ engine/network          — 通过
✅ engine/persistence      — 通过
✅ engine/pubsub           — 通过
✅ engine/remote           — 通过
✅ engine/router           — 通过
✅ engine/scene            — 通过
✅ engine/stress           — 通过（63.8s）
✅ engine/timer            — 通过
⚠️ engine/errors           — 无测试文件
⚠️ engine/console          — 无测试文件
⚠️ engine/cluster/provider/* — 无测试文件（Consul/etcd/K8s）
⚠️ engine/log              — 无测试文件
❌ engine/better/.../mongodb — MongoDB 未连接（参考实现，非核心）
```

---

## 三、v1.4 遗留问题（必须在 v1.5 解决）

### 遗留一：Remote 层 Codec 可插拔化

**问题**：v1.4 已实现 ProtobufCodec，但 Remote 层序列化仍硬编码 `json.Marshal/json.Unmarshal`，两者未打通。

**现状代码位置**：`remote/remote.go` 第158-171行，`remote/endpoint.go` 中直接使用 JSON。

**目标**：
- [ ] Remote 层引入 Codec 接口抽象，替换硬编码 JSON
- [ ] RemoteConfig 增加 Codec 配置项（默认 JSON，可选 Protobuf）
- [ ] 端点协商序列化格式（握手阶段声明编解码方式）
- [ ] 保持向后兼容（JSON 仍为默认值）

**预期代码量**：~200-300 行

---

### 遗留二：核心消息 Proto 定义

**问题**：ProtobufCodec 依赖 Go Protobuf 消息类型，但项目无 `.proto` 文件定义核心消息。

**目标**：
- [ ] 定义 `proto/` 目录，创建核心消息 .proto 文件：
  - `remote.proto` — RemoteMessage、RemoteMessageBatch
  - `system.proto` — Started、Stopping、Stopped、Restarting、Watch、Unwatch
  - `cluster.proto` — GossipState、MemberStatus、ClusterTopologyEvent
- [ ] 生成对应 Go 代码
- [ ] Remote 层使用 Protobuf 消息结构

**预期代码量**：~300-500 行（含生成代码）

---

### 遗留三：Codegen 支持 Proto 输入

**问题**：当前 Codegen 仅支持从 Go 源文件（`//msggen:message`）生成代码，不支持从 `.proto` 文件生成路由注册代码。

**目标**：
- [ ] Codegen 增加 `.proto` 文件解析能力
- [ ] 从 .proto 消息定义生成 TypeRegistry 注册代码
- [ ] 从 .proto 生成 TypeScript 类型定义（与现有 TS 生成对齐）

**优先级**：🟡 中（可在 proto 定义完成后迭代）

**预期代码量**：~300-400 行

---

## 四、v1.5 新增优化方向

### 方向一：生产就绪加固

#### 1.1 新增模块测试覆盖

**优先级**：🔴 高

**现状**：errors/、console/、cluster/provider/*、log/ 四个包无测试文件。

**目标**：
- [ ] `errors/` — 错误类型构造、Is/As 判断、Wrap 链测试
- [ ] `console/` — 命令注册与执行、TCP 连接处理测试
- [ ] `cluster/provider/consul/` — Consul Provider 单元测试（mock Consul API）
- [ ] `cluster/provider/etcd/` — etcd Provider 单元测试（mock etcd client）
- [ ] `cluster/provider/k8s/` — K8s Provider 单元测试（mock K8s API）
- [ ] `log/` — 日志输出格式与级别测试

**预期代码量**：~800-1200 行测试代码

---

#### 1.2 Graceful Shutdown（优雅停机）

**优先级**：🔴 高（生产环境刚需，防止消息丢失和状态不一致）

**目标**：
- [ ] ActorSystem 级别的 Shutdown 流程：
  1. 停止接受新消息
  2. 等待 Mailbox 中消息处理完毕（带超时）
  3. 按 Actor 层级自底向上发送 Stopping/Stopped
  4. 关闭 Remote 连接和 Cluster 成员注销
- [ ] Gate 优雅关闭：通知客户端即将断开，等待进行中请求完成
- [ ] 信号处理集成（SIGTERM/SIGINT）
- [ ] Shutdown 超时配置（默认30秒强制退出）

**预期代码量**：~300-500 行

---

#### 1.3 消息追踪与链路 ID

**优先级**：🟡 中（生产环境排查问题必备）

**目标**：
- [ ] Envelope 增加 TraceID 字段（可选，零开销当不使用时）
- [ ] 消息转发/请求时自动传播 TraceID
- [ ] 日志中间件输出 TraceID
- [ ] Dashboard 支持按 TraceID 查询消息流转路径
- [ ] 与外部 Tracing 系统集成预留接口（OpenTelemetry Span 可选）

**预期代码量**：~400-600 行

---

#### 1.4 Rate Limiter（消息限流）

**优先级**：🟡 中（防止恶意客户端或热点 Actor 拖垮系统）

**目标**：
- [ ] 实现令牌桶限流中间件（per-Actor 或 per-Connection）
- [ ] Gate 层连接级限流（消息/秒上限）
- [ ] Actor 级别限流（Mailbox 积压超阈值触发背压）
- [ ] 限流策略可配置（丢弃 / 延迟 / 回压通知）

**预期代码量**：~200-300 行

---

### 方向二：游戏引擎层增强

#### 2.1 场景转移与跨场景通信

**优先级**：🔴 高（MMO 游戏核心功能）

**目标**：
- [ ] 实体跨场景转移协议（TransferEntity 消息 + 状态序列化/反序列化）
- [ ] SceneManager 支持跨节点场景定位（结合 Cluster 一致性哈希）
- [ ] 转移过程中消息暂存（防止转移期间消息丢失）
- [ ] 跨场景 AOI 边界处理（相邻场景实体可见性）

**预期代码量**：~500-700 行

---

#### 2.2 ECS 系统调度器

**优先级**：🟡 中（ECS 当前仅有 World/Entity/Component 数据结构，缺 System 调度）

**目标**：
- [ ] 实现 System 接口（Update 方法 + 组件查询 Query）
- [ ] SystemGroup 有序调度（优先级排序）
- [ ] 固定帧率 Tick 驱动（如 20Hz/50ms 游戏帧）
- [ ] 并行 System 调度（无数据依赖的 System 可并行执行）
- [ ] 与 Actor 模型融合：Scene Actor 内驱动 ECS 帧循环

**预期代码量**：~400-600 行

---

#### 2.3 战斗/技能框架抽象

**优先级**：🟢 低（提供参考模式，不强制使用）

**目标**：
- [ ] 基于 ECS 的 Buff/Effect 组件模式示例
- [ ] 伤害计算管线抽象（DamagePipeline：命中→暴击→减伤→最终）
- [ ] 技能时间线（Timeline：前摇→释放→后摇→冷却）
- [ ] 提供 example/combat_example.go 示例

**预期代码量**：~300-500 行

---

### 方向三：DevOps 与可观测性

#### 3.1 结构化日志体系

**优先级**：🔴 高（替换 log.Printf，生产环境可搜索可聚合）

**现状**：项目使用 `log/` 包，基于标准库 log.Printf。

**目标**：
- [ ] 引入结构化日志接口（Logger 接口，key-value 字段）
- [ ] 默认实现：JSON 格式输出（方便 ELK/Loki 采集）
- [ ] 日志级别：Debug/Info/Warn/Error（可运行时动态调整）
- [ ] 关键路径补充结构化日志（Actor 生命周期、Remote 连接、Cluster 拓扑变更）
- [ ] 与现有 log/ 包兼容，渐进替换

**预期代码量**：~300-400 行

---

#### 3.2 Prometheus 指标导出

**优先级**：🟡 中（生产监控标配）

**目标**：
- [ ] 定义核心指标（基于现有 middleware/metrics.go 的数据）：
  - `engine_actor_message_total` — 按消息类型计数
  - `engine_actor_message_duration_seconds` — 处理耗时直方图
  - `engine_actor_mailbox_depth` — Mailbox 队列深度
  - `engine_remote_connection_count` — 远程连接数
  - `engine_cluster_member_count` — 集群成员数
  - `engine_gate_connection_count` — 客户端连接数
- [ ] HTTP /metrics 端点（Prometheus 拉取格式）
- [ ] 作为可选依赖（`go build -tags prometheus`），不引入默认依赖

**预期代码量**：~300-500 行

---

#### 3.3 Dashboard v2 增强

**优先级**：🟢 低

**目标**：
- [ ] 消息流量实时图表（最近 5 分钟趋势线）
- [ ] 集群拓扑可视化（节点关系图，成员状态着色）
- [ ] Actor 消息火焰图（热点路径分析）
- [ ] 配置在线编辑（ConfigManager 热重载触发）
- [ ] 操作审计日志

**预期代码量**：~500-800 行

---

### 方向四：协议与互操作

#### 4.1 客户端 SDK 生成增强

**优先级**：🟡 中（降低前端/客户端开发对接成本）

**现状**：Codegen 可生成 TypeScript 类型定义，但缺乏完整的客户端 SDK。

**目标**：
- [ ] TypeScript SDK：WebSocket 连接管理 + 消息收发 + 自动重连
- [ ] C# SDK 类型生成（Unity 客户端适配）
- [ ] SDK 含消息序列化/反序列化逻辑（与服务端编解码对齐）
- [ ] 自动生成 API 文档（消息列表 + 字段说明）

**预期代码量**：~600-1000 行（含多语言模板）

---

#### 4.2 消息版本兼容

**优先级**：🟢 低（客户端热更新场景需要）

**目标**：
- [ ] 消息版本号字段
- [ ] 向后兼容策略（新增字段使用默认值，删除字段忽略）
- [ ] 版本协商（连接握手阶段确认协议版本）
- [ ] 版本迁移工具

**预期代码量**：~200-300 行

---

## 五、优先级排序与迭代计划

### 第一批（v1.5-alpha）— v1.4 遗留 + 生产加固

| 序号 | 任务 | 优先级 | 预期工作量 |
|------|------|--------|-----------|
| 1 | Remote 层 Codec 可插拔化 | 🔴 高 | ~250 行 |
| 2 | 核心消息 Proto 定义 | 🔴 高 | ~400 行 |
| 3 | 新增模块测试覆盖 | 🔴 高 | ~1000 行 |
| 4 | Graceful Shutdown | 🔴 高 | ~400 行 |
| 5 | 结构化日志体系 | 🔴 高 | ~350 行 |

### 第二批（v1.5-beta）— 游戏引擎 + 可观测性

| 序号 | 任务 | 优先级 | 预期工作量 |
|------|------|--------|-----------|
| 6 | 场景转移与跨场景通信 | 🔴 高 | ~600 行 |
| 7 | 消息追踪与链路 ID | 🟡 中 | ~500 行 |
| 8 | ECS 系统调度器 | 🟡 中 | ~500 行 |
| 9 | Rate Limiter 消息限流 | 🟡 中 | ~250 行 |
| 10 | Prometheus 指标导出 | 🟡 中 | ~400 行 |

### 第三批（v1.5-rc）— 互操作 + 体验优化

| 序号 | 任务 | 优先级 | 预期工作量 |
|------|------|--------|-----------|
| 11 | Codegen 支持 Proto 输入 | 🟡 中 | ~350 行 |
| 12 | 客户端 SDK 生成增强 | 🟡 中 | ~800 行 |
| 13 | Dashboard v2 增强 | 🟢 低 | ~600 行 |
| 14 | 战斗/技能框架抽象 | 🟢 低 | ~400 行 |
| 15 | 消息版本兼容 | 🟢 低 | ~250 行 |

### 总预期新增代码量：~6,000-7,500 行

---

## 六、技术决策要点

### 6.1 Remote Codec 切换策略

**推荐**：接口抽象 + 配置注入

```go
// RemoteConfig 新增 Codec 配置
type RemoteConfig struct {
    // ...existing fields...
    Codec codec.Codec  // 默认 nil 使用 JSON，可配置为 ProtobufCodec
}
```

**理由**：
- 最小改动：仅需修改 remote/remote.go 和 remote/endpoint.go 中的序列化调用
- 保持向后兼容：默认行为不变
- 端点协商：连接握手阶段声明编解码方式，允许混合部署

### 6.2 结构化日志方案

**推荐**：自定义轻量接口 + slog 适配

**理由**：
- Go 1.21+ 标准库 `log/slog` 已提供结构化日志
- 项目使用 Go 1.24+，可直接采用 slog
- 保持零外部依赖原则
- 定义引擎自己的 Logger 接口，默认实现基于 slog

### 6.3 Prometheus 集成策略

**推荐**：Build Tag 隔离（`-tags prometheus`）

**理由**：
- Prometheus client_golang 是较重依赖
- 通过 build tag 做条件编译，默认构建不引入
- 无 tag 时指标收集仍然工作（dashboard 内部指标），只是不暴露 /metrics 端点

### 6.4 ECS System 调度模型

**推荐**：Scene Actor 内驱动，固定帧率 Tick

```
Scene Actor
└── Tick(deltaTime)
    ├── PhysicsSystem.Update()
    ├── MovementSystem.Update()
    ├── CombatSystem.Update()
    └── AOISystem.Update()
```

**理由**：
- 与 Actor 模型无缝融合（Scene Actor 拥有 World）
- 帧率由 Timer 驱动，不额外创建 goroutine
- System 执行顺序可控，数据一致性有保障

---

## 七、风险评估

| 风险 | 影响 | 应对 |
|------|------|------|
| Remote Codec 切换引入不兼容 | 已部署节点通信中断 | 握手协商 + 默认 JSON 兜底 |
| Proto 定义变更频繁 | 生成代码与手写代码冲突 | 生成代码放独立目录，.gitignore 不追踪 |
| 结构化日志替换面广 | 可能引入大量改动 | 渐进替换，先核心路径后边缘模块 |
| ECS System 与 Actor 模型耦合 | 设计不当导致性能瓶颈 | 先在 Scene Actor 内部封闭使用，不跨 Actor 共享 World |
| Prometheus 依赖引入 | 违背零依赖原则 | Build Tag 隔离，默认不引入 |
| 跨场景转移状态一致性 | 转移过程中消息丢失 | Stash 机制暂存消息，两阶段转移协议 |

---

## 八、v1.3 → v1.4 → v1.5 演进总览

```
v1.3（架构奠基）
├── Phase 1-4 核心功能全部完成
├── Actor 引擎 + 分布式 + 集群 + 游戏层
└── ~67,000 行代码

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

v1.5（生产就绪）— 规划中
├── 打通 Remote + Protobuf 全链路
├── 优雅停机 + 消息追踪 + 限流
├── 结构化日志 + Prometheus 指标
├── 场景转移 + ECS 调度器
├── 客户端 SDK 增强
└── 目标：可上线的生产级引擎
```

---

*文档版本：v1.5*
*生成时间：2026-04-02*
*基于 v1.4 需求审核生成*
