package canary

import (
	"sync"

	"engine/actor"
)

// Labelable 可提取灰度标签的消息接口
// 消息实现此接口即可参与灰度路由
type Labelable interface {
	CanaryLabels() map[string]string
}

// CanaryRouterState 灰度感知的路由状态，实现 router.RouterState 接口
type CanaryRouterState struct {
	engine       *Engine
	system       *actor.ActorSystem
	allRoutees   []*actor.PID
	versionNodes map[string][]*actor.PID // version -> node PIDs
	mu           sync.RWMutex
}

// NewCanaryRouterState 创建灰度路由状态
func NewCanaryRouterState(engine *Engine, system *actor.ActorSystem) *CanaryRouterState {
	return &CanaryRouterState{
		engine:       engine,
		system:       system,
		versionNodes: make(map[string][]*actor.PID),
	}
}

// RouteMessage 根据灰度规则路由消息
func (s *CanaryRouterState) RouteMessage(message interface{}, sender *actor.PID) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// 提取灰度标签
	var labels map[string]string
	if lb, ok := message.(Labelable); ok {
		labels = lb.CanaryLabels()
	}

	// 灰度引擎决策
	if s.engine.IsEnabled() && labels != nil {
		targetVersion := s.engine.Route(labels)
		if targetVersion != "" {
			if nodes, ok := s.versionNodes[targetVersion]; ok && len(nodes) > 0 {
				// 确定性选择该版本的一个节点
				idx := hashSelect(labels, len(nodes))
				s.system.Root.Send(nodes[idx], message)
				return
			}
		}
	}

	// 回退：使用默认路由（第一个可用节点）
	if len(s.allRoutees) > 0 {
		s.system.Root.Send(s.allRoutees[0], message)
	}
}

// SetRoutees 设置全部路由目标（由 router 框架调用）
func (s *CanaryRouterState) SetRoutees(routees []*actor.PID) {
	s.mu.Lock()
	s.allRoutees = routees
	s.mu.Unlock()
}

// GetRoutees 获取全部路由目标
func (s *CanaryRouterState) GetRoutees() []*actor.PID {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.allRoutees
}

// UpdateVersionNodes 更新版本到节点的映射
func (s *CanaryRouterState) UpdateVersionNodes(mapping map[string][]*actor.PID) {
	s.mu.Lock()
	s.versionNodes = mapping
	s.mu.Unlock()
}

// hashSelect 基于标签的确定性选择
func hashSelect(labels map[string]string, n int) int {
	if n <= 0 {
		return 0
	}
	key := labels["user_id"]
	if key == "" {
		return 0
	}
	h := uint32(0)
	for _, c := range key {
		h = h*31 + uint32(c)
	}
	return int(h % uint32(n))
}
