package scene

import "engine/actor"

// AOI 兴趣区域接口，统一不同 AOI 算法的操作
// 实现：Grid（九宫格）、CrossLinkedList（十字链表）、Lighthouse（灯塔）
type AOI interface {
	// Add 添加实体
	Add(entity *GridEntity)
	// Remove 移除实体
	Remove(entityID string) *GridEntity
	// Get 获取实体
	Get(entityID string) *GridEntity
	// Move 移动实体，返回新进入和离开 AOI 的实体列表
	Move(entityID string, newX, newY float32) (entered, left []*GridEntity)
	// GetNearby 获取实体 AOI 范围内的所有其他实体
	GetNearby(entityID string) []*GridEntity
	// EntityCount 场景内实体总数
	EntityCount() int
}

// AOIAlgorithm AOI 算法类型
type AOIAlgorithm int

const (
	// AOIGrid 九宫格算法（默认），适合中小规模场景
	AOIGrid AOIAlgorithm = iota
	// AOICrossLink 十字链表算法，适合实体密集且频繁移动的大规模场景
	AOICrossLink
	// AOILighthouse 灯塔算法，适合超大地图低密度场景
	AOILighthouse
)

// AOIConfig AOI 通用配置
type AOIConfig struct {
	Width     float32      // 场景宽度
	Height    float32      // 场景高度
	ViewRange float32      // 视野半径
	Algorithm AOIAlgorithm // 算法选择
	CellSize  float32      // 九宫格/灯塔的单元格大小（AOIGrid/AOILighthouse 使用）
}

// NewAOI 根据配置创建对应的 AOI 实现
func NewAOI(config AOIConfig) AOI {
	switch config.Algorithm {
	case AOICrossLink:
		return NewCrossLinkedListAOI(config.Width, config.Height, config.ViewRange)
	case AOILighthouse:
		cellSize := config.CellSize
		if cellSize <= 0 {
			cellSize = config.ViewRange
		}
		return NewLighthouseAOI(config.Width, config.Height, cellSize, config.ViewRange)
	default:
		cellSize := config.CellSize
		if cellSize <= 0 {
			cellSize = config.ViewRange
		}
		return NewGridAOI(config.Width, config.Height, cellSize)
	}
}

// GridAOI 将现有 Grid 包装为 AOI 接口
type GridAOI struct {
	grid *Grid
}

// NewGridAOI 创建九宫格 AOI
func NewGridAOI(width, height, cellSize float32) *GridAOI {
	return &GridAOI{
		grid: NewGrid(GridConfig{Width: width, Height: height, CellSize: cellSize}),
	}
}

func (g *GridAOI) Add(entity *GridEntity) {
	g.grid.Add(entity)
}

func (g *GridAOI) Remove(entityID string) *GridEntity {
	return g.grid.Remove(entityID)
}

func (g *GridAOI) Get(entityID string) *GridEntity {
	return g.grid.Get(entityID)
}

func (g *GridAOI) Move(entityID string, newX, newY float32) (entered, left []*GridEntity) {
	return g.grid.Move(entityID, newX, newY)
}

func (g *GridAOI) GetNearby(entityID string) []*GridEntity {
	return g.grid.GetAOI(entityID)
}

func (g *GridAOI) EntityCount() int {
	return g.grid.EntityCount()
}

// --- GridEntity 构造辅助 ---

// NewGridEntity 创建 GridEntity
func NewGridEntity(id string, x, y float32, pid *actor.PID, data interface{}) *GridEntity {
	return &GridEntity{
		ID:   id,
		X:    x,
		Y:    y,
		PID:  pid,
		Data: data,
	}
}
