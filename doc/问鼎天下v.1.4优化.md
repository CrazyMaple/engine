# 问鼎天下 v1.4 优化计划

> 基于 v1.3 架构设计的需求审核，识别未完成项并规划下一阶段迭代
> 审核日期：2026-04-02
> 当前代码量：~67,000 行 Go 代码（不含 better/ 参考实现）

---

## 一、v1.3 需求完成度审核

### Phase 1：核心 Actor 引擎 — ✅ 完全完成

| 需求项 | 状态 | 说明 |
|--------|------|------|
| Actor 接口 + Context（5个核心接口） | ✅ | 6个子接口：Info/Message/Sender/Receiver/Spawner + 复合 Context |
| PID + Process + ProcessRegistry | ✅ | 本地+远程寻址支持 |
| Props + Spawner | ✅ | Actor 工厂模式 |
| Mailbox + MPSC 无锁队列 | ✅ | defaultMailbox + internal/queue |
| Dispatcher | ✅ | Goroutine 和 Synchronous 两种实现 |
| Supervision 策略 | ✅ | OneForOneStrategy |
| 生命周期消息 | ✅ | Started/Stopping/Stopped/Restarting |
| Behavior Stack | ✅ | Become/BecomeStacked/UnbecomeStacked |
| Future/Promise | ✅ | 带超时机制 |
| EventStream | ✅ | 发布-订阅事件总线 |
| DeadLetter | ✅ | 死信处理 |
| Gate 模块（TCP） | ✅ | TCP 接入，WebSocket 预留 |
| MsgParser 消息分帧 | ✅ | 可配置长度字段、字节序、FNV 校验 |
| Agent 模式 | ✅ | 每连接一个 Agent |
| Timer/CronExpr | ✅ | AfterFunc/CronFunc + Cron 表达式 |
| Console 运维模块 | ✅ | 控制台命令支持 |
| 轻量级编解码层 | ✅ | Codec 接口 + JSON 实现 |
| Actor 地址设计（预留远程） | ✅ | PID.Address 字段 |
| Envelope 对象池 | ✅ | MessageEnvelope/Buffer/PID 对象池 |

### Phase 2：轻量级分布式 — ✅ 完全完成

| 需求项 | 状态 | 说明 |
|--------|------|------|
| Remote 模块（自研 TCP 协议，非 gRPC） | ✅ | JSON 序列化 + 长度分帧 |
| EndpointManager | ✅ | 端点生命周期管理 |
| Endpoint 自动重连 | ✅ | 可配置间隔（默认3秒） |
| RemoteProcess | ✅ | 用户/系统消息转发 |
| PID 扩展（Address 远程寻址） | ✅ | 本地/远程透明 |
| 位置透明 Send/Request | ✅ | ProcessRegistry 自动路由 |
| 连接池管理 | ✅ | TCPClient 连接池 |
| 消息批处理 | ✅ | 最多64条批量发送 |
| HMAC 签名 | ✅ | HMAC-SHA256 |
| TLS 加密 | ✅ | 双向 TLS 支持 |
| TypeRegistry 类型注册 | ✅ | 消息类型序列化注册 |

### Phase 3：集群管理 — ✅ 基本完成

| 需求项 | 状态 | 说明 |
|--------|------|------|
| Cluster 模块 | ✅ | 成员管理 + 拓扑事件 |
| Gossip 协议 | ✅ | 状态同步 + 心跳检测 |
| 成员状态管理 | ✅ | Alive/Suspect/Dead 转移 |
| 一致性哈希 | ✅ | Rendezvous Hashing (HRW) |
| Router - Broadcast | ✅ | 广播路由 |
| Router - RoundRobin | ✅ | 轮询路由（原子计数器） |
| Router - ConsistentHash | ✅ | 一致性哈希路由 |
| 虚拟 Actor（Grain） | ✅ | Kind+Identity 定位，自动激活/去激活，TTL |
| PubSub 发布订阅 | ✅ | 基于 Topic Grain 实现 |
| 拓扑变更通知 | ✅ | ClusterTopologyEvent |
| Provider 接口 | ✅ | 可插拔成员发现（仅 AutoManaged） |

### Phase 4：游戏引擎层 — ✅ 大部分完成

| 需求项 | 状态 | 说明 |
|--------|------|------|
| Scene Actor 场景管理 | ✅ | SceneManager + SceneActor |
| AOI 兴趣区域（九宫格） | ✅ | Grid 空间索引 |
| ECS 实体组件系统 | ✅ | World/Entity/Component |
| 数据持久化 - Storage 接口 | ✅ | Save/Load/Delete |
| 数据持久化 - MemoryStorage | ✅ | 完整实现 |
| 数据持久化 - MongoStorage | ⚠️ | 基础实现，连接管理待完善 |
| PersistenceMiddleware | ✅ | 自动保存/恢复状态 |
| RecordFile 配置表加载 | ✅ | TSV 解析 + 索引 + 类型转换 |
| ConfigManager | ✅ | 配置加载管理 |
| Codegen 消息注册代码生成 | ✅ | Go + TypeScript 生成 |
| 中间件 - 日志 | ✅ | 消息类型记录 + 耗时统计 |
| 中间件 - 指标 | ✅ | 消息计数 + 延迟追踪 |
| 中间件 - ACL | ✅ | 访问控制 |
| 中间件 - 签名 | ✅ | HMAC 签名验证 |
| 中间件 - Chain 组合 | ✅ | 链式装配 |
| **WebSocket Gate 接入** | ❌ | Gate 仅 TCP，WebSocket 未实现 |
| **Protobuf 编解码** | ❌ | 仅 JSON 实现，缺 Protobuf |
| **配置热重载** | ❌ | ConfigManager 预留但未实现 |

### Phase 5：生产加固 — ⚠️ 部分完成

| 需求项 | 状态 | 说明 |
|--------|------|------|
| Envelope 对象池 | ✅ | MessageEnvelope/Buffer/PID |
| 消息批处理 | ✅ | Remote 层批量发送 |
| Dashboard Web 面板 | ✅ | REST API + Actor 信息 |
| HotActor 热点追踪 | ✅ | 高频 Actor 检测 |
| TLS 加密通信 | ✅ | Network + Remote |
| ACL 权限控制 | ✅ | 中间件实现 |
| 消息签名验证 | ✅ | HMAC-SHA256 |
| 压力测试框架 | ✅ | cluster_test + connection_test |
| **基准测试套件** | ⚠️ | 仅 Remote 有 benchmark，其他模块缺失 |
| **集群状态可视化** | ⚠️ | Dashboard 基础，缺详细拓扑图 |
| **外部服务发现** | ❌ | 仅 AutoManaged，无 Consul/etcd/K8s |
| **Actor 拓扑查看** | ⚠️ | 基础实现，缺父子层级展示 |
| **GC/Goroutine 运行时指标** | ❌ | Dashboard 缺运行时性能数据 |
| **压力测试稳定性** | ❌ | TestStressClusterNodeFailure 失败 |

---

## 二、当前构建与测试状态

```
构建状态：✅ go build ./... 通过（零错误）

测试状态：
✅ engine/actor           — 通过
✅ engine/cluster          — 通过
✅ engine/codegen          — 通过
✅ engine/config           — 通过
✅ engine/dashboard        — 通过
✅ engine/ecs              — 通过
✅ engine/grain            — 通过
✅ engine/middleware        — 通过
✅ engine/network          — 通过
✅ engine/persistence      — 通过
✅ engine/pubsub           — 通过
✅ engine/remote           — 通过（无测试用例）
✅ engine/router           — 通过
✅ engine/scene            — 通过
❌ engine/stress           — TestStressClusterNodeFailure 超时失败
❌ engine/better/.../mongodb — MongoDB 未连接（参考实现，非核心）
⚠️ engine/timer            — 无测试文件
⚠️ engine/gate             — 无测试文件
⚠️ engine/codec            — 无测试文件
```

---

## 三、v1.4 优化方向

基于审核结果，v1.4 聚焦三个方向：**补齐短板**、**质量加固**、**开发体验**。

---

### 方向一：补齐功能短板

#### 1.1 WebSocket Gate 接入

**优先级**：🔴 高（游戏客户端刚需，H5/微信小游戏必需）

**现状**：Gate 模块仅支持 TCP，WebSocket 预留但未实现。

**目标**：
- [ ] 在 `gate/` 模块中实现 WebSocket 接入
- [ ] 复用现有 `network/` 层的连接抽象
- [ ] 支持 `ws://` 和 `wss://`（TLS）
- [ ] 与 TCP Gate 共享 Processor/Agent 逻辑
- [ ] 支持消息分帧（与 TCP MsgParser 兼容的二进制消息格式）
- [ ] 支持心跳/Ping-Pong 保活

**关键设计**：
```
Gate
├── TCPGate   (已有)
├── WSGate    (新增)
└── Processor (共享)
```

**预期代码量**：~300-500 行

---

#### 1.2 Protobuf 编解码支持

**优先级**：🔴 高（生产环境性能和跨语言刚需）

**现状**：Codec 仅有 JSON 实现，生产环境 JSON 序列化性能不足，且无法与 C#/TypeScript 客户端高效通信。

**目标**：
- [ ] 实现 `ProtobufCodec`，实现 `Codec` 接口
- [ ] Remote 层消息序列化从 JSON 切换到 Protobuf（可配置）
- [ ] 定义核心消息的 `.proto` 文件（RemoteMessage、系统消息等）
- [ ] Codegen 支持从 `.proto` 文件生成路由代码

**预期代码量**：~500-800 行

---

#### 1.3 配置热重载

**优先级**：🟡 中（线上运营期间调整配置不停服）

**现状**：ConfigManager 存在但热重载未实现。

**目标**：
- [ ] 实现文件监听（fsnotify 或轮询）
- [ ] 配置变更通知机制（EventStream 或回调）
- [ ] 原子替换配置对象（读写锁保护）
- [ ] RecordFile 支持增量重载

**预期代码量**：~200-400 行

---

#### 1.4 MongoStorage 完善

**优先级**：🟡 中（持久化层生产可用）

**现状**：MongoStorage 基础实现，连接管理和错误处理待完善。

**目标**：
- [ ] 连接池管理（连接超时、最大连接数）
- [ ] 重试机制（网络抖动场景）
- [ ] 批量操作支持（BulkSave/BulkLoad）
- [ ] 索引自动创建
- [ ] 完善错误定义和处理

**预期代码量**：~200-300 行

---

### 方向二：质量加固

#### 2.1 测试覆盖补齐

**优先级**：🔴 高（核心模块无测试 = 重构时无安全网）

**现状**：gate、timer、codec、remote 缺少测试用例。

**目标**：
- [ ] `gate/` — Gate 网关集成测试（TCP 连接、消息路由、Agent 生命周期）
- [ ] `timer/` — Dispatcher 单元测试（AfterFunc 精度、CronFunc 调度、取消）
- [ ] `codec/` — JSONCodec 编解码测试（正常/边界/错误场景）
- [ ] `remote/` — 远程通信测试（连接、断线重连、消息收发、签名验证）
- [ ] 修复 `stress/TestStressClusterNodeFailure` 超时问题

**预期代码量**：~1000-1500 行测试代码

---

#### 2.2 基准测试套件

**优先级**：🟡 中（性能基线，防止回归）

**现状**：仅 Remote 有 benchmark，核心路径缺少基准。

**目标**：
- [ ] `actor/` — Spawn 速度、Send 吞吐、Request-Response 延迟
- [ ] `internal/` — MPSC Queue Push/Pop 吞吐
- [ ] `mailbox/` — 消息投递吞吐（系统消息 vs 用户消息）
- [ ] `cluster/` — Gossip 收敛时间、一致性哈希查询速度
- [ ] `scene/` — AOI Grid 查询性能（不同实体密度）
- [ ] `codec/` — JSON 编解码吞吐（对比 Protobuf 后）

**预期代码量**：~500-800 行

---

#### 2.3 错误处理规范化

**优先级**：🟡 中（生产环境可观测性）

**目标**：
- [ ] 定义引擎级错误类型（`engine/errors` 包）
- [ ] Remote 连接错误分类（ConnectError/TimeoutError/AuthError）
- [ ] Cluster 状态错误分类（JoinError/SplitBrainError）
- [ ] 错误包装（`fmt.Errorf` 或 `errors.Join`），保留调用链
- [ ] 关键路径添加结构化日志（替代 `log.Printf`）

**预期代码量**：~300-500 行

---

### 方向三：开发体验优化

#### 3.1 Dashboard 增强

**优先级**：🟢 低（运维体验提升）

**目标**：
- [ ] 运行时指标页面（GC 次数/暂停时间、Goroutine 数量、内存使用）
- [ ] Actor 父子层级拓扑图（树形展示）
- [ ] Mailbox 队列深度监控
- [ ] 集群成员状态实时刷新
- [ ] 简单的 Web UI（HTML + JS，无前端框架依赖）

**预期代码量**：~500-800 行

---

#### 3.2 开发文档与示例完善

**优先级**：🟢 低（降低团队上手门槛）

**目标**：
- [ ] 补充 `example/` 目录：
  - WebSocket Gate 接入示例
  - Grain 虚拟 Actor 使用示例
  - PubSub 发布订阅示例
  - 持久化存储示例
  - 中间件组合示例
- [ ] API 文档注释补齐（核心公开接口）
- [ ] 架构图更新（反映当前代码结构）

---

#### 3.3 AllForOne 监管策略

**优先级**：🟢 低（当前仅 OneForOne，部分场景需要联动重启）

**目标**：
- [ ] 实现 `AllForOneStrategy`（一个子 Actor 失败，重启所有兄弟）
- [ ] 自定义 Decider 函数支持
- [ ] 窗口期重启限制（MaxRetries + WithinDuration）

**预期代码量**：~100-200 行

---

## 四、优先级排序与迭代计划

### 第一批（v1.4-alpha）— 功能补齐

| 序号 | 任务 | 优先级 | 预期工作量 |
|------|------|--------|-----------|
| 1 | WebSocket Gate 接入 | 🔴 高 | ~400 行 |
| 2 | 核心模块测试补齐 | 🔴 高 | ~1200 行 |
| 3 | Protobuf 编解码 | 🔴 高 | ~600 行 |
| 4 | 修复压力测试 | 🔴 高 | ~100 行 |

### 第二批（v1.4-beta）— 质量加固

| 序号 | 任务 | 优先级 | 预期工作量 |
|------|------|--------|-----------|
| 5 | 配置热重载 | 🟡 中 | ~300 行 |
| 6 | MongoStorage 完善 | 🟡 中 | ~250 行 |
| 7 | 基准测试套件 | 🟡 中 | ~600 行 |
| 8 | 错误处理规范化 | 🟡 中 | ~400 行 |

### 第三批（v1.4-rc）— 体验优化

| 序号 | 任务 | 优先级 | 预期工作量 |
|------|------|--------|-----------|
| 9 | Dashboard 增强 | 🟢 低 | ~600 行 |
| 10 | 示例与文档完善 | 🟢 低 | ~500 行 |
| 11 | AllForOne 监管策略 | 🟢 低 | ~150 行 |

### 总预期新增代码量：~5,000-6,000 行

---

## 五、技术决策要点

### 5.1 WebSocket 实现方案

**推荐**：使用 `golang.org/x/net/websocket` 或 `nhooyr.io/websocket`（轻量，无 CGO）

**理由**：
- `gorilla/websocket` 已归档（不再维护）
- `nhooyr.io/websocket` 支持 `context.Context`，API 更现代
- 保持"最少依赖"原则，选择最轻量的方案

### 5.2 Protobuf 集成策略

**推荐**：可选依赖，不强制

**理由**：
- 保持 Codec 接口的可插拔性
- 开发阶段用 JSON（可读性好），生产切 Protobuf（性能好）
- Remote 层序列化格式通过 RemoteConfig 配置

### 5.3 配置热重载方案

**推荐**：文件轮询（非 fsnotify）

**理由**：
- fsnotify 在 Docker/K8s 环境下有已知问题（ConfigMap 更新不触发事件）
- 轮询方案虽不够实时，但在所有环境下表现一致
- 游戏配置更新频率低（分钟级），轮询完全够用

---

## 六、风险评估

| 风险 | 影响 | 应对 |
|------|------|------|
| WebSocket 引入新依赖 | 违背零依赖原则 | 优先考虑标准库 `net/http` + 手动 Upgrade |
| Protobuf 增加构建复杂度 | 需要 protoc 工具链 | 作为可选编解码器，不影响核心构建 |
| 测试补齐工作量大 | 延误功能开发 | 优先覆盖核心路径，边界场景后续补充 |
| 压力测试不稳定 | 掩盖真实 bug | 分析 TestStressClusterNodeFailure 根因，区分测试问题和引擎问题 |

---

*文档版本：v1.4*
*生成时间：2026-04-02*
*基于 v1.3 需求审核生成*
