package actor

import "testing"

func TestPIDCache_BasicGetSet(t *testing.T) {
	cache := &PIDCache{}
	pid := NewLocalPID("test-pid")
	proc := &deadLetterProcess{}

	if _, ok := cache.Get(pid); ok {
		t.Fatal("empty cache should miss")
	}

	cache.Set(pid, proc)
	if p, ok := cache.Get(pid); !ok || p != proc {
		t.Fatal("cache hit expected")
	}

	cache.Invalidate(pid)
	newPid := NewLocalPID("test-pid")
	if _, ok := cache.Get(newPid); ok {
		t.Fatal("after invalidate, cache should miss")
	}
}

func TestPIDCache_HitRate(t *testing.T) {
	cache := &PIDCache{}
	pid := NewLocalPID("hit-test")
	proc := &deadLetterProcess{}
	cache.Set(pid, proc)

	for i := 0; i < 100; i++ {
		cache.Get(pid)
	}
	for i := 0; i < 10; i++ {
		cache.Get(NewLocalPID("missing"))
	}

	rate := cache.HitRate()
	if rate < 0.9 || rate > 1.0 {
		t.Fatalf("expected hit rate ~0.91, got %f", rate)
	}
}

func TestProcessRegistry_PIDCacheIntegration(t *testing.T) {
	globalPIDCache.InvalidateAll()

	system := NewActorSystem()
	props := PropsFromFunc(func(ctx Context) {})
	pid := system.Root.SpawnNamed(props, "cache-integration-test")

	newPid := NewLocalPID("cache-integration-test")
	proc, ok := system.ProcessRegistry.Get(newPid)
	if !ok {
		t.Fatal("expected to find process")
	}
	if proc == nil {
		t.Fatal("process should not be nil")
	}

	hits, _ := globalPIDCache.Stats()
	if hits == 0 {
		t.Fatal("expected at least one cache hit after lookups")
	}

	system.Root.Stop(pid)
}
