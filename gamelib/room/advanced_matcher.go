package room

import (
	"math"
	"sort"
	"sync"
	"time"
)

// --- 多维度匹配器（v1.11 ELO/MMR 进阶） ---
//
// 维度：
//   - 段位差 (|r_i - r_j|)：越小越好
//   - 网络延迟差 (|lat_i - lat_j|)：越小越好
//   - 等待时间容忍度：等待越久，允许越大的误差
//   - 自定义标签（地区、阵营）可通过 Filter 控制
//
// 匹配目标：最小化加权误差总和。

// AdvancedMatcherConfig 多维匹配器配置
type AdvancedMatcherConfig struct {
	// PlayersPerMatch 每局玩家数
	PlayersPerMatch int

	// RatingWeight 段位差权重（单位：分数差→误差贡献）
	RatingWeight float64
	// LatencyWeight 延迟差权重（单位：毫秒→误差贡献）
	LatencyWeight float64

	// MaxAllowedCost 初始最大可接受总误差（新手匹配推荐 300）
	MaxAllowedCost float64
	// CostGrowthPerSecond 每等待 1 秒，误差上限提升的增量
	CostGrowthPerSecond float64
	// CostCeiling 误差上限天花板（等再久也不能突破）
	CostCeiling float64

	// MMRStore 可选 MMR 存储；若提供则使用 EffectiveRating 代替 Rating
	MMRStore MMRStore

	// TagFilter 可选硬性过滤（返回 false 则两人必不可匹配）
	TagFilter func(a, b PlayerInfo) bool
}

// DefaultAdvancedConfig 返回合理的默认配置
func DefaultAdvancedConfig() AdvancedMatcherConfig {
	return AdvancedMatcherConfig{
		PlayersPerMatch:     2,
		RatingWeight:        1.0,
		LatencyWeight:       0.5,
		MaxAllowedCost:      300,
		CostGrowthPerSecond: 20,
		CostCeiling:         2000,
	}
}

// advancedEntry 包含进入时间的条目
type advancedEntry struct {
	Player   PlayerInfo
	JoinedAt time.Time
	Latency  int // 毫秒，来自 Player.Metadata["latency_ms"]
}

// AdvancedMatcher 多维度匹配器
type AdvancedMatcher struct {
	cfg   AdvancedMatcherConfig
	queue []advancedEntry
	mu    sync.Mutex
	now   func() time.Time // 测试注入

	// 质量指标
	totalMatches      int
	totalWaitSum      time.Duration
	totalRatingSpread float64
}

// NewAdvancedMatcher 创建匹配器
func NewAdvancedMatcher(cfg AdvancedMatcherConfig) *AdvancedMatcher {
	if cfg.PlayersPerMatch <= 0 {
		cfg.PlayersPerMatch = 2
	}
	if cfg.RatingWeight <= 0 {
		cfg.RatingWeight = 1
	}
	if cfg.LatencyWeight < 0 {
		cfg.LatencyWeight = 0
	}
	if cfg.MaxAllowedCost <= 0 {
		cfg.MaxAllowedCost = 300
	}
	if cfg.CostCeiling <= 0 {
		cfg.CostCeiling = math.Max(2000, cfg.MaxAllowedCost*10)
	}
	return &AdvancedMatcher{cfg: cfg, now: time.Now}
}

// SetNow 测试注入时间
func (m *AdvancedMatcher) SetNow(fn func() time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.now = fn
}

// AddPlayer 入队
func (m *AdvancedMatcher) AddPlayer(player PlayerInfo) {
	m.mu.Lock()
	defer m.mu.Unlock()
	lat := 0
	if player.Metadata != nil {
		if v, ok := player.Metadata["latency_ms"]; ok {
			switch n := v.(type) {
			case int:
				lat = n
			case float64:
				lat = int(n)
			}
		}
	}
	m.queue = append(m.queue, advancedEntry{
		Player:   player,
		JoinedAt: m.now(),
		Latency:  lat,
	})
}

// RemovePlayer 出队
func (m *AdvancedMatcher) RemovePlayer(playerID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, e := range m.queue {
		if e.Player.PlayerID == playerID {
			m.queue = append(m.queue[:i], m.queue[i+1:]...)
			return
		}
	}
}

// QueueSize 队列长度
func (m *AdvancedMatcher) QueueSize() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.queue)
}

// pairCost 计算两玩家的匹配误差
func (m *AdvancedMatcher) pairCost(a, b advancedEntry) float64 {
	ra := EffectiveRating(a.Player, m.cfg.MMRStore)
	rb := EffectiveRating(b.Player, m.cfg.MMRStore)
	rDiff := math.Abs(ra - rb)
	lDiff := math.Abs(float64(a.Latency - b.Latency))
	return rDiff*m.cfg.RatingWeight + lDiff*m.cfg.LatencyWeight
}

// allowedCost 根据锚点等待时间动态扩大
func (m *AdvancedMatcher) allowedCost(anchor advancedEntry, now time.Time) float64 {
	waited := now.Sub(anchor.JoinedAt)
	c := m.cfg.MaxAllowedCost + waited.Seconds()*m.cfg.CostGrowthPerSecond
	if c > m.cfg.CostCeiling {
		return m.cfg.CostCeiling
	}
	return c
}

// Match 尝试匹配。使用"锚点 + 贪心配伴"的策略：
// 以等待最久的玩家为锚点，选择 cost 最低的若干队友。
func (m *AdvancedMatcher) Match() []PlayerInfo {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.queue) < m.cfg.PlayersPerMatch {
		return nil
	}
	now := m.now()

	// 按等待时间升序（最老的在前）
	order := make([]int, len(m.queue))
	for i := range m.queue {
		order[i] = i
	}
	sort.Slice(order, func(i, j int) bool {
		return m.queue[order[i]].JoinedAt.Before(m.queue[order[j]].JoinedAt)
	})

	for _, anchorIdx := range order {
		anchor := m.queue[anchorIdx]
		maxCost := m.allowedCost(anchor, now)

		// 收集候选（通过硬过滤），按 cost 排序
		type cand struct {
			idx  int
			cost float64
		}
		cands := make([]cand, 0, len(m.queue)-1)
		for j, e := range m.queue {
			if j == anchorIdx {
				continue
			}
			if m.cfg.TagFilter != nil && !m.cfg.TagFilter(anchor.Player, e.Player) {
				continue
			}
			c := m.pairCost(anchor, e)
			if c <= maxCost {
				cands = append(cands, cand{idx: j, cost: c})
			}
		}
		if len(cands) < m.cfg.PlayersPerMatch-1 {
			continue
		}
		sort.Slice(cands, func(i, j int) bool { return cands[i].cost < cands[j].cost })

		matched := make([]int, 0, m.cfg.PlayersPerMatch)
		matched = append(matched, anchorIdx)
		for _, c := range cands[:m.cfg.PlayersPerMatch-1] {
			matched = append(matched, c.idx)
		}

		// 记录质量指标
		m.recordMatchLocked(matched, now)

		// 取出玩家并倒序删除
		result := make([]PlayerInfo, len(matched))
		for k, idx := range matched {
			result[k] = m.queue[idx].Player
		}
		sort.Sort(sort.Reverse(sort.IntSlice(matched)))
		for _, idx := range matched {
			m.queue = append(m.queue[:idx], m.queue[idx+1:]...)
		}
		return result
	}
	return nil
}

// recordMatchLocked 更新匹配质量指标（调用者持锁）
func (m *AdvancedMatcher) recordMatchLocked(matched []int, now time.Time) {
	m.totalMatches++
	minR, maxR := math.Inf(1), math.Inf(-1)
	for _, idx := range matched {
		e := m.queue[idx]
		m.totalWaitSum += now.Sub(e.JoinedAt)
		r := EffectiveRating(e.Player, m.cfg.MMRStore)
		if r < minR {
			minR = r
		}
		if r > maxR {
			maxR = r
		}
	}
	m.totalRatingSpread += maxR - minR
}

// QualityMetrics 匹配质量指标快照
type QualityMetrics struct {
	TotalMatches     int
	AvgWait          time.Duration
	AvgRatingSpread  float64 // 队伍内最大-最小 Rating 差的平均值（越小越公平）
	CurrentQueueSize int
}

// Metrics 获取质量指标
func (m *AdvancedMatcher) Metrics() QualityMetrics {
	m.mu.Lock()
	defer m.mu.Unlock()
	q := QualityMetrics{
		TotalMatches:     m.totalMatches,
		CurrentQueueSize: len(m.queue),
	}
	if m.totalMatches > 0 {
		q.AvgWait = m.totalWaitSum / time.Duration(m.totalMatches*m.cfg.PlayersPerMatch)
		q.AvgRatingSpread = m.totalRatingSpread / float64(m.totalMatches)
	}
	return q
}

// ResetMetrics 清空指标计数（用于测试 / 周期汇报）
func (m *AdvancedMatcher) ResetMetrics() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.totalMatches = 0
	m.totalWaitSum = 0
	m.totalRatingSpread = 0
}
