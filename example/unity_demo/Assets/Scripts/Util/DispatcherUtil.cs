// Unity Demo — 主线程调度
//
// TypedGameClient 读循环运行在 ThreadPool 线程；而 Unity API 只能在主线程调用。
// 本工具把工作项投递到一个 SynchronizationContext 队列，由 MonoBehaviour.Update 抽取消费。

using System;
using System.Collections.Concurrent;
#if UNITY_2017_1_OR_NEWER
using UnityEngine;
#endif

namespace EngineUnityDemo
{
    public static class DispatcherUtil
    {
        private static readonly ConcurrentQueue<Action> _queue = new ConcurrentQueue<Action>();

        /// <summary>从任意线程投递工作项</summary>
        public static void Post(Action action)
        {
            if (action == null) return;
            _queue.Enqueue(action);
        }

        /// <summary>在 Unity 主线程 Update 中调用，消费全部积压任务</summary>
        public static void Pump(int maxPerFrame = 128)
        {
            int i = 0;
            while (i++ < maxPerFrame && _queue.TryDequeue(out var action))
            {
                try { action(); }
                catch (Exception ex) {
#if UNITY_2017_1_OR_NEWER
                    Debug.LogException(ex);
#else
                    System.Console.Error.WriteLine(ex);
#endif
                }
            }
        }
    }

#if UNITY_2017_1_OR_NEWER
    /// <summary>把 DispatcherUtil.Pump 自动挂进 MonoBehaviour 生命周期</summary>
    [DefaultExecutionOrder(-1000)]
    public sealed class DispatcherPump : MonoBehaviour
    {
        void Update() => DispatcherUtil.Pump();
    }
#endif
}
