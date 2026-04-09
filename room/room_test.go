package room

import (
	"fmt"
	"math"
	"testing"
	"time"
)

func TestRoomLifecycle(t *testing.T) {
	mgr := NewRoomManager()

	// 创建房间
	config := RoomConfig{
		RoomType:   "pvp_1v1",
		MinPlayers: 2,
		MaxPlayers: 2,
	}

	roomID, err := mgr.CreateRoom(config, &PlayerInfo{PlayerID: "p1", Name: "Player1"})
	if err != nil {
		t.Fatalf("create room: %v", err)
	}

	// 查找房间
	info, found := mgr.FindRoom(roomID)
	if !found {
		t.Fatal("room not found")
	}
	if info.PlayerCount() != 1 {
		t.Fatalf("expected 1 player, got %d", info.PlayerCount())
	}
	if info.State != RoomStateWaiting {
		t.Fatalf("expected waiting, got %v", info.State)
	}

	// 第二个玩家加入 → 自动开始（满员）
	err = mgr.JoinRoom(roomID, PlayerInfo{PlayerID: "p2", Name: "Player2"})
	if err != nil {
		t.Fatalf("join room: %v", err)
	}

	info, _ = mgr.FindRoom(roomID)
	if info.State != RoomStateRunning {
		t.Fatalf("expected running after full, got %v", info.State)
	}

	// 结束游戏
	err = mgr.FinishGame(roomID)
	if err != nil {
		t.Fatalf("finish game: %v", err)
	}

	info, _ = mgr.FindRoom(roomID)
	if info.State != RoomStateFinished {
		t.Fatalf("expected finished, got %v", info.State)
	}

	// 销毁房间
	mgr.DestroyRoom(roomID)
	if _, found := mgr.FindRoom(roomID); found {
		t.Fatal("room should be destroyed")
	}
}

func TestRoomJoinErrors(t *testing.T) {
	mgr := NewRoomManager()

	config := RoomConfig{
		RoomType:   "pvp",
		MinPlayers: 2,
		MaxPlayers: 2,
	}

	roomID, _ := mgr.CreateRoom(config, nil)

	// 加入两个玩家
	mgr.JoinRoom(roomID, PlayerInfo{PlayerID: "p1"})
	mgr.JoinRoom(roomID, PlayerInfo{PlayerID: "p2"})

	// 房间满了，不能加入
	err := mgr.JoinRoom(roomID, PlayerInfo{PlayerID: "p3"})
	if err == nil {
		t.Fatal("expected error for full room")
	}

	// 房间不存在
	err = mgr.JoinRoom("nonexistent", PlayerInfo{PlayerID: "p1"})
	if err == nil {
		t.Fatal("expected error for missing room")
	}
}

func TestRoomLeave(t *testing.T) {
	mgr := NewRoomManager()

	config := RoomConfig{
		RoomType:   "pvp",
		MinPlayers: 2,
		MaxPlayers: 4,
	}

	roomID, _ := mgr.CreateRoom(config, nil)
	mgr.JoinRoom(roomID, PlayerInfo{PlayerID: "p1"})
	mgr.JoinRoom(roomID, PlayerInfo{PlayerID: "p2"})

	// 离开
	err := mgr.LeaveRoom(roomID, "p1")
	if err != nil {
		t.Fatalf("leave: %v", err)
	}

	info, _ := mgr.FindRoom(roomID)
	if info.PlayerCount() != 1 {
		t.Fatalf("expected 1 player after leave, got %d", info.PlayerCount())
	}

	// 离开不存在的玩家
	err = mgr.LeaveRoom(roomID, "p99")
	if err == nil {
		t.Fatal("expected error for missing player")
	}
}

func TestListRooms(t *testing.T) {
	mgr := NewRoomManager()

	mgr.CreateRoom(RoomConfig{RoomType: "pvp", MinPlayers: 2, MaxPlayers: 2}, nil)
	mgr.CreateRoom(RoomConfig{RoomType: "pvp", MinPlayers: 2, MaxPlayers: 2}, nil)
	mgr.CreateRoom(RoomConfig{RoomType: "pve", MinPlayers: 1, MaxPlayers: 4}, nil)

	// 列出所有房间
	all := mgr.ListRooms("", nil)
	if len(all) != 3 {
		t.Fatalf("expected 3 rooms, got %d", len(all))
	}

	// 按类型过滤
	pvp := mgr.ListRooms("pvp", nil)
	if len(pvp) != 2 {
		t.Fatalf("expected 2 pvp rooms, got %d", len(pvp))
	}

	// 按状态过滤
	waiting := RoomStateWaiting
	wRooms := mgr.ListRooms("", &waiting)
	if len(wRooms) != 3 {
		t.Fatalf("expected 3 waiting rooms, got %d", len(wRooms))
	}
}

func TestFindAvailableRoom(t *testing.T) {
	mgr := NewRoomManager()

	config := RoomConfig{RoomType: "pvp", MinPlayers: 2, MaxPlayers: 2}

	// 无可用房间
	_, found := mgr.FindAvailableRoom("pvp")
	if found {
		t.Fatal("expected no available room")
	}

	// 创建一个
	mgr.CreateRoom(config, nil)

	// 找到可用房间
	info, found := mgr.FindAvailableRoom("pvp")
	if !found {
		t.Fatal("expected available room")
	}
	if info.RoomType != "pvp" {
		t.Fatalf("expected pvp, got %s", info.RoomType)
	}
}

func TestRoomEvents(t *testing.T) {
	mgr := NewRoomManager()
	events := make([]RoomEvent, 0)
	mgr.SetEventHandler(func(event RoomEvent) {
		events = append(events, event)
	})

	config := RoomConfig{RoomType: "pvp", MinPlayers: 2, MaxPlayers: 2}
	roomID, _ := mgr.CreateRoom(config, nil)
	mgr.JoinRoom(roomID, PlayerInfo{PlayerID: "p1"})
	mgr.JoinRoom(roomID, PlayerInfo{PlayerID: "p2"}) // 自动 start
	mgr.FinishGame(roomID)
	mgr.DestroyRoom(roomID)

	expectedTypes := []string{"created", "player_joined", "player_joined", "started", "finished", "destroyed"}
	if len(events) != len(expectedTypes) {
		t.Fatalf("expected %d events, got %d: %v", len(expectedTypes), len(events), events)
	}
	for i, et := range expectedTypes {
		if events[i].Type != et {
			t.Fatalf("event %d: expected %s, got %s", i, et, events[i].Type)
		}
	}
}

// --- 匹配器测试 ---

func TestQueueMatcher(t *testing.T) {
	m := NewQueueMatcher(2)

	// 不够人
	if result := m.Match(); result != nil {
		t.Fatal("expected nil match")
	}

	m.AddPlayer(PlayerInfo{PlayerID: "p1"})
	m.AddPlayer(PlayerInfo{PlayerID: "p2"})
	m.AddPlayer(PlayerInfo{PlayerID: "p3"})

	// 第一次匹配
	result := m.Match()
	if result == nil || len(result) != 2 {
		t.Fatalf("expected 2 matched players, got %v", result)
	}
	if result[0].PlayerID != "p1" || result[1].PlayerID != "p2" {
		t.Fatalf("expected p1,p2, got %s,%s", result[0].PlayerID, result[1].PlayerID)
	}

	// 队列剩余 1 人
	if m.QueueSize() != 1 {
		t.Fatalf("expected queue size 1, got %d", m.QueueSize())
	}
}

func TestEloMatcher(t *testing.T) {
	m := NewEloMatcher(EloMatcherConfig{
		PlayersPerMatch: 2,
		InitialRange:    100,
		MaxRange:        500,
	})

	// 差距太大，无法匹配
	m.AddPlayer(PlayerInfo{PlayerID: "p1", Rating: 1000})
	m.AddPlayer(PlayerInfo{PlayerID: "p2", Rating: 1500})

	result := m.Match()
	if result != nil {
		t.Fatal("expected no match for large ELO gap")
	}

	// 添加一个范围内的玩家
	m.AddPlayer(PlayerInfo{PlayerID: "p3", Rating: 1050})

	result = m.Match()
	if result == nil || len(result) != 2 {
		t.Fatal("expected match for close ELO")
	}
	// p1(1000) 和 p3(1050) 应该匹配
	ids := map[string]bool{result[0].PlayerID: true, result[1].PlayerID: true}
	if !ids["p1"] || !ids["p3"] {
		t.Fatalf("expected p1+p3, got %s+%s", result[0].PlayerID, result[1].PlayerID)
	}
}

func TestConditionMatcher(t *testing.T) {
	// 匹配条件：等级差不超过 5
	m := NewConditionMatcher(2, func(a, b PlayerInfo) bool {
		return math.Abs(float64(a.Level-b.Level)) <= 5
	})

	m.AddPlayer(PlayerInfo{PlayerID: "p1", Level: 10})
	m.AddPlayer(PlayerInfo{PlayerID: "p2", Level: 50}) // 差距太大
	m.AddPlayer(PlayerInfo{PlayerID: "p3", Level: 12}) // 与 p1 匹配

	result := m.Match()
	if result == nil || len(result) != 2 {
		t.Fatal("expected match")
	}
	ids := map[string]bool{result[0].PlayerID: true, result[1].PlayerID: true}
	if !ids["p1"] || !ids["p3"] {
		t.Fatalf("expected p1+p3, got %s+%s", result[0].PlayerID, result[1].PlayerID)
	}
}

func TestMatcherRemovePlayer(t *testing.T) {
	m := NewQueueMatcher(2)
	m.AddPlayer(PlayerInfo{PlayerID: "p1"})
	m.AddPlayer(PlayerInfo{PlayerID: "p2"})

	m.RemovePlayer("p1")
	if m.QueueSize() != 1 {
		t.Fatalf("expected 1 after remove, got %d", m.QueueSize())
	}

	if result := m.Match(); result != nil {
		t.Fatal("expected no match after remove")
	}
}

func TestMatchServiceIntegration(t *testing.T) {
	mgr := NewRoomManager()
	svc := NewMatchService(mgr)

	config := RoomConfig{RoomType: "pvp", MinPlayers: 2, MaxPlayers: 2}
	svc.RegisterMatcher("pvp", NewQueueMatcher(2), config)

	// 添加玩家
	svc.EnqueuePlayer("pvp", PlayerInfo{PlayerID: "p1"})
	svc.EnqueuePlayer("pvp", PlayerInfo{PlayerID: "p2"})

	// 手动 tick
	svc.tick()

	// 应该创建了一个房间
	rooms := mgr.ListRooms("pvp", nil)
	if len(rooms) != 1 {
		t.Fatalf("expected 1 room, got %d", len(rooms))
	}
	if rooms[0].PlayerCount() != 2 {
		t.Fatalf("expected 2 players in room, got %d", rooms[0].PlayerCount())
	}
}

func TestMatchServiceUnknownType(t *testing.T) {
	mgr := NewRoomManager()
	svc := NewMatchService(mgr)

	err := svc.EnqueuePlayer("unknown", PlayerInfo{PlayerID: "p1"})
	if err == nil {
		t.Fatal("expected error for unknown room type")
	}
}

func TestRoomStateStrings(t *testing.T) {
	states := []RoomState{RoomStateWaiting, RoomStateRunning, RoomStateFinished, RoomStateStopped}
	expected := []string{"waiting", "running", "finished", "stopped"}
	for i, s := range states {
		if s.String() != expected[i] {
			t.Fatalf("state %d: expected %s, got %s", i, expected[i], s.String())
		}
	}
}

func TestRoomStartNotEnoughPlayers(t *testing.T) {
	mgr := NewRoomManager()
	config := RoomConfig{RoomType: "pvp", MinPlayers: 3, MaxPlayers: 4}
	roomID, _ := mgr.CreateRoom(config, nil)
	mgr.JoinRoom(roomID, PlayerInfo{PlayerID: "p1"})

	err := mgr.StartGame(roomID)
	if err == nil {
		t.Fatal("expected error for not enough players")
	}
}

func TestRoomLeaveAutoDestroy(t *testing.T) {
	mgr := NewRoomManager()
	config := RoomConfig{RoomType: "pvp", MinPlayers: 2, MaxPlayers: 4}
	roomID, _ := mgr.CreateRoom(config, nil)
	mgr.JoinRoom(roomID, PlayerInfo{PlayerID: "p1"})

	// 离开 → 等待中无人 → 自动销毁
	mgr.LeaveRoom(roomID, "p1")
	if _, found := mgr.FindRoom(roomID); found {
		t.Fatal("room should be auto-destroyed when empty in waiting state")
	}
}

func TestRoomInfoMethods(t *testing.T) {
	info := &RoomInfo{
		State:   RoomStateWaiting,
		Players: []PlayerInfo{{PlayerID: "p1"}, {PlayerID: "p2"}},
		Config:  RoomConfig{MinPlayers: 2, MaxPlayers: 4},
	}

	if info.PlayerCount() != 2 {
		t.Fatal("expected 2")
	}
	if info.IsFull() {
		t.Fatal("expected not full")
	}
	if !info.CanStart() {
		t.Fatal("expected can start")
	}
}

func TestRoomWaitTimeout(t *testing.T) {
	mgr := NewRoomManager()
	config := RoomConfig{
		RoomType:    "pvp",
		MinPlayers:  2,
		MaxPlayers:  4,
		WaitTimeout: 100 * time.Millisecond,
	}
	roomID, _ := mgr.CreateRoom(config, nil)
	mgr.JoinRoom(roomID, PlayerInfo{PlayerID: "p1"})

	// 等待超时销毁（不够人开始）
	time.Sleep(200 * time.Millisecond)
	if _, found := mgr.FindRoom(roomID); found {
		t.Fatal("room should be destroyed after wait timeout with insufficient players")
	}
}

func TestMultipleMatchRounds(t *testing.T) {
	mgr := NewRoomManager()
	svc := NewMatchService(mgr)

	config := RoomConfig{RoomType: "pvp", MinPlayers: 2, MaxPlayers: 2}
	svc.RegisterMatcher("pvp", NewQueueMatcher(2), config)

	for i := 0; i < 6; i++ {
		svc.EnqueuePlayer("pvp", PlayerInfo{PlayerID: fmt.Sprintf("p%d", i)})
	}

	svc.tick()

	rooms := mgr.ListRooms("pvp", nil)
	if len(rooms) != 3 {
		t.Fatalf("expected 3 rooms, got %d", len(rooms))
	}
}
