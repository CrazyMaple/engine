package remote

import "engine/actor"

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
	rp.remote.Send(pid, nil, message, MessageTypeUser)
}

// SendSystemMessage 发送系统消息
func (rp *RemoteProcess) SendSystemMessage(pid *actor.PID, message interface{}) {
	rp.remote.Send(pid, nil, message, MessageTypeSystem)
}

// Stop 停止远程Actor
func (rp *RemoteProcess) Stop(pid *actor.PID) {
	rp.remote.Send(pid, nil, &actor.Stopping{}, MessageTypeSystem)
}
