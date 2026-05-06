# engine_sample/CLAUDE.md — 参考骨架：ChanRPC + Scene + Actor + 中心服

> **只读参考实现**，对标 Leaf 风格的最小游戏引擎骨架。Go 1.24。
> 本目录不参与 `engine/` 模块的编译/测试/LOC 统计，仅作为 `engine/` 精简迭代的参照物。

## 定位

把一个分布式多人服务器的"最小可跑通"骨架拆成三件事：

1. **本进程 Actor 调度**：一个 Actor = 一条 goroutine + 一个 ChanRPC Server（基于 `chan` 的 RPC 队列）。
2. **同机/跨机 Actor 互通**：每个远端 server 在本机对应一个 `ServerActor`（代理 + TCP 长连接），消息先经 ActorMessage（protobuf）序列化再投递。
3. **外部集中式中心服**：独立二进制 `centerserver/`，用 Redis + gRPC/HTTP 负责"某个 (actorType, actorId) 应当落在哪个 serverId"的服务发现、创建版本号与维护开关。

这套骨架**几乎没有"分布式中间件"抽象**（无 gossip、无一致性哈希、无分脑检测、无 grain、无 singleton），只做"足够用的"路由 + "够简单的"中心服。

## 模块地图（7 个 Go module，聚合式仓库）

每个顶级目录都是独立 Go module、独立 `go.mod`、独立 `.git`；本 CLAUDE.md 合并描述。

```
engine_sample/
├── hf/                    接口层 module（0 实现，全是 I + 全局变量）
│   ├── I.go               顶层聚合接口 hf.I + 单例 hf.Instance
│   ├── actorI/            IActor / IScene / IActorMessage / IEngineManage / ISceneManage / ICenterActor …
│   ├── engineI/           IServer / ISkeleton / IChanRpcManage
│   ├── gateI/             IGate / IAgent / IConn / IProcessor / ITCPClient / IWSClient
│   ├── tzDataI/           ITZData（配置表加密）
│   ├── conf/              全局 var（ServerId / WSAddr / Centers / MaxMsgLen …）
│   ├── enum/              actorType / actorState / eventcode / msgType / tickType / enginelog
│   ├── e/                 错误码常量（SYS_超时 / SYS_服务器内部错误 …）
│   ├── f/                 全局函数变量池（CallToMessage / GoSendMessage / GetNowTime …）
│   ├── m/                 全局 Processor + 内置消息接口钩子
│   └── B/                 全局单例 B.Obj hf.I（实现包 init 时注入）
│
├── message/               消息契约 module
│   ├── message.go         IData / IMessage / IReqMessage / IRepMessage
│   ├── msglist/           Register(id,factory,level) → 全局消息 ID→Factory 注册表
│   └── byteBuf/           网络字节流封装
│
├── enginemessage/         引擎自用 protobuf message module
│   ├── msg.xml            消息 DSL（用 makemsg.exe 生成 .pb.go + marshal/unmarshal）
│   ├── def/               S2CRep 等公共响应（Tag 即错误码）
│   ├── center/            S2SCreateActorReq / S2SDeleteActorReq / S2SServerHeart / S2SDistributionRuleServer …
│   ├── centerrpc/         中心服 gRPC 契约（CenterService）
│   └── serverrpc/         服务器间 gRPC 契约
│
├── basics/                引擎核心实现 module（即 `hf.I` 的实现）
│   ├── basecore/
│   │   ├── chanrpc/       ★ chan RPC server/client（Go/Go1/GoBytes/GoRead/Call/AsynCall）
│   │   ├── skeleton/      ★ ChanRPC Server Run 循环（select ChanCall / ReadCall / closeSig）
│   │   ├── network/       TCP / WS server+client、MsgParser（4 字节长度前缀）
│   │   ├── gate/          gate.Manage + agent 代理（把 network 适配成 gateI）
│   │   ├── netbyte/       消息字节流编码（idx + rpcId + body）
│   │   ├── goSlice/       []interface{} 对象池
│   │   ├── common/        bytes.Grow 内联版本
│   │   └── VMProtect/     套壳加密校验
│   ├── actors/
│   │   ├── actorMessage/  ★ ActorMessage（protobuf + sync.Pool）含 MsgType/TargetScene/TargetActor/RpcId
│   │   ├── baseActor/     ★ baseActor：Actor 基类（skeleton + chanrpc + actions 注册 + tick 回调）
│   │   ├── baseScene/     ★ baseScene：Scene 是特殊 Actor，管理一组 {actorId → Actor goroutine}，全局 tick 广播
│   │   └── sceneManage/   ★ 全局 Scene 注册表、pprof/status/close/maintenance HTTP 端点、信号优雅关闭
│   ├── basebusiness/
│   │   ├── scenes/
│   │   │   ├── commonScene/           通用 scene + nilActor / noExistActor（处理不存在的 actor）
│   │   │   ├── serverScene/           ★ 跨机消息入口 scene（托管 gate + 所有 serverActor）
│   │   │   │   └── serverActor/       ★ 每个远端 serverId 一个代理 actor（含 TCP client + callback 表）
│   │   │   ├── serverIdCache/         ★ 本机缓存 "actorType+actorId→serverId"，miss 时走 gRPC 询问中心服
│   │   │   └── serverList/            Redis HASH `server:list`，服务器注册/心跳/订阅版本号
│   │   ├── systemTime/                自定义时间源（秒/分/时/天/周 tick 帧）
│   │   └── tzdata/                    配置表加密读取
│   ├── consts/ errmsg/                错误码
│   ├── hf/instance/                   instance 实现（init 时把各 Manage 赋给 B.Obj，main 必须调 SetCheck）
│   ├── hf/sa/                         把 f.Go*、f.Call* 等全局函数注入到 hf/f 包的静态变量
│   ├── hf/B/                          别名 hf/B 的指针引用
│   ├── main/main.go                   暴露 GetI() / Init(code) 库入口（VMP 校验通过后 SetCheck）
│   └── makeEncrypt/                   加密工具
│
└── centerserver/          独立二进制 module（中心服 + 路由代理）
    ├── centerserver/main.go           启动中心服（gRPC + HTTP）
    ├── web/                           HTTP 路由 + gRPC 入口 + redisOpp（创建/删除/心跳/分配）
    ├── centerRoute/                   WS 反向代理（按 serverId 分流到中心服实例）
    ├── httpRequest/                   调用游戏服的维护 HTTP 接口
    ├── I/                             ICenterActor / IRule / IServerData 接口
    └── conf/ consts/ errmsg/          配置/常量
```

## 核心模型

### 1) Actor = goroutine + ChanRPC Server

```
Actor.Go(msgId, args...)
  → ChanRPCServer.Go1(eventcode.ActorMsgHandle, goSlice{msgId, args...})
    → ChanCall <- callInfo
      → skeleton.Run 的 select 循环
        → server.Exec(ci)
          → baseActor.OnHandleMessage(args)
            → actions[msgId](self, args[1:])
```

- **每个 Actor 一个独立 goroutine**，消息严格顺序处理。
- **ChanRPC** 区分 `ChanCall`（写）和 `ReadCall`（只读）两条队列；读消息可以在业务 actor 阻塞等待 RPC 回包时顺带处理。
- **Call** = 同步阻塞 + 2 秒默认超时；**Go** = fire-and-forget；**AsynCall** = 带回调的异步。
- **channel 满了就丢**（`len(ChanCall)==cap(ChanCall)` 直接 `return`）——背压策略极简。

### 2) Scene = 管理同类型 Actor 的特殊 Actor

- `baseScene` 继承 `IActor`，内部维护 `actors map[uint32]*ActorData`。
- `GetActor(id)` 不存在时 **自动向中心服请求创建**（`f.RequestCreateActor` → 中心服校验 group/维护状态 → 返回 version），然后 `go v.Actor.Start()`。
- **全局计时器**（`sceneManage.globalTimer`）每 33ms 一帧，根据帧差投递 TickChange/SecondChange/MinuteChange/HourChange/DayChange/WeekChange/Second60Change 到每个 scene 的 ChanRPC 队列，scene 再 fan-out 到所有子 actor。
- `tickType.TickType` 位掩码可以让 scene 只订阅部分 tick。
- **空闲回收**：actor `LastTime > freeTime` 或 `state == NeedClose` 时，scene 关闭它。
- **优雅关闭**：SIGTERM 后先关 PlayerAgent scene（断网关）→ 其余 scene 逆序关；每个 scene 用 `TimeoutWaitGroup` 等 actor.OnDestroy 完成。

### 3) 跨机：ServerActor + ServerScene

- `serverScene` 是本机唯一的对外 gate 持有者（TCP listen）：
  - 收到字节流 → `NewActorMessage1(data)` 反序列化 → 根据 `TargetSceneId`/`TargetActorId` 路由到对应 scene.GetActor.GoNetMsg；
  - 若是 `Reply`：路由到发起方的 `serverActor.GoReceiveRepMsg`（对应 callback chan 唤醒阻塞的 Call）；
  - 若目标 scene/actor 不存在：转发到 `nilActor` / `noExistActor`，统一构造错误回包以避免上游超时卡住。
- `serverActor` 是每个远端 serverId 的本地代理，内部维护：
  - TCP client 到远端 gate；
  - 连接态机（None/Connecting/ConnectSuccess）；
  - 未连上时的 `cacheMessage [][]byte` 队列；
  - 断连后的缓存回灌 → 送去 `noExistActor` 走错误回执。
- 与自己同 serverId 通信时用 `inAgent`（**进程内 channel**，不走 TCP），零复制。

### 4) 服务发现：serverList + serverIdCache + centerserver

- **serverList**：Redis HASH `{WSGroup}:server:list` 存 `serverId → "addr:port"`；本机每秒拉取版本号，变化时全量刷新，触发 `onServerOnline/onServerOffline` 回调。
- **serverIdCache**：本机 `sync.Map` 缓存 `(actorType<<32 | actorId) → serverId`；miss 时通过 gRPC 问中心服（`RequestDistributionServer`），超过 1800 秒未心跳则清理。
- **centerserver**：独立二进制，提供 gRPC 服务 `CenterService.Request(MessageBody)` + HTTP `/api/handle`，内部按 msgId 分发到 `redisOpp`：
  - `S2SCreateActorReq` → 检查 group + 维护名单 → 存 Redis → 返回 version（版本号避免"新 actor 被旧 server 的删除请求杀死"）；
  - `S2SDeleteActorReq`（含 version 对比）/ `S2SServerHeart` / `S2SDistributionRuleServerReq`（路由规则）/ `S2SGetAllActorIdReq`…
- **centerRoute**：WS 反向代理工具，把对中心服的请求按 serverId 分流到对应中心服实例（水平扩中心服用）。

### 5) 依赖反转：hf 包是钩子网关

- `hf/B/B.Obj hf.I`：单例，在实现包 `basics/hf/instance` 的 `init()` 里装配（`ins._sceneManage = &sceneManage.Manage{}` …）；
- `hf/f/*`：一堆 `var CallToMessage func(...)` 形式的全局函数指针，由 `basics/hf/sa/sa.go` 的 `init()` 批量注入；
- `hf/m/Processor`：`IProcessor`（消息编解码器），由业务层赋值；
- `hf/enum/actorType`：所有 `GetScenePlayer/GetSceneServer/...` 也都是 `var ... func() ActorType`，业务层注册具体类型。

**效果**：
- 引擎核心（`hf/*`、`message`、`enginemessage`）**不依赖任何业务代码**；
- 业务层（上层 `gamelib`）可以随意扩展 actorType、PCK_ID、全局函数；
- 单元测试时可替换任意钩子。

## 消息流转全景

```
本机 Actor.GoMsg(pb)
  └─ ChanRPCServer.Go1 → Mailbox(ChanCall) → skeleton.Run → OnHandleMessage
  
本机 actor.CallToMessage(scenePlayer, id, req)
  └─ NewActorMessage + MessageType=1 + callback.AddCallBack(timeout)
  └─ sa.GoSendMessage  
      └─ getActorServerId(scenePlayer, id)
           ├─ serverIdCache 命中 → 直接拿 serverId
           └─ miss → centerserver.RequestDistributionServer → 缓存
      └─ getServerActorByServerId(serverId).GoSendMessage
           └─ ServerActor.ChanRPC → agent.WriteBuff(bytes)  (TCP OR inAgent)
  └─ block on callback chan（同时继续处理 ReadCall）

远端:
  TCP/WS server 收到 bytes → serverScene.goReceiveMsg
    └─ 若 MsgType==Reply：routedToServerActor.GoReceiveRepMsg → callback chan <- ret
    └─ 否则：sceneMap[TargetScene].GetActor(TargetActor).GoNetMsg(data)
         └─ baseActor.onGoNetMsg → actions[pck.Id](self, pck)
         └─ 若注册函数有返回 IRepMessage：自动 replyMessage（MsgType=Reply，反向发回）
```

## 启动时序（游戏服）

```go
func main() {
    // 1. 业务初始化 conf、redis、time、actorType 注册
    // 2. 注入消息工厂到 hf/m.Processor；msglist.Register(id, factoryFn, level)
    // 3. hf.Instance = instance.CreateInstance(); instance.SetCheck()
    // 4. 业务层 RegisterCreateActor / RegisterScene（内置 SceneServer / SceneNil / SceneActorNoExist）
    // 5. basics/actors/sceneManage.Run() —— 启动全局 tick、pprof、信号监听、阻塞直至 SIGTERM
}
```

## 构建 / 加密 / 部署

```bash
# 每个 module 单独构建
(cd basics && ./build.sh && ./enc.sh)   # 产出 basics_vmp.so（VMP 加密的共享库）
(cd centerserver/centerserver && ./bulid.sh)
(cd centerserver/centerRoute && go build .)
(cd enginemessage && ./make.sh)          # msg.xml → .pb.go（用 makemsg.exe）

# 业务层通过 basics_vmp.so 的 GetI()/Init(code) 获取引擎实例
```

## 关键设计取舍

| 选择 | 原因 |
|---|---|
| **chan 而不是无锁队列** | 代码 300 行内可读，Go 原生语义直观；代价是每消息一次 chan 操作 |
| **Actor 固定 5000 容量 channel** | 足够业务用；满则日志 + 丢包，简单粗暴 |
| **Scene 持有一切** | actor 创建/关闭/tick/存活检查全部走 scene；actor 自身不直接创建 actor |
| **protobuf ActorMessage** | 跨机消息契约稳定；字段紧凑；配 sync.Pool 几乎零 GC |
| **中心服是独立二进制** | 游戏服重启不影响全局状态；中心服崩了游戏服可降级（用缓存 + 自发现） |
| **Redis 作唯一强一致存储** | 用 `HEXPIRE`/version 作为乐观锁，避免双主；不需要 Raft/Gossip |
| **不做 gossip/一致性哈希** | 中心服直接按规则分配 serverId，规则可按 group/地区定制 |
| **VMP 套壳** | 商用游戏防破解，校验失败 fatal |
| **全局 `hf/f`、`hf/B.Obj`** | 业务层注入一次，后续调用无反射；以静态变量换灵活性 |

## 外部依赖（basics 为主）

- `github.com/gorilla/websocket` — WS server/client
- `google.golang.org/grpc` + `google.golang.org/protobuf` — 与中心服通信
- `tzgit.kaixinxiyou.com/utils/common/*` — 私有 utils（log / redis / byteslice pool / tzid…）
- `tzgit.kaixinxiyou.com/utils/common/redisSubscribe` — 服务器列表版本订阅

## 规模参考

- 源码约 **1.1 万行**（不含 protobuf 生成、不含 centerserver）；centerserver 另约 3 千行。
- 无测试套件、无 benchmark（商业闭源代码库惯例）。
- 对比 engine/ 当前约 **1.99 万行** 源码 + 9.5 千行测试，engine_sample 的"骨架实际体量"小一半。

## 不做什么

- ❌ 不做 gossip、一致性哈希、分脑检测、vector clock、Raft
- ❌ 不做 grain/virtual actor、singleton、migration
- ❌ 不做 rolling upgrade、canary、multi-DC、federation
- ❌ 不做连接池、消息分片、zero-copy、消息加密、消息签名
- ❌ 不做 OpenTelemetry / W3C traceparent
- ❌ 不做 pub/sub topic（有需要时在业务层自己做）
- ❌ 不做多种 mailbox 策略、work-stealing dispatcher、Future、Stash、Behavior 栈
- ❌ 不做 Supervisor Directive 树（panic 用 recover+log 就地吞掉）

这些是"工程上可以没有、业务上按需再加"的功能，是 engine_sample 刻意不放进骨架的边界。
