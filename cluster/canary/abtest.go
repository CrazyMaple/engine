package canary

import (
	"hash/fnv"
	"sort"
	"sync"
	"time"
)

// ExperimentStatus 实验状态
type ExperimentStatus string

const (
	ExperimentDraft     ExperimentStatus = "draft"
	ExperimentRunning   ExperimentStatus = "running"
	ExperimentPaused    ExperimentStatus = "paused"
	ExperimentCompleted ExperimentStatus = "completed"
)

// Variant 实验变体
type Variant struct {
	// Name 变体名（如 "control", "treatment_a", "treatment_b"）
	Name string `json:"name"`
	// Weight 流量权重（所有变体权重之和应为 100）
	Weight int `json:"weight"`
	// Config 变体配置（由业务层解读）
	Config map[string]string `json:"config,omitempty"`
}

// Experiment A/B 测试实验定义
type Experiment struct {
	// ID 实验唯一标识
	ID string `json:"id"`
	// Name 实验名称
	Name string `json:"name"`
	// Description 实验描述
	Description string `json:"description,omitempty"`
	// Status 实验状态
	Status ExperimentStatus `json:"status"`
	// Variants 变体列表
	Variants []Variant `json:"variants"`
	// TargetRules 参与实验的用户匹配规则（可选，为空则全量参与）
	TargetRules []ConditionGroup `json:"targetRules,omitempty"`
	// StartTime 实验开始时间
	StartTime *time.Time `json:"startTime,omitempty"`
	// EndTime 实验结束时间
	EndTime *time.Time `json:"endTime,omitempty"`
	// CreatedAt 创建时间
	CreatedAt time.Time `json:"createdAt"`
}

// ExperimentResult 实验分配结果
type ExperimentResult struct {
	ExperimentID string `json:"experimentId"`
	VariantName  string `json:"variantName"`
}

// ABTestManager A/B 测试管理器
type ABTestManager struct {
	mu          sync.RWMutex
	experiments map[string]*Experiment

	// 实验命中统计：experiment_id -> variant_name -> count
	assignCounts map[string]map[string]int64
}

// NewABTestManager 创建 A/B 测试管理器
func NewABTestManager() *ABTestManager {
	return &ABTestManager{
		experiments:  make(map[string]*Experiment),
		assignCounts: make(map[string]map[string]int64),
	}
}

// CreateExperiment 创建实验
func (m *ABTestManager) CreateExperiment(exp Experiment) error {
	if exp.ID == "" {
		return &WeightError{Msg: "experiment ID is required"}
	}
	if len(exp.Variants) < 2 {
		return &WeightError{Msg: "at least 2 variants required"}
	}

	total := 0
	for _, v := range exp.Variants {
		if v.Weight < 0 {
			return &WeightError{Msg: "variant weight cannot be negative"}
		}
		total += v.Weight
	}
	if total != 100 {
		return &WeightError{Msg: "variant weights must sum to 100"}
	}

	if exp.Status == "" {
		exp.Status = ExperimentDraft
	}
	exp.CreatedAt = time.Now()

	m.mu.Lock()
	m.experiments[exp.ID] = &exp
	m.assignCounts[exp.ID] = make(map[string]int64)
	m.mu.Unlock()
	return nil
}

// GetExperiment 获取实验
func (m *ABTestManager) GetExperiment(id string) *Experiment {
	m.mu.RLock()
	defer m.mu.RUnlock()
	exp, ok := m.experiments[id]
	if !ok {
		return nil
	}
	cp := *exp
	return &cp
}

// ListExperiments 列出所有实验
func (m *ABTestManager) ListExperiments() []Experiment {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]Experiment, 0, len(m.experiments))
	for _, exp := range m.experiments {
		result = append(result, *exp)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.Before(result[j].CreatedAt)
	})
	return result
}

// StartExperiment 启动实验
func (m *ABTestManager) StartExperiment(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	exp, ok := m.experiments[id]
	if !ok {
		return &WeightError{Msg: "experiment not found: " + id}
	}
	if exp.Status != ExperimentDraft && exp.Status != ExperimentPaused {
		return &WeightError{Msg: "can only start draft or paused experiments"}
	}
	exp.Status = ExperimentRunning
	now := time.Now()
	if exp.StartTime == nil {
		exp.StartTime = &now
	}
	return nil
}

// PauseExperiment 暂停实验
func (m *ABTestManager) PauseExperiment(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	exp, ok := m.experiments[id]
	if !ok {
		return &WeightError{Msg: "experiment not found: " + id}
	}
	if exp.Status != ExperimentRunning {
		return &WeightError{Msg: "can only pause running experiments"}
	}
	exp.Status = ExperimentPaused
	return nil
}

// CompleteExperiment 完成实验
func (m *ABTestManager) CompleteExperiment(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	exp, ok := m.experiments[id]
	if !ok {
		return &WeightError{Msg: "experiment not found: " + id}
	}
	exp.Status = ExperimentCompleted
	now := time.Now()
	exp.EndTime = &now
	return nil
}

// DeleteExperiment 删除实验
func (m *ABTestManager) DeleteExperiment(id string) {
	m.mu.Lock()
	delete(m.experiments, id)
	delete(m.assignCounts, id)
	m.mu.Unlock()
}

// Assign 为用户分配实验变体
// 基于 user_id 的确定性哈希，保证同一用户始终分配到同一变体
func (m *ABTestManager) Assign(userID string, labels map[string]string) []ExperimentResult {
	m.mu.Lock()
	defer m.mu.Unlock()

	var results []ExperimentResult

	for _, exp := range m.experiments {
		if exp.Status != ExperimentRunning {
			continue
		}

		// 检查用户是否匹配实验目标规则
		if len(exp.TargetRules) > 0 {
			matched := true
			for _, group := range exp.TargetRules {
				if !matchGroup(group, labels) {
					matched = false
					break
				}
			}
			if !matched {
				continue
			}
		}

		// 基于 user_id + experiment_id 确定性哈希分桶
		variant := assignVariant(userID, exp.ID, exp.Variants)
		if variant != "" {
			results = append(results, ExperimentResult{
				ExperimentID: exp.ID,
				VariantName:  variant,
			})
			if m.assignCounts[exp.ID] == nil {
				m.assignCounts[exp.ID] = make(map[string]int64)
			}
			m.assignCounts[exp.ID][variant]++
		}
	}

	return results
}

// ExperimentStats 返回实验分配统计
func (m *ABTestManager) ExperimentStats(id string) map[string]int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	counts, ok := m.assignCounts[id]
	if !ok {
		return nil
	}
	result := make(map[string]int64, len(counts))
	for k, v := range counts {
		result[k] = v
	}
	return result
}

// assignVariant 基于确定性哈希为用户分配变体
func assignVariant(userID, experimentID string, variants []Variant) string {
	h := fnv.New32a()
	h.Write([]byte(userID))
	h.Write([]byte(":"))
	h.Write([]byte(experimentID))
	bucket := int(h.Sum32() % 100)

	cumulative := 0
	for _, v := range variants {
		cumulative += v.Weight
		if bucket < cumulative {
			return v.Name
		}
	}

	if len(variants) > 0 {
		return variants[len(variants)-1].Name
	}
	return ""
}
