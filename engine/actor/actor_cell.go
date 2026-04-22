package actor

import (
	"fmt"
	"sync"
	"time"
)

// actorCell Actor单元，实现Process和Context接口
type actorCell struct {
	actor              Actor
	producer           Producer
	parent             *PID
	self               *PID
	children           map[string]*PID
	watchers           map[string]*PID
	watching           map[string]*PID
	supervisorStrategy SupervisorStrategy
	receiveTimeout     time.Duration
	behavior           *BehaviorStack
	mailbox            Mailbox
	message            interface{}
	rawMessage         interface{} // 原始消息（含信封），用于 Stash
	sender             *PID
	traceID            string // 当前消息的链路追踪 ID
	stopping           bool
	restarting         bool
	restartStats       *RestartStatistics
	receiveTimeoutTimer *time.Timer
	lifecycleHooks     *LifecycleHooks
	stash              *messageStash
	mu                 sync.RWMutex
}

// Process接口实现

func (cell *actorCell) SendUserMessage(pid *PID, message interface{}) {
	cell.mailbox.PostUserMessage(message)
}

func (cell *actorCell) SendSystemMessage(pid *PID, message interface{}) {
	cell.mailbox.PostSystemMessage(message)
}

func (cell *actorCell) Stop(pid *PID) {
	cell.SendSystemMessage(pid, &Stopping{})
}

// InfoContext接口实现

func (cell *actorCell) Self() *PID {
	return cell.self
}

func (cell *actorCell) Parent() *PID {
	return cell.parent
}

func (cell *actorCell) Children() []*PID {
	cell.mu.RLock()
	defer cell.mu.RUnlock()

	children := make([]*PID, 0, len(cell.children))
	for _, child := range cell.children {
		children = append(children, child)
	}
	return children
}

// MessageContext接口实现

func (cell *actorCell) Message() interface{} {
	return cell.message
}

func (cell *actorCell) TraceID() string {
	return cell.traceID
}

// SenderContext接口实现

func (cell *actorCell) Send(pid *PID, message interface{}) {
	sendMessageWithTrace(pid, message, cell.self, cell.traceID)
}

func (cell *actorCell) Request(pid *PID, message interface{}) {
	sendMessageWithTrace(pid, message, cell.self, cell.traceID)
}

func (cell *actorCell) RequestFuture(pid *PID, message interface{}, timeout time.Duration) *Future {
	future := NewFuture(timeout)
	futurePID := GeneratePID()
	future.SetPID(futurePID)

	futureProc := &futureProcess{future: future}
	defaultSystem.ProcessRegistry.Add(futurePID, futureProc)

	sendMessage(pid, message, futurePID)
	return future
}

// ReceiverContext接口实现

func (cell *actorCell) Respond(message interface{}) {
	if cell.sender != nil {
		sendMessage(cell.sender, message, cell.self)
	}
}

func (cell *actorCell) Sender() *PID {
	return cell.sender
}

func (cell *actorCell) SetReceiveTimeout(timeout time.Duration) {
	cell.receiveTimeout = timeout
	if cell.receiveTimeoutTimer != nil {
		cell.receiveTimeoutTimer.Stop()
	}
	if timeout > 0 {
		cell.receiveTimeoutTimer = time.AfterFunc(timeout, func() {
			cell.SendSystemMessage(cell.self, &ReceiveTimeout{})
		})
	}
}

func (cell *actorCell) CancelReceiveTimeout() {
	if cell.receiveTimeoutTimer != nil {
		cell.receiveTimeoutTimer.Stop()
		cell.receiveTimeoutTimer = nil
	}
}

// SpawnerContext接口实现

func (cell *actorCell) Spawn(props *Props) *PID {
	id := fmt.Sprintf("%s/$%d", cell.self.Id, len(cell.children)+1)
	return cell.SpawnNamed(props, id)
}

func (cell *actorCell) SpawnNamed(props *Props, name string) *PID {
	pid, err := props.spawn(name, cell.self)
	if err != nil {
		panic(err)
	}

	cell.mu.Lock()
	cell.children[pid.Id] = pid
	cell.mu.Unlock()

	defaultSystem.ProcessRegistry.Add(pid, pid.p)
	pid.p.SendSystemMessage(pid, &Started{})
	return pid
}

func (cell *actorCell) StopActor(pid *PID) {
	pid.p.Stop(pid)
}

func (cell *actorCell) Watch(pid *PID) {
	cell.mu.Lock()
	cell.watching[pid.Id] = pid
	cell.mu.Unlock()

	pid.p.SendSystemMessage(pid, &Watch{Watcher: cell.self})
}

func (cell *actorCell) Unwatch(pid *PID) {
	cell.mu.Lock()
	delete(cell.watching, pid.Id)
	cell.mu.Unlock()

	pid.p.SendSystemMessage(pid, &Unwatch{Watcher: cell.self})
}

// 消息处理

func (cell *actorCell) invokeUserMessage(message interface{}) {
	cell.processMessage(message, false)
}

func (cell *actorCell) invokeSystemMessage(message interface{}) {
	cell.processMessage(message, true)
}

func (cell *actorCell) processMessage(message interface{}, isSystem bool) {
	defer func() {
		if r := recover(); r != nil {
			cell.handlePanic(r)
		}
	}()

	// 解包消息信封，提取 sender 和 traceID
	actualMsg, sender, traceID := UnwrapEnvelopeFull(message)
	cell.message = actualMsg
	cell.rawMessage = message // 保留原始消息用于 Stash
	cell.sender = sender
	cell.traceID = traceID

	// 处理完成后归还信封到池中
	if env, ok := message.(*MessageEnvelope); ok {
		defer ReleaseEnvelope(env)
	}

	switch msg := actualMsg.(type) {
	case *Started:
		cell.invokePreStart()
		cell.behavior.Receive(cell)
	case *Stopping:
		cell.handleStopping()
	case *Stopped:
		cell.handleStopped()
	case *Restarting:
		cell.handleRestarting()
	case *Watch:
		cell.handleWatch(msg)
	case *Unwatch:
		cell.handleUnwatch(msg)
	case *Terminated:
		cell.handleTerminated(msg)
	case *PoisonPill:
		cell.Stop(cell.self)
	case *Failure:
		cell.handleFailure(msg)
	default:
		cell.behavior.Receive(cell)
	}
}

func (cell *actorCell) handlePanic(reason interface{}) {
	if cell.parent == nil {
		return
	}

	if cell.restartStats == nil {
		cell.restartStats = &RestartStatistics{}
	}

	failure := &Failure{
		Who:          cell.self,
		Reason:       reason,
		RestartStats: cell.restartStats,
	}

	cell.parent.p.SendSystemMessage(cell.parent, failure)
}

func (cell *actorCell) handleStopping() {
	cell.stopping = true
	cell.behavior.Receive(cell)
	cell.stopAllChildren()
	cell.SendSystemMessage(cell.self, &Stopped{})
}

func (cell *actorCell) handleStopped() {
	defaultSystem.ProcessRegistry.Remove(cell.self)
	cell.notifyWatchers()
	cell.CancelReceiveTimeout()
	cell.invokePostStop()
}

func (cell *actorCell) handleRestarting() {
	cell.restarting = true
	cell.invokePreRestart()
	cell.stopAllChildren()
	cell.actor = cell.producer()
	cell.behavior = NewBehaviorStack(cell.actor.Receive)
	cell.restarting = false
	cell.SendSystemMessage(cell.self, &Started{})
	cell.invokePostRestart()
}

func (cell *actorCell) handleWatch(msg *Watch) {
	cell.mu.Lock()
	cell.watchers[msg.Watcher.Id] = msg.Watcher
	cell.mu.Unlock()
}

func (cell *actorCell) handleUnwatch(msg *Unwatch) {
	cell.mu.Lock()
	delete(cell.watchers, msg.Watcher.Id)
	cell.mu.Unlock()
}

func (cell *actorCell) handleTerminated(msg *Terminated) {
	cell.mu.Lock()
	delete(cell.children, msg.Who.Id)
	delete(cell.watching, msg.Who.Id)
	cell.mu.Unlock()

	cell.behavior.Receive(cell)
}

func (cell *actorCell) stopAllChildren() {
	cell.mu.RLock()
	children := make([]*PID, 0, len(cell.children))
	for _, child := range cell.children {
		children = append(children, child)
	}
	cell.mu.RUnlock()

	for _, child := range children {
		child.p.Stop(child)
	}
}

func (cell *actorCell) notifyWatchers() {
	cell.mu.RLock()
	watchers := make([]*PID, 0, len(cell.watchers))
	for _, watcher := range cell.watchers {
		watchers = append(watchers, watcher)
	}
	cell.mu.RUnlock()

	for _, watcher := range watchers {
		watcher.p.SendSystemMessage(watcher, &Terminated{Who: cell.self})
	}
}

// StashContext 接口实现

func (cell *actorCell) Stash() error {
	if cell.stash == nil {
		cell.stash = newMessageStash(DefaultStashCapacity)
	}
	// 暂存原始消息（含信封信息），但不归还信封到池中
	// 需要复制信封以避免被 processMessage 的 defer ReleaseEnvelope 回收
	msg := cell.rawMessage
	if env, ok := msg.(*MessageEnvelope); ok {
		copied := &MessageEnvelope{
			Message: env.Message,
			Sender:  env.Sender,
			TraceID: env.TraceID,
		}
		msg = copied
	}
	return cell.stash.push(msg)
}

func (cell *actorCell) UnstashAll() {
	if cell.stash == nil {
		return
	}
	msgs := cell.stash.popAll()
	for _, m := range msgs {
		cell.mailbox.PostUserMessage(m.message)
	}
}

func (cell *actorCell) StashSize() int {
	if cell.stash == nil {
		return 0
	}
	return cell.stash.size()
}

// Supervisor接口实现

func (cell *actorCell) EscalateFailure(reason interface{}, message interface{}) {
	if cell.parent != nil {
		failure := &Failure{
			Who:          cell.self,
			Reason:       reason,
			RestartStats: cell.restartStats,
		}
		cell.parent.p.SendSystemMessage(cell.parent, failure)
	}
}

func (cell *actorCell) RestartChildren(pids ...*PID) {
	for _, pid := range pids {
		pid.p.SendSystemMessage(pid, &Restarting{})
	}
}

func (cell *actorCell) StopChildren(pids ...*PID) {
	for _, pid := range pids {
		pid.p.Stop(pid)
	}
}

func (cell *actorCell) ResumeChildren(pids ...*PID) {
	// 恢复子Actor（当前实现为空）
}

func (cell *actorCell) handleFailure(msg *Failure) {
	if cell.supervisorStrategy == nil {
		return
	}

	directive := cell.supervisorStrategy.HandleFailure(cell, msg.Who, msg.RestartStats, msg.Reason)

	switch directive {
	case ResumeDirective:
		cell.ResumeChildren(msg.Who)
	case RestartDirective:
		cell.RestartChildren(msg.Who)
	case StopDirective:
		cell.StopChildren(msg.Who)
	case EscalateDirective:
		cell.EscalateFailure(msg.Reason, msg)
	}
}

// 生命周期钩子调用

func (cell *actorCell) hookTimeout() time.Duration {
	if cell.lifecycleHooks != nil && cell.lifecycleHooks.HookTimeout > 0 {
		return cell.lifecycleHooks.HookTimeout
	}
	return DefaultHookTimeout
}

func (cell *actorCell) invokePreStart() {
	if cell.lifecycleHooks != nil && cell.lifecycleHooks.PreStart != nil {
		executeHook(cell, cell.lifecycleHooks.PreStart, cell.hookTimeout())
	}
}

func (cell *actorCell) invokePostStop() {
	if cell.lifecycleHooks != nil && cell.lifecycleHooks.PostStop != nil {
		executeHook(cell, cell.lifecycleHooks.PostStop, cell.hookTimeout())
	}
}

func (cell *actorCell) invokePreRestart() {
	if cell.lifecycleHooks != nil && cell.lifecycleHooks.PreRestart != nil {
		executeHook(cell, cell.lifecycleHooks.PreRestart, cell.hookTimeout())
	}
}

func (cell *actorCell) invokePostRestart() {
	if cell.lifecycleHooks != nil && cell.lifecycleHooks.PostRestart != nil {
		executeHook(cell, cell.lifecycleHooks.PostRestart, cell.hookTimeout())
	}
}






