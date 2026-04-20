// Unity Demo — MonoBehaviour 入口
//
// 场景布局建议：
//   - 主摄像机挂一个 DispatcherPump
//   - 一个空 GameObject 挂本脚本 DemoController（Inspector 填 ServerAddress / Transport）
//   - UGUI: InputField(聊天) + Text(聊天记录) + Text(排行榜) + 一个 Cube(自己) + 若干动态 Cube(其他玩家)
//
// 本示例完整演示：登录 RPC → WASD 移动 → 聊天 → 排行榜异步流

#if UNITY_2017_1_OR_NEWER
using System;
using System.Collections.Generic;
using System.Text;
using System.Threading;
using System.Threading.Tasks;
using UnityEngine;
using UnityEngine.UI;

namespace EngineUnityDemo
{
    public sealed class DemoController : MonoBehaviour
    {
        [Header("Connection")]
        [Tooltip("服务器地址，例如 127.0.0.1:8000（TCP）或 127.0.0.1:9100（KCP）")]
        public string ServerAddress = "127.0.0.1:8000";
        public TransportKind Transport = TransportKind.Tcp;
        public string PlayerName = "Unity";

        [Header("Scene References")]
        public Transform SelfAvatar;
        public Transform[] OtherAvatars;      // 预先铺几个占位 Cube，动态按 PlayerId 分配
        public InputField ChatInput;
        public Text ChatLog;
        public Text LeaderboardText;

        [Header("Movement")]
        public float MoveSpeed = 3f;
        public float SyncHz = 10f;             // 移动同步频率

        private TypedGameClient _client;
        private string _playerId;
        private readonly Dictionary<string, Transform> _others = new Dictionary<string, Transform>();
        private int _otherCursor;
        private float _nextSyncAt;
        private readonly StringBuilder _chatBuf = new StringBuilder();
        private CancellationTokenSource _runCts;

        async void Start()
        {
            // 主线程泵车
            if (FindObjectOfType<DispatcherPump>() == null)
                gameObject.AddComponent<DispatcherPump>();

            _client = new TypedGameClient(new ClientOptions
            {
                Address = ServerAddress,
                Transport = Transport,
                MarshalToMainThread = true,
            });
            _client.OnConnected    += () => AppendChat($"[sys] connected via {Transport}");
            _client.OnDisconnected += r  => AppendChat($"[sys] disconnected: {r}");
            _client.OnError        += e  => Debug.LogWarning("[client] " + e.Message);

            // 订阅 Push 消息
            _client.Push.On<PositionBroadcast>(OnPositionBroadcast);
            _client.Push.On<ChatBroadcast>(OnChatBroadcast);

            _runCts = new CancellationTokenSource();
            // 排行榜采用异步流消费
            _ = LeaderboardLoop(_runCts.Token);

            try
            {
                await _client.ConnectAsync();
                var resp = await _client.CallAsync<LoginResponse>(
                    new LoginRequest { PlayerName = PlayerName },
                    TimeSpan.FromSeconds(5));
                if (!resp.Ok) { AppendChat("[sys] login failed: " + resp.Msg); return; }
                _playerId = resp.PlayerId;
                AppendChat($"[sys] logged in as {_playerId}");
            }
            catch (Exception ex) { AppendChat("[sys] error: " + ex.Message); }
        }

        void Update()
        {
            if (_client == null || !_client.IsConnected || SelfAvatar == null) return;

            // 本地移动
            float x = Input.GetAxis("Horizontal");
            float z = Input.GetAxis("Vertical");
            if (Mathf.Abs(x) > 0.01f || Mathf.Abs(z) > 0.01f)
            {
                var delta = new Vector3(x, 0, z) * (MoveSpeed * Time.deltaTime);
                SelfAvatar.position += delta;
            }

            // 节流同步
            if (Time.time >= _nextSyncAt)
            {
                _nextSyncAt = Time.time + 1f / Mathf.Max(1f, SyncHz);
                _client.Send(new MoveRequest
                {
                    X = SelfAvatar.position.x, Y = SelfAvatar.position.y, Z = SelfAvatar.position.z,
                });
            }

            // 聊天发送（回车）
            if (ChatInput != null && Input.GetKeyDown(KeyCode.Return) && !string.IsNullOrWhiteSpace(ChatInput.text))
            {
                _client.Send(new ChatSendRequest { Channel = "world", Content = ChatInput.text });
                ChatInput.text = "";
            }
        }

        void OnDestroy()
        {
            _runCts?.Cancel();
            _client?.Dispose();
        }

        // ---------------- Handlers ----------------

        private void OnPositionBroadcast(PositionBroadcast msg)
        {
            if (msg.PlayerId == _playerId) return;
            if (!_others.TryGetValue(msg.PlayerId, out var t))
            {
                if (OtherAvatars == null || _otherCursor >= OtherAvatars.Length) return;
                t = OtherAvatars[_otherCursor++];
                _others[msg.PlayerId] = t;
                t.gameObject.SetActive(true);
            }
            t.position = new Vector3(msg.X, msg.Y, msg.Z);
        }

        private void OnChatBroadcast(ChatBroadcast msg)
        {
            AppendChat($"[{msg.Channel}] {msg.From}: {msg.Content}");
        }

        private async Task LeaderboardLoop(CancellationToken ct)
        {
            try
            {
                // await foreach 消费 PushStream，不阻塞 Unity 主线程
                await foreach (var snap in _client.Push.OnPush<LeaderboardNotify>(capacity: 16, ct))
                {
                    if (snap?.Entries == null) continue;
                    var sb = new StringBuilder("== Leaderboard ==\n");
                    foreach (var e in snap.Entries)
                        sb.AppendLine($"#{e.Rank,2} {e.PlayerId,-16} {e.Score}");
                    if (LeaderboardText != null) LeaderboardText.text = sb.ToString();
                }
            }
            catch (OperationCanceledException) { }
        }

        private void AppendChat(string line)
        {
            _chatBuf.AppendLine(line);
            if (_chatBuf.Length > 8000) _chatBuf.Remove(0, _chatBuf.Length - 6000);
            if (ChatLog != null) ChatLog.text = _chatBuf.ToString();
            Debug.Log(line);
        }
    }
}
#endif
