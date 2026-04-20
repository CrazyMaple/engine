package skill

import "math"

// TargetShape 范围形状
type TargetShape int

const (
	ShapeNone     TargetShape = iota
	ShapeCircle               // 圆形（默认）
	ShapeSector               // 扇形
	ShapeRect                 // 矩形（以施法者朝向为 X 正向）
	ShapeLine                 // 直线（长度 Range 宽度 Width）
)

// TargetFilterFn 自定义筛选函数：在已通过形状过滤的候选上再次过滤
type TargetFilterFn func(t TargetPosition) bool

// TargetQuery 复杂选目标查询参数
type TargetQuery struct {
	Shape TargetShape
	// CenterX/Y 形状中心
	CenterX, CenterY float32
	// Radius 半径（Circle/Sector 使用）
	Radius float32
	// DirAngle 朝向角度（弧度，0 为 X 轴正向）
	DirAngle float32
	// FOV 视野角度（弧度，Sector 使用，例如 math.Pi/3 = 60 度张角）
	FOV float32
	// Width 矩形宽度（Rect 使用）或直线宽度（Line 使用）
	Width float32
	// Length 矩形长度（Rect 使用）或直线长度（Line 使用）
	Length float32
	// MaxCount 返回的最大目标数（0 = 不限制）
	MaxCount int
	// Filter 自定义筛选
	Filter TargetFilterFn
}

// Select 在候选列表里按 TargetQuery 过滤目标，返回命中的目标 ID 列表（按距离近→远排序）
func (q *TargetQuery) Select(candidates []TargetPosition) []string {
	type scored struct {
		id   string
		dist float32
	}
	hits := make([]scored, 0, len(candidates))
	for _, t := range candidates {
		if !q.shapeHit(t) {
			continue
		}
		if q.Filter != nil && !q.Filter(t) {
			continue
		}
		dx := t.X - q.CenterX
		dy := t.Y - q.CenterY
		hits = append(hits, scored{id: t.ID, dist: dx*dx + dy*dy})
	}
	// 冒泡按距离升序，候选通常较少，避免引入 sort 依赖开销
	for i := 1; i < len(hits); i++ {
		for j := i; j > 0 && hits[j-1].dist > hits[j].dist; j-- {
			hits[j-1], hits[j] = hits[j], hits[j-1]
		}
	}
	if q.MaxCount > 0 && len(hits) > q.MaxCount {
		hits = hits[:q.MaxCount]
	}
	out := make([]string, len(hits))
	for i, h := range hits {
		out[i] = h.id
	}
	return out
}

// shapeHit 单点是否落入当前形状
func (q *TargetQuery) shapeHit(t TargetPosition) bool {
	switch q.Shape {
	case ShapeCircle, ShapeNone:
		return inCircle(t.X, t.Y, q.CenterX, q.CenterY, q.Radius)
	case ShapeSector:
		if !inCircle(t.X, t.Y, q.CenterX, q.CenterY, q.Radius) {
			return false
		}
		return inSector(t.X, t.Y, q.CenterX, q.CenterY, q.DirAngle, q.FOV)
	case ShapeRect:
		return inRect(t.X, t.Y, q.CenterX, q.CenterY, q.DirAngle, q.Length, q.Width)
	case ShapeLine:
		return inLine(t.X, t.Y, q.CenterX, q.CenterY, q.DirAngle, q.Length, q.Width)
	}
	return false
}

func inCircle(px, py, cx, cy, r float32) bool {
	if r <= 0 {
		return false
	}
	dx := px - cx
	dy := py - cy
	return dx*dx+dy*dy <= r*r
}

func inSector(px, py, cx, cy, dir, fov float32) bool {
	if fov <= 0 {
		return false
	}
	dx := float64(px - cx)
	dy := float64(py - cy)
	// 向量为零时视为同点，归为命中
	if dx == 0 && dy == 0 {
		return true
	}
	angle := math.Atan2(dy, dx)
	diff := angleDiff(angle, float64(dir))
	return diff <= float64(fov)/2
}

// inRect 以施法者朝向 dir 为 X 正向构造局部坐标系，点落入矩形 [0, length] x [-width/2, width/2]
func inRect(px, py, cx, cy, dir, length, width float32) bool {
	if length <= 0 || width <= 0 {
		return false
	}
	dx := float64(px - cx)
	dy := float64(py - cy)
	cos := math.Cos(float64(dir))
	sin := math.Sin(float64(dir))
	// 世界坐标 → 局部坐标（旋转 -dir）
	lx := dx*cos + dy*sin
	ly := -dx*sin + dy*cos
	if lx < 0 || lx > float64(length) {
		return false
	}
	half := float64(width) / 2
	return ly >= -half && ly <= half
}

// inLine 直线命中：与 Rect 完全相同，区分仅在语义与 Width 的典型取值
func inLine(px, py, cx, cy, dir, length, width float32) bool {
	return inRect(px, py, cx, cy, dir, length, width)
}

// angleDiff 计算两个角度的最短绝对差值（弧度，0..π）
func angleDiff(a, b float64) float64 {
	d := math.Mod(a-b, 2*math.Pi)
	if d > math.Pi {
		d -= 2 * math.Pi
	} else if d < -math.Pi {
		d += 2 * math.Pi
	}
	if d < 0 {
		d = -d
	}
	return d
}

// TargetQueue 按优先级排序的目标队列（方便 AOE 多次命中的顺序控制）
type TargetQueue struct {
	items []TargetPosition
	score func(t TargetPosition) float32
}

// NewTargetQueue 创建队列，score 越小越优先
func NewTargetQueue(score func(t TargetPosition) float32) *TargetQueue {
	return &TargetQueue{score: score}
}

// Add 加入目标
func (q *TargetQueue) Add(t TargetPosition) {
	q.items = append(q.items, t)
	// 插入排序
	for i := len(q.items) - 1; i > 0; i-- {
		if q.score(q.items[i]) < q.score(q.items[i-1]) {
			q.items[i-1], q.items[i] = q.items[i], q.items[i-1]
		} else {
			break
		}
	}
}

// Pop 取出当前最优目标
func (q *TargetQueue) Pop() (TargetPosition, bool) {
	if len(q.items) == 0 {
		return TargetPosition{}, false
	}
	t := q.items[0]
	q.items = q.items[1:]
	return t, true
}

// Len 队列长度
func (q *TargetQueue) Len() int { return len(q.items) }

// ByDistance 返回一个按距离打分的 TargetQueue（距离中心越近优先）
func ByDistance(cx, cy float32) func(TargetPosition) float32 {
	return func(t TargetPosition) float32 {
		dx := t.X - cx
		dy := t.Y - cy
		return dx*dx + dy*dy
	}
}
