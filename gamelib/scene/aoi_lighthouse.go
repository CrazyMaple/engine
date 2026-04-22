package scene

// LighthouseAOI 灯塔 AOI 算法
// 在场景上按固定间距放置"灯塔"（观察点），每个灯塔维护其观察范围内的实体列表
// 查询时只需检查实体所在灯塔及相邻灯塔的实体列表
// 优点：适合超大地图低密度分布，查询复杂度与总实体数无关
// 缺点：灯塔间距必须与视野范围匹配，内存占用与场景面积成正比
type LighthouseAOI struct {
	width, height float32
	cellSize      float32 // 灯塔间距
	viewRange     float32
	cols, rows    int
	towers        [][]*tower
	entities      map[string]*lighthouseEntity
}

// tower 灯塔节点
type tower struct {
	x, y     int
	watchers map[string]*GridEntity // 在此灯塔观察范围内的实体
}

// lighthouseEntity 灯塔中的实体信息
type lighthouseEntity struct {
	entity   *GridEntity
	towerX   int // 当前所在灯塔坐标
	towerY   int
	watching map[[2]int]bool // 该实体正在被哪些灯塔追踪
}

// NewLighthouseAOI 创建灯塔 AOI
func NewLighthouseAOI(width, height, cellSize, viewRange float32) *LighthouseAOI {
	if cellSize <= 0 {
		cellSize = viewRange
	}
	cols := int(width/cellSize) + 1
	rows := int(height/cellSize) + 1

	towers := make([][]*tower, rows)
	for r := 0; r < rows; r++ {
		towers[r] = make([]*tower, cols)
		for c := 0; c < cols; c++ {
			towers[r][c] = &tower{
				x:        c,
				y:        r,
				watchers: make(map[string]*GridEntity),
			}
		}
	}

	return &LighthouseAOI{
		width:     width,
		height:    height,
		cellSize:  cellSize,
		viewRange: viewRange,
		cols:      cols,
		rows:      rows,
		towers:    towers,
		entities:  make(map[string]*lighthouseEntity),
	}
}

func (l *LighthouseAOI) Add(entity *GridEntity) {
	if _, exists := l.entities[entity.ID]; exists {
		return
	}

	tx, ty := l.posToTower(entity.X, entity.Y)
	le := &lighthouseEntity{
		entity:   entity,
		towerX:   tx,
		towerY:   ty,
		watching: make(map[[2]int]bool),
	}
	l.entities[entity.ID] = le

	// 注册到所有覆盖的灯塔
	l.registerToTowers(le)
}

func (l *LighthouseAOI) Remove(entityID string) *GridEntity {
	le, ok := l.entities[entityID]
	if !ok {
		return nil
	}
	delete(l.entities, entityID)

	// 从所有灯塔移除
	l.unregisterFromTowers(le)

	return le.entity
}

func (l *LighthouseAOI) Get(entityID string) *GridEntity {
	if le, ok := l.entities[entityID]; ok {
		return le.entity
	}
	return nil
}

func (l *LighthouseAOI) Move(entityID string, newX, newY float32) (entered, left []*GridEntity) {
	le, ok := l.entities[entityID]
	if !ok {
		return nil, nil
	}

	oldTX, oldTY := le.towerX, le.towerY
	newTX, newTY := l.posToTower(newX, newY)

	// 先获取旧 AOI
	oldNearby := l.nearbySetFor(le)

	// 更新坐标
	le.entity.X = newX
	le.entity.Y = newY

	// 如果灯塔位置变化，需要重新注册
	if oldTX != newTX || oldTY != newTY {
		l.unregisterFromTowers(le)
		le.towerX = newTX
		le.towerY = newTY
		l.registerToTowers(le)
	}

	// 新 AOI
	newNearby := l.nearbySetFor(le)

	// 计算差集
	for id, e := range newNearby {
		if _, inOld := oldNearby[id]; !inOld {
			entered = append(entered, e)
		}
	}
	for id, e := range oldNearby {
		if _, inNew := newNearby[id]; !inNew {
			left = append(left, e)
		}
	}

	return entered, left
}

func (l *LighthouseAOI) GetNearby(entityID string) []*GridEntity {
	le, ok := l.entities[entityID]
	if !ok {
		return nil
	}
	nearby := l.nearbySetFor(le)
	result := make([]*GridEntity, 0, len(nearby))
	for _, e := range nearby {
		result = append(result, e)
	}
	return result
}

func (l *LighthouseAOI) EntityCount() int {
	return len(l.entities)
}

// posToTower 世界坐标转灯塔坐标
func (l *LighthouseAOI) posToTower(x, y float32) (int, int) {
	tx := int(x / l.cellSize)
	ty := int(y / l.cellSize)
	if tx < 0 {
		tx = 0
	}
	if ty < 0 {
		ty = 0
	}
	if tx >= l.cols {
		tx = l.cols - 1
	}
	if ty >= l.rows {
		ty = l.rows - 1
	}
	return tx, ty
}

// coverRange 计算实体视野覆盖的灯塔范围
func (l *LighthouseAOI) coverRange(tx, ty int) (minX, maxX, minY, maxY int) {
	// 视野能覆盖多少个灯塔格
	span := int(l.viewRange/l.cellSize) + 1

	minX = tx - span
	maxX = tx + span
	minY = ty - span
	maxY = ty + span

	if minX < 0 {
		minX = 0
	}
	if minY < 0 {
		minY = 0
	}
	if maxX >= l.cols {
		maxX = l.cols - 1
	}
	if maxY >= l.rows {
		maxY = l.rows - 1
	}
	return
}

// registerToTowers 将实体注册到覆盖范围内的所有灯塔
func (l *LighthouseAOI) registerToTowers(le *lighthouseEntity) {
	minX, maxX, minY, maxY := l.coverRange(le.towerX, le.towerY)
	for ty := minY; ty <= maxY; ty++ {
		for tx := minX; tx <= maxX; tx++ {
			l.towers[ty][tx].watchers[le.entity.ID] = le.entity
			le.watching[[2]int{tx, ty}] = true
		}
	}
}

// unregisterFromTowers 将实体从所有灯塔移除
func (l *LighthouseAOI) unregisterFromTowers(le *lighthouseEntity) {
	for key := range le.watching {
		tx, ty := key[0], key[1]
		if ty >= 0 && ty < l.rows && tx >= 0 && tx < l.cols {
			delete(l.towers[ty][tx].watchers, le.entity.ID)
		}
	}
	le.watching = make(map[[2]int]bool)
}

// nearbySetFor 获取实体 AOI 范围内的所有实体（通过灯塔索引）
func (l *LighthouseAOI) nearbySetFor(le *lighthouseEntity) map[string]*GridEntity {
	result := make(map[string]*GridEntity)
	vr := l.viewRange
	x, y := le.entity.X, le.entity.Y

	// 查询实体所在灯塔及相邻灯塔的实体
	minX, maxX, minY, maxY := l.coverRange(le.towerX, le.towerY)
	for ty := minY; ty <= maxY; ty++ {
		for tx := minX; tx <= maxX; tx++ {
			for id, e := range l.towers[ty][tx].watchers {
				if id == le.entity.ID {
					continue
				}
				dx := e.X - x
				dy := e.Y - y
				if dx >= -vr && dx <= vr && dy >= -vr && dy <= vr {
					result[id] = e
				}
			}
		}
	}
	return result
}
