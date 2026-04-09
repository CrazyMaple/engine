package dashboard

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"sort"
	"strings"
	"time"
)

// --- 运行时数据采集辅助 ---

// memStatsCompact 紧凑的运行时统计（供 live_push 和 export 复用）
type memStatsCompact struct {
	GoVersion     string
	NumGoroutine  int
	NumCPU        int
	Alloc         uint64
	TotalAlloc    uint64
	Sys           uint64
	HeapAlloc     uint64
	HeapInuse     uint64
	StackInuse    uint64
	NumGC         uint32
	LastGCPauseMs float64
	PauseTotalMs  float64
	GCCPUPercent  float64
}

// readMemStats 读取运行时内存统计
func readMemStats(mc *memStatsCompact) {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	mc.GoVersion = runtime.Version()
	mc.NumGoroutine = runtime.NumGoroutine()
	mc.NumCPU = runtime.NumCPU()
	mc.Alloc = mem.Alloc
	mc.TotalAlloc = mem.TotalAlloc
	mc.Sys = mem.Sys
	mc.HeapAlloc = mem.HeapAlloc
	mc.HeapInuse = mem.HeapInuse
	mc.StackInuse = mem.StackInuse
	mc.NumGC = mem.NumGC

	if mem.NumGC > 0 {
		mc.LastGCPauseMs = float64(mem.PauseNs[(mem.NumGC+255)%256]) / 1e6
	}
	mc.PauseTotalMs = float64(mem.PauseTotalNs) / 1e6
	mc.GCCPUPercent = mem.GCCPUFraction * 100
}

// --- 运行报告导出 ---

// RuntimeReport 完整运行报告结构
type RuntimeReport struct {
	GeneratedAt    string                 `json:"generated_at"`
	System         systemInfo             `json:"system"`
	Runtime        runtimeInfo            `json:"runtime"`
	Metrics        interface{}            `json:"metrics,omitempty"`
	MetricsHistory []MetricsPoint         `json:"metrics_history,omitempty"`
	Cluster        *clusterInfo           `json:"cluster,omitempty"`
	HotActors      interface{}            `json:"hot_actors,omitempty"`
	DeadLetters    *deadLetterResponse    `json:"dead_letters,omitempty"`
	AuditLog       interface{}            `json:"audit_log,omitempty"`
	Extra          map[string]interface{} `json:"extra,omitempty"`
}

// ExportReport 生成运行报告
func (h *handlers) ExportReport() *RuntimeReport {
	report := &RuntimeReport{
		GeneratedAt: time.Now().Format(time.RFC3339),
	}

	// 系统信息
	if h.config.System != nil {
		report.System = systemInfo{
			Address:    h.config.System.Address,
			ActorCount: h.config.System.ProcessRegistry.Count(),
			GoVersion:  runtime.Version(),
		}
	}

	// 运行时信息
	h.fillRuntimeInfo(&report.Runtime)

	// 指标快照
	if h.config.Metrics != nil {
		report.Metrics = h.config.Metrics.Snapshot()
	}

	// 流量历史
	if h.config.MetricsHistory != nil {
		report.MetricsHistory = h.config.MetricsHistory.GetHistory()
	}

	// 集群信息
	if h.config.Cluster != nil {
		self := h.config.Cluster.Self()
		members := h.config.Cluster.Members()
		ci := &clusterInfo{
			Name: h.config.Cluster.Config().ClusterName,
			Self: &memberInfo{
				Address: self.Address,
				Id:      self.Id,
				Status:  self.Status.String(),
				Kinds:   self.Kinds,
			},
			Members: make([]memberInfo, 0, len(members)),
			Kinds:   h.config.Cluster.Config().Kinds,
		}
		for _, m := range members {
			ci.Members = append(ci.Members, memberInfo{
				Address:  m.Address,
				Id:       m.Id,
				Status:   m.Status.String(),
				Kinds:    m.Kinds,
				LastSeen: m.LastSeen.Format(time.RFC3339),
			})
		}
		report.Cluster = ci
	}

	// 热点 Actor
	if h.config.HotTracker != nil {
		report.HotActors = h.config.HotTracker.TopN(50)
	}

	// 死信
	if h.config.DeadLetterMonitor != nil {
		report.DeadLetters = &deadLetterResponse{
			Stats:   h.config.DeadLetterMonitor.Stats(),
			Records: h.config.DeadLetterMonitor.RecentRecords(100),
		}
	}

	// 审计日志
	if h.config.AuditLog != nil {
		report.AuditLog = h.config.AuditLog.Recent(200)
	}

	return report
}

// handleReportJSON GET /api/report?format=json - 导出 JSON 运行报告
func (h *handlers) handleReportJSON(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	report := h.ExportReport()

	w.Header().Set("Content-Disposition",
		fmt.Sprintf("attachment; filename=report-%s.json", time.Now().Format("20060102-150405")))
	writeJSON(w, report)
}

// handleReportCSV GET /api/report.csv - 导出 CSV 运行报告（仅热点 Actor 和指标）
func (h *handlers) handleReportCSV(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf("attachment; filename=report-%s.csv", time.Now().Format("20060102-150405")))

	writer := csv.NewWriter(w)
	defer writer.Flush()

	writeReportCSV(writer, h)
}

// writeReportCSV 生成 CSV 报告内容
func writeReportCSV(w *csv.Writer, h *handlers) {
	// 章节 1: 基础信息
	w.Write([]string{"# System"})
	w.Write([]string{"Key", "Value"})
	if h.config.System != nil {
		w.Write([]string{"address", h.config.System.Address})
		w.Write([]string{"actor_count", fmt.Sprintf("%d", h.config.System.ProcessRegistry.Count())})
	}
	w.Write([]string{"go_version", runtime.Version()})
	w.Write([]string{"generated_at", time.Now().Format(time.RFC3339)})
	w.Write([]string{})

	// 章节 2: 热点 Actor
	if h.config.HotTracker != nil {
		w.Write([]string{"# Hot Actors"})
		w.Write([]string{"PID", "MsgCount", "AvgLatencyNs"})
		stats := h.config.HotTracker.TopN(50)
		for _, s := range stats {
			w.Write([]string{
				s.PID,
				fmt.Sprintf("%d", s.MsgCount),
				fmt.Sprintf("%d", s.AvgLatNs),
			})
		}
		w.Write([]string{})
	}

	// 章节 3: 死信统计
	if h.config.DeadLetterMonitor != nil {
		w.Write([]string{"# Dead Letters"})
		w.Write([]string{"MsgType", "Count"})
		stats := h.config.DeadLetterMonitor.Stats()
		type entry struct {
			K string
			V int64
		}
		entries := make([]entry, 0, len(stats.TypeCounts))
		for k, v := range stats.TypeCounts {
			entries = append(entries, entry{k, v})
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].V > entries[j].V })
		for _, e := range entries {
			w.Write([]string{e.K, fmt.Sprintf("%d", e.V)})
		}
	}
}

// --- Actor 消息热力图 ---

// HeatmapCell 热力图单元（Actor 作为行，时间片为列，值为消息数）
type HeatmapCell struct {
	PID      string `json:"pid"`
	TimeIdx  int    `json:"time_idx"`
	MsgCount int64  `json:"msg_count"`
}

// Heatmap 热力图数据
type Heatmap struct {
	TimePoints []int64       `json:"time_points"` // Unix 毫秒
	Actors     []string      `json:"actors"`
	Cells      []HeatmapCell `json:"cells"`
}

// handleHeatmap GET /api/actors/heatmap - 生成 Actor 消息热力图
func (h *handlers) handleHeatmap(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.config.HotTracker == nil {
		writeError(w, http.StatusNotFound, "hot tracker not configured")
		return
	}

	// 基于当前 HotActorTracker 的快照生成单时间片热力图
	stats := h.config.HotTracker.TopN(30)

	now := time.Now().UnixMilli()
	heatmap := Heatmap{
		TimePoints: []int64{now},
		Actors:     make([]string, 0, len(stats)),
		Cells:      make([]HeatmapCell, 0, len(stats)),
	}

	for _, s := range stats {
		heatmap.Actors = append(heatmap.Actors, s.PID)
		heatmap.Cells = append(heatmap.Cells, HeatmapCell{
			PID:      s.PID,
			TimeIdx:  0,
			MsgCount: s.MsgCount,
		})
	}

	writeJSON(w, heatmap)
}

// --- 辅助：流式写入 JSON 到 Writer ---

func writeJSONTo(w io.Writer, v interface{}) error {
	enc := json.NewEncoder(w)
	return enc.Encode(v)
}

// parseTopics 解析逗号分隔的主题列表
func parseTopics(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}
