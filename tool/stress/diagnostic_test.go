package stress

import (
	"strings"
	"testing"
	"time"

	"engine/actor"
	"engine/cluster"
)

func TestMembershipRecorder_RecordsLifecycleEvents(t *testing.T) {
	system := actor.NewActorSystem()
	rec := NewMembershipRecorder(system, "n0")
	defer rec.Stop()

	member := &cluster.Member{Id: "a", Address: "127.0.0.1:1", Status: cluster.MemberAlive}

	system.EventStream.Publish(&cluster.MemberJoinedEvent{Member: member})
	system.EventStream.Publish(&cluster.MemberSuspectEvent{Member: member})
	system.EventStream.Publish(&cluster.MemberDeadEvent{Member: member})
	system.EventStream.Publish(&cluster.MemberLeftEvent{Member: member})

	// 给订阅回调一点时间
	time.Sleep(50 * time.Millisecond)

	events := rec.Events()
	if len(events) != 4 {
		t.Fatalf("expected 4 events, got %d", len(events))
	}
	kinds := []string{events[0].Kind, events[1].Kind, events[2].Kind, events[3].Kind}
	want := []string{"joined", "suspect", "dead", "left"}
	for i, k := range kinds {
		if k != want[i] {
			t.Errorf("event %d kind=%s want %s", i, k, want[i])
		}
	}
	if !strings.Contains(rec.Dump(), "joined") {
		t.Error("dump missing joined event")
	}
}

func TestMembershipRecorder_WaitFor(t *testing.T) {
	system := actor.NewActorSystem()
	rec := NewMembershipRecorder(system, "n0")
	defer rec.Stop()

	go func() {
		time.Sleep(30 * time.Millisecond)
		system.EventStream.Publish(&cluster.MemberJoinedEvent{
			Member: &cluster.Member{Id: "x", Address: "127.0.0.1:1", Status: cluster.MemberAlive},
		})
	}()
	if !rec.WaitFor("joined", "x", 1*time.Second) {
		t.Error("expected to observe joined event")
	}
	if rec.WaitFor("dead", "", 50*time.Millisecond) {
		t.Error("did not expect dead event")
	}
}

func TestCheckpointDumper_StartStop(t *testing.T) {
	// 无 cluster 时也应安全运行
	d := StartCheckpointDumper(nil, nil, 20*time.Millisecond)
	time.Sleep(80 * time.Millisecond)
	d.Stop()
	// 重复 Stop 不应 panic
	d.Stop()
}
