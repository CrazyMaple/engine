package syncx

import (
	"math"
	"testing"
)

// 简单预测函数：位置 += 速度
func testPredictFn(state map[string]interface{}, input *PlayerInput) map[string]interface{} {
	newState := make(map[string]interface{}, len(state))
	for k, v := range state {
		newState[k] = v
	}
	for _, action := range input.Actions {
		switch action.Type {
		case 1: // 移动
			if dx, ok := action.Data["dx"]; ok {
				x, _ := newState["x"].(float64)
				newState["x"] = x + dx.(float64)
			}
			if dy, ok := action.Data["dy"]; ok {
				y, _ := newState["y"].(float64)
				newState["y"] = y + dy.(float64)
			}
		}
	}
	return newState
}

func TestPredictionClient_BasicPredict(t *testing.T) {
	config := DefaultPredictionConfig()
	pc := NewPredictionClient("p1", config, testPredictFn, nil, 20)
	pc.displayState = map[string]interface{}{"x": 0.0, "y": 0.0}

	// 预测一个移动操作
	result := pc.PredictInput(&PlayerInput{
		PlayerID: "p1",
		FrameNum: 1,
		Actions: []InputAction{{
			Type: 1,
			Data: map[string]interface{}{"dx": 5.0},
		}},
	})

	x, _ := result["x"].(float64)
	if x != 5.0 {
		t.Errorf("predicted x = %f, want 5.0", x)
	}
	if pc.PendingCount() != 1 {
		t.Errorf("pending count = %d, want 1", pc.PendingCount())
	}
}

func TestPredictionClient_Reconcile(t *testing.T) {
	config := DefaultPredictionConfig()
	config.SmoothCorrection = false
	pc := NewPredictionClient("p1", config, testPredictFn, nil, 20)
	pc.displayState = map[string]interface{}{"x": 0.0, "y": 0.0}

	// 预测两帧输入
	pc.PredictInput(&PlayerInput{
		PlayerID: "p1", FrameNum: 1,
		Actions: []InputAction{{Type: 1, Data: map[string]interface{}{"dx": 5.0}}},
	})
	pc.PredictInput(&PlayerInput{
		PlayerID: "p1", FrameNum: 2,
		Actions: []InputAction{{Type: 1, Data: map[string]interface{}{"dx": 3.0}}},
	})

	// 服务端确认帧 1，状态与预测一致
	result := pc.ReconcileServerState(1, map[string]interface{}{"x": 5.0, "y": 0.0})

	// 帧 2 的预测应重新应用：5.0 + 3.0 = 8.0
	x, _ := result["x"].(float64)
	if x != 8.0 {
		t.Errorf("reconciled x = %f, want 8.0", x)
	}
	if pc.PendingCount() != 1 {
		t.Errorf("pending count after reconcile = %d, want 1", pc.PendingCount())
	}
}

func TestPredictionClient_ReconcileCorrection(t *testing.T) {
	config := DefaultPredictionConfig()
	config.SmoothCorrection = false
	pc := NewPredictionClient("p1", config, testPredictFn, nil, 20)
	pc.displayState = map[string]interface{}{"x": 0.0, "y": 0.0}

	// 预测一帧
	pc.PredictInput(&PlayerInput{
		PlayerID: "p1", FrameNum: 1,
		Actions: []InputAction{{Type: 1, Data: map[string]interface{}{"dx": 5.0}}},
	})

	// 服务端返回不同值（如碰撞后修正）
	result := pc.ReconcileServerState(1, map[string]interface{}{"x": 3.0, "y": 0.0})

	// 无待确认输入，应直接采用服务端值
	x, _ := result["x"].(float64)
	if x != 3.0 {
		t.Errorf("corrected x = %f, want 3.0", x)
	}
}

func TestPredictionClient_MaxPredictionFrames(t *testing.T) {
	config := DefaultPredictionConfig()
	config.MaxPredictionFrames = 3
	pc := NewPredictionClient("p1", config, testPredictFn, nil, 20)
	pc.displayState = map[string]interface{}{"x": 0.0}

	// 添加 4 个预测帧（超过最大 3）
	for i := 1; i <= 4; i++ {
		pc.PredictInput(&PlayerInput{
			PlayerID: "p1", FrameNum: uint64(i),
			Actions: []InputAction{{Type: 1, Data: map[string]interface{}{"dx": 1.0}}},
		})
	}

	// 最多保留 3 个
	if pc.PendingCount() != 3 {
		t.Errorf("pending count = %d, want 3 (capped)", pc.PendingCount())
	}
}

// --- Rollback 测试 ---

func testSimulateFn(state map[string]interface{}, inputs []PlayerInput) map[string]interface{} {
	newState := make(map[string]interface{}, len(state))
	for k, v := range state {
		newState[k] = v
	}
	for _, input := range inputs {
		for _, action := range input.Actions {
			if action.Type == 1 {
				if dx, ok := action.Data["dx"]; ok {
					x, _ := newState["x"].(float64)
					newState["x"] = x + dx.(float64)
				}
			}
		}
	}
	return newState
}

func TestRollbackManager_BasicTick(t *testing.T) {
	var broadcasts []interface{}
	broadcast := func(playerIDs []string, msg interface{}) {
		broadcasts = append(broadcasts, msg)
	}

	config := DefaultRollbackConfig()
	rm := NewRollbackManager(config, testSimulateFn, broadcast)
	rm.SetState(map[string]interface{}{"x": 0.0})

	// 帧 0 添加输入
	rm.OnInput(PlayerInput{
		PlayerID: "p1", FrameNum: 0,
		Actions: []InputAction{{Type: 1, Data: map[string]interface{}{"dx": 5.0}}},
	})

	// Tick 帧 0
	rm.Tick(0)

	state := rm.CurrentState()
	x, _ := state["x"].(float64)
	if x != 5.0 {
		t.Errorf("state x = %f, want 5.0", x)
	}
	if len(broadcasts) != 1 {
		t.Errorf("broadcasts = %d, want 1", len(broadcasts))
	}
}

func TestRollbackManager_LateInput(t *testing.T) {
	var broadcasts []interface{}
	broadcast := func(playerIDs []string, msg interface{}) {
		broadcasts = append(broadcasts, msg)
	}

	config := DefaultRollbackConfig()
	rm := NewRollbackManager(config, testSimulateFn, broadcast)
	rm.SetState(map[string]interface{}{"x": 0.0})

	// 正常推进帧 0、1、2
	rm.Tick(0)
	rm.Tick(1)
	rm.Tick(2)

	// x 应该仍然是 0（没有输入）
	state := rm.CurrentState()
	x, _ := state["x"].(float64)
	if x != 0.0 {
		t.Errorf("before rollback: x = %f, want 0.0", x)
	}

	broadcasts = broadcasts[:0]

	// 收到迟到的帧 1 输入
	rolled := rm.OnInput(PlayerInput{
		PlayerID: "p1", FrameNum: 1,
		Actions: []InputAction{{Type: 1, Data: map[string]interface{}{"dx": 10.0}}},
	})

	if !rolled {
		t.Error("expected rollback to occur")
	}

	// 回滚重算后，x 应该是 10.0
	state = rm.CurrentState()
	x, _ = state["x"].(float64)
	if x != 10.0 {
		t.Errorf("after rollback: x = %f, want 10.0", x)
	}

	// 应有校正广播
	if len(broadcasts) == 0 {
		t.Error("expected correction broadcast after rollback")
	}
}

func TestRollbackManager_MaxRollbackFrames(t *testing.T) {
	broadcast := func(playerIDs []string, msg interface{}) {}

	config := DefaultRollbackConfig()
	config.MaxRollbackFrames = 2
	rm := NewRollbackManager(config, testSimulateFn, broadcast)
	rm.SetState(map[string]interface{}{"x": 0.0})

	// 推进 5 帧
	for i := 0; i < 5; i++ {
		rm.Tick(uint64(i))
	}

	// 帧 1 的迟到输入（距当前帧差 4，超过最大回滚 2）
	rolled := rm.OnInput(PlayerInput{
		PlayerID: "p1", FrameNum: 1,
		Actions: []InputAction{{Type: 1, Data: map[string]interface{}{"dx": 10.0}}},
	})

	if rolled {
		t.Error("should not rollback beyond MaxRollbackFrames")
	}
}

// --- Interpolation 测试 ---

func TestInterpolator_BasicInterpolation(t *testing.T) {
	config := DefaultInterpolationConfig()
	ip := NewInterpolator(config)
	ip.AddEntity("e1")

	// 推入两帧快照
	ip.PushSnapshot("e1", EntitySnapshot{
		FrameNum: 0,
		Position: InterpolableValue{X: 0, Y: 0},
	})
	ip.PushSnapshot("e1", EntitySnapshot{
		FrameNum: 10,
		Position: InterpolableValue{X: 10, Y: 20},
	})

	// 在帧 5 处插值
	state, ok := ip.GetInterpolatedState("e1", 5, 0)
	if !ok {
		t.Fatal("expected interpolated state")
	}

	if math.Abs(state.Position.X-5) > 0.01 {
		t.Errorf("interpolated X = %f, want 5", state.Position.X)
	}
	if math.Abs(state.Position.Y-10) > 0.01 {
		t.Errorf("interpolated Y = %f, want 10", state.Position.Y)
	}
}

func TestInterpolator_Extrapolation(t *testing.T) {
	config := DefaultInterpolationConfig()
	ip := NewInterpolator(config)
	ip.AddEntity("e1")

	ip.PushSnapshot("e1", EntitySnapshot{
		FrameNum: 0,
		Position: InterpolableValue{X: 0, Y: 0},
	})
	ip.PushSnapshot("e1", EntitySnapshot{
		FrameNum: 10,
		Position: InterpolableValue{X: 10, Y: 20},
	})

	// 外推到帧 15
	state, ok := ip.GetExtrapolatedState("e1", 15)
	if !ok {
		t.Fatal("expected extrapolated state")
	}

	// 速度：vx=1/帧, vy=2/帧，从帧 10(10,20) 外推 5 帧
	if math.Abs(state.Position.X-15) > 0.01 {
		t.Errorf("extrapolated X = %f, want 15", state.Position.X)
	}
	if math.Abs(state.Position.Y-30) > 0.01 {
		t.Errorf("extrapolated Y = %f, want 30", state.Position.Y)
	}
}

func TestInterpolator_EntityLifecycle(t *testing.T) {
	config := DefaultInterpolationConfig()
	ip := NewInterpolator(config)

	ip.AddEntity("e1")
	if ip.EntityCount() != 1 {
		t.Errorf("entity count = %d, want 1", ip.EntityCount())
	}

	ip.RemoveEntity("e1")
	if ip.EntityCount() != 0 {
		t.Errorf("entity count after remove = %d, want 0", ip.EntityCount())
	}
}

func TestInterpolator_SingleSnapshot(t *testing.T) {
	config := DefaultInterpolationConfig()
	ip := NewInterpolator(config)
	ip.AddEntity("e1")

	ip.PushSnapshot("e1", EntitySnapshot{
		FrameNum: 5,
		Position: InterpolableValue{X: 3, Y: 7},
	})

	// 只有一帧数据时，直接返回该帧
	state, ok := ip.GetInterpolatedState("e1", 5, 0)
	if !ok {
		t.Fatal("expected state with single snapshot")
	}
	if state.Position.X != 3 {
		t.Errorf("X = %f, want 3", state.Position.X)
	}
}

func TestInterpolator_BufferOverflow(t *testing.T) {
	config := InterpolationConfig{BufferSize: 3, InterpolationDelay: 1}
	ip := NewInterpolator(config)
	ip.AddEntity("e1")

	// 推入 5 帧（超过缓冲 3）
	for i := 0; i < 5; i++ {
		ip.PushSnapshot("e1", EntitySnapshot{
			FrameNum: uint64(i * 10),
			Position: InterpolableValue{X: float64(i * 10), Y: 0},
		})
	}

	// 最新帧应该是第 5 个
	if ip.LatestFrame("e1") != 40 {
		t.Errorf("latest frame = %d, want 40", ip.LatestFrame("e1"))
	}
}
