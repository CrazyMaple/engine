package leaderboard

import (
	"sync"
	"testing"
	"time"
)

func setupSeasonMgr(t *testing.T) (*SeasonManager, *LeaderboardActor) {
	t.Helper()
	la := NewLeaderboardActor(LeaderboardConfig{})
	sm := NewSeasonManager(la)
	return sm, la
}

func TestSeasonManager_TickStateMachine(t *testing.T) {
	sm, la := setupSeasonMgr(t)
	start := time.Unix(1000, 0)
	cfg := SeasonConfig{
		Board:          "arena",
		SeasonID:       "s1",
		StartAt:        start,
		EndAt:          start.Add(1 * time.Hour),
		EndingSoonLead: 15 * time.Minute,
	}
	if err := sm.RegisterSeason(cfg); err != nil {
		t.Fatalf("register: %v", err)
	}

	// Upcoming
	if _, st, _ := sm.Current("arena"); st != SeasonUpcoming {
		t.Errorf("want upcoming, got %v", st)
	}

	// 推进到 StartAt → Active
	sm.Tick(start)
	if _, st, _ := sm.Current("arena"); st != SeasonActive {
		t.Errorf("want active, got %v", st)
	}

	// T-10min（进入 EndingSoon 窗口）
	sm.Tick(start.Add(50 * time.Minute))
	if _, st, _ := sm.Current("arena"); st != SeasonEndingSoon {
		t.Errorf("want ending_soon, got %v", st)
	}

	// 录入一些分数，用于结算
	b := la.GetOrCreateBoard("arena")
	b.UpdateScore("p1", 100, "Alice")
	b.UpdateScore("p2", 50, "Bob")

	// 推进到 EndAt → Settling → Archived
	settled := sm.Tick(start.Add(1 * time.Hour))
	if len(settled) != 1 || settled[0] != "s1" {
		t.Errorf("want settled=[s1], got %v", settled)
	}
	if _, st, _ := sm.Current("arena"); st != SeasonArchived {
		t.Errorf("want archived, got %v", st)
	}
	if len(sm.History("arena")) != 1 {
		t.Errorf("want 1 history snapshot, got %d", len(sm.History("arena")))
	}
}

func TestSeasonManager_RewardDispatch(t *testing.T) {
	sm, la := setupSeasonMgr(t)
	now := time.Unix(2000, 0)

	cfg := SeasonConfig{
		Board:    "rank",
		SeasonID: "s2",
		StartAt:  now.Add(-2 * time.Hour),
		EndAt:    now.Add(-1 * time.Hour),
		Rewards: []SeasonRewardRule{
			{MinRank: 1, MaxRank: 1, RewardName: "gold", RewardCount: 1000},
			{MinRank: 2, MaxRank: 3, RewardName: "silver", RewardCount: 500},
		},
	}
	if err := sm.RegisterSeason(cfg); err != nil {
		t.Fatalf("register: %v", err)
	}

	// 录入选手
	b := la.GetOrCreateBoard("rank")
	b.UpdateScore("alice", 1000, "")
	b.UpdateScore("bob", 800, "")
	b.UpdateScore("carol", 600, "")
	b.UpdateScore("dave", 400, "")

	var mu sync.Mutex
	rewards := make([]SeasonReward, 0)
	sm.SetRewardSink(func(season string, r SeasonReward) error {
		mu.Lock()
		defer mu.Unlock()
		if season != "s2" {
			t.Errorf("want season=s2, got %s", season)
		}
		rewards = append(rewards, r)
		return nil
	})

	archived := false
	sm.SetArchiveSink(func(snap *SeasonSnapshot) error {
		if snap.SeasonID != "s2" {
			t.Errorf("archive snapshot wrong season: %s", snap.SeasonID)
		}
		archived = true
		return nil
	})

	snap, err := sm.SettleNow("rank", now)
	if err != nil {
		t.Fatalf("settle: %v", err)
	}
	if !archived {
		t.Error("archive sink not invoked")
	}
	if len(snap.Entries) != 4 {
		t.Errorf("want 4 entries archived, got %d", len(snap.Entries))
	}

	mu.Lock()
	defer mu.Unlock()
	if len(rewards) != 3 {
		t.Fatalf("want 3 rewards dispatched (ranks 1,2,3), got %d", len(rewards))
	}
	if rewards[0].PlayerID != "alice" || rewards[0].RewardName != "gold" {
		t.Errorf("rank1 wrong: %+v", rewards[0])
	}
	if rewards[1].PlayerID != "bob" || rewards[1].RewardName != "silver" {
		t.Errorf("rank2 wrong: %+v", rewards[1])
	}
	if rewards[2].PlayerID != "carol" || rewards[2].RewardName != "silver" {
		t.Errorf("rank3 wrong: %+v", rewards[2])
	}
}

func TestSeasonManager_CarryRatio(t *testing.T) {
	sm, la := setupSeasonMgr(t)
	now := time.Unix(3000, 0)
	cfg := SeasonConfig{
		Board:      "carry",
		SeasonID:   "s3",
		StartAt:    now.Add(-time.Hour),
		EndAt:      now,
		CarryRatio: 0.5,
	}
	_ = sm.RegisterSeason(cfg)

	b := la.GetOrCreateBoard("carry")
	b.UpdateScore("p1", 1000, "")
	b.UpdateScore("p2", 600, "")

	if _, err := sm.SettleNow("carry", now); err != nil {
		t.Fatalf("settle: %v", err)
	}

	top := b.GetTopN(10)
	if len(top) != 2 {
		t.Fatalf("want 2 remaining, got %d", len(top))
	}
	if top[0].Entry.Score != 500 || top[1].Entry.Score != 300 {
		t.Errorf("carry ratio wrong: %+v", top)
	}
}

func TestSeasonManager_CarryReset(t *testing.T) {
	sm, la := setupSeasonMgr(t)
	now := time.Unix(4000, 0)
	cfg := SeasonConfig{
		Board:      "reset",
		SeasonID:   "s4",
		StartAt:    now.Add(-time.Hour),
		EndAt:      now,
		CarryRatio: 0, // 完全重置
	}
	_ = sm.RegisterSeason(cfg)

	b := la.GetOrCreateBoard("reset")
	b.UpdateScore("p1", 1000, "")

	if _, err := sm.SettleNow("reset", now); err != nil {
		t.Fatalf("settle: %v", err)
	}
	if b.Size() != 0 {
		t.Errorf("board should be reset to 0, got size=%d", b.Size())
	}
}

func TestSeasonManager_CrossSeasonQuery(t *testing.T) {
	sm, la := setupSeasonMgr(t)
	now := time.Unix(5000, 0)

	// 赛季 1
	_ = sm.RegisterSeason(SeasonConfig{
		Board:      "global",
		SeasonID:   "s1",
		StartAt:    now.Add(-2 * time.Hour),
		EndAt:      now.Add(-1 * time.Hour),
		CarryRatio: 0,
	})
	b := la.GetOrCreateBoard("global")
	b.UpdateScore("alice", 500, "")
	b.UpdateScore("bob", 300, "")
	if _, err := sm.SettleNow("global", now.Add(-time.Hour)); err != nil {
		t.Fatalf("settle s1: %v", err)
	}

	// 清理 active map 以便再次注册
	sm.mu.Lock()
	delete(sm.configs, "global")
	delete(sm.active, "global")
	sm.mu.Unlock()

	// 赛季 2
	_ = sm.RegisterSeason(SeasonConfig{
		Board:    "global",
		SeasonID: "s2",
		StartAt:  now.Add(-30 * time.Minute),
		EndAt:    now.Add(time.Hour),
	})
	b.UpdateScore("alice", 400, "")
	b.UpdateScore("carol", 200, "")

	// historical：仅 s1 快照
	hist := sm.QueryCrossSeason(CrossSeasonQuery{Board: "global", Scope: "historical", N: 10})
	if len(hist) != 2 {
		t.Errorf("historical want 2, got %d", len(hist))
	}

	// all-time：s1 + 当前（alice 会聚合 500+400=900）
	all := sm.QueryCrossSeason(CrossSeasonQuery{Board: "global", Scope: "all-time", N: 10})
	var aliceEntry *CrossSeasonEntry
	for i := range all {
		if all[i].PlayerID == "alice" {
			aliceEntry = &all[i]
		}
	}
	if aliceEntry == nil {
		t.Fatal("alice missing in all-time aggregate")
	}
	if aliceEntry.TotalScore != 900 {
		t.Errorf("alice total want 900, got %v", aliceEntry.TotalScore)
	}
	if aliceEntry.Appearances != 2 {
		t.Errorf("alice appearances want 2, got %d", aliceEntry.Appearances)
	}

	// current：仅当前赛季
	cur := sm.QueryCrossSeason(CrossSeasonQuery{Board: "global", Scope: "current", N: 10})
	if len(cur) != 2 {
		t.Errorf("current want 2, got %d", len(cur))
	}
}

func TestSeasonManager_Validate(t *testing.T) {
	sm, _ := setupSeasonMgr(t)
	cases := []struct {
		name string
		cfg  SeasonConfig
		want bool // want error?
	}{
		{"no board", SeasonConfig{SeasonID: "x", StartAt: time.Unix(1, 0), EndAt: time.Unix(2, 0)}, true},
		{"no id", SeasonConfig{Board: "b", StartAt: time.Unix(1, 0), EndAt: time.Unix(2, 0)}, true},
		{"bad range", SeasonConfig{Board: "b", SeasonID: "s", StartAt: time.Unix(2, 0), EndAt: time.Unix(1, 0)}, true},
		{"ok", SeasonConfig{Board: "ok", SeasonID: "ok", StartAt: time.Unix(1, 0), EndAt: time.Unix(2, 0)}, false},
	}
	for _, c := range cases {
		err := sm.RegisterSeason(c.cfg)
		if (err != nil) != c.want {
			t.Errorf("%s: err=%v want err=%v", c.name, err, c.want)
		}
	}
}
