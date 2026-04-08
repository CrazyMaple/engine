package gate

import (
	"encoding/binary"
	"errors"
	"testing"
	"time"
)

func TestSecurityChainEmpty(t *testing.T) {
	chain := NewSecurityChain()
	ctx := &SecurityContext{RemoteAddr: "1.2.3.4", ConnID: "test"}

	if err := chain.ProcessConnect(ctx); err != nil {
		t.Fatalf("empty chain should not reject: %v", err)
	}
	if result := chain.ProcessMessage(ctx, []byte("data")); result != FilterPass {
		t.Fatalf("empty chain should pass: %d", result)
	}
}

func TestSecurityChainNil(t *testing.T) {
	var chain *SecurityChain
	ctx := &SecurityContext{}

	if err := chain.ProcessConnect(ctx); err != nil {
		t.Fatalf("nil chain should not reject: %v", err)
	}
	if result := chain.ProcessMessage(ctx, nil); result != FilterPass {
		t.Fatalf("nil chain should pass: %d", result)
	}
}

func TestIPRateLimitFilter(t *testing.T) {
	f := NewIPRateLimitFilter(2, time.Second)

	ctx := &SecurityContext{RemoteAddr: "1.2.3.4", ConnID: "c1"}
	if err := f.OnConnect(ctx); err != nil {
		t.Fatalf("first connection should pass: %v", err)
	}

	ctx2 := &SecurityContext{RemoteAddr: "1.2.3.4", ConnID: "c2"}
	if err := f.OnConnect(ctx2); err != nil {
		t.Fatalf("second connection should pass: %v", err)
	}

	ctx3 := &SecurityContext{RemoteAddr: "1.2.3.4", ConnID: "c3"}
	if err := f.OnConnect(ctx3); err == nil {
		t.Fatal("third connection should be rejected")
	}

	// 不同 IP 不受影响
	ctx4 := &SecurityContext{RemoteAddr: "5.6.7.8", ConnID: "c4"}
	if err := f.OnConnect(ctx4); err != nil {
		t.Fatalf("different IP should pass: %v", err)
	}
}

func TestMessageValidatorFilterPacketSize(t *testing.T) {
	f := NewMessageValidatorFilter(10)
	ctx := &SecurityContext{ConnID: "test"}

	if result := f.OnMessage(ctx, make([]byte, 5)); result != FilterPass {
		t.Fatal("small packet should pass")
	}

	if result := f.OnMessage(ctx, make([]byte, 20)); result != FilterReject {
		t.Fatal("large packet should be rejected")
	}
}

func TestMessageValidatorFilterMsgIDWhitelist(t *testing.T) {
	f := NewMessageValidatorFilter(0).WithAllowedMsgIDs([]uint16{1, 2, 3})
	ctx := &SecurityContext{ConnID: "test"}

	// 构造消息：前 2 字节为 msgID
	validMsg := make([]byte, 10)
	binary.BigEndian.PutUint16(validMsg, 1)
	if result := f.OnMessage(ctx, validMsg); result != FilterPass {
		t.Fatal("valid msg ID should pass")
	}

	invalidMsg := make([]byte, 10)
	binary.BigEndian.PutUint16(invalidMsg, 99)
	if result := f.OnMessage(ctx, invalidMsg); result != FilterReject {
		t.Fatal("invalid msg ID should be rejected")
	}
}

func TestTokenFilter(t *testing.T) {
	verifier := func(token string) (string, time.Time, error) {
		if token == "valid-token" {
			return "user123", time.Now().Add(time.Hour), nil
		}
		return "", time.Time{}, errors.New("invalid token")
	}

	extractor := func(data []byte) (string, bool) {
		// 简单约定：以 "AUTH:" 开头的消息是认证消息
		if len(data) > 5 && string(data[:5]) == "AUTH:" {
			return string(data[5:]), true
		}
		return "", false
	}

	f := NewTokenFilter(verifier, extractor)

	ctx := &SecurityContext{
		ConnID:      "test",
		ConnectedAt: time.Now(),
	}

	// 未认证时发送非认证消息应被拒绝
	if result := f.OnMessage(ctx, []byte("regular message")); result != FilterReject {
		t.Fatal("non-auth message before auth should be rejected")
	}

	// 发送无效 token
	if result := f.OnMessage(ctx, []byte("AUTH:bad-token")); result != FilterReject {
		t.Fatal("invalid token should be rejected")
	}

	// 发送有效 token
	if result := f.OnMessage(ctx, []byte("AUTH:valid-token")); result != FilterPass {
		t.Fatal("valid token should pass")
	}
	if !ctx.Authenticated || ctx.UserID != "user123" {
		t.Fatalf("context should be authenticated: auth=%v user=%s", ctx.Authenticated, ctx.UserID)
	}

	// 认证后的常规消息应放行
	if result := f.OnMessage(ctx, []byte("regular message")); result != FilterPass {
		t.Fatal("authenticated connection should pass regular messages")
	}
}

func TestAntiReplayFilter(t *testing.T) {
	f := NewAntiReplayFilter(0, 8, 30*time.Second)
	ctx := &SecurityContext{ConnID: "test"}

	makeMsg := func(seq uint64, ts int64) []byte {
		data := make([]byte, 16)
		binary.BigEndian.PutUint64(data[0:8], seq)
		binary.BigEndian.PutUint64(data[8:16], uint64(ts))
		return data
	}

	now := time.Now().Unix()

	// 正常递增序列号
	if result := f.OnMessage(ctx, makeMsg(1, now)); result != FilterPass {
		t.Fatal("first message should pass")
	}
	if result := f.OnMessage(ctx, makeMsg(2, now)); result != FilterPass {
		t.Fatal("incrementing seq should pass")
	}

	// 重放：序列号不递增
	if result := f.OnMessage(ctx, makeMsg(1, now)); result != FilterReject {
		t.Fatal("replay should be rejected")
	}

	// 时间戳过旧
	if result := f.OnMessage(ctx, makeMsg(3, now-60)); result != FilterReject {
		t.Fatal("old timestamp should be rejected")
	}
}

func TestAnomalyDetectorFilter(t *testing.T) {
	f := NewAnomalyDetectorFilter(3, time.Minute)

	ctx := &SecurityContext{RemoteAddr: "1.2.3.4", ConnID: "test"}
	if err := f.OnConnect(ctx); err != nil {
		t.Fatalf("should connect: %v", err)
	}

	// 积累违规
	ctx.AddViolation()
	ctx.AddViolation()
	if result := f.OnMessage(ctx, nil); result != FilterPass {
		t.Fatal("below threshold should pass")
	}

	ctx.AddViolation() // 达到阈值
	if result := f.OnMessage(ctx, nil); result != FilterKick {
		t.Fatal("at threshold should kick")
	}

	// IP 应被封禁
	if !f.IsBanned("1.2.3.4") {
		t.Fatal("IP should be banned")
	}

	// 新连接应被拒绝
	ctx2 := &SecurityContext{RemoteAddr: "1.2.3.4", ConnID: "test2"}
	if err := f.OnConnect(ctx2); err == nil {
		t.Fatal("banned IP should be rejected")
	}
}

func TestSecurityConfigBuildChain(t *testing.T) {
	config := DefaultSecurityConfig()
	config.EnableIPLimit = true
	config.EnableMsgValidation = true
	config.EnableAnomalyDetect = true

	chain := config.BuildChain()
	if chain == nil {
		t.Fatal("chain should not be nil with enabled features")
	}
	if len(chain.filters) != 3 {
		t.Fatalf("expected 3 filters, got %d", len(chain.filters))
	}
}

func TestSecurityConfigBuildChainAllDisabled(t *testing.T) {
	config := DefaultSecurityConfig()
	chain := config.BuildChain()
	if chain != nil {
		t.Fatal("chain should be nil when all features disabled")
	}
}

func TestSecurityChainIntegration(t *testing.T) {
	chain := NewSecurityChain(
		NewIPRateLimitFilter(100, time.Second),
		NewMessageValidatorFilter(1024),
		NewAnomalyDetectorFilter(5, time.Minute),
	)

	ctx := &SecurityContext{
		RemoteAddr:  "10.0.0.1",
		ConnID:      "integration-test",
		ConnectedAt: time.Now(),
	}

	if err := chain.ProcessConnect(ctx); err != nil {
		t.Fatalf("connect should pass: %v", err)
	}

	// 正常消息
	if result := chain.ProcessMessage(ctx, make([]byte, 100)); result != FilterPass {
		t.Fatal("normal message should pass")
	}

	// 超大消息
	if result := chain.ProcessMessage(ctx, make([]byte, 2048)); result != FilterReject {
		t.Fatal("oversized message should be rejected")
	}

	chain.ProcessDisconnect(ctx)
}
