# Leaf 游戏服务器框架源码分析

> 版本: 1.1.3 | 语言: Go | 作者: name5566

## 一、框架总览

Leaf 是一个轻量级 Go 语言游戏服务器框架，采用模块化设计，核心思想是**每个模块运行在独立 goroutine 中，模块间通过 ChanRPC 通信**，避免共享内存带来的并发问题。

### 目录结构

```
leaf-master/
├── leaf.go              # 框架入口，启动/关闭流程
├── version.go           # 版本号 (1.1.3)
├── conf/                # 全局配置
├── module/              # 模块系统 (Module + Skeleton)
├── chanrpc/             # 基于 channel 的 RPC 机制
├── gate/                # 网关模块 (TCP/WebSocket 接入)
├── network/             # 网络层 (TCP/WebSocket/消息处理器)
│   ├── json/            # JSON 消息处理器
│   └── protobuf/        # Protobuf 消息处理器
├── go/                  # 安全 goroutine 管理
├── timer/               # 定时器与 Cron 表达式
├── console/             # 运维控制台 (TCP 命令行)
├── cluster/             # 集群通信 (节点间 TCP)
├── db/mongodb/          # MongoDB 连接池封装
├── recordfile/          # CSV/TSV 配置表读取
├── log/                 # 分级日志系统
└── util/                # 工具集 (DeepCopy/Map/Rand/Semaphore)
```

### 启动流程 (`leaf.go`)

```
leaf.Run(mods...)
  ├── 1. 初始化日志系统
  ├── 2. 注册并初始化所有 Module (顺序调用 OnInit)
  ├── 3. 每个 Module 在独立 goroutine 中运行 Run()
  ├── 4. 初始化 Cluster (节点间通信)
  ├── 5. 初始化 Console (运维控制台)
  ├── 6. 阻塞等待系统信号 (Interrupt/Kill)
  └── 7. 逆序销毁: Console → Cluster → Module
```

---

## 二、模块系统 (`module/`)

### 2.1 Module 接口 (`module.go`)

```go
type Module interface {
    OnInit()
    OnDestroy()
    Run(closeSig chan bool)
}
```

模块生命周期管理：
- **Register**: 注册模块到全局 `mods` 切片
- **Init**: 顺序调用每个模块的 `OnInit()`，然后为每个模块启动独立 goroutine 执行 `Run()`
- **Destroy**: **逆序**关闭模块（通过 `closeSig` 通知），等待 goroutine 退出后调用 `OnDestroy()`

### 2.2 Skeleton 骨架 (`skeleton.go`)

Skeleton 是模块的"骨架"，为模块提供开箱即用的基础设施：

| 组件 | 类型 | 作用 |
|------|------|------|
| `g` | `*g.Go` | 安全 goroutine 管理 |
| `dispatcher` | `*timer.Dispatcher` | 定时器调度 |
| `server` | `*chanrpc.Server` | ChanRPC 服务端 |
| `client` | `*chanrpc.Client` | ChanRPC 异步调用客户端 |
| `commandServer` | `*chanrpc.Server` | 控制台命令处理 |

**核心事件循环** (`Run` 方法) — 单 goroutine 的 select 多路复用：

```go
select {
case <-closeSig:           // 关闭信号
case ri := <-client.ChanAsynRet:  // 异步 RPC 回调
case ci := <-server.ChanCall:     // RPC 请求处理
case ci := <-commandServer.ChanCall: // 控制台命令
case cb := <-g.ChanCb:           // goroutine 回调
case t := <-dispatcher.ChanTimer: // 定时器触发
}
```

提供的便捷方法：
- `AfterFunc(d, cb)` — 延时执行
- `CronFunc(cronExpr, cb)` — Cron 定时任务
- `Go(f, cb)` — 安全 goroutine
- `AsynCall(server, id, args...)` — 异步 RPC
- `RegisterChanRPC(id, f)` — 注册 RPC 处理函数
- `RegisterCommand(name, help, f)` — 注册控制台命令

---

## 三、ChanRPC 模块 (`chanrpc/`)

基于 Go channel 实现的轻量级 RPC 系统，是模块间通信的核心机制。

### 3.1 核心结构

```
Server                          Client
┌─────────────────────┐        ┌──────────────────────┐
│ functions: map[id]f │        │ chanSyncRet: chan     │  ← 同步调用返回
│ ChanCall:  chan      │◄───────│ ChanAsynRet: chan     │  ← 异步调用返回
└─────────────────────┘        │ pendingAsynCall: int  │
                               └──────────────────────┘
```

### 3.2 三种调用模式

| 模式 | 方法 | 特点 |
|------|------|------|
| 单向发送 | `Server.Go(id, args...)` | 无返回值，不阻塞，fire-and-forget |
| 同步调用 | `Call0/Call1/CallN` | 阻塞等待返回，支持 0/1/N 个返回值 |
| 异步调用 | `Client.AsynCall(id, args..., cb)` | 非阻塞，结果通过回调函数返回 |

### 3.3 函数签名约定

注册的处理函数必须是以下三种签名之一：
```go
func(args []interface{})                // 无返回值 → Call0
func(args []interface{}) interface{}    // 单返回值 → Call1
func(args []interface) []interface{}  // 多返回值 → CallN
```

异步回调函数签名：
```go
func(err error)                        // 对应 Call0
func(ret interface{}, err error)       // 对应 Call1
func(ret []interface{}, err error)     // 对应 CallN
```

### 3.4 线程安全性

- **Server**: 非 goroutine 安全，一个 Server 只能在一个 goroutine 中使用
- **Client**: 非 goroutine 安全
- `Server.Go()` 和 `Server.Call0/1/N()` 是 goroutine 安全的（通过 channel 通信）

---

## 四、网络层 (`network/`)

### 4.1 接口定义

```go
// 连接抽象
type Conn interface {
    ReadMsg() ([]byte, error)
    WriteMsg(args ...[]byte) error
    LocalAddr() net.Addr
    RemoteAddr() net.Addr
    Close()
    Destroy()
}

// 网络代理 (每个连接一个)
type Agent interface {
    Run()
    OnClose()
}

// 消息处理器
type Processor interface {
    Route(msg interface{}, userData interface{}) error
    Unmarshal(data []byte) (interface{}, error)
    Marshal(msg interface{}) ([][]byte, error)
}
```

### 4.2 TCP 实现

**TCPServer** (`tcp_server.go`):
- 监听指定地址，Accept 新连接
- 连接数上限控制 (`MaxConnNum`)
- 临时错误指数退避重试 (5ms → 1s)
- 每个连接创建 `TCPConn` + `Agent`，在独立 goroutine 运行

**TCPClient** (`tcp_client.go`):
- 支持多连接 (`ConnNum`)
- 自动重连 (`AutoReconnect`)
- 可配置重连间隔 (`ConnectInterval`)

**TCPConn** (`tcp_conn.go`):
- 写操作通过 `writeChan` 异步化（独立写 goroutine）
- 写缓冲满时直接销毁连接（防止慢客户端阻塞）
- `Close()` 优雅关闭 vs `Destroy()` 强制关闭 (SetLinger(0))

**MsgParser** (`tcp_msg.go`) — TCP 消息协议：
```
┌──────────┬──────────┐
│ len (1/2/4 bytes) │ data     │
└──────────┴──────────┘
```
- 支持 1/2/4 字节长度头
- 支持大端/小端字节序
- 消息长度校验 (min/max)

### 4.3 WebSocket 实现

**WSServer** (`ws_server.go`):
- 基于 `gorilla/websocket`
- 支持 TLS (CertFile/KeyFile)
- HTTP Upgrade 处理
- 连接数上限控制

**WSClient** (`ws_client.go`):
- 与 TCPClient 对称设计
- 支持自动重连

**WSConn** (`ws_conn.go`):
- 与 TCPConn 类似的异步写机制
- 使用 BinaryMessage 类型
- 消息长度校验

### 4.4 消息处理器

#### Protobuf 处理器 (`network/protobuf/`)

消息格式：
```
┌────────────────┬─────────────────────┐
│ id (2 bytes)   │ protobuf message    │
└────────────────┴─────────────────────┘
```

- 消息通过 `Register()` 注册，自动分配递增 uint16 ID
- 支持三种路由方式：`msgRouter` (ChanRPC)、`msgHandler` (直接回调)、`msgRawHandler` (原始数据回调)
- 最大支持 65535 种消息类型

#### JSON 处理器 (`network/json/`)

消息格式：
```json
{"MsgName": { ...msg fields... }}
```

- 以结构体名称作为消息 ID
- 同样支持 Router/Handler/RawHandler 三种路由
- JSON 数据中只能有一个顶层 key

---

## 五、Gate 网关模块 (`gate/`)

Gate 是连接客户端与服务器逻辑的桥梁。

### 5.1 Agent 接口 (`agent.go`)

```go
type Agent interface {
    WriteMsg(msg interface{})
    LocalAddr() net.Addr
    RemoteAddr() net.Addr
    Close()
    Destroy()
    UserData() interface{}
    SetUserData(data interface{})
}
```

### 5.2 Gate 结构 (`gate.go`)

Gate 同时支持 TCP 和 WebSocket：

```go
type Gate struct {
    MaxConnNum, PendingWriteNum int
    MaxMsgLen                   uint32
    Processor                   network.Processor  // JSON 或 Protobuf
    AgentChanRPC                *chanrpc.Server     // 通知游戏逻辑

    // WebSocket 配置
    WSAddr, CertFile, KeyFile string
    HTTPTimeout               time.Duration

    // TCP 配置
    TCPAddr      string
    LenMsgLen    int
    LittleEndian bool
}
```

消息流转：
```
客户端 → conn.ReadMsg() → Processor.Unmarshal() → Processor.Route() → 游戏逻辑
游戏逻辑 → agent.WriteMsg() → Processor.Marshal() → conn.WriteMsg() → 客户端
```

连接生命周期事件通过 ChanRPC 通知：
- 新连接: `AgentChanRPC.Go("NewAgent", agent)`
- 断开: `AgentChanRPC.Call0("CloseAgent", agent)`

---

## 六、Go 模块 (`go/`)

安全的 goroutine 管理，确保回调在模块主 goroutine 中执行。

### 6.1 基本模式

```go
// f 在新 goroutine 执行，cb 回到主 goroutine 执行
skeleton.Go(func() {
    // 耗时操作 (如数据库查询)
}, func() {
    // 回调，在模块主 goroutine 中安全执行
})
```

工作原理：
1. `Go(f, cb)` 启动新 goroutine 执行 `f`
2. `f` 完成后将 `cb` 发送到 `ChanCb`
3. Skeleton 事件循环从 `ChanCb` 取出 `cb` 并执行

### 6.2 LinearContext — 顺序执行上下文

保证多个 Go 调用按提交顺序执行（通过 `mutexExecution` 互斥锁串行化）：

```go
ctx := skeleton.NewLinearContext()
ctx.Go(f1, cb1)  // 先执行
ctx.Go(f2, cb2)  // f1 完成后才执行 f2
```

适用场景：需要保证操作顺序的异步任务（如同一玩家的多次数据库操作）。

---

## 七、Timer 模块 (`timer/`)

### 7.1 Dispatcher 定时器调度

```go
// 延时执行
timer := dispatcher.AfterFunc(5*time.Second, func() {
    // 5秒后在模块主 goroutine 中执行
})
timer.Stop() // 取消

// Cron 定时任务
cron := dispatcher.CronFunc(cronExpr, func() {
    // 按 cron 表达式周期执行
})
cron.Stop() // 取消
```

工作原理：使用 `time.AfterFunc` 在到期时将 Timer 发送到 `ChanTimer`，由 Skeleton 事件循环执行回调。

### 7.2 CronExpr — Cron 表达式解析

支持标准 Cron 格式（5 或 6 字段）：

```
秒(可选) 分 时 日 月 周
```

特殊字符：`*`（任意）、`,`（列表）、`-`（范围）、`/`（步长）

内部使用 uint64 位图高效匹配时间字段。`Next(t)` 方法计算下一个触发时间。

---

## 八、Console 控制台 (`console/`)

通过 TCP 连接提供运维命令行界面。

### 8.1 内置命令

| 命令 | 功能 |
|------|------|
| `help` | 显示所有可用命令 |
| `cpuprof start/stop` | CPU 性能分析 (pprof) |
| `prof goroutine/heap/thread/block` | 运行时 profile 快照 |
| `quit` | 退出控制台连接 |

### 8.2 自定义命令

通过 `Skeleton.RegisterCommand()` 注册，命令处理函数在模块主 goroutine 中执行（通过 ChanRPC）：

```go
skeleton.RegisterCommand("status", "show server status", func(args []interface{}) interface{} {
    return "online players: 100"
})
```

---

## 九、Cluster 集群 (`cluster/`)

节点间 TCP 通信的基础框架（当前为骨架实现）：

- 根据 `conf.ListenAddr` 启动 TCP Server 监听其他节点连接
- 根据 `conf.ConnAddrs` 主动连接其他节点
- 使用 4 字节消息长度头，最大消息 4GB
- Agent 的 `Run()` 和 `OnClose()` 当前为空实现，需业务层扩展

---

## 十、数据库 (`db/mongodb/`)

MongoDB 连接池封装，基于 `mgo.v2`。

### 核心设计 — Session 堆

使用最小堆管理 Session，引用计数最少的 Session 优先分配：

```go
s := dialContext.Ref()    // 获取 session (引用计数+1)
defer dialContext.UnRef(s) // 归还 session (引用计数-1)
// 使用 s 进行数据库操作
```

### 便捷方法

| 方法 | 功能 |
|------|------|
| `Dial(url, sessionNum)` | 创建连接池 |
| `EnsureCounter(db, col, id)` | 确保自增计数器存在 |
| `NextSeq(db, col, id)` | 获取下一个自增序号 |
| `EnsureIndex(db, col, key)` | 创建普通索引 |
| `EnsureUniqueIndex(db, col, key)` | 创建唯一索引 |

所有方法均为 goroutine 安全。

---

## 十一、RecordFile 配置表 (`recordfile/`)

CSV/TSV 格式的配置表读取工具。

- 默认分隔符: Tab (`\t`)，注释符: `#`
- 第一行为表头（跳过），从第二行开始解析数据
- 支持类型: bool, int系列, uint系列, float系列, string, struct/array/slice/map (JSON解析)
- 支持字段索引: struct tag 为 `"index"` 的字段自动建立索引，支持 O(1) 查找
- 索引字段不允许重复值

---

## 十二、Log 日志 (`log/`)

四级日志系统：

| 级别 | 常量 | 前缀 | 说明 |
|------|------|------|------|
| Debug | 0 | `[debug  ]` | 调试信息 |
| Release | 1 | `[release]` | 发布级信息 |
| Error | 2 | `[error  ]` | 错误信息 |
| Fatal | 3 | `[fatal  ]` | 致命错误，调用 `os.Exit(1)` |

- 支持输出到文件（按时间戳命名）或标准输出
- 全局单例 `gLogger`，通过 `Export()` 替换
- 提供包级函数 `log.Debug/Release/Error/Fatal` 直接使用

---

## 十三、Util 工具集 (`util/`)

### DeepCopy (`deepcopy.go`)
- 递归深拷贝，支持 Interface/Ptr/Map/Slice/Struct
- 支持 `deepcopy:"-"` tag 跳过字段
- `DeepCopy(dst, src)` 拷贝到已有对象，`DeepClone(v)` 返回新对象

### Map (`map.go`)
- 基于 `sync.RWMutex` 的并发安全 Map
- 提供 Safe/Unsafe 两套 API
- `TestAndSet` 原子测试并设置
- `RLockRange/LockRange` 安全遍历

### Rand (`rand.go`)
- `RandGroup(p...)` — 按权重随机选组（如掉落概率）
- `RandInterval(b1, b2)` — 闭区间随机整数
- `RandIntervalN(b1, b2, n)` — 闭区间不重复随机 n 个整数（Fisher-Yates 变体）

### Semaphore (`semaphore.go`)
- 基于 channel 的信号量，`Acquire()` 获取，`Release()` 释放

---

## 十四、全局配置 (`conf/`)

```go
var (
    LenStackBuf     = 4096        // panic 堆栈缓冲区大小
    LogLevel        string        // 日志级别
    LogPath         string        // 日志文件路径
    LogFlag         int           // 标准库 log flag
    ConsolePort     int           // 控制台端口 (0=禁用)
    ConsolePrompt   = "Leaf# "   // 控制台提示符
    ProfilePath     string        // pprof 输出路径
    ListenAddr      string        // 集群监听地址
    ConnAddrs       []string      // 集群连接地址列表
    PendingWriteNum int           // 集群写缓冲大小
)
```

---

## 十五、设计模式总结

| 模式 | 应用 | 说明 |
|------|------|------|
| 单线程事件循环 | Skeleton.Run | select 多路复用，避免锁竞争 |
| Actor 模型 | Module + ChanRPC | 每个模块独立 goroutine，消息传递通信 |
| 异步回调 | Go 模块 | 耗时操作异步执行，回调回到主线程 |
| 工厂模式 | NewAgent 函数 | 连接创建时通过工厂函数生成 Agent |
| 最小堆调度 | MongoDB Session 池 | 引用计数最少的 Session 优先分配 |
| 观察者模式 | Gate → AgentChanRPC | 连接事件通知游戏逻辑 |

---

## 十六、Goroutine 模型深度分析

### 16.1 整体 Goroutine 拓扑

```
                    ┌─────────────────────────────────────────────┐
                    │              Main Goroutine                 │
                    │  leaf.Run() → 阻塞等待 os.Signal            │
                    └─────────────────────────────────────────────┘
                                        │
              ┌─────────────────────────┼─────────────────────────┐
              ▼                         ▼                         ▼
   ┌──────────────────┐    ┌──────────────────┐    ┌──────────────────┐
   │  Module A (gate) │    │ Module B (game)  │    │  Module C (...)  │
   │  Skeleton.Run()  │    │  Skeleton.Run()  │    │  Skeleton.Run()  │
   │  select 事件循环  │    │  select 事件循环  │    │  select 事件循环  │
   └────────┬─────────┘    └────────┬─────────┘    └──────────────────┘
            │                       │
   ┌────────┼────────┐              │
   ▼        ▼        ▼              ▼
 TCP      WS      每连接         Go() 产生的
 Accept   Accept  读写goroutine   工作 goroutine
```

### 16.2 关键并发边界

框架中存在三个关键的并发边界，所有跨边界通信都通过 channel 完成：

```
┌─────────────────────────────────────────────────────────┐
│ 边界1: 网络层 → 模块层                                    │
│                                                         │
│  network goroutine ──ChanCall──→ Skeleton 事件循环       │
│  (Gate.agent.Run)    (ChanRPC)   (module goroutine)     │
│                                                         │
│ 边界2: 工作 goroutine → 模块层                            │
│                                                         │
│  Go() goroutine ────ChanCb────→ Skeleton 事件循环        │
│  (异步任务执行)      (回调通道)   (module goroutine)       │
│                                                         │
│ 边界3: 定时器 → 模块层                                    │
│                                                         │
│  time.AfterFunc ───ChanTimer──→ Skeleton 事件循环        │
│  (runtime timer)   (定时通道)   (module goroutine)       │
└─────────────────────────────────────────────────────────┘
```

核心原则：**所有业务逻辑回调都在模块主 goroutine 中执行，无需加锁。**

### 16.3 TCPConn 读写分离模型

每个 TCP 连接产生 2 个 goroutine：

```
              TCPConn
┌──────────────────────────────┐
│                              │
│  读 goroutine (agent.Run)    │  ← 由 TCPServer.run() 启动
│  for { conn.ReadMsg() }     │
│         │                    │
│         ▼                    │
│  Processor.Unmarshal()       │
│  Processor.Route()           │
│         │                    │
│         ▼                    │
│  ChanRPC → 模块主goroutine   │
│                              │
│  写 goroutine (newTCPConn)   │  ← 由 newTCPConn() 启动
│  for b := range writeChan {  │
│      conn.Write(b)           │
│  }                           │
│                              │
└──────────────────────────────┘

写入路径: 业务层 → agent.WriteMsg() → writeChan → 写goroutine → net.Conn.Write()
```

写缓冲区满时的处理策略：直接调用 `doDestroy()` 销毁连接，防止慢客户端拖垮服务器。

---

## 十七、ChanRPC 调用流程详解

### 17.1 同步调用时序 (Call0/Call1/CallN)

```
 Module A (调用方)                          Module B (服务方)
 goroutine X                               Skeleton 事件循环
     │                                          │
     │  1. client.Call1(id, args...)             │
     │     ├─ 查找 functions[id] → f             │
     │     ├─ 构造 CallInfo{f, args, chanSyncRet}│
     │     └─ ChanCall ← CallInfo ──────────────►│
     │                                          │  2. select case ci := <-ChanCall
     │         阻塞等待                          │     server.Exec(ci)
     │         chanSyncRet                      │     ├─ 执行 f(args)
     │              ◄──────────────── RetInfo ───│     └─ chanRet ← RetInfo{ret, err}
     │  3. return ret, err                      │
     ▼                                          ▼
```

关键点：
- 调用方 goroutine 会**阻塞**在 `chanSyncRet` 上等待返回
- `chanSyncRet` 容量为 1，保证不会阻塞服务方
- 如果在模块自身的 Skeleton 中同步调用自己，会**死锁**（事件循环被阻塞）

### 17.2 异步调用时序 (AsynCall)

```
 Module A Skeleton                          Module B Skeleton
 事件循环                                    事件循环
     │                                          │
     │  1. skeleton.AsynCall(serverB, id,       │
     │         arg1, arg2, func(ret, err){})    │
     │     ├─ 最后一个参数为回调 cb              │
     │     ├─ 构造 CallInfo{f, args, ChanAsynRet, cb}
     │     └─ ChanCall ← CallInfo ──────────────►│
     │                                          │  2. server.Exec(ci)
     │  事件循环继续运行（不阻塞）               │     ├─ 执行 f(args)
     │  ...处理其他事件...                       │     └─ ChanAsynRet ← RetInfo{ret,err,cb}
     │                                          │
     │  3. select case ri := <-ChanAsynRet      │
     │     client.Cb(ri)                        │
     │     └─ 执行 cb(ret, err)                 │
     ▼                                          ▼
```

关键点：
- 调用方**不阻塞**，立即返回继续处理其他事件
- 回调 `cb` 在调用方的 Skeleton 事件循环中执行（线程安全）
- `pendingAsynCall` 计数器防止异步调用过多（超过 `ChanAsynRet` 容量时直接报错）
- `call()` 使用非阻塞发送 (`select default`)，ChanCall 满时返回错误而非阻塞

### 17.3 单向发送 (Server.Go)

```go
// 最简单的模式，无返回值
s.ChanCall <- &CallInfo{
    f:    f,
    args: args,
    // chanRet 为 nil → 不需要返回
}
```

- `chanRet` 为 nil 时，`ret()` 方法直接返回，不发送任何结果
- 使用 `defer recover()` 吞掉 channel 关闭后的 panic

---

## 十八、消息路由全链路分析

### 18.1 客户端请求完整链路 (以 Protobuf + TCP 为例)

```
客户端发送字节流
    │
    ▼
┌─────────────────────────────────────────────────────┐
│ 1. TCPConn.ReadMsg()                                │
│    MsgParser.Read(conn)                             │
│    ├─ io.ReadFull 读取 lenMsgLen 字节 → 解析消息长度  │
│    └─ io.ReadFull 读取 msgLen 字节 → 返回 []byte     │
└──────────────────────┬──────────────────────────────┘
                       ▼
┌─────────────────────────────────────────────────────┐
│ 2. Processor.Unmarshal(data)                        │
│    protobuf.Processor.Unmarshal()                   │
│    ├─ 读取前 2 字节 → msgID (uint16)                 │
│    ├─ msgInfo[msgID] → 获取消息元信息                 │
│    ├─ reflect.New(msgType.Elem()) → 创建消息实例      │
│    └─ proto.UnmarshalMerge(data[2:], msg) → 反序列化 │
└──────────────────────┬──────────────────────────────┘
                       ▼
┌─────────────────────────────────────────────────────┐
│ 3. Processor.Route(msg, agent)                      │
│    ├─ 方式A: msgHandler([]interface{}{msg, agent})   │
│    │         直接在网络 goroutine 中执行（需自行保证安全）│
│    └─ 方式B: msgRouter.Go(msgType, msg, agent)       │
│              通过 ChanRPC 投递到目标模块事件循环        │
└──────────────────────┬──────────────────────────────┘
                       ▼
┌─────────────────────────────────────────────────────┐
│ 4. 目标模块 Skeleton 事件循环                         │
│    select case ci := <-server.ChanCall:             │
│    server.Exec(ci)                                  │
│    └─ 执行注册的处理函数 f([]interface{}{msg, agent}) │
└─────────────────────────────────────────────────────┘
```

### 18.2 服务器响应链路

```
模块处理函数中调用 agent.WriteMsg(respMsg)
    │
    ▼
┌──────────────────────────────────────────────┐
│ gate.agent.WriteMsg(msg)                     │
│ ├─ Processor.Marshal(msg)                    │
│ │   ├─ 查找 msgID[msgType] → id             │
│ │   ├─ binary 编码 id → []byte{id}          │
│ │   └─ proto.Marshal(msg) → []byte{data}    │
│ │   返回 [][]byte{id, data}                 │
│ └─ conn.WriteMsg(id, data)                   │
│     └─ MsgParser.Write(conn, id, data)       │
│         ├─ 计算总长度 msgLen                  │
│         ├─ 编码长度头 + 拼接数据              │
│         └─ tcpConn.Write(msg) → writeChan    │
└──────────────────────────────────────────────┘
    │
    ▼  (异步，写 goroutine)
net.Conn.Write(bytes) → 客户端
```

### 18.3 JSON vs Protobuf 处理器对比

| 特性 | JSON Processor | Protobuf Processor |
|------|---------------|-------------------|
| 消息标识 | 结构体名称 (string) | 自增 uint16 ID |
| 存储结构 | `map[string]*MsgInfo` | `[]*MsgInfo` (slice) |
| 序列化开销 | 较高 (文本格式) | 较低 (二进制格式) |
| 可读性 | 高 (人类可读) | 低 (需工具解析) |
| 消息格式 | `{"Name": {...}}` | `[2B id][protobuf bytes]` |
| 适用场景 | 调试/Web客户端 | 正式环境/高性能 |

---

## 十九、连接生命周期管理

### 19.1 TCP 连接完整生命周期

```
阶段1: 建立连接
  TCPServer.run()
    ├─ ln.Accept() → net.Conn
    ├─ 检查连接数上限
    ├─ newTCPConn(conn) → 启动写 goroutine
    └─ NewAgent(tcpConn) → 创建 Agent
        └─ AgentChanRPC.Go("NewAgent", agent)  ← 通知业务层

阶段2: 数据交换
  go agent.Run()  (独立 goroutine)
    └─ for { ReadMsg → Unmarshal → Route }

阶段3: 连接关闭 (三种触发方式)
  a) 客户端断开 → ReadMsg 返回 error → 跳出循环
  b) 服务端主动 → agent.Close() → writeChan ← nil → 写goroutine退出
  c) 强制销毁   → agent.Destroy() → SetLinger(0) + conn.Close()

阶段4: 清理
  tcpConn.Close()
  delete(server.conns, conn)
  agent.OnClose()
    └─ AgentChanRPC.Call0("CloseAgent", agent)  ← 同步通知业务层
  server.wgConns.Done()
```

### 19.2 Close vs Destroy 语义

| 方法 | 行为 | 场景 |
|------|------|------|
| `Close()` | 发送 nil 到 writeChan，等待写 goroutine 自然退出 | 正常关闭，确保缓冲数据发送完毕 |
| `Destroy()` | SetLinger(0) + 立即关闭底层连接 | 异常关闭，丢弃未发送数据，立即释放资源 |

### 19.3 连接事件通知的不对称设计

```
新连接:  AgentChanRPC.Go("NewAgent", agent)     ← 单向发送，不等待
断开:    AgentChanRPC.Call0("CloseAgent", agent)  ← 同步调用，等待处理完成
```

原因：
- 新连接通知用 `Go`（fire-and-forget），因为连接建立不需要等待业务层确认
- 断开通知用 `Call0`（同步），确保业务层完成清理（如保存玩家数据）后才释放连接资源

---

## 二十、错误处理与容错机制

### 20.1 Panic 恢复策略

Leaf 在所有关键执行路径上都设置了 `defer recover()`，确保单个请求的 panic 不会崩溃整个服务：

| 位置 | 文件 | 恢复后行为 |
|------|------|-----------|
| ChanRPC 执行 | `chanrpc/chanrpc.go:99` | 记录错误日志，向调用方返回 error |
| ChanRPC 回调 | `chanrpc/chanrpc.go:352` | 记录错误日志，继续处理下一个回调 |
| Go 异步任务 | `go/go.go:39` | 记录错误日志，仍然发送 cb 到 ChanCb |
| Go 回调执行 | `go/go.go:57` | 记录错误日志，pendingGo 计数正常递减 |
| Timer 回调 | `timer/timer.go:33` | 记录错误日志，cb 置 nil 防止重复执行 |
| Module 销毁 | `module/module.go:58` | 记录错误日志，继续销毁下一个模块 |

堆栈信息通过 `conf.LenStackBuf` 控制（默认 4096 字节），设为 0 则不打印堆栈。

### 20.2 TCPServer Accept 错误处理

```go
// tcp_server.go:70 — 指数退避重试
if ne, ok := err.(net.Error); ok && ne.Temporary() {
    tempDelay *= 2                    // 5ms → 10ms → 20ms → ...
    if max := 1 * time.Second; tempDelay > max {
        tempDelay = max               // 上限 1 秒
    }
    time.Sleep(tempDelay)
    continue
}
```

仅对临时性网络错误重试，永久性错误直接退出 Accept 循环。

---

## 二十一、优雅关闭流程

### 21.1 关闭信号传播链

```
os.Signal (Interrupt/Kill)
    │
    ▼
leaf.Run() 捕获信号
    │
    ├─ 1. console.Destroy()
    │      └─ TCPServer.Close() → 关闭监听 + 关闭所有连接 + 等待所有 Agent 退出
    │
    ├─ 2. cluster.Destroy()
    │      ├─ server.Close()
    │      └─ 所有 client.Close()
    │
    └─ 3. module.Destroy() (逆序)
           └─ 对每个 module:
               ├─ closeSig <- true          发送关闭信号
               ├─ m.wg.Wait()              等待 Run() 退出
               └─ m.mi.OnDestroy()         执行清理逻辑
```

### 21.2 Skeleton 关闭细节

```go
func (s *Skeleton) Run(closeSig chan bool) {
    for {
        select {
        case <-closeSig:
            s.commandServer.Close()     // 1. 关闭命令服务器
            s.server.Close()            // 2. 关闭 RPC 服务器
            for !s.g.Idle() || !s.client.Idle() {
                s.g.Close()             // 3. 等待所有 Go() 任务完成
                s.client.Close()        // 4. 等待所有异步 RPC 回调完成
            }
            return                      // 5. 退出事件循环
```

关闭顺序保证：
1. 先关闭 ChanRPC Server（不再接受新请求，已排队请求返回 "server closed" 错误）
2. 再等待所有异步任务和回调完成（确保数据一致性）
3. 最后退出事件循环，触发 `OnDestroy()`

---

## 二十二、框架扩展点

### 22.1 自定义模块

实现 `module.Module` 接口，内嵌 `Skeleton` 即可获得完整基础设施：

```go
type MyModule struct {
    *module.Skeleton
}

func (m *MyModule) OnInit() {
    m.Skeleton = &module.Skeleton{
        GoLen:              10,
        TimerDispatcherLen: 10,
        AsynCallLen:        10,
        ChanRPCServer:      chanrpc.NewServer(100),
    }
    m.Skeleton.Init()

    // 注册消息处理
    m.Skeleton.RegisterChanRPC(msgType, handler)
}

func (m *MyModule) OnDestroy() {
    // 清理资源
}

// Run 由 Skeleton 提供，无需重写
```

### 22.2 自定义消息处理器

实现 `network.Processor` 接口即可替换序列化方案：

```go
type Processor interface {
    Route(msg interface{}, userData interface{}) error  // 路由消息
    Unmarshal(data []byte) (interface{}, error)         // 反序列化
    Marshal(msg interface{}) ([][]byte, error)          // 序列化
}
```

可扩展方向：MsgPack、FlatBuffers、自定义二进制协议等。

### 22.3 自定义控制台命令

通过 Skeleton 注册，命令在模块主 goroutine 中执行，可安全访问模块状态：

```go
skeleton.RegisterCommand("online", "show online count", func(args []interface{}) interface{} {
    return fmt.Sprintf("online: %d", playerCount)
})
```

### 22.4 扩展 Cluster

当前 `cluster/cluster.go` 中 Agent 为空实现，扩展方向：
- 实现节点发现与注册
- 节点间 RPC 调用
- 负载均衡与故障转移
- 分布式消息广播

---

## 二十三、典型使用模式

### 23.1 游戏服务器典型架构

```
leaf.Run(
    gate.Module,    // 网关模块：接收客户端连接
    game.Module,    // 游戏模块：处理游戏逻辑
    login.Module,   // 登录模块：处理认证
)
```

模块间协作：

```
Gate Module                    Game Module
┌────────────┐                ┌────────────────────┐
│ Gate{      │                │ Skeleton{          │
│   TCPAddr  │  "NewAgent"   │   ChanRPCServer ◄──┤── Gate.AgentChanRPC
│   WSAddr   │ ──ChanRPC──► │ }                   │
│   Processor│  "CloseAgent" │                     │
│ }          │ ──ChanRPC──► │ RegisterChanRPC(    │
└────────────┘                │   msgType, handler) │
                              └────────────────────┘
```

### 23.2 消息处理注册模式

```go
// 在 game 模块 OnInit 中
func (m *GameModule) OnInit() {
    // 注册 Protobuf 消息到本模块的 ChanRPC Server
    processor.SetRouter(&pb.MoveReq{}, m.ChanRPCServer)
    processor.SetRouter(&pb.AttackReq{}, m.ChanRPCServer)

    // 注册处理函数
    m.RegisterChanRPC(reflect.TypeOf(&pb.MoveReq{}), handleMove)
    m.RegisterChanRPC(reflect.TypeOf(&pb.AttackReq{}), handleAttack)

    // 注册连接事件
    m.RegisterChanRPC("NewAgent", onNewAgent)
    m.RegisterChanRPC("CloseAgent", onCloseAgent)
}
```

### 23.3 异步数据库操作模式

```go
// 在模块主 goroutine 中发起异步 DB 操作
skeleton.Go(func() {
    // 在工作 goroutine 中执行（可安全阻塞）
    db.Save(playerData)
}, func() {
    // 回调在模块主 goroutine 中执行（可安全访问模块状态）
    player.saved = true
})
```

### 23.4 定时器使用模式

```go
// 延时任务
skeleton.AfterFunc(5*time.Minute, func() {
    kickIdlePlayers()
})

// 周期任务 (每天凌晨 0 点)
cronExpr, _ := timer.NewCronExpr("0 0 0 * * *")
skeleton.CronFunc(cronExpr, func() {
    dailyReset()
})
```

---

## 二十四、框架优缺点分析

### 优点

| 方面 | 说明 |
|------|------|
| 简洁轻量 | 核心代码约 2000 行，易于理解和定制 |
| 并发安全 | 单线程事件循环 + ChanRPC，业务层无需加锁 |
| 模块化 | 模块独立运行，职责清晰，易于扩展 |
| 双协议 | 同时支持 TCP 和 WebSocket |
| 双序列化 | 内置 JSON 和 Protobuf 处理器 |
| 容错性 | 全链路 panic 恢复，单请求异常不影响服务 |
| 优雅关闭 | 完整的关闭流程，确保数据不丢失 |

### 局限性

| 方面 | 说明 |
|------|------|
| 集群能力 | cluster 模块仅为骨架，需自行实现节点通信协议 |
| 类型安全 | 大量使用 `interface{}`，缺乏编译期类型检查 |
| 消息上限 | Protobuf 处理器最多 65535 种消息 |
| DB 支持 | 仅封装 MongoDB，其他数据库需自行集成 |
| 无热更新 | 不支持运行时热更新代码或配置 |
| Go 版本 | 未使用泛型等新特性（兼容旧版本 Go） |

---

## 二十五、核心数据结构速查表

| 结构体 | 包 | 核心字段 | 用途 |
|--------|-----|---------|------|
| `Server` | chanrpc | `functions`, `ChanCall` | RPC 服务端，注册并执行函数 |
| `Client` | chanrpc | `chanSyncRet`, `ChanAsynRet` | RPC 客户端，发起同步/异步调用 |
| `CallInfo` | chanrpc | `f`, `args`, `chanRet`, `cb` | 一次 RPC 调用的完整信息 |
| `Skeleton` | module | `g`, `dispatcher`, `server`, `client` | 模块骨架，提供事件循环 |
| `Gate` | gate | `Processor`, `AgentChanRPC` | 网关，桥接网络与业务 |
| `TCPServer` | network | `ln`, `conns`, `msgParser` | TCP 服务端 |
| `TCPConn` | network | `conn`, `writeChan`, `msgParser` | TCP 连接封装 |
| `MsgParser` | network | `lenMsgLen`, `littleEndian` | TCP 消息编解码 |
| `WSServer` | network | `ln`, `handler` | WebSocket 服务端 |
| `WSConn` | network | `conn`, `writeChan`, `maxMsgLen` | WebSocket 连接封装 |
| `Processor` | protobuf | `msgInfo[]`, `msgID map` | Protobuf 消息处理器 |
| `Processor` | json | `msgInfo map` | JSON 消息处理器 |
| `Go` | g | `ChanCb`, `pendingGo` | 安全 goroutine 管理 |
| `LinearContext` | g | `linearGo`, `mutexExecution` | 顺序执行上下文 |
| `Dispatcher` | timer | `ChanTimer` | 定时器调度器 |
| `CronExpr` | timer | `sec`,`min`,`hour`,`dom`,`month`,`dow` | Cron 表达式 (位图) |
| `DialContext` | mongodb | `sessions` (SessionHeap) | MongoDB 连接池 |
| `RecordFile` | recordfile | `records[]`, `indexes[]` | 配置表数据 |
| `Logger` | log | `level`, `baseLogger` | 分级日志 |
| `Map` | util | `sync.RWMutex`, `m` | 并发安全 Map |

---

## 二十六、与 MapleWish 框架对比参考

| 维度 | Leaf | MapleWish |
|------|------|-----------|
| 通信方式 | ChanRPC (进程内 channel) | Kafka (跨进程消息队列) |
| 部署模型 | 单进程多模块 | 多进程分布式 (Gateway + GameServer) |
| Actor 实现 | Module = Actor (粗粒度) | Actor 独立实体 (细粒度，三邮箱) |
| 网络接入 | Gate 模块内置 | Gateway 独立进程 |
| 消息路由 | Processor + ChanRPC | Kafka Topic + ProtoBuf |
| 定时系统 | timer.Dispatcher | Actor 内置周变/日变/秒变 |
| 场景管理 | 无内置 | Space 系统 |
| 扩展性 | 单机，需自行实现集群 | 天然分布式，Kafka 解耦 |
