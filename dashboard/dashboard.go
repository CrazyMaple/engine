package dashboard

import (
	"context"
	"net/http"
	"sync"
	"time"

	"engine/actor"
	"engine/cluster"
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
}

// Dashboard Web 管理面板
type Dashboard struct {
	config  Config
	server  *http.Server
	started bool
	mu      sync.Mutex
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
}
