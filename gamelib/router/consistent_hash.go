package router

import (
	"hash/fnv"

	"engine/actor"
	"engine/log"
)

// consistentHashState 一致性哈希路由策略：根据消息 Hash 值路由到固定 routee
type consistentHashState struct {
	routees []*actor.PID
	system  *actor.ActorSystem
}

func (s *consistentHashState) RouteMessage(message interface{}, sender *actor.PID) {
	if len(s.routees) == 0 {
		return
	}

	h, ok := message.(Hasher)
	if !ok {
		log.Error("ConsistentHashRouter: message does not implement Hasher interface")
		return
	}

	key := h.Hash()
	idx := hashToIndex(key, len(s.routees))
	s.system.Root.Send(s.routees[idx], message)
}

func (s *consistentHashState) SetRoutees(routees []*actor.PID) {
	s.routees = routees
}

func (s *consistentHashState) GetRoutees() []*actor.PID {
	return s.routees
}

// hashToIndex 使用 FNV-1a 哈希将 key 映射到 [0, n) 范围
func hashToIndex(key string, n int) int {
	h := fnv.New32a()
	h.Write([]byte(key))
	return int(h.Sum32()) % n
}

// NewConsistentHashGroup 创建一致性哈希路由器
// 消息需实现 Hasher 接口，相同 Hash 值的消息总是路由到同一个 routee
func NewConsistentHashGroup(system *actor.ActorSystem, routees ...*actor.PID) *actor.PID {
	state := &consistentHashState{system: system}
	return spawnRouter(system, state, routees)
}
