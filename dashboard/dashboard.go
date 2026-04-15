package dashboard

import (
	"context"
	"net/http"
	"sync"
	"time"

	"engine/actor"
	"engine/cluster"
	"engine/cluster/canary"
	"engine/config"
	"engine/log"
	"engine/middleware"
)

// Config Dashboard 配置
type Config struct {
	// Addr HTTP 监听地址（默认 "127.0.0.1:8080"）
	Addr string
	// System ActorSystem 实例
	System *actor.ActorSystem
	// Cluster 集群实例（可选，为 nil 则不显示集群信息）
	Cluster *cluster.Cluster
	// Metrics 指标收集器（可选）
	Metrics *middleware.Metrics
	// HotTracker 热点 Actor 追踪器（可选）
	HotTracker *HotActorTracker
	// TraceStore 追踪记录存储（可选，为 nil 则不支持追踪查询）
	TraceStore *middleware.TraceStore
	// MetricsRegistry 指标注册中心（可选，提供更完整的 Prometheus 指标）
	MetricsRegistry *middleware.MetricsRegistry
	// MetricsHistory 消息流量历史（可选，提供趋势图数据）
	MetricsHistory *MetricsHistory
	// ConfigManager 配置管理器（可选，支持在线查看和重载配置）
	ConfigManager *config.Manager
	// AuditLog 审计日志（可选）
	AuditLog *AuditLog
	// Auth 访问鉴权配置（可选，nil 则无鉴权）
	Auth *AuthConfig
	// DeadLetterMonitor 死信监控器（可选）
	DeadLetterMonitor *actor.DeadLetterMonitor
	// HealthChecker 健康检查管理器（可选，注册 /healthz 和 /readyz 端点）
	HealthChecker *HealthChecker
	// LivePush Dashboard v3 实时 WebSocket 推送配置（可选）
	LivePush *LivePushConfig
	// Profiler 性能分析器（可选，支持按需/自动 Profiling）
	Profiler *middleware.Profiler
	// ActorProfiler Actor 级别 Profiling（可选，支持每 Actor 消息耗时统计）
	ActorProfiler *middleware.ActorProfiler
	// CanaryEngine 灰度发布引擎（可选，支持灰度规则和权重路由）
	CanaryEngine *canary.Engine
	// CanaryComparator 灰度指标对比器（可选，支持版本间指标对比）
	CanaryComparator *canary.Comparator
	// GMManager GM 管理后台（可选，支持 GM 命令、权限控制、批量操作）
	GMManager *GMManager
}

// Dashboard Web 管理面板
type Dashboard struct {
	config   Config
	server   *http.Server
	livePush *LivePushServer
	started  bool
	mu       sync.Mutex
}

// New 创建 Dashboard
func New(cfg Config) *Dashboard {
	if cfg.Addr == "" {
		cfg.Addr = "127.0.0.1:8080"
	}
	return &Dashboard{config: cfg}
}

// Start 启动 Dashboard HTTP 服务
func (d *Dashboard) Start() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.started {
		return nil
	}

	// 初始化 v3 WebSocket 实时推送（路由注册前创建）
	if d.config.LivePush != nil {
		d.livePush = NewLivePushServer(d.config, *d.config.LivePush)
	}

	mux := http.NewServeMux()
	d.registerRoutes(mux)

	var handler http.Handler = mux
	if d.config.Auth != nil {
		handler = authMiddleware(d.config.Auth, mux)
	}

	d.server = &http.Server{
		Addr:         d.config.Addr,
		Handler:      handler,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	go func() {
		log.Info("Dashboard started on %s", d.config.Addr)
		if err := d.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("Dashboard error: %v", err)
		}
	}()

	// 启动 v3 WebSocket 实时推送
	if d.livePush != nil {
		d.livePush.Start()
	}

	d.started = true
	return nil
}

// Stop 停止 Dashboard
func (d *Dashboard) Stop() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if !d.started {
		return nil
	}

	// 停止 v3 LivePush
	if d.livePush != nil {
		d.livePush.Stop()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := d.server.Shutdown(ctx)
	d.started = false
	log.Info("Dashboard stopped")
	return err
}

func (d *Dashboard) registerRoutes(mux *http.ServeMux) {
	h := &handlers{config: d.config}

	mux.HandleFunc("/", h.handleIndex)
	mux.HandleFunc("/api/system", h.handleSystem)
	mux.HandleFunc("/api/actors", h.handleActors)
	mux.HandleFunc("/api/actors/", h.handleActorChildren)
	mux.HandleFunc("/api/cluster", h.handleCluster)
	mux.HandleFunc("/api/cluster/members", h.handleClusterMembers)
	mux.HandleFunc("/api/metrics", h.handleMetrics)
	mux.HandleFunc("/api/metrics/prometheus", h.handleMetricsPrometheus)
	mux.HandleFunc("/api/hotactors", h.handleHotActors)
	mux.HandleFunc("/api/runtime", h.handleRuntime)
	mux.HandleFunc("/api/actors/topology", h.handleActorTopology)
	mux.HandleFunc("/api/traces", h.handleTraces)
	mux.HandleFunc("/api/metrics/history", h.handleMetricsHistory)
	mux.HandleFunc("/api/cluster/graph", h.handleClusterGraph)
	mux.HandleFunc("/api/actors/flamegraph", h.handleFlameGraph)
	mux.HandleFunc("/api/config", h.handleConfig)
	mux.HandleFunc("/api/config/reload", h.handleConfigReload)
	mux.HandleFunc("/api/audit", h.handleAuditLog)
	mux.HandleFunc("/api/log/level", h.handleLogLevel)
	mux.HandleFunc("/api/deadletters", h.handleDeadLetters)

	// v3 新增路由
	mux.HandleFunc("/api/report", h.handleReportJSON)
	mux.HandleFunc("/api/report.csv", h.handleReportCSV)
	mux.HandleFunc("/api/actors/heatmap", h.handleHeatmap)

	// WebSocket 实时推送（v3）
	if d.livePush != nil {
		mux.HandleFunc("/ws/live", d.livePush.HandleWebSocket)
	}

	// Profiler 端点（按需/自动 Profiling + Actor 级别分析）
	if d.config.Profiler != nil {
		mux.HandleFunc("/api/profiler/cpu", h.handleProfilerCPU)
		mux.HandleFunc("/api/profiler/heap", h.handleProfilerHeap)
		mux.HandleFunc("/api/profiler/goroutine", h.handleProfilerGoroutine)
		mux.HandleFunc("/api/profiler/block", h.handleProfilerBlock)
		mux.HandleFunc("/api/profiler/list", h.handleProfilerList)
		mux.HandleFunc("/api/profiler/get", h.handleProfilerGet)
		mux.HandleFunc("/api/profiler/diff", h.handleProfilerDiff)
		mux.HandleFunc("/api/profiler/auto/config", h.handleProfilerAutoConfig)
	}
	if d.config.ActorProfiler != nil {
		mux.HandleFunc("/api/profiler/actors", h.handleProfilerActors)
		mux.HandleFunc("/api/profiler/actors/enable", h.handleProfilerActorsEnable)
		mux.HandleFunc("/api/profiler/actors/disable", h.handleProfilerActorsDisable)
	}

	// 灰度发布端点
	if d.config.CanaryEngine != nil {
		mux.HandleFunc("/api/canary/status", h.handleCanaryStatus)
		mux.HandleFunc("/api/canary/rules", h.handleCanaryRules)
		mux.HandleFunc("/api/canary/weights", h.handleCanaryWeights)
		mux.HandleFunc("/api/canary/promote", h.handleCanaryPromote)
		mux.HandleFunc("/api/canary/rollback", h.handleCanaryRollback)
	}
	if d.config.CanaryComparator != nil {
		mux.HandleFunc("/api/canary/compare", h.handleCanaryCompare)
	}

	// GM 管理后台端点
	if d.config.GMManager != nil {
		gmHandlers := NewGMHandlers(d.config.GMManager)
		gmHandlers.RegisterRoutes(mux)
	}

	// 健康检查端点（与 Dashboard 复用端口）
	if d.config.HealthChecker != nil {
		RegisterHealthRoutes(mux, d.config.HealthChecker)
	}
}
