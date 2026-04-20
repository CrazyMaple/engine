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

// variantObservations 单个变体的指标观测
type variantObservations struct {
	// continuous 连续型指标的 Welford 累积量（支持多指标，按 metric 名分桶）
	continuous map[string]*runningStats
	// successes 计数型指标：某 metric 下的成功次数
	successes map[string]int64
	// trials 计数型指标：某 metric 下的尝试次数
	trials map[string]int64
}

func newVariantObservations() *variantObservations {
	return &variantObservations{
		continuous: make(map[string]*runningStats),
		successes:  make(map[string]int64),
		trials:     make(map[string]int64),
	}
}

// runningStats Welford 在线均值/方差
type runningStats struct {
	n    int
	mean float64
	m2   float64
}

func (r *runningStats) push(x float64) {
	r.n++
	delta := x - r.mean
	r.mean += delta / float64(r.n)
	r.m2 += delta * (x - r.mean)
}

func (r *runningStats) sample() Sample {
	if r.n <= 1 {
		return Sample{N: r.n, Mean: r.mean}
	}
	return Sample{N: r.n, Mean: r.mean, Variance: r.m2 / float64(r.n-1)}
}

// ABTestManager A/B 测试管理器
type ABTestManager struct {
	mu          sync.RWMutex
	experiments map[string]*Experiment

	// 实验命中统计：experiment_id -> variant_name -> count
	assignCounts map[string]map[string]int64

	// 实验观测值：experiment_id -> variant_name -> *variantObservations
	observations map[string]map[string]*variantObservations
}

// NewABTestManager 创建 A/B 测试管理器
func NewABTestManager() *ABTestManager {
	return &ABTestManager{
		experiments:  make(map[string]*Experiment),
		assignCounts: make(map[string]map[string]int64),
		observations: make(map[string]map[string]*variantObservations),
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
	m.observations[exp.ID] = make(map[string]*variantObservations)
	for _, v := range exp.Variants {
		m.observations[exp.ID][v.Name] = newVariantObservations()
	}
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
	delete(m.observations, id)
	m.mu.Unlock()
}

// RecordMetric 记录连续型指标观测（如在线时长、客单价）
// 若 experiment 或 variant 不存在则静默丢弃。
func (m *ABTestManager) RecordMetric(experimentID, variant, metric string, value float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	bucket := m.observations[experimentID]
	if bucket == nil {
		return
	}
	obs := bucket[variant]
	if obs == nil {
		return
	}
	rs := obs.continuous[metric]
	if rs == nil {
		rs = &runningStats{}
		obs.continuous[metric] = rs
	}
	rs.push(value)
}

// RecordConversion 记录转化/成功事件（Successes++, Trials++）
func (m *ABTestManager) RecordConversion(experimentID, variant, metric string, success bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	bucket := m.observations[experimentID]
	if bucket == nil {
		return
	}
	obs := bucket[variant]
	if obs == nil {
		return
	}
	obs.trials[metric]++
	if success {
		obs.successes[metric]++
	}
}

// AnalyzeOptions 分析选项
type AnalyzeOptions struct {
	// Metric 指定指标名
	Metric string
	// Control 对照组变体名（默认 exp.Variants[0]）
	Control string
	// Treatment 实验组变体名（默认 exp.Variants[1]）
	Treatment string
	// Kind 指标类型："continuous"（默认）或 "proportion"
	Kind string
	// Alpha 显著性水平（默认 0.05）
	Alpha float64
	// MinSample 每组最小样本量（默认 30）
	MinSample int
}

// Analyze 对实验进行显著性分析
// 未记录任何观测值时返回 Underpowered 结论。
func (m *ABTestManager) Analyze(experimentID string, opts AnalyzeOptions) (ExperimentAnalysis, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	exp, ok := m.experiments[experimentID]
	if !ok {
		return ExperimentAnalysis{}, &WeightError{Msg: "experiment not found: " + experimentID}
	}
	bucket := m.observations[experimentID]
	if bucket == nil {
		return ExperimentAnalysis{}, &WeightError{Msg: "experiment has no observations: " + experimentID}
	}
	control := opts.Control
	treatment := opts.Treatment
	if control == "" || treatment == "" {
		if len(exp.Variants) < 2 {
			return ExperimentAnalysis{}, &WeightError{Msg: "experiment needs at least 2 variants"}
		}
		if control == "" {
			control = exp.Variants[0].Name
		}
		if treatment == "" {
			treatment = exp.Variants[1].Name
		}
	}
	c, cok := bucket[control]
	t, tok := bucket[treatment]
	if !cok || !tok {
		return ExperimentAnalysis{}, &WeightError{Msg: "unknown variant"}
	}

	switch opts.Kind {
	case "", "continuous":
		metric := opts.Metric
		if metric == "" {
			return ExperimentAnalysis{}, &WeightError{Msg: "metric name required for continuous analysis"}
		}
		cs := Sample{}
		ts := Sample{}
		if rs := c.continuous[metric]; rs != nil {
			cs = rs.sample()
		}
		if rs := t.continuous[metric]; rs != nil {
			ts = rs.sample()
		}
		return AnalyzeContinuous(cs, ts, opts.Alpha, opts.MinSample), nil
	case "proportion":
		metric := opts.Metric
		if metric == "" {
			return ExperimentAnalysis{}, &WeightError{Msg: "metric name required for proportion analysis"}
		}
		cp := Proportion{N: int(c.trials[metric]), Successes: int(c.successes[metric])}
		tp := Proportion{N: int(t.trials[metric]), Successes: int(t.successes[metric])}
		return AnalyzeProportion(cp, tp, opts.Alpha, opts.MinSample), nil
	default:
		return ExperimentAnalysis{}, &WeightError{Msg: "unsupported analysis kind: " + opts.Kind}
	}
}

// ObservedMetrics 返回某实验下已记录的指标名清单（按变体分桶）
func (m *ABTestManager) ObservedMetrics(experimentID string) map[string]map[string]int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	bucket := m.observations[experimentID]
	if bucket == nil {
		return nil
	}
	out := make(map[string]map[string]int64, len(bucket))
	for variant, obs := range bucket {
		counts := make(map[string]int64)
		for metric, rs := range obs.continuous {
			counts["continuous:"+metric] = int64(rs.n)
		}
		for metric, n := range obs.trials {
			counts["proportion:"+metric] = n
		}
		out[variant] = counts
	}
	return out
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
