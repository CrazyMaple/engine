package testkit

import (
	"io"
	"math/rand"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// --- 远程通信测试辅助 ---
// 模拟网络分区、延迟注入、丢包等网络故障

// FaultProxy TCP 故障注入代理
// 在源端口和目标端口之间创建代理，可动态注入网络故障
type FaultProxy struct {
	listenAddr string
	targetAddr string
	listener   net.Listener
	mu         sync.RWMutex
	conns      []net.Conn
	closed     int32

	// 故障注入配置
	latency    time.Duration // 附加延迟
	dropRate   float64       // 丢包率 0.0-1.0
	partitioned bool         // 网络分区（完全中断）
	bandwidth  int           // 带宽限制 bytes/sec，0=无限制
}

// NewFaultProxy 创建故障注入代理
// 在 listenAddr 监听，转发到 targetAddr
func NewFaultProxy(listenAddr, targetAddr string) *FaultProxy {
	return &FaultProxy{
		listenAddr: listenAddr,
		targetAddr: targetAddr,
	}
}

// Start 启动代理
func (p *FaultProxy) Start() error {
	l, err := net.Listen("tcp", p.listenAddr)
	if err != nil {
		return err
	}
	p.listener = l
	go p.acceptLoop()
	return nil
}

// ListenAddr 返回实际监听地址
func (p *FaultProxy) ListenAddr() string {
	if p.listener == nil {
		return p.listenAddr
	}
	return p.listener.Addr().String()
}

// SetLatency 设置附加延迟
func (p *FaultProxy) SetLatency(d time.Duration) {
	p.mu.Lock()
	p.latency = d
	p.mu.Unlock()
}

// SetDropRate 设置丢包率（0.0 = 不丢包，1.0 = 全丢）
func (p *FaultProxy) SetDropRate(rate float64) {
	p.mu.Lock()
	p.dropRate = rate
	p.mu.Unlock()
}

// SetPartitioned 设置网络分区（完全中断通信）
func (p *FaultProxy) SetPartitioned(partitioned bool) {
	p.mu.Lock()
	p.partitioned = partitioned
	if partitioned {
		// 关闭所有现有连接
		for _, c := range p.conns {
			c.Close()
		}
		p.conns = nil
	}
	p.mu.Unlock()
}

// SetBandwidth 设置带宽限制（bytes/sec），0 = 无限制
func (p *FaultProxy) SetBandwidth(bytesPerSec int) {
	p.mu.Lock()
	p.bandwidth = bytesPerSec
	p.mu.Unlock()
}

// Reset 重置所有故障注入
func (p *FaultProxy) Reset() {
	p.mu.Lock()
	p.latency = 0
	p.dropRate = 0
	p.partitioned = false
	p.bandwidth = 0
	p.mu.Unlock()
}

// ActiveConns 返回当前活跃连接数
func (p *FaultProxy) ActiveConns() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.conns)
}

// Stop 关闭代理
func (p *FaultProxy) Stop() {
	if !atomic.CompareAndSwapInt32(&p.closed, 0, 1) {
		return
	}
	if p.listener != nil {
		p.listener.Close()
	}
	p.mu.Lock()
	for _, c := range p.conns {
		c.Close()
	}
	p.conns = nil
	p.mu.Unlock()
}

func (p *FaultProxy) acceptLoop() {
	for atomic.LoadInt32(&p.closed) == 0 {
		conn, err := p.listener.Accept()
		if err != nil {
			return
		}

		p.mu.RLock()
		partitioned := p.partitioned
		p.mu.RUnlock()

		if partitioned {
			conn.Close()
			continue
		}

		// 连接到目标
		target, err := net.DialTimeout("tcp", p.targetAddr, 5*time.Second)
		if err != nil {
			conn.Close()
			continue
		}

		p.mu.Lock()
		p.conns = append(p.conns, conn, target)
		p.mu.Unlock()

		// 双向转发
		go p.relay(conn, target)
		go p.relay(target, conn)
	}
}

func (p *FaultProxy) relay(src, dst net.Conn) {
	buf := make([]byte, 4096)
	defer func() {
		src.Close()
		dst.Close()
		p.removeConn(src)
		p.removeConn(dst)
	}()

	for {
		n, err := src.Read(buf)
		if err != nil {
			return
		}

		p.mu.RLock()
		latency := p.latency
		dropRate := p.dropRate
		partitioned := p.partitioned
		bandwidth := p.bandwidth
		p.mu.RUnlock()

		if partitioned {
			return
		}

		// 丢包模拟
		if dropRate > 0 && rand.Float64() < dropRate {
			continue
		}

		// 延迟注入
		if latency > 0 {
			time.Sleep(latency)
		}

		// 带宽限制
		if bandwidth > 0 {
			sleepTime := time.Duration(float64(n) / float64(bandwidth) * float64(time.Second))
			time.Sleep(sleepTime)
		}

		if _, err := dst.Write(buf[:n]); err != nil {
			return
		}
	}
}

func (p *FaultProxy) removeConn(c net.Conn) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i, conn := range p.conns {
		if conn == c {
			p.conns[i] = p.conns[len(p.conns)-1]
			p.conns = p.conns[:len(p.conns)-1]
			return
		}
	}
}

// --- 快捷构造函数 ---

// NewFaultProxyAuto 创建使用自动分配端口的代理
func NewFaultProxyAuto(targetAddr string) *FaultProxy {
	return NewFaultProxy("127.0.0.1:0", targetAddr)
}

// PartitionNodes 在两个节点之间创建网络分区
// 返回一个清理函数用于恢复连接
func PartitionNodes(proxy *FaultProxy) func() {
	proxy.SetPartitioned(true)
	return func() { proxy.SetPartitioned(false) }
}

// SlowNetwork 模拟慢速网络
func SlowNetwork(proxy *FaultProxy, latency time.Duration, dropRate float64) func() {
	proxy.SetLatency(latency)
	proxy.SetDropRate(dropRate)
	return func() { proxy.Reset() }
}

// --- FaultInjector 多代理管理 ---

// FaultInjector 管理多个 FaultProxy，简化集群故障注入
type FaultInjector struct {
	proxies map[string]*FaultProxy // key: "nodeA->nodeB"
}

// NewFaultInjector 创建故障注入管理器
func NewFaultInjector() *FaultInjector {
	return &FaultInjector{
		proxies: make(map[string]*FaultProxy),
	}
}

// AddProxy 注册代理
func (fi *FaultInjector) AddProxy(name string, proxy *FaultProxy) {
	fi.proxies[name] = proxy
}

// PartitionAll 对所有代理注入网络分区
func (fi *FaultInjector) PartitionAll() {
	for _, p := range fi.proxies {
		p.SetPartitioned(true)
	}
}

// HealAll 恢复所有代理
func (fi *FaultInjector) HealAll() {
	for _, p := range fi.proxies {
		p.Reset()
	}
}

// InjectLatency 对所有代理注入延迟
func (fi *FaultInjector) InjectLatency(d time.Duration) {
	for _, p := range fi.proxies {
		p.SetLatency(d)
	}
}

// StopAll 关闭所有代理
func (fi *FaultInjector) StopAll() {
	for _, p := range fi.proxies {
		p.Stop()
	}
}

// Ensure interface for io usage
var _ io.Closer = (*FaultProxy)(nil)

// Close implements io.Closer
func (p *FaultProxy) Close() error {
	p.Stop()
	return nil
}
