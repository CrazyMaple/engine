package middleware

import (
	"sync"
	"testing"
	"time"
)

func TestTokenBucket_Basic(t *testing.T) {
	// 10 tokens/sec, burst of 5
	tb := NewTokenBucket(10, 5)

	// 初始应有 5 个令牌
	for i := 0; i < 5; i++ {
		if !tb.Allow() {
			t.Fatalf("token %d should be allowed", i)
		}
	}

	// 第6次应被拒绝
	if tb.Allow() {
		t.Error("6th token should be denied")
	}
}

func TestTokenBucket_Refill(t *testing.T) {
	tb := NewTokenBucket(100, 1) // 100/sec, burst 1

	// 用完令牌
	tb.Allow()
	if tb.Allow() {
		t.Error("should be denied after burst exhausted")
	}

	// 等待一段时间让令牌补充
	time.Sleep(20 * time.Millisecond)

	if !tb.Allow() {
		t.Error("should be allowed after refill")
	}
}

func TestTokenBucket_Concurrent(t *testing.T) {
	tb := NewTokenBucket(1000, 100)

	var wg sync.WaitGroup
	var allowed, denied int64
	var mu sync.Mutex

	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if tb.Allow() {
				mu.Lock()
				allowed++
				mu.Unlock()
			} else {
				mu.Lock()
				denied++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	total := allowed + denied
	if total != 200 {
		t.Errorf("expected 200 total, got %d", total)
	}
	// 初始 burst=100，应允许约 100 个
	if allowed > 110 || allowed < 90 {
		t.Errorf("expected ~100 allowed, got %d", allowed)
	}
}

func TestConnectionRateLimiter(t *testing.T) {
	cl := NewConnectionRateLimiter(10, 3)

	// 连接1: 允许 3 次突发
	for i := 0; i < 3; i++ {
		if !cl.Allow("conn-1") {
			t.Fatalf("conn-1 token %d should be allowed", i)
		}
	}
	if cl.Allow("conn-1") {
		t.Error("conn-1 4th token should be denied")
	}

	// 连接2: 独立计数
	if !cl.Allow("conn-2") {
		t.Error("conn-2 first token should be allowed")
	}

	if cl.Count() != 2 {
		t.Errorf("expected 2 connections tracked, got %d", cl.Count())
	}

	// 移除连接
	cl.Remove("conn-1")
	if cl.Count() != 1 {
		t.Errorf("expected 1 connection after remove, got %d", cl.Count())
	}
}

func TestRateLimitConfig_Actions(t *testing.T) {
	// 验证动作常量值
	if RateLimitDrop != 0 {
		t.Error("RateLimitDrop should be 0")
	}
	if RateLimitLog != 1 {
		t.Error("RateLimitLog should be 1")
	}
}
