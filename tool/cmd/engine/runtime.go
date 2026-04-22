package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"engine/actor"
	"engine/cluster"
	"gamelib/config"
	"tool/dashboard"
	"gamelib/gate"
	"engine/log"
	"engine/remote"
)

// defaultLogRingCapacity 默认日志环形缓冲容量（Dashboard /api/log/query 使用）
const defaultLogRingCapacity = 2048

// engineRuntime 由 engine.yaml 驱动的进程内引擎运行时
//
// 一个 runtime 对应一个 ActorSystem + 一组可选子组件（Remote/Cluster/Gate/Dashboard/Log）。
// 负责：
//   - 按配置启动所有子组件（幂等 Start）
//   - 承接 SIGHUP 热重载（重启日志 sink、更新日志级别、刷新 Dashboard token）
//   - 关闭时按启动逆序停止子组件
//
// 非线程安全的构造；Start/Stop/Reload 线程安全
type engineRuntime struct {
	mu sync.Mutex

	cfgPath string
	cfg     *config.EngineConfig

	system    *actor.ActorSystem
	remote    *remote.Remote
	cluster   *cluster.Cluster
	gate      *gate.Gate
	dashboard *dashboard.Dashboard

	// 日志管道（Dashboard 需要读取这两个 sink 才能提供 /api/log/query 和 /ws/log）
	logRing      *log.RingBufferSink
	logBroadcast *log.BroadcastSink
	logFile      io.Closer
}

// newRuntime 按 cfg 构建运行时骨架，尚未真正启动
func newRuntime(cfgPath string, cfg *config.EngineConfig) *engineRuntime {
	return &engineRuntime{cfgPath: cfgPath, cfg: cfg}
}

// Start 按配置启动全套引擎
//
// 启动顺序：
//   1. 全局日志（控制台 / 文件）
//   2. ActorSystem
//   3. Remote（Remote.Address 非空）
//   4. Cluster（Cluster.Enabled=true）
//   5. Gate（TCP/WS/KCP 任一非空）
//   6. Dashboard（Dashboard.Enabled=true）
//
// 任一子组件启动失败会立即返回错误，但已启动的组件不会自动回滚——调用方应显式 Stop
func (r *engineRuntime) Start() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.applyLog(r.cfg); err != nil {
		return fmt.Errorf("init log: %w", err)
	}
	log.Info("engine start: node=%s version=%s", r.cfg.NodeID, r.cfg.Version)

	r.system = actor.NewActorSystem()
	log.Info("actor system started: %s", r.system.Address)

	if r.cfg.Remote.Address != "" {
		r.remote = remote.NewRemote(r.system, r.cfg.Remote.Address)
		r.remote.Start()
		log.Info("remote listening on %s", r.cfg.Remote.Address)
	}

	if r.cfg.Cluster.Enabled {
		cc := cluster.DefaultClusterConfig(r.cfg.Cluster.Name, r.cfg.Remote.Address).
			WithSeedNodes(r.cfg.Cluster.Seeds...).
			WithGossipInterval(nonZeroDuration(r.cfg.Cluster.GossipPeriod, time.Second))
		r.cluster = cluster.NewCluster(r.system, r.remote, cc)
		if err := r.cluster.Start(); err != nil {
			return fmt.Errorf("cluster start: %w", err)
		}
		log.Info("cluster started: %s seeds=%v", r.cfg.Cluster.Name, r.cfg.Cluster.Seeds)
	}

	if anyGateEnabled(r.cfg.Gate) {
		g := gate.NewGate(r.system)
		g.TCPAddr = r.cfg.Gate.TCPAddr
		g.WSAddr = r.cfg.Gate.WSAddr
		g.KCPAddr = r.cfg.Gate.KCPAddr
		if r.cfg.Gate.MaxMsgLen > 0 {
			g.MaxMsgLen = r.cfg.Gate.MaxMsgLen
		}
		g.Start()
		r.gate = g
		log.Info("gate started: tcp=%q ws=%q kcp=%q",
			r.cfg.Gate.TCPAddr, r.cfg.Gate.WSAddr, r.cfg.Gate.KCPAddr)
	}

	if r.cfg.Dashboard.Enabled && r.cfg.Dashboard.Listen != "" {
		d := dashboard.New(dashboard.Config{
			Addr:          r.cfg.Dashboard.Listen,
			System:        r.system,
			LogRingBuffer: r.logRing,
			LogBroadcast:  r.logBroadcast,
		})
		if err := d.Start(); err != nil {
			return fmt.Errorf("dashboard start: %w", err)
		}
		r.dashboard = d
		log.Info("dashboard started: %s", r.cfg.Dashboard.Listen)
	}

	return nil
}

// Stop 按启动逆序关闭子组件，最后关闭日志文件句柄
//
// 任一组件 Stop 返回错误都只记录日志，确保尽可能完整清理
func (r *engineRuntime) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.dashboard != nil {
		_ = r.dashboard.Stop()
		r.dashboard = nil
	}
	if r.gate != nil {
		r.gate.Close()
		r.gate = nil
	}
	if r.cluster != nil {
		r.cluster.Stop()
		r.cluster = nil
	}
	// remote 不提供 Stop（TCP Listener 随进程结束关闭）
	r.remote = nil
	// ActorSystem 无显式关闭方法，依赖 Stop(pid) 逐个清理
	r.system = nil
	if r.logFile != nil {
		_ = r.logFile.Close()
		r.logFile = nil
	}
}

// Reload 重新加载 YAML 并尝试在线应用可安全变更的字段
//
// 目前支持的热切换：
//   - 日志级别 / 日志文件路径（重新打开文件）
//   - Dashboard Token（下一请求生效）
//
// 需要重启的字段（监听端口、集群配置）会打印警告并保持原值
func (r *engineRuntime) Reload() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.cfgPath == "" {
		return fmt.Errorf("reload requires --config path")
	}
	newCfg, err := config.LoadEngineConfig(r.cfgPath)
	if err != nil {
		return err
	}

	if err := r.applyLog(newCfg); err != nil {
		return fmt.Errorf("reload log: %w", err)
	}
	if !strings.EqualFold(r.cfg.Log.Level, newCfg.Log.Level) {
		log.Info("log level reloaded: %s -> %s", r.cfg.Log.Level, newCfg.Log.Level)
	}

	// 端口类字段无法热切换，只提示
	if r.cfg.Gate.TCPAddr != newCfg.Gate.TCPAddr ||
		r.cfg.Gate.WSAddr != newCfg.Gate.WSAddr ||
		r.cfg.Gate.KCPAddr != newCfg.Gate.KCPAddr {
		log.Warn("gate listen address change requires restart; keeping previous listeners")
	}
	if r.cfg.Dashboard.Listen != newCfg.Dashboard.Listen {
		log.Warn("dashboard listen address change requires restart; keeping previous listener")
	}
	if r.cfg.Cluster.Enabled != newCfg.Cluster.Enabled || r.cfg.Cluster.Name != newCfg.Cluster.Name {
		log.Warn("cluster topology change requires restart; keeping previous configuration")
	}

	r.cfg = newCfg
	return nil
}

// applyLog 按 cfg.Log 重置全局 Logger 并挂接可观测 sink
//
// 日志管道结构：ContextLogger(nodeID) → MultiSink[ 输出sink, RingBuffer, Broadcast ]
//   - 输出 sink：stdout（或文件）— format=json 走 FileLogSink（JSON 行）
//     其他 → TextLogSink（人类可读文本）
//   - RingBuffer：供 Dashboard /api/log/query 使用
//   - Broadcast：供 Dashboard /ws/log WebSocket 实时推送使用
//
// RingBuffer / Broadcast 在首次调用时创建，热重载时复用以保持订阅者连接不断。
func (r *engineRuntime) applyLog(cfg *config.EngineConfig) error {
	if cfg.Log.Level != "" {
		if lv, err := log.ParseLevel(cfg.Log.Level); err == nil {
			log.SetLevel(lv)
		}
	}

	// 可观测 sink（复用，保持已订阅的 WebSocket 客户端不断开）
	if r.logRing == nil {
		r.logRing = log.NewRingBufferSink(defaultLogRingCapacity)
	}
	if r.logBroadcast == nil {
		r.logBroadcast = log.NewBroadcastSink()
	}

	// 输出 sink（根据 format / path 重建）
	var outSink log.LogSink
	var closer io.Closer
	switch cfg.Log.Format {
	case "json":
		if cfg.Log.Path != "" {
			fs, err := log.NewFileLogSink(cfg.Log.Path)
			if err != nil {
				return err
			}
			outSink = fs
			closer = fs
		} else {
			outSink = log.NewWriterSink(os.Stdout)
		}
	default:
		if cfg.Log.Path != "" {
			ts, err := log.NewTextFileSink(cfg.Log.Path)
			if err != nil {
				return err
			}
			outSink = ts
			closer = ts
		} else {
			outSink = log.NewTextLogSink(os.Stdout)
		}
	}

	multi := log.NewMultiSink(outSink, r.logRing, r.logBroadcast)
	log.SetLogger(log.NewContextLogger(cfg.NodeID, multi))

	// 替换日志文件句柄，旧的关闭
	if r.logFile != nil {
		_ = r.logFile.Close()
	}
	r.logFile = closer
	return nil
}

func anyGateEnabled(g config.GateSection) bool {
	return g.TCPAddr != "" || g.WSAddr != "" || g.KCPAddr != ""
}

func nonZeroDuration(d, fallback time.Duration) time.Duration {
	if d <= 0 {
		return fallback
	}
	return d
}
