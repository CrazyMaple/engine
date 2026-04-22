package remote

import (
	"sync"
	"sync/atomic"
	"time"

	"engine/log"
)

// HealthCheckConfig 连接健康检查配置
type HealthCheckConfig struct {
	// PingInterval Ping 发送间隔（默认 5s）
	PingInterval time.Duration
	// PingTimeout 等待 Pong 响应的超时时间（默认 3s）
	PingTimeout time.Duration
	// MaxMissedPings 连续未收到 Pong 的最大次数，超过则认为连接已死（默认 3）
	MaxMissedPings int
}

// DefaultHealthCheckConfig 返回默认健康检查配置
func DefaultHealthCheckConfig() HealthCheckConfig {
	return HealthCheckConfig{
		PingInterval:   5 * time.Second,
		PingTimeout:    3 * time.Second,
		MaxMissedPings: 3,
	}
}

func (c *HealthCheckConfig) defaults() {
	if c.PingInterval <= 0 {
		c.PingInterval = 5 * time.Second
	}
	if c.PingTimeout <= 0 {
		c.PingTimeout = 3 * time.Second
	}
	if c.MaxMissedPings <= 0 {
		c.MaxMissedPings = 3
	}
}

// IsEnabled 检查健康检查是否已配置
func (c *HealthCheckConfig) IsEnabled() bool {
	return c.PingInterval > 0
}

// PingMessage 健康检查 Ping 消息
type PingMessage struct {
	Timestamp int64 `json:"timestamp"`
}

// PongMessage 健康检查 Pong 响应
type PongMessage struct {
	Timestamp int64 `json:"timestamp"`
}

// healthChecker 连接健康检查器
type healthChecker struct {
	endpoint    *Endpoint
	config      HealthCheckConfig
	missedPings int32 // atomic
	stopChan    chan struct{}
	stopped     int32 // atomic
	mu          sync.Mutex
}

func newHealthChecker(ep *Endpoint, cfg HealthCheckConfig) *healthChecker {
	cfg.defaults()
	return &healthChecker{
		endpoint: ep,
		config:   cfg,
		stopChan: make(chan struct{}),
	}
}

// Start 启动健康检查循环
func (hc *healthChecker) Start() {
	go hc.loop()
}

// Stop 停止健康检查
func (hc *healthChecker) Stop() {
	if atomic.CompareAndSwapInt32(&hc.stopped, 0, 1) {
		close(hc.stopChan)
	}
}

// OnPong 收到 Pong 响应时调用，重置 missedPings 计数器
func (hc *healthChecker) OnPong() {
	atomic.StoreInt32(&hc.missedPings, 0)
}

func (hc *healthChecker) loop() {
	ticker := time.NewTicker(hc.config.PingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-hc.stopChan:
			return
		case <-ticker.C:
			hc.sendPing()
			missed := atomic.AddInt32(&hc.missedPings, 1)
			if int(missed) > hc.config.MaxMissedPings {
				log.Warn("Health check failed for %s: %d missed pings", hc.endpoint.address, missed)
				hc.endpoint.clearConn()
				atomic.StoreInt32(&hc.missedPings, 0)
			}
		}
	}
}

func (hc *healthChecker) sendPing() {
	ping := &RemoteMessage{
		Message:  &PingMessage{Timestamp: time.Now().UnixMilli()},
		TypeName: "PingMessage",
	}
	// 直接入 sendChan，不阻塞
	select {
	case hc.endpoint.sendChan <- ping:
	default:
		log.Debug("Health check ping dropped (send channel full)")
	}
}
