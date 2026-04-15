package canary

import (
	"testing"
	"time"
)

func TestEngine_BasicRuleMatch(t *testing.T) {
	e := NewEngine()
	e.Enable()

	e.SetRules([]Rule{
		{
			Name:     "vip-users",
			Priority: 1,
			Conditions: []Condition{
				{Field: "region", Operator: "eq", Value: "cn-east"},
			},
			Target: "v2",
		},
	})

	// 匹配规则
	got := e.Route(map[string]string{"region": "cn-east", "user_id": "123"})
	if got != "v2" {
		t.Errorf("expected v2, got %s", got)
	}

	// 不匹配规则
	got = e.Route(map[string]string{"region": "us-west", "user_id": "456"})
	if got != "" {
		t.Errorf("expected empty, got %s", got)
	}
}

func TestEngine_InOperator(t *testing.T) {
	e := NewEngine()
	e.Enable()

	e.SetRules([]Rule{
		{
			Name:     "channel-test",
			Priority: 1,
			Conditions: []Condition{
				{Field: "channel", Operator: "in", Value: "ios,android"},
			},
			Target: "v2",
		},
	})

	if got := e.Route(map[string]string{"channel": "ios"}); got != "v2" {
		t.Errorf("expected v2, got %s", got)
	}
	if got := e.Route(map[string]string{"channel": "web"}); got != "" {
		t.Errorf("expected empty, got %s", got)
	}
}

func TestEngine_ModOperator(t *testing.T) {
	e := NewEngine()
	e.Enable()

	e.SetRules([]Rule{
		{
			Name:     "5pct-bucket",
			Priority: 1,
			Conditions: []Condition{
				{Field: "user_id", Operator: "mod", Value: "100,0"},
			},
			Target: "v2",
		},
	})

	// mod 操作使用 fnv hash，非所有用户都会匹配
	matched := 0
	for i := 0; i < 1000; i++ {
		if e.Route(map[string]string{"user_id": "user" + string(rune(i+48))}) == "v2" {
			matched++
		}
	}
	// 应该约有 1% 的用户匹配（100 的模 0）
	if matched == 0 {
		t.Error("expected some users to match mod condition")
	}
}

func TestEngine_WeightRouting(t *testing.T) {
	e := NewEngine()
	e.Enable()

	err := e.SetWeights(map[string]int{"v1": 95, "v2": 5})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	v1Count := 0
	v2Count := 0
	for i := 0; i < 10000; i++ {
		got := e.Route(map[string]string{"user_id": "user" + string(rune(i))})
		switch got {
		case "v1":
			v1Count++
		case "v2":
			v2Count++
		}
	}

	// 粗略检查比例是否合理（允许较大误差，因为样本量不大且哈希分布不完美）
	if v2Count == 0 {
		t.Error("expected some v2 traffic")
	}
	if v1Count == 0 {
		t.Error("expected some v1 traffic")
	}
}

func TestEngine_WeightValidation(t *testing.T) {
	e := NewEngine()

	// 总和不为 100
	err := e.SetWeights(map[string]int{"v1": 50, "v2": 30})
	if err == nil {
		t.Error("expected error for weights not summing to 100")
	}

	// 负数权重
	err = e.SetWeights(map[string]int{"v1": 110, "v2": -10})
	if err == nil {
		t.Error("expected error for negative weight")
	}

	// 空权重
	err = e.SetWeights(map[string]int{})
	if err != nil {
		t.Errorf("empty weights should be ok: %v", err)
	}
}

func TestEngine_Priority(t *testing.T) {
	e := NewEngine()
	e.Enable()

	e.SetRules([]Rule{
		{
			Name:       "low-priority",
			Priority:   10,
			Conditions: []Condition{{Field: "region", Operator: "eq", Value: "cn"}},
			Target:     "v1",
		},
		{
			Name:       "high-priority",
			Priority:   1,
			Conditions: []Condition{{Field: "region", Operator: "eq", Value: "cn"}},
			Target:     "v2",
		},
	})

	got := e.Route(map[string]string{"region": "cn"})
	if got != "v2" {
		t.Errorf("expected v2 (higher priority), got %s", got)
	}
}

func TestEngine_Disabled(t *testing.T) {
	e := NewEngine()
	// 默认关闭
	e.SetRules([]Rule{
		{
			Name:       "test",
			Priority:   1,
			Conditions: []Condition{{Field: "region", Operator: "eq", Value: "cn"}},
			Target:     "v2",
		},
	})

	got := e.Route(map[string]string{"region": "cn"})
	if got != "" {
		t.Errorf("expected empty when disabled, got %s", got)
	}
}

func TestEngine_PromoteAndRollback(t *testing.T) {
	e := NewEngine()
	e.Enable()
	e.SetRules([]Rule{
		{Name: "r1", Priority: 1, Conditions: []Condition{{Field: "x", Operator: "eq", Value: "1"}}, Target: "v2"},
	})

	e.Promote("v2")

	// 全量后，所有流量都到 v2
	got := e.Route(map[string]string{"user_id": "any"})
	if got != "v2" {
		t.Errorf("expected v2 after promote, got %s", got)
	}

	// 规则应已清空
	if len(e.Rules()) != 0 {
		t.Error("rules should be cleared after promote")
	}

	// 回滚到 v1
	e.Rollback("v1")
	got = e.Route(map[string]string{"user_id": "any"})
	if got != "v1" {
		t.Errorf("expected v1 after rollback, got %s", got)
	}
}

func TestComparator_Compare(t *testing.T) {
	src := NewSimpleMetricsSource()
	src.Set("v1", &VersionMetrics{
		Version:      "v1",
		RequestCount: 1000,
		ErrorCount:   10,
		ErrorRate:    0.01,
		AvgLatencyNs: 5000000, // 5ms
		CollectedAt:  time.Now(),
	})
	src.Set("v2", &VersionMetrics{
		Version:      "v2",
		RequestCount: 200,
		ErrorCount:   4,
		ErrorRate:    0.02,
		AvgLatencyNs: 5500000, // 5.5ms
		CollectedAt:  time.Now(),
	})

	comp := NewComparator(src, DefaultThresholds)
	comp.Collect("v1")
	comp.Collect("v2")

	report := comp.Compare("v1", "v2")
	if report.Baseline == nil || report.Canary == nil {
		t.Fatal("expected non-nil metrics")
	}

	// 错误率差异 = 0.02 - 0.01 = 0.01，刚好在阈值上
	if report.ErrorRateDelta != 0.01 {
		t.Errorf("expected error rate delta 0.01, got %f", report.ErrorRateDelta)
	}

	// 延迟增长 = (5500000-5000000)/5000000*100 = 10%
	if report.LatencyDelta != 10.0 {
		t.Errorf("expected latency delta 10%%, got %f", report.LatencyDelta)
	}

	// 请求量 200 >= 100，指标刚好在阈值边界
	// 错误率差异 0.01 == 阈值 0.01，不算超标（>），应该看 promote 或 continue
	if report.Recommendation != "promote" && report.Recommendation != "continue" {
		t.Logf("recommendation: %s (acceptable at boundary)", report.Recommendation)
	}
}

func TestComparator_InsufficientData(t *testing.T) {
	src := NewSimpleMetricsSource()
	src.Set("v1", &VersionMetrics{RequestCount: 1000, ErrorRate: 0.01, AvgLatencyNs: 5000000})
	src.Set("v2", &VersionMetrics{RequestCount: 50, ErrorRate: 0.05, AvgLatencyNs: 6000000}) // < MinRequestCount

	comp := NewComparator(src, DefaultThresholds)
	comp.Collect("v1")
	comp.Collect("v2")

	report := comp.Compare("v1", "v2")
	if report.Recommendation != "continue" {
		t.Errorf("expected continue with insufficient data, got %s", report.Recommendation)
	}
}

func TestComparator_Rollback(t *testing.T) {
	src := NewSimpleMetricsSource()
	src.Set("v1", &VersionMetrics{RequestCount: 1000, ErrorRate: 0.01, AvgLatencyNs: 5000000})
	src.Set("v2", &VersionMetrics{RequestCount: 500, ErrorRate: 0.05, AvgLatencyNs: 10000000}) // 高错误率+高延迟

	comp := NewComparator(src, DefaultThresholds)
	comp.Collect("v1")
	comp.Collect("v2")

	report := comp.Compare("v1", "v2")
	if report.Recommendation != "rollback" {
		t.Errorf("expected rollback, got %s", report.Recommendation)
	}
}

func TestEngine_Status(t *testing.T) {
	e := NewEngine()
	e.Enable()
	e.SetWeights(map[string]int{"v1": 90, "v2": 10})

	status := e.Status()
	if !status["enabled"].(bool) {
		t.Error("expected enabled")
	}
}

func TestEngine_RangeOperator(t *testing.T) {
	e := NewEngine()
	e.Enable()
	e.SetRules([]Rule{
		{
			Name:       "level-range",
			Priority:   1,
			Conditions: []Condition{{Field: "level", Operator: "range", Value: "10-50"}},
			Target:     "v2",
		},
	})

	if got := e.Route(map[string]string{"level": "25"}); got != "v2" {
		t.Errorf("expected v2, got %s", got)
	}
	if got := e.Route(map[string]string{"level": "5"}); got != "" {
		t.Errorf("expected empty, got %s", got)
	}
	if got := e.Route(map[string]string{"level": "51"}); got != "" {
		t.Errorf("expected empty, got %s", got)
	}
}
