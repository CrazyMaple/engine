package quest

// --- 复合前置条件（AND/OR + 声望/等级/已完成任务） ---

// PrereqOp 复合操作符
type PrereqOp int

const (
	PrereqAll  PrereqOp = iota // 所有子条件必须满足（AND）
	PrereqAny                  // 任一子条件满足即可（OR）
	PrereqNone                 // 所有子条件都不能满足（NOR）
)

// Prerequisite 复合前置条件（树形结构）
type Prerequisite struct {
	// Op 复合操作符，仅当 Children 非空时生效
	Op PrereqOp
	// Children 子条件（组合用）
	Children []*Prerequisite

	// --- 叶子节点条件（Children 为空时使用）---

	// QuestID 要求已完成的任务 ID
	QuestID string

	// MinLevel 要求最低等级（0 = 不检查）
	MinLevel int

	// ReputationKey 声望字段名（空 = 不检查）
	ReputationKey string
	// ReputationMin 声望最低值
	ReputationMin int

	// Flag 自定义业务标志（如 "has_guild", "vip5"）
	Flag string

	// Negate 叶子条件结果取反
	Negate bool
}

// PrereqContext 评估上下文
// 由 Tracker 构造，携带玩家属性供 Prerequisite 查询。
type PrereqContext struct {
	// QuestDone 判断任务是否已完成并领奖
	QuestDone func(questID string) bool
	// Level 玩家当前等级
	Level int
	// Reputation 玩家声望（key→value）
	Reputation map[string]int
	// Flags 自定义标志
	Flags map[string]bool
}

// Evaluate 评估前置条件
func (p *Prerequisite) Evaluate(ctx *PrereqContext) bool {
	if p == nil {
		return true
	}
	// 非叶子节点：按 Op 聚合
	if len(p.Children) > 0 {
		switch p.Op {
		case PrereqAll:
			for _, c := range p.Children {
				if !c.Evaluate(ctx) {
					return false
				}
			}
			return true
		case PrereqAny:
			for _, c := range p.Children {
				if c.Evaluate(ctx) {
					return true
				}
			}
			return false
		case PrereqNone:
			for _, c := range p.Children {
				if c.Evaluate(ctx) {
					return false
				}
			}
			return true
		}
		return false
	}
	// 叶子条件
	ok := p.evalLeaf(ctx)
	if p.Negate {
		return !ok
	}
	return ok
}

func (p *Prerequisite) evalLeaf(ctx *PrereqContext) bool {
	if p.QuestID != "" {
		if ctx.QuestDone == nil || !ctx.QuestDone(p.QuestID) {
			return false
		}
	}
	if p.MinLevel > 0 && ctx.Level < p.MinLevel {
		return false
	}
	if p.ReputationKey != "" {
		v, ok := ctx.Reputation[p.ReputationKey]
		if !ok || v < p.ReputationMin {
			return false
		}
	}
	if p.Flag != "" {
		if !ctx.Flags[p.Flag] {
			return false
		}
	}
	return true
}

// --- 便捷构造器 ---

// AllOf 所有子条件必须满足
func AllOf(children ...*Prerequisite) *Prerequisite {
	return &Prerequisite{Op: PrereqAll, Children: children}
}

// AnyOf 任一子条件满足即可
func AnyOf(children ...*Prerequisite) *Prerequisite {
	return &Prerequisite{Op: PrereqAny, Children: children}
}

// NoneOf 所有子条件都不能满足
func NoneOf(children ...*Prerequisite) *Prerequisite {
	return &Prerequisite{Op: PrereqNone, Children: children}
}

// RequireQuest 要求已完成某任务
func RequireQuest(questID string) *Prerequisite {
	return &Prerequisite{QuestID: questID}
}

// RequireLevel 要求最低等级
func RequireLevel(lvl int) *Prerequisite {
	return &Prerequisite{MinLevel: lvl}
}

// RequireReputation 要求声望达到
func RequireReputation(key string, min int) *Prerequisite {
	return &Prerequisite{ReputationKey: key, ReputationMin: min}
}

// RequireFlag 要求玩家带有某标志
func RequireFlag(flag string) *Prerequisite {
	return &Prerequisite{Flag: flag}
}
