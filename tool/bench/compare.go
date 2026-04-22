package bench

import (
	"fmt"
	"sort"
)

// RegressionLevel 回归严重度级别
type RegressionLevel int

const (
	RegressionNone     RegressionLevel = iota // 无变化或在阈值内
	RegressionImproved                        // 优化（变好）
	RegressionMinor                           // 轻度回退
	RegressionMajor                           // 严重回退
	RegressionMissing                         // 基线中缺失
)

// String 字符串表示
func (l RegressionLevel) String() string {
	switch l {
	case RegressionNone:
		return "none"
	case RegressionImproved:
		return "improved"
	case RegressionMinor:
		return "minor"
	case RegressionMajor:
		return "major"
	case RegressionMissing:
		return "missing"
	}
	return "unknown"
}

// DiffEntry 单个基准的对比结果
type DiffEntry struct {
	Name       string
	Baseline   BenchResult
	Current    BenchResult
	NsDelta    float64         // 绝对变化 (ns/op)，current - baseline
	NsDeltaPct float64         // 相对变化百分比
	BytesDelta int64           // B/op 变化
	AllocsDelta int64          // allocs/op 变化
	Level      RegressionLevel // 回归严重度
	Note       string          // 人类可读说明
}

// Thresholds 回归阈值配置
type Thresholds struct {
	MinorPct    float64 // 回退超过此百分比视为 Minor（默认 5）
	MajorPct    float64 // 回退超过此百分比视为 Major（默认 15）
	ImprovePct  float64 // 性能提升超过此百分比视为 Improved（默认 5）
	IgnoreBelow float64 // ns/op 小于此值的基准忽略波动（默认 100ns，避免噪声）
}

// DefaultThresholds 默认阈值
func DefaultThresholds() Thresholds {
	return Thresholds{
		MinorPct:    5.0,
		MajorPct:    15.0,
		ImprovePct:  5.0,
		IgnoreBelow: 100.0,
	}
}

// CompareReport 对比总览
type CompareReport struct {
	Entries      []DiffEntry
	MajorCount   int
	MinorCount   int
	ImprovedCount int
	MissingCount int
	TotalCount   int
}

// HasRegression 是否存在 Major 级别回归
func (r *CompareReport) HasRegression() bool {
	return r.MajorCount > 0
}

// Compare 对比当前结果与基线
// baseline 为 nil 时所有条目标记为 Missing
func Compare(baseline *Baseline, current []BenchResult, th Thresholds) *CompareReport {
	report := &CompareReport{}
	// 按名字排序，输出稳定
	sorted := make([]BenchResult, len(current))
	copy(sorted, current)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Key() < sorted[j].Key() })

	for _, cur := range sorted {
		key := cur.Key()
		entry := DiffEntry{
			Name:    key,
			Current: cur,
		}

		if baseline == nil {
			entry.Level = RegressionMissing
			entry.Note = "no baseline"
			report.MissingCount++
			report.TotalCount++
			report.Entries = append(report.Entries, entry)
			continue
		}

		base, ok := baseline.Results[key]
		if !ok {
			entry.Level = RegressionMissing
			entry.Note = "missing from baseline"
			report.MissingCount++
			report.TotalCount++
			report.Entries = append(report.Entries, entry)
			continue
		}

		entry.Baseline = base
		entry.NsDelta = cur.NsPerOp - base.NsPerOp
		if base.NsPerOp > 0 {
			entry.NsDeltaPct = entry.NsDelta / base.NsPerOp * 100.0
		}
		entry.BytesDelta = cur.BytesPerOp - base.BytesPerOp
		entry.AllocsDelta = cur.AllocsPerOp - base.AllocsPerOp

		// 判定回归级别
		absPct := entry.NsDeltaPct
		if absPct < 0 {
			absPct = -absPct
		}
		switch {
		case base.NsPerOp < th.IgnoreBelow && absPct < 50:
			// 极短基准的小幅波动归类为 none
			entry.Level = RegressionNone
		case entry.NsDeltaPct <= -th.ImprovePct:
			entry.Level = RegressionImproved
			entry.Note = fmt.Sprintf("%.2f%% faster", -entry.NsDeltaPct)
			report.ImprovedCount++
		case entry.NsDeltaPct >= th.MajorPct:
			entry.Level = RegressionMajor
			entry.Note = fmt.Sprintf("%.2f%% slower (major)", entry.NsDeltaPct)
			report.MajorCount++
		case entry.NsDeltaPct >= th.MinorPct:
			entry.Level = RegressionMinor
			entry.Note = fmt.Sprintf("%.2f%% slower (minor)", entry.NsDeltaPct)
			report.MinorCount++
		default:
			entry.Level = RegressionNone
		}

		report.TotalCount++
		report.Entries = append(report.Entries, entry)
	}
	return report
}

// TextSummary 生成适合 CI 终端输出的文本报告
func (r *CompareReport) TextSummary() string {
	var b = newBuilder()
	b.WriteString("Bench Regression Report\n")
	b.WriteString("=======================\n")
	fmtF := "%-50s %12s %12s %10s\n"
	b.WriteString(fmt.Sprintf(fmtF, "Benchmark", "Baseline", "Current", "Delta"))
	for _, e := range r.Entries {
		delta := "--"
		if e.Level != RegressionMissing {
			delta = fmt.Sprintf("%+.2f%%", e.NsDeltaPct)
		}
		baseCol := "--"
		if e.Level != RegressionMissing {
			baseCol = fmt.Sprintf("%.1f ns", e.Baseline.NsPerOp)
		}
		marker := ""
		switch e.Level {
		case RegressionMajor:
			marker = " [MAJOR]"
		case RegressionMinor:
			marker = " [minor]"
		case RegressionImproved:
			marker = " [faster]"
		case RegressionMissing:
			marker = " [new]"
		}
		b.WriteString(fmt.Sprintf(fmtF, e.Name+marker, baseCol,
			fmt.Sprintf("%.1f ns", e.Current.NsPerOp), delta))
	}
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("Totals: %d benchmarks | %d major | %d minor | %d improved | %d new\n",
		r.TotalCount, r.MajorCount, r.MinorCount, r.ImprovedCount, r.MissingCount))
	return b.String()
}
