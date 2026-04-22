package quest

import (
	"fmt"
	"sync"
	"time"
)

// GameEvent 游戏事件（用于驱动任务进度）
type GameEvent struct {
	Type     string // 事件类型（如 "kill_monster","collect_item","reach_level"）
	TargetID string // 目标对象 ID
	Count    int    // 数量
	PlayerID string // 触发玩家 ID
}

// QuestTracker 任务追踪器
// 管理玩家的任务列表，监听游戏事件驱动进度
type QuestTracker struct {
	mu       sync.RWMutex
	playerID string
	registry *QuestRegistry
	active   map[string]*QuestInstance // questID → 实例
	history  map[string]QuestStatus    // questID → 终态（已完成/已失败）
	onChange func(playerID string, quest *QuestInstance) // 进度变更回调

	// --- v1.11 扩展：玩家属性供复合前置条件评估 ---
	playerLevel int
	reputation  map[string]int
	flags       map[string]bool
}

// NewQuestTracker 创建任务追踪器
func NewQuestTracker(playerID string, registry *QuestRegistry) *QuestTracker {
	return &QuestTracker{
		playerID:   playerID,
		registry:   registry,
		active:     make(map[string]*QuestInstance),
		history:    make(map[string]QuestStatus),
		reputation: make(map[string]int),
		flags:      make(map[string]bool),
	}
}

// SetPlayerLevel 记录玩家等级（影响复合前置条件）
func (t *QuestTracker) SetPlayerLevel(level int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.playerLevel = level
}

// SetReputation 设置声望字段
func (t *QuestTracker) SetReputation(key string, value int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.reputation[key] = value
}

// SetFlag 设置/清除自定义标志
func (t *QuestTracker) SetFlag(flag string, value bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.flags[flag] = value
}

// buildPrereqContextLocked 构造复合前置条件评估上下文（调用者持锁）
func (t *QuestTracker) buildPrereqContextLocked() *PrereqContext {
	return &PrereqContext{
		QuestDone: func(id string) bool {
			return t.history[id] == QuestRewarded
		},
		Level:      t.playerLevel,
		Reputation: t.reputation,
		Flags:      t.flags,
	}
}

// ChooseBranch 在活跃的分支任务上选择分支
func (t *QuestTracker) ChooseBranch(questID, branchID string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	q, ok := t.active[questID]
	if !ok {
		return fmt.Errorf("quest %s not active", questID)
	}
	if q.Def.Branches == nil {
		return fmt.Errorf("quest %s has no branches", questID)
	}
	if !q.ChooseBranch(branchID) {
		return fmt.Errorf("invalid branch %s or already chosen", branchID)
	}
	if t.onChange != nil {
		t.onChange(t.playerID, q)
	}
	return nil
}

// SetOnChange 设置进度变更回调
func (t *QuestTracker) SetOnChange(fn func(playerID string, quest *QuestInstance)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.onChange = fn
}

// Accept 接取任务
func (t *QuestTracker) Accept(questID string, now time.Time) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	// 检查是否已接取
	if _, ok := t.active[questID]; ok {
		return fmt.Errorf("quest %s already active", questID)
	}

	// 检查是否已完成
	if status, ok := t.history[questID]; ok && status == QuestRewarded {
		return fmt.Errorf("quest %s already completed", questID)
	}

	// 获取任务定义
	def, ok := t.registry.Get(questID)
	if !ok {
		return fmt.Errorf("quest %s not found", questID)
	}

	// 检查前置任务
	for _, prereq := range def.PrereqIDs {
		status, ok := t.history[prereq]
		if !ok || status != QuestRewarded {
			return fmt.Errorf("prerequisite quest %s not completed", prereq)
		}
	}

	// 复合前置条件（v1.11 扩展）
	if def.Prereq != nil {
		ctx := t.buildPrereqContextLocked()
		if !def.Prereq.Evaluate(ctx) {
			return fmt.Errorf("composite prerequisite not satisfied")
		}
	}

	// 玩家等级（如果 Def.Level > 0）
	if def.Level > 0 && t.playerLevel > 0 && t.playerLevel < def.Level {
		return fmt.Errorf("level %d below requirement %d", t.playerLevel, def.Level)
	}

	inst := NewQuestInstance(def, t.playerID, now)
	t.active[questID] = inst
	return nil
}

// HandleEvent 处理游戏事件，更新所有相关任务进度
func (t *QuestTracker) HandleEvent(event GameEvent) []QuestUpdate {
	t.mu.Lock()
	defer t.mu.Unlock()

	var updates []QuestUpdate
	for _, quest := range t.active {
		if quest.UpdateProgress(event.Type, event.TargetID, event.Count) {
			updates = append(updates, QuestUpdate{
				QuestID:  quest.Def.ID,
				PlayerID: t.playerID,
				Status:   quest.Status,
				Progress: quest.Progress(),
			})

			if t.onChange != nil {
				t.onChange(t.playerID, quest)
			}
		}
	}
	return updates
}

// ClaimRewards 领取任务奖励
func (t *QuestTracker) ClaimRewards(questID string) ([]RewardDef, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	quest, ok := t.active[questID]
	if !ok {
		return nil, fmt.Errorf("quest %s not active", questID)
	}

	rewards, err := quest.ClaimRewards()
	if err != nil {
		return nil, err
	}
	if rewards == nil {
		return nil, fmt.Errorf("quest %s not completed", questID)
	}

	// 移入历史记录
	t.history[questID] = QuestRewarded
	delete(t.active, questID)

	return rewards, nil
}

// Abandon 放弃任务
func (t *QuestTracker) Abandon(questID string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	quest, ok := t.active[questID]
	if !ok {
		return fmt.Errorf("quest %s not active", questID)
	}

	quest.Fail()
	t.history[questID] = QuestFailed
	delete(t.active, questID)
	return nil
}

// CheckExpired 检查并处理超时任务
func (t *QuestTracker) CheckExpired(now time.Time) []string {
	t.mu.Lock()
	defer t.mu.Unlock()

	var expired []string
	for id, quest := range t.active {
		if quest.IsExpired(now) {
			quest.Fail()
			t.history[id] = QuestFailed
			delete(t.active, id)
			expired = append(expired, id)
		}
	}
	return expired
}

// GetActive 获取所有活跃任务
func (t *QuestTracker) GetActive() []*QuestInstance {
	t.mu.RLock()
	defer t.mu.RUnlock()

	result := make([]*QuestInstance, 0, len(t.active))
	for _, q := range t.active {
		result = append(result, q)
	}
	return result
}

// GetQuest 获取指定任务
func (t *QuestTracker) GetQuest(questID string) (*QuestInstance, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	q, ok := t.active[questID]
	return q, ok
}

// IsCompleted 检查任务是否已完成并领奖
func (t *QuestTracker) IsCompleted(questID string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.history[questID] == QuestRewarded
}

// ActiveCount 活跃任务数量
func (t *QuestTracker) ActiveCount() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.active)
}

// QuestUpdate 任务进度更新通知
type QuestUpdate struct {
	QuestID  string
	PlayerID string
	Status   QuestStatus
	Progress int // 百分比 0-100
}
