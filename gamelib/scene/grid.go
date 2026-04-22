package scene

import "engine/actor"

// GridConfig 网格配置
type GridConfig struct {
	Width    float32 // 场景宽度
	Height   float32 // 场景高度
	CellSize float32 // 单元格大小
}

// Cell 网格单元
type Cell struct {
	X, Y     int
	entities map[string]*GridEntity
}

// GridEntity 网格中的实体
type GridEntity struct {
	ID   string
	X, Y float32
	PID  *actor.PID
	Data interface{}
	cx   int // 当前所在格子 X
	cy   int // 当前所在格子 Y
}

// Grid 九宫格空间索引
type Grid struct {
	config GridConfig
	cols   int
	rows   int
	cells  [][]*Cell
	lookup map[string]*GridEntity // entityID → entity
}

// NewGrid 创建网格
func NewGrid(config GridConfig) *Grid {
	cols := int(config.Width/config.CellSize) + 1
	rows := int(config.Height/config.CellSize) + 1

	cells := make([][]*Cell, rows)
	for r := 0; r < rows; r++ {
		cells[r] = make([]*Cell, cols)
		for c := 0; c < cols; c++ {
			cells[r][c] = &Cell{
				X:        c,
				Y:        r,
				entities: make(map[string]*GridEntity),
			}
		}
	}

	return &Grid{
		config: config,
		cols:   cols,
		rows:   rows,
		cells:  cells,
		lookup: make(map[string]*GridEntity),
	}
}

// posToCell 世界坐标转格子坐标
func (g *Grid) posToCell(x, y float32) (int, int) {
	cx := int(x / g.config.CellSize)
	cy := int(y / g.config.CellSize)
	if cx < 0 {
		cx = 0
	}
	if cy < 0 {
		cy = 0
	}
	if cx >= g.cols {
		cx = g.cols - 1
	}
	if cy >= g.rows {
		cy = g.rows - 1
	}
	return cx, cy
}

// Add 添加实体到网格
func (g *Grid) Add(entity *GridEntity) {
	cx, cy := g.posToCell(entity.X, entity.Y)
	entity.cx = cx
	entity.cy = cy
	g.cells[cy][cx].entities[entity.ID] = entity
	g.lookup[entity.ID] = entity
}

// Remove 移除实体
func (g *Grid) Remove(entityID string) *GridEntity {
	entity, ok := g.lookup[entityID]
	if !ok {
		return nil
	}
	delete(g.cells[entity.cy][entity.cx].entities, entityID)
	delete(g.lookup, entityID)
	return entity
}

// Get 获取实体
func (g *Grid) Get(entityID string) *GridEntity {
	return g.lookup[entityID]
}

// Move 移动实体，返回新进入和离开 AOI 的实体列表
func (g *Grid) Move(entityID string, newX, newY float32) (entered, left []*GridEntity) {
	entity, ok := g.lookup[entityID]
	if !ok {
		return nil, nil
	}

	oldCX, oldCY := entity.cx, entity.cy
	newCX, newCY := g.posToCell(newX, newY)

	entity.X = newX
	entity.Y = newY

	// 同一格子内移动，无 AOI 变化
	if oldCX == newCX && oldCY == newCY {
		return nil, nil
	}

	// 从旧格子移到新格子
	delete(g.cells[oldCY][oldCX].entities, entityID)
	entity.cx = newCX
	entity.cy = newCY
	g.cells[newCY][newCX].entities[entityID] = entity

	// 计算 AOI 差集
	oldNeighbors := g.neighborSet(oldCX, oldCY)
	newNeighbors := g.neighborSet(newCX, newCY)

	// 新进入的格子（在新 AOI 但不在旧 AOI）
	for key, cell := range newNeighbors {
		if _, inOld := oldNeighbors[key]; !inOld {
			for _, e := range cell.entities {
				if e.ID != entityID {
					entered = append(entered, e)
				}
			}
		}
	}

	// 离开的格子（在旧 AOI 但不在新 AOI）
	for key, cell := range oldNeighbors {
		if _, inNew := newNeighbors[key]; !inNew {
			for _, e := range cell.entities {
				if e.ID != entityID {
					left = append(left, e)
				}
			}
		}
	}

	return entered, left
}

// GetAOI 获取实体的兴趣区域内所有其他实体
func (g *Grid) GetAOI(entityID string) []*GridEntity {
	entity, ok := g.lookup[entityID]
	if !ok {
		return nil
	}

	cells := g.GetNeighborCells(entity.cx, entity.cy)
	result := make([]*GridEntity, 0)
	for _, cell := range cells {
		for _, e := range cell.entities {
			if e.ID != entityID {
				result = append(result, e)
			}
		}
	}
	return result
}

// GetNeighborCells 获取九宫格（包含自身）
func (g *Grid) GetNeighborCells(cx, cy int) []*Cell {
	cells := make([]*Cell, 0, 9)
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			nx, ny := cx+dx, cy+dy
			if nx >= 0 && nx < g.cols && ny >= 0 && ny < g.rows {
				cells = append(cells, g.cells[ny][nx])
			}
		}
	}
	return cells
}

// neighborSet 返回九宫格 cell 集合（用于 AOI 差集计算）
func (g *Grid) neighborSet(cx, cy int) map[[2]int]*Cell {
	set := make(map[[2]int]*Cell, 9)
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			nx, ny := cx+dx, cy+dy
			if nx >= 0 && nx < g.cols && ny >= 0 && ny < g.rows {
				set[[2]int{nx, ny}] = g.cells[ny][nx]
			}
		}
	}
	return set
}

// EntityCount 场景中的实体数量
func (g *Grid) EntityCount() int {
	return len(g.lookup)
}
