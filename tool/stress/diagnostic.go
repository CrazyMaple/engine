package stress

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"engine/actor"
	"engine/cluster"
)

// MembershipRecorder 订阅集群成员事件，记录每次变更的时间戳，用于根因定位
type MembershipRecorder struct {
	mu           sync.Mutex
	events       []MembershipEvent
	subscription *actor.Subscription
	eventStream  *actor.EventStream
	nodeLabel    string
	started      time.Time
}

// MembershipEvent 一条成员事件
type MembershipEvent struct {
	At      time.Duration
	Node    string
	Kind    string
	Subject string
}

// NewMembershipRecorder 订阅 ActorSystem.EventStream 的成员变更事件
// nodeLabel 标记事件属于哪个节点的视角（用于多节点比对）
func NewMembershipRecorder(system *actor.ActorSystem, nodeLabel string) *MembershipRecorder {
	r := &MembershipRecorder{
		eventStream: system.EventStream,
		nodeLabel:   nodeLabel,
		started:     time.Now(),
	}
	r.subscription = system.EventStream.Subscribe(func(ev interface{}) {
		r.record(ev)
	})
	return r
}

// Stop 取消订阅
func (r *MembershipRecorder) Stop() {
	if r.subscription != nil && r.eventStream != nil {
		r.eventStream.Unsubscribe(r.subscription)
	}
}

func (r *MembershipRecorder) record(ev interface{}) {
	r.mu.Lock()
	defer r.mu.Unlock()
	at := time.Since(r.started)
	switch m := ev.(type) {
	case *cluster.MemberJoinedEvent:
		r.events = append(r.events, MembershipEvent{At: at, Node: r.nodeLabel, Kind: "joined", Subject: memberDesc(m.Member)})
	case *cluster.MemberLeftEvent:
		r.events = append(r.events, MembershipEvent{At: at, Node: r.nodeLabel, Kind: "left", Subject: memberDesc(m.Member)})
	case *cluster.MemberSuspectEvent:
		r.events = append(r.events, MembershipEvent{At: at, Node: r.nodeLabel, Kind: "suspect", Subject: memberDesc(m.Member)})
	case *cluster.MemberDeadEvent:
		r.events = append(r.events, MembershipEvent{At: at, Node: r.nodeLabel, Kind: "dead", Subject: memberDesc(m.Member)})
	}
}

// Events 返回记录的事件副本
func (r *MembershipRecorder) Events() []MembershipEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]MembershipEvent, len(r.events))
	copy(out, r.events)
	return out
}

// WaitFor 等待某类事件首次出现，超时返回 false
// kind 之一：joined/left/suspect/dead；subject 为空表示任意成员。
func (r *MembershipRecorder) WaitFor(kind, subject string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		for _, e := range r.events {
			if e.Kind == kind && (subject == "" || strings.Contains(e.Subject, subject)) {
				r.mu.Unlock()
				return true
			}
		}
		r.mu.Unlock()
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

// Dump 以可读形式打印事件流
func (r *MembershipRecorder) Dump() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var sb strings.Builder
	for _, e := range r.events {
		fmt.Fprintf(&sb, "[%s +%6.3fs] %-7s %s\n", e.Node, e.At.Seconds(), e.Kind, e.Subject)
	}
	return sb.String()
}

// WaitMembersEventDriven 事件驱动地等待目标节点观测到 minMembers 个成员
// 相比轮询 waitForConvergence，它一旦收到 MemberJoinedEvent 就重新评估，能更准确地反映收敛时刻。
func WaitMembersEventDriven(t *testing.T, c *cluster.Cluster, es *actor.EventStream, minMembers int, timeout time.Duration) bool {
	t.Helper()
	if c == nil || es == nil {
		return false
	}
	// 若已经满足直接返回
	if len(c.Members()) >= minMembers {
		return true
	}
	notify := make(chan struct{}, 8)
	sub := es.Subscribe(func(ev interface{}) {
		switch ev.(type) {
		case *cluster.MemberJoinedEvent,
			*cluster.MemberLeftEvent,
			*cluster.MemberDeadEvent,
			*cluster.ClusterTopologyEvent:
			select {
			case notify <- struct{}{}:
			default:
			}
		}
	})
	defer es.Unsubscribe(sub)

	deadline := time.Now().Add(timeout)
	// 定期兜底检查（gossip 有可能在事件订阅之前已经推进）
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		if len(c.Members()) >= minMembers {
			return true
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return false
		}
		select {
		case <-notify:
		case <-ticker.C:
		case <-time.After(remaining):
			return len(c.Members()) >= minMembers
		}
	}
}

// MembershipSnapshot 单个节点的成员视角快照
type MembershipSnapshot struct {
	Node    string
	At      time.Duration
	Members []memberSummary
}

type memberSummary struct {
	Address string
	Status  string
}

// CheckpointDumper 每秒 dump 一次集群成员视图，帮助定位卡在哪一阶段
type CheckpointDumper struct {
	mu       sync.Mutex
	stopCh   chan struct{}
	started  time.Time
	clusters []*cluster.Cluster
	labels   []string
	history  []MembershipSnapshot
	wg       sync.WaitGroup
}

// StartCheckpointDumper 启动一个 goroutine 每 interval 记录一次所有节点的成员视图
// 返回值需在测试结束前调用 Stop。
func StartCheckpointDumper(clusters []*cluster.Cluster, labels []string, interval time.Duration) *CheckpointDumper {
	if interval <= 0 {
		interval = 1 * time.Second
	}
	d := &CheckpointDumper{
		stopCh:   make(chan struct{}),
		started:  time.Now(),
		clusters: clusters,
		labels:   labels,
	}
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		tick := time.NewTicker(interval)
		defer tick.Stop()
		for {
			select {
			case <-d.stopCh:
				return
			case <-tick.C:
				d.capture()
			}
		}
	}()
	return d
}

func (d *CheckpointDumper) capture() {
	d.mu.Lock()
	defer d.mu.Unlock()
	for i, c := range d.clusters {
		if c == nil {
			continue
		}
		label := fmt.Sprintf("node%d", i)
		if i < len(d.labels) {
			label = d.labels[i]
		}
		var ms []memberSummary
		for _, m := range c.Members() {
			ms = append(ms, memberSummary{
				Address: m.Address,
				Status:  m.Status.String(),
			})
		}
		sort.Slice(ms, func(a, b int) bool { return ms[a].Address < ms[b].Address })
		d.history = append(d.history, MembershipSnapshot{
			Node:    label,
			At:      time.Since(d.started),
			Members: ms,
		})
	}
}

// Stop 终止采样 goroutine
func (d *CheckpointDumper) Stop() {
	select {
	case <-d.stopCh:
		return
	default:
		close(d.stopCh)
	}
	d.wg.Wait()
}

// Snapshots 返回采样副本
func (d *CheckpointDumper) Snapshots() []MembershipSnapshot {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]MembershipSnapshot, len(d.history))
	copy(out, d.history)
	return out
}

// Dump 以表格形式输出采样历史
func (d *CheckpointDumper) Dump() string {
	snapshots := d.Snapshots()
	var sb strings.Builder
	sb.WriteString("T(s)   | Node   | Members\n")
	sb.WriteString("-------+--------+---------\n")
	for _, snap := range snapshots {
		addrs := make([]string, 0, len(snap.Members))
		for _, m := range snap.Members {
			addrs = append(addrs, fmt.Sprintf("%s(%s)", m.Address, m.Status))
		}
		fmt.Fprintf(&sb, "%6.2f | %-6s | %s\n", snap.At.Seconds(), snap.Node, strings.Join(addrs, ", "))
	}
	return sb.String()
}

// memberDesc 格式化单个成员
func memberDesc(m *cluster.Member) string {
	if m == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%s/%s(%s)", m.Id, m.Address, m.Status)
}

// AwaitNodeLeft 事件驱动地等待 observer 视角确认 subjectAddress 不再 Alive
// 相比在 waitForCondition 中轮询 Members()，它订阅 MemberLeft/Dead 事件，更灵敏。
func AwaitNodeLeft(es *actor.EventStream, c *cluster.Cluster, subjectAddress string, timeout time.Duration) bool {
	if es == nil || c == nil {
		return false
	}
	// 若已不可见则立即返回
	if !hasAliveMember(c, subjectAddress) {
		return true
	}
	notify := make(chan struct{}, 8)
	sub := es.Subscribe(func(ev interface{}) {
		switch e := ev.(type) {
		case *cluster.MemberLeftEvent:
			if e.Member != nil && e.Member.Address == subjectAddress {
				select {
				case notify <- struct{}{}:
				default:
				}
			}
		case *cluster.MemberDeadEvent:
			if e.Member != nil && e.Member.Address == subjectAddress {
				select {
				case notify <- struct{}{}:
				default:
				}
			}
		}
	})
	defer es.Unsubscribe(sub)

	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		if !hasAliveMember(c, subjectAddress) {
			return true
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return false
		}
		select {
		case <-notify:
		case <-ticker.C:
		case <-time.After(remaining):
			return !hasAliveMember(c, subjectAddress)
		}
	}
}

func hasAliveMember(c *cluster.Cluster, address string) bool {
	for _, m := range c.Members() {
		if m.Address == address && m.Status == cluster.MemberAlive {
			return true
		}
	}
	return false
}
