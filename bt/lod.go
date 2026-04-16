package bt

import (
	"math"
	"sync"
)

// LODLevel 行为树 LOD 级别
type LODLevel int

const (
	// LODFull 全速 Tick（每帧）
	LODFull LODLevel = 0
	// LODHalf 半速 Tick（每 2 帧）
	LODHalf LODLevel = 1
	// LODQuarter 低速 Tick（每 4 帧）
	LODQuarter LODLevel = 2
	// LODPaused 暂停 Tick
	LODPaused LODLevel = 3
)

// LODConfig LOD 配置
type LODConfig struct {
	// NearRange 近距范围阈值（LODFull）
	NearRange float64
	// MidRange 中距范围阈值（LODHalf）
	MidRange float64
	// FarRange 远距范围阈值（LODQuarter，超出则 LODPaused）
	FarRange float64
}

// DefaultLODConfig 默认 LOD 配置
func DefaultLODConfig() LODConfig {
	return LODConfig{
		NearRange: 100,
		MidRange:  300,
		FarRange:  600,
	}
}

// LODManager 行为树 LOD 管理器
// 根据实体与观察者的距离，自动调整行为树 Tick 频率
type LODManager struct {
	mu      sync.RWMutex
	config  LODConfig
	entries map[string]*LODEntry // entityID → LOD 状态
}

// LODEntry 单个实体的 LOD 状态
type LODEntry struct {
	EntityID    string
	Level       LODLevel
	Accumulator int // Tick 计数器
	Tree        *Tree
}

// NewLODManager 创建 LOD 管理器
func NewLODManager(config LODConfig) *LODManager {
	return &LODManager{
		config:  config,
		entries: make(map[string]*LODEntry),
	}
}

// Register 注册实体的行为树
func (m *LODManager) Register(entityID string, tree *Tree) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries[entityID] = &LODEntry{
		EntityID: entityID,
		Level:    LODFull,
		Tree:     tree,
	}
}

// Unregister 注销实体
func (m *LODManager) Unregister(entityID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.entries, entityID)
}

// UpdateLOD 根据实体位置和观察者位置更新 LOD 级别
func (m *LODManager) UpdateLOD(entityID string, entityX, entityY float64, observers []struct{ X, Y float64 }) {
	m.mu.Lock()
	defer m.mu.Unlock()

	entry, ok := m.entries[entityID]
	if !ok {
		return
	}

	entry.Level = m.calculateLevel(entityX, entityY, observers)
}

// ShouldTick 判断实体是否应该执行 Tick，并更新累加器
func (m *LODManager) ShouldTick(entityID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	entry, ok := m.entries[entityID]
	if !ok {
		return false
	}

	interval := tickInterval(entry.Level)
	if interval <= 0 {
		return false
	}

	entry.Accumulator++
	if entry.Accumulator >= interval {
		entry.Accumulator = 0
		return true
	}
	return false
}

// GetLevel 获取实体当前 LOD 级别
func (m *LODManager) GetLevel(entityID string) LODLevel {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if entry, ok := m.entries[entityID]; ok {
		return entry.Level
	}
	return LODPaused
}

// SetLevel 手动设置 LOD 级别
func (m *LODManager) SetLevel(entityID string, level LODLevel) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if entry, ok := m.entries[entityID]; ok {
		entry.Level = level
	}
}

// Count 返回注册的实体数量
func (m *LODManager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.entries)
}

// Stats 返回各 LOD 级别的实体数量统计
func (m *LODManager) Stats() map[LODLevel]int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := make(map[LODLevel]int)
	for _, entry := range m.entries {
		stats[entry.Level]++
	}
	return stats
}

// calculateLevel 计算到最近观察者的距离并确定 LOD 级别
func (m *LODManager) calculateLevel(x, y float64, observers []struct{ X, Y float64 }) LODLevel {
	if len(observers) == 0 {
		return LODFull // 无观察者时全速
	}

	minDist := math.MaxFloat64
	for _, obs := range observers {
		dx := x - obs.X
		dy := y - obs.Y
		dist := math.Sqrt(dx*dx + dy*dy)
		if dist < minDist {
			minDist = dist
		}
	}

	switch {
	case minDist <= m.config.NearRange:
		return LODFull
	case minDist <= m.config.MidRange:
		return LODHalf
	case minDist <= m.config.FarRange:
		return LODQuarter
	default:
		return LODPaused
	}
}

// tickInterval 返回 LOD 级别对应的帧间隔
func tickInterval(level LODLevel) int {
	switch level {
	case LODFull:
		return 1
	case LODHalf:
		return 2
	case LODQuarter:
		return 4
	case LODPaused:
		return 0
	default:
		return 1
	}
}
