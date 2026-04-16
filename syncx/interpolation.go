package syncx

// InterpolationConfig 插值配置
type InterpolationConfig struct {
	// BufferSize 状态缓冲大小（保留最近 N 个服务端快照用于插值）
	BufferSize int
	// InterpolationDelay 插值延迟帧数（越大越平滑但延迟越高）
	InterpolationDelay int
}

// DefaultInterpolationConfig 默认插值配置
func DefaultInterpolationConfig() InterpolationConfig {
	return InterpolationConfig{
		BufferSize:         8,
		InterpolationDelay: 2,
	}
}

// InterpolableValue 可插值的数值
type InterpolableValue struct {
	X float64
	Y float64
}

// EntitySnapshot 实体在某一帧的快照
type EntitySnapshot struct {
	FrameNum uint64
	Position InterpolableValue
	Rotation float64
	Extra    map[string]interface{} // 其他可同步的状态
}

// Interpolator 实体状态插值器
// 用于平滑展示其他玩家（非本地玩家）的位置更新
// 通过在两个服务端快照之间进行线性插值，避免跳变
type Interpolator struct {
	config   InterpolationConfig
	entities map[string]*entityBuffer // entityID → 状态缓冲
}

type entityBuffer struct {
	snapshots []EntitySnapshot
	head      int
	count     int
	capacity  int
}

// NewInterpolator 创建插值器
func NewInterpolator(config InterpolationConfig) *Interpolator {
	if config.BufferSize <= 0 {
		config.BufferSize = 8
	}
	if config.InterpolationDelay <= 0 {
		config.InterpolationDelay = 2
	}
	return &Interpolator{
		config:   config,
		entities: make(map[string]*entityBuffer),
	}
}

// AddEntity 注册实体
func (ip *Interpolator) AddEntity(entityID string) {
	ip.entities[entityID] = &entityBuffer{
		snapshots: make([]EntitySnapshot, ip.config.BufferSize),
		capacity:  ip.config.BufferSize,
	}
}

// RemoveEntity 注销实体
func (ip *Interpolator) RemoveEntity(entityID string) {
	delete(ip.entities, entityID)
}

// PushSnapshot 推入新的服务端快照
func (ip *Interpolator) PushSnapshot(entityID string, snapshot EntitySnapshot) {
	buf, ok := ip.entities[entityID]
	if !ok {
		return
	}

	idx := (buf.head + buf.count) % buf.capacity
	buf.snapshots[idx] = snapshot
	if buf.count < buf.capacity {
		buf.count++
	} else {
		buf.head = (buf.head + 1) % buf.capacity
	}
}

// GetInterpolatedState 获取实体的插值状态
// renderFrame: 当前渲染帧号（应该是服务端帧号 - 插值延迟）
// t: 帧内插值比例 [0, 1)
func (ip *Interpolator) GetInterpolatedState(entityID string, renderFrame uint64, t float64) (EntitySnapshot, bool) {
	buf, ok := ip.entities[entityID]
	if !ok || buf.count < 2 {
		// 数据不足，返回最新已知状态
		if ok && buf.count == 1 {
			return buf.snapshots[buf.head], true
		}
		return EntitySnapshot{}, false
	}

	// 找到 renderFrame 两侧的快照（用于插值）
	var prev, next *EntitySnapshot
	for i := 0; i < buf.count-1; i++ {
		idx := (buf.head + i) % buf.capacity
		nextIdx := (buf.head + i + 1) % buf.capacity
		s0 := &buf.snapshots[idx]
		s1 := &buf.snapshots[nextIdx]
		if s0.FrameNum <= renderFrame && s1.FrameNum > renderFrame {
			prev = s0
			next = s1
			break
		}
	}

	// 如果没找到区间，使用最新的两帧外推
	if prev == nil || next == nil {
		idx0 := (buf.head + buf.count - 2) % buf.capacity
		idx1 := (buf.head + buf.count - 1) % buf.capacity
		prev = &buf.snapshots[idx0]
		next = &buf.snapshots[idx1]
	}

	// 计算插值参数
	frameDiff := next.FrameNum - prev.FrameNum
	if frameDiff == 0 {
		return *next, true
	}
	alpha := float64(renderFrame-prev.FrameNum)/float64(frameDiff) + t/float64(frameDiff)
	if alpha < 0 {
		alpha = 0
	}
	if alpha > 1 {
		alpha = 1
	}

	// 线性插值
	result := EntitySnapshot{
		FrameNum: renderFrame,
		Position: InterpolableValue{
			X: prev.Position.X + (next.Position.X-prev.Position.X)*alpha,
			Y: prev.Position.Y + (next.Position.Y-prev.Position.Y)*alpha,
		},
		Rotation: prev.Rotation + (next.Rotation-prev.Rotation)*alpha,
	}

	return result, true
}

// GetExtrapolatedState 获取实体的外推状态（当服务端数据尚未到达时）
// 基于最近两帧的速度进行线性外推
func (ip *Interpolator) GetExtrapolatedState(entityID string, targetFrame uint64) (EntitySnapshot, bool) {
	buf, ok := ip.entities[entityID]
	if !ok || buf.count < 2 {
		if ok && buf.count == 1 {
			return buf.snapshots[buf.head], true
		}
		return EntitySnapshot{}, false
	}

	// 取最近两帧
	idx0 := (buf.head + buf.count - 2) % buf.capacity
	idx1 := (buf.head + buf.count - 1) % buf.capacity
	s0 := buf.snapshots[idx0]
	s1 := buf.snapshots[idx1]

	frameDiff := s1.FrameNum - s0.FrameNum
	if frameDiff == 0 {
		return s1, true
	}

	// 计算每帧速度
	vx := (s1.Position.X - s0.Position.X) / float64(frameDiff)
	vy := (s1.Position.Y - s0.Position.Y) / float64(frameDiff)
	vr := (s1.Rotation - s0.Rotation) / float64(frameDiff)

	// 外推
	dt := float64(targetFrame - s1.FrameNum)
	result := EntitySnapshot{
		FrameNum: targetFrame,
		Position: InterpolableValue{
			X: s1.Position.X + vx*dt,
			Y: s1.Position.Y + vy*dt,
		},
		Rotation: s1.Rotation + vr*dt,
	}

	return result, true
}

// LatestFrame 返回实体最新快照的帧号
func (ip *Interpolator) LatestFrame(entityID string) uint64 {
	buf, ok := ip.entities[entityID]
	if !ok || buf.count == 0 {
		return 0
	}
	idx := (buf.head + buf.count - 1) % buf.capacity
	return buf.snapshots[idx].FrameNum
}

// EntityCount 返回已注册的实体数量
func (ip *Interpolator) EntityCount() int {
	return len(ip.entities)
}
