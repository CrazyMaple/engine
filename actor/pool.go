package actor

import "sync"

// ---- MessageEnvelope 池 ----

var envelopePool = sync.Pool{
	New: func() interface{} { return &MessageEnvelope{} },
}

// AcquireEnvelope 从池中获取 MessageEnvelope 并初始化
func AcquireEnvelope(msg interface{}, sender *PID) *MessageEnvelope {
	env := envelopePool.Get().(*MessageEnvelope)
	env.Message = msg
	env.Sender = sender
	return env
}

// ReleaseEnvelope 将 MessageEnvelope 归还到池中
func ReleaseEnvelope(env *MessageEnvelope) {
	if env == nil {
		return
	}
	env.Message = nil
	env.Sender = nil
	env.TraceID = ""
	envelopePool.Put(env)
}

// ---- 字节缓冲区池 ----

var bufferPool = sync.Pool{
	New: func() interface{} {
		buf := make([]byte, 0, 4096)
		return &buf
	},
}

// AcquireBuffer 从池中获取字节缓冲区
func AcquireBuffer() *[]byte {
	return bufferPool.Get().(*[]byte)
}

// ReleaseBuffer 将字节缓冲区归还到池中
func ReleaseBuffer(buf *[]byte) {
	if buf == nil {
		return
	}
	*buf = (*buf)[:0]
	bufferPool.Put(buf)
}

// ---- PID 池 ----

var pidPool = sync.Pool{
	New: func() interface{} { return &PID{} },
}

// AcquirePID 从池中获取 PID
func AcquirePID(address, id string) *PID {
	pid := pidPool.Get().(*PID)
	pid.Address = address
	pid.Id = id
	pid.p = nil
	return pid
}

// ReleasePID 将 PID 归还到池中（仅用于临时 PID，如 Future 的 PID）
func ReleasePID(pid *PID) {
	if pid == nil {
		return
	}
	pid.Address = ""
	pid.Id = ""
	pid.p = nil
	pidPool.Put(pid)
}
