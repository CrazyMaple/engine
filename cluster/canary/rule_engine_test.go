package canary

import (
	"testing"
)

func TestRuleEngine_ANDLogic(t *testing.T) {
	re := NewRuleEngine()
	re.SetAdvancedRules([]AdvancedRule{
		{
			Name:     "cn-ios",
			Priority: 1,
			Enabled:  true,
			Groups: []ConditionGroup{
				{
					Logic: "and",
					Conditions: []Condition{
						{Field: "region", Operator: "eq", Value: "cn"},
						{Field: "channel", Operator: "eq", Value: "ios"},
					},
				},
			},
			Target: "v2",
		},
	})

	// 两个条件都满足
	if got := re.Match(map[string]string{"region": "cn", "channel": "ios"}); got != "v2" {
		t.Errorf("expected v2, got %s", got)
	}

	// 只满足一个
	if got := re.Match(map[string]string{"region": "cn", "channel": "android"}); got != "" {
		t.Errorf("expected empty, got %s", got)
	}
}

func TestRuleEngine_ORLogic(t *testing.T) {
	re := NewRuleEngine()
	re.SetAdvancedRules([]AdvancedRule{
		{
			Name:     "multi-region",
			Priority: 1,
			Enabled:  true,
			Groups: []ConditionGroup{
				{
					Logic: "or",
					Conditions: []Condition{
						{Field: "region", Operator: "eq", Value: "cn-east"},
						{Field: "region", Operator: "eq", Value: "cn-south"},
					},
				},
			},
			Target: "v2",
		},
	})

	if got := re.Match(map[string]string{"region": "cn-east"}); got != "v2" {
		t.Errorf("expected v2, got %s", got)
	}
	if got := re.Match(map[string]string{"region": "cn-south"}); got != "v2" {
		t.Errorf("expected v2, got %s", got)
	}
	if got := re.Match(map[string]string{"region": "us-west"}); got != "" {
		t.Errorf("expected empty, got %s", got)
	}
}

func TestRuleEngine_MultiGroupAND(t *testing.T) {
	re := NewRuleEngine()
	re.SetAdvancedRules([]AdvancedRule{
		{
			Name:     "cn-mobile",
			Priority: 1,
			Enabled:  true,
			Groups: []ConditionGroup{
				// 组1：地区匹配（OR）
				{
					Logic: "or",
					Conditions: []Condition{
						{Field: "region", Operator: "eq", Value: "cn-east"},
						{Field: "region", Operator: "eq", Value: "cn-south"},
					},
				},
				// 组2：渠道匹配（OR）
				{
					Logic: "or",
					Conditions: []Condition{
						{Field: "channel", Operator: "eq", Value: "ios"},
						{Field: "channel", Operator: "eq", Value: "android"},
					},
				},
			},
			Target: "v2",
		},
	})

	// 两个组都满足
	if got := re.Match(map[string]string{"region": "cn-east", "channel": "ios"}); got != "v2" {
		t.Errorf("expected v2, got %s", got)
	}

	// 只满足一个组
	if got := re.Match(map[string]string{"region": "cn-east", "channel": "web"}); got != "" {
		t.Errorf("expected empty, got %s", got)
	}
}

func TestRuleEngine_DisabledRule(t *testing.T) {
	re := NewRuleEngine()
	re.SetAdvancedRules([]AdvancedRule{
		{
			Name:    "disabled",
			Enabled: false,
			Groups: []ConditionGroup{
				{Conditions: []Condition{{Field: "region", Operator: "eq", Value: "cn"}}},
			},
			Target: "v2",
		},
	})

	if got := re.Match(map[string]string{"region": "cn"}); got != "" {
		t.Errorf("expected empty for disabled rule, got %s", got)
	}
}

func TestRuleEngine_Priority(t *testing.T) {
	re := NewRuleEngine()
	re.SetAdvancedRules([]AdvancedRule{
		{
			Name:     "low",
			Priority: 10,
			Enabled:  true,
			Groups: []ConditionGroup{
				{Conditions: []Condition{{Field: "region", Operator: "eq", Value: "cn"}}},
			},
			Target: "v1",
		},
		{
			Name:     "high",
			Priority: 1,
			Enabled:  true,
			Groups: []ConditionGroup{
				{Conditions: []Condition{{Field: "region", Operator: "eq", Value: "cn"}}},
			},
			Target: "v2",
		},
	})

	if got := re.Match(map[string]string{"region": "cn"}); got != "v2" {
		t.Errorf("expected v2 (higher priority), got %s", got)
	}
}

func TestRuleEngine_HitCounts(t *testing.T) {
	re := NewRuleEngine()
	re.SetAdvancedRules([]AdvancedRule{
		{
			Name:     "test",
			Priority: 1,
			Enabled:  true,
			Groups: []ConditionGroup{
				{Conditions: []Condition{{Field: "region", Operator: "eq", Value: "cn"}}},
			},
			Target: "v2",
		},
	})

	re.Match(map[string]string{"region": "cn"})
	re.Match(map[string]string{"region": "cn"})
	re.Match(map[string]string{"region": "us"}) // 不匹配

	counts := re.HitCounts()
	if counts["test"] != 2 {
		t.Errorf("expected 2 hits, got %d", counts["test"])
	}

	re.ResetHitCounts()
	counts = re.HitCounts()
	if counts["test"] != 0 {
		t.Errorf("expected 0 hits after reset, got %d", counts["test"])
	}
}

func TestIntegratedEngine(t *testing.T) {
	base := NewEngine()
	base.Enable()
	base.SetWeights(map[string]int{"v1": 100})

	re := NewRuleEngine()
	re.SetAdvancedRules([]AdvancedRule{
		{
			Name:     "vip",
			Priority: 1,
			Enabled:  true,
			Groups: []ConditionGroup{
				{Conditions: []Condition{{Field: "vip", Operator: "eq", Value: "true"}}},
			},
			Target: "v2",
		},
	})

	ie := IntegrateWithEngine(base, re)

	// VIP 用户走增强规则
	if got := ie.Route(map[string]string{"vip": "true", "user_id": "u1"}); got != "v2" {
		t.Errorf("expected v2, got %s", got)
	}

	// 普通用户回退到基础权重
	if got := ie.Route(map[string]string{"user_id": "u2"}); got != "v1" {
		t.Errorf("expected v1, got %s", got)
	}
}

func TestIntegratedEngine_Disabled(t *testing.T) {
	base := NewEngine()
	// 未启用

	re := NewRuleEngine()
	re.SetAdvancedRules([]AdvancedRule{
		{
			Name:    "test",
			Enabled: true,
			Groups: []ConditionGroup{
				{Conditions: []Condition{{Field: "x", Operator: "eq", Value: "1"}}},
			},
			Target: "v2",
		},
	})

	ie := IntegrateWithEngine(base, re)
	if got := ie.Route(map[string]string{"x": "1"}); got != "" {
		t.Errorf("expected empty when base engine disabled, got %s", got)
	}
}

func TestUserBucket(t *testing.T) {
	bucket := UserBucket("user123")
	if bucket < 0 || bucket >= 100 {
		t.Errorf("bucket out of range: %d", bucket)
	}

	// 确定性：同一 user_id 总是同一桶
	bucket2 := UserBucket("user123")
	if bucket != bucket2 {
		t.Errorf("expected same bucket, got %d and %d", bucket, bucket2)
	}
}
