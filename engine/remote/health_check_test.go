package remote

import (
	"testing"
	"time"
)

func TestHealthCheckConfig(t *testing.T) {
	cfg := DefaultHealthCheckConfig()
	if cfg.PingInterval != 5*time.Second {
		t.Errorf("PingInterval = %v, want 5s", cfg.PingInterval)
	}
	if cfg.PingTimeout != 3*time.Second {
		t.Errorf("PingTimeout = %v, want 3s", cfg.PingTimeout)
	}
	if cfg.MaxMissedPings != 3 {
		t.Errorf("MaxMissedPings = %d, want 3", cfg.MaxMissedPings)
	}
	if !cfg.IsEnabled() {
		t.Error("default config should be enabled")
	}
}

func TestHealthCheckConfigDefaults(t *testing.T) {
	cfg := HealthCheckConfig{}
	cfg.defaults()
	if cfg.PingInterval <= 0 {
		t.Error("defaults should set PingInterval")
	}
}

func TestConnPoolConfig(t *testing.T) {
	cfg := DefaultConnPoolConfig()
	if cfg.MinConns != 1 {
		t.Errorf("MinConns = %d, want 1", cfg.MinConns)
	}
	if cfg.MaxConns != 8 {
		t.Errorf("MaxConns = %d, want 8", cfg.MaxConns)
	}
	if !cfg.IsEnabled() {
		t.Error("default pool should be enabled (MaxConns > 1)")
	}
}

func TestRetryQueue(t *testing.T) {
	cfg := RetryQueueConfig{
		Enabled:      true,
		MaxRetries:   2,
		RetryDelay:   1 * time.Millisecond,
		MaxQueueSize: 10,
	}
	rq := newRetryQueue(cfg)

	msg := &RemoteMessage{TypeName: "test"}
	rq.Add(msg)

	if rq.Len() != 1 {
		t.Fatalf("queue length = %d, want 1", rq.Len())
	}

	// Drain 但消息还未到重试时间（已过因 delay = 1ms）
	time.Sleep(2 * time.Millisecond)

	sent := 0
	rq.Drain(func(m *RemoteMessage) error {
		sent++
		return nil
	})

	if sent != 1 {
		t.Errorf("sent = %d, want 1", sent)
	}
	if rq.Len() != 0 {
		t.Errorf("queue should be empty after successful drain, got %d", rq.Len())
	}
}

func TestRetryQueueMaxSize(t *testing.T) {
	cfg := RetryQueueConfig{
		Enabled:      true,
		MaxRetries:   3,
		RetryDelay:   time.Minute, // long delay to prevent drain
		MaxQueueSize: 3,
	}
	rq := newRetryQueue(cfg)

	for i := 0; i < 5; i++ {
		rq.Add(&RemoteMessage{TypeName: "test"})
	}

	if rq.Len() != 3 {
		t.Errorf("queue length = %d, want 3 (max size)", rq.Len())
	}
}

func TestPingPongMessages(t *testing.T) {
	ping := &PingMessage{Timestamp: time.Now().UnixMilli()}
	if ping.Timestamp <= 0 {
		t.Error("PingMessage timestamp should be positive")
	}

	pong := &PongMessage{Timestamp: ping.Timestamp}
	if pong.Timestamp != ping.Timestamp {
		t.Error("PongMessage timestamp should match PingMessage")
	}
}
