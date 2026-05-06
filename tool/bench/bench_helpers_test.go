package bench

import "testing"

// 子基准命名通用 helper：把 size 标记成 "{prefix}-{N}B" 或 "{prefix}-{N}KB"。
// 解析侧据此把基准切分为可与基线对齐的 Tag。
func sizeTag(prefix string, size int) string {
	switch {
	case size < 1024:
		return prefix + "-" + itoa(size) + "B"
	default:
		return prefix + "-" + itoa(size/1024) + "KB"
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := make([]byte, 0, 8)
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	return string(buf)
}

// reportPercentiles 把 LatencyRecorder 的 P50 / P95 / P99 上报为 b 的自定义指标。
// 解析侧据此与基线做尾延迟对比。
func reportPercentiles(b *testing.B, rec *LatencyRecorder) {
	b.Helper()
	p := rec.Percentiles()
	b.ReportMetric(p.P50, "p50-ns/op")
	b.ReportMetric(p.P95, "p95-ns/op")
	b.ReportMetric(p.P99, "p99-ns/op")
}
