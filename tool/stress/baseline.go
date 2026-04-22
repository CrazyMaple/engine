package stress

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"
)

// PerfBaseline 性能基线，用于 CI 阶段对比基准结果
// 例如保存 P50/P99 延迟、TPS 等关键指标
type PerfBaseline struct {
	Name      string             `json:"name"`
	UpdatedAt time.Time          `json:"updated_at"`
	Metrics   map[string]float64 `json:"metrics"`
}

// PerfBaselines 多版本基线集合
type PerfBaselines struct {
	Path  string                       `json:"-"`
	Items map[string]*PerfBaseline         `json:"items"`
	Hist  map[string][]baselineHistory `json:"history,omitempty"`
	mu    sync.Mutex
}

type baselineHistory struct {
	UpdatedAt time.Time          `json:"updated_at"`
	Metrics   map[string]float64 `json:"metrics"`
}

// LoadPerfBaselines 从 JSON 文件加载基线（不存在则返回空集合）
func LoadPerfBaselines(path string) (*PerfBaselines, error) {
	b := &PerfBaselines{
		Path:  path,
		Items: make(map[string]*PerfBaseline),
		Hist:  make(map[string][]baselineHistory),
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return b, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(data, b); err != nil {
		return nil, err
	}
	if b.Items == nil {
		b.Items = make(map[string]*PerfBaseline)
	}
	if b.Hist == nil {
		b.Hist = make(map[string][]baselineHistory)
	}
	return b, nil
}

// Save 写回 JSON 文件
func (bs *PerfBaselines) Save() error {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	if bs.Path == "" {
		return fmt.Errorf("baselines path empty")
	}
	data, err := json.MarshalIndent(bs, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(bs.Path, data, 0644)
}

// Update 更新某个基准的基线值，老值进入历史
func (bs *PerfBaselines) Update(name string, metrics map[string]float64) {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	if old, ok := bs.Items[name]; ok {
		bs.Hist[name] = append(bs.Hist[name], baselineHistory{
			UpdatedAt: old.UpdatedAt,
			Metrics:   cloneMetrics(old.Metrics),
		})
		if len(bs.Hist[name]) > 50 {
			bs.Hist[name] = bs.Hist[name][len(bs.Hist[name])-50:]
		}
	}
	bs.Items[name] = &PerfBaseline{
		Name:      name,
		UpdatedAt: time.Now(),
		Metrics:   cloneMetrics(metrics),
	}
}

// Diff 对比当前结果与基线，返回每个指标的相对变化（百分比）
// 正值表示上升，负值表示下降。基线缺失则忽略对应指标
func (bs *PerfBaselines) Diff(name string, current map[string]float64) map[string]float64 {
	bs.mu.Lock()
	base := bs.Items[name]
	bs.mu.Unlock()
	if base == nil {
		return nil
	}
	out := make(map[string]float64, len(current))
	for k, v := range current {
		if old, ok := base.Metrics[k]; ok && old != 0 {
			out[k] = (v - old) / old * 100.0
		}
	}
	return out
}

// Regression 判定是否性能回归（任意指标变化超过阈值）
// thresholds: 指标名 -> 允许的最大相对变化百分比（如 5.0 表示允许 5% 上下浮动）
// 返回回归报告：{指标名: 实际变化%}
func (bs *PerfBaselines) Regression(name string, current, thresholds map[string]float64) map[string]float64 {
	diff := bs.Diff(name, current)
	if diff == nil {
		return nil
	}
	report := make(map[string]float64)
	for metric, change := range diff {
		threshold, ok := thresholds[metric]
		if !ok {
			continue
		}
		abs := change
		if abs < 0 {
			abs = -abs
		}
		if abs > threshold {
			report[metric] = change
		}
	}
	return report
}

// Names 列出当前已记录的基线名称（按字典序）
func (bs *PerfBaselines) Names() []string {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	out := make([]string, 0, len(bs.Items))
	for k := range bs.Items {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func cloneMetrics(m map[string]float64) map[string]float64 {
	dst := make(map[string]float64, len(m))
	for k, v := range m {
		dst[k] = v
	}
	return dst
}
