package leaderboard

import (
	"testing"
)

func TestSkipList_BasicUpsertAndRank(t *testing.T) {
	sl := NewSkipList()

	sl.Upsert("alice", 100, "Alice")
	sl.Upsert("bob", 200, "Bob")
	sl.Upsert("carol", 150, "Carol")

	if sl.Size() != 3 {
		t.Errorf("size = %d, want 3", sl.Size())
	}

	if rank := sl.GetRank("bob"); rank != 1 {
		t.Errorf("bob rank = %d, want 1", rank)
	}
	if rank := sl.GetRank("carol"); rank != 2 {
		t.Errorf("carol rank = %d, want 2", rank)
	}
	if rank := sl.GetRank("alice"); rank != 3 {
		t.Errorf("alice rank = %d, want 3", rank)
	}
}

func TestSkipList_UpdateScore(t *testing.T) {
	sl := NewSkipList()
	sl.Upsert("alice", 100, "")
	sl.Upsert("bob", 200, "")

	// 更新 alice 分数超过 bob
	sl.Upsert("alice", 300, "")

	if rank := sl.GetRank("alice"); rank != 1 {
		t.Errorf("alice rank after update = %d, want 1", rank)
	}
	if sl.Size() != 2 {
		t.Errorf("size = %d, want 2 (update, not insert)", sl.Size())
	}
}

func TestSkipList_TopN(t *testing.T) {
	sl := NewSkipList()
	for i := 1; i <= 10; i++ {
		sl.Upsert(
			string(rune('a'+i-1)),
			float64(i*10),
			"",
		)
	}

	top3 := sl.TopN(3)
	if len(top3) != 3 {
		t.Fatalf("TopN(3) returned %d entries", len(top3))
	}

	// 最高分是 j (100)，然后 i (90)，h (80)
	if top3[0].Entry.PlayerID != "j" || top3[0].Entry.Score != 100 {
		t.Errorf("top1 = %+v, want {j 100}", top3[0])
	}
	if top3[1].Rank != 2 {
		t.Errorf("rank 2 = %d", top3[1].Rank)
	}
}

func TestSkipList_AroundMe(t *testing.T) {
	sl := NewSkipList()
	for i := 1; i <= 10; i++ {
		sl.Upsert(
			string(rune('a'+i-1)),
			float64(i*10),
			"",
		)
	}
	// 排名: j=1, i=2, h=3, g=4, f=5, e=6, d=7, c=8, b=9, a=10

	around := sl.AroundMe("f", 2)
	// 期望: d=7, e=6? 等等，按降序：f排名5，前2名是 g=4, h=3，后2名是 e=6, d=7
	// 结果应该是 h,g,f,e,d 排名 3,4,5,6,7
	if len(around) != 5 {
		t.Fatalf("AroundMe returned %d, want 5", len(around))
	}
	if around[0].Rank != 3 {
		t.Errorf("first rank = %d, want 3", around[0].Rank)
	}
	if around[2].Entry.PlayerID != "f" {
		t.Errorf("middle = %s, want f", around[2].Entry.PlayerID)
	}
}

func TestSkipList_Remove(t *testing.T) {
	sl := NewSkipList()
	sl.Upsert("alice", 100, "")
	sl.Upsert("bob", 200, "")

	if !sl.Remove("alice") {
		t.Error("Remove returned false")
	}
	if sl.GetRank("alice") != -1 {
		t.Error("alice still in skip list after remove")
	}
	if sl.Size() != 1 {
		t.Errorf("size = %d after remove", sl.Size())
	}
}

func TestBoard_MaxSize(t *testing.T) {
	b := NewBoard(BoardConfig{Name: "test", MaxSize: 3})

	b.UpdateScore("a", 10, "")
	b.UpdateScore("b", 20, "")
	b.UpdateScore("c", 30, "")
	b.UpdateScore("d", 40, "")

	if b.Size() != 3 {
		t.Errorf("size = %d, want 3 (MaxSize limit)", b.Size())
	}

	// 最低分 a=10 应该被裁掉
	if rank, _ := b.GetRank("a"); rank != -1 {
		t.Errorf("lowest score should be trimmed, but rank = %d", rank)
	}
	if rank, _ := b.GetRank("d"); rank != 1 {
		t.Errorf("highest score rank = %d, want 1", rank)
	}
}

func TestBoard_SnapshotRestore(t *testing.T) {
	b := NewBoard(BoardConfig{Name: "test"})
	b.UpdateScore("alice", 100, "Alice")
	b.UpdateScore("bob", 200, "Bob")

	snap := b.Snapshot()

	b2 := NewBoard(BoardConfig{Name: "test"})
	b2.RestoreFromSnapshot(snap)

	if b2.Size() != 2 {
		t.Errorf("restored size = %d, want 2", b2.Size())
	}
	if rank, _ := b2.GetRank("bob"); rank != 1 {
		t.Errorf("restored bob rank = %d, want 1", rank)
	}
}

func TestLeaderboardActor_ProcessMessage(t *testing.T) {
	la := NewLeaderboardActor(LeaderboardConfig{
		Boards: []BoardConfig{
			{Name: "global"},
		},
	})

	// 更新分数
	resp := la.ProcessMessage(&UpdateScoreRequest{
		Board:    "global",
		PlayerID: "p1",
		Score:    500,
	})
	usr, ok := resp.(*UpdateScoreResponse)
	if !ok {
		t.Fatalf("resp type = %T", resp)
	}
	if usr.Rank != 1 {
		t.Errorf("first update rank = %d", usr.Rank)
	}

	la.ProcessMessage(&UpdateScoreRequest{Board: "global", PlayerID: "p2", Score: 1000})
	la.ProcessMessage(&UpdateScoreRequest{Board: "global", PlayerID: "p3", Score: 300})

	// 查询排名
	rankResp := la.ProcessMessage(&GetRankRequest{Board: "global", PlayerID: "p2"})
	if gr, ok := rankResp.(*GetRankResponse); !ok || gr.Rank != 1 {
		t.Errorf("p2 rank = %+v", rankResp)
	}

	// TopN
	topResp := la.ProcessMessage(&GetTopNRequest{Board: "global", N: 2})
	topR, ok := topResp.(*GetTopNResponse)
	if !ok || len(topR.Entries) != 2 {
		t.Fatalf("topN = %+v", topResp)
	}
	if topR.Entries[0].Entry.PlayerID != "p2" {
		t.Errorf("top1 = %s, want p2", topR.Entries[0].Entry.PlayerID)
	}
}

func TestLeaderboardActor_SnapshotCallback(t *testing.T) {
	var snapshotCalled bool
	var snapshotData map[string]*BoardSnapshot

	la := NewLeaderboardActor(LeaderboardConfig{
		Boards: []BoardConfig{{Name: "daily"}},
		SnapshotFn: func(s map[string]*BoardSnapshot) {
			snapshotCalled = true
			snapshotData = s
		},
		SnapshotInterval: 3,
	})

	la.ProcessMessage(&UpdateScoreRequest{Board: "daily", PlayerID: "p1", Score: 100})
	la.ProcessMessage(&UpdateScoreRequest{Board: "daily", PlayerID: "p2", Score: 200})
	if snapshotCalled {
		t.Error("snapshot called too early")
	}

	la.ProcessMessage(&UpdateScoreRequest{Board: "daily", PlayerID: "p3", Score: 300})
	if !snapshotCalled {
		t.Error("snapshot not called after interval")
	}
	if _, ok := snapshotData["daily"]; !ok {
		t.Error("snapshot missing daily board")
	}
}
