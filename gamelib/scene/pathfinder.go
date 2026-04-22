package scene

import (
	"container/heap"
	"math"
	"sync"
)

// Point 2D 坐标点
type Point struct {
	X, Y int
}

// Vector2 浮点坐标
type Vector2 struct {
	X, Y float32
}

// Pathfinder 寻路接口
type Pathfinder interface {
	// FindPath 从起点到终点寻路，返回路径点列表（含起终点）
	// 返回 nil 表示无法到达
	FindPath(from, to Point) []Point
	// SetWalkable 设置某格是否可通行
	SetWalkable(x, y int, walkable bool)
	// IsWalkable 查询某格是否可通行
	IsWalkable(x, y int) bool
	// SetWeight 设置某格的移动代价权重（默认 1.0）
	SetWeight(x, y int, weight float32)
}

// PathCache 路径缓存
type PathCache struct {
	cache map[[4]int][]Point // [fromX, fromY, toX, toY] -> path
	mu    sync.RWMutex
	maxSize int
}

// NewPathCache 创建路径缓存
func NewPathCache(maxSize int) *PathCache {
	if maxSize <= 0 {
		maxSize = 1024
	}
	return &PathCache{
		cache:   make(map[[4]int][]Point),
		maxSize: maxSize,
	}
}

// Get 查缓存
func (pc *PathCache) Get(from, to Point) ([]Point, bool) {
	pc.mu.RLock()
	defer pc.mu.RUnlock()
	path, ok := pc.cache[[4]int{from.X, from.Y, to.X, to.Y}]
	if !ok {
		return nil, false
	}
	// 返回副本
	result := make([]Point, len(path))
	copy(result, path)
	return result, true
}

// Put 写缓存
func (pc *PathCache) Put(from, to Point, path []Point) {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	if len(pc.cache) >= pc.maxSize {
		// 简单 LRU：清空一半
		count := 0
		for k := range pc.cache {
			delete(pc.cache, k)
			count++
			if count >= pc.maxSize/2 {
				break
			}
		}
	}
	cached := make([]Point, len(path))
	copy(cached, path)
	pc.cache[[4]int{from.X, from.Y, to.X, to.Y}] = cached
}

// Invalidate 使缓存失效（地图变化时调用）
func (pc *PathCache) Invalidate() {
	pc.mu.Lock()
	pc.cache = make(map[[4]int][]Point)
	pc.mu.Unlock()
}

// AStarPathfinder Grid-based A* 寻路
type AStarPathfinder struct {
	width, height int
	grid          [][]pathCell
	cache         *PathCache
	allowDiagonal bool // 是否允许对角线移动
}

// pathCell 寻路格子信息
type pathCell struct {
	walkable bool
	weight   float32
}

// NewAStarPathfinder 创建 A* 寻路器
// width/height: 网格尺寸；allowDiagonal: 是否允许斜向移动
func NewAStarPathfinder(width, height int, allowDiagonal bool) *AStarPathfinder {
	grid := make([][]pathCell, height)
	for y := 0; y < height; y++ {
		grid[y] = make([]pathCell, width)
		for x := 0; x < width; x++ {
			grid[y][x] = pathCell{walkable: true, weight: 1.0}
		}
	}
	return &AStarPathfinder{
		width:         width,
		height:        height,
		grid:          grid,
		cache:         NewPathCache(1024),
		allowDiagonal: allowDiagonal,
	}
}

func (a *AStarPathfinder) SetWalkable(x, y int, walkable bool) {
	if x >= 0 && x < a.width && y >= 0 && y < a.height {
		a.grid[y][x].walkable = walkable
		a.cache.Invalidate()
	}
}

func (a *AStarPathfinder) IsWalkable(x, y int) bool {
	if x < 0 || x >= a.width || y < 0 || y >= a.height {
		return false
	}
	return a.grid[y][x].walkable
}

func (a *AStarPathfinder) SetWeight(x, y int, weight float32) {
	if x >= 0 && x < a.width && y >= 0 && y < a.height {
		a.grid[y][x].weight = weight
		a.cache.Invalidate()
	}
}

// Cache 返回路径缓存（可供外部管理）
func (a *AStarPathfinder) Cache() *PathCache {
	return a.cache
}

// FindPath A* 寻路
func (a *AStarPathfinder) FindPath(from, to Point) []Point {
	// 边界和可达性检查
	if !a.IsWalkable(from.X, from.Y) || !a.IsWalkable(to.X, to.Y) {
		return nil
	}
	if from.X == to.X && from.Y == to.Y {
		return []Point{from}
	}

	// 查缓存
	if cached, ok := a.cache.Get(from, to); ok {
		return cached
	}

	// A* 核心算法
	openSet := &astarHeap{}
	heap.Init(openSet)

	cameFrom := make(map[Point]Point)
	gScore := make(map[Point]float32)
	gScore[from] = 0

	heap.Push(openSet, &astarNode{
		point: from,
		f:     heuristic(from, to),
	})

	closed := make(map[Point]bool)

	for openSet.Len() > 0 {
		current := heap.Pop(openSet).(*astarNode)

		if current.point.X == to.X && current.point.Y == to.Y {
			path := a.reconstructPath(cameFrom, to)
			a.cache.Put(from, to, path)
			return path
		}

		if closed[current.point] {
			continue
		}
		closed[current.point] = true

		for _, neighbor := range a.neighbors(current.point) {
			if closed[neighbor] {
				continue
			}

			moveCost := a.grid[neighbor.Y][neighbor.X].weight
			// 对角线移动代价 * sqrt(2)
			if neighbor.X != current.point.X && neighbor.Y != current.point.Y {
				moveCost *= 1.414
			}

			tentativeG := gScore[current.point] + moveCost

			if oldG, exists := gScore[neighbor]; exists && tentativeG >= oldG {
				continue
			}

			cameFrom[neighbor] = current.point
			gScore[neighbor] = tentativeG
			f := tentativeG + heuristic(neighbor, to)

			heap.Push(openSet, &astarNode{
				point: neighbor,
				f:     f,
			})
		}
	}

	return nil // 无法到达
}

// neighbors 获取可通行的邻居格子
func (a *AStarPathfinder) neighbors(p Point) []Point {
	dirs4 := [][2]int{{0, -1}, {0, 1}, {-1, 0}, {1, 0}}
	dirs8 := [][2]int{{0, -1}, {0, 1}, {-1, 0}, {1, 0}, {-1, -1}, {-1, 1}, {1, -1}, {1, 1}}

	dirs := dirs4
	if a.allowDiagonal {
		dirs = dirs8
	}

	result := make([]Point, 0, len(dirs))
	for _, d := range dirs {
		nx, ny := p.X+d[0], p.Y+d[1]
		if nx >= 0 && nx < a.width && ny >= 0 && ny < a.height && a.grid[ny][nx].walkable {
			// 对角线移动时检查相邻格不被阻挡（防穿角）
			if d[0] != 0 && d[1] != 0 {
				if !a.grid[p.Y][nx].walkable || !a.grid[ny][p.X].walkable {
					continue
				}
			}
			result = append(result, Point{nx, ny})
		}
	}
	return result
}

// reconstructPath 从 cameFrom 重建路径
func (a *AStarPathfinder) reconstructPath(cameFrom map[Point]Point, current Point) []Point {
	path := []Point{current}
	for {
		prev, ok := cameFrom[current]
		if !ok {
			break
		}
		path = append(path, prev)
		current = prev
	}
	// 反转
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	return path
}

// heuristic 启发函数（切比雪夫距离，适配 8 方向移动）
func heuristic(a, b Point) float32 {
	dx := math.Abs(float64(a.X - b.X))
	dy := math.Abs(float64(a.Y - b.Y))
	// Octile distance
	if dx > dy {
		return float32(1.414*dy + (dx - dy))
	}
	return float32(1.414*dx + (dy - dx))
}

// --- A* 优先队列实现 ---

type astarNode struct {
	point Point
	f     float32
	index int
}

type astarHeap []*astarNode

func (h astarHeap) Len() int            { return len(h) }
func (h astarHeap) Less(i, j int) bool   { return h[i].f < h[j].f }
func (h astarHeap) Swap(i, j int)        { h[i], h[j] = h[j], h[i]; h[i].index = i; h[j].index = j }

func (h *astarHeap) Push(x interface{}) {
	n := x.(*astarNode)
	n.index = len(*h)
	*h = append(*h, n)
}

func (h *astarHeap) Pop() interface{} {
	old := *h
	n := len(old)
	node := old[n-1]
	old[n-1] = nil
	*h = old[:n-1]
	return node
}

// --- NavMesh 预留接口 ---

// NavMeshPathfinder NavMesh 寻路接口预留（3D 寻路扩展）
type NavMeshPathfinder interface {
	Pathfinder
	// LoadNavMesh 加载导航网格数据
	LoadNavMesh(data []byte) error
	// FindPath3D 3D 寻路
	FindPath3D(from, to Vector2) []Vector2
}
