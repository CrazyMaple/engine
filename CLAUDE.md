# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Engine — 基于 Actor 模型的 Go 游戏后端引擎（`engine`），融合 Proto.Actor 的分布式能力和 Leaf 的游戏开发便利性。Go 1.24+，核心模块零外部依赖。

## Build & Test Commands

```bash
# 安装依赖
go mod tidy

# 构建全部
go build ./...

# 运行全部测试
go test ./...

# 运行单个包测试
go test ./actor/...

# 运行单个测试函数
go test ./actor/... -run TestXxx

# 运行基准测试
go test ./actor/... -bench=. -benchmem

# 代码生成工具（消息注册/TypeScript绑定）
go run codegen/cmd/msggen/main.go

# 运行示例 - 远程通信
go run example/remote_example.go server   # 节点1
go run example/remote_example.go client   # 节点2
```

## Architecture

### 消息流转路径

`ctx.Send(pid, msg)` → Envelope(含sender) → ProcessRegistry(本地) 或 RemoteProcess(远程) → 目标Actor Mailbox → Dispatcher调度 → 系统消息优先处理 → 用户消息 → Behavior函数处理

### 核心层（Phase 1-2，已完成）

- **`actor/`** — Actor 系统核心。ActorCell = Process + Context；PID 寻址（本地 `id` / 远程 `address:port/id`）；Props 构建 Actor 配置（dispatcher、mailbox、supervisor）；BehaviorStack 支持 Become/BecomeStacked 行为切换；Envelope 对象池减少 GC。
- **`remote/`** — 分布式通信，基于 TCP（非 gRPC）。EndpointManager 管理连接池和自动重连；RemoteProcess 实现 Process 接口做远程代理；TypeRegistry 做消息类型序列化注册；支持 HMAC 签名和 TLS。
- **`network/`** — TCP/WebSocket 传输层，长度分帧消息解析（MsgParser），连接池管理。
- **`internal/`** — MPSC 无锁队列（mailbox 底层）。

### 集群层（Phase 3）

- **`cluster/`** — Gossip 协议拓扑管理，一致性哈希路由。
- **`grain/`** — 虚拟 Actor 模式，(Kind, Identity) 定位，按需激活。
- **`router/`** — 路由策略：Broadcast、RoundRobin、ConsistentHash。
- **`pubsub/`** — 基于 Topic 的发布订阅。

### 游戏引擎层（Phase 4）

- **`scene/`** — 场景管理 + Grid 空间分区（AOI 兴趣区域）。
- **`ecs/`** — Entity Component System，World/Entity/Component 抽象。
- **`config/`** — RecordFile（Tab 分隔）和 JSON 配置加载。
- **`persistence/`** — 状态持久化，Storage 接口支持 MemoryStorage 和 MongoStorage。

### 基础设施

- **`gate/`** — 客户端网关（TCP/WebSocket），Agent 处理连接，Processor 路由消息。
- **`codec/`** — 消息编解码（JSON 实现）。
- **`middleware/`** — Actor 消息管道装饰器（日志、指标、ACL、签名验证）。
- **`dashboard/`** — Web 运维面板，REST API + HotActor 热更新。
- **`timer/`** — 定时器系统。
- **`codegen/`** — 从 `//msggen:message` 注解的 Go struct 生成注册代码和 TypeScript 类型。
- **`better/`** — 参考实现（vendored Leaf 和 ProtoActor 源码），不直接编译进项目。

## Key Design Patterns

- **Actor-First**: 一切皆 Actor，所有交互通过消息传递
- **位置透明**: 本地/远程 Actor 使用相同 `ctx.Send(pid, msg)` API
- **监管树**: 父 Actor 监管子 Actor 故障，Directive 策略（Resume/Restart/Stop/Escalate）
- **Middleware 链**: 可堆叠的消息处理装饰器
- **Props 构建模式**: Actor 配置蓝图（dispatcher、mailbox、supervisor strategy）
- **Envelope 对象池**: 消息封装复用，减少 GC 压力
