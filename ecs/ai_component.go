package ecs

import (
	"engine/bt"
)

// AIComponent AI 组件，持有行为树实例和黑板
type AIComponent struct {
	// Tree 行为树实例
	Tree *bt.Tree
	// Enabled 是否启用 AI 驱动
	Enabled bool
	// LODLevel 当前 LOD 级别（0=全速,1=半速,2=低速,3=暂停）
	LODLevel int
	// TickAccumulator LOD 降频计数器
	TickAccumulator int
	// LastStatus 上次 Tick 返回状态
	LastStatus bt.Status
	// TickCount 累计 Tick 次数
	TickCount uint64
	// TreeID 行为树模板 ID（用于调试和可视化）
	TreeID string
}

func (a *AIComponent) ComponentType() string { return "AI" }

// NewAIComponent 创建 AI 组件
func NewAIComponent(tree *bt.Tree, treeID string) *AIComponent {
	return &AIComponent{
		Tree:    tree,
		Enabled: true,
		TreeID:  treeID,
	}
}

// ShouldTick 根据 LOD 级别判断是否应该执行 Tick
func (a *AIComponent) ShouldTick() bool {
	if !a.Enabled {
		return false
	}
	// LOD 级别对应的 Tick 间隔（每 N 帧执行一次）
	interval := lodTickInterval(a.LODLevel)
	if interval <= 0 {
		return false // LOD 级别 3 = 暂停
	}
	a.TickAccumulator++
	if a.TickAccumulator >= interval {
		a.TickAccumulator = 0
		return true
	}
	return false
}

// lodTickInterval 返回 LOD 级别对应的帧间隔
func lodTickInterval(level int) int {
	switch level {
	case 0:
		return 1 // 每帧
	case 1:
		return 2 // 每 2 帧
	case 2:
		return 4 // 每 4 帧
	case 3:
		return 0 // 暂停
	default:
		return 1
	}
}
