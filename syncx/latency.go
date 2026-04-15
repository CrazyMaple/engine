package syncx

import (
	"time"
)

// LatencyTracker 玩家延迟追踪器
type LatencyTracker struct {
	players    map[string]*playerLatency
	windowSize int // RTT 滑动窗口大小
}

type playerLatency struct {
	samples []time.Duration
	index   int
	filled  bool
}

// NewLatencyTracker 创建延迟追踪器
// windowSize: 滑动窗口大小（保留最近 N 次 RTT 采样）
func NewLatencyTracker(windowSize int) *LatencyTracker {
	if windowSize <= 0 {
		windowSize = 10
	}
	return &LatencyTracker{
		players:    make(map[string]*playerLatency),
		windowSize: windowSize,
	}
}

// OnPlayerJoin 玩家加入
func (lt *LatencyTracker) OnPlayerJoin(playerID string) {
	lt.players[playerID] = &playerLatency{
		samples: make([]time.Duration, lt.windowSize),
	}
}

// OnPlayerLeave 玩家离开
func (lt *LatencyTracker) OnPlayerLeave(playerID string) {
	delete(lt.players, playerID)
}

// RecordPong 记录 Pong 响应，计算 RTT
func (lt *LatencyTracker) RecordPong(pong *PongMsg) time.Duration {
	pl, ok := lt.players[pong.PlayerID]
	if !ok {
		return 0
	}

	rtt := time.Duration(time.Now().UnixNano() - pong.Timestamp)
	pl.samples[pl.index] = rtt
	pl.index = (pl.index + 1) % lt.windowSize
	if pl.index == 0 {
		pl.filled = true
	}

	return rtt
}

// GetRTT 获取玩家平均 RTT
func (lt *LatencyTracker) GetRTT(playerID string) time.Duration {
	pl, ok := lt.players[playerID]
	if !ok {
		return 0
	}

	count := lt.windowSize
	if !pl.filled {
		count = pl.index
	}
	if count == 0 {
		return 0
	}

	var total time.Duration
	for i := 0; i < count; i++ {
		total += pl.samples[i]
	}
	return total / time.Duration(count)
}

// GetHalfRTT 获取玩家单程延迟（RTT / 2）
func (lt *LatencyTracker) GetHalfRTT(playerID string) time.Duration {
	return lt.GetRTT(playerID) / 2
}

// CompensationFrames 计算延迟补偿帧数
// 根据玩家 RTT 和帧间隔，计算需要回退多少帧
func (lt *LatencyTracker) CompensationFrames(playerID string, frameInterval time.Duration) int {
	halfRTT := lt.GetHalfRTT(playerID)
	if halfRTT <= 0 || frameInterval <= 0 {
		return 0
	}
	frames := int(halfRTT / frameInterval)
	if frames < 0 {
		return 0
	}
	return frames
}

// AllRTTs 返回所有玩家的 RTT 快照
func (lt *LatencyTracker) AllRTTs() map[string]time.Duration {
	rtts := make(map[string]time.Duration, len(lt.players))
	for pid := range lt.players {
		rtts[pid] = lt.GetRTT(pid)
	}
	return rtts
}
