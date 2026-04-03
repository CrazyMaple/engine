package ecs

import "time"

// Ticker 固定帧率驱动器，在 Actor 内部使用
// 非线程安全，应在单个 Actor 的消息处理中调用
type Ticker struct {
	world       *World
	systems     *SystemGroup
	interval    time.Duration // 帧间隔
	lastTick    time.Time     // 上次 Tick 时间
	frameCount  uint64        // 已执行帧数
	accumulator time.Duration // 累计时间（用于固定步长）
	parallel    bool          // 是否使用并行调度
}

// TickMsg Actor 内驱动 Tick 的消息
type TickMsg struct{}

// NewTicker 创建帧驱动器
// fps: 帧率，如 20 表示每秒 20 帧（50ms 间隔）
func NewTicker(world *World, systems *SystemGroup, fps int) *Ticker {
	interval := time.Second / time.Duration(fps)
	return &Ticker{
		world:    world,
		systems:  systems,
		interval: interval,
		lastTick: time.Now(),
	}
}

// SetParallel 设置是否使用并行系统调度
func (t *Ticker) SetParallel(parallel bool) {
	t.parallel = parallel
}

// Tick 执行一帧更新，使用固定步长
// 返回是否有帧被执行
func (t *Ticker) Tick() bool {
	now := time.Now()
	elapsed := now.Sub(t.lastTick)
	t.lastTick = now

	t.accumulator += elapsed
	executed := false

	// 固定步长循环，防止帧间隔抖动导致物理模拟不稳定
	for t.accumulator >= t.interval {
		t.accumulator -= t.interval
		t.update(t.interval)
		t.frameCount++
		executed = true
	}

	return executed
}

// TickOnce 执行单帧更新（不使用累积器）
func (t *Ticker) TickOnce(deltaTime time.Duration) {
	t.update(deltaTime)
	t.frameCount++
	t.lastTick = time.Now()
}

// update 内部更新
func (t *Ticker) update(deltaTime time.Duration) {
	if t.parallel {
		t.systems.UpdateParallel(t.world, deltaTime)
	} else {
		t.systems.Update(t.world, deltaTime)
	}
}

// FrameCount 已执行帧数
func (t *Ticker) FrameCount() uint64 {
	return t.frameCount
}

// Interval 帧间隔
func (t *Ticker) Interval() time.Duration {
	return t.interval
}

// World 获取关联的 World
func (t *Ticker) World() *World {
	return t.world
}

// Systems 获取关联的 SystemGroup
func (t *Ticker) Systems() *SystemGroup {
	return t.systems
}
