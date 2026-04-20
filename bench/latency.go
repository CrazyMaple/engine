package bench

import (
	"math"
	"sort"
	"sync"
)

// LatencyRecorder 延迟采样器：在基准内部记录每次操作延迟，统计 P50/P95/P99
//
// 使用方式：
//
//	rec := NewLatencyRecorder(b.N)
//	for i := 0; i < b.N; i++ {
//	    t0 := time.Now()
//	    doWork()
//	    rec.Add(time.Since(t0).Nanoseconds())
//	}
//	p := rec.Percentiles()
//	b.ReportMetric(p.P99, "p99-ns/op")
type LatencyRecorder struct {
	mu      sync.Mutex
	samples []int64
}

// NewLatencyRecorder 创建延迟采样器，预分配 hint 个桶。
func NewLatencyRecorder(hint int) *LatencyRecorder {
	if hint <= 0 {
		hint = 1024
	}
	return &LatencyRecorder{samples: make([]int64, 0, hint)}
}

// Add 记录一次延迟（纳秒）。
func (r *LatencyRecorder) Add(ns int64) {
	r.mu.Lock()
	r.samples = append(r.samples, ns)
	r.mu.Unlock()
}

// Count 返回已记录的样本数。
func (r *LatencyRecorder) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.samples)
}

// Percentiles 返回 P50/P95/P99 百分位（ns）。无样本时返回零值。
func (r *LatencyRecorder) Percentiles() LatencyPercentiles {
	r.mu.Lock()
	n := len(r.samples)
	if n == 0 {
		r.mu.Unlock()
		return LatencyPercentiles{}
	}
	cp := make([]int64, n)
	copy(cp, r.samples)
	r.mu.Unlock()

	sort.Slice(cp, func(i, j int) bool { return cp[i] < cp[j] })
	return LatencyPercentiles{
		P50:  percentile(cp, 0.50),
		P95:  percentile(cp, 0.95),
		P99:  percentile(cp, 0.99),
		Max:  float64(cp[n-1]),
		Mean: meanFloat(cp),
	}
}

// LatencyPercentiles 百分位统计
type LatencyPercentiles struct {
	P50  float64
	P95  float64
	P99  float64
	Max  float64
	Mean float64
}

// percentile 线性插值计算百分位（输入必须已排序）
func percentile(sorted []int64, p float64) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if n == 1 {
		return float64(sorted[0])
	}
	idx := p * float64(n-1)
	lo := int(math.Floor(idx))
	hi := int(math.Ceil(idx))
	if lo == hi {
		return float64(sorted[lo])
	}
	frac := idx - float64(lo)
	return float64(sorted[lo])*(1-frac) + float64(sorted[hi])*frac
}

func meanFloat(samples []int64) float64 {
	if len(samples) == 0 {
		return 0
	}
	var sum int64
	for _, v := range samples {
		sum += v
	}
	return float64(sum) / float64(len(samples))
}

// ApplyTo 将百分位写入 BenchResult，便于与基线对比
func (p LatencyPercentiles) ApplyTo(r *BenchResult) {
	r.P50Ns = p.P50
	r.P95Ns = p.P95
	r.P99Ns = p.P99
}
