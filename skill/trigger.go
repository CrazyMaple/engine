package skill

// 条件触发器：技能阶段 Hook 执行时按条件决定是否继续派生连锁。
// 通过 Condition 类型描述声明式判断，TriggerEngine 负责求值。

// ConditionOp 条件比较操作符
type ConditionOp int

const (
	OpEq    ConditionOp = iota // 等于
	OpNe                       // 不等于
	OpGt                       // 大于
	OpGe                       // 大于等于
	OpLt                       // 小于
	OpLe                       // 小于等于
)

// ConditionType 条件类别
type ConditionType int

const (
	CondAlways         ConditionType = iota // 必定成立（无条件触发）
	CondHit                                 // 本次是否命中（结果 Applied）
	CondCrit                                // 是否暴击（由扩展字段传入）
	CondHPBelow                             // 目标血量百分比 < Value
	CondHPAbove                             // 目标血量百分比 > Value
	CondHasBuff                             // 目标是否拥有指定 Buff（BuffID）
	CondLacksBuff                           // 目标是否缺少指定 Buff（BuffID）
	CondCasterHasBuff                       // 施法者是否拥有指定 Buff
	CondHitCount                            // 本次效果命中目标数 op Value
	CondTag                                 // 本次技能是否带有指定标签（BuffID 字段复用为 tag）
)

// Condition 触发条件
type Condition struct {
	Type    ConditionType
	Op      ConditionOp
	Value   float64 // 比较数值
	BuffID  string  // Buff ID / Tag 字符串（按 Type 解释）
	Negate  bool    // 是否取反
}

// TriggerContext 触发判定上下文
type TriggerContext struct {
	CasterID    string
	TargetID    string
	SkillDef    *SkillDef
	Results     []EffectResult     // 本次执行的效果结果
	Crit        bool               // 是否暴击（由上层计算后注入）
	CasterHPPct float64            // 施法者当前血量百分比 (0-1)
	TargetHPPct float64            // 目标当前血量百分比 (0-1)
	CasterBuffs map[string]bool    // 施法者 BuffID 集合
	TargetBuffs map[string]bool    // 目标 BuffID 集合
}

// HitCount 返回 Applied==true 的效果结果数
func (c *TriggerContext) HitCount() int {
	n := 0
	for _, r := range c.Results {
		if r.Applied {
			n++
		}
	}
	return n
}

// AnyApplied 是否至少有一次命中
func (c *TriggerContext) AnyApplied() bool {
	for _, r := range c.Results {
		if r.Applied {
			return true
		}
	}
	return false
}

// Evaluate 判断条件是否成立
func (cd *Condition) Evaluate(ctx *TriggerContext) bool {
	raw := cd.rawEvaluate(ctx)
	if cd.Negate {
		return !raw
	}
	return raw
}

func (cd *Condition) rawEvaluate(ctx *TriggerContext) bool {
	switch cd.Type {
	case CondAlways:
		return true
	case CondHit:
		return ctx.AnyApplied()
	case CondCrit:
		return ctx.Crit
	case CondHPBelow:
		return compareFloat(ctx.TargetHPPct, cd.Value, cd.Op)
	case CondHPAbove:
		return compareFloat(ctx.TargetHPPct, cd.Value, cd.Op)
	case CondHasBuff:
		return ctx.TargetBuffs != nil && ctx.TargetBuffs[cd.BuffID]
	case CondLacksBuff:
		return ctx.TargetBuffs == nil || !ctx.TargetBuffs[cd.BuffID]
	case CondCasterHasBuff:
		return ctx.CasterBuffs != nil && ctx.CasterBuffs[cd.BuffID]
	case CondHitCount:
		return compareFloat(float64(ctx.HitCount()), cd.Value, cd.Op)
	case CondTag:
		if ctx.SkillDef == nil {
			return false
		}
		for _, t := range ctx.SkillDef.Tags {
			if t == cd.BuffID {
				return true
			}
		}
		return false
	}
	return false
}

func compareFloat(lhs, rhs float64, op ConditionOp) bool {
	switch op {
	case OpEq:
		return lhs == rhs
	case OpNe:
		return lhs != rhs
	case OpGt:
		return lhs > rhs
	case OpGe:
		return lhs >= rhs
	case OpLt:
		return lhs < rhs
	case OpLe:
		return lhs <= rhs
	}
	return false
}

// Trigger 触发器：条件满足后产出一次派生动作
type Trigger struct {
	Name       string      // 触发器名称（日志用）
	Conditions []Condition // 全部条件（AND 关系），空=必定触发
	// ChainSkillID 触发后需要连锁的技能 ID（可选）
	ChainSkillID string
	// ChainDelay 链式技能的延迟（毫秒，0=立即）
	ChainDelayMS int
	// ApplyBuff 触发后为目标施加的 Buff（可选，BuffID 走 EffectPipeline Buff 注册表）
	ApplyBuff string
	// ExtraEffects 触发后追加执行的效果 ID 列表
	ExtraEffects []string
	// Once 是否只触发一次
	Once bool
	// fired 内部状态
	fired bool
}

// Match 检查所有条件是否均成立
func (t *Trigger) Match(ctx *TriggerContext) bool {
	if t.Once && t.fired {
		return false
	}
	for i := range t.Conditions {
		if !t.Conditions[i].Evaluate(ctx) {
			return false
		}
	}
	return true
}

// Fire 标记已触发（Once 用）
func (t *Trigger) Fire() { t.fired = true }

// Reset 重置触发状态（新的一次技能释放前调用）
func (t *Trigger) Reset() { t.fired = false }
