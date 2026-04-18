package middleware

import (
	"sync"
	"testing"
	"time"

	"engine/actor"
)

func TestRBAC_DefaultAllowWithoutRules(t *testing.T) {
	r := NewRBAC()
	ok, reason := r.CheckMessage("any", "target", "*SomeMsg")
	if !ok {
		t.Fatalf("should allow when no rules: %s", reason)
	}
}

func TestRBAC_DefaultDenyWithoutDeclaration(t *testing.T) {
	r := NewRBAC()
	r.SetDefaultDeny(true)
	ok, _ := r.CheckMessage("any", "target", "*UnknownMsg")
	if ok {
		t.Fatal("default deny should reject undeclared message")
	}
}

func TestRBAC_GrantAndCheckPermission(t *testing.T) {
	r := NewRBAC()
	r.GrantPermission("admin", "user:ban")
	r.BindSenderRole("admin-*", "admin")

	if !r.HasPermission("admin-1", "user:ban") {
		t.Error("admin-1 should have user:ban")
	}
	if r.HasPermission("player-1", "user:ban") {
		t.Error("player-1 should not have user:ban")
	}
}

func TestRBAC_RequirePermissionGate(t *testing.T) {
	r := NewRBAC()
	r.GrantPermission("gm", "gm:kick")
	r.BindSenderRole("gm-*", "gm")
	r.RequirePermission("*KickMsg", "gm:kick")

	// gm-alice 有 gm:kick 权限
	if ok, _ := r.CheckMessage("gm-alice", "target", "*KickMsg"); !ok {
		t.Error("gm-alice should be allowed to send *KickMsg")
	}
	// player-bob 无权限
	if ok, _ := r.CheckMessage("player-bob", "target", "*KickMsg"); ok {
		t.Error("player-bob should be denied")
	}
	// 未声明权限的消息在默认策略下放行
	if ok, _ := r.CheckMessage("player-bob", "target", "*ChatMsg"); !ok {
		t.Error("chat msg should be allowed by default")
	}
}

func TestRBAC_RevokePermission(t *testing.T) {
	r := NewRBAC()
	r.GrantPermission("admin", "cfg:reload")
	r.BindSenderRole("admin", "admin")

	if !r.HasPermission("admin", "cfg:reload") {
		t.Fatal("expected permission granted")
	}
	r.RevokePermission("admin", "cfg:reload")
	if r.HasPermission("admin", "cfg:reload") {
		t.Fatal("expected permission revoked")
	}
}

func TestRBAC_RemoveRole(t *testing.T) {
	r := NewRBAC()
	r.GrantPermission("editor", "doc:edit")
	r.GrantPermission("editor", "doc:delete")
	r.BindSenderRole("editor-*", "editor")

	r.RemoveRole("editor")
	if r.HasPermission("editor-1", "doc:edit") {
		t.Fatal("all editor perms should be gone")
	}
}

func TestRBAC_UnbindSenderRole(t *testing.T) {
	r := NewRBAC()
	r.GrantPermission("gm", "gm:any")
	r.BindSenderRole("gm-*", "gm")

	if !r.HasPermission("gm-1", "gm:any") {
		t.Fatal("expected permission before unbind")
	}
	r.UnbindSenderRole("gm-*")
	if r.HasPermission("gm-1", "gm:any") {
		t.Fatal("expected no permission after unbind")
	}
}

func TestRBAC_ClearRequirement(t *testing.T) {
	r := NewRBAC()
	r.RequirePermission("*Msg", "x:y")
	r.ClearRequirement("*Msg")
	if ok, _ := r.CheckMessage("anyone", "target", "*Msg"); !ok {
		t.Fatal("expected allow after clearing requirement")
	}
}

func TestRBAC_MultipleRoles(t *testing.T) {
	r := NewRBAC()
	r.GrantPermission("reader", "data:read")
	r.GrantPermission("writer", "data:write")
	r.BindSenderRole("service-*", "reader", "writer")

	if !r.HasPermission("service-1", "data:read") {
		t.Error("should have read")
	}
	if !r.HasPermission("service-1", "data:write") {
		t.Error("should have write")
	}
}

func TestRBAC_RolesOfDedup(t *testing.T) {
	r := NewRBAC()
	r.BindSenderRole("*", "a")
	r.BindSenderRole("x-*", "a", "b")

	roles := r.RolesOf("x-1")
	if len(roles) != 2 {
		t.Fatalf("expected 2 roles, got %v", roles)
	}
}

func TestRBAC_Namespace_CrossDenied(t *testing.T) {
	r := NewRBAC()
	r.SetNamespace("tenant-a/", "tenant-a")
	r.SetNamespace("tenant-b/", "tenant-b")

	ok, reason := r.CheckMessage("tenant-a/actor1", "tenant-b/actor2", "*Msg")
	if ok {
		t.Fatal("expected cross-namespace denial")
	}
	if reason == "" {
		t.Fatal("expected reason text")
	}
}

func TestRBAC_Namespace_SameAllowed(t *testing.T) {
	r := NewRBAC()
	r.SetNamespace("tenant-a/", "tenant-a")

	ok, _ := r.CheckMessage("tenant-a/a1", "tenant-a/a2", "*Msg")
	if !ok {
		t.Fatal("same namespace should be allowed")
	}
}

func TestRBAC_Namespace_OnlyOneSet(t *testing.T) {
	r := NewRBAC()
	r.SetNamespace("tenant-a/", "tenant-a")
	// 发送者没有命名空间 → 放行
	if ok, _ := r.CheckMessage("public/svc", "tenant-a/a1", "*Msg"); !ok {
		t.Error("no sender ns should allow")
	}
}

func TestRBAC_Namespace_LongestPrefixWins(t *testing.T) {
	r := NewRBAC()
	r.SetNamespace("tenant/", "parent")
	r.SetNamespace("tenant/a/", "child-a")

	if ns := r.Namespace("tenant/a/svc"); ns != "child-a" {
		t.Fatalf("expected child-a, got %s", ns)
	}
	if ns := r.Namespace("tenant/b/svc"); ns != "parent" {
		t.Fatalf("expected parent, got %s", ns)
	}
}

func TestRBAC_RemoveNamespace(t *testing.T) {
	r := NewRBAC()
	r.SetNamespace("t/", "t")
	r.RemoveNamespace("t/")
	if r.Namespace("t/x") != "" {
		t.Fatal("expected empty namespace after remove")
	}
}

func TestRBAC_ConcurrentChanges(t *testing.T) {
	r := NewRBAC()
	r.GrantPermission("role1", "perm1")
	r.BindSenderRole("s-*", "role1")
	r.RequirePermission("*Msg", "perm1")

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_, _ = r.CheckMessage("s-1", "t", "*Msg")
				r.HasPermission("s-1", "perm1")
			}
		}()
	}
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				r.GrantPermission("role1", "extra")
				r.RevokePermission("role1", "extra")
			}
		}(i)
	}
	wg.Wait()
}

// 集成测试：通过 Props.WithReceiverMiddleware 实际应用到 Actor。

type recordActor struct {
	mu       sync.Mutex
	received []interface{}
}

func (a *recordActor) Receive(ctx actor.Context) {
	// 过滤系统生命周期消息，只记录业务消息
	switch ctx.Message().(type) {
	case *actor.Started, *actor.Stopping, *actor.Stopped, *actor.Restarting:
		return
	}
	a.mu.Lock()
	a.received = append(a.received, ctx.Message())
	a.mu.Unlock()
}

func (a *recordActor) count() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.received)
}

type protectedMsg struct{ Body string }
type publicMsg struct{ Body string }

func TestRBAC_MiddlewareBlocksUnauthorized(t *testing.T) {
	rec := &recordActor{}

	rb := NewRBAC()
	rb.RequirePermission("*middleware.protectedMsg", "ops:execute")
	// 不绑定任何角色 → 默认 sender 没有权限

	system := actor.DefaultSystem()
	props := actor.PropsFromProducer(func() actor.Actor { return rec }).
		WithReceiverMiddleware(NewRBACMiddleware(rb))
	pid := system.Root.Spawn(props)
	defer system.Root.Stop(pid)

	system.Root.Send(pid, &publicMsg{Body: "hi"})
	system.Root.Send(pid, &protectedMsg{Body: "attack"})
	time.Sleep(80 * time.Millisecond)

	// 只有 publicMsg 应该被记录
	if n := rec.count(); n != 1 {
		t.Fatalf("expected 1 received msg, got %d", n)
	}
}

func TestRBAC_MiddlewareAllowsAuthorized(t *testing.T) {
	rec := &recordActor{}

	rb := NewRBAC()
	rb.GrantPermission("root", "ops:execute")
	rb.BindSenderRole("*", "root") // 所有发送者授予 root 角色（测试用）
	rb.RequirePermission("*middleware.protectedMsg", "ops:execute")

	system := actor.DefaultSystem()
	props := actor.PropsFromProducer(func() actor.Actor { return rec }).
		WithReceiverMiddleware(NewRBACMiddleware(rb))
	pid := system.Root.Spawn(props)
	defer system.Root.Stop(pid)

	system.Root.Send(pid, &protectedMsg{Body: "ok"})
	time.Sleep(80 * time.Millisecond)

	if n := rec.count(); n != 1 {
		t.Fatalf("expected 1 received msg, got %d", n)
	}
}
