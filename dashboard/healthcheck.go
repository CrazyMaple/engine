package dashboard

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// HealthStatus 健康状态
type HealthStatus string

const (
	HealthStatusUp   HealthStatus = "UP"
	HealthStatusDown HealthStatus = "DOWN"
)

// HealthCheck 健康检查项
type HealthCheck struct {
	Name    string
	CheckFn func() HealthCheckResult
}

// HealthCheckResult 单项检查结果
type HealthCheckResult struct {
	Status  HealthStatus           `json:"status"`
	Details map[string]interface{} `json:"details,omitempty"`
}

// HealthCheckResponse 健康检查响应
type HealthCheckResponse struct {
	Status  HealthStatus                `json:"status"`
	Checks  map[string]HealthCheckResult `json:"checks,omitempty"`
	Time    string                       `json:"time"`
}

// HealthChecker 健康检查管理器
type HealthChecker struct {
	mu             sync.RWMutex
	livenessChecks []HealthCheck
	readinessChecks []HealthCheck
}

// NewHealthChecker 创建健康检查管理器
func NewHealthChecker() *HealthChecker {
	return &HealthChecker{}
}

// AddLivenessCheck 注册 Liveness 检查项
func (hc *HealthChecker) AddLivenessCheck(name string, fn func() HealthCheckResult) {
	hc.mu.Lock()
	defer hc.mu.Unlock()
	hc.livenessChecks = append(hc.livenessChecks, HealthCheck{Name: name, CheckFn: fn})
}

// AddReadinessCheck 注册 Readiness 检查项
func (hc *HealthChecker) AddReadinessCheck(name string, fn func() HealthCheckResult) {
	hc.mu.Lock()
	defer hc.mu.Unlock()
	hc.readinessChecks = append(hc.readinessChecks, HealthCheck{Name: name, CheckFn: fn})
}

// CheckLiveness 执行所有 Liveness 检查
func (hc *HealthChecker) CheckLiveness() HealthCheckResponse {
	return hc.runChecks(hc.livenessChecks)
}

// CheckReadiness 执行所有 Readiness 检查
func (hc *HealthChecker) CheckReadiness() HealthCheckResponse {
	return hc.runChecks(hc.readinessChecks)
}

func (hc *HealthChecker) runChecks(checks []HealthCheck) HealthCheckResponse {
	hc.mu.RLock()
	checksCopy := make([]HealthCheck, len(checks))
	copy(checksCopy, checks)
	hc.mu.RUnlock()

	resp := HealthCheckResponse{
		Status: HealthStatusUp,
		Checks: make(map[string]HealthCheckResult, len(checksCopy)),
		Time:   time.Now().Format(time.RFC3339),
	}

	for _, check := range checksCopy {
		result := runSingleCheck(check.CheckFn)
		resp.Checks[check.Name] = result
		if result.Status == HealthStatusDown {
			resp.Status = HealthStatusDown
		}
	}

	return resp
}

// runSingleCheck 执行单个检查，捕获 panic
func runSingleCheck(fn func() HealthCheckResult) (result HealthCheckResult) {
	defer func() {
		if r := recover(); r != nil {
			result = HealthCheckResult{
				Status:  HealthStatusDown,
				Details: map[string]interface{}{"panic": r},
			}
		}
	}()
	return fn()
}

// RegisterHealthRoutes 注册健康检查路由到已有的 ServeMux
func RegisterHealthRoutes(mux *http.ServeMux, checker *HealthChecker) {
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		resp := checker.CheckLiveness()
		writeHealthResponse(w, resp)
	})

	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		resp := checker.CheckReadiness()
		writeHealthResponse(w, resp)
	})
}

func writeHealthResponse(w http.ResponseWriter, resp HealthCheckResponse) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if resp.Status == HealthStatusDown {
		w.WriteHeader(http.StatusServiceUnavailable)
	} else {
		w.WriteHeader(http.StatusOK)
	}
	json.NewEncoder(w).Encode(resp)
}
