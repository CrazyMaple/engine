package quest

import "time"

// QuestStatus 任务状态
type QuestStatus int

const (
	QuestPending    QuestStatus = iota // 未接取
	QuestActive                        // 进行中
	QuestCompleted                     // 已完成（待领奖）
	QuestRewarded                      // 已领奖
	QuestFailed                        // 已失败
)

// QuestType 任务类型
type QuestType int

const (
	QuestTypeMain   QuestType = iota // 主线任务
	QuestTypeSide                    // 支线任务
	QuestTypeDaily                   // 每日任务
	QuestTypeWeekly                  // 每周任务
)

// QuestDef 任务定义（配表数据）
type QuestDef struct {
	ID          string      // 任务 ID
	Name        string      // 任务名称
	Description string      // 任务描述
	Type        QuestType   // 任务类型
	Steps       []StepDef   // 任务步骤
	Rewards     []RewardDef // 奖励列表
	PrereqIDs   []string    // 前置任务 ID
	Level       int         // 解锁等级要求
	TimeLimit   time.Duration // 限时（0=无限制）
	AutoAccept  bool        // 是否自动接取

	// --- v1.11 扩展（可选）---

	// Prereq 复合前置条件（AND/OR + 声望 + 等级 + 标志），优先级高于 PrereqIDs+Level
	Prereq *Prerequisite
	// Branches 分支任务定义（nil = 线性任务）
	Branches *BranchDef
	// Shared 如果为 true，该任务支持队伍共享进度
	Shared bool
}

// StepDef 任务步骤定义
type StepDef struct {
	ID          string // 步骤 ID
	Description string // 步骤描述
	EventType   string // 监听事件类型
	TargetID    string // 目标对象 ID（可选，如特定怪物 ID）
	Required    int    // 需要完成的数量
}

// RewardDef 奖励定义
type RewardDef struct {
	Type   string // 奖励类型（"item","exp","gold","mail"）
	ItemID string // 道具 ID（Type=item 时）
	Count  int    // 数量
}

// QuestInstance 任务运行时实例
type QuestInstance struct {
	Def        *QuestDef
	Status     QuestStatus
	Steps      []StepProgress  // 各步骤进度
	AcceptTime time.Time       // 接取时间
	PlayerID   string
	// Branch 分支任务运行时（Def.Branches != nil 时初始化）
	Branch *BranchState
	// TeamID 若该任务来自共享池，记录队伍 ID
	TeamID string
}

// StepProgress 步骤进度
type StepProgress struct {
	StepID   string
	Current  int  // 当前进度
	Required int  // 需要完成的数量
	Done     bool // 是否已完成
}

// NewQuestInstance 创建任务实例
func NewQuestInstance(def *QuestDef, playerID string, now time.Time) *QuestInstance {
	steps := make([]StepProgress, len(def.Steps))
	for i, s := range def.Steps {
		steps[i] = StepProgress{
			StepID:   s.ID,
			Required: s.Required,
		}
	}
	inst := &QuestInstance{
		Def:        def,
		Status:     QuestActive,
		Steps:      steps,
		AcceptTime: now,
		PlayerID:   playerID,
	}
	if def.Branches != nil {
		inst.Branch = &BranchState{}
	}
	return inst
}

// IsExpired 检查任务是否超时
func (q *QuestInstance) IsExpired(now time.Time) bool {
	if q.Def.TimeLimit <= 0 {
		return false
	}
	return now.Sub(q.AcceptTime) > q.Def.TimeLimit
}

// UpdateProgress 更新步骤进度
// 返回 true 表示有进度变化
func (q *QuestInstance) UpdateProgress(eventType string, targetID string, count int) bool {
	if q.Status != QuestActive {
		return false
	}

	changed := false
	for i, step := range q.Steps {
		if step.Done {
			continue
		}
		stepDef := q.Def.Steps[i]
		if stepDef.EventType != eventType {
			continue
		}
		if stepDef.TargetID != "" && stepDef.TargetID != targetID {
			continue
		}
		q.Steps[i].Current += count
		if q.Steps[i].Current >= q.Steps[i].Required {
			q.Steps[i].Current = q.Steps[i].Required
			q.Steps[i].Done = true
		}
		changed = true
	}

	// 检查是否所有步骤完成
	if changed && q.allStepsDone() {
		q.Status = QuestCompleted
	}

	return changed
}

// ClaimRewards 领取奖励，返回奖励列表
func (q *QuestInstance) ClaimRewards() ([]RewardDef, error) {
	if q.Status != QuestCompleted {
		return nil, nil
	}
	q.Status = QuestRewarded
	return q.Def.Rewards, nil
}

// Fail 标记任务失败
func (q *QuestInstance) Fail() {
	if q.Status == QuestActive {
		q.Status = QuestFailed
	}
}

// Progress 返回总进度百分比（0-100）
func (q *QuestInstance) Progress() int {
	if len(q.Steps) == 0 {
		return 100
	}
	totalRequired := 0
	totalCurrent := 0
	for _, s := range q.Steps {
		totalRequired += s.Required
		totalCurrent += s.Current
	}
	if totalRequired == 0 {
		return 100
	}
	return totalCurrent * 100 / totalRequired
}

func (q *QuestInstance) allStepsDone() bool {
	for _, s := range q.Steps {
		if !s.Done {
			return false
		}
	}
	return true
}

// QuestRegistry 任务定义注册表
type QuestRegistry struct {
	quests map[string]*QuestDef
}

// NewQuestRegistry 创建任务注册表
func NewQuestRegistry() *QuestRegistry {
	return &QuestRegistry{
		quests: make(map[string]*QuestDef),
	}
}

// Register 注册任务定义
func (r *QuestRegistry) Register(def *QuestDef) {
	r.quests[def.ID] = def
}

// Get 获取任务定义
func (r *QuestRegistry) Get(id string) (*QuestDef, bool) {
	def, ok := r.quests[id]
	return def, ok
}

// GetByType 按类型获取任务定义列表
func (r *QuestRegistry) GetByType(qt QuestType) []*QuestDef {
	var result []*QuestDef
	for _, def := range r.quests {
		if def.Type == qt {
			result = append(result, def)
		}
	}
	return result
}

// All 获取所有任务定义
func (r *QuestRegistry) All() []*QuestDef {
	result := make([]*QuestDef, 0, len(r.quests))
	for _, def := range r.quests {
		result = append(result, def)
	}
	return result
}
