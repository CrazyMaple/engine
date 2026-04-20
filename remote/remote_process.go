package remote

import (
	"engine/actor"
	"engine/telemetry"
)

// RemoteProcess 远程Actor进程代理
type RemoteProcess struct {
	remote *Remote
}

// NewRemoteProcess 创建远程进程代理
func NewRemoteProcess(remote *Remote) *RemoteProcess {
	return &RemoteProcess{
		remote: remote,
	}
}

// SendUserMessage 发送用户消息
func (rp *RemoteProcess) SendUserMessage(pid *actor.PID, message interface{}) {
	// 解包信封，将 sender / TraceID 单独传递给 remote.Send，
	// 确保实际消息类型被正确序列化和类型注册查找
	msg, sender, traceID := actor.UnwrapEnvelopeFull(message)
	traceParent := formatTraceParent(traceID)
	rp.remote.SendWithTrace(pid, sender, msg, MessageTypeUser, traceParent)
}

// SendSystemMessage 发送系统消息
func (rp *RemoteProcess) SendSystemMessage(pid *actor.PID, message interface{}) {
	rp.remote.Send(pid, nil, message, MessageTypeSystem)
}

// Stop 停止远程Actor
func (rp *RemoteProcess) Stop(pid *actor.PID) {
	rp.remote.Send(pid, nil, &actor.Stopping{}, MessageTypeSystem)
}

// formatTraceParent 将 envelope.TraceID 规整为 W3C traceparent
// envelope 的 TraceID 可能是：
//   - 完整 traceparent "00-xx-yy-zz"：原样返回
//   - 32 字符 traceID：派生新子 span 补齐
//   - 其它短字符串：转为新根上下文但保留原 ID（兼容旧自定义格式）
func formatTraceParent(traceID string) string {
	if traceID == "" {
		return ""
	}
	tc := telemetry.ParseShort(traceID)
	if tc.IsValid() {
		return tc.TraceParent()
	}
	// 无效则以短 ID 作为 TraceID 生成新 Span
	if len(traceID) == 32 {
		tc = telemetry.TraceContext{TraceID: traceID, Flags: 0x01}.NewChild()
		return tc.TraceParent()
	}
	return ""
}
