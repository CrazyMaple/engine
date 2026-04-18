package middleware

import (
	"reflect"
	"strings"
	"sync"

	"engine/actor"
	"engine/log"
)

// RBAC 基于角色的访问控制器。
//
// 模型：
//   发送者 (sender PID) --匹配 pattern--> 角色 (Role) --授予--> 权限 (Permission)
//   消息类型 --声明--> 所需权限
//
// 检查流程：
//   1. 命名空间隔离：若 sender 与 receiver 分属不同命名空间，直接拒绝
//   2. 权限检查：sender 持有的任一角色拥有消息所需权限则放行
//
// 所有写操作加锁，支持运行时动态变更（由 Dashboard API 驱动）。
type RBAC struct {
	mu          sync.RWMutex
	rolePerms   map[string]map[string]struct{} // role → set<permission>
	senderRoles []senderRoleBinding            // 顺序保留（便于 Unbind 匹配）
	msgPerm     map[string]string              // message type → required permission
	namespaces  map[string]string              // pid prefix → namespace
	defaultDeny bool                           // 未声明所需权限的消息的默认策略
}

type senderRoleBinding struct {
	pattern string
	roles   []string
}

// NewRBAC 创建 RBAC 控制器。默认策略为放行未声明权限要求的消息。
func NewRBAC() *RBAC {
	return &RBAC{
		rolePerms:  make(map[string]map[string]struct{}),
		msgPerm:    make(map[string]string),
		namespaces: make(map[string]string),
	}
}

// SetDefaultDeny 设置默认策略。
// true：未通过 RequirePermission 声明的消息类型默认拒绝（严格模式）。
// false（默认）：未声明的消息放行，仅对声明了权限要求的消息做校验。
func (r *RBAC) SetDefaultDeny(deny bool) {
	r.mu.Lock()
	r.defaultDeny = deny
	r.mu.Unlock()
}

// GrantPermission 赋予角色权限（幂等）。
func (r *RBAC) GrantPermission(role, permission string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	perms, ok := r.rolePerms[role]
	if !ok {
		perms = make(map[string]struct{})
		r.rolePerms[role] = perms
	}
	perms[permission] = struct{}{}
}

// RevokePermission 撤销角色某项权限。
func (r *RBAC) RevokePermission(role, permission string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if perms, ok := r.rolePerms[role]; ok {
		delete(perms, permission)
	}
}

// RemoveRole 删除角色及其所有权限。
func (r *RBAC) RemoveRole(role string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.rolePerms, role)
}

// BindSenderRole 将发送者 PID pattern（glob）绑定到一个或多个角色。
// 多次对同一 pattern 调用会追加为新绑定记录。
func (r *RBAC) BindSenderRole(senderPattern string, roles ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.senderRoles = append(r.senderRoles, senderRoleBinding{
		pattern: senderPattern,
		roles:   append([]string(nil), roles...),
	})
}

// UnbindSenderRole 移除所有以该 pattern 绑定的记录。
func (r *RBAC) UnbindSenderRole(senderPattern string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.senderRoles[:0]
	for _, b := range r.senderRoles {
		if b.pattern != senderPattern {
			out = append(out, b)
		}
	}
	r.senderRoles = out
}

// RequirePermission 声明发送某种消息类型所需的权限。
// messageType 使用 reflect.TypeOf(msg).String() 的格式，例如 "*gm.BanRequest"。
func (r *RBAC) RequirePermission(messageType, permission string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.msgPerm[messageType] = permission
}

// ClearRequirement 移除消息类型的权限要求（恢复默认策略）。
func (r *RBAC) ClearRequirement(messageType string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.msgPerm, messageType)
}

// SetNamespace 将 PID 前缀归属到指定命名空间（多租户隔离）。
// 同一命名空间内的 Actor 互相可达；跨命名空间默认拒绝。
func (r *RBAC) SetNamespace(pidPrefix, namespace string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.namespaces[pidPrefix] = namespace
}

// RemoveNamespace 解除 PID 前缀的命名空间归属。
func (r *RBAC) RemoveNamespace(pidPrefix string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.namespaces, pidPrefix)
}

// Namespace 查询 PID 所属的命名空间（最长前缀匹配）。
func (r *RBAC) Namespace(pid string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.namespaceLocked(pid)
}

func (r *RBAC) namespaceLocked(pid string) string {
	var bestPrefix string
	var bestNs string
	for prefix, ns := range r.namespaces {
		if strings.HasPrefix(pid, prefix) && len(prefix) > len(bestPrefix) {
			bestPrefix = prefix
			bestNs = ns
		}
	}
	return bestNs
}

// RolesOf 返回发送者匹配到的全部角色（去重，顺序保持首次出现）。
func (r *RBAC) RolesOf(senderPID string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.rolesOfLocked(senderPID)
}

func (r *RBAC) rolesOfLocked(senderPID string) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, b := range r.senderRoles {
		if !matchPattern(b.pattern, senderPID) {
			continue
		}
		for _, role := range b.roles {
			if _, dup := seen[role]; dup {
				continue
			}
			seen[role] = struct{}{}
			out = append(out, role)
		}
	}
	return out
}

// HasPermission 判断发送者是否拥有指定权限。
func (r *RBAC) HasPermission(senderPID, permission string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.hasPermissionLocked(senderPID, permission)
}

func (r *RBAC) hasPermissionLocked(senderPID, permission string) bool {
	for _, role := range r.rolesOfLocked(senderPID) {
		if perms, ok := r.rolePerms[role]; ok {
			if _, has := perms[permission]; has {
				return true
			}
		}
	}
	return false
}

// CheckMessage 判断发送者是否可向接收者发送指定类型消息。
// 返回 (allow, reason)；allow=true 时 reason 为空。
func (r *RBAC) CheckMessage(senderPID, receiverPID, msgType string) (bool, string) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// 命名空间隔离：双方都有命名空间且不一致则拒绝
	senderNS := r.namespaceLocked(senderPID)
	receiverNS := r.namespaceLocked(receiverPID)
	if senderNS != "" && receiverNS != "" && senderNS != receiverNS {
		return false, "cross-namespace: " + senderNS + "->" + receiverNS
	}

	perm, required := r.msgPerm[msgType]
	if !required {
		if r.defaultDeny {
			return false, "no permission declared for " + msgType
		}
		return true, ""
	}
	if r.hasPermissionLocked(senderPID, perm) {
		return true, ""
	}
	return false, "missing permission: " + perm
}

// rbacActor 中间件 Actor 包装。
type rbacActor struct {
	inner actor.Actor
	rbac  *RBAC
}

// NewRBACMiddleware 创建 RBAC 中间件。
// 系统生命周期消息（Started/Stopping/Stopped/Restarting）直接放行，
// 仅对用户消息做权限校验。被拒绝的消息记录日志后丢弃。
func NewRBACMiddleware(rbac *RBAC) actor.ReceiverMiddleware {
	return func(next actor.Actor) actor.Actor {
		return &rbacActor{inner: next, rbac: rbac}
	}
}

func (a *rbacActor) Receive(ctx actor.Context) {
	switch ctx.Message().(type) {
	case *actor.Started, *actor.Stopping, *actor.Stopped, *actor.Restarting:
		a.inner.Receive(ctx)
		return
	}

	senderID := ""
	if ctx.Sender() != nil {
		senderID = ctx.Sender().String()
	}
	receiverID := ""
	if ctx.Self() != nil {
		receiverID = ctx.Self().String()
	}
	msgType := reflect.TypeOf(ctx.Message()).String()

	if ok, reason := a.rbac.CheckMessage(senderID, receiverID, msgType); !ok {
		log.Debug("[rbac] denied msg=%s from=%s to=%s reason=%s", msgType, senderID, receiverID, reason)
		return
	}
	a.inner.Receive(ctx)
}
