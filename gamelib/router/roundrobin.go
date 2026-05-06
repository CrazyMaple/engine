package router

import (
	"sync/atomic"

	"engine/actor"
)

// roundRobinState 轮询路由策略：按顺序轮流发送
type roundRobinState struct {
	routees []*actor.PID
	system  *actor.ActorSystem
	index   uint64
}

func (s *roundRobinState) RouteMessage(message interface{}, sender *actor.PID) {
	if len(s.routees) == 0 {
		return
	}
	i := atomic.AddUint64(&s.index, 1) - 1
	pid := s.routees[i%uint64(len(s.routees))]
	s.system.Root.Send(pid, message)
}

func (s *roundRobinState) SetRoutees(routees []*actor.PID) {
	s.routees = routees
}

func (s *roundRobinState) GetRoutees() []*actor.PID {
	return s.routees
}

// NewRoundRobinGroup 创建轮询路由器
// 消息按顺序轮流发送到各 routee
func NewRoundRobinGroup(system *actor.ActorSystem, routees ...*actor.PID) *actor.PID {
	state := &roundRobinState{system: system}
	return spawnRouter(system, state, routees)
}
