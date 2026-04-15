# 问鼎天下 v1.9 优化计划

> 基于 v1.8 需求审核，识别遗留项与下一阶段迭代方向
> 审核日期：2026-04-15
> 当前代码量：~59,154 行 Go 代码（不含 better/ 参考实现，361 个文件）

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

## 一、v1.8 需求完成度审核

### v1.7 待改善项 — ⚠️ 部分解决

| 需求项 | 状态 | 说明 |
|--------|------|------|
| 排行榜 GetRank 性能 O(N)→O(log N) | ❌ 未解决 | leaderboard/leaderboard.go:149 `getRankLocked()` 仍为 O(N) 链表遍历（`for curr.next[0] != nil`），未利用跳表层级加速 |
| 邮件持久化后端 | ❌ 未解决 | mail/ 模块无 Storage 接口或持久化集成，仍为纯内存实现 |
| Saga 分布式集成 | ❌ 未解决 | saga/ 模块无 Remote 层引用，仍为进程内协调，未与跨节点通信打通 |
| Demo 游戏客户端 | ❌ 未解决 | demo_game/ 仍为纯服务端模拟，无 TypeScript/Web 客户端 |

**结论**：v1.7 的 4 项待改善项在 v1.8 中均未处理，需在 v1.9 中决定优先级。

---

### 方向一：性能极致优化 — ✅ 完全完成（3/3）

| 需求项 | 状态 | 说明 |
|--------|------|------|
| Actor Zero-Alloc 消息通道 | ✅ | actor/ring_buffer.go（149行）实现无锁 MPSC Ring Buffer + CAS 写位置预留 + cache-line padding 防伪共享；actor/pid_cache.go（109行）双级 PID 缓存 + 命中率统计；actor/mailbox_ringbuf.go（156行）基于 Ring Buffer 的零分配 Mailbox；含完整基准测试 zeroalloc_bench_test.go（172行）和单测 ringbuf_cache_test.go（240行） |
| Remote 层 Zero-Copy 序列化 | ✅ | remote/zero_copy.go（294行）实现 ZeroCopyCodec 利用 io.Writer/Reader 零拷贝编解码 + Buffer 池化 + WriterTo 快速路径；remote/fragment.go（258行）大消息分片传输（>64KB 自动分片为 32KB 块）+ 乱序重组 + 重复检测 + 超时清理；codec/stream_codec.go（201行）流式编解码接口 + JSON 流式实现 + 适配器；含基准测试 zerocopy_bench_test.go（358行） |
| Mailbox 自适应调度策略 | ✅ | actor/mailbox_adaptive.go（236行）自适应吞吐量 Mailbox + 队列深度动态调整 + 协作式让出；actor/dispatcher_worksteal.go（236行）工作窃取调度器（N Worker 线程 + 无锁本地队列 + 随机受害者选择）；actor/scheduling_metrics.go（24行）调度指标定义；含完整测试 adaptive_dispatcher_test.go（282行） |

---

### 方向二：集群高可用增强 — ✅ 完全完成（3/3）

| 需求项 | 状态 | 说明 |
|--------|------|------|
| Actor 状态迁移（Live Migration） | ✅ | cluster/migration.go（647行）实现 Migratable 接口 + MigrationManager 两阶段迁移（Pause→Serialize→Transfer→Restore→Resume）+ Redirect 表自动转发 + Singleton 联动 + 超时回滚；含测试 migration_test.go（268行） |
| 集群滚动升级支持 | ✅ | cluster/rolling_upgrade.go（728行）实现 RollingUpgradeCoordinator 多阶段升级（Drain→Upgrade→Canary→Complete）+ 节点健康检查 + Gossip 状态广播 + N-1 版本前向兼容 + Migration 联动排空；含测试 rolling_upgrade_test.go（205行） |
| 多数据中心支持 | ✅ | cluster/multi_dc.go（432行）实现 MultiDCManager DC 感知路由（本地优先）+ 跨 DC 心跳倍率 + 故障转移（含冷却期）+ 读写分离路由 + 拓扑变更追踪；含测试 multi_dc_test.go（369行） |

---

### 方向三：游戏引擎进阶 — ✅ 完全完成（4/4）

| 需求项 | 状态 | 说明 |
|--------|------|------|
| 帧同步/状态同步框架 | ✅ | syncx/ 目录（723行）：lockstep.go 帧同步 + CRC32 哈希校验；statesync.go 状态同步状态机；latency.go 延迟补偿；strategy.go SyncStrategy 接口；messages.go 协议消息；含完整测试 syncx_test.go |
| AI 行为树框架 | ✅ | bt/ 目录（1,001行）：tree.go 行为树核心 + JSON 配置加载 + ActionRegistry；composite.go Sequence/Selector/Parallel 组合节点；decorator.go Inverter/Repeater/Limiter 装饰节点；blackboard.go 共享数据；leaf.go Action/Condition 叶子节点；含完整测试 bt_test.go |
| 战斗回放系统 | ✅ | replay/ 目录（763行）：recorder.go 事件录制；player.go 回放播放（倍速/暂停/跳转）；format.go 紧凑二进制格式 + 版本控制；含完整测试 replay_test.go |
| 背包/道具系统框架 | ✅ | inventory/ 目录（767行）：inventory.go 背包管理（增删查改/容量/排序/堆叠合并/拆分）；item.go ItemStack + ItemTemplate 定义；messages.go 协议消息；含完整测试 inventory_test.go |

---

### 方向四：运维与部署 — ✅ 完全完成（3/3）

| 需求项 | 状态 | 说明 |
|--------|------|------|
| Kubernetes Operator 支持 | ✅ | deploy/k8s/ 目录（353行）：configmap_watch.go ConfigMap 热重载（symlink 追踪）；hpa_metrics.go 自定义 HPA 指标提供器（连接数/Actor 数）；labels.go K8s 标签工具；含测试 hpa_metrics_test.go |
| 灰度发布与流量控制 | ✅ | cluster/canary/ 目录（850行）：canary.go 灰度发布引擎 + 流量权重控制；comparator.go 基线 A/B 对比（错误率/延迟/资源）；router.go 灰度感知路由；dashboard/handlers_canary.go（148行）HTTP API（状态/规则/权重/全量/回滚）；含完整测试 canary_test.go |
| 链路级性能分析 | ✅ | middleware/profiler.go（347行）pprof 集成 + Ring Buffer 存储 + 自动触发（CPU/GC 阈值）；middleware/profiler_actor.go（163行）Actor 封装；dashboard/handlers_profiler.go（301行）HTTP API（CPU/Heap/Goroutine/Block Profile + Diff 分析）；含测试 profiler_test.go |

---

### 方向五：生态与互操作 — ✅ 完全完成（4/4）

| 需求项 | 状态 | 说明 |
|--------|------|------|
| Unity/Unreal 客户端 SDK | ✅ | codegen/templates_csharp_sdk.go（475行）完整 C# Unity SDK 模板（TCP/WebSocket 连接管理 + 自动重连 + 消息路由/反序列化 + JSON Codec）；codegen/templates_sdk_enhanced.go（431行）增强版 TypeScript SDK（消息路由器 + 类型安全消息映射 + JSON/Protobuf 双模式编解码） |
| GM 管理后台框架 | ✅ | dashboard/gm.go（463行）实现 GMManager + GMCommand 命令框架 + GMUser 角色权限模型（admin/operator/cs/readonly）+ 批量操作支持 + 审计日志集成 |
| 自动化压测框架 | ✅ | stress/bench.go（472行）压测场景 DSL（JSON 配置）+ 指标收集（TPS/P99 延迟/错误率）+ CI 基线对比 + 报告生成；stress/distributed.go（341行）分布式压测协调器（Master-Worker 架构）+ 并发任务分发 + 报告聚合 |
| 插件体系标准化 | ✅ | hotreload/manifest.go（366行）YAML 插件清单加载 + DependencyResolver 拓扑排序依赖解析 + LifecyclePlugin 5 阶段生命周期（Init→Start→HealthCheck→Stop→Cleanup）+ 插件隔离上下文 |

---

## 二、当前构建与测试状态

```
构建状态：✅ go build ./... 通过（零错误）

测试状态（37 个引擎包通过，1 个压测包间歇失败）：
✅ engine/actor                      — 通过
✅ engine/bt                         — 通过
✅ engine/cluster                     — 通过
✅ engine/cluster/canary              — 通过（v1.8 新增）
✅ engine/cluster/federation          — 通过
✅ engine/cluster/provider/consul     — 通过
✅ engine/cluster/provider/etcd       — 通过
✅ engine/cluster/provider/k8s        — 通过
✅ engine/codec                       — 通过
✅ engine/codegen                     — 通过
✅ engine/config                      — 通过
✅ engine/console                     — 通过
✅ engine/dashboard                   — 通过
✅ engine/deploy/k8s                  — 通过（v1.8 新增）
✅ engine/ecs                         — 通过
✅ engine/errors                      — 通过
✅ engine/gate                        — 通过
✅ engine/grain                       — 通过
✅ engine/hotreload                   — 通过
✅ engine/internal                    — 通过
✅ engine/inventory                   — 通过（v1.8 新增）
✅ engine/leaderboard                 — 通过
✅ engine/log                         — 通过
✅ engine/mail                        — 通过
✅ engine/middleware                   — 通过
✅ engine/network                     — 通过
✅ engine/persistence                 — 通过
✅ engine/proto                       — 通过
✅ engine/pubsub                      — 通过
✅ engine/remote                      — 通过
✅ engine/replay                      — 通过（v1.8 新增）
✅ engine/room                        — 通过
✅ engine/router                      — 通过
✅ engine/saga                        — 通过
✅ engine/scene                       — 通过
✅ engine/syncx                       — 通过（v1.8 新增）
✅ engine/timer                       — 通过

⚠️ engine/stress                     — TestStressClusterNodeFailure 间歇失败（压测级测试，非核心逻辑缺陷）

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

## 三、v1.8 遗留问题

### 已确认遗留项

| 来源 | 项目 | 说明 | 优先级 |
|------|------|------|--------|
| v1.7 遗留 | 排行榜 GetRank O(N) 性能 | getRankLocked() 仍为链表遍历，应利用跳表层级实现 O(log N) | 🟢 低 |
| v1.7 遗留 | 邮件持久化后端 | mail/ 纯内存实现，生产需数据库存储 | 🟡 中 |
| v1.7 遗留 | Saga 分布式集成 | 仅进程内协调，未打通 Remote 层跨节点 Saga | 🟡 中 |
| v1.7 遗留 | Demo 游戏客户端 | 缺少 TypeScript Web 客户端示例 | 🟢 低 |
| v1.8 新增 | stress 测试稳定性 | TestStressClusterNodeFailure 间歇失败 | 🟢 低 |
| v1.8 新增 | K8s CRD/Operator | v1.8 仅完成 Helm 基础（ConfigMap/HPA/Labels），CRD 按计划延后 | 🟡 中 |
| v1.8 新增 | Helm Chart YAML 模板 | deploy/k8s/ 仅含 Go 代码，缺少实际 Helm Chart YAML 部署模板 | 🟡 中 |

### 待改善项（非阻塞，可在 v1.9 中迭代优化）

| 项目 | 说明 | 优先级 |
|------|------|--------|
| 行为树 Dashboard 可视化 | bt/ 已实现完整行为树，但缺少 Dashboard 中的执行路径可视化调试 | 🟢 低 |
| 行为树 ECS 集成 | bt/ 与 ecs/ 未打通 AISystem 驱动 | 🟡 中 |
| 帧同步确定性物理 | syncx/ 帧同步无定点数运算支持，浮点数会导致跨平台不一致 | 🟡 中 |
| 背包交易安全 | inventory/ 缺少双方背包原子交易（需 Saga 集成） | 🟢 低 |
| 回放存储策略 | replay/ 仅支持内存/本地，缺少冷数据归档 | 🟢 低 |
| 灰度规则引擎 | canary/ 按权重路由已实现，但缺少按用户 ID/地区/渠道条件匹配 | 🟡 中 |
| 分布式压测 Bot | stress/ 有框架，缺少可编程虚拟玩家（Bot）行为模型 | 🟡 中 |

---

## 四、v1.9 新增优化方向

### 方向一：生产级加固（Production Hardening）

#### 1.1 邮件系统持久化 + Saga 跨节点集成

**优先级**：🔴 高（两个连续两版未解决的遗留项，需彻底清零）

**现状**：
- mail/ 纯内存实现，服务重启数据丢失
- saga/ 仅进程内协调，无法跨节点编排分布式事务

**目标**：
- [ ] mail/storage.go：MailStorage 接口（Save/Load/Delete/ListByPlayer/MarkRead）
- [ ] mail/memory_storage.go：MemoryMailStorage 实现（保持现有行为兼容）
- [ ] mail/mongo_storage.go：MongoMailStorage 实现（复用 persistence/ 的 MongoDB 连接）
- [ ] saga/remote_saga.go：RemoteSagaCoordinator，基于 remote/ 层的跨节点 Step 执行
- [ ] saga/remote_saga.go：分布式补偿链——失败时反向通知各节点执行 Compensate
- [ ] saga/remote_saga_test.go：跨节点 Saga 集成测试

**预期代码量**：~500-600 行

---

#### 1.2 排行榜 GetRank O(log N) 优化

**优先级**：🟡 中（性能优化，大规模玩家时影响显著）

**现状**：leaderboard/leaderboard.go:149 `getRankLocked()` 遍历链表底层计数，时间复杂度 O(N)。

**目标**：
- [ ] 跳表节点增加 span 字段：记录每层到下一节点的跨越元素数
- [ ] getRankLocked() 利用高层 span 累加计算排名，O(log N) 复杂度
- [ ] 插入/删除时维护 span 一致性
- [ ] 基准测试对比优化前后（10K/100K/1M 玩家场景）

**预期代码量**：~100-150 行（改造现有代码）

---

#### 1.3 Stress 测试稳定性修复

**优先级**：🟢 低（不影响核心功能，但影响 CI 可靠性）

**目标**：
- [ ] 修复 TestStressClusterNodeFailure 间歇失败（排查竞态条件或超时设置）
- [ ] 压测测试加入 `-timeout` 和 `-count` 重复验证
- [ ] 隔离压测测试到独立 CI 阶段（避免阻塞核心测试）

**预期代码量**：~50-100 行

---

### 方向二：确定性计算与同步深化

#### 2.1 定点数运算库

**优先级**：🔴 高（帧同步游戏跨平台一致性的基石）

**现状**：syncx/ 帧同步框架已就绪，但使用 float64 运算，不同平台/编译器浮点结果可能不一致。

**目标**：
- [ ] fixedpoint/fixed.go：Q16.16 定点数类型（Add/Sub/Mul/Div/Sqrt/Sin/Cos/Atan2）
- [ ] fixedpoint/vec2.go：定点数 2D 向量（距离/归一化/点积/叉积）
- [ ] fixedpoint/math.go：定点数数学函数（查表法三角函数，牛顿法开方）
- [ ] 与 syncx/ 帧同步集成：FrameSyncRoom 使用定点数校验哈希
- [ ] 与 scene/ AOI 集成：提供定点数坐标选项
- [ ] 基准测试：定点数 vs float64 性能对比

**预期代码量**：~500-600 行

---

#### 2.2 客户端预测与服务端回滚

**优先级**：🟡 中（状态同步模式的进阶能力）

**现状**：syncx/statesync.go 实现了基础状态同步，但缺少客户端预测和服务端回滚。

**目标**：
- [ ] syncx/prediction.go：客户端输入预测框架（本地立即执行 + 服务端权威校正）
- [ ] syncx/rollback.go：服务端历史状态环形缓冲 + 状态回滚重算（Rollback & Resimulate）
- [ ] syncx/interpolation.go：实体状态插值（平滑其他玩家的位置更新）
- [ ] 与 latency.go 集成：基于 RTT 自适应预测帧数

**预期代码量**：~400-500 行

---

### 方向三：AI 与游戏逻辑深化

#### 3.1 行为树 ECS 集成 + Dashboard 可视化

**优先级**：🟡 中（NPC 智能行为的标准开发流程）

**现状**：bt/ 和 ecs/ 均已实现但未打通；Dashboard 缺少行为树调试界面。

**目标**：
- [ ] ecs/ai_system.go：AISystem 驱动所有 AI Entity 的行为树 Tick
- [ ] ecs/ai_component.go：AIComponent 持有行为树实例 + Blackboard
- [ ] bt/lod.go：行为树 LOD（远距 NPC 降低 Tick 频率，近距全速 Tick）
- [ ] dashboard/handlers_bt.go：行为树执行路径可视化 API（当前节点高亮 + 执行历史）
- [ ] 配合 scene/ AOI：仅 AOI 范围内的 NPC 执行高频 Tick

**预期代码量**：~400-500 行

---

#### 3.2 技能/Buff 系统框架

**优先级**：🟡 中（中重度游戏战斗系统标配）

**现状**：ecs/ 有 Combat System 示例，但缺乏通用的技能和 Buff 抽象。

**目标**：
- [ ] skill/skill.go：Skill 定义（ID/冷却/消耗/目标类型/效果链）
- [ ] skill/buff.go：Buff/Debuff 系统（叠加/刷新/互斥/优先级/定时器衰减）
- [ ] skill/effect.go：Effect 管线（伤害计算/属性修改/状态施加/AOE 范围检测）
- [ ] skill/cooldown.go：冷却管理（全局 CD/独立 CD/CD 重置）
- [ ] 配表驱动：从 config/ RecordFile 加载技能/Buff 模板
- [ ] 与 ECS 集成：SkillSystem + BuffSystem 作为 ECS System 注册

**预期代码量**：~600-700 行

---

#### 3.3 任务/成就系统框架

**优先级**：🟢 低（游戏内容系统，作为引擎可选模块）

**目标**：
- [ ] quest/quest.go：Quest 定义（多步骤/条件触发/奖励发放）
- [ ] quest/tracker.go：QuestTracker 事件监听 + 进度更新
- [ ] quest/achievement.go：Achievement 成就系统（一次性达成 + 持久记录）
- [ ] 与 mail/ 集成：任务完成自动发放邮件奖励
- [ ] 与 leaderboard/ 集成：成就积分排行

**预期代码量**：~400-500 行

---

### 方向四：部署与运维完善

#### 4.1 Helm Chart 完整模板

**优先级**：🔴 高（K8s 部署标准化的最后一公里）

**现状**：deploy/k8s/ 有 Go 代码（ConfigMap 监听/HPA 指标/标签），但缺少实际 Helm Chart。

**目标**：
- [ ] deploy/helm/engine/Chart.yaml：Helm Chart 元数据
- [ ] deploy/helm/engine/values.yaml：默认配置（单节点/集群/多区域三种 Preset）
- [ ] deploy/helm/engine/templates/deployment.yaml：Deployment + 探针（/healthz → liveness、/readyz → readiness）
- [ ] deploy/helm/engine/templates/service.yaml：Service + 端口定义
- [ ] deploy/helm/engine/templates/configmap.yaml：引擎配置注入
- [ ] deploy/helm/engine/templates/hpa.yaml：HPA 自动伸缩（基于自定义指标）
- [ ] deploy/helm/engine/templates/serviceMonitor.yaml：Prometheus ServiceMonitor（可选）
- [ ] README 使用说明

**预期代码量**：~400-500 行（YAML 模板）

---

#### 4.2 K8s CRD + Operator（Phase 2）

**优先级**：🟡 中（v1.8 技术决策中明确延后到 v1.9 的项目）

**现状**：v1.8 按计划仅完成 Helm 基础，CRD/Operator 是 Phase 2。

**目标**：
- [ ] deploy/operator/crd.go：EngineCluster CRD 定义（集群规模/版本/配置/升级策略）
- [ ] deploy/operator/controller.go：Reconcile 控制器（监听 CRD 变更 → 执行滚动升级/扩缩容）
- [ ] deploy/operator/scaler.go：自动扩缩容逻辑（基于连接数/Actor 数/CPU 触发）
- [ ] 与 cluster/rolling_upgrade.go 联动：Operator 编排调用 RollingUpgradeCoordinator
- [ ] 与 cluster/migration.go 联动：缩容时自动触发 Actor Live Migration

**预期代码量**：~600-700 行

---

#### 4.3 灰度规则引擎增强

**优先级**：🟡 中（大规模运营精细化发布）

**现状**：cluster/canary/ 已实现权重路由和基线对比，但规则仅支持百分比权重。

**目标**：
- [ ] cluster/canary/rule_engine.go：规则引擎（按用户 ID 分桶 / 地区 / 渠道 / 自定义属性匹配）
- [ ] cluster/canary/rule_engine.go：规则优先级 + 组合条件（AND/OR）
- [ ] cluster/canary/ab_test.go：A/B Test 支持（灰度不仅限于版本升级，还可用于功能实验）
- [ ] Dashboard 灰度管理页面 API 增强：规则配置/实验创建/数据查看

**预期代码量**：~300-400 行

---

### 方向五：开发体验与生态完善

#### 5.1 TypeScript Web 客户端 Demo

**优先级**：🟡 中（Demo 游戏的完整前后端体验）

**现状**：
- codegen/ 已能生成 TypeScript SDK（含消息路由/Protobuf 双模式）
- example/demo_game/ 仅服务端模拟

**目标**：
- [ ] example/demo_game/web_client/：基于生成的 TypeScript SDK 的 Web 客户端
- [ ] WebSocket 连接 + 消息收发
- [ ] 简单 UI：登录/匹配/猜数游戏界面/排行榜展示
- [ ] 作为 SDK 使用的最佳实践示例

**预期代码量**：~500-600 行（TypeScript）

---

#### 5.2 集成测试框架

**优先级**：🟡 中（多节点场景的自动化测试能力）

**现状**：actor/testkit.go 提供单节点测试工具，缺乏多节点集成测试支持。

**目标**：
- [ ] testkit/cluster_testkit.go：多节点测试辅助（自动启动 N 个本地节点 + 组集群）
- [ ] testkit/remote_testkit.go：远程通信测试辅助（模拟网络分区/延迟/丢包）
- [ ] testkit/scenario.go：场景测试 DSL（描述式多步骤测试流程）
- [ ] 与 stress/ 框架共享基础设施

**预期代码量**：~400-500 行

---

#### 5.3 压测虚拟玩家（Bot）框架

**优先级**：🟡 中（自动化压测的业务模拟能力）

**现状**：stress/ 有分布式压测框架，但缺少可编程的虚拟玩家。

**目标**：
- [ ] stress/bot.go：Bot 接口（Login/Play/Logout 生命周期）
- [ ] stress/bot_builder.go：BotBuilder 链式配置（行为序列/随机间隔/条件分支）
- [ ] stress/bot_pool.go：BotPool 批量管理（并发创建/销毁/指标聚合）
- [ ] 与 gate/ 集成：Bot 通过 TCP/WebSocket 连接真实网关
- [ ] 内置行为模板：IdleBot/ActiveBot/StressBot 三种预设

**预期代码量**：~400-500 行

---

#### 5.4 engine-cli 增强

**优先级**：🟢 低（开发者工具链完善）

**目标**：
- [ ] `engine plugin install/remove/list`：插件管理命令（对接 hotreload/manifest.go）
- [ ] `engine cluster status`：集群状态查看（连接 Dashboard API）
- [ ] `engine migrate`：手动触发 Actor 迁移
- [ ] `engine bench`：快速运行内置基准测试套件

**预期代码量**：~300-400 行

---

## 五、优先级排序与迭代计划

### 第一批（v1.9-alpha）— 遗留清零 + 确定性计算

| 序号 | 任务 | 优先级 | 预期工作量 |
|------|------|--------|-----------|
| 1 | 邮件持久化 + Saga 跨节点集成 | 🔴 高 | ~550 行 |
| 2 | 定点数运算库 | 🔴 高 | ~550 行 |
| 3 | Helm Chart 完整模板 | 🔴 高 | ~450 行 |
| 4 | 排行榜 GetRank O(log N) | 🟡 中 | ~125 行 |
| 5 | Stress 测试稳定性修复 | 🟢 低 | ~75 行 |

### 第二批（v1.9-beta）— 游戏深化 + 部署自动化

| 序号 | 任务 | 优先级 | 预期工作量 |
|------|------|--------|-----------|
| 6 | K8s CRD + Operator | 🟡 中 | ~650 行 |
| 7 | 技能/Buff 系统框架 | 🟡 中 | ~650 行 |
| 8 | 行为树 ECS 集成 + Dashboard 可视化 | 🟡 中 | ~450 行 |
| 9 | 客户端预测与服务端回滚 | 🟡 中 | ~450 行 |
| 10 | 灰度规则引擎增强 | 🟡 中 | ~350 行 |

### 第三批（v1.9-rc）— 生态 + 开发体验

| 序号 | 任务 | 优先级 | 预期工作量 |
|------|------|--------|-----------|
| 11 | TypeScript Web 客户端 Demo | 🟡 中 | ~550 行 |
| 12 | 集成测试框架 | 🟡 中 | ~450 行 |
| 13 | 压测虚拟玩家 Bot 框架 | 🟡 中 | ~450 行 |
| 14 | 任务/成就系统框架 | 🟢 低 | ~450 行 |
| 15 | engine-cli 增强 | 🟢 低 | ~350 行 |

### 总预期新增代码量：~6,000-7,500 行

---

## 六、技术决策要点

### 6.1 邮件持久化方案

**推荐**：Storage 接口抽象 + 可插拔后端

```go
type MailStorage interface {
    Save(playerID string, mail *Mail) error
    Load(playerID string) ([]*Mail, error)
    MarkRead(playerID string, mailID string) error
    Delete(playerID string, mailID string) error
    CleanExpired() (int, error)
}
```

**理由**：
- 与 persistence/ 已有的 Storage 接口模式一致
- MemoryMailStorage 保持测试和开发环境零依赖
- MongoMailStorage 复用现有 MongoDB 连接管理
- 未来可扩展 Redis/MySQL 后端

### 6.2 定点数精度选择

**推荐**：Q16.16 固定精度

| 方案 | 整数范围 | 小数精度 | 适用场景 |
|------|---------|----------|---------|
| Q16.16 | ±32767 | 1/65536 ≈ 0.000015 | 游戏坐标/物理计算 |
| Q24.8 | ±8388607 | 1/256 ≈ 0.0039 | 大地图/低精度 |
| Q8.24 | ±127 | 1/16777216 | 高精度物理 |

**理由**：
- Q16.16 是游戏行业事实标准（Unity DOTS、Deterministic Physics 均采用）
- 32767 足够覆盖绝大部分游戏场景坐标范围
- int32 底层类型，运算高效且跨平台一致
- 三角函数用查表法（1024 点）+ 线性插值，精度足够

### 6.3 Saga 跨节点方案

**推荐**：基于 Remote Request-Response 的分布式编排

```
Coordinator (Node A)
├── Step 1: Local Action  → success
├── Step 2: Remote Action (Node B) → remote.Request(nodeBPID, sagaStepMsg)
├── Step 3: Remote Action (Node C) → remote.Request(nodeCPID, sagaStepMsg)
└── 任一失败 → 反向 Compensate（先 Step 3 补偿 → Step 2 补偿 → Step 1 补偿）
```

**理由**：
- 复用 remote/request_response.go 的 RemoteFutureRegistry，无需新建传输通道
- Coordinator 仍为单点编排（简单可靠），仅 Step 执行分布在多节点
- 补偿链天然支持：SagaBuilder 已有 Compensate 定义
- 超时和重试配置沿用现有 SagaStep 参数

### 6.4 K8s Operator 架构

**推荐**：Lightweight Operator（不依赖 controller-runtime）

**理由**：
- controller-runtime 引入大量依赖（client-go、apimachinery），与引擎零外部依赖原则冲突
- 轻量实现：直接使用 K8s REST API + Watch 机制
- deploy/k8s/ 已有 ConfigMap Watch 基础，可复用
- 生产环境可选集成 controller-runtime（`//go:build k8s_operator`）

### 6.5 技能/Buff 系统设计

**推荐**：数据驱动 + Effect Pipeline

```
Skill Cast Flow:
  CastRequest → CooldownCheck → CostCheck → TargetSelect → EffectPipeline
                                                              ├── DamageEffect
                                                              ├── BuffApplyEffect
                                                              ├── HealEffect
                                                              └── AOEEffect

Buff Tick Flow:
  BuffSystem.Update(dt) → foreach Buff → Tick → CheckExpire → Apply/Remove
```

**理由**：
- Effect Pipeline 模式可组合、可扩展（新增效果类型无需修改核心流程）
- 配表驱动：技能 ID → 效果链 JSON，策划可直接配置
- 与 ECS 融合：SkillComponent + BuffComponent 挂载到 Entity
- 与 timer/ 集成：Buff 持续时间/技能冷却由定时器管理

---

## 七、风险评估

| 风险 | 影响 | 应对 |
|------|------|------|
| 定点数精度丢失 | 物理模拟出现累积误差 | 单元测试覆盖边界值；提供 Fixed.ToFloat64() 调试接口；文档明确精度范围 |
| 跨节点 Saga 网络中断 | 分布式事务卡在中间状态 | 补偿超时自动重试（已有 MaxRetries）；SagaStore 持久化状态可恢复；最终一致性兜底 |
| K8s Operator 版本兼容 | 不同 K8s 版本 API 差异 | 仅使用 stable API（apps/v1、core/v1）；版本矩阵测试 1.24-1.30 |
| 技能系统性能开销 | 大量 Buff Tick 导致帧时间超限 | Buff 批处理（同类 Buff 合并 Tick）；过期 Buff 延迟清理；LOD 降频 |
| Helm Chart 配置爆炸 | values.yaml 过于复杂 | 提供 Preset（minimal/cluster/global）；分层 values 文件 |
| 邮件持久化迁移 | 现有内存数据丢失 | MemoryMailStorage 保持默认；迁移工具将内存快照写入 MongoDB |
| 集成测试环境依赖 | CI 需要多端口/多进程 | 使用随机端口分配；进程内模拟多节点（共享内存通信） |

---

## 八、v1.3 → v1.9 演进总览

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
└── 遗留：4 项待改善（GetRank 性能/邮件持久化/Saga 分布式/Demo 客户端）

v1.8（极致性能 + 商业就绪）— ✅ 完成（17/17 = 100%）
├── 性能：Zero-Alloc 消息通道 + Zero-Copy 序列化 + 自适应调度 + 工作窃取
├── 高可用：Actor Live Migration + 滚动升级 + 多数据中心
├── 游戏进阶：帧同步/状态同步 + AI 行为树 + 战斗回放 + 背包道具
├── 运维：K8s 基础集成 + 灰度发布 + 持续 Profiling
├── 生态：Unity/UE SDK 模板 + GM 后台 + 自动化压测 + 插件标准化
└── 遗留：v1.7 的 4 项待改善未处理 + Helm Chart 缺 YAML + CRD 按计划延后

v1.9（生产加固 + 游戏深化）— 规划中
├── 加固：邮件持久化 + Saga 跨节点 + 排行榜优化（遗留清零）
├── 确定性：定点数运算库 + 客户端预测/回滚
├── 游戏深化：技能/Buff 系统 + 行为树 ECS 集成 + 任务/成就
├── 部署：Helm Chart 完整模板 + K8s CRD/Operator + 灰度规则引擎
├── 生态：TypeScript Demo 客户端 + 集成测试框架 + 压测 Bot + CLI 增强
└── 目标：遗留清零 + 游戏业务能力纵深 + K8s 全链路部署
```

---

## 九、v1.8 完成度总结

| 分类 | 完成 | 未完成 | 完成度 |
|------|------|--------|--------|
| v1.7 待改善项 | 0/4 | 4 | 0%（非阻塞项，延续至 v1.9） |
| 方向一：性能极致优化 | 3/3 | 0 | 100% |
| 方向二：集群高可用增强 | 3/3 | 0 | 100% |
| 方向三：游戏引擎进阶 | 4/4 | 0 | 100% |
| 方向四：运维与部署 | 3/3 | 0 | 100% |
| 方向五：生态与互操作 | 4/4 | 0 | 100% |
| **v1.8 新增需求合计** | **17/17** | **0** | **100%** |

**关键结论**：

- v1.8 规划的 17 项新增需求 **全部完成**（100%），实现质量高（均为真实实现，非桩代码）
- 新增 8 个模块/子包（bt/、syncx/、replay/、inventory/、deploy/k8s/、cluster/canary/、stress 分布式增强、hotreload/manifest）
- 构建零错误，37 个引擎测试包通过（stress 包间歇失败属压测级问题）
- 代码量从 v1.7 的 ~45,400 行增长到 ~59,154 行（+30.3%），文件数从 302 增长到 361（+59 个新文件）
- **v1.7 遗留的 4 项待改善项未在 v1.8 中解决**，需在 v1.9-alpha 优先处理
- v1.9 的重点从"性能极致优化"转向"生产加固"（遗留清零）+ "游戏业务纵深"（技能/Buff/任务）+ "部署全链路"（Helm/Operator）

---

*文档版本：v1.9*
*生成时间：2026-04-15*
*基于 v1.8 需求审核生成*
*当前代码量：~59,154 行 Go 代码（不含 better/ 参考实现，361 个文件）*
