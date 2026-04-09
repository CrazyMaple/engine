package actor

import (
	"sync"
	"testing"
	"time"
)

func TestDeadLetterMonitor_CountAndRecord(t *testing.T) {
	es := NewEventStream()
	mon := NewDeadLetterMonitor(es, DeadLetterMonitorConfig{
		MaxStoredRecords: 100,
	})
	defer mon.Stop()

	// Publish dead letter events
	for i := 0; i < 5; i++ {
		es.Publish(&DeadLetterEvent{
			PID:     NewLocalPID("target-1"),
			Message: "test-msg",
			Sender:  NewLocalPID("sender-1"),
		})
	}

	time.Sleep(10 * time.Millisecond)

	stats := mon.Stats()
	if stats.TotalCount != 5 {
		t.Fatalf("expected total 5, got %d", stats.TotalCount)
	}
	if stats.TypeCounts["string"] != 5 {
		t.Fatalf("expected 5 string messages, got %d", stats.TypeCounts["string"])
	}

	records := mon.RecentRecords(10)
	if len(records) != 5 {
		t.Fatalf("expected 5 records, got %d", len(records))
	}
	if records[0].TargetPID != "target-1" {
		t.Errorf("expected target-1, got %s", records[0].TargetPID)
	}
	if records[0].SenderPID != "sender-1" {
		t.Errorf("expected sender-1, got %s", records[0].SenderPID)
	}
}

func TestDeadLetterMonitor_Alert(t *testing.T) {
	es := NewEventStream()
	mon := NewDeadLetterMonitor(es, DeadLetterMonitorConfig{
		AlertThreshold: 3,
		AlertWindow:    time.Second,
	})
	defer mon.Stop()

	var mu sync.Mutex
	var alerts []*DeadLetterAlertEvent
	es.Subscribe(func(event interface{}) {
		if alert, ok := event.(*DeadLetterAlertEvent); ok {
			mu.Lock()
			alerts = append(alerts, alert)
			mu.Unlock()
		}
	})

	// Send 3 dead letters to trigger alert
	for i := 0; i < 3; i++ {
		es.Publish(&DeadLetterEvent{
			PID:     NewLocalPID("gone"),
			Message: i,
		})
	}

	time.Sleep(20 * time.Millisecond)

	mu.Lock()
	count := len(alerts)
	mu.Unlock()

	if count != 1 {
		t.Fatalf("expected 1 alert, got %d", count)
	}
}

func TestDeadLetterMonitor_StorageEviction(t *testing.T) {
	es := NewEventStream()
	mon := NewDeadLetterMonitor(es, DeadLetterMonitorConfig{
		MaxStoredRecords: 10,
	})
	defer mon.Stop()

	// Send 15 dead letters — should evict oldest
	for i := 0; i < 15; i++ {
		es.Publish(&DeadLetterEvent{
			PID:     NewLocalPID("x"),
			Message: i,
		})
	}

	time.Sleep(10 * time.Millisecond)

	records := mon.RecentRecords(100)
	if len(records) > 10 {
		t.Fatalf("expected at most 10 records after eviction, got %d", len(records))
	}
}

func TestDeadLetterMonitor_TypeCounts(t *testing.T) {
	es := NewEventStream()
	mon := NewDeadLetterMonitor(es, DeadLetterMonitorConfig{})
	defer mon.Stop()

	es.Publish(&DeadLetterEvent{PID: NewLocalPID("x"), Message: "str"})
	es.Publish(&DeadLetterEvent{PID: NewLocalPID("x"), Message: 42})
	es.Publish(&DeadLetterEvent{PID: NewLocalPID("x"), Message: "str2"})

	time.Sleep(10 * time.Millisecond)

	stats := mon.Stats()
	if stats.TotalCount != 3 {
		t.Fatalf("expected 3 total, got %d", stats.TotalCount)
	}
	if stats.TypeCounts["string"] != 2 {
		t.Errorf("expected 2 string, got %d", stats.TypeCounts["string"])
	}
	if stats.TypeCounts["int"] != 1 {
		t.Errorf("expected 1 int, got %d", stats.TypeCounts["int"])
	}
}

func TestDeadLetterMonitor_Metrics(t *testing.T) {
	es := NewEventStream()

	// Simple mock metrics
	mock := &mockDeadLetterMetrics{}
	mon := NewDeadLetterMonitor(es, DeadLetterMonitorConfig{
		Metrics: mock,
	})
	defer mon.Stop()

	es.Publish(&DeadLetterEvent{PID: NewLocalPID("x"), Message: "hello"})
	time.Sleep(10 * time.Millisecond)

	mock.mu.Lock()
	if mock.count != 1 {
		t.Fatalf("expected 1 metric inc, got %d", mock.count)
	}
	mock.mu.Unlock()
}

type mockDeadLetterMetrics struct {
	mu    sync.Mutex
	count int
}

func (m *mockDeadLetterMetrics) IncCounter(name, help string, labels map[string]string, delta int64) {
	m.mu.Lock()
	m.count++
	m.mu.Unlock()
}
