package proto

import (
	"testing"

	"engine/codec"
)

func TestProtoPIDMarshalUnmarshal(t *testing.T) {
	pid := &ProtoPID{Address: "localhost:8080", Id: "actor-1"}
	data, err := pid.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary error: %v", err)
	}

	decoded := &ProtoPID{}
	if err := decoded.UnmarshalBinary(data); err != nil {
		t.Fatalf("UnmarshalBinary error: %v", err)
	}

	if decoded.Address != "localhost:8080" || decoded.Id != "actor-1" {
		t.Fatalf("unexpected: %+v", decoded)
	}
}

func TestProtoRemoteMessageRoundtrip(t *testing.T) {
	msg := &ProtoRemoteMessage{
		Target:          &ProtoPID{Address: "host1:8080", Id: "target-1"},
		Sender:          &ProtoPID{Address: "host2:8080", Id: "sender-1"},
		Payload:         []byte(`{"key":"value"}`),
		MsgType:         0,
		TypeName:        "TestMsg",
		ProtocolVersion: 1,
	}

	data, err := msg.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary error: %v", err)
	}

	decoded := &ProtoRemoteMessage{}
	if err := decoded.UnmarshalBinary(data); err != nil {
		t.Fatalf("UnmarshalBinary error: %v", err)
	}

	if decoded.Target.Address != "host1:8080" || decoded.Target.Id != "target-1" {
		t.Fatalf("target mismatch: %+v", decoded.Target)
	}
	if decoded.Sender.Address != "host2:8080" || decoded.Sender.Id != "sender-1" {
		t.Fatalf("sender mismatch: %+v", decoded.Sender)
	}
	if string(decoded.Payload) != `{"key":"value"}` {
		t.Fatalf("payload mismatch: %s", decoded.Payload)
	}
	if decoded.TypeName != "TestMsg" || decoded.ProtocolVersion != 1 {
		t.Fatalf("fields mismatch: type=%s, version=%d", decoded.TypeName, decoded.ProtocolVersion)
	}
}

func TestProtoRemoteMessageNilPIDs(t *testing.T) {
	msg := &ProtoRemoteMessage{
		Payload:  []byte("data"),
		MsgType:  1,
		TypeName: "Sys",
	}

	data, err := msg.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary error: %v", err)
	}

	decoded := &ProtoRemoteMessage{}
	if err := decoded.UnmarshalBinary(data); err != nil {
		t.Fatalf("UnmarshalBinary error: %v", err)
	}

	if decoded.Target != nil || decoded.Sender != nil {
		t.Fatalf("expected nil PIDs, got target=%v sender=%v", decoded.Target, decoded.Sender)
	}
}

func TestProtoRemoteMessageBatchRoundtrip(t *testing.T) {
	batch := &ProtoRemoteMessageBatch{
		Messages: []*ProtoRemoteMessage{
			{TypeName: "A", Payload: []byte("a")},
			{TypeName: "B", Payload: []byte("b"), Target: &ProtoPID{Id: "t"}},
		},
	}

	data, err := batch.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary error: %v", err)
	}

	decoded := &ProtoRemoteMessageBatch{}
	if err := decoded.UnmarshalBinary(data); err != nil {
		t.Fatalf("UnmarshalBinary error: %v", err)
	}

	if len(decoded.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(decoded.Messages))
	}
	if decoded.Messages[0].TypeName != "A" || decoded.Messages[1].TypeName != "B" {
		t.Fatalf("type names mismatch")
	}
}

func TestProtoSystemMessagesRoundtrip(t *testing.T) {
	// Empty messages
	started := &ProtoStarted{}
	data, _ := started.MarshalBinary()
	decoded := &ProtoStarted{}
	if err := decoded.UnmarshalBinary(data); err != nil {
		t.Fatalf("Started unmarshal error: %v", err)
	}

	// Watch with PID
	watch := &ProtoWatch{Watcher: &ProtoPID{Id: "watcher-1"}}
	data, _ = watch.MarshalBinary()
	decodedWatch := &ProtoWatch{}
	if err := decodedWatch.UnmarshalBinary(data); err != nil {
		t.Fatalf("Watch unmarshal error: %v", err)
	}
	if decodedWatch.Watcher.Id != "watcher-1" {
		t.Fatalf("watcher mismatch: %+v", decodedWatch.Watcher)
	}

	// Terminated
	term := &ProtoTerminated{Who: &ProtoPID{Address: "host:80", Id: "dead"}}
	data, _ = term.MarshalBinary()
	decodedTerm := &ProtoTerminated{}
	if err := decodedTerm.UnmarshalBinary(data); err != nil {
		t.Fatalf("Terminated unmarshal error: %v", err)
	}
	if decodedTerm.Who.Id != "dead" {
		t.Fatalf("who mismatch: %+v", decodedTerm.Who)
	}
}

func TestProtoMemberRoundtrip(t *testing.T) {
	m := &ProtoMember{
		Address: "10.0.0.1:8080",
		Id:      "node-abc",
		Kinds:   []string{"player", "npc"},
		Status:  ProtoMemberAlive,
		Seq:     42,
	}

	data, err := m.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary error: %v", err)
	}

	decoded := &ProtoMember{}
	if err := decoded.UnmarshalBinary(data); err != nil {
		t.Fatalf("UnmarshalBinary error: %v", err)
	}

	if decoded.Address != "10.0.0.1:8080" || decoded.Id != "node-abc" {
		t.Fatalf("basic fields mismatch: %+v", decoded)
	}
	if len(decoded.Kinds) != 2 || decoded.Kinds[0] != "player" || decoded.Kinds[1] != "npc" {
		t.Fatalf("kinds mismatch: %v", decoded.Kinds)
	}
	if decoded.Status != ProtoMemberAlive || decoded.Seq != 42 {
		t.Fatalf("status/seq mismatch: %d, %d", decoded.Status, decoded.Seq)
	}
}

func TestProtoGossipStateRoundtrip(t *testing.T) {
	state := &ProtoGossipState{
		Members: map[string]*ProtoMemberGossipState{
			"node-1": {Address: "host1:80", Id: "node-1", Kinds: []string{"a"}, Status: ProtoMemberAlive, Seq: 10},
			"node-2": {Address: "host2:80", Id: "node-2", Kinds: []string{"b", "c"}, Status: ProtoMemberSuspect, Seq: 20},
		},
	}

	data, err := state.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary error: %v", err)
	}

	decoded := &ProtoGossipState{}
	if err := decoded.UnmarshalBinary(data); err != nil {
		t.Fatalf("UnmarshalBinary error: %v", err)
	}

	if len(decoded.Members) != 2 {
		t.Fatalf("expected 2 members, got %d", len(decoded.Members))
	}

	m1 := decoded.Members["node-1"]
	if m1 == nil || m1.Address != "host1:80" || m1.Seq != 10 {
		t.Fatalf("node-1 mismatch: %+v", m1)
	}
}

func TestProtoGossipRequestRoundtrip(t *testing.T) {
	req := &ProtoGossipRequest{
		ClusterName: "test-cluster",
		State: &ProtoGossipState{
			Members: map[string]*ProtoMemberGossipState{
				"n1": {Address: "a:80", Id: "n1", Status: ProtoMemberAlive, Seq: 1},
			},
		},
	}

	data, err := req.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary error: %v", err)
	}

	decoded := &ProtoGossipRequest{}
	if err := decoded.UnmarshalBinary(data); err != nil {
		t.Fatalf("UnmarshalBinary error: %v", err)
	}

	if decoded.ClusterName != "test-cluster" {
		t.Fatalf("cluster name mismatch: %s", decoded.ClusterName)
	}
	if decoded.State == nil || len(decoded.State.Members) != 1 {
		t.Fatalf("state mismatch")
	}
}

func TestProtoClusterTopologyEventRoundtrip(t *testing.T) {
	event := &ProtoClusterTopologyEvent{
		Members: []*ProtoMember{
			{Address: "a:80", Id: "a", Kinds: []string{"k1"}, Status: ProtoMemberAlive, Seq: 1},
		},
		Joined: []*ProtoMember{
			{Address: "a:80", Id: "a", Kinds: []string{"k1"}, Status: ProtoMemberAlive, Seq: 1},
		},
		Left: []*ProtoMember{},
	}

	data, err := event.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary error: %v", err)
	}

	decoded := &ProtoClusterTopologyEvent{}
	if err := decoded.UnmarshalBinary(data); err != nil {
		t.Fatalf("UnmarshalBinary error: %v", err)
	}

	if len(decoded.Members) != 1 || len(decoded.Joined) != 1 || len(decoded.Left) != 0 {
		t.Fatalf("counts mismatch: members=%d joined=%d left=%d",
			len(decoded.Members), len(decoded.Joined), len(decoded.Left))
	}
}

func TestRegisterAllMessagesWithBinaryCodec(t *testing.T) {
	c := codec.NewBinaryCodec()
	RegisterAllMessages(c)

	// 测试编码解码 round-trip
	msg := &ProtoRemoteMessage{
		Target:   &ProtoPID{Address: "host:80", Id: "t1"},
		Payload:  []byte("test"),
		TypeName: "Test",
	}

	data, err := c.Encode(msg)
	if err != nil {
		t.Fatalf("Encode error: %v", err)
	}

	decoded, err := c.Decode(data)
	if err != nil {
		t.Fatalf("Decode error: %v", err)
	}

	rm, ok := decoded.(*ProtoRemoteMessage)
	if !ok {
		t.Fatalf("expected *ProtoRemoteMessage, got %T", decoded)
	}
	if rm.TypeName != "Test" {
		t.Fatalf("TypeName mismatch: %s", rm.TypeName)
	}
}
