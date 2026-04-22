package skill

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gamelib/config"
)

// --- RecordFile（Tab 分隔）技能行定义 ---
// 表头顺序：ID Name Level Cooldown(ms) GlobalCD(ms) CastTime(ms) BackSwing(ms) TargetType Range AOERadius CostType CostValue Effects Tags Description
// Effects/Tags 采用 "|" 分隔，或 JSON 数组。
type skillRow struct {
	ID          string  `rf:"index"`
	Name        string
	Level       int
	CooldownMS  int
	GlobalCDMS  int
	CastTimeMS  int
	BackSwingMS int
	TargetType  int
	Range       float32
	AOERadius   float32
	CostType    int
	CostValue   int
	Effects     string
	Tags        string
	Description string
}

// --- RecordFile Buff 行定义 ---
// 表头：ID Name Category Duration(ms) MaxStack StackPolicy TickInterval(ms) Priority MutexGroup Tags Modifiers TickDamage TickHeal
// Modifiers 为 JSON：[{"Attribute":"attack","AddValue":10,"MulValue":0}]
type buffRow struct {
	ID           string `rf:"index"`
	Name         string
	Category     int
	DurationMS   int
	MaxStack     int
	StackPolicy  int
	TickInterval int
	Priority     int
	MutexGroup   string
	Tags         string
	Modifiers    string
	TickDamage   float32
	TickHeal     float32
}

// LoadSkillsFromRecordFile 从 Tab 分隔的 RecordFile 加载技能定义
func LoadSkillsFromRecordFile(path string, registry *SkillRegistry) (int, error) {
	rf, err := config.NewRecordFile(skillRow{})
	if err != nil {
		return 0, err
	}
	if err := rf.Read(path); err != nil {
		return 0, err
	}
	count := 0
	for i := 0; i < rf.NumRecord(); i++ {
		row := rf.Record(i).(*skillRow)
		def, err := rowToSkillDef(row)
		if err != nil {
			return count, fmt.Errorf("row %d (id=%s): %w", i, row.ID, err)
		}
		registry.Register(def)
		count++
	}
	return count, nil
}

// LoadBuffsFromRecordFile 从 Tab 分隔的 RecordFile 加载 Buff 定义
func LoadBuffsFromRecordFile(path string, pipeline *BuffRegistry) (int, error) {
	rf, err := config.NewRecordFile(buffRow{})
	if err != nil {
		return 0, err
	}
	if err := rf.Read(path); err != nil {
		return 0, err
	}
	count := 0
	for i := 0; i < rf.NumRecord(); i++ {
		row := rf.Record(i).(*buffRow)
		def, err := rowToBuffDef(row)
		if err != nil {
			return count, fmt.Errorf("row %d (id=%s): %w", i, row.ID, err)
		}
		pipeline.Register(def)
		count++
	}
	return count, nil
}

// --- JSON 加载 ---

// skillJSON 技能 JSON 结构
type skillJSON struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Level       int        `json:"level"`
	CooldownMS  int        `json:"cooldown_ms"`
	GlobalCDMS  int        `json:"global_cd_ms"`
	CastTimeMS  int        `json:"cast_time_ms"`
	BackSwingMS int        `json:"back_swing_ms"`
	TargetType  int        `json:"target_type"`
	Range       float32    `json:"range"`
	AOERadius   float32    `json:"aoe_radius"`
	CostType    int        `json:"cost_type"`
	CostValue   int        `json:"cost_value"`
	Effects     []string   `json:"effects"`
	Tags        []string   `json:"tags"`
	Description string     `json:"description"`
	Phases      []phaseJSON   `json:"phases,omitempty"`
	Triggers    []triggerJSON `json:"triggers,omitempty"`
	Chain       *chainNodeJSON `json:"chain,omitempty"`
}

// phaseJSON 阶段 JSON
type phaseJSON struct {
	Kind          int           `json:"kind"`
	DurationMS    int           `json:"duration_ms"`
	Effects       []string      `json:"effects"`
	Triggers      []triggerJSON `json:"triggers,omitempty"`
	TickIntervalMS int          `json:"tick_interval_ms,omitempty"`
	Interruptible bool          `json:"interruptible"`
}

// triggerJSON 触发器 JSON
type triggerJSON struct {
	Name         string          `json:"name"`
	Conditions   []conditionJSON `json:"conditions,omitempty"`
	ChainSkillID string          `json:"chain_skill_id,omitempty"`
	ChainDelayMS int             `json:"chain_delay_ms,omitempty"`
	ApplyBuff    string          `json:"apply_buff,omitempty"`
	ExtraEffects []string        `json:"extra_effects,omitempty"`
	Once         bool            `json:"once"`
}

// conditionJSON 条件 JSON
type conditionJSON struct {
	Type   int     `json:"type"`
	Op     int     `json:"op"`
	Value  float64 `json:"value,omitempty"`
	BuffID string  `json:"buff_id,omitempty"`
	Negate bool    `json:"negate,omitempty"`
}

// chainNodeJSON DAG 节点 JSON（递归）
type chainNodeJSON struct {
	ID        string          `json:"id"`
	SkillID   string          `json:"skill_id,omitempty"`
	BuffID    string          `json:"buff_id,omitempty"`
	Effects   []string        `json:"effects,omitempty"`
	DelayMS   int             `json:"delay_ms,omitempty"`
	Condition *conditionJSON  `json:"condition,omitempty"`
	Next      []chainNodeJSON `json:"next,omitempty"`
}

// buffJSON Buff JSON 结构
type buffJSON struct {
	ID           string              `json:"id"`
	Name         string              `json:"name"`
	Category     int                 `json:"category"`
	DurationMS   int                 `json:"duration_ms"`
	MaxStack     int                 `json:"max_stack"`
	StackPolicy  int                 `json:"stack_policy"`
	TickInterval int                 `json:"tick_interval_ms"`
	Priority     int                 `json:"priority"`
	MutexGroup   string              `json:"mutex_group"`
	Tags         []string            `json:"tags"`
	Modifiers    []AttributeModifier `json:"modifiers"`
	TickDamage   float32             `json:"tick_damage"`
	TickHeal     float32             `json:"tick_heal"`
}

// LoadSkillsFromJSON 从 JSON 文件加载技能列表
func LoadSkillsFromJSON(path string, registry *SkillRegistry) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	var rows []skillJSON
	if err := json.Unmarshal(data, &rows); err != nil {
		return 0, err
	}
	for _, r := range rows {
		registry.Register(skillJSONToDef(&r))
	}
	return len(rows), nil
}

// LoadBuffsFromJSON 从 JSON 文件加载 Buff 列表
func LoadBuffsFromJSON(path string, registry *BuffRegistry) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	var rows []buffJSON
	if err := json.Unmarshal(data, &rows); err != nil {
		return 0, err
	}
	for _, r := range rows {
		registry.Register(buffJSONToDef(&r))
	}
	return len(rows), nil
}

// BuffRegistry Buff 定义注册表
type BuffRegistry struct {
	buffs map[string]*BuffDef
}

// NewBuffRegistry 创建 Buff 注册表
func NewBuffRegistry() *BuffRegistry {
	return &BuffRegistry{
		buffs: make(map[string]*BuffDef),
	}
}

// Register 注册
func (r *BuffRegistry) Register(def *BuffDef) {
	r.buffs[def.ID] = def
}

// Get 获取
func (r *BuffRegistry) Get(id string) (*BuffDef, bool) {
	def, ok := r.buffs[id]
	return def, ok
}

// All 列出全部
func (r *BuffRegistry) All() []*BuffDef {
	result := make([]*BuffDef, 0, len(r.buffs))
	for _, d := range r.buffs {
		result = append(result, d)
	}
	return result
}

// --- 行/JSON 到 Def 转换 ---

func rowToSkillDef(r *skillRow) (*SkillDef, error) {
	if r.ID == "" {
		return nil, fmt.Errorf("empty skill id")
	}
	return &SkillDef{
		ID:          r.ID,
		Name:        r.Name,
		Level:       r.Level,
		Cooldown:    time.Duration(r.CooldownMS) * time.Millisecond,
		GlobalCD:    time.Duration(r.GlobalCDMS) * time.Millisecond,
		CastTime:    time.Duration(r.CastTimeMS) * time.Millisecond,
		BackSwing:   time.Duration(r.BackSwingMS) * time.Millisecond,
		TargetType:  TargetType(r.TargetType),
		Range:       r.Range,
		AOERadius:   r.AOERadius,
		CostType:    CostType(r.CostType),
		CostValue:   r.CostValue,
		Effects:     splitList(r.Effects),
		Tags:        splitList(r.Tags),
		Description: r.Description,
	}, nil
}

func rowToBuffDef(r *buffRow) (*BuffDef, error) {
	if r.ID == "" {
		return nil, fmt.Errorf("empty buff id")
	}
	var mods []AttributeModifier
	trimmed := strings.TrimSpace(r.Modifiers)
	if trimmed != "" && trimmed != "[]" {
		if err := json.Unmarshal([]byte(trimmed), &mods); err != nil {
			return nil, fmt.Errorf("modifiers unmarshal: %w", err)
		}
	}
	return &BuffDef{
		ID:           r.ID,
		Name:         r.Name,
		Category:     BuffCategory(r.Category),
		Duration:     time.Duration(r.DurationMS) * time.Millisecond,
		MaxStack:     r.MaxStack,
		StackPolicy:  StackPolicy(r.StackPolicy),
		TickInterval: time.Duration(r.TickInterval) * time.Millisecond,
		Priority:     r.Priority,
		MutexGroup:   r.MutexGroup,
		Tags:         splitList(r.Tags),
		Modifiers:    mods,
		TickDamage:   r.TickDamage,
		TickHeal:     r.TickHeal,
	}, nil
}

func skillJSONToDef(r *skillJSON) *SkillDef {
	def := &SkillDef{
		ID:          r.ID,
		Name:        r.Name,
		Level:       r.Level,
		Cooldown:    time.Duration(r.CooldownMS) * time.Millisecond,
		GlobalCD:    time.Duration(r.GlobalCDMS) * time.Millisecond,
		CastTime:    time.Duration(r.CastTimeMS) * time.Millisecond,
		BackSwing:   time.Duration(r.BackSwingMS) * time.Millisecond,
		TargetType:  TargetType(r.TargetType),
		Range:       r.Range,
		AOERadius:   r.AOERadius,
		CostType:    CostType(r.CostType),
		CostValue:   r.CostValue,
		Effects:     r.Effects,
		Tags:        r.Tags,
		Description: r.Description,
	}
	if len(r.Phases) > 0 {
		ps := &PhasedSkill{Phases: make([]PhaseDef, len(r.Phases))}
		for i, p := range r.Phases {
			ps.Phases[i] = PhaseDef{
				Kind:          PhaseKind(p.Kind),
				Duration:      time.Duration(p.DurationMS) * time.Millisecond,
				Effects:       p.Effects,
				Triggers:      triggersFromJSON(p.Triggers),
				TickInterval:  time.Duration(p.TickIntervalMS) * time.Millisecond,
				Interruptible: p.Interruptible,
			}
		}
		def.Phased = ps
	}
	if len(r.Triggers) > 0 {
		def.Triggers = triggersFromJSON(r.Triggers)
	}
	if r.Chain != nil {
		def.Chain = &ChainPlan{Root: chainNodeFromJSON(r.Chain)}
	}
	return def
}

func triggersFromJSON(list []triggerJSON) []*Trigger {
	if len(list) == 0 {
		return nil
	}
	out := make([]*Trigger, len(list))
	for i, t := range list {
		trg := &Trigger{
			Name:         t.Name,
			ChainSkillID: t.ChainSkillID,
			ChainDelayMS: t.ChainDelayMS,
			ApplyBuff:    t.ApplyBuff,
			ExtraEffects: t.ExtraEffects,
			Once:         t.Once,
		}
		if len(t.Conditions) > 0 {
			trg.Conditions = make([]Condition, len(t.Conditions))
			for j, c := range t.Conditions {
				trg.Conditions[j] = conditionFromJSON(c)
			}
		}
		out[i] = trg
	}
	return out
}

func conditionFromJSON(c conditionJSON) Condition {
	return Condition{
		Type:   ConditionType(c.Type),
		Op:     ConditionOp(c.Op),
		Value:  c.Value,
		BuffID: c.BuffID,
		Negate: c.Negate,
	}
}

func chainNodeFromJSON(n *chainNodeJSON) *ChainNode {
	if n == nil {
		return nil
	}
	node := &ChainNode{
		ID:      n.ID,
		SkillID: n.SkillID,
		BuffID:  n.BuffID,
		Effects: n.Effects,
		Delay:   time.Duration(n.DelayMS) * time.Millisecond,
	}
	if n.Condition != nil {
		cd := conditionFromJSON(*n.Condition)
		node.Condition = &cd
	}
	if len(n.Next) > 0 {
		node.Next = make([]*ChainNode, len(n.Next))
		for i := range n.Next {
			node.Next[i] = chainNodeFromJSON(&n.Next[i])
		}
	}
	return node
}

func buffJSONToDef(r *buffJSON) *BuffDef {
	return &BuffDef{
		ID:           r.ID,
		Name:         r.Name,
		Category:     BuffCategory(r.Category),
		Duration:     time.Duration(r.DurationMS) * time.Millisecond,
		MaxStack:     r.MaxStack,
		StackPolicy:  StackPolicy(r.StackPolicy),
		TickInterval: time.Duration(r.TickInterval) * time.Millisecond,
		Priority:     r.Priority,
		MutexGroup:   r.MutexGroup,
		Tags:         r.Tags,
		Modifiers:    r.Modifiers,
		TickDamage:   r.TickDamage,
		TickHeal:     r.TickHeal,
	}
}

// splitList 按 "|" 拆分字符串列表，空字符串返回 nil
func splitList(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	// 支持 JSON 数组形式 ["a","b"]
	if strings.HasPrefix(s, "[") {
		var list []string
		if err := json.Unmarshal([]byte(s), &list); err == nil {
			return list
		}
	}
	parts := strings.Split(s, "|")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ParseDurationMS 辅助函数：将毫秒字符串转为 time.Duration
func ParseDurationMS(s string) (time.Duration, error) {
	ms, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0, err
	}
	return time.Duration(ms) * time.Millisecond, nil
}
