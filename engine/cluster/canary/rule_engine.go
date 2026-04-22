package canary

import (
	"hash/fnv"
	"sort"
	"sync"
)

// ConditionGroup 条件组，支持 AND/OR 逻辑
type ConditionGroup struct {
	// Logic 组内条件的逻辑关系："and"（默认）或 "or"
	Logic string `json:"logic,omitempty"`
	// Conditions 条件列表
	Conditions []Condition `json:"conditions"`
}

// AdvancedRule 增强型规则，支持 AND/OR 组合条件
type AdvancedRule struct {
	Name     string           `json:"name"`
	Priority int              `json:"priority"` // 越小优先级越高
	Groups   []ConditionGroup `json:"groups"`   // 组间为 AND，组内按 Logic 字段
	Target   string           `json:"target"`   // 目标版本
	Enabled  bool             `json:"enabled"`  // 规则是否启用
}

// RuleEngine 增强规则引擎
// 在基础 Engine 之上提供 OR 逻辑、规则启用/禁用、命中统计
type RuleEngine struct {
	mu    sync.RWMutex
	rules []AdvancedRule

	// 命中统计：rule name -> hit count
	hitCounts map[string]int64
}

// NewRuleEngine 创建增强规则引擎
func NewRuleEngine() *RuleEngine {
	return &RuleEngine{
		hitCounts: make(map[string]int64),
	}
}

// SetAdvancedRules 设置增强规则集（按优先级排序）
func (re *RuleEngine) SetAdvancedRules(rules []AdvancedRule) {
	sorted := make([]AdvancedRule, len(rules))
	copy(sorted, rules)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Priority < sorted[j].Priority
	})
	re.mu.Lock()
	re.rules = sorted
	re.hitCounts = make(map[string]int64)
	re.mu.Unlock()
}

// AdvancedRules 返回当前规则
func (re *RuleEngine) AdvancedRules() []AdvancedRule {
	re.mu.RLock()
	defer re.mu.RUnlock()
	result := make([]AdvancedRule, len(re.rules))
	copy(result, re.rules)
	return result
}

// Match 按优先级匹配规则，返回目标版本
// labels 包含请求标签（user_id, region, channel 等）
func (re *RuleEngine) Match(labels map[string]string) string {
	re.mu.Lock()
	defer re.mu.Unlock()

	for i := range re.rules {
		rule := &re.rules[i]
		if !rule.Enabled {
			continue
		}
		if matchAdvancedRule(rule, labels) {
			re.hitCounts[rule.Name]++
			return rule.Target
		}
	}
	return ""
}

// HitCounts 返回各规则命中统计
func (re *RuleEngine) HitCounts() map[string]int64 {
	re.mu.RLock()
	defer re.mu.RUnlock()
	result := make(map[string]int64, len(re.hitCounts))
	for k, v := range re.hitCounts {
		result[k] = v
	}
	return result
}

// ResetHitCounts 重置命中统计
func (re *RuleEngine) ResetHitCounts() {
	re.mu.Lock()
	re.hitCounts = make(map[string]int64)
	re.mu.Unlock()
}

// matchAdvancedRule 匹配增强规则：组间为 AND，组内按 Logic 字段
func matchAdvancedRule(rule *AdvancedRule, labels map[string]string) bool {
	if len(rule.Groups) == 0 {
		return false
	}
	// 所有组都必须匹配（组间 AND）
	for _, group := range rule.Groups {
		if !matchGroup(group, labels) {
			return false
		}
	}
	return true
}

// matchGroup 匹配条件组
func matchGroup(group ConditionGroup, labels map[string]string) bool {
	if len(group.Conditions) == 0 {
		return false
	}

	if group.Logic == "or" {
		// OR：任一条件匹配即可
		for _, cond := range group.Conditions {
			if matchCondition(cond, labels) {
				return true
			}
		}
		return false
	}

	// AND（默认）：所有条件必须匹配
	for _, cond := range group.Conditions {
		if !matchCondition(cond, labels) {
			return false
		}
	}
	return true
}

// IntegrateWithEngine 将增强规则引擎集成到基础 Engine
// 在 Engine.Route() 的规则匹配阶段优先使用 RuleEngine
func IntegrateWithEngine(engine *Engine, ruleEngine *RuleEngine) *IntegratedEngine {
	return &IntegratedEngine{
		base:       engine,
		ruleEngine: ruleEngine,
	}
}

// IntegratedEngine 集成了增强规则引擎的灰度引擎
type IntegratedEngine struct {
	base       *Engine
	ruleEngine *RuleEngine
}

// Route 路由请求：先匹配增强规则，再回退到基础 Engine
func (ie *IntegratedEngine) Route(labels map[string]string) string {
	if !ie.base.IsEnabled() {
		return ""
	}

	// 1. 增强规则优先
	if target := ie.ruleEngine.Match(labels); target != "" {
		return target
	}

	// 2. 回退到基础 Engine（基础规则 + 权重路由）
	return ie.base.Route(labels)
}

// UserBucket 根据 user_id 计算固定分桶（0-99）
// 用于按百分比精确分流
func UserBucket(userID string) int {
	h := fnv.New32a()
	h.Write([]byte(userID))
	return int(h.Sum32() % 100)
}
