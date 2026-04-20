// Unity Demo — 强类型 Game Client
//
// 同时支持 TCP / KCP 两种传输（由 ITransport 抽象解耦），对上暴露：
//   - CallAsync<TResp>(req, timeout)    Task 风格 RPC（基于 __rpc_id 关联）
//   - Push.On<T>(handler)                回调风格订阅
//   - Push.OnceAsync<T>(timeout)         单条等待
//   - Push.OnPush<T>()                   IAsyncEnumerable 流
//
// 设计要点：
//   - 读循环在后台线程，Router 回调通过 DispatcherUtil 切回 Unity 主线程（默认）
//   - JSON codec 对齐 engine gate 默认帧格式：4 字节大端长度 + JSON 载荷

using System;
using System.Collections.Concurrent;
using System.Collections.Generic;
using System.Net.Sockets;
using System.Text;
using System.Threading;
using System.Threading.Channels;
using System.Threading.Tasks;
using Newtonsoft.Json;
using Newtonsoft.Json.Linq;

namespace EngineUnityDemo
{
    public enum TransportKind { Tcp, Kcp }

    public class ClientOptions
    {
        public string Address { get; set; } = "127.0.0.1:8000";
        public TransportKind Transport { get; set; } = TransportKind.Tcp;
        public int ReconnectMs { get; set; } = 3000;
        public int HeartbeatMs { get; set; } = 25000;
        public int MaxReconnect { get; set; } = 5;
        /// <summary>是否把 Router 回调切回 Unity 主线程（仅在 Unity 运行时 true）</summary>
        public bool MarshalToMainThread { get; set; } = true;
    }

    // ============================================================
    // Transport 抽象：TCP / KCP 共用同一上层协议（4 字节大端长度 + JSON）
    // ============================================================

    public interface ITransport : IDisposable
    {
        Task ConnectAsync(string address, CancellationToken ct);
        Task SendAsync(byte[] frame);
        Task<byte[]> ReceiveAsync(CancellationToken ct);
    }

    public sealed class TcpTransport : ITransport
    {
        private TcpClient _tcp;
        private NetworkStream _stream;

        public async Task ConnectAsync(string address, CancellationToken ct)
        {
            var parts = address.Split(':');
            var host = parts[0];
            var port = parts.Length > 1 ? int.Parse(parts[1]) : 8000;
            _tcp = new TcpClient();
            await _tcp.ConnectAsync(host, port).ConfigureAwait(false);
            _tcp.NoDelay = true;
            _stream = _tcp.GetStream();
        }

        public Task SendAsync(byte[] frame) => _stream.WriteAsync(frame, 0, frame.Length);

        public async Task<byte[]> ReceiveAsync(CancellationToken ct)
        {
            var lenBuf = new byte[4];
            if (!await ReadExact(lenBuf, ct)) return null;
            int len = (lenBuf[0] << 24) | (lenBuf[1] << 16) | (lenBuf[2] << 8) | lenBuf[3];
            if (len <= 0 || len > 10 * 1024 * 1024) return null;
            var data = new byte[len];
            if (!await ReadExact(data, ct)) return null;
            return data;
        }

        public void Dispose()
        {
            try { _stream?.Close(); _tcp?.Close(); } catch { }
        }

        private async Task<bool> ReadExact(byte[] buf, CancellationToken ct)
        {
            int off = 0;
            while (off < buf.Length)
            {
                var n = await _stream.ReadAsync(buf, off, buf.Length - off, ct).ConfigureAwait(false);
                if (n <= 0) return false;
                off += n;
            }
            return true;
        }
    }

    // ============================================================
    // TypedGameClient — 上层强类型封装
    // ============================================================

    public sealed class TypedGameClient : IDisposable
    {
        private readonly ClientOptions _options;
        private ITransport _transport;
        private CancellationTokenSource _cts;
        private readonly ConcurrentDictionary<long, TaskCompletionSource<JObject>> _pending
            = new ConcurrentDictionary<long, TaskCompletionSource<JObject>>();
        private long _rpcSeq;
        private volatile bool _connected;
        private int _reconnectAttempts;

        public event Action OnConnected;
        public event Action<string> OnDisconnected;
        public event Action<Exception> OnError;

        public bool IsConnected => _connected;

        public PushBroker Push { get; }

        public TypedGameClient(ClientOptions options)
        {
            _options = options ?? throw new ArgumentNullException(nameof(options));
            Push = new PushBroker(_options.MarshalToMainThread);
        }

        public async Task ConnectAsync()
        {
            _cts = new CancellationTokenSource();
            _transport = _options.Transport == TransportKind.Kcp
                ? KcpTransport.Create()
                : new TcpTransport();

            await _transport.ConnectAsync(_options.Address, _cts.Token).ConfigureAwait(false);
            _connected = true;
            _reconnectAttempts = 0;
            OnConnected?.Invoke();

            _ = Task.Run(() => ReadLoop(_cts.Token));
            if (_options.HeartbeatMs > 0)
                _ = Task.Run(() => HeartbeatLoop(_cts.Token));
        }

        public void Disconnect(string reason = "client disconnect")
        {
            if (!_connected) return;
            _connected = false;
            _cts?.Cancel();
            _transport?.Dispose();
            OnDisconnected?.Invoke(reason);
        }

        public void Send<T>(T msg) where T : MessageBase
        {
            if (!_connected) return;
            var json = JsonConvert.SerializeObject(msg);
            var payload = Encoding.UTF8.GetBytes(json);
            var frame = new byte[4 + payload.Length];
            frame[0] = (byte)(payload.Length >> 24);
            frame[1] = (byte)(payload.Length >> 16);
            frame[2] = (byte)(payload.Length >> 8);
            frame[3] = (byte)payload.Length;
            Buffer.BlockCopy(payload, 0, frame, 4, payload.Length);
            try { _transport.SendAsync(frame).GetAwaiter(); }
            catch (Exception ex) { OnError?.Invoke(ex); Reconnect(); }
        }

        /// <summary>RPC 风格调用：发请求 → 等 __rpc_id 回传的响应，超时抛 TimeoutException</summary>
        public async Task<TResp> CallAsync<TResp>(MessageBase req, TimeSpan timeout,
            CancellationToken ct = default)
            where TResp : MessageBase
        {
            var id = Interlocked.Increment(ref _rpcSeq);
            req.RpcId = id;
            var tcs = new TaskCompletionSource<JObject>(TaskCreationOptions.RunContinuationsAsynchronously);
            _pending[id] = tcs;

            using var linked = CancellationTokenSource.CreateLinkedTokenSource(ct);
            if (timeout > TimeSpan.Zero) linked.CancelAfter(timeout);
            linked.Token.Register(() =>
            {
                if (_pending.TryRemove(id, out var hit))
                    hit.TrySetException(ct.IsCancellationRequested
                        ? (Exception)new OperationCanceledException(ct)
                        : new TimeoutException($"RPC timeout: {req.Type}"));
            });

            Send(req);
            var raw = await tcs.Task.ConfigureAwait(false);
            return raw.ToObject<TResp>();
        }

        public void Dispose() => Disconnect("disposed");

        // ---- 内部 ----

        private async Task ReadLoop(CancellationToken ct)
        {
            try
            {
                while (!ct.IsCancellationRequested)
                {
                    var data = await _transport.ReceiveAsync(ct).ConfigureAwait(false);
                    if (data == null) { Reconnect(); return; }
                    HandleFrame(data);
                }
            }
            catch (OperationCanceledException) { }
            catch (Exception ex) { OnError?.Invoke(ex); Reconnect(); }
        }

        private void HandleFrame(byte[] data)
        {
            JObject obj;
            try { obj = JObject.Parse(Encoding.UTF8.GetString(data)); }
            catch { return; }
            var type = obj["type"]?.ToString();
            if (string.IsNullOrEmpty(type)) return;

            // __rpc_id 关联响应
            var rpcId = obj["__rpc_id"]?.ToObject<long?>();
            if (rpcId.HasValue && _pending.TryRemove(rpcId.Value, out var waiting))
            {
                waiting.TrySetResult(obj);
                return;
            }
            // 否则进入 Push 路由
            Push.Dispatch(type, obj);
        }

        private async Task HeartbeatLoop(CancellationToken ct)
        {
            try
            {
                while (!ct.IsCancellationRequested)
                {
                    await Task.Delay(_options.HeartbeatMs, ct).ConfigureAwait(false);
                    var json = "{\"type\":\"__ping__\"}";
                    var payload = Encoding.UTF8.GetBytes(json);
                    var frame = new byte[4 + payload.Length];
                    frame[0] = (byte)(payload.Length >> 24);
                    frame[1] = (byte)(payload.Length >> 16);
                    frame[2] = (byte)(payload.Length >> 8);
                    frame[3] = (byte)payload.Length;
                    Buffer.BlockCopy(payload, 0, frame, 4, payload.Length);
                    try { await _transport.SendAsync(frame); } catch { break; }
                }
            }
            catch (OperationCanceledException) { }
        }

        private async void Reconnect()
        {
            if (!_connected && _reconnectAttempts >= _options.MaxReconnect) return;
            _connected = false;
            _transport?.Dispose();
            while (_reconnectAttempts < _options.MaxReconnect)
            {
                _reconnectAttempts++;
                try
                {
                    await Task.Delay(_options.ReconnectMs).ConfigureAwait(false);
                    await ConnectAsync().ConfigureAwait(false);
                    return;
                }
                catch (Exception ex) { OnError?.Invoke(ex); }
            }
            OnDisconnected?.Invoke("max reconnect attempts reached");
        }
    }

    // ============================================================
    // PushBroker — 强类型事件订阅器
    // ============================================================

    public sealed class PushBroker
    {
        private readonly bool _marshalToMainThread;
        private readonly Dictionary<string, List<Action<JObject>>> _handlers
            = new Dictionary<string, List<Action<JObject>>>();
        private readonly object _lock = new object();

        public PushBroker(bool marshalToMainThread) { _marshalToMainThread = marshalToMainThread; }

        public IDisposable On<T>(Action<T> handler) where T : MessageBase
        {
            var type = typeof(T).Name;
            Action<JObject> wrapped = obj =>
            {
                var typed = obj.ToObject<T>();
                if (_marshalToMainThread) DispatcherUtil.Post(() => handler(typed));
                else handler(typed);
            };
            lock (_lock)
            {
                if (!_handlers.TryGetValue(type, out var list))
                    _handlers[type] = list = new List<Action<JObject>>();
                list.Add(wrapped);
            }
            return new Subscription(this, type, wrapped);
        }

        public Task<T> OnceAsync<T>(TimeSpan timeout, CancellationToken ct = default)
            where T : MessageBase
        {
            var tcs = new TaskCompletionSource<T>(TaskCreationOptions.RunContinuationsAsynchronously);
            IDisposable sub = null;
            sub = On<T>(msg =>
            {
                if (tcs.TrySetResult(msg)) sub?.Dispose();
            });
            if (timeout > TimeSpan.Zero)
            {
                var linked = CancellationTokenSource.CreateLinkedTokenSource(ct);
                linked.CancelAfter(timeout);
                linked.Token.Register(() =>
                {
                    if (tcs.TrySetException(new TimeoutException($"Push.Once<{typeof(T).Name}> timeout")))
                        sub?.Dispose();
                });
            }
            return tcs.Task;
        }

        /// <summary>基于 Channel 的异步流订阅，使用 `await foreach` 消费</summary>
        public IAsyncEnumerable<T> OnPush<T>(int capacity = 64,
            CancellationToken ct = default) where T : MessageBase
        {
            var ch = Channel.CreateBounded<T>(new BoundedChannelOptions(capacity)
            {
                FullMode = BoundedChannelFullMode.DropOldest,
                SingleReader = true, SingleWriter = true,
            });
            var sub = On<T>(msg => ch.Writer.TryWrite(msg));
            ct.Register(() => { ch.Writer.TryComplete(); sub.Dispose(); });
            return ReadAll(ch, ct);
        }

        private static async IAsyncEnumerable<T> ReadAll<T>(Channel<T> ch,
            [System.Runtime.CompilerServices.EnumeratorCancellation] CancellationToken ct)
        {
            while (await ch.Reader.WaitToReadAsync(ct).ConfigureAwait(false))
                while (ch.Reader.TryRead(out var v))
                    yield return v;
        }

        public void Dispatch(string type, JObject payload)
        {
            List<Action<JObject>> snapshot = null;
            lock (_lock)
            {
                if (_handlers.TryGetValue(type, out var list) && list.Count > 0)
                    snapshot = new List<Action<JObject>>(list);
            }
            if (snapshot == null) return;
            foreach (var h in snapshot) { try { h(payload); } catch { /* ignore */ } }
        }

        private sealed class Subscription : IDisposable
        {
            private readonly PushBroker _broker; private readonly string _type;
            private readonly Action<JObject> _handler; private bool _disposed;
            public Subscription(PushBroker b, string t, Action<JObject> h) { _broker = b; _type = t; _handler = h; }
            public void Dispose()
            {
                if (_disposed) return;
                _disposed = true;
                lock (_broker._lock)
                {
                    if (_broker._handlers.TryGetValue(_type, out var list))
                        list.Remove(_handler);
                }
            }
        }
    }
}
