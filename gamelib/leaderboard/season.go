package leaderboard

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

// SeasonState 赛季状态机
type SeasonState int

const (
	SeasonUpcoming   SeasonState = iota // 未开始
	SeasonActive                        // 进行中
	SeasonEndingSoon                    // 结束倒计时（默认 T-7d 提示期）
	SeasonSettling                      // 结算中（冻结排行榜 + 发放奖励）
	SeasonArchived                      // 已归档
)

// String 状态字符串（用于 API 输出）
func (s SeasonState) String() string {
	switch s {
	case SeasonUpcoming:
		return "upcoming"
	case SeasonActive:
		return "active"
	case SeasonEndingSoon:
		return "ending_soon"
	case SeasonSettling:
		return "settling"
	case SeasonArchived:
		return "archived"
	}
	return "unknown"
}

// SeasonRewardRule 赛季奖励规则（按排名段发放）
type SeasonRewardRule struct {
	// MinRank/MaxRank 覆盖的排名区间（闭区间，1-based）
	MinRank, MaxRank int
	// RewardName 奖励描述（由业务层解释：邮件 ItemID、标题等）
	RewardName string
	// RewardCount 奖励数量
	RewardCount int
	// ExtraData 额外数据（如装扮 ID、称号 ID）
	ExtraData map[string]string
}

// SeasonConfig 赛季配置
type SeasonConfig struct {
	// Board 绑定的排行榜名
	Board string
	// SeasonID 赛季 ID（如 "s3", "2026Q1"）
	SeasonID string
	// StartAt / EndAt 赛季时间
	StartAt time.Time
	EndAt   time.Time
	// EndingSoonLead 提前进入 EndingSoon 的时长（默认 7 天）
	EndingSoonLead time.Duration
	// CarryRatio 上赛季分数继承比例（0 = 重置，1 = 完整保留），默认 0
	CarryRatio float64
	// Rewards 奖励规则
	Rewards []SeasonRewardRule
}

// Validate 简单校验
func (c *SeasonConfig) Validate() error {
	if c.Board == "" {
		return fmt.Errorf("season board required")
	}
	if c.SeasonID == "" {
		return fmt.Errorf("season id required")
	}
	if !c.EndAt.After(c.StartAt) {
		return fmt.Errorf("end must be after start")
	}
	return nil
}

// SeasonSnapshot 赛季结算快照（归档用途）
type SeasonSnapshot struct {
	SeasonID   string        `json:"season_id"`
	Board      string        `json:"board"`
	StartAt    int64         `json:"start_at"`
	EndAt      int64         `json:"end_at"`
	SettledAt  int64         `json:"settled_at"`
	Entries    []RankedEntry `json:"entries"`
	RewardLogs []SeasonReward `json:"reward_logs"`
}

// SeasonReward 发放记录
type SeasonReward struct {
	PlayerID    string            `json:"player_id"`
	Rank        int               `json:"rank"`
	RewardName  string            `json:"reward_name"`
	RewardCount int               `json:"reward_count"`
	ExtraData   map[string]string `json:"extra_data,omitempty"`
}

// SeasonRewardSink 奖励派发回调（通常接入 mail/inventory）
type SeasonRewardSink func(season string, reward SeasonReward) error

// SeasonArchiveSink 归档落地回调（通常写入对象存储 / 本地文件）
type SeasonArchiveSink func(snap *SeasonSnapshot) error

// SeasonManager 多赛季管理器
type SeasonManager struct {
	mu sync.RWMutex

	la        *LeaderboardActor
	active    map[string]*SeasonState // board → state 指针（允许原子观测）
	configs   map[string]*SeasonConfig
	history   map[string][]*SeasonSnapshot // board → archived snapshots (最新在末尾)
	rewardFn  SeasonRewardSink
	archiveFn SeasonArchiveSink
}

// NewSeasonManager 创建赛季管理器
func NewSeasonManager(la *LeaderboardActor) *SeasonManager {
	return &SeasonManager{
		la:      la,
		active:  make(map[string]*SeasonState),
		configs: make(map[string]*SeasonConfig),
		history: make(map[string][]*SeasonSnapshot),
	}
}

// SetRewardSink 设置奖励发放回调
func (sm *SeasonManager) SetRewardSink(fn SeasonRewardSink) {
	sm.mu.Lock()
	sm.rewardFn = fn
	sm.mu.Unlock()
}

// SetArchiveSink 设置归档回调
func (sm *SeasonManager) SetArchiveSink(fn SeasonArchiveSink) {
	sm.mu.Lock()
	sm.archiveFn = fn
	sm.mu.Unlock()
}

// RegisterSeason 注册一个新赛季
// 同 Board 同 SeasonID 重复注册返回错误。
func (sm *SeasonManager) RegisterSeason(cfg SeasonConfig) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	if cfg.EndingSoonLead == 0 {
		cfg.EndingSoonLead = 7 * 24 * time.Hour
	}
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// 一个 Board 同时仅允许一个活跃赛季
	if old, ok := sm.configs[cfg.Board]; ok && old.SeasonID == cfg.SeasonID {
		return fmt.Errorf("season %s/%s already registered", cfg.Board, cfg.SeasonID)
	}
	cp := cfg
	sm.configs[cfg.Board] = &cp
	state := SeasonUpcoming
	sm.active[cfg.Board] = &state
	return nil
}

// Current 获取某 Board 当前赛季配置
func (sm *SeasonManager) Current(board string) (*SeasonConfig, SeasonState, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	cfg, ok := sm.configs[board]
	if !ok {
		return nil, SeasonUpcoming, false
	}
	state := sm.active[board]
	return cfg, *state, true
}

// Tick 推进状态机：根据 now 更新赛季状态、在到期时自动结算
// 返回结算过的赛季 ID 列表（用于外部日志 / 告警）。
func (sm *SeasonManager) Tick(now time.Time) []string {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	var settled []string
	for board, cfg := range sm.configs {
		st := sm.active[board]
		switch *st {
		case SeasonUpcoming:
			if !now.Before(cfg.StartAt) {
				*st = SeasonActive
			}
		case SeasonActive:
			if !now.Before(cfg.EndAt.Add(-cfg.EndingSoonLead)) {
				*st = SeasonEndingSoon
			}
			fallthrough
		case SeasonEndingSoon:
			if !now.Before(cfg.EndAt) {
				*st = SeasonSettling
				if snap, err := sm.settleLocked(board, cfg, now); err == nil {
					sm.history[board] = append(sm.history[board], snap)
					settled = append(settled, cfg.SeasonID)
					*st = SeasonArchived
				}
			}
		}
	}
	return settled
}

// SettleNow 立即结算赛季（手动触发：GM 运维或测试用）
func (sm *SeasonManager) SettleNow(board string, now time.Time) (*SeasonSnapshot, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	cfg, ok := sm.configs[board]
	if !ok {
		return nil, fmt.Errorf("board %s no season registered", board)
	}
	snap, err := sm.settleLocked(board, cfg, now)
	if err != nil {
		return nil, err
	}
	sm.history[board] = append(sm.history[board], snap)
	st := sm.active[board]
	*st = SeasonArchived
	return snap, nil
}

// settleLocked 执行结算：快照 + 归档 + 奖励派发 + 分数处理（继承/重置）
// 调用者需持有 sm.mu（Write 锁）
func (sm *SeasonManager) settleLocked(board string, cfg *SeasonConfig, now time.Time) (*SeasonSnapshot, error) {
	b := sm.la.GetOrCreateBoard(board)
	entries := b.GetTopN(b.Size())

	snap := &SeasonSnapshot{
		SeasonID:  cfg.SeasonID,
		Board:     board,
		StartAt:   cfg.StartAt.Unix(),
		EndAt:     cfg.EndAt.Unix(),
		SettledAt: now.Unix(),
		Entries:   entries,
	}

	// 奖励派发
	if sm.rewardFn != nil && len(cfg.Rewards) > 0 {
		for _, e := range entries {
			rule := findRewardRule(cfg.Rewards, e.Rank)
			if rule == nil {
				continue
			}
			r := SeasonReward{
				PlayerID:    e.Entry.PlayerID,
				Rank:        e.Rank,
				RewardName:  rule.RewardName,
				RewardCount: rule.RewardCount,
				ExtraData:   rule.ExtraData,
			}
			if err := sm.rewardFn(cfg.SeasonID, r); err == nil {
				snap.RewardLogs = append(snap.RewardLogs, r)
			}
		}
	}

	// 归档
	if sm.archiveFn != nil {
		_ = sm.archiveFn(snap)
	}

	// 处理继承比例：按比例保留分数，或清空
	if cfg.CarryRatio >= 1.0 {
		// 完整保留，无需处理
	} else if cfg.CarryRatio <= 0 {
		b.Reset()
	} else {
		// 部分保留
		for _, e := range entries {
			b.UpdateScore(e.Entry.PlayerID, e.Entry.Score*cfg.CarryRatio, e.Entry.Extra)
		}
	}

	return snap, nil
}

// findRewardRule 定位当前排名适用的奖励规则
func findRewardRule(rules []SeasonRewardRule, rank int) *SeasonRewardRule {
	for i := range rules {
		r := &rules[i]
		if rank >= r.MinRank && rank <= r.MaxRank {
			return r
		}
	}
	return nil
}

// History 获取某 Board 的历史赛季快照（按时间升序）
func (sm *SeasonManager) History(board string) []*SeasonSnapshot {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	out := make([]*SeasonSnapshot, len(sm.history[board]))
	copy(out, sm.history[board])
	return out
}

// GetSnapshot 按 (board, seasonID) 查询历史快照
func (sm *SeasonManager) GetSnapshot(board, seasonID string) (*SeasonSnapshot, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	for _, s := range sm.history[board] {
		if s.SeasonID == seasonID {
			return s, true
		}
	}
	return nil, false
}

// CrossSeasonQuery 跨赛季查询玩家累计排名
// scope: "current" / "historical" / "all-time"
// - current: 仅当前赛季排行榜
// - historical: 全部历史快照合并（按分数求和）
// - all-time: historical + current 合并
type CrossSeasonQuery struct {
	Board string
	Scope string
	N     int // TopN
}

// CrossSeasonEntry 跨赛季合并条目
type CrossSeasonEntry struct {
	PlayerID    string  `json:"player_id"`
	TotalScore  float64 `json:"total_score"`
	Appearances int     `json:"appearances"`
	BestRank    int     `json:"best_rank"`
}

// QueryCrossSeason 跨赛季合并查询
func (sm *SeasonManager) QueryCrossSeason(q CrossSeasonQuery) []CrossSeasonEntry {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	agg := make(map[string]*CrossSeasonEntry)
	addEntries := func(entries []RankedEntry) {
		for _, e := range entries {
			row, ok := agg[e.Entry.PlayerID]
			if !ok {
				row = &CrossSeasonEntry{
					PlayerID:    e.Entry.PlayerID,
					BestRank:    e.Rank,
					Appearances: 0,
				}
				agg[e.Entry.PlayerID] = row
			}
			row.TotalScore += e.Entry.Score
			row.Appearances++
			if e.Rank < row.BestRank {
				row.BestRank = e.Rank
			}
		}
	}

	if q.Scope == "historical" || q.Scope == "all-time" || q.Scope == "" {
		for _, snap := range sm.history[q.Board] {
			addEntries(snap.Entries)
		}
	}
	if q.Scope == "current" || q.Scope == "all-time" {
		if b, ok := sm.la.boards[q.Board]; ok {
			addEntries(b.GetTopN(b.Size()))
		}
	}

	result := make([]CrossSeasonEntry, 0, len(agg))
	for _, v := range agg {
		result = append(result, *v)
	}
	// 按总分降序
	sort.Slice(result, func(i, j int) bool { return result[i].TotalScore > result[j].TotalScore })
	if q.N > 0 && len(result) > q.N {
		result = result[:q.N]
	}
	return result
}
