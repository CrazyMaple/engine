package router

import (
	"fmt"
	"sync"
	"sync/atomic"

	"engine/actor"
)

// RouterState 路由策略接口
type RouterState interface {
	// RouteMessage 按策略路由消息
	RouteMessage(message interface{}, sender *actor.PID)
	// SetRoutees 设置路由目标列表
	SetRoutees(routees []*actor.PID)
	// GetRoutees 获取路由目标列表
	GetRoutees() []*actor.PID
}

// routerProcess 路由器进程，实现 actor.Process 接口
type routerProcess struct {
	state   RouterState
	system  *actor.ActorSystem
	mu      sync.RWMutex
}

func (r *routerProcess) SendUserMessage(pid *actor.PID, message interface{}) {
	// 处理管理消息
	switch msg := message.(type) {
	case *AddRoutee:
		r.addRoutee(msg.PID)
	case *RemoveRoutee:
		r.removeRoutee(msg.PID)
	default:
		// 路由消息，sender 信息在 envelope 外部无法获取，传 nil
		r.mu.RLock()
		r.state.RouteMessage(message, nil)
		r.mu.RUnlock()
	}
}

func (r *routerProcess) SendSystemMessage(pid *actor.PID, message interface{}) {
	// 路由器不处理系统消息
}

func (r *routerProcess) Stop(pid *actor.PID) {
	r.system.ProcessRegistry.Remove(pid)
}

func (r *routerProcess) addRoutee(pid *actor.PID) {
	r.mu.Lock()
	defer r.mu.Unlock()

	routees := r.state.GetRoutees()
	for _, existing := range routees {
		if existing.Equal(pid) {
			return
		}
	}
	r.state.SetRoutees(append(routees, pid))
}

func (r *routerProcess) removeRoutee(pid *actor.PID) {
	r.mu.Lock()
	defer r.mu.Unlock()

	routees := r.state.GetRoutees()
	newRoutees := make([]*actor.PID, 0, len(routees))
	for _, existing := range routees {
		if !existing.Equal(pid) {
			newRoutees = append(newRoutees, existing)
		}
	}
	r.state.SetRoutees(newRoutees)
}

// 路由器 PID 计数器
var routerCounter uint64

// spawnRouter 创建路由器并注册到进程表
func spawnRouter(system *actor.ActorSystem, state RouterState, routees []*actor.PID) *actor.PID {
	id := fmt.Sprintf("$router/%d", atomic.AddUint64(&routerCounter, 1))
	pid := actor.NewLocalPID(id)

	state.SetRoutees(routees)

	proc := &routerProcess{
		state:  state,
		system: system,
	}

	system.ProcessRegistry.Add(pid, proc)
	return pid
}
