package syncx

import (
	"testing"
	"time"
)

func TestFrameSyncRoom_BasicFlow(t *testing.T) {
	var received []interface{}
	broadcast := func(playerIDs []string, msg interface{}) {
		received = append(received, msg)
	}

	config := DefaultSyncConfig()
	fs := NewFrameSyncRoom(config, broadcast)
	fs.OnPlayerJoin("p1")
	fs.OnPlayerJoin("p2")

	// 玩家提交输入
	fs.OnPlayerInput("p1", &PlayerInput{
		PlayerID: "p1",
		FrameNum: 0,
		Actions:  []InputAction{{Type: 1}},
	})
	fs.OnPlayerInput("p2", &PlayerInput{
		PlayerID: "p2",
		FrameNum: 0,
		Actions:  []InputAction{{Type: 2}},
	})

	// 执行一帧
	fs.Tick(0)

	if len(received) != 1 {
		t.Fatalf("expected 1 broadcast, got %d", len(received))
	}

	frame, ok := received[0].(*FrameData)
	if !ok {
		t.Fatal("expected *FrameData")
	}
	if frame.FrameNum != 0 {
		t.Errorf("expected frame 0, got %d", frame.FrameNum)
	}
	if len(frame.Inputs) != 2 {
		t.Errorf("expected 2 inputs, got %d", len(frame.Inputs))
	}
}

func TestFrameSyncRoom_MissingInput(t *testing.T) {
	var received []interface{}
	broadcast := func(playerIDs []string, msg interface{}) {
		received = append(received, msg)
	}

	fs := NewFrameSyncRoom(DefaultSyncConfig(), broadcast)
	fs.OnPlayerJoin("p1")
	fs.OnPlayerJoin("p2")

	// 只有 p1 提交了输入
	fs.OnPlayerInput("p1", &PlayerInput{
		PlayerID: "p1",
		FrameNum: 0,
		Actions:  []InputAction{{Type: 1}},
	})

	fs.Tick(0)

	frame := received[0].(*FrameData)
	if len(frame.Inputs) != 2 {
		t.Errorf("expected 2 inputs (one empty), got %d", len(frame.Inputs))
	}
}

func TestFrameSyncRoom_PlayerLeave(t *testing.T) {
	var received []interface{}
	broadcast := func(_ []string, msg interface{}) {
		received = append(received, msg)
	}

	fs := NewFrameSyncRoom(DefaultSyncConfig(), broadcast)
	fs.OnPlayerJoin("p1")
	fs.OnPlayerJoin("p2")
	fs.OnPlayerLeave("p2")

	fs.Tick(0)

	frame := received[0].(*FrameData)
	if len(frame.Inputs) != 1 {
		t.Errorf("expected 1 input after leave, got %d", len(frame.Inputs))
	}
}

func TestFrameSyncRoom_HashConsistency(t *testing.T) {
	var frames []*FrameData
	broadcast := func(_ []string, msg interface{}) {
		if f, ok := msg.(*FrameData); ok {
			frames = append(frames, f)
		}
	}

	fs := NewFrameSyncRoom(DefaultSyncConfig(), broadcast)
	fs.OnPlayerJoin("p1")

	// 相同输入应产生相同哈希
	fs.OnPlayerInput("p1", &PlayerInput{PlayerID: "p1", FrameNum: 0, Actions: []InputAction{{Type: 1}}})
	fs.Tick(0)

	fs.OnPlayerInput("p1", &PlayerInput{PlayerID: "p1", FrameNum: 1, Actions: []InputAction{{Type: 1}}})
	fs.Tick(1)

	if frames[0].Hash != frames[1].Hash {
		t.Error("same inputs should produce same hash")
	}
}

func TestStateSyncRoom_BasicFlow(t *testing.T) {
	var received []interface{}
	broadcast := func(playerIDs []string, msg interface{}) {
		received = append(received, msg)
	}

	posX := 0.0
	updateFn := func(inputs []PlayerInput) map[string]interface{} {
		for _, input := range inputs {
			for _, action := range input.Actions {
				if action.Type == 1 { // move right
					posX += 1.0
				}
			}
		}
		return map[string]interface{}{"posX": posX}
	}

	config := DefaultSyncConfig()
	config.SnapshotInterval = 0 // 禁用定期快照
	ss := NewStateSyncRoom(config, broadcast, updateFn)
	ss.OnPlayerJoin("p1")

	// 收到完整快照（join 时）
	if len(received) != 1 {
		t.Fatalf("expected 1 snapshot on join, got %d", len(received))
	}
	if _, ok := received[0].(*StateSnapshot); !ok {
		t.Fatal("expected StateSnapshot on join")
	}

	// 提交输入并 Tick
	received = nil
	ss.OnPlayerInput("p1", &PlayerInput{
		PlayerID: "p1",
		FrameNum: 1,
		Actions:  []InputAction{{Type: 1}},
	})
	ss.Tick(1)

	if len(received) != 1 {
		t.Fatalf("expected 1 delta, got %d", len(received))
	}
	delta, ok := received[0].(*StateDelta)
	if !ok {
		t.Fatal("expected *StateDelta")
	}
	if delta.Changed["posX"] != 1.0 {
		t.Errorf("expected posX=1.0, got %v", delta.Changed["posX"])
	}
}

func TestStateSyncRoom_NoDelta(t *testing.T) {
	var received []interface{}
	broadcast := func(_ []string, msg interface{}) {
		received = append(received, msg)
	}

	state := map[string]interface{}{"x": 0}
	updateFn := func(inputs []PlayerInput) map[string]interface{} {
		return state
	}

	config := DefaultSyncConfig()
	config.SnapshotInterval = 0
	ss := NewStateSyncRoom(config, broadcast, updateFn)
	ss.OnPlayerJoin("p1")

	received = nil
	ss.Tick(1)
	ss.Tick(2)

	// 第一帧有 delta（prevState 为空→有x），第二帧无变化
	if len(received) != 1 {
		t.Errorf("expected 1 delta (no change on second tick), got %d", len(received))
	}
}

func TestStateSyncRoom_Snapshot(t *testing.T) {
	var snapshots int
	broadcast := func(_ []string, msg interface{}) {
		if _, ok := msg.(*StateSnapshot); ok {
			snapshots++
		}
	}

	updateFn := func(inputs []PlayerInput) map[string]interface{} {
		return map[string]interface{}{"x": 1}
	}

	config := DefaultSyncConfig()
	config.SnapshotInterval = 5
	ss := NewStateSyncRoom(config, broadcast, updateFn)

	for i := uint64(1); i <= 10; i++ {
		ss.Tick(i)
	}

	// 帧 5 和 帧 10 应发送快照
	if snapshots != 2 {
		t.Errorf("expected 2 snapshots, got %d", snapshots)
	}
}

func TestLatencyTracker(t *testing.T) {
	lt := NewLatencyTracker(5)
	lt.OnPlayerJoin("p1")

	// 模拟 5 次 Pong
	for i := 0; i < 5; i++ {
		now := time.Now().UnixNano()
		rtt := lt.RecordPong(&PongMsg{
			PlayerID:  "p1",
			Timestamp: now - int64(10*time.Millisecond), // 模拟 10ms RTT
		})
		if rtt < 9*time.Millisecond || rtt > 20*time.Millisecond {
			t.Errorf("unexpected RTT: %v", rtt)
		}
	}

	avgRTT := lt.GetRTT("p1")
	if avgRTT < 5*time.Millisecond || avgRTT > 20*time.Millisecond {
		t.Errorf("unexpected avg RTT: %v", avgRTT)
	}

	halfRTT := lt.GetHalfRTT("p1")
	if halfRTT > avgRTT {
		t.Error("half RTT should be <= RTT")
	}
}

func TestLatencyTracker_CompensationFrames(t *testing.T) {
	lt := NewLatencyTracker(3)
	lt.OnPlayerJoin("p1")

	// 模拟 100ms RTT
	for i := 0; i < 3; i++ {
		lt.RecordPong(&PongMsg{
			PlayerID:  "p1",
			Timestamp: time.Now().UnixNano() - int64(100*time.Millisecond),
		})
	}

	// 50ms 帧间隔（20fps），100ms RTT / 2 = 50ms → 1 帧
	frames := lt.CompensationFrames("p1", 50*time.Millisecond)
	if frames < 1 {
		t.Errorf("expected at least 1 compensation frame, got %d", frames)
	}
}

func TestLatencyTracker_PlayerLeave(t *testing.T) {
	lt := NewLatencyTracker(5)
	lt.OnPlayerJoin("p1")
	lt.OnPlayerLeave("p1")

	rtt := lt.GetRTT("p1")
	if rtt != 0 {
		t.Errorf("expected 0 RTT after leave, got %v", rtt)
	}
}
