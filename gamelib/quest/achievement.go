package quest

import (
	"sync"
	"time"
)

// AchievementDef 成就定义
type AchievementDef struct {
	ID          string // 成就 ID
	Name        string // 成就名称
	Description string // 成就描述
	Category    string // 分类（如 "combat","exploration","social"）
	EventType   string // 监听事件类型
	TargetID    string // 目标对象 ID（可选）
	Required    int    // 达成条件数量
	Points      int    // 成就积分
	Rewards     []RewardDef // 奖励（可选）
	Hidden      bool   // 是否隐藏成就（达成前不可见）
}

// AchievementInstance 成就运行时实例
type AchievementInstance struct {
	Def       *AchievementDef
	Current   int       // 当前进度
	Achieved  bool      // 是否已达成
	Claimed   bool      // 是否已领奖
	AchievedAt time.Time // 达成时间
}

// Progress 返回进度百分比
func (a *AchievementInstance) Progress() int {
	if a.Def.Required <= 0 {
		return 100
	}
	p := a.Current * 100 / a.Def.Required
	if p > 100 {
		return 100
	}
	return p
}

// AchievementTracker 成就追踪器
type AchievementTracker struct {
	mu           sync.RWMutex
	playerID     string
	achievements map[string]*AchievementInstance // achievementID → 实例
	totalPoints  int
	onAchieved   func(playerID string, achievement *AchievementInstance) // 达成回调
}

// NewAchievementTracker 创建成就追踪器
func NewAchievementTracker(playerID string, defs []*AchievementDef) *AchievementTracker {
	tracker := &AchievementTracker{
		playerID:     playerID,
		achievements: make(map[string]*AchievementInstance, len(defs)),
	}
	for _, def := range defs {
		tracker.achievements[def.ID] = &AchievementInstance{
			Def: def,
		}
	}
	return tracker
}

// SetOnAchieved 设置达成回调
func (t *AchievementTracker) SetOnAchieved(fn func(playerID string, achievement *AchievementInstance)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.onAchieved = fn
}

// HandleEvent 处理游戏事件，更新成就进度
func (t *AchievementTracker) HandleEvent(event GameEvent) []AchievementUpdate {
	t.mu.Lock()
	defer t.mu.Unlock()

	var updates []AchievementUpdate
	for _, ach := range t.achievements {
		if ach.Achieved {
			continue
		}
		def := ach.Def
		if def.EventType != event.Type {
			continue
		}
		if def.TargetID != "" && def.TargetID != event.TargetID {
			continue
		}

		ach.Current += event.Count
		if ach.Current >= def.Required {
			ach.Current = def.Required
			ach.Achieved = true
			ach.AchievedAt = time.Now()
			t.totalPoints += def.Points

			if t.onAchieved != nil {
				t.onAchieved(t.playerID, ach)
			}
		}

		updates = append(updates, AchievementUpdate{
			AchievementID: def.ID,
			PlayerID:      t.playerID,
			Current:       ach.Current,
			Required:      def.Required,
			Achieved:      ach.Achieved,
		})
	}
	return updates
}

// ClaimRewards 领取成就奖励
func (t *AchievementTracker) ClaimRewards(achievementID string) ([]RewardDef, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	ach, ok := t.achievements[achievementID]
	if !ok || !ach.Achieved || ach.Claimed {
		return nil, false
	}

	ach.Claimed = true
	return ach.Def.Rewards, true
}

// GetAll 获取所有成就（含进度）
func (t *AchievementTracker) GetAll() []*AchievementInstance {
	t.mu.RLock()
	defer t.mu.RUnlock()

	result := make([]*AchievementInstance, 0, len(t.achievements))
	for _, ach := range t.achievements {
		result = append(result, ach)
	}
	return result
}

// GetAchieved 获取已达成的成就
func (t *AchievementTracker) GetAchieved() []*AchievementInstance {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var result []*AchievementInstance
	for _, ach := range t.achievements {
		if ach.Achieved {
			result = append(result, ach)
		}
	}
	return result
}

// GetByCategory 按分类获取成就
func (t *AchievementTracker) GetByCategory(category string) []*AchievementInstance {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var result []*AchievementInstance
	for _, ach := range t.achievements {
		if ach.Def.Category == category {
			result = append(result, ach)
		}
	}
	return result
}

// TotalPoints 获取已达成的总积分
func (t *AchievementTracker) TotalPoints() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.totalPoints
}

// AchievedCount 已达成成就数量
func (t *AchievementTracker) AchievedCount() int {
	t.mu.RLock()
	defer t.mu.RUnlock()

	count := 0
	for _, ach := range t.achievements {
		if ach.Achieved {
			count++
		}
	}
	return count
}

// TotalCount 成就总数
func (t *AchievementTracker) TotalCount() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.achievements)
}

// AchievementUpdate 成就进度更新通知
type AchievementUpdate struct {
	AchievementID string
	PlayerID      string
	Current       int
	Required      int
	Achieved      bool
}
