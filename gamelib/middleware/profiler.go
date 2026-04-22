package middleware

import (
	"bytes"
	"fmt"
	"runtime"
	"runtime/pprof"
	"sync"
	"sync/atomic"
	"time"

	"engine/log"
)

// ProfileType Profile 类型
type ProfileType int

const (
	ProfileCPU       ProfileType = iota // CPU Profile
	ProfileHeap                         // 堆内存 Profile
	ProfileGoroutine                    // Goroutine 栈 Profile
	ProfileBlock                        // 阻塞 Profile
)

func (t ProfileType) String() string {
	switch t {
	case ProfileCPU:
		return "cpu"
	case ProfileHeap:
		return "heap"
	case ProfileGoroutine:
		return "goroutine"
	case ProfileBlock:
		return "block"
	default:
		return "unknown"
	}
}

// ProfileResult 一次 Profile 采集结果
type ProfileResult struct {
	ID        string      `json:"id"`
	Type      ProfileType `json:"type"`
	TypeName  string      `json:"type_name"`
	Timestamp time.Time   `json:"timestamp"`
	Duration  time.Duration `json:"duration,omitempty"` // CPU Profile 采集时长
	Size      int         `json:"size"`                // 数据大小（字节）
	TraceID   string      `json:"trace_id,omitempty"`  // 关联的 OTel TraceID
	Trigger   string      `json:"trigger"`             // "manual" | "auto:cpu" | "auto:gc"
	Data      []byte      `json:"-"`                   // pprof 二进制数据（不序列化到 JSON）
}

// ProfileSummary Profile 摘要（不含数据）
type ProfileSummary struct {
	ID        string      `json:"id"`
	Type      string      `json:"type"`
	Timestamp time.Time   `json:"timestamp"`
	Duration  time.Duration `json:"duration,omitempty"`
	Size      int         `json:"size"`
	TraceID   string      `json:"trace_id,omitempty"`
	Trigger   string      `json:"trigger"`
}

// ProfileStore Profile 存储（环形缓冲区）
type ProfileStore struct {
	mu       sync.RWMutex
	profiles []*ProfileResult
	maxSize  int
	seq      int64
}

// NewProfileStore 创建 Profile 存储
func NewProfileStore(maxSize int) *ProfileStore {
	if maxSize <= 0 {
		maxSize = 20
	}
	return &ProfileStore{
		profiles: make([]*ProfileResult, 0, maxSize),
		maxSize:  maxSize,
	}
}

// Save 存储一个 Profile（超过上限则淘汰最旧的）
func (s *ProfileStore) Save(p *ProfileResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.profiles) >= s.maxSize {
		s.profiles = s.profiles[1:]
	}
	s.profiles = append(s.profiles, p)
}

// Get 按 ID 获取 Profile
func (s *ProfileStore) Get(id string) *ProfileResult {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, p := range s.profiles {
		if p.ID == id {
			return p
		}
	}
	return nil
}

// List 返回所有 Profile 摘要（不含数据）
func (s *ProfileStore) List() []ProfileSummary {
	s.mu.RLock()
	defer s.mu.RUnlock()
	summaries := make([]ProfileSummary, len(s.profiles))
	for i, p := range s.profiles {
		summaries[i] = ProfileSummary{
			ID:        p.ID,
			Type:      p.TypeName,
			Timestamp: p.Timestamp,
			Duration:  p.Duration,
			Size:      p.Size,
			TraceID:   p.TraceID,
			Trigger:   p.Trigger,
		}
	}
	return summaries
}

func (s *ProfileStore) nextID(profileType ProfileType) string {
	seq := atomic.AddInt64(&s.seq, 1)
	return fmt.Sprintf("%s-%d-%d", profileType.String(), time.Now().Unix(), seq)
}

// AutoProfileConfig 自动 Profiling 配置
type AutoProfileConfig struct {
	Enabled          bool          `json:"enabled"`
	CPUThreshold     float64       `json:"cpu_threshold"`      // CPU 使用率阈值（%）
	GCPauseThreshold time.Duration `json:"gc_pause_threshold"` // GC 暂停时间阈值
	CheckInterval    time.Duration `json:"check_interval"`     // 检查间隔
	ProfileDuration  time.Duration `json:"profile_duration"`   // CPU Profile 采集时长
}

// Profiler 性能分析器
type Profiler struct {
	store      *ProfileStore
	autoConfig AutoProfileConfig
	cpuRunning int32 // 原子标记：是否正在进行 CPU Profile
	stopCh     chan struct{}
	stopped    int32
}

// NewProfiler 创建 Profiler
func NewProfiler(store *ProfileStore, autoCfg AutoProfileConfig) *Profiler {
	if autoCfg.CheckInterval <= 0 {
		autoCfg.CheckInterval = 10 * time.Second
	}
	if autoCfg.ProfileDuration <= 0 {
		autoCfg.ProfileDuration = 10 * time.Second
	}
	if autoCfg.CPUThreshold <= 0 {
		autoCfg.CPUThreshold = 80.0
	}
	if autoCfg.GCPauseThreshold <= 0 {
		autoCfg.GCPauseThreshold = 50 * time.Millisecond
	}
	return &Profiler{
		store:      store,
		autoConfig: autoCfg,
		stopCh:     make(chan struct{}),
	}
}

// Store 返回底层存储
func (p *Profiler) Store() *ProfileStore {
	return p.store
}

// AutoConfig 返回当前自动采集配置
func (p *Profiler) AutoConfig() AutoProfileConfig {
	return p.autoConfig
}

// SetAutoConfig 更新自动采集配置
func (p *Profiler) SetAutoConfig(cfg AutoProfileConfig) {
	p.autoConfig = cfg
}

// StartCPUProfile 采集 CPU Profile
func (p *Profiler) StartCPUProfile(duration time.Duration, traceID string) (*ProfileResult, error) {
	if !atomic.CompareAndSwapInt32(&p.cpuRunning, 0, 1) {
		return nil, fmt.Errorf("CPU profile already in progress")
	}
	defer atomic.StoreInt32(&p.cpuRunning, 0)

	if duration <= 0 {
		duration = 10 * time.Second
	}
	if duration > 60*time.Second {
		duration = 60 * time.Second
	}

	var buf bytes.Buffer
	if err := pprof.StartCPUProfile(&buf); err != nil {
		return nil, fmt.Errorf("start CPU profile: %w", err)
	}
	time.Sleep(duration)
	pprof.StopCPUProfile()

	result := &ProfileResult{
		ID:        p.store.nextID(ProfileCPU),
		Type:      ProfileCPU,
		TypeName:  ProfileCPU.String(),
		Timestamp: time.Now(),
		Duration:  duration,
		Size:      buf.Len(),
		TraceID:   traceID,
		Trigger:   "manual",
		Data:      buf.Bytes(),
	}
	p.store.Save(result)
	return result, nil
}

// CaptureHeapProfile 采集堆内存 Profile
func (p *Profiler) CaptureHeapProfile(traceID string) *ProfileResult {
	var buf bytes.Buffer
	runtime.GC() // 触发 GC 以获取更准确的堆信息
	pprof.WriteHeapProfile(&buf)

	result := &ProfileResult{
		ID:        p.store.nextID(ProfileHeap),
		Type:      ProfileHeap,
		TypeName:  ProfileHeap.String(),
		Timestamp: time.Now(),
		Size:      buf.Len(),
		TraceID:   traceID,
		Trigger:   "manual",
		Data:      buf.Bytes(),
	}
	p.store.Save(result)
	return result
}

// CaptureGoroutineProfile 采集 Goroutine 栈 Profile
func (p *Profiler) CaptureGoroutineProfile(traceID string) *ProfileResult {
	var buf bytes.Buffer
	prof := pprof.Lookup("goroutine")
	if prof != nil {
		prof.WriteTo(&buf, 0)
	}

	result := &ProfileResult{
		ID:        p.store.nextID(ProfileGoroutine),
		Type:      ProfileGoroutine,
		TypeName:  ProfileGoroutine.String(),
		Timestamp: time.Now(),
		Size:      buf.Len(),
		TraceID:   traceID,
		Trigger:   "manual",
		Data:      buf.Bytes(),
	}
	p.store.Save(result)
	return result
}

// CaptureBlockProfile 采集阻塞 Profile
func (p *Profiler) CaptureBlockProfile(traceID string) *ProfileResult {
	// 启用 block profiling
	runtime.SetBlockProfileRate(1)
	time.Sleep(3 * time.Second)
	runtime.SetBlockProfileRate(0)

	var buf bytes.Buffer
	prof := pprof.Lookup("block")
	if prof != nil {
		prof.WriteTo(&buf, 0)
	}

	result := &ProfileResult{
		ID:        p.store.nextID(ProfileBlock),
		Type:      ProfileBlock,
		TypeName:  ProfileBlock.String(),
		Timestamp: time.Now(),
		Duration:  3 * time.Second,
		Size:      buf.Len(),
		TraceID:   traceID,
		Trigger:   "manual",
		Data:      buf.Bytes(),
	}
	p.store.Save(result)
	return result
}

// StartAutoWatch 启动自动 Profiling 监控
func (p *Profiler) StartAutoWatch() {
	if !p.autoConfig.Enabled {
		return
	}
	go p.autoWatchLoop()
}

// Stop 停止自动监控
func (p *Profiler) Stop() {
	if atomic.CompareAndSwapInt32(&p.stopped, 0, 1) {
		close(p.stopCh)
	}
}

func (p *Profiler) autoWatchLoop() {
	ticker := time.NewTicker(p.autoConfig.CheckInterval)
	defer ticker.Stop()

	var lastNumGC uint32

	for {
		select {
		case <-p.stopCh:
			return
		case <-ticker.C:
			var stats runtime.MemStats
			runtime.ReadMemStats(&stats)

			// 检查 GC 暂停时间
			if stats.NumGC > lastNumGC {
				// 检查最近一次 GC 暂停
				idx := (stats.NumGC + 255) % 256
				pause := time.Duration(stats.PauseNs[idx])
				if pause > p.autoConfig.GCPauseThreshold {
					log.Info("[profiler] auto-trigger heap profile: GC pause %v > threshold %v", pause, p.autoConfig.GCPauseThreshold)
					result := p.CaptureHeapProfile("")
					result.Trigger = "auto:gc"
				}
				lastNumGC = stats.NumGC
			}

			// 检查 goroutine 数量作为 CPU 负载近似指标
			numGoroutine := runtime.NumGoroutine()
			cpuApprox := float64(numGoroutine) / float64(runtime.NumCPU()) * 10 // 粗略估算
			if cpuApprox > p.autoConfig.CPUThreshold {
				if atomic.LoadInt32(&p.cpuRunning) == 0 {
					log.Info("[profiler] auto-trigger CPU profile: goroutine load %.1f%% > threshold %.1f%%", cpuApprox, p.autoConfig.CPUThreshold)
					go func() {
						result, err := p.StartCPUProfile(p.autoConfig.ProfileDuration, "")
						if err == nil {
							result.Trigger = "auto:cpu"
						}
					}()
				}
			}
		}
	}
}
