// Unity Demo — 消息定义（手写示例；真实工程推荐由 msggen 生成）
//
// 设计要点：
//   1. 所有消息都带一个静态 `TypeName`，用作 Router/Transport 的 key
//   2. 使用 Newtonsoft.Json 序列化（与 Engine gate 默认 JSON codec 对齐）
//   3. 为 RPC 风格的 Request 提供 __rpc_id 字段，服务端原样回传

using System.Collections.Generic;
using Newtonsoft.Json;

namespace EngineUnityDemo
{
    // ====== 通用基类 ======

    public abstract class MessageBase
    {
        [JsonProperty("type")] public string Type => GetType().Name;
        /// <summary>RPC 关联 ID：请求时客户端分配，响应时服务端原样回传</summary>
        [JsonProperty("__rpc_id", NullValueHandling = NullValueHandling.Ignore)]
        public long? RpcId { get; set; }
    }

    // ====== 登录 (RPC) ======

    public class LoginRequest : MessageBase
    {
        [JsonProperty("player_name")] public string PlayerName { get; set; }
    }

    public class LoginResponse : MessageBase
    {
        [JsonProperty("ok")] public bool Ok { get; set; }
        [JsonProperty("player_id")] public string PlayerId { get; set; }
        [JsonProperty("msg")] public string Msg { get; set; }
    }

    // ====== 移动 ======

    public class MoveRequest : MessageBase
    {
        [JsonProperty("x")] public float X { get; set; }
        [JsonProperty("y")] public float Y { get; set; }
        [JsonProperty("z")] public float Z { get; set; }
    }

    /// <summary>服务端广播的位置更新（含自己与其他玩家）</summary>
    public class PositionBroadcast : MessageBase
    {
        [JsonProperty("player_id")] public string PlayerId { get; set; }
        [JsonProperty("x")] public float X { get; set; }
        [JsonProperty("y")] public float Y { get; set; }
        [JsonProperty("z")] public float Z { get; set; }
    }

    // ====== 聊天 ======

    public class ChatSendRequest : MessageBase
    {
        [JsonProperty("channel")] public string Channel { get; set; } = "world";
        [JsonProperty("content")] public string Content { get; set; }
    }

    public class ChatBroadcast : MessageBase
    {
        [JsonProperty("channel")] public string Channel { get; set; }
        [JsonProperty("from")] public string From { get; set; }
        [JsonProperty("content")] public string Content { get; set; }
        [JsonProperty("timestamp")] public long Timestamp { get; set; }
    }

    // ====== 排行榜 ======

    public class LeaderboardEntry
    {
        [JsonProperty("rank")] public int Rank { get; set; }
        [JsonProperty("player_id")] public string PlayerId { get; set; }
        [JsonProperty("score")] public long Score { get; set; }
    }

    public class LeaderboardNotify : MessageBase
    {
        [JsonProperty("entries")] public List<LeaderboardEntry> Entries { get; set; }
    }

    // ====== 心跳/错误 ======

    public class ServerError : MessageBase
    {
        [JsonProperty("code")] public int Code { get; set; }
        [JsonProperty("msg")] public string Msg { get; set; }
    }
}
