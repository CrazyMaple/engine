package middleware

import (
	"fmt"
	"io"
	"sort"
	"sync"
	"sync/atomic"
)

// GaugeValue 仪表盘值，可带标签
type GaugeValue struct {
	Labels map[string]string
	Value  float64
}

// gaugeDesc 注册的 Gauge 描述
type gaugeDesc struct {
	name string
	help string
	fn   func() []GaugeValue
}

// counterKey 带标签的计数器键
type counterKey struct {
	name   string
	labels string // 排序后的 label=value 拼接
}

// counterFamily 一个指标名下的所有标签组合
type counterFamily struct {
	help     string
	counters map[string]*int64 // labels string -> counter
	mu       sync.RWMutex
}

// MetricsRegistry 指标注册中心，支持 Counter 和 Gauge 两种类型
// 自行输出 Prometheus text exposition 格式，零外部依赖
type MetricsRegistry struct {
	mu             sync.RWMutex
	counterFamilys map[string]*counterFamily // name -> family
	gauges         []gaugeDesc
}

// NewMetricsRegistry 创建指标注册中心
func NewMetricsRegistry() *MetricsRegistry {
	return &MetricsRegistry{
		counterFamilys: make(map[string]*counterFamily),
	}
}

// IncCounter 递增一个带标签的计数器
func (r *MetricsRegistry) IncCounter(name, help string, labels map[string]string, delta int64) {
	family := r.getOrCreateFamily(name, help)
	key := labelsToString(labels)

	family.mu.RLock()
	counter, ok := family.counters[key]
	family.mu.RUnlock()

	if ok {
		atomic.AddInt64(counter, delta)
		return
	}

	family.mu.Lock()
	if counter, ok = family.counters[key]; ok {
		family.mu.Unlock()
		atomic.AddInt64(counter, delta)
		return
	}
	v := delta
	family.counters[key] = &v
	family.mu.Unlock()
}

// RegisterGauge 注册一个 Gauge 指标，在采集时调用 fn 获取当前值
func (r *MetricsRegistry) RegisterGauge(name, help string, fn func() []GaugeValue) {
	r.mu.Lock()
	r.gauges = append(r.gauges, gaugeDesc{name: name, help: help, fn: fn})
	r.mu.Unlock()
}

// WritePrometheus 输出完整的 Prometheus text exposition 格式
func (r *MetricsRegistry) WritePrometheus(w io.Writer) {
	// 输出 counter 指标
	r.mu.RLock()
	names := make([]string, 0, len(r.counterFamilys))
	for name := range r.counterFamilys {
		names = append(names, name)
	}
	gauges := make([]gaugeDesc, len(r.gauges))
	copy(gauges, r.gauges)
	r.mu.RUnlock()

	sort.Strings(names)

	for _, name := range names {
		r.mu.RLock()
		family := r.counterFamilys[name]
		r.mu.RUnlock()

		family.mu.RLock()
		fmt.Fprintf(w, "# HELP %s %s\n", name, family.help)
		fmt.Fprintf(w, "# TYPE %s counter\n", name)

		keys := make([]string, 0, len(family.counters))
		for k := range family.counters {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		for _, key := range keys {
			val := atomic.LoadInt64(family.counters[key])
			if key == "" {
				fmt.Fprintf(w, "%s %d\n", name, val)
			} else {
				fmt.Fprintf(w, "%s{%s} %d\n", name, key, val)
			}
		}
		family.mu.RUnlock()
	}

	// 输出 gauge 指标
	for _, g := range gauges {
		values := g.fn()
		if len(values) == 0 {
			continue
		}
		fmt.Fprintf(w, "# HELP %s %s\n", g.name, g.help)
		fmt.Fprintf(w, "# TYPE %s gauge\n", g.name)
		for _, v := range values {
			labelStr := labelsToString(v.Labels)
			if labelStr == "" {
				fmt.Fprintf(w, "%s %g\n", g.name, v.Value)
			} else {
				fmt.Fprintf(w, "%s{%s} %g\n", g.name, labelStr, v.Value)
			}
		}
	}
}

func (r *MetricsRegistry) getOrCreateFamily(name, help string) *counterFamily {
	r.mu.RLock()
	family, ok := r.counterFamilys[name]
	r.mu.RUnlock()

	if ok {
		return family
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if family, ok = r.counterFamilys[name]; ok {
		return family
	}
	family = &counterFamily{
		help:     help,
		counters: make(map[string]*int64),
	}
	r.counterFamilys[name] = family
	return family
}

// labelsToString 将标签 map 转为排序后的 key="value",key2="value2" 格式
func labelsToString(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	result := make([]byte, 0, 64)
	for i, k := range keys {
		if i > 0 {
			result = append(result, ',')
		}
		result = append(result, k...)
		result = append(result, '=')
		result = append(result, '"')
		result = append(result, labels[k]...)
		result = append(result, '"')
	}
	return string(result)
}
