package canary

import (
	"hash/fnv"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Rule 灰度路由规则
type Rule struct {
	Name       string      `json:"name"`
	Priority   int         `json:"priority"`   // 越小优先级越高
	Conditions []Condition `json:"conditions"`  // AND 逻辑，全部满足才匹配
	Target     string      `json:"target"`      // 目标版本
	Weight     int         `json:"weight"`      // 0-100 权重（仅在无明确规则匹配时使用）
}

// Condition 匹配条件
type Condition struct {
	Field    string `json:"field"`    // "user_id", "region", "channel", "user_id_mod"
	Operator string `json:"operator"` // "eq", "in", "range", "mod"
	Value    string `json:"value"`    // 单值或逗号分隔
}

// Engine 灰度发布引擎
type Engine struct {
	mu      sync.RWMutex
	rules   []Rule
	weights map[string]int // version -> weight (总和应为 100)
	enabled bool
}

// NewEngine 创建灰度引擎
func NewEngine() *Engine {
	return &Engine{
		weights: make(map[string]int),
	}
}

// Enable 启用灰度路由
func (e *Engine) Enable() {
	e.mu.Lock()
	e.enabled = true
	e.mu.Unlock()
}

// Disable 关闭灰度路由
func (e *Engine) Disable() {
	e.mu.Lock()
	e.enabled = false
	e.mu.Unlock()
}

// IsEnabled 是否启用
func (e *Engine) IsEnabled() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.enabled
}

// SetRules 原子替换所有规则（按 Priority 排序）
func (e *Engine) SetRules(rules []Rule) {
	sorted := make([]Rule, len(rules))
	copy(sorted, rules)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Priority < sorted[j].Priority
	})
	e.mu.Lock()
	e.rules = sorted
	e.mu.Unlock()
}

// Rules 返回当前规则
func (e *Engine) Rules() []Rule {
	e.mu.RLock()
	defer e.mu.RUnlock()
	result := make([]Rule, len(e.rules))
	copy(result, e.rules)
	return result
}

// SetWeights 设置版本权重路由（如 {"v1": 95, "v2": 5}）
func (e *Engine) SetWeights(weights map[string]int) error {
	total := 0
	for _, w := range weights {
		if w < 0 {
			return &WeightError{Msg: "weight cannot be negative"}
		}
		total += w
	}
	if total != 100 && total != 0 {
		return &WeightError{Msg: "weights must sum to 100, got " + strconv.Itoa(total)}
	}
	e.mu.Lock()
	e.weights = weights
	e.mu.Unlock()
	return nil
}

// Weights 返回当前权重
func (e *Engine) Weights() map[string]int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	result := make(map[string]int, len(e.weights))
	for k, v := range e.weights {
		result[k] = v
	}
	return result
}

// Route 决定请求应路由到哪个版本
// labels 包含请求标签（如 user_id, region, channel）
// 返回目标版本字符串，如果未匹配任何规则且无权重配置，返回空字符串
func (e *Engine) Route(labels map[string]string) string {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if !e.enabled {
		return ""
	}

	// 1. 按优先级匹配规则
	for _, rule := range e.rules {
		if matchRule(rule, labels) {
			return rule.Target
		}
	}

	// 2. 按权重路由（使用 user_id 做确定性哈希，保证粘性）
	if len(e.weights) > 0 {
		return weightRoute(e.weights, labels)
	}

	return ""
}

// Status 返回灰度状态摘要
func (e *Engine) Status() map[string]interface{} {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return map[string]interface{}{
		"enabled": e.enabled,
		"rules":   len(e.rules),
		"weights": e.weights,
	}
}

// Promote 全量发布：将指定版本权重设为 100%，清空规则
func (e *Engine) Promote(version string) {
	e.mu.Lock()
	e.weights = map[string]int{version: 100}
	e.rules = nil
	e.mu.Unlock()
}

// Rollback 回滚：将指定版本权重设为 100%，清空规则
func (e *Engine) Rollback(version string) {
	e.Promote(version) // 逻辑相同，语义不同
}

// matchRule 检查标签是否满足规则的所有条件
func matchRule(rule Rule, labels map[string]string) bool {
	for _, cond := range rule.Conditions {
		if !matchCondition(cond, labels) {
			return false
		}
	}
	return len(rule.Conditions) > 0
}

// matchCondition 检查单个条件
func matchCondition(cond Condition, labels map[string]string) bool {
	val, exists := labels[cond.Field]

	switch cond.Operator {
	case "eq":
		return val == cond.Value
	case "in":
		parts := strings.Split(cond.Value, ",")
		for _, p := range parts {
			if strings.TrimSpace(p) == val {
				return true
			}
		}
		return false
	case "range":
		// 格式: "min-max"，值为数字
		parts := strings.SplitN(cond.Value, "-", 2)
		if len(parts) != 2 {
			return false
		}
		min, err1 := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
		max, err2 := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
		v, err3 := strconv.ParseInt(val, 10, 64)
		if err1 != nil || err2 != nil || err3 != nil {
			return false
		}
		return v >= min && v <= max
	case "mod":
		// 格式: "divisor,remainder"，对 field 值取模
		if !exists {
			return false
		}
		parts := strings.SplitN(cond.Value, ",", 2)
		if len(parts) != 2 {
			return false
		}
		divisor, err1 := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
		remainder, err2 := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
		if err1 != nil || err2 != nil || divisor <= 0 {
			return false
		}
		h := fnv.New32a()
		h.Write([]byte(val))
		return int64(h.Sum32())%divisor == remainder
	default:
		return false
	}
}

// weightRoute 基于权重的确定性路由
func weightRoute(weights map[string]int, labels map[string]string) string {
	// 使用 user_id 或第一个可用标签做哈希，保证粘性
	hashKey := labels["user_id"]
	if hashKey == "" {
		// 回退到任意可用标签
		for _, v := range labels {
			hashKey = v
			break
		}
	}
	if hashKey == "" {
		hashKey = "default"
	}

	h := fnv.New32a()
	h.Write([]byte(hashKey))
	bucket := int(h.Sum32() % 100)

	// 按版本名排序确保确定性
	versions := make([]string, 0, len(weights))
	for v := range weights {
		versions = append(versions, v)
	}
	sort.Strings(versions)

	cumulative := 0
	for _, v := range versions {
		cumulative += weights[v]
		if bucket < cumulative {
			return v
		}
	}

	// 回退：返回最后一个版本
	if len(versions) > 0 {
		return versions[len(versions)-1]
	}
	return ""
}

// WeightError 权重配置错误
type WeightError struct {
	Msg string
}

func (e *WeightError) Error() string {
	return "canary weight error: " + e.Msg
}
