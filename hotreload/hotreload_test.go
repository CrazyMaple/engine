package hotreload

import (
	"errors"
	"sync"
	"testing"
	"time"
)

// --- Mock Plugin for testing ---

type mockPlugin struct {
	name       string
	version    string
	loadErr    error
	reloadErr  error
	unloadErr  error
	healthErr  error
	loaded     bool
	reloaded   bool
	unloaded   bool
	mu         sync.Mutex
}

func newMockPlugin(name, version string) *mockPlugin {
	return &mockPlugin{name: name, version: version}
}

func (p *mockPlugin) Name() string    { return p.name }
func (p *mockPlugin) Version() string { return p.version }

func (p *mockPlugin) Load() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.loadErr != nil {
		return p.loadErr
	}
	p.loaded = true
	return nil
}

func (p *mockPlugin) Reload(old Plugin) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.reloadErr != nil {
		return p.reloadErr
	}
	p.reloaded = true
	return nil
}

func (p *mockPlugin) Unload() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.unloadErr != nil {
		return p.unloadErr
	}
	p.unloaded = true
	return nil
}

func (p *mockPlugin) Health() error {
	return p.healthErr
}

// --- Manager Tests ---

func TestManager_Load(t *testing.T) {
	m := NewManager()
	p := newMockPlugin("test-plugin", "1.0.0")

	if err := m.Load(p); err != nil {
		t.Fatalf("load failed: %v", err)
	}

	// Verify loaded
	got, ok := m.Get("test-plugin")
	if !ok {
		t.Fatal("plugin not found after load")
	}
	if got.Version() != "1.0.0" {
		t.Errorf("expected version 1.0.0, got %s", got.Version())
	}

	// List should return it
	list := m.List()
	if len(list) != 1 {
		t.Fatalf("expected 1 plugin, got %d", len(list))
	}
	if list[0].Status != "loaded" {
		t.Errorf("expected status 'loaded', got %s", list[0].Status)
	}
}

func TestManager_LoadDuplicate(t *testing.T) {
	m := NewManager()
	p1 := newMockPlugin("dup", "1.0")
	p2 := newMockPlugin("dup", "2.0")

	m.Load(p1)
	err := m.Load(p2)
	if !errors.Is(err, ErrPluginLoaded) {
		t.Fatalf("expected ErrPluginLoaded, got %v", err)
	}
}

func TestManager_LoadError(t *testing.T) {
	m := NewManager()
	p := newMockPlugin("fail", "1.0")
	p.loadErr = errors.New("init failed")

	err := m.Load(p)
	if err == nil {
		t.Fatal("expected error on load")
	}

	// Should not be registered
	_, ok := m.Get("fail")
	if ok {
		t.Error("failed plugin should not be registered")
	}
}

func TestManager_Reload(t *testing.T) {
	m := NewManager()
	v1 := newMockPlugin("app", "1.0")
	v2 := newMockPlugin("app", "2.0")

	m.Load(v1)
	if err := m.Reload(v2); err != nil {
		t.Fatalf("reload failed: %v", err)
	}

	got, _ := m.Get("app")
	if got.Version() != "2.0" {
		t.Errorf("expected version 2.0, got %s", got.Version())
	}

	// Check history
	history := m.ReloadHistory(10)
	if len(history) != 1 {
		t.Fatalf("expected 1 history event, got %d", len(history))
	}
	if !history[0].Success {
		t.Error("expected success=true")
	}
	if history[0].OldVersion != "1.0" || history[0].NewVersion != "2.0" {
		t.Errorf("unexpected versions in history: %s -> %s", history[0].OldVersion, history[0].NewVersion)
	}
}

func TestManager_ReloadNotFound(t *testing.T) {
	m := NewManager()
	p := newMockPlugin("missing", "1.0")

	err := m.Reload(p)
	if !errors.Is(err, ErrPluginNotFound) {
		t.Fatalf("expected ErrPluginNotFound, got %v", err)
	}
}

func TestManager_ReloadError(t *testing.T) {
	m := NewManager()
	v1 := newMockPlugin("app", "1.0")
	v2 := newMockPlugin("app", "2.0")
	v2.reloadErr = errors.New("reload failed")

	m.Load(v1)
	err := m.Reload(v2)
	if err == nil {
		t.Fatal("expected error on reload")
	}

	// Should still have v1
	got, _ := m.Get("app")
	if got.Version() != "1.0" {
		t.Errorf("version should not change on failed reload")
	}

	// History should record failure
	history := m.ReloadHistory(10)
	if len(history) != 1 {
		t.Fatal("expected 1 history event")
	}
	if history[0].Success {
		t.Error("expected success=false")
	}
}

func TestManager_Rollback(t *testing.T) {
	m := NewManager()
	v1 := newMockPlugin("app", "1.0")
	v2 := newMockPlugin("app", "2.0")

	m.Load(v1)
	m.Reload(v2)

	// Rollback to v1
	if err := m.Rollback("app"); err != nil {
		t.Fatalf("rollback failed: %v", err)
	}

	got, _ := m.Get("app")
	if got.Version() != "1.0" {
		t.Errorf("expected version 1.0 after rollback, got %s", got.Version())
	}

	// Second rollback should fail (no previous)
	err := m.Rollback("app")
	if !errors.Is(err, ErrRollbackFailed) {
		t.Fatalf("expected ErrRollbackFailed, got %v", err)
	}
}

func TestManager_Unload(t *testing.T) {
	m := NewManager()
	p := newMockPlugin("app", "1.0")

	m.Load(p)
	if err := m.Unload("app"); err != nil {
		t.Fatalf("unload failed: %v", err)
	}

	_, ok := m.Get("app")
	if ok {
		t.Error("plugin should not exist after unload")
	}

	p.mu.Lock()
	if !p.unloaded {
		t.Error("Unload() was not called on plugin")
	}
	p.mu.Unlock()
}

func TestManager_UnloadNotFound(t *testing.T) {
	m := NewManager()
	err := m.Unload("missing")
	if !errors.Is(err, ErrPluginNotFound) {
		t.Fatalf("expected ErrPluginNotFound, got %v", err)
	}
}

func TestManager_EventListener(t *testing.T) {
	m := NewManager()

	var mu sync.Mutex
	var events []ReloadEvent
	m.SetEventListener(func(event ReloadEvent) {
		mu.Lock()
		events = append(events, event)
		mu.Unlock()
	})

	v1 := newMockPlugin("app", "1.0")
	v2 := newMockPlugin("app", "2.0")

	m.Load(v1)
	m.Reload(v2)

	time.Sleep(20 * time.Millisecond) // wait for async listener

	mu.Lock()
	defer mu.Unlock()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].PluginName != "app" {
		t.Errorf("expected plugin name 'app', got %s", events[0].PluginName)
	}
}

func TestManager_ReloadHistory(t *testing.T) {
	m := NewManager()
	v1 := newMockPlugin("app", "1.0")
	m.Load(v1)

	for i := 2; i <= 5; i++ {
		v := newMockPlugin("app", "")
		v.version = ""
		next := newMockPlugin("app", "")
		_ = v
		_ = next
	}

	// Empty history
	history := m.ReloadHistory(10)
	if len(history) != 0 {
		t.Fatalf("expected empty history, got %d", len(history))
	}
}
