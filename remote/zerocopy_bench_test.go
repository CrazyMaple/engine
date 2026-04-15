package remote

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"engine/actor"
	"engine/codec"
)

// ---- 单元测试 ----

func TestZeroCopyCodec_EncodeDecode(t *testing.T) {
	rc := DefaultRemoteCodec()
	zc := NewZeroCopyCodec(rc)

	// 使用 []byte payload 走 WriterTo 快路径，避免类型注册依赖
	msg := &RemoteMessage{
		Target:   actor.NewPID("remote:1234", "actor-1"),
		Sender:   actor.NewPID("local:5678", "actor-2"),
		Message:  []byte(`{"type":"TestMessage","data":"hello"}`),
		Type:     MessageTypeUser,
		TypeName: "TestMessage",
	}

	var buf bytes.Buffer
	n, err := zc.EncodeEnvelopeTo(&buf, msg)
	if err != nil {
		t.Fatalf("encode error: %v", err)
	}
	if n == 0 {
		t.Fatal("encoded 0 bytes")
	}

	decoded, err := ReadRemoteMessageFrom(&buf)
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if decoded == nil {
		t.Fatal("decoded nil")
	}
	if decoded.Target.Id != "actor-1" {
		t.Fatalf("target id mismatch: %s", decoded.Target.Id)
	}
}

func TestRemoteMessage_WriterTo(t *testing.T) {
	// 使用已编码的 []byte payload 直接测试 WriterTo 路径
	payload := []byte("hello world")
	msg := &RemoteMessage{
		Target:   actor.NewPID("host:port", "id-1"),
		Sender:   actor.NewPID("host:port", "id-2"),
		Message:  payload,
		Type:     MessageTypeUser,
		TypeName: "RawBytes",
	}

	var buf bytes.Buffer
	n, err := msg.WriteTo(&buf)
	if err != nil {
		t.Fatalf("WriteTo error: %v", err)
	}
	if n == 0 {
		t.Fatal("wrote 0 bytes")
	}

	// 读回
	decoded, err := ReadRemoteMessageFrom(&buf)
	if err != nil {
		t.Fatalf("ReadRemoteMessageFrom error: %v", err)
	}
	if decoded.Target == nil || decoded.Target.Id != "id-1" {
		t.Fatalf("target mismatch: %+v", decoded.Target)
	}
	if decoded.Sender == nil || decoded.Sender.Id != "id-2" {
		t.Fatalf("sender mismatch: %+v", decoded.Sender)
	}
	if decoded.TypeName != "RawBytes" {
		t.Fatalf("typename mismatch: %s", decoded.TypeName)
	}
	decodedPayload, ok := decoded.Message.([]byte)
	if !ok {
		t.Fatalf("payload should be []byte, got %T", decoded.Message)
	}
	if !bytes.Equal(decodedPayload, payload) {
		t.Fatalf("payload mismatch: got %q want %q", decodedPayload, payload)
	}
}

func TestStreamJSONCodec_EncodeDecode(t *testing.T) {
	// 需要注册类型才能正常 Decode
	type testMsg struct {
		Type string `json:"type"`
		Name string `json:"name"`
		Age  int    `json:"age"`
	}
	sc := codec.NewStreamJSONCodec()
	sc.Register(&testMsg{})

	msg := &testMsg{Type: "testMsg", Name: "alice", Age: 30}
	var buf bytes.Buffer
	n, err := sc.EncodeTo(&buf, msg)
	if err != nil {
		t.Fatalf("encode error: %v", err)
	}
	if n == 0 {
		t.Fatal("encoded 0 bytes")
	}

	decoded, err := sc.DecodeFrom(&buf)
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}
	dm, ok := decoded.(*testMsg)
	if !ok {
		t.Fatalf("decoded type mismatch: %T", decoded)
	}
	if dm.Name != "alice" || dm.Age != 30 {
		t.Fatalf("decoded data mismatch: %+v", dm)
	}
}

func TestFragmenter_NoFragmentation(t *testing.T) {
	f := NewFragmenter(1024, 256)
	data := []byte("short message")
	frags, err := f.Fragment(data)
	if err != nil {
		t.Fatalf("fragment error: %v", err)
	}
	if len(frags) != 1 {
		t.Fatalf("expected 1 fragment, got %d", len(frags))
	}
	if !bytes.Equal(frags[0], data) {
		t.Fatal("data mismatch")
	}
}

func TestFragmenter_Fragmentation(t *testing.T) {
	f := NewFragmenter(100, 64)
	data := make([]byte, 500)
	for i := range data {
		data[i] = byte(i % 256)
	}

	frags, err := f.Fragment(data)
	if err != nil {
		t.Fatalf("fragment error: %v", err)
	}

	expectedFragments := (500 + 64 - 1) / 64 // = 8
	if len(frags) != expectedFragments {
		t.Fatalf("expected %d fragments, got %d", expectedFragments, len(frags))
	}

	// 验证每个分片都有正确的头
	for i, frag := range frags {
		if !IsFragment(frag) {
			t.Fatalf("fragment %d missing magic", i)
		}
		h, err := DecodeHeader(frag)
		if err != nil {
			t.Fatalf("decode header %d: %v", i, err)
		}
		if h.Sequence != uint16(i) {
			t.Fatalf("sequence mismatch at %d: got %d", i, h.Sequence)
		}
		if h.Total != uint16(expectedFragments) {
			t.Fatalf("total mismatch: got %d", h.Total)
		}
	}
}

func TestReassembler_CompleteReassembly(t *testing.T) {
	f := NewFragmenter(100, 64)
	data := make([]byte, 300)
	for i := range data {
		data[i] = byte(i)
	}

	frags, err := f.Fragment(data)
	if err != nil {
		t.Fatalf("fragment error: %v", err)
	}

	r := NewReassembler(5 * time.Second)
	defer r.Stop()

	var reassembled []byte
	for i, frag := range frags {
		out, err := r.Feed(frag)
		if err != nil {
			t.Fatalf("feed %d: %v", i, err)
		}
		if i < len(frags)-1 && out != nil {
			t.Fatalf("unexpected completion at %d", i)
		}
		if i == len(frags)-1 {
			reassembled = out
		}
	}

	if !bytes.Equal(reassembled, data) {
		t.Fatalf("reassembled data mismatch")
	}

	if r.Pending() != 0 {
		t.Fatalf("expected 0 pending, got %d", r.Pending())
	}
}

func TestReassembler_OutOfOrder(t *testing.T) {
	f := NewFragmenter(100, 64)
	data := make([]byte, 300)
	for i := range data {
		data[i] = byte(i)
	}

	frags, _ := f.Fragment(data)

	r := NewReassembler(5 * time.Second)
	defer r.Stop()

	// 乱序送入：先最后一个，再倒序
	var reassembled []byte
	order := []int{4, 0, 2, 3, 1}
	for idx, i := range order {
		out, err := r.Feed(frags[i])
		if err != nil {
			t.Fatalf("feed %d: %v", i, err)
		}
		if idx < len(order)-1 && out != nil {
			t.Fatalf("unexpected completion at step %d", idx)
		}
		if idx == len(order)-1 {
			reassembled = out
		}
	}

	if !bytes.Equal(reassembled, data) {
		t.Fatal("reassembled data mismatch")
	}
}

func TestReassembler_DuplicateFragments(t *testing.T) {
	f := NewFragmenter(100, 64)
	data := make([]byte, 300)
	frags, _ := f.Fragment(data)

	r := NewReassembler(5 * time.Second)
	defer r.Stop()

	// 发送一个分片两次
	_, _ = r.Feed(frags[0])
	out, _ := r.Feed(frags[0]) // 重复
	if out != nil {
		t.Fatal("duplicate should not trigger completion")
	}
}

// ---- Benchmarks ----

// BenchmarkZeroCopyEncode 对比 Zero-Copy 编码 vs 普通 Marshal
func BenchmarkZeroCopyEncode(b *testing.B) {
	rc := DefaultRemoteCodec()
	zc := NewZeroCopyCodec(rc)

	msg := &RemoteMessage{
		Target:   actor.NewPID("host:1234", "target"),
		Sender:   actor.NewPID("host:5678", "sender"),
		Message:  []byte("hello world, this is a test payload"),
		Type:     MessageTypeUser,
		TypeName: "TestMessage",
	}

	b.Run("WriterTo", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			var buf bytes.Buffer
			_, _ = msg.WriteTo(&buf)
		}
	})

	b.Run("ZeroCopyCodec", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			var buf bytes.Buffer
			_, _ = zc.EncodeEnvelopeTo(&buf, msg)
		}
	})

	b.Run("PooledBuffer", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			buf, err := zc.MarshalEnvelopeToBuffer(msg)
			if err != nil {
				b.Fatal(err)
			}
			zc.ReleaseBuffer(buf)
		}
	})
}

// BenchmarkCodecComparison 对比 JSON/Binary/WriterTo 三种编码方式
func BenchmarkCodecComparison(b *testing.B) {
	rc := DefaultRemoteCodec()
	msg := &RemoteMessage{
		Target:   actor.NewPID("host:1234", "target"),
		Sender:   actor.NewPID("host:5678", "sender"),
		Message:  []byte("benchmark payload"),
		Type:     MessageTypeUser,
		TypeName: "BenchMsg",
	}

	b.Run("JSON-Marshal", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := rc.MarshalEnvelope(msg)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("Binary-WriterTo", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			var buf bytes.Buffer
			_, err := msg.WriteTo(&buf)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkFragmentation 测试分片性能
func BenchmarkFragmentation(b *testing.B) {
	f := NewFragmenter(64*1024, 32*1024)

	sizes := []int{1024, 16 * 1024, 128 * 1024, 1024 * 1024}
	for _, size := range sizes {
		data := make([]byte, size)
		b.Run(fmt.Sprintf("size-%dKB", size/1024), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _ = f.Fragment(data)
			}
		})
	}
}
