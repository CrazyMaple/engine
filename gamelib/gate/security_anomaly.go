package gate

import (
	"fmt"
	"sync"
	"time"

	"engine/log"
)

// AnomalyDetectorFilter 异常连接检测过滤器
// 短时间内累计违规超过阈值则踢出并临时封禁 IP
type AnomalyDetectorFilter struct {
	maxViolations int           // 触发封禁的违规次数阈值
	banDuration   time.Duration // 封禁时长
	bannedIPs     map[string]time.Time
	mu            sync.Mutex
}

// NewAnomalyDetectorFilter 创建异常检测过滤器
func NewAnomalyDetectorFilter(maxViolations int, banDuration time.Duration) *AnomalyDetectorFilter {
	if maxViolations <= 0 {
		maxViolations = 10
	}
	if banDuration <= 0 {
		banDuration = 5 * time.Minute
	}
	f := &AnomalyDetectorFilter{
		maxViolations: maxViolations,
		banDuration:   banDuration,
		bannedIPs:     make(map[string]time.Time),
	}
	go f.cleanupLoop()
	return f
}

func (f *AnomalyDetectorFilter) Name() string { return "anomaly_detector" }

func (f *AnomalyDetectorFilter) OnConnect(ctx *SecurityContext) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if banExpiry, banned := f.bannedIPs[ctx.RemoteAddr]; banned {
		if time.Now().Before(banExpiry) {
			return fmt.Errorf("IP %s is banned until %v", ctx.RemoteAddr, banExpiry)
		}
		delete(f.bannedIPs, ctx.RemoteAddr)
	}
	return nil
}

func (f *AnomalyDetectorFilter) OnMessage(ctx *SecurityContext, _ []byte) FilterResult {
	// 检查 IP 是否被封禁
	f.mu.Lock()
	if banExpiry, banned := f.bannedIPs[ctx.RemoteAddr]; banned && time.Now().Before(banExpiry) {
		f.mu.Unlock()
		return FilterKick
	}
	f.mu.Unlock()

	// 检查累计违规次数
	violations := ctx.Violations // 读取原子值
	if int(violations) >= f.maxViolations {
		f.mu.Lock()
		f.bannedIPs[ctx.RemoteAddr] = time.Now().Add(f.banDuration)
		f.mu.Unlock()
		log.Warn("[%s] conn=%s IP %s banned for %v (violations=%d)",
			f.Name(), ctx.ConnID, ctx.RemoteAddr, f.banDuration, violations)
		return FilterKick
	}

	return FilterPass
}

func (f *AnomalyDetectorFilter) OnDisconnect(_ *SecurityContext) {}

// IsBanned 检查 IP 是否被封禁
func (f *AnomalyDetectorFilter) IsBanned(ip string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if banExpiry, banned := f.bannedIPs[ip]; banned {
		if time.Now().Before(banExpiry) {
			return true
		}
		delete(f.bannedIPs, ip)
	}
	return false
}

func (f *AnomalyDetectorFilter) cleanupLoop() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		f.mu.Lock()
		now := time.Now()
		for ip, expiry := range f.bannedIPs {
			if now.After(expiry) {
				delete(f.bannedIPs, ip)
			}
		}
		f.mu.Unlock()
	}
}
