// Package bench 引擎级性能基准回归体系
// 与 stress/baseline 的区别：stress 面向压测场景的 TPS/P99 基线；
// bench/ 面向 `go test -bench=.` 产出的 Benchmark 结果解析与回归对比。
package bench

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"
)

// BenchResult 单次基准测试结果
type BenchResult struct {
	Name       string  `json:"name"`         // 基准名，如 BenchmarkActorSend
	Package    string  `json:"package"`      // 所属包路径
	Iterations int64   `json:"iterations"`   // N
	NsPerOp    float64 `json:"ns_per_op"`    // 每次操作耗时
	MBPerSec   float64 `json:"mb_per_sec,omitempty"`
	BytesPerOp int64   `json:"bytes_per_op,omitempty"`
	AllocsPerOp int64  `json:"allocs_per_op,omitempty"`
}

// Key 返回基准的唯一标识（含包路径以避免同名基准冲突）
func (r BenchResult) Key() string {
	if r.Package == "" {
		return r.Name
	}
	return r.Package + "." + r.Name
}

// Baseline 基线文件内容
type Baseline struct {
	Version   string                   `json:"version"`
	UpdatedAt time.Time                `json:"updated_at"`
	Commit    string                   `json:"commit,omitempty"`
	Results   map[string]BenchResult   `json:"results"`
	History   map[string][]BenchResult `json:"history,omitempty"`
}

// BaselineStore 基线文件的加载/保存/更新管理器
type BaselineStore struct {
	Path string
	mu   sync.Mutex
	data *Baseline
}

// NewBaselineStore 基于文件路径创建基线存储
func NewBaselineStore(path string) *BaselineStore {
	return &BaselineStore{Path: path}
}

// Load 从磁盘加载基线，文件不存在返回空基线
func (s *BaselineStore) Load() (*Baseline, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	b := &Baseline{
		Version: "1.0",
		Results: make(map[string]BenchResult),
		History: make(map[string][]BenchResult),
	}
	data, err := os.ReadFile(s.Path)
	if err != nil {
		if os.IsNotExist(err) {
			s.data = b
			return b, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(data, b); err != nil {
		return nil, fmt.Errorf("parse baseline %s: %w", s.Path, err)
	}
	if b.Results == nil {
		b.Results = make(map[string]BenchResult)
	}
	if b.History == nil {
		b.History = make(map[string][]BenchResult)
	}
	s.data = b
	return b, nil
}

// Save 将基线写回磁盘（缩进 JSON 便于 diff）
func (s *BaselineStore) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data == nil {
		return fmt.Errorf("baseline not loaded")
	}
	data, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.Path, data, 0644)
}

// Update 批量更新基线，旧值归入历史（每个基准最多保留 50 条历史）
func (s *BaselineStore) Update(results []BenchResult, commit string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data == nil {
		s.data = &Baseline{
			Version: "1.0",
			Results: make(map[string]BenchResult),
			History: make(map[string][]BenchResult),
		}
	}
	for _, r := range results {
		key := r.Key()
		if old, ok := s.data.Results[key]; ok {
			s.data.History[key] = append(s.data.History[key], old)
			if len(s.data.History[key]) > 50 {
				s.data.History[key] = s.data.History[key][len(s.data.History[key])-50:]
			}
		}
		s.data.Results[key] = r
	}
	s.data.UpdatedAt = time.Now()
	if commit != "" {
		s.data.Commit = commit
	}
}

// Data 返回当前基线数据（未加载时会先尝试加载）
func (s *BaselineStore) Data() (*Baseline, error) {
	s.mu.Lock()
	if s.data != nil {
		d := s.data
		s.mu.Unlock()
		return d, nil
	}
	s.mu.Unlock()
	return s.Load()
}

// SortedKeys 返回按名字排序的基准键列表
func (b *Baseline) SortedKeys() []string {
	keys := make([]string, 0, len(b.Results))
	for k := range b.Results {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
