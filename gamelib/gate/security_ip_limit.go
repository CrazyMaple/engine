package gate

import (
	"fmt"
	"sync"
	"time"
)

// IPRateLimitFilter IP 连接频率限制过滤器
// 限制同一 IP 在时间窗口内的最大连接数
type IPRateLimitFilter struct {
	maxConnsPerWindow int
	window            time.Duration
	tracker           map[string]*ipTracker
	mu                sync.Mutex
}

type ipTracker struct {
	count    int
	windowStart time.Time
}

// NewIPRateLimitFilter 创建 IP 限流过滤器
// maxConns: 每个窗口的最大连接数, window: 时间窗口（如 1*time.Second）
func NewIPRateLimitFilter(maxConns int, window time.Duration) *IPRateLimitFilter {
	if maxConns <= 0 {
		maxConns = 10
	}
	if window <= 0 {
		window = time.Second
	}
	f := &IPRateLimitFilter{
		maxConnsPerWindow: maxConns,
		window:            window,
		tracker:           make(map[string]*ipTracker),
	}
	// 启动清理协程
	go f.cleanupLoop()
	return f
}

func (f *IPRateLimitFilter) Name() string { return "ip_rate_limit" }

func (f *IPRateLimitFilter) OnConnect(ctx *SecurityContext) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	ip := ctx.RemoteAddr
	now := time.Now()

	t, ok := f.tracker[ip]
	if !ok || now.Sub(t.windowStart) > f.window {
		f.tracker[ip] = &ipTracker{count: 1, windowStart: now}
		return nil
	}

	t.count++
	if t.count > f.maxConnsPerWindow {
		return fmt.Errorf("IP %s exceeded connection rate limit (%d/%v)", ip, f.maxConnsPerWindow, f.window)
	}
	return nil
}

func (f *IPRateLimitFilter) OnMessage(_ *SecurityContext, _ []byte) FilterResult {
	return FilterPass
}

func (f *IPRateLimitFilter) OnDisconnect(_ *SecurityContext) {}

func (f *IPRateLimitFilter) cleanupLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		f.mu.Lock()
		now := time.Now()
		for ip, t := range f.tracker {
			if now.Sub(t.windowStart) > f.window*2 {
				delete(f.tracker, ip)
			}
		}
		f.mu.Unlock()
	}
}
