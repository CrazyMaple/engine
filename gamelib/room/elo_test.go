package room

import (
	"math"
	"testing"
)

func TestEloConfig_Expected(t *testing.T) {
	c := DefaultEloConfig()
	// 相同分数 → 50%
	if e := c.Expected(1500, 1500); math.Abs(e-0.5) > 1e-9 {
		t.Errorf("same rating expected=0.5, got %v", e)
	}
	// A 高 400 分 → 约 90.9%
	e := c.Expected(1900, 1500)
	if e < 0.9 || e > 0.92 {
		t.Errorf("want ~0.909, got %v", e)
	}
}

func TestEloConfig_UpdateRating(t *testing.T) {
	c := DefaultEloConfig()
	// 相同分数，A 胜 → A+16, B-16
	nA, nB := c.UpdateRating(1500, 1500, EloWin)
	if math.Abs(nA-1516) > 1e-9 || math.Abs(nB-1484) > 1e-9 {
		t.Errorf("want 1516/1484, got %v/%v", nA, nB)
	}
	// A 胜更弱的对手，获得更少（比较 delta）
	nA2, _ := c.UpdateRating(1900, 1500, EloWin)
	gainStrong := nA - 1500  // A 胜同分对手的加分
	gainWeak := nA2 - 1900    // A 胜弱对手的加分
	if gainWeak >= gainStrong {
		t.Errorf("winning weak opponent should give less, gainWeak=%v gainStrong=%v", gainWeak, gainStrong)
	}
	// 平局
	nA3, nB3 := c.UpdateRating(1500, 1500, EloDraw)
	if math.Abs(nA3-1500) > 1e-9 || math.Abs(nB3-1500) > 1e-9 {
		t.Errorf("draw at same rating should be unchanged, got %v/%v", nA3, nB3)
	}
}

func TestEloConfig_MinRating(t *testing.T) {
	c := EloConfig{KFactor: 100, MinRating: 50, Scale: 400}
	// 一个 100 分玩家连输，不应掉到 50 以下
	nA, _ := c.UpdateRating(100, 2000, EloLoss)
	if nA < 50 {
		t.Errorf("rating should floor at 50, got %v", nA)
	}
}

func TestEloConfig_UpdateTeam(t *testing.T) {
	c := DefaultEloConfig()
	winners := []PlayerInfo{
		{PlayerID: "w1", Rating: 1500},
		{PlayerID: "w2", Rating: 1500},
	}
	losers := []PlayerInfo{
		{PlayerID: "l1", Rating: 1500},
		{PlayerID: "l2", Rating: 1500},
	}
	deltas := c.UpdateTeam(winners, losers)
	if deltas["w1"] <= 0 || deltas["l1"] >= 0 {
		t.Errorf("winners should gain, losers should lose, got %+v", deltas)
	}
	if math.Abs(deltas["w1"]-16) > 1e-9 {
		t.Errorf("balanced match K/2 = 16, got %v", deltas["w1"])
	}
}

func TestInMemoryMMRStore(t *testing.T) {
	s := NewInMemoryMMRStore(1000)
	if v := s.GetMMR("alice"); v != 1000 {
		t.Errorf("default mmr = 1000, got %v", v)
	}
	s.SetMMR("alice", 1200)
	if v := s.GetMMR("alice"); v != 1200 {
		t.Errorf("want 1200, got %v", v)
	}
}

func TestEffectiveRating(t *testing.T) {
	p := PlayerInfo{PlayerID: "p1", Rating: 1000}
	// nil store → 返回 Rating
	if r := EffectiveRating(p, nil); r != 1000 {
		t.Errorf("nil store want 1000, got %v", r)
	}
	// 有 store → 使用 MMR
	s := NewInMemoryMMRStore(1500)
	if r := EffectiveRating(p, s); r != 1500 {
		t.Errorf("store MMR want 1500, got %v", r)
	}
	// metadata weight=0 → 完全使用 Rating
	p.Metadata = map[string]interface{}{"mmr_weight": float64(0)}
	if r := EffectiveRating(p, s); r != 1000 {
		t.Errorf("weight=0 want 1000, got %v", r)
	}
	// weight=0.5 → 混合
	p.Metadata["mmr_weight"] = float64(0.5)
	if r := EffectiveRating(p, s); math.Abs(r-1250) > 1e-9 {
		t.Errorf("weight=0.5 want 1250, got %v", r)
	}
}
