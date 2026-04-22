# Unity Demo — Engine Game Client

一个最小但完整的 Unity 端示例，覆盖三个高频场景：

- **角色移动**（Position 同步）
- **聊天**（频道广播）
- **排行榜**（订阅式推送）

并同时演示两种网络传输：

- **TCP**：`GameClient` 的默认传输
- **KCP**：通过 `KcpTransport` 封装（需要 kcp2k / unity-kcp2k 作为 Unity 端依赖，示例代码留有钩子）

客户端使用从 `codegen/templates_proto_sdk.go` 生成的 **强类型 C# SDK**，体验 Task + 超时 + `PushStream<T>` 三件套。

---

## 目录

```
unity_demo/
├── README.md                      # 本文件
├── Assets/
│   └── Scripts/
│       ├── GameMessages.cs        # 消息模型（手写版，真实工程建议 codegen 生成）
│       ├── TypedGameClient.cs     # 强类型客户端（TCP + KCP 双传输 + Router）
│       ├── KcpTransport.cs        # KCP 传输适配（依赖 kcp2k，留钩子）
│       ├── DemoController.cs      # MonoBehaviour 入口（移动/聊天/排行榜 UI 驱动）
│       └── Util/
│           └── DispatcherUtil.cs  # 主线程调度，供 Router 回调切回 Unity 主线程
└── server/
    └── main.go                    # 配套 Go 服务器（同时监听 TCP:8000 + KCP:9100）
```

## 快速上手

```bash
# 1) 启动服务器
cd example/unity_demo/server
go run main.go                # TCP :8000 / KCP :9100

# 2) 将 Assets/Scripts 目录导入到 Unity 工程
#    - Unity 2021.3 LTS 以上
#    - Package Manager 引入 Newtonsoft.Json（用于 JSON codec）
#    - 如使用 KCP 传输，额外引入 kcp2k (https://github.com/MirrorNetworking/kcp2k)
#
# 3) 创建一个空场景，把 DemoController 挂到 Main Camera，
#    在 Inspector 里配置 ServerAddress / Transport（Tcp|Kcp），
#    运行即可看到移动 / 聊天 / 排行榜三合一演示。
```

## 强类型 SDK 的三件套

| 能力 | API | 对应 TS 侧 |
|------|-----|-----------|
| Task + 超时 RPC | `await TypedGameClient.CallAsync<TResp>(…, timeout)` | `await rpc.call(type, req, {timeoutMs})` |
| 单条等待 | `await TypedGameClient.Push.OnceAsync<T>(…)` | `await push.once(type)` |
| 异步订阅流 | `await foreach (var m in client.Push.OnPush<T>(…))` | `for await (const m of push.onPush(type))` |

生成对应代码：

```bash
engine gen -proto=messages.proto -cs-proto-sdk=Assets/Scripts/ProtoSdk.cs \
           -cs-rpc=Assets/Scripts/RpcEnhance.cs -cs-ns=UnityDemo
```
