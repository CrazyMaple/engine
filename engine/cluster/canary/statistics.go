package canary

import (
	"math"
)

// Verdict A/B 实验结论
type Verdict string

const (
	// VerdictWinner 有显著差异，给出赢家
	VerdictWinner Verdict = "winner"
	// VerdictNoSignificant 无显著差异
	VerdictNoSignificant Verdict = "no_significant"
	// VerdictUnderpowered 样本不足，无法得出结论
	VerdictUnderpowered Verdict = "underpowered"
)

// 默认显著性阈值与最小样本量
const (
	DefaultSignificanceLevel = 0.05
	DefaultMinSampleSize     = 30
)

// Sample 连续型样本统计（用于 t 检验）
type Sample struct {
	N       int     // 样本量
	Mean    float64 // 均值
	Variance float64 // 样本方差（n-1）
}

// NewSample 从原始数据构造 Sample（Welford 算法，数值稳定）
func NewSample(values []float64) Sample {
	var n int
	var mean, m2 float64
	for _, x := range values {
		n++
		delta := x - mean
		mean += delta / float64(n)
		m2 += delta * (x - mean)
	}
	var variance float64
	if n > 1 {
		variance = m2 / float64(n-1)
	}
	return Sample{N: n, Mean: mean, Variance: variance}
}

// Proportion 二项分布样本（用于比例检验）
type Proportion struct {
	N         int // 总样本
	Successes int // 成功数
}

// Rate 返回成功率
func (p Proportion) Rate() float64 {
	if p.N == 0 {
		return 0
	}
	return float64(p.Successes) / float64(p.N)
}

// TTestResult Welch's t 检验结果
type TTestResult struct {
	TStatistic float64 // t 统计量
	DF         float64 // 自由度（Welch-Satterthwaite）
	PValue     float64 // 双尾 p 值
	MeanDiff   float64 // 均值差 treatment - control
}

// WelchTTest 双样本 Welch 不等方差 t 检验
// control 为对照组，treatment 为实验组。
// 若任一样本量 < 2 或方差为 0，返回 PValue=1 表示无法拒绝原假设。
func WelchTTest(control, treatment Sample) TTestResult {
	res := TTestResult{MeanDiff: treatment.Mean - control.Mean, PValue: 1.0}
	if control.N < 2 || treatment.N < 2 {
		return res
	}
	v1 := control.Variance / float64(control.N)
	v2 := treatment.Variance / float64(treatment.N)
	se := math.Sqrt(v1 + v2)
	if se == 0 || math.IsNaN(se) {
		return res
	}
	res.TStatistic = res.MeanDiff / se

	// Welch-Satterthwaite 自由度
	num := (v1 + v2) * (v1 + v2)
	den := (v1*v1)/float64(control.N-1) + (v2*v2)/float64(treatment.N-1)
	if den == 0 {
		return res
	}
	res.DF = num / den
	res.PValue = twoSidedTPValue(res.TStatistic, res.DF)
	return res
}

// ProportionTestResult 比例 z 检验结果
type ProportionTestResult struct {
	ZStatistic float64 // z 统计量
	PValue     float64 // 双尾 p 值
	RateDiff   float64 // 比例差 treatment - control
}

// ProportionZTest 两独立样本比例 z 检验（合并方差）
func ProportionZTest(control, treatment Proportion) ProportionTestResult {
	res := ProportionTestResult{
		RateDiff: treatment.Rate() - control.Rate(),
		PValue:   1.0,
	}
	if control.N == 0 || treatment.N == 0 {
		return res
	}
	pPool := float64(control.Successes+treatment.Successes) /
		float64(control.N+treatment.N)
	se := math.Sqrt(pPool * (1 - pPool) * (1.0/float64(control.N) + 1.0/float64(treatment.N)))
	if se == 0 {
		return res
	}
	res.ZStatistic = res.RateDiff / se
	res.PValue = twoSidedNormalPValue(res.ZStatistic)
	return res
}

// ChiSquareResult 皮尔逊卡方独立性检验结果（2x2 列联表）
type ChiSquareResult struct {
	ChiSquare float64
	DF        int
	PValue    float64
}

// ChiSquareTest 2x2 列联表皮尔逊卡方检验
// 表格：
//
//	            Success  Failure
//	 Control    a        b
//	 Treatment  c        d
func ChiSquareTest(control, treatment Proportion) ChiSquareResult {
	res := ChiSquareResult{DF: 1, PValue: 1.0}
	if control.N == 0 || treatment.N == 0 {
		return res
	}
	a := float64(control.Successes)
	b := float64(control.N - control.Successes)
	c := float64(treatment.Successes)
	d := float64(treatment.N - treatment.Successes)
	n := a + b + c + d
	row1 := a + b
	row2 := c + d
	col1 := a + c
	col2 := b + d
	if row1 == 0 || row2 == 0 || col1 == 0 || col2 == 0 {
		return res
	}
	// 期望频数
	e11 := row1 * col1 / n
	e12 := row1 * col2 / n
	e21 := row2 * col1 / n
	e22 := row2 * col2 / n
	chi := sqDiff(a, e11)/e11 + sqDiff(b, e12)/e12 +
		sqDiff(c, e21)/e21 + sqDiff(d, e22)/e22
	res.ChiSquare = chi
	res.PValue = chiSquarePValue(chi, 1)
	return res
}

// WilsonInterval Wilson 评分区间（比例 95% / 可配置置信度）
// z 为目标置信度对应的标准正态分位数（95% → 1.96, 99% → 2.576）
func WilsonInterval(successes, total int, z float64) (low, high float64) {
	if total == 0 {
		return 0, 0
	}
	n := float64(total)
	p := float64(successes) / n
	z2 := z * z
	denom := 1 + z2/n
	center := (p + z2/(2*n)) / denom
	margin := z * math.Sqrt((p*(1-p)/n)+z2/(4*n*n)) / denom
	return center - margin, center + margin
}

// StandardNormalQuantile 常用双尾置信度对应的 z 值
func StandardNormalQuantile(confidence float64) float64 {
	switch {
	case confidence >= 0.99:
		return 2.5758
	case confidence >= 0.98:
		return 2.3263
	case confidence >= 0.95:
		return 1.9600
	case confidence >= 0.90:
		return 1.6449
	default:
		return 1.9600
	}
}

// AnalyzeContinuous 基于 Welch's t-test + 均值差 Wilson 近似区间的综合分析
// alpha 为显著性水平（如 0.05）。
// minSample 为每组最小样本量，任一组不足则返回 Underpowered。
func AnalyzeContinuous(control, treatment Sample, alpha float64, minSample int) ExperimentAnalysis {
	if alpha <= 0 {
		alpha = DefaultSignificanceLevel
	}
	if minSample <= 0 {
		minSample = DefaultMinSampleSize
	}
	tt := WelchTTest(control, treatment)
	z := StandardNormalQuantile(1 - alpha)
	// 均值差的 95%（或 1-alpha）近似置信区间
	se := math.Sqrt(control.Variance/math.Max(float64(control.N), 1) +
		treatment.Variance/math.Max(float64(treatment.N), 1))
	low := tt.MeanDiff - z*se
	high := tt.MeanDiff + z*se

	a := ExperimentAnalysis{
		ControlN:           control.N,
		TreatmentN:         treatment.N,
		ControlMean:        control.Mean,
		TreatmentMean:      treatment.Mean,
		Diff:               tt.MeanDiff,
		PValue:             tt.PValue,
		SignificanceLevel:  alpha,
		ConfidenceInterval: [2]float64{low, high},
		Verdict:            decideVerdict(control.N, treatment.N, minSample, tt.PValue, alpha, tt.MeanDiff),
	}
	return a
}

// AnalyzeProportion 基于 z 检验 + Wilson 区间的比例分析
func AnalyzeProportion(control, treatment Proportion, alpha float64, minSample int) ExperimentAnalysis {
	if alpha <= 0 {
		alpha = DefaultSignificanceLevel
	}
	if minSample <= 0 {
		minSample = DefaultMinSampleSize
	}
	z := StandardNormalQuantile(1 - alpha)
	pt := ProportionZTest(control, treatment)
	// 使用 treatment 与 control 的 Wilson 区间差作为近似区间
	tlo, thi := WilsonInterval(treatment.Successes, treatment.N, z)
	clo, chi := WilsonInterval(control.Successes, control.N, z)
	low := tlo - chi
	high := thi - clo
	return ExperimentAnalysis{
		ControlN:           control.N,
		TreatmentN:         treatment.N,
		ControlMean:        control.Rate(),
		TreatmentMean:      treatment.Rate(),
		Diff:               pt.RateDiff,
		PValue:             pt.PValue,
		SignificanceLevel:  alpha,
		ConfidenceInterval: [2]float64{low, high},
		Verdict:            decideVerdict(control.N, treatment.N, minSample, pt.PValue, alpha, pt.RateDiff),
	}
}

// ExperimentAnalysis 统一的实验分析结果
type ExperimentAnalysis struct {
	ControlN           int        `json:"controlN"`
	TreatmentN         int        `json:"treatmentN"`
	ControlMean        float64    `json:"controlMean"`
	TreatmentMean      float64    `json:"treatmentMean"`
	Diff               float64    `json:"diff"`
	PValue             float64    `json:"pValue"`
	SignificanceLevel  float64    `json:"significanceLevel"`
	ConfidenceInterval [2]float64 `json:"confidenceInterval"`
	Verdict            Verdict    `json:"verdict"`
}

func decideVerdict(nc, nt, minSample int, pValue, alpha, diff float64) Verdict {
	if nc < minSample || nt < minSample {
		return VerdictUnderpowered
	}
	if math.IsNaN(pValue) {
		return VerdictUnderpowered
	}
	if pValue < alpha && diff != 0 {
		return VerdictWinner
	}
	return VerdictNoSignificant
}

// ---- 概率分布辅助函数（纯 Go，无外部依赖） ----

// sqDiff (x-y)^2
func sqDiff(x, y float64) float64 {
	d := x - y
	return d * d
}

// erf 误差函数 Abramowitz & Stegun 7.1.26 近似
func erf(x float64) float64 {
	sign := 1.0
	if x < 0 {
		sign = -1
		x = -x
	}
	const (
		a1 = 0.254829592
		a2 = -0.284496736
		a3 = 1.421413741
		a4 = -1.453152027
		a5 = 1.061405429
		p  = 0.3275911
	)
	t := 1.0 / (1.0 + p*x)
	y := 1.0 - (((((a5*t+a4)*t)+a3)*t+a2)*t+a1)*t*math.Exp(-x*x)
	return sign * y
}

// stdNormalCDF 标准正态累积分布函数
func stdNormalCDF(z float64) float64 {
	return 0.5 * (1 + erf(z/math.Sqrt2))
}

// twoSidedNormalPValue 标准正态双尾 p 值
func twoSidedNormalPValue(z float64) float64 {
	if math.IsNaN(z) {
		return 1.0
	}
	return 2 * (1 - stdNormalCDF(math.Abs(z)))
}

// twoSidedTPValue Student's t 分布双尾 p 值
// 基于不完全贝塔函数 I(x; a, b)：
//
//	P(|T| > t) = I(df/(df+t^2); df/2, 1/2)
func twoSidedTPValue(t, df float64) float64 {
	if df <= 0 || math.IsNaN(t) {
		return 1.0
	}
	x := df / (df + t*t)
	return regularizedIncompleteBeta(x, df/2, 0.5)
}

// chiSquarePValue 卡方分布上尾 p 值 P(X^2 > x) = 1 - gammaP(df/2, x/2)
func chiSquarePValue(x float64, df int) float64 {
	if x <= 0 || df <= 0 {
		return 1.0
	}
	return 1 - regularizedGammaP(float64(df)/2, x/2)
}

// lnGamma 对数伽马函数 Lanczos 近似
func lnGamma(x float64) float64 {
	// Lanczos 近似 g=7, n=9
	cof := []float64{
		0.99999999999980993,
		676.5203681218851,
		-1259.1392167224028,
		771.32342877765313,
		-176.61502916214059,
		12.507343278686905,
		-0.13857109526572012,
		9.9843695780195716e-6,
		1.5056327351493116e-7,
	}
	if x < 0.5 {
		return math.Log(math.Pi/math.Sin(math.Pi*x)) - lnGamma(1-x)
	}
	x -= 1
	a := cof[0]
	for i := 1; i < len(cof); i++ {
		a += cof[i] / (x + float64(i))
	}
	t := x + float64(len(cof)) - 1.5
	return 0.5*math.Log(2*math.Pi) + (x+0.5)*math.Log(t) - t + math.Log(a)
}

// regularizedIncompleteBeta 正则化不完全贝塔函数 I_x(a, b)
// 使用连分数展开（Numerical Recipes 6.4）
func regularizedIncompleteBeta(x, a, b float64) float64 {
	if x <= 0 {
		return 0
	}
	if x >= 1 {
		return 1
	}
	bt := math.Exp(lnGamma(a+b) - lnGamma(a) - lnGamma(b) +
		a*math.Log(x) + b*math.Log(1-x))
	if x < (a+1)/(a+b+2) {
		return bt * betacf(x, a, b) / a
	}
	return 1 - bt*betacf(1-x, b, a)/b
}

// betacf 连分数求贝塔函数，Numerical Recipes 算法
func betacf(x, a, b float64) float64 {
	const (
		maxIter = 200
		eps     = 3e-14
		fpmin   = 1e-300
	)
	qab := a + b
	qap := a + 1
	qam := a - 1
	c := 1.0
	d := 1.0 - qab*x/qap
	if math.Abs(d) < fpmin {
		d = fpmin
	}
	d = 1.0 / d
	h := d
	for m := 1; m <= maxIter; m++ {
		mf := float64(m)
		m2 := 2 * mf
		aa := mf * (b - mf) * x / ((qam + m2) * (a + m2))
		d = 1 + aa*d
		if math.Abs(d) < fpmin {
			d = fpmin
		}
		c = 1 + aa/c
		if math.Abs(c) < fpmin {
			c = fpmin
		}
		d = 1 / d
		h *= d * c
		aa = -(a + mf) * (qab + mf) * x / ((a + m2) * (qap + m2))
		d = 1 + aa*d
		if math.Abs(d) < fpmin {
			d = fpmin
		}
		c = 1 + aa/c
		if math.Abs(c) < fpmin {
			c = fpmin
		}
		d = 1 / d
		del := d * c
		h *= del
		if math.Abs(del-1) < eps {
			break
		}
	}
	return h
}

// regularizedGammaP 正则化下不完全伽马函数 P(a, x)
func regularizedGammaP(a, x float64) float64 {
	if x <= 0 {
		return 0
	}
	if x < a+1 {
		return gammaSeries(a, x)
	}
	return 1 - gammaCF(a, x)
}

// gammaSeries 序列展开求 P(a, x)，x < a+1 收敛较快
func gammaSeries(a, x float64) float64 {
	const (
		maxIter = 200
		eps     = 3e-14
	)
	gln := lnGamma(a)
	ap := a
	sum := 1.0 / a
	del := sum
	for i := 0; i < maxIter; i++ {
		ap += 1
		del *= x / ap
		sum += del
		if math.Abs(del) < math.Abs(sum)*eps {
			break
		}
	}
	return sum * math.Exp(-x+a*math.Log(x)-gln)
}

// gammaCF 连分数求 Q(a, x) = 1 - P(a, x)，x >= a+1 收敛较快
func gammaCF(a, x float64) float64 {
	const (
		maxIter = 200
		eps     = 3e-14
		fpmin   = 1e-300
	)
	gln := lnGamma(a)
	b := x + 1 - a
	c := 1.0 / fpmin
	d := 1.0 / b
	h := d
	for i := 1; i <= maxIter; i++ {
		fi := float64(i)
		an := -fi * (fi - a)
		b += 2
		d = an*d + b
		if math.Abs(d) < fpmin {
			d = fpmin
		}
		c = b + an/c
		if math.Abs(c) < fpmin {
			c = fpmin
		}
		d = 1 / d
		del := d * c
		h *= del
		if math.Abs(del-1) < eps {
			break
		}
	}
	return math.Exp(-x+a*math.Log(x)-gln) * h
}
