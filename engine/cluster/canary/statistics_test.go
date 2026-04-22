package canary

import (
	"math"
	"testing"
)

// approxEqual 允许 tol 绝对误差的浮点比较
func approxEqual(a, b, tol float64) bool {
	if math.IsNaN(a) || math.IsNaN(b) {
		return false
	}
	return math.Abs(a-b) <= tol
}

func TestNewSample(t *testing.T) {
	s := NewSample([]float64{2, 4, 4, 4, 5, 5, 7, 9})
	if s.N != 8 {
		t.Errorf("N=%d want 8", s.N)
	}
	if !approxEqual(s.Mean, 5.0, 1e-9) {
		t.Errorf("mean=%v want 5", s.Mean)
	}
	// 样本方差（n-1）= 4.5714...
	if !approxEqual(s.Variance, 32.0/7.0, 1e-9) {
		t.Errorf("variance=%v want %v", s.Variance, 32.0/7.0)
	}
}

func TestWelchTTest_NoDifference(t *testing.T) {
	a := NewSample([]float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10})
	b := NewSample([]float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10})
	res := WelchTTest(a, b)
	if !approxEqual(res.PValue, 1.0, 1e-9) {
		t.Errorf("identical samples should give p=1, got %v", res.PValue)
	}
}

func TestWelchTTest_KnownValues(t *testing.T) {
	// 参考 scipy.stats.ttest_ind(equal_var=False):
	// a=[20,22,19,24,25,22,21,23], b=[28,27,26,30,29,28,27,31]
	// t ~ -7.38, p ~ 1.8e-5
	a := NewSample([]float64{20, 22, 19, 24, 25, 22, 21, 23})
	b := NewSample([]float64{28, 27, 26, 30, 29, 28, 27, 31})
	res := WelchTTest(a, b)
	if res.TStatistic <= 0 {
		t.Errorf("expected positive t for b-a mean diff, got %v", res.TStatistic)
	}
	if res.PValue > 1e-3 {
		t.Errorf("expected highly significant p, got %v", res.PValue)
	}
	// 均值差 b-a = 28.25 - 22 = 6.25
	if !approxEqual(res.MeanDiff, 6.25, 1e-9) {
		t.Errorf("mean diff=%v want 6.25", res.MeanDiff)
	}
}

func TestWelchTTest_SmallSample(t *testing.T) {
	a := NewSample([]float64{1})
	b := NewSample([]float64{2})
	res := WelchTTest(a, b)
	if res.PValue != 1.0 {
		t.Errorf("N=1 should yield p=1, got %v", res.PValue)
	}
}

func TestProportionZTest(t *testing.T) {
	// 对照 5%，实验 10%，各 1000 样本 — 显著
	c := Proportion{N: 1000, Successes: 50}
	tr := Proportion{N: 1000, Successes: 100}
	res := ProportionZTest(c, tr)
	if res.PValue > 0.01 {
		t.Errorf("expected highly significant, got p=%v", res.PValue)
	}
	if !approxEqual(res.RateDiff, 0.05, 1e-9) {
		t.Errorf("rate diff=%v want 0.05", res.RateDiff)
	}
}

func TestProportionZTest_NoDifference(t *testing.T) {
	c := Proportion{N: 500, Successes: 50}
	tr := Proportion{N: 500, Successes: 50}
	res := ProportionZTest(c, tr)
	if !approxEqual(res.PValue, 1.0, 1e-6) {
		t.Errorf("equal rates should give p≈1, got %v", res.PValue)
	}
}

func TestChiSquareTest(t *testing.T) {
	// 50/1000 vs 100/1000 —— 与 z 检验等价，p 应显著
	c := Proportion{N: 1000, Successes: 50}
	tr := Proportion{N: 1000, Successes: 100}
	res := ChiSquareTest(c, tr)
	if res.ChiSquare <= 0 {
		t.Errorf("chi-square must be positive, got %v", res.ChiSquare)
	}
	if res.PValue > 0.01 {
		t.Errorf("expected significant p, got %v", res.PValue)
	}
}

func TestWilsonInterval(t *testing.T) {
	// 50/100 在 95% 置信下约 [0.404, 0.596]
	lo, hi := WilsonInterval(50, 100, 1.96)
	if lo < 0.39 || lo > 0.42 {
		t.Errorf("lo=%v out of range", lo)
	}
	if hi < 0.58 || hi > 0.61 {
		t.Errorf("hi=%v out of range", hi)
	}
	// 极端比例：0/10 下界应紧贴 0（允许浮点精度的微小偏差）
	lo, hi = WilsonInterval(0, 10, 1.96)
	if lo < -1e-9 {
		t.Errorf("lower bound should be ~0, got %v", lo)
	}
	if hi <= 0 {
		t.Errorf("upper bound must be >0 for 0/10, got %v", hi)
	}
}

func TestAnalyzeContinuous_Underpowered(t *testing.T) {
	a := NewSample([]float64{1, 2, 3})
	b := NewSample([]float64{4, 5, 6})
	res := AnalyzeContinuous(a, b, 0.05, 30)
	if res.Verdict != VerdictUnderpowered {
		t.Errorf("expected underpowered, got %s", res.Verdict)
	}
}

func TestAnalyzeContinuous_Winner(t *testing.T) {
	ctrl := make([]float64, 100)
	treat := make([]float64, 100)
	for i := 0; i < 100; i++ {
		ctrl[i] = float64(i % 5)
		treat[i] = float64(i%5) + 2
	}
	res := AnalyzeContinuous(NewSample(ctrl), NewSample(treat), 0.05, 30)
	if res.Verdict != VerdictWinner {
		t.Errorf("expected winner, got %s (p=%v)", res.Verdict, res.PValue)
	}
	if res.Diff <= 0 {
		t.Errorf("treatment should be higher, diff=%v", res.Diff)
	}
}

func TestAnalyzeProportion_Winner(t *testing.T) {
	ctrl := Proportion{N: 500, Successes: 25}
	treat := Proportion{N: 500, Successes: 75}
	res := AnalyzeProportion(ctrl, treat, 0.05, 30)
	if res.Verdict != VerdictWinner {
		t.Errorf("expected winner, got %s (p=%v)", res.Verdict, res.PValue)
	}
}

func TestAnalyzeProportion_NoSignificant(t *testing.T) {
	ctrl := Proportion{N: 500, Successes: 50}
	treat := Proportion{N: 500, Successes: 52}
	res := AnalyzeProportion(ctrl, treat, 0.05, 30)
	if res.Verdict != VerdictNoSignificant {
		t.Errorf("expected no_significant, got %s (p=%v)", res.Verdict, res.PValue)
	}
}

func TestStdNormalCDF(t *testing.T) {
	// N(0,1) CDF 已知值
	cases := []struct {
		z, want float64
	}{
		{0, 0.5},
		{1, 0.8413},
		{1.96, 0.9750},
		{-1, 0.1587},
	}
	for _, c := range cases {
		got := stdNormalCDF(c.z)
		if !approxEqual(got, c.want, 5e-4) {
			t.Errorf("stdNormalCDF(%v)=%v want %v", c.z, got, c.want)
		}
	}
}

func TestTwoSidedTPValue(t *testing.T) {
	// df=10, t=2.228 对应 p≈0.05（双尾）
	p := twoSidedTPValue(2.228, 10)
	if !approxEqual(p, 0.05, 5e-3) {
		t.Errorf("p=%v want ~0.05", p)
	}
	// t=0 时 p=1
	if p0 := twoSidedTPValue(0, 10); !approxEqual(p0, 1.0, 1e-9) {
		t.Errorf("t=0 p=%v want 1", p0)
	}
}

func TestChiSquarePValue(t *testing.T) {
	// df=1, x=3.841 对应 p≈0.05
	p := chiSquarePValue(3.841, 1)
	if !approxEqual(p, 0.05, 5e-3) {
		t.Errorf("p=%v want ~0.05", p)
	}
}

func TestABTestManager_RecordAndAnalyze(t *testing.T) {
	m := NewABTestManager()
	exp := Experiment{
		ID: "exp1",
		Variants: []Variant{
			{Name: "control", Weight: 50},
			{Name: "treatment", Weight: 50},
		},
	}
	if err := m.CreateExperiment(exp); err != nil {
		t.Fatalf("create: %v", err)
	}
	for i := 0; i < 100; i++ {
		m.RecordMetric("exp1", "control", "dau", float64(i%5))
		m.RecordMetric("exp1", "treatment", "dau", float64(i%5)+2)
	}
	res, err := m.Analyze("exp1", AnalyzeOptions{Metric: "dau"})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if res.Verdict != VerdictWinner {
		t.Errorf("expected winner, got %s (p=%v diff=%v)", res.Verdict, res.PValue, res.Diff)
	}
	if res.ControlN != 100 || res.TreatmentN != 100 {
		t.Errorf("sample size mismatch: %d / %d", res.ControlN, res.TreatmentN)
	}

	// 转化率指标
	for i := 0; i < 500; i++ {
		m.RecordConversion("exp1", "control", "click", i < 50)
		m.RecordConversion("exp1", "treatment", "click", i < 150)
	}
	res2, err := m.Analyze("exp1", AnalyzeOptions{
		Metric: "click",
		Kind:   "proportion",
	})
	if err != nil {
		t.Fatalf("analyze proportion: %v", err)
	}
	if res2.Verdict != VerdictWinner {
		t.Errorf("expected winner, got %s (p=%v)", res2.Verdict, res2.PValue)
	}

	metrics := m.ObservedMetrics("exp1")
	if metrics["control"]["continuous:dau"] != 100 {
		t.Errorf("expected 100 dau samples in control, got %v", metrics["control"])
	}
	if metrics["treatment"]["proportion:click"] != 500 {
		t.Errorf("expected 500 click trials in treatment, got %v", metrics["treatment"])
	}
}

func TestABTestManager_AnalyzeUnknownExperiment(t *testing.T) {
	m := NewABTestManager()
	if _, err := m.Analyze("missing", AnalyzeOptions{Metric: "x"}); err == nil {
		t.Fatal("expected error for missing experiment")
	}
}
