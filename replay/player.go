package replay

// Player 回放播放器
type Player struct {
	data       *ReplayData
	cursor     int     // 当前事件索引
	currentTime int64  // 当前播放时间（UnixNano）
	speed      float64 // 播放倍速
	paused     bool
	finished   bool
}

// NewPlayer 创建回放播放器
func NewPlayer(data *ReplayData) *Player {
	return &Player{
		data:        data,
		currentTime: data.StartTime,
		speed:       1.0,
	}
}

// Tick 推进回放到指定时间，返回本帧触发的事件
// deltaNano: 距上一帧经过的纳秒数
func (p *Player) Tick(deltaNano int64) []ReplayEvent {
	if p.paused || p.finished {
		return nil
	}

	// 按倍速推进时间
	p.currentTime += int64(float64(deltaNano) * p.speed)

	var triggered []ReplayEvent
	for p.cursor < len(p.data.Events) {
		event := p.data.Events[p.cursor]
		if event.Timestamp > p.currentTime {
			break
		}
		triggered = append(triggered, event)
		p.cursor++
	}

	if p.cursor >= len(p.data.Events) {
		p.finished = true
	}

	return triggered
}

// SetSpeed 设置播放倍速（1.0 = 正常，2.0 = 二倍速）
func (p *Player) SetSpeed(speed float64) {
	if speed <= 0 {
		speed = 1.0
	}
	p.speed = speed
}

// Speed 当前倍速
func (p *Player) Speed() float64 {
	return p.speed
}

// Pause 暂停播放
func (p *Player) Pause() {
	p.paused = true
}

// Resume 恢复播放
func (p *Player) Resume() {
	p.paused = false
}

// IsPaused 是否暂停中
func (p *Player) IsPaused() bool {
	return p.paused
}

// IsFinished 是否播放完毕
func (p *Player) IsFinished() bool {
	return p.finished
}

// SeekTo 跳转到指定时间戳
func (p *Player) SeekTo(timestamp int64) {
	p.currentTime = timestamp
	p.finished = false

	// 二分查找定位 cursor
	lo, hi := 0, len(p.data.Events)
	for lo < hi {
		mid := (lo + hi) / 2
		if p.data.Events[mid].Timestamp <= timestamp {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	p.cursor = lo
}

// Progress 返回播放进度 [0.0, 1.0]
func (p *Player) Progress() float64 {
	if p.data.Duration <= 0 {
		return 1.0
	}
	elapsed := p.currentTime - p.data.StartTime
	progress := float64(elapsed) / float64(p.data.Duration)
	if progress < 0 {
		return 0
	}
	if progress > 1 {
		return 1
	}
	return progress
}

// CurrentTime 当前播放时间
func (p *Player) CurrentTime() int64 {
	return p.currentTime
}

// TotalEvents 总事件数
func (p *Player) TotalEvents() int {
	return len(p.data.Events)
}

// RemainingEvents 剩余未播放事件数
func (p *Player) RemainingEvents() int {
	return len(p.data.Events) - p.cursor
}

// Reset 重置到开头
func (p *Player) Reset() {
	p.cursor = 0
	p.currentTime = p.data.StartTime
	p.paused = false
	p.finished = false
}
