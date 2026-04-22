package syncx

import "testing"

// TestStateSyncDeltaModeOnJoin 验证启用 Delta 模式后，OnPlayerJoin 发送 FrameDelta(Snapshot)
// 而不是 StateSnapshot，保证客户端 DeltaDecoder 首包即可重建全量状态
func TestStateSyncDeltaModeOnJoin(t *testing.T) {
	var received []interface{}
	broadcast := func(_ []string, msg interface{}) {
		received = append(received, msg)
	}
	updateFn := func(_ []PlayerInput) map[string]interface{} {
		return map[string]interface{}{"x": int64(1), "y": int64(2)}
	}

	cfg := DefaultSyncConfig()
	cfg.SnapshotInterval = 0
	cfg.EnableDeltaCompression = true
	ss := NewStateSyncRoom(cfg, broadcast, updateFn)

	// Tick 一次让编码器填充
	ss.Tick(1)

	received = nil
	ss.OnPlayerJoin("p1")

	if len(received) != 1 {
		t.Fatalf("expected 1 snapshot on join, got %d", len(received))
	}
	fd, ok := received[0].(*FrameDelta)
	if !ok {
		t.Fatalf("expected *FrameDelta, got %T", received[0])
	}
	if !fd.Snapshot {
		t.Fatal("FrameDelta.Snapshot should be true on join")
	}
	if len(fd.Entities) == 0 {
		t.Fatal("snapshot should carry entity state")
	}
	if ss.Metrics().SnapshotFrames.Load() == 0 {
		t.Fatal("SnapshotFrames metric should have incremented")
	}
}

// TestStateSyncDeltaModeEncode 验证 Tick 产生 FrameDelta 并计入 DeltaFrames
func TestStateSyncDeltaModeEncode(t *testing.T) {
	state := map[string]interface{}{"hp": int64(100)}
	var received []interface{}
	broadcast := func(_ []string, msg interface{}) {
		received = append(received, msg)
	}
	updateFn := func(_ []PlayerInput) map[string]interface{} { return state }

	cfg := DefaultSyncConfig()
	cfg.SnapshotInterval = 0
	cfg.EnableDeltaCompression = true
	ss := NewStateSyncRoom(cfg, broadcast, updateFn)

	// Tick 1：首次 → 所有字段都是新的
	ss.Tick(1)
	if len(received) != 1 {
		t.Fatalf("expected 1 frame on first tick, got %d", len(received))
	}
	_, ok := received[0].(*FrameDelta)
	if !ok {
		t.Fatalf("expected *FrameDelta, got %T", received[0])
	}

	// Tick 2：无变化 → 不广播
	received = nil
	ss.Tick(2)
	if len(received) != 0 {
		t.Fatalf("expected 0 frame when unchanged, got %d", len(received))
	}
	if ss.Metrics().EmptyFrames.Load() == 0 {
		t.Fatal("EmptyFrames metric should have incremented on no-change tick")
	}

	// Tick 3：字段变化 → 增量帧
	state["hp"] = int64(80)
	received = nil
	ss.Tick(3)
	if len(received) != 1 {
		t.Fatalf("expected 1 delta frame after state change, got %d", len(received))
	}
	fd := received[0].(*FrameDelta)
	if fd.Snapshot {
		t.Fatal("delta frame should not have Snapshot=true on non-snapshot-interval tick")
	}
	if ss.Metrics().DeltaFrames.Load() == 0 {
		t.Fatal("DeltaFrames metric should have incremented")
	}
}

// TestStateSyncResyncRequest 验证 OnResyncRequest 响应全量快照
func TestStateSyncResyncRequest(t *testing.T) {
	state := map[string]interface{}{"hp": int64(100)}
	var deliveredTo [][]string
	broadcast := func(playerIDs []string, msg interface{}) {
		if fd, ok := msg.(*FrameDelta); ok && fd.Snapshot {
			deliveredTo = append(deliveredTo, playerIDs)
		}
	}
	updateFn := func(_ []PlayerInput) map[string]interface{} { return state }

	cfg := DefaultSyncConfig()
	cfg.SnapshotInterval = 0
	cfg.EnableDeltaCompression = true
	ss := NewStateSyncRoom(cfg, broadcast, updateFn)

	ss.OnPlayerJoin("p1")
	ss.OnPlayerJoin("p2")

	// 先 Tick 一次让状态填充
	ss.Tick(1)

	deliveredTo = nil
	ss.OnResyncRequest(&ResyncRequest{PlayerID: "p2", Reason: "timeout"})
	// Tick 前不应立即发送
	if len(deliveredTo) != 0 {
		t.Fatalf("resync should be deferred to next tick, got %d deliveries", len(deliveredTo))
	}

	ss.Tick(2)

	// 期望精确发给 p2 一次 Snapshot
	if len(deliveredTo) != 1 {
		t.Fatalf("expected 1 unicast snapshot after resync, got %d", len(deliveredTo))
	}
	if len(deliveredTo[0]) != 1 || deliveredTo[0][0] != "p2" {
		t.Fatalf("snapshot should be unicast to p2, got %v", deliveredTo[0])
	}
	if ss.Metrics().ResyncRequests.Load() != 1 {
		t.Fatalf("ResyncRequests metric: want 1, got %d", ss.Metrics().ResyncRequests.Load())
	}
}

// TestStateSyncResyncRequestNoDelta 非 Delta 模式下退化为直接发送 StateSnapshot
func TestStateSyncResyncRequestNoDelta(t *testing.T) {
	state := map[string]interface{}{"hp": int64(100)}
	var snapshots int
	broadcast := func(_ []string, msg interface{}) {
		if _, ok := msg.(*StateSnapshot); ok {
			snapshots++
		}
	}
	updateFn := func(_ []PlayerInput) map[string]interface{} { return state }

	cfg := DefaultSyncConfig()
	cfg.SnapshotInterval = 0
	ss := NewStateSyncRoom(cfg, broadcast, updateFn)
	ss.OnPlayerJoin("p1")
	snapshots = 0

	ss.OnResyncRequest(&ResyncRequest{PlayerID: "p1"})
	if snapshots != 1 {
		t.Fatalf("expected 1 snapshot in non-delta mode, got %d", snapshots)
	}
}

// TestStateSyncResyncRequestUnknownPlayer 未知玩家请求被忽略
func TestStateSyncResyncRequestUnknownPlayer(t *testing.T) {
	state := map[string]interface{}{"hp": int64(100)}
	broadcast := func(_ []string, _ interface{}) {}
	updateFn := func(_ []PlayerInput) map[string]interface{} { return state }

	cfg := DefaultSyncConfig()
	cfg.EnableDeltaCompression = true
	ss := NewStateSyncRoom(cfg, broadcast, updateFn)

	ss.OnResyncRequest(nil)
	ss.OnResyncRequest(&ResyncRequest{PlayerID: ""})
	ss.OnResyncRequest(&ResyncRequest{PlayerID: "unknown"})
	if ss.Metrics().ResyncRequests.Load() != 0 {
		t.Fatal("invalid resync requests should not count")
	}
}

// TestStateSyncCompressionRatio 多帧只改一个字段时，压缩率应明显 < 1
func TestStateSyncCompressionRatio(t *testing.T) {
	// 构造 20 个字段的状态，每帧只变 1 个
	state := make(map[string]interface{}, 20)
	for i := 0; i < 20; i++ {
		state[keyN(i)] = int64(i)
	}
	broadcast := func(_ []string, _ interface{}) {}
	updateFn := func(_ []PlayerInput) map[string]interface{} { return state }

	cfg := DefaultSyncConfig()
	cfg.SnapshotInterval = 0
	cfg.EnableDeltaCompression = true
	ss := NewStateSyncRoom(cfg, broadcast, updateFn)

	// Tick 1 为首帧（全量）；再改 1 字段跑 50 帧
	ss.Tick(1)
	for i := uint64(2); i <= 51; i++ {
		state[keyN(0)] = int64(i) // 只动 field0
		ss.Tick(i)
	}

	ratio := ss.Metrics().CompressionRatio()
	if ratio <= 0 || ratio >= 0.5 {
		t.Fatalf("expected compression ratio in (0, 0.5), got %.3f", ratio)
	}
	if ss.Metrics().DeltaFrames.Load() == 0 {
		t.Fatal("DeltaFrames metric should have accumulated")
	}
}

// TestStateSyncSnapshotInterval Delta 模式下周期性快照正常触发
func TestStateSyncSnapshotInterval(t *testing.T) {
	state := map[string]interface{}{"x": int64(0)}
	snapshots := 0
	broadcast := func(_ []string, msg interface{}) {
		if fd, ok := msg.(*FrameDelta); ok && fd.Snapshot {
			snapshots++
		}
	}
	updateFn := func(_ []PlayerInput) map[string]interface{} { return state }

	cfg := DefaultSyncConfig()
	cfg.SnapshotInterval = 5
	cfg.EnableDeltaCompression = true
	ss := NewStateSyncRoom(cfg, broadcast, updateFn)

	for i := uint64(1); i <= 10; i++ {
		state["x"] = int64(i) // 每帧变化
		ss.Tick(i)
	}

	// 帧 5、10 各一次快照
	if snapshots != 2 {
		t.Fatalf("expected 2 snapshot frames, got %d", snapshots)
	}
}

func keyN(i int) string {
	return "f" + string(rune('0'+i/10)) + string(rune('0'+i%10))
}
