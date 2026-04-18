package bench

import (
	"fmt"
	"html"
	"strings"
	"time"
)

// stringBuilder 包装 strings.Builder，避免与 compare.go 重复定义
type stringBuilder = strings.Builder

func newBuilder() *stringBuilder {
	return &stringBuilder{}
}

// HTMLReport 生成 HTML 性能报告
// 包含：摘要卡片 + 详情表格 + 历史趋势图（内嵌 SVG）
func HTMLReport(report *CompareReport, baseline *Baseline) []byte {
	b := newBuilder()
	b.WriteString(htmlHeader)
	b.WriteString("<body><div class=\"container\">")
	b.WriteString("<h1>Engine Benchmark Regression Report</h1>")
	b.WriteString(fmt.Sprintf("<p class=\"meta\">generated at %s</p>",
		time.Now().Format(time.RFC3339)))

	// 摘要
	b.WriteString("<div class=\"summary\">")
	b.WriteString(summaryCard("Total", fmt.Sprintf("%d", report.TotalCount), "total"))
	b.WriteString(summaryCard("Major", fmt.Sprintf("%d", report.MajorCount), "major"))
	b.WriteString(summaryCard("Minor", fmt.Sprintf("%d", report.MinorCount), "minor"))
	b.WriteString(summaryCard("Improved", fmt.Sprintf("%d", report.ImprovedCount), "improved"))
	b.WriteString(summaryCard("New", fmt.Sprintf("%d", report.MissingCount), "new"))
	b.WriteString("</div>")

	// 详情表
	b.WriteString("<h2>Details</h2>")
	b.WriteString("<table class=\"detail\">")
	b.WriteString("<tr><th>Benchmark</th><th>Baseline ns/op</th><th>Current ns/op</th>" +
		"<th>Δ %</th><th>B/op Δ</th><th>allocs Δ</th><th>Status</th><th>Note</th></tr>")
	for _, e := range report.Entries {
		cls := cssForLevel(e.Level)
		baseNs := "—"
		deltaPct := "—"
		if e.Level != RegressionMissing {
			baseNs = fmt.Sprintf("%.2f", e.Baseline.NsPerOp)
			deltaPct = fmt.Sprintf("%+.2f%%", e.NsDeltaPct)
		}
		b.WriteString(fmt.Sprintf(
			"<tr class=\"%s\"><td>%s</td><td>%s</td><td>%.2f</td><td>%s</td>"+
				"<td>%+d</td><td>%+d</td><td>%s</td><td>%s</td></tr>",
			cls, html.EscapeString(e.Name), baseNs, e.Current.NsPerOp, deltaPct,
			e.BytesDelta, e.AllocsDelta, e.Level.String(), html.EscapeString(e.Note),
		))
	}
	b.WriteString("</table>")

	b.WriteString(fmt.Sprintf(
		"<p class=\"totals\">Totals: %d benchmarks | %d major | %d minor | %d improved | %d new</p>",
		report.TotalCount, report.MajorCount, report.MinorCount, report.ImprovedCount, report.MissingCount,
	))

	// 历史趋势（仅列出基线中有 >= 2 条历史的基准）
	if baseline != nil && len(baseline.History) > 0 {
		b.WriteString("<h2>Trends</h2>")
		for _, key := range baseline.SortedKeys() {
			history := baseline.History[key]
			if len(history) < 2 {
				continue
			}
			b.WriteString(fmt.Sprintf("<h3>%s</h3>", html.EscapeString(key)))
			b.WriteString(sparklineSVG(history, baseline.Results[key]))
		}
	}

	b.WriteString("</div></body></html>")
	return []byte(b.String())
}

func summaryCard(label, value, kind string) string {
	return fmt.Sprintf(`<div class="card %s"><span class="label">%s</span><span class="value">%s</span></div>`,
		kind, label, value)
}

func cssForLevel(l RegressionLevel) string {
	switch l {
	case RegressionMajor:
		return "major"
	case RegressionMinor:
		return "minor"
	case RegressionImproved:
		return "improved"
	case RegressionMissing:
		return "new"
	}
	return ""
}

// sparklineSVG 生成简单的 SVG 折线图
func sparklineSVG(history []BenchResult, current BenchResult) string {
	// 合并历史 + 当前
	pts := append([]BenchResult{}, history...)
	pts = append(pts, current)
	if len(pts) < 2 {
		return ""
	}
	const w, h = 600.0, 60.0
	minV, maxV := pts[0].NsPerOp, pts[0].NsPerOp
	for _, p := range pts {
		if p.NsPerOp < minV {
			minV = p.NsPerOp
		}
		if p.NsPerOp > maxV {
			maxV = p.NsPerOp
		}
	}
	span := maxV - minV
	if span == 0 {
		span = 1
	}
	var path strings.Builder
	for i, p := range pts {
		x := float64(i) / float64(len(pts)-1) * w
		y := h - (p.NsPerOp-minV)/span*h
		if i == 0 {
			path.WriteString(fmt.Sprintf("M%.1f,%.1f", x, y))
		} else {
			path.WriteString(fmt.Sprintf(" L%.1f,%.1f", x, y))
		}
	}
	return fmt.Sprintf(`<svg viewBox="0 0 %.0f %.0f" width="%.0f" height="%.0f" class="spark"><path d="%s" fill="none" stroke="#2b8cbe" stroke-width="1.5"/></svg>`,
		w, h, w, h, path.String())
}

const htmlHeader = `<!doctype html><html><head><meta charset="utf-8">
<title>Engine Bench Report</title>
<style>
body { font-family: -apple-system, BlinkMacSystemFont, Segoe UI, sans-serif; margin: 0; background: #f7f7f9; }
.container { max-width: 1100px; margin: 24px auto; padding: 24px; background: white; border-radius: 8px; box-shadow: 0 2px 8px rgba(0,0,0,.06); }
h1 { margin-top: 0; }
.meta { color: #888; font-size: 13px; }
.summary { display: flex; gap: 12px; margin: 18px 0; }
.card { flex: 1; padding: 14px; border-radius: 6px; background: #fafafa; display: flex; flex-direction: column; align-items: center; }
.card .label { font-size: 12px; color: #555; letter-spacing: .5px; text-transform: uppercase; }
.card .value { font-size: 28px; font-weight: 600; margin-top: 4px; }
.card.major .value { color: #e53935; }
.card.minor .value { color: #f29b2c; }
.card.improved .value { color: #2e7d32; }
.card.new .value { color: #1e88e5; }
table.detail { width: 100%; border-collapse: collapse; font-size: 13px; }
table.detail th, table.detail td { padding: 6px 10px; text-align: left; border-bottom: 1px solid #eee; }
table.detail tr.major { background: #fdecea; }
table.detail tr.minor { background: #fff4e0; }
table.detail tr.improved { background: #e7f5ea; }
table.detail tr.new { background: #e3f2fd; }
.spark { background: #fafafa; border: 1px solid #eee; border-radius: 4px; }
h2 { margin-top: 28px; }
h3 { margin-top: 18px; font-size: 14px; font-family: monospace; }
</style></head>`
