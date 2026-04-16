package ecs

import (
	"math"
	"time"
)

// AISystem 驱动所有 AI Entity 的行为树 Tick
// 支持 LOD 降频：根据实体与观察点的距离自动调整 Tick 频率
type AISystem struct {
	priority int

	// LOD 配置
	LODEnabled    bool    // 是否启用 LOD
	LODNearRange  float32 // 近距范围（全速 Tick）
	LODMidRange   float32 // 中距范围（半速 Tick）
	LODFarRange   float32 // 远距范围（低速 Tick）

	// 观察点（通常为玩家位置）
	observers []ObserverPoint
}

// ObserverPoint 观察点，用于 LOD 距离计算
type ObserverPoint struct {
	X, Y float32
}

// NewAISystem 创建 AI 系统
func NewAISystem(priority int) *AISystem {
	return &AISystem{
		priority:     priority,
		LODEnabled:   true,
		LODNearRange: 100,
		LODMidRange:  300,
		LODFarRange:  600,
	}
}

func (s *AISystem) Name() string  { return "AISystem" }
func (s *AISystem) Priority() int { return s.priority }

// SetObservers 设置观察点列表（通常每帧由外部更新为玩家位置）
func (s *AISystem) SetObservers(observers []ObserverPoint) {
	s.observers = observers
}

// AddObserver 添加观察点
func (s *AISystem) AddObserver(x, y float32) {
	s.observers = append(s.observers, ObserverPoint{X: x, Y: y})
}

// ClearObservers 清空观察点
func (s *AISystem) ClearObservers() {
	s.observers = s.observers[:0]
}

// Update 每帧更新所有 AI 实体
func (s *AISystem) Update(world *World, deltaTime time.Duration) {
	entities := world.Query("AI")
	for _, e := range entities {
		comp, _ := e.Get("AI")
		ai := comp.(*AIComponent)
		if !ai.Enabled || ai.Tree == nil {
			continue
		}

		// LOD 更新
		if s.LODEnabled && len(s.observers) > 0 {
			pos := e.GetPosition()
			if pos != nil {
				ai.LODLevel = s.calculateLOD(pos.X, pos.Y)
			}
		}

		// 根据 LOD 决定是否 Tick
		if !ai.ShouldTick() {
			continue
		}

		// 将实体信息写入黑板供行为树使用
		bb := ai.Tree.Blackboard()
		bb.Set("entity_id", e.ID)
		bb.Set("delta_time", deltaTime.Seconds())

		// 写入位置信息
		if pos := e.GetPosition(); pos != nil {
			bb.Set("pos_x", float64(pos.X))
			bb.Set("pos_y", float64(pos.Y))
			bb.Set("pos_z", float64(pos.Z))
		}

		// 写入血量信息
		if hp := e.GetHealth(); hp != nil {
			bb.Set("health", hp.Current)
			bb.Set("health_max", hp.Max)
		}

		// 执行行为树
		ai.LastStatus = ai.Tree.Tick()
		ai.TickCount++
	}
}

// calculateLOD 计算实体到最近观察点的距离并返回 LOD 级别
func (s *AISystem) calculateLOD(x, y float32) int {
	minDist := float32(math.MaxFloat32)
	for _, obs := range s.observers {
		dx := x - obs.X
		dy := y - obs.Y
		dist := float32(math.Sqrt(float64(dx*dx + dy*dy)))
		if dist < minDist {
			minDist = dist
		}
	}

	switch {
	case minDist <= s.LODNearRange:
		return 0 // 全速
	case minDist <= s.LODMidRange:
		return 1 // 半速
	case minDist <= s.LODFarRange:
		return 2 // 低速
	default:
		return 3 // 暂停
	}
}

// CanParallel AISystem 读写 AI 和 Position 组件，不与其他系统并行
func (s *AISystem) CanParallel() bool { return false }
