package testkit

import (
	"testing"
	"time"

	"engine/actor"
	"gamelib/middleware"
	"engine/remote"
	"engine/telemetry"
)

// TestTraceE2E_RemoteRoundtrip 端到端验证 TraceID 在 remote 消息链路上的全链路传播:
//
//   发送端 Span (tracer.Start)
//       └─ TracePropagation.TraceParent
//              └─ RemoteMessage.TraceParent           (remote 出站注入)
//                     └─ codec.MarshalEnvelope → Unmarshal   (跨节点序列化往返)
//                            └─ actor.WrapEnvelopeWithTrace  (远端接收端还原为 envelope.TraceID)
//                                   └─ StartFromPropagation  (远端子 Span 继承同一 TraceID)
//                                          └─ InMemorySpanExporter 看到两条 Span
//                                                 └─ testkit DSL 断言 (AssertSpan / AssertSpanCount / AssertTraceDuration)
//
// 该测试闭环 v1.11 OpenTelemetry 需求:
//   1. remote 出站消息主动注入 W3C traceparent — remote.RemoteMessage.TraceParent + codec 保留
//   2. 远端反序列化还原为 actor.MessageEnvelope.TraceID — actor.WrapEnvelopeWithTrace
//   3. testkit AssertSpan/AssertTraceDuration DSL 可直接对 remote 产生的 Span 断言
func TestTraceE2E_RemoteRoundtrip(t *testing.T) {
	exp := middleware.NewInMemorySpanExporter()
	tracer := middleware.NewTracer(middleware.TracerConfig{
		ServiceName: "node-a",
		Exporter:    exp,
	})
	defer tracer.Shutdown()

	// --- 发送端: 产生 Span 并取出 TracePropagation ---
	sendSpan, prop := tracer.Start("rpc.send-to-remote")
	time.Sleep(time.Millisecond)
	sendSpan.End()

	if prop.TraceParent == "" {
		t.Fatal("expected non-empty traceparent from tracer.Start")
	}

	// --- remote 出站: 注入 TraceParent 到 RemoteMessage ---
	msg := &remote.RemoteMessage{
		Target:      &actor.PID{Address: "node-b:9999", Id: "server"},
		Sender:      &actor.PID{Address: "node-a:8888", Id: "client"},
		Type:        remote.MessageTypeUser,
		TypeName:    "DemoPing",
		Message:     map[string]interface{}{"greeting": "hello"},
		TraceParent: prop.TraceParent,
	}

	// --- 跨节点: codec 序列化 → 反序列化 ---
	codec := remote.DefaultRemoteCodec()
	data, err := codec.MarshalEnvelope(msg)
	if err != nil {
		t.Fatalf("MarshalEnvelope: %v", err)
	}
	isBatch, _, recvMsg, err := codec.UnmarshalEnvelope(data)
	if err != nil {
		t.Fatalf("UnmarshalEnvelope: %v", err)
	}
	if isBatch {
		t.Fatal("expected single, got batch")
	}
	if recvMsg.TraceParent != prop.TraceParent {
		t.Fatalf("TraceParent lost across codec: got %q want %q", recvMsg.TraceParent, prop.TraceParent)
	}

	// --- 远端接收: WrapEnvelopeWithTrace 还原 envelope.TraceID ---
	env := actor.WrapEnvelopeWithTrace(recvMsg.Message, recvMsg.Sender, recvMsg.TraceParent)
	me, ok := env.(*actor.MessageEnvelope)
	if !ok {
		t.Fatalf("expected *MessageEnvelope, got %T", env)
	}
	if me.TraceID != prop.TraceParent {
		t.Fatalf("envelope TraceID mismatch: got %q want %q", me.TraceID, prop.TraceParent)
	}
	defer actor.ReleaseEnvelope(me)

	// --- 远端处理: 基于收到的 traceparent 产生子 Span ---
	recvSpan, _ := tracer.StartFromPropagation("actor.receive",
		middleware.TracePropagation{TraceParent: me.TraceID})
	time.Sleep(time.Millisecond)
	recvSpan.End()

	// --- 断言: 两条 Span 共享同一 TraceID ---
	tc := telemetry.ParseTraceParent(prop.TraceParent)
	if !tc.IsValid() {
		t.Fatalf("traceparent parse failed: %q", prop.TraceParent)
	}

	NewScenario(t, "remote trace e2e").
		Setup(UseSpanExporter(exp)).
		Verify("send span exists", AssertSpan("rpc.send-to-remote", tc.TraceID)).
		Verify("receive span exists", AssertSpan("actor.receive", tc.TraceID)).
		Verify("spans share trace", AssertSpanCount(tc.TraceID, 2)).
		Verify("trace duration bounded", AssertTraceDuration(tc.TraceID, 0, 5*time.Second)).
		Run()
}

// TestTraceE2E_RemoteRoundtrip_EmptyTraceParent 无 TraceID 场景:
// 确认没有追踪上下文时,链路降级为"裸消息",不产生 envelope、不造成 Span 污染。
func TestTraceE2E_RemoteRoundtrip_EmptyTraceParent(t *testing.T) {
	msg := &remote.RemoteMessage{
		Target:   &actor.PID{Address: "node-b:9999", Id: "server"},
		Type:     remote.MessageTypeUser,
		TypeName: "DemoPing",
		Message:  map[string]interface{}{"greeting": "hello"},
	}

	codec := remote.DefaultRemoteCodec()
	data, err := codec.MarshalEnvelope(msg)
	if err != nil {
		t.Fatalf("MarshalEnvelope: %v", err)
	}
	_, _, recvMsg, err := codec.UnmarshalEnvelope(data)
	if err != nil {
		t.Fatalf("UnmarshalEnvelope: %v", err)
	}
	if recvMsg.TraceParent != "" {
		t.Fatalf("expected empty TraceParent, got %q", recvMsg.TraceParent)
	}

	// 无 sender 无 traceID 时,WrapEnvelopeWithTrace 应原样返回消息,不构造 envelope
	env := actor.WrapEnvelopeWithTrace(recvMsg.Message, nil, recvMsg.TraceParent)
	if _, ok := env.(*actor.MessageEnvelope); ok {
		t.Fatalf("expected bare message, got *MessageEnvelope")
	}
}
