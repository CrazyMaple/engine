package middleware

import (
	"path"
	"reflect"
	"sync"

	"engine/actor"
	"engine/log"
)

// Permission 权限类型
type Permission int

const (
	// PermAllow 允许
	PermAllow Permission = iota
	// PermDeny 拒绝
	PermDeny
)

// ACLRule 访问控制规则
type ACLRule struct {
	// SenderPattern 发送者 PID 匹配模式（支持 glob，"" 匹配所有）
	SenderPattern string
	// MessageType 消息类型名称（reflect.Type.String() 格式，"" 匹配所有）
	MessageType string
	// Permission 权限（Allow 或 Deny）
	Permission Permission
}

// ACL 访问控制列表
type ACL struct {
	rules []ACLRule
	mu    sync.RWMutex
}

// NewACL 创建访问控制列表
func NewACL() *ACL {
	return &ACL{}
}

// AddRule 添加规则（后添加的规则优先级更高）
func (a *ACL) AddRule(rule ACLRule) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.rules = append(a.rules, rule)
}

// RemoveRule 移除指定索引的规则
func (a *ACL) RemoveRule(index int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if index >= 0 && index < len(a.rules) {
		a.rules = append(a.rules[:index], a.rules[index+1:]...)
	}
}

// ClearRules 清空所有规则
func (a *ACL) ClearRules() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.rules = nil
}

// Check 检查权限，返回最后一条匹配规则的结果（默认允许）
func (a *ACL) Check(senderPID string, msgType string) Permission {
	a.mu.RLock()
	defer a.mu.RUnlock()

	result := PermAllow
	for _, rule := range a.rules {
		if matchPattern(rule.SenderPattern, senderPID) && matchPattern(rule.MessageType, msgType) {
			result = rule.Permission
		}
	}
	return result
}

// matchPattern 使用 glob 模式匹配
func matchPattern(pattern, value string) bool {
	if pattern == "" {
		return true
	}
	matched, err := path.Match(pattern, value)
	if err != nil {
		return false
	}
	return matched
}

// aclActor ACL 中间件 Actor
type aclActor struct {
	inner actor.Actor
	acl   *ACL
}

// NewACLMiddleware 创建 ACL 中间件
// 系统生命周期消息直接放行，用户消息按 ACL 规则过滤
// 被拒绝的消息记录日志后丢弃
func NewACLMiddleware(acl *ACL) actor.ReceiverMiddleware {
	return func(next actor.Actor) actor.Actor {
		return &aclActor{inner: next, acl: acl}
	}
}

func (a *aclActor) Receive(ctx actor.Context) {
	// 系统生命周期消息直接放行
	switch ctx.Message().(type) {
	case *actor.Started, *actor.Stopping, *actor.Stopped, *actor.Restarting:
		a.inner.Receive(ctx)
		return
	}

	senderID := ""
	if ctx.Sender() != nil {
		senderID = ctx.Sender().String()
	}
	msgType := reflect.TypeOf(ctx.Message()).String()

	if a.acl.Check(senderID, msgType) == PermDeny {
		log.Debug("[acl] denied msg=%s from=%s to=%s", msgType, senderID, ctx.Self())
		return
	}

	a.inner.Receive(ctx)
}
