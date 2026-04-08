package remote

import (
	"sync"
	"sync/atomic"
	"time"

	"engine/log"
)

// ConnPoolConfig 连接池动态扩缩容配置
type ConnPoolConfig struct {
	// MinConns 最小连接数（默认 1）
	MinConns int
	// MaxConns 最大连接数（默认 8）
	MaxConns int
	// ScaleUpThreshold 待发送消息数超过此阈值时扩容（默认 100）
	ScaleUpThreshold int
	// ScaleDownTimeout 连接空闲超过此时间后缩容（默认 30s）
	ScaleDownTimeout time.Duration
	// ScaleCheckInterval 扩缩容检查间隔（默认 5s）
	ScaleCheckInterval time.Duration
}

// DefaultConnPoolConfig 返回默认连接池配置
func DefaultConnPoolConfig() ConnPoolConfig {
	return ConnPoolConfig{
		MinConns:           1,
		MaxConns:           8,
		ScaleUpThreshold:   100,
		ScaleDownTimeout:   30 * time.Second,
		ScaleCheckInterval: 5 * time.Second,
	}
}

func (c *ConnPoolConfig) defaults() {
	if c.MinConns <= 0 {
		c.MinConns = 1
	}
	if c.MaxConns <= 0 {
		c.MaxConns = 8
	}
	if c.MaxConns < c.MinConns {
		c.MaxConns = c.MinConns
	}
	if c.ScaleUpThreshold <= 0 {
		c.ScaleUpThreshold = 100
	}
	if c.ScaleDownTimeout <= 0 {
		c.ScaleDownTimeout = 30 * time.Second
	}
	if c.ScaleCheckInterval <= 0 {
		c.ScaleCheckInterval = 5 * time.Second
	}
}

// IsEnabled 检查连接池是否启用（MaxConns > 1 视为启用）
func (c *ConnPoolConfig) IsEnabled() bool {
	return c.MaxConns > 1
}

// pooledEndpoint 连接池中的单个连接实例
type pooledEndpoint struct {
	endpoint *Endpoint
	lastUsed time.Time
	mu       sync.Mutex
}

func (pe *pooledEndpoint) touch() {
	pe.mu.Lock()
	pe.lastUsed = time.Now()
	pe.mu.Unlock()
}

func (pe *pooledEndpoint) idleDuration() time.Duration {
	pe.mu.Lock()
	d := time.Since(pe.lastUsed)
	pe.mu.Unlock()
	return d
}

// connPool 管理到单个远端地址的多条连接
type connPool struct {
	address   string
	config    ConnPoolConfig
	endpoints []*pooledEndpoint
	mu        sync.RWMutex
	counter   uint64 // atomic: round-robin 计数器
	pending   int64  // atomic: 待发送消息计数
	stopChan  chan struct{}
	stopped   int32
	// 用于创建新 Endpoint 的配置
	signer *MessageSigner
	tlsCfg interface{} // *network.TLSConfig, 避免循环引用
	codec  *RemoteCodec
}

func newConnPool(address string, cfg ConnPoolConfig) *connPool {
	cfg.defaults()
	return &connPool{
		address:  address,
		config:   cfg,
		stopChan: make(chan struct{}),
	}
}

// Send 通过 round-robin 选择连接发送消息
func (p *connPool) Send(msg *RemoteMessage) {
	atomic.AddInt64(&p.pending, 1)

	p.mu.RLock()
	n := len(p.endpoints)
	if n == 0 {
		p.mu.RUnlock()
		atomic.AddInt64(&p.pending, -1)
		log.Debug("ConnPool: no endpoints available for %s", p.address)
		return
	}
	idx := atomic.AddUint64(&p.counter, 1) % uint64(n)
	pe := p.endpoints[idx]
	p.mu.RUnlock()

	pe.touch()
	pe.endpoint.Send(msg)
	atomic.AddInt64(&p.pending, -1)
}

// Start 初始化连接池并启动扩缩容循环
func (p *connPool) Start() {
	// 创建最小数量的连接
	for i := 0; i < p.config.MinConns; i++ {
		p.addEndpoint()
	}
	go p.scaleLoop()
}

// Stop 停止连接池
func (p *connPool) Stop() {
	if atomic.CompareAndSwapInt32(&p.stopped, 0, 1) {
		close(p.stopChan)
	}
	p.mu.Lock()
	for _, pe := range p.endpoints {
		pe.endpoint.Stop()
	}
	p.endpoints = nil
	p.mu.Unlock()
}

// Size 返回当前连接数
func (p *connPool) Size() int {
	p.mu.RLock()
	n := len(p.endpoints)
	p.mu.RUnlock()
	return n
}

func (p *connPool) addEndpoint() {
	ep := NewEndpoint(p.address)
	ep.signer = p.signer
	ep.codec = p.codec
	ep.Start()

	pe := &pooledEndpoint{
		endpoint: ep,
		lastUsed: time.Now(),
	}

	p.mu.Lock()
	p.endpoints = append(p.endpoints, pe)
	p.mu.Unlock()

	log.Debug("ConnPool: added endpoint for %s (total: %d)", p.address, p.Size())
}

func (p *connPool) removeIdle() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.endpoints) <= p.config.MinConns {
		return
	}

	// 从后往前检查，移除空闲的
	for i := len(p.endpoints) - 1; i >= p.config.MinConns; i-- {
		if p.endpoints[i].idleDuration() > p.config.ScaleDownTimeout {
			p.endpoints[i].endpoint.Stop()
			p.endpoints = append(p.endpoints[:i], p.endpoints[i+1:]...)
			log.Debug("ConnPool: removed idle endpoint for %s (total: %d)", p.address, len(p.endpoints))
			break // 每次只移除一个
		}
	}
}

func (p *connPool) scaleLoop() {
	ticker := time.NewTicker(p.config.ScaleCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-p.stopChan:
			return
		case <-ticker.C:
			pending := atomic.LoadInt64(&p.pending)
			size := p.Size()

			// 扩容：待发送消息过多
			if int(pending) > p.config.ScaleUpThreshold && size < p.config.MaxConns {
				p.addEndpoint()
			}

			// 缩容：移除空闲连接
			p.removeIdle()
		}
	}
}
