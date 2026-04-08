package gate

import (
	"fmt"
	"sync/atomic"
	"time"
)

// FilterResult 安全过滤器的处理结果
type FilterResult int

const (
	// FilterPass 通过，继续下一个过滤器
	FilterPass FilterResult = iota
	// FilterReject 拒绝当前消息（不断开连接）
	FilterReject
	// FilterKick 踢出客户端（断开连接）
	FilterKick
)

// SecurityFilter 安全过滤器接口
// 每个过滤器实现一种安全策略，组成责任链
type SecurityFilter interface {
	// Name 过滤器名称（用于日志和指标）
	Name() string
	// OnConnect 连接建立时调用，返回 error 表示拒绝连接
	OnConnect(ctx *SecurityContext) error
	// OnMessage 收到消息时调用
	OnMessage(ctx *SecurityContext, data []byte) FilterResult
	// OnDisconnect 连接断开时调用（清理资源）
	OnDisconnect(ctx *SecurityContext)
}

// SecurityContext 每连接安全上下文
type SecurityContext struct {
	RemoteAddr    string
	ConnID        string
	Authenticated bool
	UserID        string
	LastSeqNum    uint64
	ConnectedAt   time.Time
	Violations    int32 // 原子操作
	Metadata      map[string]interface{}
}

// AddViolation 原子地增加违规计数
func (ctx *SecurityContext) AddViolation() int32 {
	return atomic.AddInt32(&ctx.Violations, 1)
}

// SecurityChain 安全过滤器链
type SecurityChain struct {
	filters []SecurityFilter
}

// NewSecurityChain 创建安全过滤器链
func NewSecurityChain(filters ...SecurityFilter) *SecurityChain {
	return &SecurityChain{filters: filters}
}

// ProcessConnect 处理新连接，任一过滤器拒绝则返回 error
func (sc *SecurityChain) ProcessConnect(ctx *SecurityContext) error {
	if sc == nil || len(sc.filters) == 0 {
		return nil
	}
	for _, f := range sc.filters {
		if err := f.OnConnect(ctx); err != nil {
			return fmt.Errorf("[%s] %w", f.Name(), err)
		}
	}
	return nil
}

// ProcessMessage 处理消息，返回最严格的 FilterResult
func (sc *SecurityChain) ProcessMessage(ctx *SecurityContext, data []byte) FilterResult {
	if sc == nil || len(sc.filters) == 0 {
		return FilterPass
	}
	for _, f := range sc.filters {
		result := f.OnMessage(ctx, data)
		if result != FilterPass {
			return result
		}
	}
	return FilterPass
}

// ProcessDisconnect 处理连接断开
func (sc *SecurityChain) ProcessDisconnect(ctx *SecurityContext) {
	if sc == nil || len(sc.filters) == 0 {
		return
	}
	for _, f := range sc.filters {
		f.OnDisconnect(ctx)
	}
}
