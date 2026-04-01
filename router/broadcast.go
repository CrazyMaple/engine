package router

import "engine/actor"

// broadcastState 广播路由策略：将消息发送给所有 routee
type broadcastState struct {
	routees []*actor.PID
	system  *actor.ActorSystem
}

func (s *broadcastState) RouteMessage(message interface{}, sender *actor.PID) {
	for _, pid := range s.routees {
		s.system.Root.Send(pid, message)
	}
}

func (s *broadcastState) SetRoutees(routees []*actor.PID) {
	s.routees = routees
}

func (s *broadcastState) GetRoutees() []*actor.PID {
	return s.routees
}

// NewBroadcastGroup 创建广播路由器
// 消息将被发送到所有 routee
func NewBroadcastGroup(system *actor.ActorSystem, routees ...*actor.PID) *actor.PID {
	state := &broadcastState{system: system}
	return spawnRouter(system, state, routees)
}
