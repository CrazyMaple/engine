package stress

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// --- 分布式压测协调器 ---
// 支持多台机器协同发压，一个 Coordinator + 多个 Worker

// WorkerState Worker 状态
type WorkerState string

const (
	WorkerIdle    WorkerState = "idle"
	WorkerRunning WorkerState = "running"
	WorkerDone    WorkerState = "done"
	WorkerError   WorkerState = "error"
)

// WorkerInfo 已注册的 Worker 信息
type WorkerInfo struct {
	ID      string      `json:"id"`
	Addr    string      `json:"addr"`
	State   WorkerState `json:"state"`
	Report  *Report     `json:"report,omitempty"`
	Error   string      `json:"error,omitempty"`
	Updated time.Time   `json:"updated"`
}

// Coordinator 分布式压测协调器
type Coordinator struct {
	mu      sync.RWMutex
	workers map[string]*WorkerInfo
	config  *ScenarioConfig
}

// NewCoordinator 创建协调器
func NewCoordinator() *Coordinator {
	return &Coordinator{
		workers: make(map[string]*WorkerInfo),
	}
}

// RegisterWorker 注册 Worker
func (c *Coordinator) RegisterWorker(id, addr string) {
	c.mu.Lock()
	c.workers[id] = &WorkerInfo{
		ID:      id,
		Addr:    addr,
		State:   WorkerIdle,
		Updated: time.Now(),
	}
	c.mu.Unlock()
}

// Workers 获取所有 Worker 信息
func (c *Coordinator) Workers() []*WorkerInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make([]*WorkerInfo, 0, len(c.workers))
	for _, w := range c.workers {
		result = append(result, w)
	}
	return result
}

// StartAll 向所有 Worker 下发压测任务
// 将总并发数均分到各 Worker
func (c *Coordinator) StartAll(ctx context.Context, cfg *ScenarioConfig) error {
	c.mu.Lock()
	c.config = cfg
	workerCount := len(c.workers)
	c.mu.Unlock()

	if workerCount == 0 {
		return fmt.Errorf("no workers registered")
	}

	// 均分并发数
	perWorker := cfg.Concurrency / workerCount
	remainder := cfg.Concurrency % workerCount

	c.mu.RLock()
	workers := make([]*WorkerInfo, 0, len(c.workers))
	for _, w := range c.workers {
		workers = append(workers, w)
	}
	c.mu.RUnlock()

	var wg sync.WaitGroup
	for i, w := range workers {
		conc := perWorker
		if i < remainder {
			conc++
		}
		if conc == 0 {
			continue
		}

		workerCfg := *cfg
		workerCfg.Concurrency = conc

		wg.Add(1)
		go func(worker *WorkerInfo, wCfg ScenarioConfig) {
			defer wg.Done()
			if err := c.sendStartToWorker(ctx, worker, &wCfg); err != nil {
				c.mu.Lock()
				worker.State = WorkerError
				worker.Error = err.Error()
				worker.Updated = time.Now()
				c.mu.Unlock()
			}
		}(w, workerCfg)
	}

	wg.Wait()
	return nil
}

// CollectReports 收集所有 Worker 的报告并聚合
func (c *Coordinator) CollectReports() *Report {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var (
		totalReq    int64
		totalErr    int64
		totalBytes  int64
		sumLatAvg   float64
		maxP99      float64
		maxP95      float64
		maxP90      float64
		maxP50      float64
		maxMax      float64
		reportCount int
	)

	cfg := c.config
	if cfg == nil {
		cfg = &ScenarioConfig{Name: "distributed"}
	}

	for _, w := range c.workers {
		if w.Report == nil {
			continue
		}
		r := w.Report
		totalReq += r.TotalRequests
		totalErr += r.TotalErrors
		totalBytes += r.TotalBytes
		sumLatAvg += r.LatencyAvg
		if r.LatencyP99 > maxP99 {
			maxP99 = r.LatencyP99
		}
		if r.LatencyP95 > maxP95 {
			maxP95 = r.LatencyP95
		}
		if r.LatencyP90 > maxP90 {
			maxP90 = r.LatencyP90
		}
		if r.LatencyP50 > maxP50 {
			maxP50 = r.LatencyP50
		}
		if r.LatencyMax > maxMax {
			maxMax = r.LatencyMax
		}
		reportCount++
	}

	dur := cfg.Duration
	if dur <= 0 {
		dur = time.Second
	}

	report := &Report{
		Scenario:      cfg.Name + " (distributed)",
		StartTime:     time.Now().Add(-dur),
		Duration:      dur,
		Concurrency:   cfg.Concurrency,
		TotalRequests: totalReq,
		TPS:           float64(totalReq) / dur.Seconds(),
		TotalBytes:    totalBytes,
		TotalErrors:   totalErr,
		LatencyP50:    maxP50,
		LatencyP90:    maxP90,
		LatencyP95:    maxP95,
		LatencyP99:    maxP99,
		LatencyMax:    maxMax,
	}

	if reportCount > 0 {
		report.LatencyAvg = sumLatAvg / float64(reportCount)
	}
	if totalReq > 0 {
		report.ErrorRate = float64(totalErr) / float64(totalReq) * 100
	}

	return report
}

func (c *Coordinator) sendStartToWorker(ctx context.Context, w *WorkerInfo, cfg *ScenarioConfig) error {
	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("http://%s/stress/start", w.Addr), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Body = http.NoBody

	// 重新创建带 body 的请求
	req, _ = http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("http://%s/stress/start", w.Addr),
		jsonReader(data))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	c.mu.Lock()
	w.State = WorkerRunning
	w.Updated = time.Now()
	c.mu.Unlock()

	return nil
}

// --- Worker 端 HTTP API ---

// WorkerServer 压测 Worker HTTP 服务
type WorkerServer struct {
	Factory BotFactory
	report  *Report
	mu      sync.Mutex
	running bool
}

// NewWorkerServer 创建 Worker 服务
func NewWorkerServer(factory BotFactory) *WorkerServer {
	return &WorkerServer{Factory: factory}
}

// RegisterRoutes 注册 Worker HTTP 端点
func (ws *WorkerServer) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/stress/start", ws.handleStart)
	mux.HandleFunc("/stress/status", ws.handleStatus)
	mux.HandleFunc("/stress/report", ws.handleReport)
}

func (ws *WorkerServer) handleStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var cfg ScenarioConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		http.Error(w, "invalid config: "+err.Error(), http.StatusBadRequest)
		return
	}

	ws.mu.Lock()
	if ws.running {
		ws.mu.Unlock()
		http.Error(w, "already running", http.StatusConflict)
		return
	}
	ws.running = true
	ws.mu.Unlock()

	go func() {
		runner := NewRunner(&cfg, ws.Factory)
		report, _ := runner.Run(context.Background())
		ws.mu.Lock()
		ws.report = report
		ws.running = false
		ws.mu.Unlock()
	}()

	w.WriteHeader(http.StatusAccepted)
}

func (ws *WorkerServer) handleStatus(w http.ResponseWriter, r *http.Request) {
	ws.mu.Lock()
	state := "idle"
	if ws.running {
		state = "running"
	} else if ws.report != nil {
		state = "done"
	}
	ws.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"state": state})
}

func (ws *WorkerServer) handleReport(w http.ResponseWriter, r *http.Request) {
	ws.mu.Lock()
	report := ws.report
	ws.mu.Unlock()

	if report == nil {
		http.Error(w, "no report available", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(report)
}

// jsonReader 包装 JSON 数据为 io.Reader
func jsonReader(data []byte) *jsonBody {
	return &jsonBody{data: data}
}

type jsonBody struct {
	data []byte
	pos  int
}

func (j *jsonBody) Read(p []byte) (n int, err error) {
	if j.pos >= len(j.data) {
		return 0, fmt.Errorf("EOF")
	}
	n = copy(p, j.data[j.pos:])
	j.pos += n
	return n, nil
}

func (j *jsonBody) Close() error { return nil }
