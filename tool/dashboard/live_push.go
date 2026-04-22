package dashboard

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// --- Dashboard v3 实时大屏 ---
//
// 提供 WebSocket 实时推送、热力图数据、一键导出运行报告
// 替代 v1/v2 的 5 秒轮询方案

// LivePushMessage 推送到 WebSocket 客户端的实时消息
type LivePushMessage struct {
	Type      string      `json:"type"`      // runtime / metrics / cluster / hotactors
	Timestamp int64       `json:"timestamp"` // Unix 毫秒
	Payload   interface{} `json:"payload"`
}

// LivePushConfig 实时推送配置
type LivePushConfig struct {
	// PushInterval 推送间隔（默认 1 秒）
	PushInterval time.Duration
	// Topics 需要推送的主题（runtime/metrics/cluster/hotactors），空则全部
	Topics []string
}

// LivePushServer 实时数据推送服务器
// 维护 WebSocket 客户端连接，定期推送运行时、指标、集群状态、热点 Actor
type LivePushServer struct {
	config    LivePushConfig
	handlers  *handlers // 借用已有 handlers 收集数据
	upgrader  websocket.Upgrader
	clients   map[*wsClient]bool
	mu        sync.RWMutex
	stopCh    chan struct{}
	topicSet  map[string]bool
}

// wsClient WebSocket 客户端封装
type wsClient struct {
	conn    *websocket.Conn
	sendCh  chan *LivePushMessage
	closeCh chan struct{}
	once    sync.Once
}

// NewLivePushServer 创建实时推送服务器
func NewLivePushServer(dashboardConfig Config, cfg LivePushConfig) *LivePushServer {
	if cfg.PushInterval <= 0 {
		cfg.PushInterval = time.Second
	}

	topicSet := make(map[string]bool)
	for _, t := range cfg.Topics {
		topicSet[t] = true
	}
	if len(topicSet) == 0 {
		// 默认推送全部主题
		for _, t := range []string{"runtime", "metrics", "cluster", "hotactors"} {
			topicSet[t] = true
		}
	}

	return &LivePushServer{
		config:   cfg,
		handlers: &handlers{config: dashboardConfig},
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 4096,
			CheckOrigin:     func(r *http.Request) bool { return true },
		},
		clients:  make(map[*wsClient]bool),
		stopCh:   make(chan struct{}),
		topicSet: topicSet,
	}
}

// Start 启动推送循环
func (lps *LivePushServer) Start() {
	go lps.pushLoop()
}

// Stop 停止推送服务器
func (lps *LivePushServer) Stop() {
	close(lps.stopCh)
	lps.mu.Lock()
	for c := range lps.clients {
		c.close()
	}
	lps.mu.Unlock()
}

// HandleWebSocket HTTP Handler 处理 WebSocket 升级
func (lps *LivePushServer) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := lps.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	client := &wsClient{
		conn:    conn,
		sendCh:  make(chan *LivePushMessage, 32),
		closeCh: make(chan struct{}),
	}

	lps.mu.Lock()
	lps.clients[client] = true
	lps.mu.Unlock()

	// 客户端写循环
	go client.writeLoop()

	// 客户端读循环（主要处理关闭，接收到的消息暂无处理）
	go func() {
		defer func() {
			lps.mu.Lock()
			delete(lps.clients, client)
			lps.mu.Unlock()
			client.close()
		}()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()
}

// ClientCount 返回当前连接的客户端数量
func (lps *LivePushServer) ClientCount() int {
	lps.mu.RLock()
	defer lps.mu.RUnlock()
	return len(lps.clients)
}

func (lps *LivePushServer) pushLoop() {
	ticker := time.NewTicker(lps.config.PushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-lps.stopCh:
			return
		case <-ticker.C:
			lps.collectAndPush()
		}
	}
}

// collectAndPush 收集数据并推送到所有客户端
func (lps *LivePushServer) collectAndPush() {
	now := time.Now().UnixMilli()
	messages := make([]*LivePushMessage, 0, 4)

	if lps.topicSet["runtime"] {
		messages = append(messages, &LivePushMessage{
			Type:      "runtime",
			Timestamp: now,
			Payload:   collectRuntime(),
		})
	}

	if lps.topicSet["metrics"] && lps.handlers.config.Metrics != nil {
		messages = append(messages, &LivePushMessage{
			Type:      "metrics",
			Timestamp: now,
			Payload:   lps.handlers.config.Metrics.Snapshot(),
		})
	}

	if lps.topicSet["cluster"] && lps.handlers.config.Cluster != nil {
		members := lps.handlers.config.Cluster.Members()
		nodes := make([]graphNode, 0, len(members))
		edges := make([]graphEdge, 0)
		for _, m := range members {
			nodes = append(nodes, graphNode{
				ID:      m.Id,
				Address: m.Address,
				Status:  m.Status.String(),
				Kinds:   m.Kinds,
			})
		}
		for i := 0; i < len(nodes); i++ {
			if nodes[i].Status != "alive" {
				continue
			}
			for j := i + 1; j < len(nodes); j++ {
				if nodes[j].Status != "alive" {
					continue
				}
				edges = append(edges, graphEdge{From: nodes[i].ID, To: nodes[j].ID})
			}
		}
		messages = append(messages, &LivePushMessage{
			Type:      "cluster",
			Timestamp: now,
			Payload:   clusterGraph{Nodes: nodes, Edges: edges},
		})
	}

	if lps.topicSet["hotactors"] && lps.handlers.config.HotTracker != nil {
		messages = append(messages, &LivePushMessage{
			Type:      "hotactors",
			Timestamp: now,
			Payload:   lps.handlers.config.HotTracker.TopN(20),
		})
	}

	lps.mu.RLock()
	clients := make([]*wsClient, 0, len(lps.clients))
	for c := range lps.clients {
		clients = append(clients, c)
	}
	lps.mu.RUnlock()

	for _, client := range clients {
		for _, msg := range messages {
			select {
			case client.sendCh <- msg:
			default:
				// 客户端发送缓冲区满，丢弃消息，防止阻塞
			}
		}
	}
}

// collectRuntime 采集 Go 运行时指标（复用 handleRuntime 的逻辑）
func collectRuntime() runtimeInfo {
	h := &handlers{}
	// 直接复用 handleRuntime 的采集逻辑，构造一个临时 ResponseWriter 无意义
	// 这里内联 runtime 信息收集
	var info runtimeInfo
	h.fillRuntimeInfo(&info)
	return info
}

// fillRuntimeInfo 填充 runtime 信息（抽取自 handleRuntime）
func (h *handlers) fillRuntimeInfo(info *runtimeInfo) {
	var mem memStatsCompact
	readMemStats(&mem)

	toMB := func(b uint64) float64 { return float64(b) / 1024 / 1024 }
	info.GoVersion = mem.GoVersion
	info.NumGoroutine = mem.NumGoroutine
	info.NumCPU = mem.NumCPU
	info.AllocMB = toMB(mem.Alloc)
	info.TotalAllocMB = toMB(mem.TotalAlloc)
	info.SysMB = toMB(mem.Sys)
	info.HeapAllocMB = toMB(mem.HeapAlloc)
	info.HeapInuseMB = toMB(mem.HeapInuse)
	info.StackInuseMB = toMB(mem.StackInuse)
	info.NumGC = mem.NumGC
	info.GCPauseMs = mem.LastGCPauseMs
	info.GCPauseTotMs = mem.PauseTotalMs
	info.GCCPUPercent = mem.GCCPUPercent
}

// writeLoop WebSocket 客户端写循环
func (c *wsClient) writeLoop() {
	defer c.close()
	for {
		select {
		case <-c.closeCh:
			return
		case msg := <-c.sendCh:
			data, err := json.Marshal(msg)
			if err != nil {
				continue
			}
			_ = c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
				return
			}
		}
	}
}

func (c *wsClient) close() {
	c.once.Do(func() {
		close(c.closeCh)
		_ = c.conn.Close()
	})
}
