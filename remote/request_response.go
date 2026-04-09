package remote

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"engine/actor"
	"engine/log"
)

// RemoteRequestMessage 远程请求消息（包含回调信息）
type RemoteRequestMessage struct {
	RequestID    string      `json:"request_id"`
	Message      interface{} `json:"message"`
	TypeName     string      `json:"type_name"`
	CallerAddr   string      `json:"caller_addr"`   // 发起方节点地址
	CallerPID    string      `json:"caller_pid"`     // 发起方 Future PID
}

// RemoteResponseMessage 远程响应消息
type RemoteResponseMessage struct {
	RequestID    string      `json:"request_id"`
	Message      interface{} `json:"message"`
	TypeName     string      `json:"type_name"`
	Error        string      `json:"error,omitempty"`
}

// RemoteFutureRegistry 管理跨节点请求的 Future 注册表
type RemoteFutureRegistry struct {
	mu      sync.RWMutex
	futures map[string]*remoteFutureEntry
}

type remoteFutureEntry struct {
	future    *actor.Future
	futurePID *actor.PID
	createdAt time.Time
}

// NewRemoteFutureRegistry 创建远程 Future 注册表
func NewRemoteFutureRegistry() *RemoteFutureRegistry {
	reg := &RemoteFutureRegistry{
		futures: make(map[string]*remoteFutureEntry),
	}
	// 启动清理协程：定时清理超时 Future
	go reg.cleanupLoop()
	return reg
}

// Register 注册一个远程 Future，返回唯一 RequestID
func (r *RemoteFutureRegistry) Register(future *actor.Future, futurePID *actor.PID) string {
	id := generateRequestID()
	r.mu.Lock()
	r.futures[id] = &remoteFutureEntry{
		future:    future,
		futurePID: futurePID,
		createdAt: time.Now(),
	}
	r.mu.Unlock()
	return id
}

// Complete 根据 RequestID 完成 Future
func (r *RemoteFutureRegistry) Complete(requestID string, message interface{}, errMsg string) bool {
	r.mu.Lock()
	entry, ok := r.futures[requestID]
	if ok {
		delete(r.futures, requestID)
	}
	r.mu.Unlock()

	if !ok {
		return false
	}

	// 通过 ProcessRegistry 获取 FutureProcess 并发送结果
	process, exists := actor.DefaultSystem().ProcessRegistry.Get(entry.futurePID)
	if !exists {
		return false
	}

	// 清理 ProcessRegistry 中的临时 FutureProcess
	defer actor.DefaultSystem().ProcessRegistry.Remove(entry.futurePID)

	if errMsg != "" {
		process.SendUserMessage(entry.futurePID, actor.WrapEnvelope(&remoteRequestError{msg: errMsg}, nil))
		return true
	}

	process.SendUserMessage(entry.futurePID, actor.WrapEnvelope(message, nil))
	return true
}

// PendingCount 返回等待中的请求数
func (r *RemoteFutureRegistry) PendingCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.futures)
}

func (r *RemoteFutureRegistry) cleanupLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		r.mu.Lock()
		now := time.Now()
		for id, entry := range r.futures {
			// 超过 5 分钟仍未完成的 Future 强制清理
			if now.Sub(entry.createdAt) > 5*time.Minute {
				actor.DefaultSystem().ProcessRegistry.Remove(entry.futurePID)
				delete(r.futures, id)
			}
		}
		r.mu.Unlock()
	}
}

type remoteRequestError struct {
	msg string
}

func (e *remoteRequestError) Error() string {
	return "remote request: " + e.msg
}

// RequestRemote 发送远程请求并等待响应
func (r *Remote) RequestRemote(target *actor.PID, message interface{}, timeout time.Duration) *actor.Future {
	future := actor.NewFuture(timeout)
	futurePID := actor.GeneratePID()
	future.SetPID(futurePID)

	// 注册 Future 进程用于接收响应
	futureProc := &actor.FutureProcess_Export{Future: future}
	r.system.ProcessRegistry.Add(futurePID, futureProc)

	// 注册到远程 Future 注册表
	if r.futureRegistry == nil {
		r.futureRegistry = NewRemoteFutureRegistry()
	}
	requestID := r.futureRegistry.Register(future, futurePID)

	// 查找类型名
	typeName, _ := defaultTypeRegistry.GetTypeName(message)

	// 构造远程请求消息
	reqMsg := &RemoteRequestMessage{
		RequestID:  requestID,
		Message:    message,
		TypeName:   typeName,
		CallerAddr: r.address,
		CallerPID:  futurePID.Id,
	}

	endpoint := r.endpointMgr.GetEndpoint(target.Address)
	if endpoint == nil {
		log.Error("Endpoint not found for request: %s", target.Address)
		future.SetPID(futurePID) // ensure PID
		return future
	}

	// 发送 RemoteRequestMessage 作为 RemoteMessage
	reqTypeName, _ := defaultTypeRegistry.GetTypeName(reqMsg)
	remoteMsg := &RemoteMessage{
		Target:   target,
		Sender:   &actor.PID{Address: r.address, Id: futurePID.Id},
		Message:  reqMsg,
		Type:     MessageTypeUser,
		TypeName: reqTypeName,
	}
	endpoint.Send(remoteMsg)

	return future
}

// handleRemoteResponse 处理远程响应（在接收端调用）
func (r *Remote) handleRemoteResponse(resp *RemoteResponseMessage) {
	if r.futureRegistry == nil {
		log.Error("Remote future registry not initialized, dropping response %s", resp.RequestID)
		return
	}
	r.futureRegistry.Complete(resp.RequestID, resp.Message, resp.Error)
}

// SendResponse 从接收端发送响应回发起方
// 供 Actor 在处理 RemoteRequestMessage 时调用
func (r *Remote) SendResponse(callerAddr, callerPID, requestID string, response interface{}, errMsg string) {
	typeName, _ := defaultTypeRegistry.GetTypeName(response)
	respMsg := &RemoteResponseMessage{
		RequestID: requestID,
		Message:   response,
		TypeName:  typeName,
		Error:     errMsg,
	}

	respTypeName, _ := defaultTypeRegistry.GetTypeName(respMsg)
	remoteMsg := &RemoteMessage{
		Target:   &actor.PID{Address: callerAddr, Id: callerPID},
		Message:  respMsg,
		Type:     MessageTypeUser,
		TypeName: respTypeName,
	}

	endpoint := r.endpointMgr.GetEndpoint(callerAddr)
	if endpoint == nil {
		log.Error("Cannot send response, endpoint not found: %s", callerAddr)
		return
	}
	endpoint.Send(remoteMsg)
}

func generateRequestID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func init() {
	// 注册远程请求/响应消息类型到全局类型注册表
	defaultTypeRegistry.Register(&RemoteRequestMessage{})
	defaultTypeRegistry.Register(&RemoteResponseMessage{})
}
