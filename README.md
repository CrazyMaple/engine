# QMMind - 游戏后端Actor引擎

基于Actor模型的轻量级游戏后端引擎，融合了Proto.Actor的分布式能力和Leaf的游戏开发便利性。

## 项目状态

### ✅ Phase 1: 核心Actor引擎 (已完成)
- Actor接口与Context
- PID与Process抽象
- Mailbox消息队列
- Dispatcher调度器
- Supervision监管策略
- Behavior行为栈
- Future/Promise异步等待
- EventStream事件总线
- Gate客户端接入
- Timer定时器系统

### ✅ Phase 2: 轻量级分布式 (已完成)
- Remote模块（TCP协议）
- EndpointManager连接管理
- RemoteProcess远程代理
- PID远程寻址扩展
- 位置透明消息路由
- 自动重连机制
- Registry节点注册发现

### 🔄 Phase 3: 集群管理 (规划中)
- Gossip协议
- 虚拟Actor (Grain)
- 路由器 (Broadcast/ConsistentHash)
- PubSub发布订阅

### 🔄 Phase 4: 游戏引擎层 (规划中)
- Scene场景管理
- AOI兴趣区域
- ECS实体组件系统
- 配置管理
- 数据持久化

## 快速开始

### 安装依赖
```bash
go mod tidy
```

### 运行示例

#### 1. 启动服务器节点
```bash
go run example/remote_example.go server
```

#### 2. 启动客户端节点
```bash
go run example/remote_example.go client
```

客户端会向服务器发送10次Ping消息，服务器回复Pong消息，演示跨节点通信。

## 核心特性

### 1. Actor模型
- 每个Actor独立的消息邮箱
- 消息驱动的并发模型
- 父子监管树
- 生命周期管理

### 2. 分布式通信
- 位置透明：代码不区分本地/远程Actor
- 自动重连：连接断开后自动恢复
- 轻量级：基于TCP，无gRPC依赖
- 高性能：MPSC无锁队列

### 3. 游戏开发友好
- 内置客户端接入（TCP/WebSocket）
- 消息分帧与编解码
- 定时器系统
- 运维控制台

## 项目结构

```
.
├── actor/          # Actor核心系统
├── remote/         # 远程通信模块
├── network/        # 网络层（TCP/WebSocket）
├── gate/           # 客户端接入网关
├── codec/          # 消息编解码
├── timer/          # 定时器系统
├── log/            # 日志系统
├── console/        # 运维控制台
├── internal/       # 内部工具（MPSC队列等）
├── example/        # 示例程序
└── doc/            # 文档
```

## 文档

- [Phase 2: 轻量级分布式](doc/phase2_remote.md)
- [架构设计文档](doc/问鼎天下v.1.3优化.md)

## 设计原则

1. **Actor-First**: 一切皆Actor
2. **消息驱动**: 所有交互通过消息传递
3. **位置透明**: 代码不区分本地/远程
4. **故障隔离**: Actor崩溃不影响其他Actor
5. **渐进式分布式**: 单节点→多节点，代码不变
6. **最少依赖**: 核心引擎零外部依赖

## 技术栈

- Go 1.24+
- 无外部依赖（核心模块）
- WebSocket支持：gorilla/websocket

## 开发路线

详见 [架构设计文档](doc/问鼎天下v.1.3优化.md) 中的迭代路线图。

## License

MIT
