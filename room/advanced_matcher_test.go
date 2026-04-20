package room

import (
	"fmt"
	"testing"
	"time"
)

func TestAdvancedMatcher_RatingCloseMatches(t *testing.T) {
	cfg := DefaultAdvancedConfig()
	cfg.PlayersPerMatch = 2
	cfg.MaxAllowedCost = 100
	m := NewAdvancedMatcher(cfg)

	now := time.Unix(1000, 0)
	m.SetNow(func() time.Time { return now })

	m.AddPlayer(PlayerInfo{PlayerID: "a", Rating: 1000})
	m.AddPlayer(PlayerInfo{PlayerID: "b", Rating: 1050})
	m.AddPlayer(PlayerInfo{PlayerID: "c", Rating: 2000}) // 过远

	pair := m.Match()
	if pair == nil {
		t.Fatal("expected match")
	}
	got := map[string]bool{}
	for _, p := range pair {
		got[p.PlayerID] = true
	}
	if !got["a"] || !got["b"] {
		t.Errorf("want a+b matched, got %+v", pair)
	}
	// c 未被匹配
	if m.QueueSize() != 1 {
		t.Errorf("c should remain in queue, size=%d", m.QueueSize())
	}
}

func TestAdvancedMatcher_LatencyWeighted(t *testing.T) {
	cfg := DefaultAdvancedConfig()
	cfg.PlayersPerMatch = 2
	cfg.MaxAllowedCost = 50
	cfg.RatingWeight = 1
	cfg.LatencyWeight = 1 // 1ms 延迟差 = 1 点误差
	m := NewAdvancedMatcher(cfg)

	now := time.Unix(2000, 0)
	m.SetNow(func() time.Time { return now })

	// a 和 b 分数接近但延迟差太大
	m.AddPlayer(PlayerInfo{PlayerID: "a", Rating: 1000, Metadata: map[string]interface{}{"latency_ms": 20}})
	m.AddPlayer(PlayerInfo{PlayerID: "b", Rating: 1010, Metadata: map[string]interface{}{"latency_ms": 200}})
	// rating diff=10, latency diff=180 → cost=190 > 50
	if res := m.Match(); res != nil {
		t.Errorf("high-latency pair should not match: %+v", res)
	}

	// c 与 a 延迟接近
	m.AddPlayer(PlayerInfo{PlayerID: "c", Rating: 1030, Metadata: map[string]interface{}{"latency_ms": 25}})
	// now pair a+c: rDiff=30 + lDiff=5 = 35 ≤ 50
	res := m.Match()
	if res == nil {
		t.Fatal("a+c should match")
	}
	got := map[string]bool{}
	for _, p := range res {
		got[p.PlayerID] = true
	}
	if !got["a"] || !got["c"] {
		t.Errorf("want a+c, got %+v", res)
	}
}

func TestAdvancedMatcher_DynamicExpansion(t *testing.T) {
	cfg := DefaultAdvancedConfig()
	cfg.PlayersPerMatch = 2
	cfg.MaxAllowedCost = 50
	cfg.CostGrowthPerSecond = 100 // 每秒容忍度 +100
	cfg.RatingWeight = 1
	cfg.LatencyWeight = 0
	m := NewAdvancedMatcher(cfg)

	start := time.Unix(3000, 0)
	now := start
	m.SetNow(func() time.Time { return now })

	m.AddPlayer(PlayerInfo{PlayerID: "low", Rating: 1000})
	m.AddPlayer(PlayerInfo{PlayerID: "high", Rating: 1200}) // diff=200

	// 0 秒：不可匹配（200 > 50）
	if res := m.Match(); res != nil {
		t.Errorf("at t=0, should not match: %+v", res)
	}

	// 等待 2 秒：容忍度 = 50 + 200 = 250 ≥ 200 → 可匹配
	now = start.Add(2 * time.Second)
	res := m.Match()
	if res == nil {
		t.Fatal("after waiting, expansion should allow match")
	}
	if len(res) != 2 {
		t.Errorf("want 2, got %d", len(res))
	}
}

func TestAdvancedMatcher_TagFilter(t *testing.T) {
	cfg := DefaultAdvancedConfig()
	cfg.PlayersPerMatch = 2
	cfg.MaxAllowedCost = 1e9
	cfg.TagFilter = func(a, b PlayerInfo) bool {
		return a.Metadata["region"] == b.Metadata["region"]
	}
	m := NewAdvancedMatcher(cfg)

	now := time.Unix(4000, 0)
	m.SetNow(func() time.Time { return now })

	m.AddPlayer(PlayerInfo{PlayerID: "cn1", Rating: 1000, Metadata: map[string]interface{}{"region": "cn"}})
	m.AddPlayer(PlayerInfo{PlayerID: "us1", Rating: 1000, Metadata: map[string]interface{}{"region": "us"}})
	// 不同 region 被 filter 挡掉
	if res := m.Match(); res != nil {
		t.Errorf("cross-region should not match: %+v", res)
	}
	m.AddPlayer(PlayerInfo{PlayerID: "cn2", Rating: 1050, Metadata: map[string]interface{}{"region": "cn"}})
	res := m.Match()
	if res == nil || len(res) != 2 {
		t.Fatalf("cn1+cn2 should match, got %+v", res)
	}
}

func TestAdvancedMatcher_MMRStoreIntegration(t *testing.T) {
	cfg := DefaultAdvancedConfig()
	cfg.PlayersPerMatch = 2
	cfg.MaxAllowedCost = 50
	cfg.RatingWeight = 1
	cfg.LatencyWeight = 0

	store := NewInMemoryMMRStore(1500)
	store.SetMMR("smurf", 2500) // 显示 1200 但隐分 2500 ⇒ 实际匹配按 2500
	cfg.MMRStore = store

	m := NewAdvancedMatcher(cfg)
	now := time.Unix(5000, 0)
	m.SetNow(func() time.Time { return now })

	m.AddPlayer(PlayerInfo{PlayerID: "smurf", Rating: 1200})
	m.AddPlayer(PlayerInfo{PlayerID: "honest", Rating: 1200})
	// 如果只看 Rating，两者差距 0；但 MMR 差距 1000 → 不可匹配
	if res := m.Match(); res != nil {
		t.Errorf("smurf+honest should not match on MMR, got %+v", res)
	}

	// 加入另一个匹配 smurf 隐分的对手
	store.SetMMR("veteran", 2450)
	m.AddPlayer(PlayerInfo{PlayerID: "veteran", Rating: 1800})
	res := m.Match()
	if res == nil {
		t.Fatal("smurf+veteran should match on MMR")
	}
	got := map[string]bool{}
	for _, p := range res {
		got[p.PlayerID] = true
	}
	if !got["smurf"] || !got["veteran"] {
		t.Errorf("want smurf+veteran, got %+v", res)
	}
}

func TestAdvancedMatcher_QualityMetrics(t *testing.T) {
	cfg := DefaultAdvancedConfig()
	cfg.PlayersPerMatch = 2
	cfg.MaxAllowedCost = 1000
	m := NewAdvancedMatcher(cfg)

	start := time.Unix(6000, 0)
	now := start
	m.SetNow(func() time.Time { return now })

	// 模拟多轮匹配：段位差递增
	for i := 0; i < 5; i++ {
		aRating := float64(1000 + i*10)
		bRating := float64(1050 + i*10)
		m.AddPlayer(PlayerInfo{PlayerID: fmt.Sprintf("a%d", i), Rating: aRating})
		m.AddPlayer(PlayerInfo{PlayerID: fmt.Sprintf("b%d", i), Rating: bRating})
		now = now.Add(500 * time.Millisecond)
		if res := m.Match(); res == nil {
			t.Fatalf("round %d: expected match", i)
		}
	}

	mets := m.Metrics()
	if mets.TotalMatches != 5 {
		t.Errorf("want 5 matches, got %d", mets.TotalMatches)
	}
	// 每对 rating spread = 50
	if mets.AvgRatingSpread != 50 {
		t.Errorf("avg spread want 50, got %v", mets.AvgRatingSpread)
	}
	if mets.AvgWait <= 0 {
		t.Error("avg wait should be > 0")
	}

	m.ResetMetrics()
	if m.Metrics().TotalMatches != 0 {
		t.Error("reset should clear metrics")
	}
}

func TestAdvancedMatcher_PrioritizesOldestAnchor(t *testing.T) {
	cfg := DefaultAdvancedConfig()
	cfg.PlayersPerMatch = 2
	cfg.MaxAllowedCost = 10000
	m := NewAdvancedMatcher(cfg)

	start := time.Unix(7000, 0)
	now := start
	m.SetNow(func() time.Time { return now })

	// 先入队的 old 应优先作为锚点
	m.AddPlayer(PlayerInfo{PlayerID: "old", Rating: 1000})
	now = now.Add(10 * time.Second)
	m.AddPlayer(PlayerInfo{PlayerID: "new1", Rating: 2000})
	m.AddPlayer(PlayerInfo{PlayerID: "new2", Rating: 2050})

	res := m.Match()
	if res == nil {
		t.Fatal("expected match")
	}
	// old 应被选中
	got := map[string]bool{}
	for _, p := range res {
		got[p.PlayerID] = true
	}
	if !got["old"] {
		t.Errorf("old should be matched first, got %+v", res)
	}
}
