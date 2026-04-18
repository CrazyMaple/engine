# 问鼎天下 v1.11 优化计划

> 基于 v1.10 需求审核，识别遗留项与下一阶段迭代方向
> 审核日期：2026-04-17
> 当前代码量：~82,646 行 Go 代码（不含 better/ 参考实现，462 个文件）

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

## 一、v1.10 需求完成度审核

### 第一批（v1.10-alpha）— ✅ 完全完成（5/5）

| 需求项 | 状态 | 说明 |
|--------|------|------|
| 技能/Buff 系统 ECS 集成 + 配表驱动 | ✅ | ecs/skill_system.go（116 行）+ ecs/buff_system.go（89 行）+ ecs/skill_component.go（57 行）+ skill/loader.go（322 行，支持 JSON/RecordFile 加载技能/Buff 模板）+ ecs/skill_system_test.go + skill/loader_test.go，合计 ~580 行 |
| 网络层 KCP/QUIC 可选传输 | ✅ | network/kcp_server.go（181 行）+ network/kcp_conn.go（489 行）+ network/kcp_config.go（69 行，四元组参数）+ network/kcp_test.go，合计 ~740 行；Conn 接口兼容 |
| 分布式日志聚合 | ✅ | log/aggregator.go（435 行，LogSink 接口 + 文件/UDP/TCP Sink + 节点 ID/TraceID 上下文）+ log/ring_buffer_sink.go（133 行）+ dashboard/handlers_log.go（114 行）+ 测试，合计 ~680 行 |
| Actor 消息加密传输 | ✅ | remote/encryption.go（218 行）AES-256-GCM 加密中间件 + 密钥协商 + 轮换；remote/encryption_test.go 完整覆盖 |
| Protobuf 客户端 SDK 生成增强 | ✅ | codegen/templates_proto_sdk.go（421 行）+ codegen/generator_proto_sdk.go（84 行）+ codegen/generator_proto_sdk_test.go，合计 ~500 行；TypeURL 注册表自动生成 |

---

### 第二批（v1.10-beta）— ⚠️ 部分完成（8/9）

| 需求项 | 状态 | 说明 |
|--------|------|------|
| 背包交易安全（Saga 原子交易） | ✅ | inventory/trade.go（398 行）TradeSession 双方原子交易 + Saga 编排 + 超时/补偿；inventory/trade_test.go 覆盖正常/超时/并发 |
| 任务系统跨模块联动 | ✅ | quest/integration.go（275 行）QuestCompleted 事件联动 mail/inventory/leaderboard；quest/integration_test.go |
| 增量状态压缩 | ✅ | syncx/delta.go（546 行）DeltaEncoder/Decoder + 字段级脏标记 + 位图压缩；syncx/delta_test.go |
| Dashboard v4 告警 + 拓扑 | ✅ | dashboard/alert.go（350 行）+ handlers_alert.go（137 行）+ handlers_topology.go（190 行）+ handlers_replay.go（165 行），合计 ~840 行 |
| Stress 测试彻底修复 + CI 隔离 | ⚠️ | stress/baseline.go（154 行）+ stress/test_helpers.go 测试辅助建立；**但 TestStressClusterNodeFailure 仍 60s 超时失败，根因未定位** |
| Actor 访问控制增强 | ✅ | middleware/rbac.go（262 行）RBAC + 命名空间隔离 + 动态权限变更；middleware/rbac_test.go |
| 引擎配置标准化（YAML） | ✅ | config/engine_config.go（410 行）统一 EngineConfig + YAML 加载 + 环境变量覆盖 + 校验；config/engine_config_test.go |
| 性能基准回归体系 | ✅ | bench/ 新建目录：baseline.go（147 行）+ compare.go（190 行）+ parser.go（133 行）+ report.go（159 行）+ bench_test.go（281 行），合计 ~910 行；cmd/engine/cmd_bench.go CLI 集成 |
| A/B 实验统计显著性检验 | ❌ | 未实现，cluster/canary/abtest.go 中零 p-value/置信区间相关代码 |

---

### 第三批（v1.10-rc）— ⚠️ 部分完成（2/5）

| 需求项 | 状态 | 说明 |
|--------|------|------|
| 预测平滑校正优化 | ✅ | syncx/prediction.go 新增 LerpCorrection/SpringCorrection/CorrectionMode（11 处新增相关标识） |
| 审计日志合规增强 | ✅ | dashboard/audit_enhanced.go（443 行）哈希链不可篡改存储 + 导出 + 二次确认；cmd/engine/cmd_audit.go（91 行）CLI；audit_enhanced_test.go 完整覆盖 |
| 社区贡献指引与 API 稳定性 | ⚠️ | codegen/stability.go（344 行）+ codegen/changelog.go（168 行）实现 Stable/Beta/Experimental 分级 + 自动 CHANGELOG；**但 CONTRIBUTING.md 未创建** |
| 回放存储策略（冷数据归档） | ❌ | replay/ 无新增归档文件，零 cold/archive/S3/object 相关实现 |
| Helm Chart README 文档 | ❌ | deploy/helm/engine/ 下无 README.md |

---

## 二、当前构建与测试状态

```
构建状态：✅ go build ./... 通过（零错误）

测试状态（47 个引擎包通过，1 个压测包仍失败）：
✅ engine/actor                 ✅ engine/bench (v1.10 新增)
✅ engine/bt                    ✅ engine/cluster
✅ engine/cluster/canary        ✅ engine/cluster/federation
✅ engine/cluster/provider/*    ✅ engine/codec
✅ engine/codegen               ✅ engine/config
✅ engine/console               ✅ engine/dashboard
✅ engine/deploy/k8s            ✅ engine/deploy/operator
✅ engine/ecs                   ✅ engine/errors
✅ engine/fixedpoint            ✅ engine/gate
✅ engine/grain                 ✅ engine/hotreload
✅ engine/internal              ✅ engine/inventory (含 trade)
✅ engine/leaderboard           ✅ engine/log (含 aggregator)
✅ engine/mail                  ✅ engine/middleware (含 rbac)
✅ engine/network (含 kcp)       ✅ engine/persistence
✅ engine/proto                 ✅ engine/pubsub
✅ engine/quest (含 integration) ✅ engine/remote (含 encryption)
✅ engine/replay                ✅ engine/room
✅ engine/router                ✅ engine/saga
✅ engine/scene                 ✅ engine/skill (含 loader)
✅ engine/syncx (含 delta)       ✅ engine/testkit
✅ engine/timer

⚠️ engine/stress                — TestStressClusterNodeFailure 60s 超时（v1.8/v1.9/v1.10 连续四版遗留，v1.10 加了 baseline.go 但根因仍未定位）

❌ engine/better/.../mongodb    — MongoDB 未连接（参考实现，不计入）
```

---

## 三、v1.10 遗留问题

### 已确认遗留项（v1.11 必须处理）

| 来源 | 项目 | 说明 | 优先级 |
|------|------|------|--------|
| v1.10 未完成 | A/B 实验统计显著性检验 | abtest.go 零 p-value/ConfidenceInterval 实现，v1.10 计划遗漏 | 🔴 高 |
| v1.10 未完成 | 回放冷数据归档 | replay/ 无归档到对象存储的能力，仅本地文件 | 🟡 中 |
| v1.10 未完成 | Helm Chart README | deploy/helm/engine/ 缺使用说明文档 | 🟢 低 |
| v1.10 未完成 | CONTRIBUTING.md | 社区贡献指引仅有 API 稳定性分级，未生成贡献者文档 | 🟢 低 |
| v1.8/9/10 连续遗留 | stress 测试根治 | TestStressClusterNodeFailure 连续四版间歇/超时失败，需根因定位而非超时调整 | 🟡 中 |

### 待改善项（v1.10 新模块集成度不足，非阻塞）

| 项目 | 说明 | 优先级 |
|------|------|--------|
| KCP + Gate 整合 | network/kcp_server.go 已实现，但 gate/ 未接入 KCP Listener 分支 | 🟡 中 |
| Delta Compression + StateSync | syncx/delta.go 独立实现，statesync.go 未启用 Delta 模式可选项 | 🟡 中 |
| 配置加载生产链路 | config/engine_config.go 实现完备，但 cmd/engine/cmd_run.go 未真正以 engine.yaml 为启动入口 | 🟡 中 |
| 日志聚合 + Dashboard 查询 | log/aggregator.go 和 dashboard/handlers_log.go 实现完备，但 RingBuffer 未接入 Dashboard WebSocket 实时推送 | 🟢 低 |
| 技能加载器与 Dashboard | skill/loader.go 支持配表，但 Dashboard 缺少技能模板管理 UI/API | 🟢 低 |
| 消息加密性能基准 | remote/encryption.go 已实现，未在 bench/ 下建立加密 vs 明文吞吐对比基线 | 🟢 低 |
| 背包交易与邮件 | trade.go 交易取消未联动 mail/ 发送补偿通知 | 🟢 低 |
| 审计日志 Web UI | audit_enhanced.go 提供 API，Dashboard 前端未有对应查询/导出页面 | 🟢 低 |

---

## 四、v1.11 新增优化方向

### 方向一：遗留清零与根治（Legacy Cleanup）

#### 1.1 A/B 实验统计显著性检验

**优先级**：🔴 高（v1.10 遗漏项，影响灰度决策科学性）

**现状**：cluster/canary/abtest.go 仅有分组统计、FNV 哈希分桶，无显著性检验。

**目标**：
- [ ] cluster/canary/statistics.go：双样本 t 检验（z-test / Welch's t-test）
- [ ] cluster/canary/statistics.go：比例检验（Chi-Square / 二项分布）
- [ ] cluster/canary/statistics.go：置信区间计算（Wilson score interval）
- [ ] cluster/canary/statistics.go：p-value 计算与显著性阈值判定（0.05 / 0.01）
- [ ] cluster/canary/abtest.go：实验结果自动推断（胜者/无显著差异/样本不足）
- [ ] dashboard/handlers_canary.go：实验分析 REST API（返回 p-value + 置信区间 + 推荐结论）
- [ ] cluster/canary/statistics_test.go：与 scipy.stats 对照的基准测试

**预期代码量**：~300-400 行

---

#### 1.2 回放冷数据归档

**优先级**：🟡 中（v1.10 遗漏项，长周期运营数据管理）

**现状**：replay/recorder.go 仅支持内存/本地落盘，缺少冷数据归档策略。

**目标**：
- [ ] replay/archive.go：ArchiveSink 接口（NewLocalArchive / NewS3Archive / NewOSSArchive）
- [ ] replay/archive.go：按时间/大小触发归档（回放文件超过 N 天或 M MB 自动上传）
- [ ] replay/archive.go：归档索引（本地保留索引，查询时按需拉取冷数据）
- [ ] replay/archive.go：压缩策略（gzip / zstd 二选一）
- [ ] dashboard/handlers_replay.go：归档管理 API（列表/下载/删除归档文件 + 本地/归档混合查询）
- [ ] replay/archive_test.go：归档上传/拉取/索引测试

**预期代码量**：~300-400 行

---

#### 1.3 Stress 测试根治（根因定位）

**优先级**：🟡 中（连续四版遗留，需彻底修复而非规避）

**现状**：v1.8/9/10 连续改进但未根治，`TestStressClusterNodeFailure` 60s 超时失败。

**目标**：
- [ ] stress/diagnostic.go：诊断钩子（Gossip 事件 + TCP 连接生命周期 + Supervisor Event 日志化）
- [ ] stress/cluster_test.go：增加阶段性检查点输出（每秒 dump 节点状态）
- [ ] stress/cluster_test.go：Gossip 收敛等待改为事件驱动（订阅 MemberChanged 而非轮询）
- [ ] stress/cluster_test.go：TCP 端口回收等待（REUSEADDR / SO_LINGER 处理）
- [ ] 根因定位后出根因报告（issue_stress_nodefailure.md）
- [ ] CI：stress/ 独立 Job，stable build tag 控制只跑核心子集

**预期代码量**：~200-300 行

---

#### 1.4 Helm Chart README + CONTRIBUTING 文档

**优先级**：🟢 低（开放友好度）

**目标**：
- [ ] deploy/helm/engine/README.md：Helm Chart 使用说明（安装 / 升级 / 卸载 / Preset 说明 / values.yaml 字段说明）
- [ ] CONTRIBUTING.md：贡献者指南（代码规范 / PR 流程 / 测试要求 / 模块负责人 / commit message 约定）
- [ ] README.md：完善顶层 README（Quick Start / 架构图 / 模块索引 / 链接到 doc/ 目录）

**预期代码量**：~150-250 行（含文档）

---

### 方向二：跨模块深度整合（Cross-Module Integration）

#### 2.1 KCP 接入 Gate 网关

**优先级**：🟡 中（v1.10 新增但未贯通到 Gate 层）

**现状**：network/kcp_server.go 实现完备，但 gate/ 未暴露 KCP 接入口。

**目标**：
- [ ] gate/gate.go：GateConfig 增加 KCPAddr 字段（与 TCPAddr / WSAddr 并列）
- [ ] gate/gate.go：KCP Listener 分支启动（复用 Agent/Processor 管线）
- [ ] gate/kcp_agent.go：KCP 专用 Agent 封装（处理 NoDelay 模式下的消息边界）
- [ ] example/demo_game：新增 KCP 客户端分支（现有 TCP/WebSocket 之外）
- [ ] gate/kcp_test.go：KCP/TCP/WebSocket 三种接入的端到端测试

**预期代码量**：~200-300 行

---

#### 2.2 Delta Compression 接入 StateSync

**优先级**：🟡 中（v1.10 新增但未启用）

**现状**：syncx/delta.go 实现独立，syncx/statesync.go 未使用。

**目标**：
- [ ] syncx/statesync.go：StateSyncRoom 增加 DeltaMode 可选项（默认关闭）
- [ ] syncx/statesync.go：DeltaMode 开启时，首包全量 + 后续每帧 Delta
- [ ] syncx/statesync.go：客户端定期请求全量重建（弱网恢复场景）
- [ ] syncx/statesync.go：Delta 压缩指标上报（Prometheus：delta_compression_ratio）
- [ ] syncx/statesync_test.go：Delta 模式端到端测试 + 带宽对比基准
- [ ] bench/：新增 Delta vs Full 状态同步基准

**预期代码量**：~250-350 行

---

#### 2.3 engine.yaml 作为引擎启动入口

**优先级**：🟡 中（v1.10 新增但未接入主启动流程）

**现状**：config/engine_config.go 可加载 YAML，但 cmd/engine/cmd_run.go 未以 YAML 为启动入口。

**目标**：
- [ ] cmd/engine/cmd_run.go：`engine run --config engine.yaml` 加载配置并启动整套引擎
- [ ] cmd/engine/cmd_init.go：`engine init` 生成默认 engine.yaml 模板
- [ ] config/engine_config.go：补齐 Gate / Dashboard / Log / Cluster 各模块字段映射
- [ ] config/engine_config.go：配置热重载（SIGHUP 触发重新加载，与 hotreload/ 集成）
- [ ] example/demo_game：改造为 engine.yaml 配置启动
- [ ] config/engine_config_test.go：端到端加载 + 启动测试

**预期代码量**：~250-350 行

---

#### 2.4 日志聚合接入 Dashboard 实时推送

**优先级**：🟢 低（v1.10 新增但实时能力未贯通）

**目标**：
- [ ] dashboard/handlers_log.go：增加 WebSocket 端点实时推送日志
- [ ] log/aggregator.go：MultiSink 增加 Dashboard WebSocket Sink
- [ ] dashboard/static/：日志实时滚动 UI（按级别/节点/TraceID 过滤）

**预期代码量**：~200-300 行

---

### 方向三：性能与可观测性加深（Performance & Observability）

#### 3.1 加密与核心路径基准对比

**优先级**：🟡 中（加密引入后性能水位需有数据支撑）

**目标**：
- [ ] bench/encryption_bench_test.go：AES-256-GCM 加密 vs 明文单消息延迟/吞吐
- [ ] bench/endpoint_bench_test.go：Remote Endpoint 加密模式 vs 非加密端到端对比
- [ ] bench/baseline.go：增加基线标签（pre-encryption / post-encryption / delta-sync）
- [ ] bench/report.go：性能报告对比图（百分位延迟 P50/P95/P99）
- [ ] CI：每次 PR 自动运行 remote/encryption + network/kcp + syncx/delta 三个关键路径基准，超过阈值自动告警

**预期代码量**：~300-400 行

---

#### 3.2 OpenTelemetry 链路追踪全链路化

**优先级**：🟡 中（现有 tracing 未全链路打通）

**目标**：
- [ ] telemetry/trace_context.go：TraceContext 跨 Actor 消息自动传播
- [ ] remote/：Envelope 携带 TraceID/SpanID（加密路径保持明文头）
- [ ] dashboard/handlers_trace.go：TraceID 查询跨节点完整调用链
- [ ] log/aggregator.go：日志自动关联 TraceID（已实现字段，需实战验证）
- [ ] testkit/：Scenario DSL 增加 AssertSpan / AssertTraceDuration 断言

**预期代码量**：~350-450 行

---

#### 3.3 热点 Actor 自动画像

**优先级**：🟢 低（定位性能热点）

**目标**：
- [ ] actor/profile.go：Actor 级消息处理耗时统计（滑动窗口 P99）
- [ ] actor/profile.go：热点 Actor 自动识别（处理时间超过阈值 / 队列堆积超过阈值）
- [ ] dashboard/handlers_profile.go：热点 Actor 查询 API（TopN 最忙 Actor）
- [ ] 与 migration/：热点 Actor 候选自动迁移建议

**预期代码量**：~300-400 行

---

### 方向四：游戏层能力完善（Game Layer）

#### 4.1 技能/Buff 系统效果链完善

**优先级**：🔴 高（v1.9 基础框架 + v1.10 ECS 集成，但效果表达力有限）

**现状**：skill/effect.go 支持伤害/治疗/Buff/AOE，但缺少条件触发、连锁技能、多阶段技能等高级表达。

**目标**：
- [ ] skill/trigger.go：条件触发器（命中/暴击/血量阈值/Buff 存在等条件触发后续效果）
- [ ] skill/chain.go：连锁技能链（释放 A 后自动触发 B，支持分支/并行）
- [ ] skill/phase.go：多阶段技能（前摇/释放/后摇/持续），每阶段独立事件
- [ ] skill/target.go：复杂选目标（扇形/锥形/自定义筛选器 + 队列）
- [ ] skill/loader.go：扩展配表支持上述新结构
- [ ] ecs/skill_system.go：Tick 驱动阶段推进和连锁触发

**预期代码量**：~500-600 行

---

#### 4.2 排行榜赛季机制

**优先级**：🟡 中（长期运营需求）

**目标**：
- [ ] leaderboard/season.go：Season 定义（开始/结束时间 + 奖励配置）
- [ ] leaderboard/season.go：赛季切换（结算上赛季 + 初始化新赛季 + 历史归档）
- [ ] leaderboard/season.go：跨赛季查询（当前赛季/历史赛季/全时排行）
- [ ] quest/integration.go：赛季结束触发邮件奖励发放
- [ ] dashboard/handlers_leaderboard.go：赛季管理 API

**预期代码量**：~300-400 行

---

#### 4.3 匹配系统进阶（ELO / MMR）

**优先级**：🟡 中（room/matcher 现为简单 FIFO/分段）

**目标**：
- [ ] room/matcher.go：ELO 评分算法（胜负后评分自动更新）
- [ ] room/matcher.go：MMR 隐分（弱化玩家对显式分段的焦虑）
- [ ] room/matcher.go：多维匹配（延迟 + 段位 + 等待时间权衡）
- [ ] room/matcher.go：匹配池动态扩容（等待时间越长匹配范围越宽）
- [ ] room/matcher_test.go：匹配质量指标测试（平均等待时间 / 公平度）

**预期代码量**：~300-400 行

---

#### 4.4 任务系统扩展（分支任务 / 条件线）

**优先级**：🟢 低（丰富任务表达力）

**目标**：
- [ ] quest/branch.go：分支任务（根据玩家选择走不同步骤）
- [ ] quest/prerequisite.go：复合前置条件（AND/OR + 声望/等级/已完成任务）
- [ ] quest/sharing.go：队伍共享任务（多人协作完成）
- [ ] quest/tracker.go：扩展支持上述特性

**预期代码量**：~250-350 行

---

### 方向五：生态与工具链（Ecosystem & Tooling）

#### 5.1 TypeScript SDK 类型安全增强

**优先级**：🟡 中（v1.9/10 已有 SDK 模板，但类型推导弱）

**目标**：
- [ ] codegen/templates_proto_sdk.go：生成强类型的消息处理器签名（RPC 风格 API）
- [ ] codegen/templates_proto_sdk.go：生成 Promise + 超时封装
- [ ] codegen/templates_proto_sdk.go：生成事件订阅器（OnPush<T>）
- [ ] example/demo_game/web_client：改造为强类型 SDK 使用示例

**预期代码量**：~250-350 行

---

#### 5.2 Unity C# SDK 实战示例

**优先级**：🟡 中（补齐 Unity 端 SDK 落地）

**目标**：
- [ ] example/unity_demo/：完整的 Unity 工程示例（角色移动 + 聊天 + 排行榜）
- [ ] example/unity_demo/：接入 KCP / TCP 两种传输
- [ ] example/unity_demo/：使用强类型 C# SDK
- [ ] codegen/templates_proto_sdk.go：C# SDK 同步加强类型推导

**预期代码量**：~400-500 行（含示例代码）

---

#### 5.3 engine doctor 诊断命令

**优先级**：🟢 低（运维友好性）

**目标**：
- [ ] cmd/engine/cmd_doctor.go：`engine doctor` 自检命令
- [ ] 检查项：配置文件合法性 + 端口可用性 + 依赖服务（etcd/consul/mongo）可达性 + 磁盘空间 + Go 版本
- [ ] 输出：文本/JSON 格式诊断报告

**预期代码量**：~200-300 行

---

## 五、优先级排序与迭代计划

### 第一批（v1.11-alpha）— 遗留清零 + 核心集成

| 序号 | 任务 | 优先级 | 预期工作量 |
|------|------|--------|-----------|
| 1 | A/B 实验统计显著性检验（v1.10 遗漏） | 🔴 高 | ~350 行 |
| 2 | 技能/Buff 系统效果链完善 | 🔴 高 | ~550 行 |
| 3 | 回放冷数据归档（v1.10 遗漏） | 🟡 中 | ~350 行 |
| 4 | Stress 测试根治（四版遗留） | 🟡 中 | ~250 行 |
| 5 | KCP 接入 Gate 网关 | 🟡 中 | ~250 行 |

### 第二批（v1.11-beta）— 深度集成 + 可观测

| 序号 | 任务 | 优先级 | 预期工作量 |
|------|------|--------|-----------|
| 6 | Delta Compression 接入 StateSync | 🟡 中 | ~300 行 |
| 7 | engine.yaml 作为引擎启动入口 | 🟡 中 | ~300 行 |
| 8 | 加密与核心路径基准对比 | 🟡 中 | ~350 行 |
| 9 | OpenTelemetry 链路追踪全链路化 | 🟡 中 | ~400 行 |
| 10 | 排行榜赛季机制 | 🟡 中 | ~350 行 |
| 11 | 匹配系统进阶（ELO / MMR） | 🟡 中 | ~350 行 |
| 12 | TypeScript SDK 类型安全增强 | 🟡 中 | ~300 行 |
| 13 | Unity C# SDK 实战示例 | 🟡 中 | ~450 行 |

### 第三批（v1.11-rc）— 文档 + 体验

| 序号 | 任务 | 优先级 | 预期工作量 |
|------|------|--------|-----------|
| 14 | Helm Chart README + CONTRIBUTING 文档 | 🟢 低 | ~200 行 |
| 15 | 日志聚合接入 Dashboard 实时推送 | 🟢 低 | ~250 行 |
| 16 | 热点 Actor 自动画像 | 🟢 低 | ~350 行 |
| 17 | 任务系统扩展（分支任务 / 条件线） | 🟢 低 | ~300 行 |
| 18 | engine doctor 诊断命令 | 🟢 低 | ~250 行 |

### 总预期新增代码量：~5,500-6,500 行

---

## 六、技术决策要点

### 6.1 A/B 统计显著性检验方案

**推荐**：Welch's t-test（不等方差）+ Wilson score 置信区间

```go
type ExperimentResult struct {
    TreatmentMean  float64
    ControlMean    float64
    PValue         float64  // < 0.05 显著
    ConfidenceInterval [2]float64  // 95% CI
    Verdict        Verdict  // Winner / NoSignificant / Underpowered
}
```

**理由**：
- Welch's t-test 对方差齐性假设更宽松，游戏 A/B 指标方差常差异显著
- Wilson score 比传统 Wald 置信区间对极端比例鲁棒
- 纯 Go 实现，零外部依赖（高斯累积分布函数可手写近似）

### 6.2 回放冷数据归档方案

**推荐**：本地索引 + 按需拉取

```
本地索引：
  replay/{session_id}/meta.json   — 保留（元数据 + 归档位置）
  replay/{session_id}/data.bin    — 超期迁移至对象存储

查询：
  1. 先查本地索引定位归档位置
  2. 按需从对象存储拉取 data.bin 到本地缓存
  3. 后续查询命中本地缓存
```

**理由**：
- 元数据本地保留让列表查询无 IO 延迟
- 按需拉取降低对象存储成本
- 本地缓存 LRU 驱逐，控制磁盘占用

### 6.3 Stress 根治方案

**推荐**：事件驱动等待 + 分层日志

```
问题猜测：
  1. Gossip 收敛慢（默认 SWIM 周期 1s，故障检测需 ~15s）
  2. TCP 端口 TIME_WAIT（测试间快速复用）
  3. Supervisor 重启慢（Backoff 策略）

对策：
  1. 订阅 cluster.MemberLeft 事件，精确等待
  2. SO_LINGER=0 强制关闭 + 动态端口
  3. 测试环境 Backoff 调为 0
  4. 每秒 dump 集群拓扑到测试日志
```

### 6.4 技能效果链方案

**推荐**：行为树节点化 + 技能 DAG

```
Skill Definition:
  Skill "冰霜新星"
    ├── Phase 0: PreCast (0.3s)
    │   └── Effect: 播放吟唱动画
    ├── Phase 1: Cast (0.1s)
    │   ├── Effect: AOE 伤害 (100 点冰伤)
    │   ├── Trigger: 若目标有"湿润" Buff
    │   │   └── Chain: 触发"冰冻"技能（stun 1s）
    │   └── Effect: 施加"缓速" Buff (3s)
    └── Phase 2: Recovery (0.5s)
```

**理由**：
- 阶段化表达自然贴合动画/音效/逻辑分离
- Trigger 条件 + Chain 链式触发让表达力等同于主流 ARPG
- Loader 配表从 JSON/RecordFile 描述完整 DAG

### 6.5 赛季机制方案

**推荐**：滚动赛季 + 冷数据快照

```
Season Lifecycle:
  Active → EndingSoon (T-7d 提示) → Settling → Archived

Settlement:
  1. 快照当前榜单为 season_{id}_final.json
  2. 发放奖励邮件（通过 quest/integration 联动）
  3. 分数衰减（继承 30%）或重置（配置化）
  4. 切换到新赛季

Archive Storage:
  历史赛季快照归档到冷存储（复用 replay/archive.go）
```

---

## 七、风险评估

| 风险 | 影响 | 应对 |
|------|------|------|
| A/B 统计实现数值稳定性 | 浮点精度问题导致 p-value 误判 | 使用数值稳定的累积方法（Welford algorithm）；与 scipy 对比验证 |
| 回放冷归档对象存储依赖 | S3/OSS 引入外部依赖 | 接口化设计，默认本地归档；对象存储通过 build tag 可选编译 |
| stress 测试根因可能在底层 TCP | 根治需动 network/ 底层代码 | 先加观测点定位；若需改底层，独立 PR 评审 |
| 技能效果链复杂度爆炸 | 配表可读性和维护成本上升 | 提供可视化编辑器（后续 v1.12 考虑）；配表校验器 + 单元测试 |
| engine.yaml 与现有代码配置冲突 | 双配置源不一致 | 明确优先级：YAML > 环境变量 > 代码默认值；冲突日志警告 |
| TypeScript/C# SDK 类型生成复杂 | 模板维护成本高 | 先做 TypeScript，验证后再做 C#；codegen 测试覆盖所有消息类型 |

---

## 八、v1.3 → v1.11 演进总览

```
v1.3（架构奠基）— ✅ 完成
└── Phase 1-4 核心功能 + Actor/集群/游戏层

v1.4（补齐加固）— ✅ 完成
└── WebSocket + Protobuf 框架 + 测试补齐 + 外部服务发现

v1.5（生产就绪）— ✅ 完成
└── Graceful + 消息追踪 + RateLimiter + ECS 调度器 + 指标

v1.6（深度优化）— ✅ 完成（94.1%）
└── Protobuf 全链路 + 弹性伸缩 + 集群单例 + 安全加固

v1.7（业务能力 + 生态）— ✅ 完成（100%）
└── 内核钩子 + Saga/ES + 房间匹配 + OpenTelemetry + 热更新

v1.8（极致性能 + 商业就绪）— ✅ 完成（100%）
└── Zero-Alloc + Live Migration + 帧/状态同步 + K8s + 自动压测

v1.9（生产加固 + 游戏深化）— ✅ 完成（96.7%）
└── 邮件持久化 + Saga 跨节点 + 定点数 + 预测/回滚 + 技能/任务 + Helm/Operator + Canary/ABTest

v1.10（深度集成 + 全链路强化）— ⚠️ 完成（15/19 = 79%）
├── ✅ 第一批 5/5：技能 ECS + KCP + 日志聚合 + 消息加密 + Protobuf SDK
├── ⚠️ 第二批 8/9：背包交易 + 任务联动 + Delta + Dashboard 告警 + RBAC + YAML 配置 + 基准回归 + （A/B 显著性缺失）
└── ⚠️ 第三批 2/5：预测平滑 + 审计合规 + （API 稳定性部分缺 CONTRIBUTING）+ （回放冷归档缺失）+ （Helm README 缺失）

v1.11（遗留清零 + 跨模块贯通）— 规划中
├── 遗留：A/B 显著性 + 回放冷归档 + stress 根治 + Helm/CONTRIBUTING 文档
├── 集成：KCP→Gate + Delta→StateSync + YAML→启动入口 + Log→Dashboard 实时
├── 性能：加密基准对比 + Trace 全链路 + 热点 Actor 画像
├── 游戏：技能效果链 + 赛季机制 + ELO 匹配 + 分支任务
├── 生态：强类型 TS SDK + Unity Demo + engine doctor
└── 目标：v1.10 遗漏项清零 + 新模块贯通到主链路 + 游戏层表达力升级
```

---

## 九、v1.10 完成度总结

| 分类 | 完成 | 未完成 | 完成度 |
|------|------|--------|--------|
| 第一批（v1.10-alpha）深度集成 | 5/5 | 0 | 100% |
| 第二批（v1.10-beta）运维深化 | 8/9 | 1 | 88.9%（A/B 显著性缺失） |
| 第三批（v1.10-rc）生态体验 | 2/5 | 3 | 40%（冷归档 / Helm README / CONTRIBUTING 均缺失） |
| **v1.10 合计** | **15/19** | **4** | **79.0%** |

**关键结论**：

- v1.10 规划 19 项新增需求完成 15 项（79.0%），4 项未完成（A/B 显著性、回放冷归档、Helm README、CONTRIBUTING）
- 连续四版遗留的 stress 测试根治仍未完成，v1.11 需根因定位而非继续调参
- 新增 10+ 模块文件（bench/ 新目录 + ecs/skill_* + inventory/trade + syncx/delta + log/aggregator + remote/encryption + middleware/rbac + config/engine_config + dashboard/alert + codegen/stability + ...）
- 构建零错误，47 个引擎测试包通过，较 v1.9 的 44 个增加 3 个（bench/ + 新增子模块）
- 代码量从 v1.9 的 ~70,374 行增长到 ~82,646 行（+17.4%，新增约 12,272 行），超出 v1.10 预期 6,000-7,500 行上限，主因是 v1.10 新模块实现密度高于预期
- v1.11 的重点从"横向扩展"转向"纵向贯通"（新模块接入主链路）+ "遗留清零"（A/B/归档/stress/文档）+ "游戏层表达力"（技能/赛季/匹配）

---

*文档版本：v1.11*
*生成时间：2026-04-17*
*基于 v1.10 需求审核生成*
*当前代码量：~82,646 行 Go 代码（不含 better/ 参考实现，462 个文件）*
