package syncx

import (
	"reflect"
	"testing"
)

func TestDeltaSchema_Register(t *testing.T) {
	s := NewDeltaSchema()
	if s.Register("hp") != 0 {
		t.Fatal("first field should be 0")
	}
	if s.Register("mp") != 1 {
		t.Fatal("second field should be 1")
	}
	if s.Register("hp") != 0 {
		t.Fatal("re-register should return same index")
	}
	if s.FieldCount() != 2 {
		t.Fatalf("field count = %d, want 2", s.FieldCount())
	}
	name, ok := s.FieldName(1)
	if !ok || name != "mp" {
		t.Fatalf("FieldName(1) = %q, want mp", name)
	}
}

func TestDeltaSchema_Overflow(t *testing.T) {
	s := NewDeltaSchema()
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on >64 fields")
		}
	}()
	for i := 0; i < 65; i++ {
		s.Register(string(rune('a' + i)))
	}
}

func TestDeltaEncoder_NewEntity(t *testing.T) {
	enc := NewDeltaEncoder(nil)
	state := map[string]map[string]interface{}{
		"e1": {"hp": int64(100), "x": 1.5},
	}
	fd := enc.Encode(1, state)
	if len(fd.Entities) != 1 {
		t.Fatalf("entities = %d, want 1", len(fd.Entities))
	}
	ed := fd.Entities[0]
	if !ed.IsNew {
		t.Fatal("entity should be marked new")
	}
	if ed.Bitmap == 0 {
		t.Fatal("bitmap should be non-zero")
	}
	if len(ed.Fields) != 2 {
		t.Fatalf("fields = %d, want 2", len(ed.Fields))
	}
}

func TestDeltaEncoder_OnlyChangedFields(t *testing.T) {
	enc := NewDeltaEncoder(nil)
	enc.Encode(1, map[string]map[string]interface{}{
		"e1": {"hp": int64(100), "x": 1.0, "y": 2.0},
	})
	// 只改 hp
	fd := enc.Encode(2, map[string]map[string]interface{}{
		"e1": {"hp": int64(80), "x": 1.0, "y": 2.0},
	})
	if len(fd.Entities) != 1 {
		t.Fatalf("entities = %d, want 1", len(fd.Entities))
	}
	ed := fd.Entities[0]
	if ed.IsNew {
		t.Fatal("should not be marked new")
	}
	if len(ed.Fields) != 1 {
		t.Fatalf("only hp should be in delta, got %d fields", len(ed.Fields))
	}
	if ed.Fields["hp"] != int64(80) {
		t.Fatalf("hp = %v, want 80", ed.Fields["hp"])
	}
}

func TestDeltaEncoder_NoChange(t *testing.T) {
	enc := NewDeltaEncoder(nil)
	enc.Encode(1, map[string]map[string]interface{}{
		"e1": {"hp": int64(100)},
	})
	fd := enc.Encode(2, map[string]map[string]interface{}{
		"e1": {"hp": int64(100)},
	})
	if len(fd.Entities) != 0 {
		t.Fatalf("expected no entity changes, got %d", len(fd.Entities))
	}
}

func TestDeltaEncoder_RemovedEntity(t *testing.T) {
	enc := NewDeltaEncoder(nil)
	enc.Encode(1, map[string]map[string]interface{}{
		"e1": {"hp": int64(100)},
		"e2": {"hp": int64(50)},
	})
	fd := enc.Encode(2, map[string]map[string]interface{}{
		"e1": {"hp": int64(100)},
	})
	if len(fd.Entities) != 1 {
		t.Fatalf("entities = %d, want 1", len(fd.Entities))
	}
	if !fd.Entities[0].IsRemoved || fd.Entities[0].EntityID != "e2" {
		t.Fatalf("expected e2 removed, got %+v", fd.Entities[0])
	}
}

func TestDeltaEncoder_RemovedField(t *testing.T) {
	enc := NewDeltaEncoder(nil)
	enc.Encode(1, map[string]map[string]interface{}{
		"e1": {"hp": int64(100), "buff": "rage"},
	})
	fd := enc.Encode(2, map[string]map[string]interface{}{
		"e1": {"hp": int64(100)},
	})
	if len(fd.Entities) != 1 {
		t.Fatalf("entities = %d", len(fd.Entities))
	}
	v, ok := fd.Entities[0].Fields["buff"]
	if !ok {
		t.Fatal("buff key should be present in delta to indicate removal")
	}
	if v != nil {
		t.Fatalf("removed field value = %v, want nil", v)
	}
}

func TestDeltaDecoder_ApplyRoundTrip(t *testing.T) {
	schema := NewDeltaSchema()
	enc := NewDeltaEncoder(schema)
	dec := NewDeltaDecoder(schema)

	frame := func(n uint64, st map[string]map[string]interface{}) {
		fd := enc.Encode(n, st)
		dec.Apply(fd)
	}

	frame(1, map[string]map[string]interface{}{
		"e1": {"hp": int64(100), "x": 1.5, "alive": true, "name": "hero"},
		"e2": {"hp": int64(50)},
	})
	frame(2, map[string]map[string]interface{}{
		"e1": {"hp": int64(80), "x": 1.5, "alive": true, "name": "hero"},
		"e2": {"hp": int64(50)},
	})
	frame(3, map[string]map[string]interface{}{
		"e1": {"hp": int64(80), "x": 2.0, "alive": false, "name": "hero"},
	})

	want := map[string]map[string]interface{}{
		"e1": {"hp": int64(80), "x": 2.0, "alive": false, "name": "hero"},
	}
	got := dec.State()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("state mismatch:\n got=%+v\nwant=%+v", got, want)
	}
}

func TestDeltaDecoder_SnapshotResets(t *testing.T) {
	schema := NewDeltaSchema()
	enc := NewDeltaEncoder(schema)
	dec := NewDeltaDecoder(schema)

	dec.Apply(enc.Encode(1, map[string]map[string]interface{}{
		"e1": {"hp": int64(100)},
		"e2": {"hp": int64(50)},
	}))

	// 服务端发送强制快照（只含 e1）
	enc.Reset()
	enc.Encode(2, map[string]map[string]interface{}{"e1": {"hp": int64(100)}})
	snap := enc.Snapshot(2)
	dec.Apply(snap)
	if dec.EntityCount() != 1 {
		t.Fatalf("after snapshot entity count = %d, want 1", dec.EntityCount())
	}
}

func TestMarshalUnmarshal_RoundTrip(t *testing.T) {
	schema := NewDeltaSchema()
	enc := NewDeltaEncoder(schema)
	dec := NewDeltaDecoder(schema)

	fd := enc.Encode(7, map[string]map[string]interface{}{
		"e1": {"hp": int64(100), "x": 1.5, "alive": true, "name": "hero"},
		"e2": {"hp": int64(50)},
	})
	buf, err := MarshalDelta(fd, schema)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got, err := UnmarshalDelta(buf, schema)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.FrameNum != fd.FrameNum || len(got.Entities) != len(fd.Entities) {
		t.Fatalf("decoded delta mismatch: got=%+v want=%+v", got, fd)
	}
	dec.Apply(got)
	state := dec.State()
	if state["e1"]["hp"] != int64(100) {
		t.Fatalf("hp = %v", state["e1"]["hp"])
	}
	if state["e1"]["x"].(float64) != 1.5 {
		t.Fatalf("x = %v", state["e1"]["x"])
	}
	if state["e1"]["alive"] != true {
		t.Fatalf("alive = %v", state["e1"]["alive"])
	}
	if state["e1"]["name"] != "hero" {
		t.Fatalf("name = %v", state["e1"]["name"])
	}
	if state["e2"]["hp"] != int64(50) {
		t.Fatalf("e2 hp = %v", state["e2"]["hp"])
	}
}

func TestMarshalDelta_RemovedField(t *testing.T) {
	schema := NewDeltaSchema()
	enc := NewDeltaEncoder(schema)
	dec := NewDeltaDecoder(schema)

	dec.Apply(enc.Encode(1, map[string]map[string]interface{}{
		"e1": {"hp": int64(100), "buff": "rage"},
	}))
	fd := enc.Encode(2, map[string]map[string]interface{}{
		"e1": {"hp": int64(100)},
	})
	buf, err := MarshalDelta(fd, schema)
	if err != nil {
		t.Fatal(err)
	}
	got, err := UnmarshalDelta(buf, schema)
	if err != nil {
		t.Fatal(err)
	}
	dec.Apply(got)
	if _, exists := dec.State()["e1"]["buff"]; exists {
		t.Fatal("buff should have been removed")
	}
}

func TestMarshalDelta_BandwidthSaving(t *testing.T) {
	schema := NewDeltaSchema()
	enc := NewDeltaEncoder(schema)

	// 10 个实体，每个 8 字段
	full := make(map[string]map[string]interface{})
	for i := 0; i < 10; i++ {
		eid := string(rune('a' + i))
		full[eid] = map[string]interface{}{
			"x": float64(i), "y": float64(i * 2), "z": float64(i * 3),
			"hp": int64(100), "mp": int64(50), "lvl": int64(1),
			"name": "n", "team": int64(0),
		}
	}
	fdFull := enc.Encode(1, full)
	bufFull, _ := MarshalDelta(fdFull, schema)

	// 仅改 1 个字段
	full["a"]["hp"] = int64(95)
	fdDelta := enc.Encode(2, full)
	bufDelta, _ := MarshalDelta(fdDelta, schema)

	if len(bufDelta) >= len(bufFull) {
		t.Fatalf("delta size %d should be much smaller than full %d", len(bufDelta), len(bufFull))
	}
	t.Logf("full=%d bytes delta=%d bytes saving=%.1f%%",
		len(bufFull), len(bufDelta),
		100*(1-float64(len(bufDelta))/float64(len(bufFull))))
}

func TestMarshalDelta_RemovedEntity(t *testing.T) {
	schema := NewDeltaSchema()
	enc := NewDeltaEncoder(schema)
	dec := NewDeltaDecoder(schema)

	dec.Apply(enc.Encode(1, map[string]map[string]interface{}{
		"e1": {"hp": int64(100)},
		"e2": {"hp": int64(50)},
	}))
	fd := enc.Encode(2, map[string]map[string]interface{}{
		"e1": {"hp": int64(100)},
	})
	buf, _ := MarshalDelta(fd, schema)
	got, err := UnmarshalDelta(buf, schema)
	if err != nil {
		t.Fatal(err)
	}
	dec.Apply(got)
	if dec.EntityCount() != 1 {
		t.Fatalf("entity count = %d, want 1", dec.EntityCount())
	}
}

func TestUnmarshalDelta_TruncatedBuffer(t *testing.T) {
	schema := NewDeltaSchema()
	enc := NewDeltaEncoder(schema)
	fd := enc.Encode(1, map[string]map[string]interface{}{"e1": {"hp": int64(1)}})
	buf, _ := MarshalDelta(fd, schema)
	if _, err := UnmarshalDelta(buf[:len(buf)-2], schema); err == nil {
		t.Fatal("expected truncated buffer error")
	}
}
