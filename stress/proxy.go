package stress

import (
	"io"
	"net"
	"sync"
	"sync/atomic"

	"engine/log"
)

// Proxy TCP 代理，支持编程式阻断/恢复，用于模拟网络分区
type Proxy struct {
	listenAddr string
	targetAddr string
	blocked    atomic.Bool
	listener   net.Listener
	conns      []net.Conn
	mu         sync.Mutex
	stopChan   chan struct{}
}

// NewProxy 创建 TCP 代理
func NewProxy(listenAddr, targetAddr string) *Proxy {
	return &Proxy{
		listenAddr: listenAddr,
		targetAddr: targetAddr,
		stopChan:   make(chan struct{}),
	}
}

// Start 启动代理
func (p *Proxy) Start() error {
	ln, err := net.Listen("tcp", p.listenAddr)
	if err != nil {
		return err
	}
	p.listener = ln

	go p.acceptLoop()
	log.Info("[proxy] started %s -> %s", p.listenAddr, p.targetAddr)
	return nil
}

// Stop 停止代理
func (p *Proxy) Stop() {
	close(p.stopChan)
	if p.listener != nil {
		p.listener.Close()
	}
	p.mu.Lock()
	for _, conn := range p.conns {
		conn.Close()
	}
	p.conns = nil
	p.mu.Unlock()
}

// Block 阻断所有流量（模拟网络分区）
func (p *Proxy) Block() {
	p.blocked.Store(true)
	// 关闭现有连接
	p.mu.Lock()
	for _, conn := range p.conns {
		conn.Close()
	}
	p.conns = nil
	p.mu.Unlock()
	log.Info("[proxy] blocked %s -> %s", p.listenAddr, p.targetAddr)
}

// Unblock 恢复流量
func (p *Proxy) Unblock() {
	p.blocked.Store(false)
	log.Info("[proxy] unblocked %s -> %s", p.listenAddr, p.targetAddr)
}

// IsBlocked 检查是否阻断
func (p *Proxy) IsBlocked() bool {
	return p.blocked.Load()
}

// Addr 返回监听地址
func (p *Proxy) Addr() string {
	if p.listener != nil {
		return p.listener.Addr().String()
	}
	return p.listenAddr
}

func (p *Proxy) acceptLoop() {
	for {
		conn, err := p.listener.Accept()
		if err != nil {
			select {
			case <-p.stopChan:
				return
			default:
				continue
			}
		}

		if p.blocked.Load() {
			conn.Close()
			continue
		}

		go p.handleConn(conn)
	}
}

func (p *Proxy) handleConn(clientConn net.Conn) {
	if p.blocked.Load() {
		clientConn.Close()
		return
	}

	targetConn, err := net.Dial("tcp", p.targetAddr)
	if err != nil {
		clientConn.Close()
		return
	}

	p.mu.Lock()
	p.conns = append(p.conns, clientConn, targetConn)
	p.mu.Unlock()

	var wg sync.WaitGroup
	wg.Add(2)

	// 双向转发
	go func() {
		defer wg.Done()
		p.relay(clientConn, targetConn)
	}()
	go func() {
		defer wg.Done()
		p.relay(targetConn, clientConn)
	}()

	wg.Wait()
	clientConn.Close()
	targetConn.Close()
}

func (p *Proxy) relay(dst, src net.Conn) {
	buf := make([]byte, 4096)
	for {
		if p.blocked.Load() {
			return
		}
		n, err := src.Read(buf)
		if err != nil {
			return
		}
		if p.blocked.Load() {
			return
		}
		if _, err := dst.Write(buf[:n]); err != nil {
			return
		}
	}
	_ = io.Discard // keep io import
}
