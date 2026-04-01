package actor

import "time"

// ActorSystem Actor系统
type ActorSystem struct {
	ProcessRegistry *ProcessRegistry
	EventStream     *EventStream
	Root            *RootContext
	DeadLetter      *PID
	Address         string // 本地地址，用于远程通信
}

// NewActorSystem 创建Actor系统
func NewActorSystem() *ActorSystem {
	system := &ActorSystem{
		ProcessRegistry: NewProcessRegistry(),
		EventStream:     NewEventStream(),
	}

	// 创建死信进程
	deadLetterPID := NewLocalPID("deadletter")
	deadLetterProc := newDeadLetterProcess(system.EventStream)
	system.ProcessRegistry.Add(deadLetterPID, deadLetterProc)
	system.DeadLetter = deadLetterPID

	// 创建Root上下文
	system.Root = &RootContext{
		system: system,
	}

	return system
}

// RootContext 根上下文
type RootContext struct {
	system *ActorSystem
}

// Spawn 创建Actor
func (rc *RootContext) Spawn(props *Props) *PID {
	pid := GeneratePID()
	return rc.SpawnNamed(props, pid.Id)
}

// SpawnNamed 创建命名Actor
func (rc *RootContext) SpawnNamed(props *Props, name string) *PID {
	pid, err := props.spawn(name, nil)
	if err != nil {
		panic(err)
	}

	rc.system.ProcessRegistry.Add(pid, pid.p)
	pid.p.SendSystemMessage(pid, &Started{})
	return pid
}

// Stop 停止Actor
func (rc *RootContext) Stop(pid *PID) {
	sendSystemMessage(pid, &Stopping{})
}

// Send 发送消息
func (rc *RootContext) Send(pid *PID, message interface{}) {
	sendMessage(pid, message, nil)
}

// Request 请求消息
func (rc *RootContext) Request(pid *PID, message interface{}) {
	sendMessage(pid, message, nil)
}

// RequestFuture 异步请求
func (rc *RootContext) RequestFuture(pid *PID, message interface{}, timeout time.Duration) *Future {
	future := NewFuture(timeout)
	futurePID := GeneratePID()
	future.SetPID(futurePID)

	futureProc := &futureProcess{future: future}
	rc.system.ProcessRegistry.Add(futurePID, futureProc)

	sendMessage(pid, message, futurePID)
	return future
}

// 全局默认系统
var defaultSystem = NewActorSystem()

// DefaultSystem 返回全局默认 ActorSystem
func DefaultSystem() *ActorSystem {
	return defaultSystem
}

// 辅助函数

func sendMessage(pid *PID, message interface{}, sender *PID) {
	if pid == nil {
		return
	}

	process, ok := defaultSystem.ProcessRegistry.Get(pid)
	if !ok {
		defaultSystem.DeadLetter.p.SendUserMessage(defaultSystem.DeadLetter, &DeadLetterEvent{
			PID:     pid,
			Message: message,
			Sender:  sender,
		})
		return
	}

	// 使用信封携带 sender 信息
	process.SendUserMessage(pid, WrapEnvelope(message, sender))
}

func sendSystemMessage(pid *PID, message interface{}) {
	if pid == nil {
		return
	}

	process, ok := defaultSystem.ProcessRegistry.Get(pid)
	if !ok {
		return
	}

	process.SendSystemMessage(pid, message)
}
