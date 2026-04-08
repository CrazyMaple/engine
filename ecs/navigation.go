package ecs

import (
	"time"
)

// Navigation 导航组件，驱动实体沿路径移动
type Navigation struct {
	// DestX, DestY 目标位置
	DestX, DestY float32
	// Path 当前路径（格子坐标序列）
	Path []NavPoint
	// PathIndex 当前路径点索引
	PathIndex int
	// Speed 移动速度（单位/秒）
	Speed float32
	// Arrived 是否已到达目标
	Arrived bool
	// CellSize 世界坐标到格子坐标的映射比例
	CellSize float32
}

// NavPoint 导航路径点
type NavPoint struct {
	X, Y float32
}

func (n *Navigation) ComponentType() string { return "Navigation" }

// CurrentTarget 获取当前目标路径点的世界坐标
func (n *Navigation) CurrentTarget() (float32, float32, bool) {
	if n.PathIndex >= len(n.Path) {
		return 0, 0, false
	}
	p := n.Path[n.PathIndex]
	return p.X, p.Y, true
}

// PathfindingSystem 寻路系统，使用 Navigation 组件驱动实体沿路径移动
// 需要实体同时拥有 Position 和 Navigation 组件
type PathfindingSystem struct {
	priority int
}

// NewPathfindingSystem 创建寻路系统
func NewPathfindingSystem(priority int) *PathfindingSystem {
	return &PathfindingSystem{priority: priority}
}

func (s *PathfindingSystem) Name() string  { return "PathfindingSystem" }
func (s *PathfindingSystem) Priority() int { return s.priority }

func (s *PathfindingSystem) Update(world *World, deltaTime time.Duration) {
	entities := world.QueryMulti("Position", "Navigation")
	dt := float32(deltaTime.Seconds())

	for _, e := range entities {
		nav := e.Get_Navigation()
		if nav == nil || nav.Arrived {
			continue
		}
		pos := e.GetPosition()
		if pos == nil {
			continue
		}

		targetX, targetY, hasTarget := nav.CurrentTarget()
		if !hasTarget {
			nav.Arrived = true
			continue
		}

		// 计算朝向目标的移动
		dx := targetX - pos.X
		dy := targetY - pos.Y
		dist := sqrt32(dx*dx + dy*dy)

		moveStep := nav.Speed * dt

		if dist <= moveStep {
			// 到达当前路径点
			pos.X = targetX
			pos.Y = targetY
			nav.PathIndex++

			// 检查是否到达最终目标
			if nav.PathIndex >= len(nav.Path) {
				nav.Arrived = true
			}
		} else {
			// 沿方向移动
			ratio := moveStep / dist
			pos.X += dx * ratio
			pos.Y += dy * ratio
		}

		// 更新 Movement 组件的速度向量（如果存在）
		if mov, ok := e.Get("Movement"); ok {
			m := mov.(*Movement)
			if nav.Arrived {
				m.VelocityX = 0
				m.VelocityY = 0
			} else {
				ndist := sqrt32(dx*dx + dy*dy)
				if ndist > 0 {
					m.VelocityX = (dx / ndist) * nav.Speed
					m.VelocityY = (dy / ndist) * nav.Speed
				}
			}
		}
	}
}

// CanParallel 寻路系统因修改 Position 等共享组件，不宜并行
func (s *PathfindingSystem) CanParallel() bool { return false }

// sqrt32 float32 平方根
func sqrt32(x float32) float32 {
	if x <= 0 {
		return 0
	}
	// 使用 Newton-Raphson 近似
	r := x
	for i := 0; i < 4; i++ {
		r = (r + x/r) / 2
	}
	return r
}

// --- Entity 快捷方法 ---

// Get_Navigation 快捷获取 Navigation 组件
func (e *Entity) Get_Navigation() *Navigation {
	if c, ok := e.Get("Navigation"); ok {
		return c.(*Navigation)
	}
	return nil
}

// GetMovement 快捷获取 Movement 组件
func (e *Entity) GetMovement() *Movement {
	if c, ok := e.Get("Movement"); ok {
		return c.(*Movement)
	}
	return nil
}
