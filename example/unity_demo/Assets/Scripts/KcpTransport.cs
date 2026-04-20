// Unity Demo — KCP 传输适配
//
// Unity 端建议使用 kcp2k (https://github.com/MirrorNetworking/kcp2k) 作为底层 KCP 实现。
// 为避免在示例工程中强依赖 kcp2k，这里仅提供接入骨架：
//   - 未引入 kcp2k 时，Create() 返回 Stub 实现（抛 NotSupportedException，引导安装）
//   - 引入后：把下方 KcpClientImpl 解注释 + 替换 TODO 处即可

using System;
using System.Threading;
using System.Threading.Tasks;

namespace EngineUnityDemo
{
    public static class KcpTransport
    {
        public static ITransport Create()
        {
#if KCP2K_AVAILABLE
            return new KcpClientImpl();
#else
            return new KcpStubTransport();
#endif
        }
    }

    internal sealed class KcpStubTransport : ITransport
    {
        public Task ConnectAsync(string address, CancellationToken ct) =>
            throw new NotSupportedException(
                "KCP 传输需要安装 kcp2k (https://github.com/MirrorNetworking/kcp2k) 并在 Player Settings " +
                "中定义 KCP2K_AVAILABLE 宏后启用 KcpClientImpl");

        public Task SendAsync(byte[] frame) => Task.CompletedTask;
        public Task<byte[]> ReceiveAsync(CancellationToken ct) => Task.FromResult<byte[]>(null);
        public void Dispose() { }
    }

#if KCP2K_AVAILABLE
    // ===== 启用后的真实实现骨架（编译要求添加 kcp2k 引用） =====
    //
    // using kcp2k;
    // internal sealed class KcpClientImpl : ITransport
    // {
    //     private KcpClient _client;
    //     private readonly System.Collections.Concurrent.ConcurrentQueue<byte[]> _inbox = new();
    //     private TaskCompletionSource<byte[]> _pending;
    //
    //     public Task ConnectAsync(string address, CancellationToken ct)
    //     {
    //         var parts = address.Split(':');
    //         var cfg = new KcpConfig(NoDelay: true, DualMode: false);
    //         _client = new KcpClient(
    //             OnConnected: () => {},
    //             OnData: (seg, ch) => {
    //                 var data = new byte[seg.Count];
    //                 Buffer.BlockCopy(seg.Array, seg.Offset, data, 0, seg.Count);
    //                 if (_pending != null && _pending.TrySetResult(data)) return;
    //                 _inbox.Enqueue(data);
    //             },
    //             OnDisconnected: () => _pending?.TrySetResult(null),
    //             OnError: (code, msg) => {},
    //             cfg);
    //         _client.Connect(parts[0], ushort.Parse(parts[1]));
    //         return Task.CompletedTask;
    //     }
    //
    //     public Task SendAsync(byte[] frame) {
    //         _client.Send(frame, KcpChannel.Reliable);
    //         return Task.CompletedTask;
    //     }
    //
    //     public Task<byte[]> ReceiveAsync(CancellationToken ct) {
    //         if (_inbox.TryDequeue(out var data)) return Task.FromResult(data);
    //         var tcs = new TaskCompletionSource<byte[]>();
    //         _pending = tcs;
    //         ct.Register(() => tcs.TrySetCanceled(ct));
    //         return tcs.Task;
    //     }
    //
    //     public void Dispose() { _client?.Disconnect(); }
    // }
#endif
}
