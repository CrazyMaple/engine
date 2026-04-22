package quest

import (
	"fmt"
	"sync"
	"time"
)

// --- 队伍共享任务 ---
//
// 机制：一个队伍共享一个任务实例。任何成员触发事件都会累加到共享进度，
// 完成后所有成员可以各自领奖（每人一份奖励）。

// SharedQuest 队伍共享的任务实例
type SharedQuest struct {
	Instance  *QuestInstance
	Members   map[string]bool // playerID → 是否已领奖
	CreatedAt time.Time
}

// SharedQuestPool 共享任务池（一个队伍一个池）
type SharedQuestPool struct {
	mu       sync.RWMutex
	teamID   string
	registry *QuestRegistry
	active   map[string]*SharedQuest // questID → 共享实例
	onChange func(teamID string, q *SharedQuest)
}

// NewSharedQuestPool 创建共享任务池
func NewSharedQuestPool(teamID string, reg *QuestRegistry) *SharedQuestPool {
	return &SharedQuestPool{
		teamID:   teamID,
		registry: reg,
		active:   make(map[string]*SharedQuest),
	}
}

// SetOnChange 设置进度变更回调
func (p *SharedQuestPool) SetOnChange(fn func(teamID string, q *SharedQuest)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.onChange = fn
}

// AddMember 注册一名成员（加入共享后可领奖）
func (p *SharedQuestPool) AddMember(playerID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, sq := range p.active {
		if _, ok := sq.Members[playerID]; !ok {
			sq.Members[playerID] = false
		}
	}
}

// RemoveMember 移除一名成员（离队场景）
func (p *SharedQuestPool) RemoveMember(playerID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, sq := range p.active {
		delete(sq.Members, playerID)
	}
}

// Accept 接取一个共享任务（必须 def.Shared==true）
func (p *SharedQuestPool) Accept(questID string, members []string, now time.Time) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if _, ok := p.active[questID]; ok {
		return fmt.Errorf("shared quest %s already active", questID)
	}
	def, ok := p.registry.Get(questID)
	if !ok {
		return fmt.Errorf("quest %s not found", questID)
	}
	if !def.Shared {
		return fmt.Errorf("quest %s is not a shared quest", questID)
	}
	inst := NewQuestInstance(def, "team:"+p.teamID, now)
	inst.TeamID = p.teamID

	sq := &SharedQuest{
		Instance:  inst,
		Members:   make(map[string]bool, len(members)),
		CreatedAt: now,
	}
	for _, id := range members {
		sq.Members[id] = false
	}
	p.active[questID] = sq
	return nil
}

// HandleEvent 成员事件进入共享任务池，驱动所有相关共享任务进度
func (p *SharedQuestPool) HandleEvent(event GameEvent) []QuestUpdate {
	p.mu.Lock()
	defer p.mu.Unlock()

	var updates []QuestUpdate
	for _, sq := range p.active {
		if _, ok := sq.Members[event.PlayerID]; !ok {
			continue // 非成员事件忽略
		}
		if sq.Instance.UpdateProgress(event.Type, event.TargetID, event.Count) {
			updates = append(updates, QuestUpdate{
				QuestID:  sq.Instance.Def.ID,
				PlayerID: event.PlayerID,
				Status:   sq.Instance.Status,
				Progress: sq.Instance.Progress(),
			})
			if p.onChange != nil {
				p.onChange(p.teamID, sq)
			}
		}
	}
	return updates
}

// ClaimRewards 成员领取共享任务奖励；每名成员仅可领取一次
func (p *SharedQuestPool) ClaimRewards(questID, playerID string) ([]RewardDef, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	sq, ok := p.active[questID]
	if !ok {
		return nil, fmt.Errorf("shared quest %s not active", questID)
	}
	if sq.Instance.Status != QuestCompleted && sq.Instance.Status != QuestRewarded {
		return nil, fmt.Errorf("shared quest %s not completed", questID)
	}
	claimed, ok := sq.Members[playerID]
	if !ok {
		return nil, fmt.Errorf("player %s not member", playerID)
	}
	if claimed {
		return nil, fmt.Errorf("player %s already claimed", playerID)
	}
	sq.Members[playerID] = true
	sq.Instance.Status = QuestRewarded // 不再阻止其他成员领奖
	// 复制一份奖励避免外部修改影响定义
	out := make([]RewardDef, len(sq.Instance.Def.Rewards))
	copy(out, sq.Instance.Def.Rewards)
	return out, nil
}

// GetShared 获取指定任务的共享实例
func (p *SharedQuestPool) GetShared(questID string) (*SharedQuest, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	sq, ok := p.active[questID]
	return sq, ok
}

// AllClaimed 所有成员都已领奖
func (sq *SharedQuest) AllClaimed() bool {
	for _, claimed := range sq.Members {
		if !claimed {
			return false
		}
	}
	return true
}

// Cleanup 清理已全部领奖的任务（由业务层按需调用）
func (p *SharedQuestPool) Cleanup() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	var removed int
	for id, sq := range p.active {
		if sq.AllClaimed() {
			delete(p.active, id)
			removed++
		}
	}
	return removed
}
