package skill

import (
	"fmt"
	"math"
)

// EffectType 效果类型
type EffectType int

const (
	EffectDamage     EffectType = iota // 伤害
	EffectHeal                         // 治疗
	EffectApplyBuff                    // 施加 Buff
	EffectRemoveBuff                   // 移除 Buff
	EffectModifyAttr                   // 修改属性
)

// EffectDef 效果定义
type EffectDef struct {
	ID       string     // 效果 ID
	Type     EffectType // 效果类型
	Value    float32    // 基础数值（伤害/治疗量）
	Ratio    float32    // 攻击力系数（如 1.5 = 150% 攻击力）
	BuffID   string     // 关联 Buff ID（EffectApplyBuff/EffectRemoveBuff 使用）
	Chance   float32    // 触发概率（0-1，1=必定触发）
	AOE      bool       // 是否为 AOE 效果
	Radius   float32    // AOE 半径
}

// EffectContext 效果执行上下文
type EffectContext struct {
	CasterID    string   // 施法者 ID
	TargetID    string   // 目标 ID
	TargetIDs   []string // AOE 多目标 ID 列表
	CasterAtk   float32  // 施法者攻击力
	CasterPos   [2]float32 // 施法者位置 [x,y]
	TargetPos   [2]float32 // 目标位置 [x,y]
	SkillDef    *SkillDef  // 技能定义
	RandFunc    func() float32 // 随机函数（0-1），nil 时默认必定触发
}

// EffectResult 效果执行结果
type EffectResult struct {
	EffectID string
	Type     EffectType
	TargetID string
	Damage   float32
	Heal     float32
	BuffID   string // 施加/移除的 Buff ID
	Applied  bool   // 是否生效
	Reason   string // 未生效原因
}

// EffectPipeline 效果执行管线
// 按顺序执行技能的效果链
type EffectPipeline struct {
	effects  map[string]*EffectDef // effectID → 定义
	handlers map[EffectType]EffectHandler
}

// EffectHandler 效果处理器
type EffectHandler func(def *EffectDef, ctx *EffectContext) []EffectResult

// NewEffectPipeline 创建效果管线
func NewEffectPipeline() *EffectPipeline {
	p := &EffectPipeline{
		effects:  make(map[string]*EffectDef),
		handlers: make(map[EffectType]EffectHandler),
	}
	// 注册默认处理器
	p.handlers[EffectDamage] = defaultDamageHandler
	p.handlers[EffectHeal] = defaultHealHandler
	p.handlers[EffectApplyBuff] = defaultApplyBuffHandler
	p.handlers[EffectRemoveBuff] = defaultRemoveBuffHandler
	p.handlers[EffectModifyAttr] = defaultModifyAttrHandler
	return p
}

// RegisterEffect 注册效果定义
func (p *EffectPipeline) RegisterEffect(def *EffectDef) {
	p.effects[def.ID] = def
}

// RegisterHandler 注册/替换效果处理器
func (p *EffectPipeline) RegisterHandler(typ EffectType, handler EffectHandler) {
	p.handlers[typ] = handler
}

// GetEffect 获取效果定义
func (p *EffectPipeline) GetEffect(id string) (*EffectDef, bool) {
	def, ok := p.effects[id]
	return def, ok
}

// Execute 按顺序执行技能效果链
func (p *EffectPipeline) Execute(effectIDs []string, ctx *EffectContext) ([]EffectResult, error) {
	var allResults []EffectResult

	for _, eid := range effectIDs {
		def, ok := p.effects[eid]
		if !ok {
			return allResults, fmt.Errorf("effect %s not found", eid)
		}

		handler, ok := p.handlers[def.Type]
		if !ok {
			return allResults, fmt.Errorf("no handler for effect type %d", def.Type)
		}

		results := handler(def, ctx)
		allResults = append(allResults, results...)
	}

	return allResults, nil
}

// --- 默认效果处理器 ---

func defaultDamageHandler(def *EffectDef, ctx *EffectContext) []EffectResult {
	// 概率判定
	if !checkChance(def.Chance, ctx.RandFunc) {
		return []EffectResult{{
			EffectID: def.ID,
			Type:     EffectDamage,
			TargetID: ctx.TargetID,
			Applied:  false,
			Reason:   "chance failed",
		}}
	}

	damage := def.Value + ctx.CasterAtk*def.Ratio

	if def.AOE && len(ctx.TargetIDs) > 0 {
		results := make([]EffectResult, 0, len(ctx.TargetIDs))
		for _, tid := range ctx.TargetIDs {
			results = append(results, EffectResult{
				EffectID: def.ID,
				Type:     EffectDamage,
				TargetID: tid,
				Damage:   damage,
				Applied:  true,
			})
		}
		return results
	}

	return []EffectResult{{
		EffectID: def.ID,
		Type:     EffectDamage,
		TargetID: ctx.TargetID,
		Damage:   damage,
		Applied:  true,
	}}
}

func defaultHealHandler(def *EffectDef, ctx *EffectContext) []EffectResult {
	if !checkChance(def.Chance, ctx.RandFunc) {
		return []EffectResult{{
			EffectID: def.ID,
			Type:     EffectHeal,
			TargetID: ctx.TargetID,
			Applied:  false,
			Reason:   "chance failed",
		}}
	}

	heal := def.Value + ctx.CasterAtk*def.Ratio

	return []EffectResult{{
		EffectID: def.ID,
		Type:     EffectHeal,
		TargetID: ctx.TargetID,
		Heal:     heal,
		Applied:  true,
	}}
}

func defaultApplyBuffHandler(def *EffectDef, ctx *EffectContext) []EffectResult {
	if !checkChance(def.Chance, ctx.RandFunc) {
		return []EffectResult{{
			EffectID: def.ID,
			Type:     EffectApplyBuff,
			TargetID: ctx.TargetID,
			BuffID:   def.BuffID,
			Applied:  false,
			Reason:   "chance failed",
		}}
	}

	return []EffectResult{{
		EffectID: def.ID,
		Type:     EffectApplyBuff,
		TargetID: ctx.TargetID,
		BuffID:   def.BuffID,
		Applied:  true,
	}}
}

func defaultRemoveBuffHandler(def *EffectDef, ctx *EffectContext) []EffectResult {
	return []EffectResult{{
		EffectID: def.ID,
		Type:     EffectRemoveBuff,
		TargetID: ctx.TargetID,
		BuffID:   def.BuffID,
		Applied:  true,
	}}
}

func defaultModifyAttrHandler(def *EffectDef, ctx *EffectContext) []EffectResult {
	if !checkChance(def.Chance, ctx.RandFunc) {
		return []EffectResult{{
			EffectID: def.ID,
			Type:     EffectModifyAttr,
			TargetID: ctx.TargetID,
			Applied:  false,
			Reason:   "chance failed",
		}}
	}

	return []EffectResult{{
		EffectID: def.ID,
		Type:     EffectModifyAttr,
		TargetID: ctx.TargetID,
		Applied:  true,
	}}
}

func checkChance(chance float32, randFunc func() float32) bool {
	if chance >= 1.0 {
		return true
	}
	if chance <= 0 {
		return false
	}
	if randFunc == nil {
		return true // 无随机函数时默认触发
	}
	return randFunc() < chance
}

// --- AOE 辅助 ---

// FindTargetsInRadius 在给定位置列表中找出半径范围内的目标 ID
func FindTargetsInRadius(centerX, centerY, radius float32, targets []TargetPosition) []string {
	var result []string
	r2 := radius * radius
	for _, t := range targets {
		dx := t.X - centerX
		dy := t.Y - centerY
		if dx*dx+dy*dy <= r2 {
			result = append(result, t.ID)
		}
	}
	return result
}

// TargetPosition 目标位置信息
type TargetPosition struct {
	ID   string
	X, Y float32
}

// Distance 计算两点间距离
func Distance(x1, y1, x2, y2 float32) float32 {
	dx := x1 - x2
	dy := y1 - y2
	return float32(math.Sqrt(float64(dx*dx + dy*dy)))
}
