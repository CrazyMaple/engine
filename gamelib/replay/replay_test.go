package replay

import (
	"testing"
	"time"
)

func TestRecorder_BasicFlow(t *testing.T) {
	rec := NewRecorder("room-1")

	rec.Record(1, []byte("move_left"))
	rec.Record(2, []byte("attack"))
	rec.Record(1, []byte("move_right"))

	if rec.EventCount() != 3 {
		t.Errorf("expected 3 events, got %d", rec.EventCount())
	}

	data := rec.Finish()
	if data.RoomID != "room-1" {
		t.Errorf("expected room-1, got %s", data.RoomID)
	}
	if len(data.Events) != 3 {
		t.Errorf("expected 3 events, got %d", len(data.Events))
	}
	if data.Version != replayVersion {
		t.Errorf("expected version %d, got %d", replayVersion, data.Version)
	}

	// Finish 后不再录制
	rec.Record(1, []byte("ignored"))
	if rec.EventCount() != 3 {
		t.Error("should not record after Finish")
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	now := time.Now().UnixNano()
	original := &ReplayData{
		Version:   1,
		RoomID:    "test-room-123",
		StartTime: now,
		Duration:  int64(5 * time.Second),
		Events: []ReplayEvent{
			{Timestamp: now + int64(100*time.Millisecond), Type: 1, Data: []byte("hello")},
			{Timestamp: now + int64(200*time.Millisecond), Type: 2, Data: []byte("world")},
			{Timestamp: now + int64(1*time.Second), Type: 3, Data: []byte{0x01, 0x02, 0x03}},
			{Timestamp: now + int64(5*time.Second), Type: 1, Data: []byte("")},
		},
	}

	encoded, err := Encode(original)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	decoded, err := Decode(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	if decoded.Version != original.Version {
		t.Errorf("version: %d != %d", decoded.Version, original.Version)
	}
	if decoded.RoomID != original.RoomID {
		t.Errorf("roomID: %s != %s", decoded.RoomID, original.RoomID)
	}
	if decoded.StartTime != original.StartTime {
		t.Errorf("startTime: %d != %d", decoded.StartTime, original.StartTime)
	}
	if decoded.Duration != original.Duration {
		t.Errorf("duration: %d != %d", decoded.Duration, original.Duration)
	}
	if len(decoded.Events) != len(original.Events) {
		t.Fatalf("event count: %d != %d", len(decoded.Events), len(original.Events))
	}

	for i, e := range decoded.Events {
		orig := original.Events[i]
		if e.Timestamp != orig.Timestamp {
			t.Errorf("event[%d] timestamp: %d != %d", i, e.Timestamp, orig.Timestamp)
		}
		if e.Type != orig.Type {
			t.Errorf("event[%d] type: %d != %d", i, e.Type, orig.Type)
		}
		if string(e.Data) != string(orig.Data) {
			t.Errorf("event[%d] data: %v != %v", i, e.Data, orig.Data)
		}
	}
}

func TestEncodeDecodeEmptyEvents(t *testing.T) {
	original := &ReplayData{
		Version:   1,
		RoomID:    "empty",
		StartTime: time.Now().UnixNano(),
	}

	encoded, err := Encode(original)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	decoded, err := Decode(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(decoded.Events) != 0 {
		t.Errorf("expected 0 events, got %d", len(decoded.Events))
	}
}

func TestPlayer_BasicPlayback(t *testing.T) {
	start := int64(1000000000)
	data := &ReplayData{
		Version:   1,
		RoomID:    "room-1",
		StartTime: start,
		Duration:  int64(3 * time.Second),
		Events: []ReplayEvent{
			{Timestamp: start + int64(500*time.Millisecond), Type: 1, Data: []byte("a")},
			{Timestamp: start + int64(1*time.Second), Type: 2, Data: []byte("b")},
			{Timestamp: start + int64(2*time.Second), Type: 3, Data: []byte("c")},
		},
	}

	p := NewPlayer(data)

	// 推进 600ms，应触发第一个事件
	events := p.Tick(int64(600 * time.Millisecond))
	if len(events) != 1 {
		t.Errorf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != 1 {
		t.Errorf("expected type 1, got %d", events[0].Type)
	}

	// 再推进 500ms（总计 1100ms），应触发第二个事件
	events = p.Tick(int64(500 * time.Millisecond))
	if len(events) != 1 {
		t.Errorf("expected 1 event, got %d", len(events))
	}

	if p.IsFinished() {
		t.Error("should not be finished yet")
	}

	// 推进到结束
	events = p.Tick(int64(2 * time.Second))
	if len(events) != 1 {
		t.Errorf("expected 1 event, got %d", len(events))
	}
	if !p.IsFinished() {
		t.Error("should be finished")
	}
}

func TestPlayer_Speed(t *testing.T) {
	start := int64(0)
	data := &ReplayData{
		Version:   1,
		RoomID:    "room-1",
		StartTime: start,
		Duration:  int64(2 * time.Second),
		Events: []ReplayEvent{
			{Timestamp: int64(1 * time.Second), Type: 1, Data: nil},
			{Timestamp: int64(2 * time.Second), Type: 2, Data: nil},
		},
	}

	p := NewPlayer(data)
	p.SetSpeed(2.0) // 二倍速

	// 推进 600ms，实际推进 1200ms，应触发第一个事件
	events := p.Tick(int64(600 * time.Millisecond))
	if len(events) != 1 {
		t.Errorf("expected 1 event at 2x speed, got %d", len(events))
	}
}

func TestPlayer_PauseResume(t *testing.T) {
	start := int64(0)
	data := &ReplayData{
		Version:   1,
		StartTime: start,
		Duration:  int64(1 * time.Second),
		Events: []ReplayEvent{
			{Timestamp: int64(500 * time.Millisecond), Type: 1, Data: nil},
		},
	}

	p := NewPlayer(data)
	p.Pause()

	events := p.Tick(int64(1 * time.Second))
	if len(events) != 0 {
		t.Error("should not trigger events while paused")
	}

	p.Resume()
	events = p.Tick(int64(1 * time.Second))
	if len(events) != 1 {
		t.Errorf("expected 1 event after resume, got %d", len(events))
	}
}

func TestPlayer_SeekTo(t *testing.T) {
	start := int64(0)
	data := &ReplayData{
		Version:   1,
		StartTime: start,
		Duration:  int64(3 * time.Second),
		Events: []ReplayEvent{
			{Timestamp: int64(1 * time.Second), Type: 1, Data: nil},
			{Timestamp: int64(2 * time.Second), Type: 2, Data: nil},
			{Timestamp: int64(3 * time.Second), Type: 3, Data: nil},
		},
	}

	p := NewPlayer(data)
	p.SeekTo(int64(1500 * time.Millisecond))

	if p.RemainingEvents() != 2 {
		t.Errorf("expected 2 remaining events, got %d", p.RemainingEvents())
	}

	// Tick 应从 event[1] 开始触发
	events := p.Tick(int64(1 * time.Second))
	if len(events) != 1 {
		t.Errorf("expected 1 event after seek, got %d", len(events))
	}
	if events[0].Type != 2 {
		t.Errorf("expected type 2, got %d", events[0].Type)
	}
}

func TestPlayer_Progress(t *testing.T) {
	start := int64(0)
	data := &ReplayData{
		Version:   1,
		StartTime: start,
		Duration:  int64(10 * time.Second),
		Events:    nil,
	}

	p := NewPlayer(data)
	if p.Progress() != 0 {
		t.Errorf("expected 0 progress, got %f", p.Progress())
	}

	p.Tick(int64(5 * time.Second))
	progress := p.Progress()
	if progress < 0.49 || progress > 0.51 {
		t.Errorf("expected ~0.5 progress, got %f", progress)
	}
}

func TestPlayer_Reset(t *testing.T) {
	start := int64(0)
	data := &ReplayData{
		Version:   1,
		StartTime: start,
		Duration:  int64(1 * time.Second),
		Events: []ReplayEvent{
			{Timestamp: int64(500 * time.Millisecond), Type: 1, Data: nil},
		},
	}

	p := NewPlayer(data)
	p.Tick(int64(1 * time.Second))
	if !p.IsFinished() {
		t.Error("should be finished")
	}

	p.Reset()
	if p.IsFinished() {
		t.Error("should not be finished after reset")
	}
	if p.RemainingEvents() != 1 {
		t.Errorf("expected 1 remaining after reset, got %d", p.RemainingEvents())
	}
}

func TestRecorderToPlayer_Integration(t *testing.T) {
	// 录制 → 编码 → 解码 → 回放
	rec := NewRecorder("room-integration")

	baseTime := time.Now().UnixNano()
	rec.RecordAt(baseTime+int64(100*time.Millisecond), 1, []byte("input1"))
	rec.RecordAt(baseTime+int64(200*time.Millisecond), 2, []byte("input2"))
	rec.RecordAt(baseTime+int64(500*time.Millisecond), 3, []byte("input3"))

	data := rec.Finish()

	// 编码 + 解码
	encoded, err := Encode(data)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := Decode(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	// 回放
	p := NewPlayer(decoded)
	var allEvents []ReplayEvent
	for !p.IsFinished() {
		events := p.Tick(int64(100 * time.Millisecond))
		allEvents = append(allEvents, events...)
	}

	if len(allEvents) != 3 {
		t.Errorf("expected 3 replayed events, got %d", len(allEvents))
	}
}
