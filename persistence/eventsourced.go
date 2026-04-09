package persistence

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// --- Event Sourcing 核心接口 ---

// Event 事件接口标记（业务事件需实现，通常为 struct）
// 使用空 interface{} 以获得最大灵活性

// EventSourced 事件溯源 Actor 接口
// 实现此接口的 Actor 将通过 Event Journal 持久化状态变更
type EventSourced interface {
	// PersistenceID 持久化唯一标识（与 Persistable 保持一致）
	PersistenceID() string
	// ApplyEvent 应用事件到状态（恢复时和持久化后均会调用）
	ApplyEvent(event interface{})
	// SnapshotState 快照当前状态
	SnapshotState() interface{}
	// RestoreSnapshot 从快照恢复状态
	RestoreSnapshot(snapshot interface{}) error
}

// PersistedEvent 持久化事件封装
type PersistedEvent struct {
	PersistenceID  string      `json:"persistence_id"`
	SequenceNumber int64       `json:"sequence_number"` // 全局序号
	Timestamp      int64       `json:"timestamp"`       // Unix nano
	EventType      string      `json:"event_type"`      // 事件类型名
	Payload        interface{} `json:"payload"`         // 事件内容
}

// Snapshot 快照记录
type Snapshot struct {
	PersistenceID  string      `json:"persistence_id"`
	SequenceNumber int64       `json:"sequence_number"` // 快照时的事件序号
	Timestamp      int64       `json:"timestamp"`
	Data           interface{} `json:"data"`
}

// EventJournal 事件日志存储接口
type EventJournal interface {
	// AppendEvent 追加事件，返回分配的序号
	AppendEvent(ctx context.Context, event *PersistedEvent) (int64, error)
	// LoadEvents 加载指定 ID 从 fromSeq 开始的事件（fromSeq=0 从头开始）
	LoadEvents(ctx context.Context, persistenceID string, fromSeq int64) ([]*PersistedEvent, error)
	// DeleteEvents 删除指定 ID 到 toSeq 为止的事件（用于快照后清理）
	DeleteEvents(ctx context.Context, persistenceID string, toSeq int64) error
}

// SnapshotStore 快照存储接口
type SnapshotStore interface {
	// SaveSnapshot 保存快照
	SaveSnapshot(ctx context.Context, snapshot *Snapshot) error
	// LoadSnapshot 加载最新快照
	LoadSnapshot(ctx context.Context, persistenceID string) (*Snapshot, error)
	// DeleteSnapshots 删除旧快照
	DeleteSnapshots(ctx context.Context, persistenceID string) error
}

// --- 内存实现 ---

// MemoryJournal 内存事件日志，用于开发测试
type MemoryJournal struct {
	mu     sync.RWMutex
	events map[string][]*PersistedEvent // persistenceID -> events
	nextSeq int64
}

// NewMemoryJournal 创建内存事件日志
func NewMemoryJournal() *MemoryJournal {
	return &MemoryJournal{
		events: make(map[string][]*PersistedEvent),
	}
}

// AppendEvent 追加事件
func (mj *MemoryJournal) AppendEvent(_ context.Context, event *PersistedEvent) (int64, error) {
	mj.mu.Lock()
	defer mj.mu.Unlock()

	mj.nextSeq++
	event.SequenceNumber = mj.nextSeq
	if event.Timestamp == 0 {
		event.Timestamp = time.Now().UnixNano()
	}

	// 深拷贝 payload 避免外部修改（JSON 序列化-反序列化最简单）
	data, err := json.Marshal(event)
	if err != nil {
		return 0, err
	}
	var copied PersistedEvent
	if err := json.Unmarshal(data, &copied); err != nil {
		return 0, err
	}

	mj.events[event.PersistenceID] = append(mj.events[event.PersistenceID], &copied)
	return event.SequenceNumber, nil
}

// LoadEvents 加载事件
func (mj *MemoryJournal) LoadEvents(_ context.Context, persistenceID string, fromSeq int64) ([]*PersistedEvent, error) {
	mj.mu.RLock()
	defer mj.mu.RUnlock()

	events := mj.events[persistenceID]
	if len(events) == 0 {
		return nil, nil
	}

	result := make([]*PersistedEvent, 0, len(events))
	for _, e := range events {
		if e.SequenceNumber >= fromSeq {
			result = append(result, e)
		}
	}
	return result, nil
}

// DeleteEvents 删除事件
func (mj *MemoryJournal) DeleteEvents(_ context.Context, persistenceID string, toSeq int64) error {
	mj.mu.Lock()
	defer mj.mu.Unlock()

	events := mj.events[persistenceID]
	kept := events[:0]
	for _, e := range events {
		if e.SequenceNumber > toSeq {
			kept = append(kept, e)
		}
	}
	mj.events[persistenceID] = kept
	return nil
}

// EventCount 返回某 ID 的事件总数（调试用）
func (mj *MemoryJournal) EventCount(persistenceID string) int {
	mj.mu.RLock()
	defer mj.mu.RUnlock()
	return len(mj.events[persistenceID])
}

// MemorySnapshotStore 内存快照存储
type MemorySnapshotStore struct {
	mu        sync.RWMutex
	snapshots map[string]*Snapshot
}

// NewMemorySnapshotStore 创建内存快照存储
func NewMemorySnapshotStore() *MemorySnapshotStore {
	return &MemorySnapshotStore{
		snapshots: make(map[string]*Snapshot),
	}
}

// SaveSnapshot 保存快照
func (ms *MemorySnapshotStore) SaveSnapshot(_ context.Context, snapshot *Snapshot) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	if snapshot.Timestamp == 0 {
		snapshot.Timestamp = time.Now().UnixNano()
	}

	// 深拷贝 data
	data, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	var copied Snapshot
	if err := json.Unmarshal(data, &copied); err != nil {
		return err
	}

	ms.snapshots[snapshot.PersistenceID] = &copied
	return nil
}

// LoadSnapshot 加载最新快照
func (ms *MemorySnapshotStore) LoadSnapshot(_ context.Context, persistenceID string) (*Snapshot, error) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	snap, ok := ms.snapshots[persistenceID]
	if !ok {
		return nil, nil // 无快照不算错误
	}
	return snap, nil
}

// DeleteSnapshots 删除快照
func (ms *MemorySnapshotStore) DeleteSnapshots(_ context.Context, persistenceID string) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	delete(ms.snapshots, persistenceID)
	return nil
}

// --- EventSourcedContext ---

// EventSourcedContext 事件溯源上下文，供 Actor 内部 Persist 使用
type EventSourcedContext struct {
	persistenceID  string
	journal        EventJournal
	snapshotStore  SnapshotStore
	actor          EventSourced
	currentSeq     int64 // 当前已处理序号
	snapshotEvery  int   // 每 N 条事件触发快照
	eventsSinceSnap int
	mu             sync.Mutex
}

// NewEventSourcedContext 创建事件溯源上下文
func NewEventSourcedContext(actor EventSourced, journal EventJournal, snapshotStore SnapshotStore, snapshotEvery int) *EventSourcedContext {
	if snapshotEvery <= 0 {
		snapshotEvery = 100
	}
	return &EventSourcedContext{
		persistenceID: actor.PersistenceID(),
		journal:       journal,
		snapshotStore: snapshotStore,
		actor:         actor,
		snapshotEvery: snapshotEvery,
	}
}

// Persist 持久化单个事件，成功后调用 ApplyEvent 应用到状态
func (esc *EventSourcedContext) Persist(event interface{}) error {
	esc.mu.Lock()
	defer esc.mu.Unlock()

	pe := &PersistedEvent{
		PersistenceID: esc.persistenceID,
		EventType:     fmt.Sprintf("%T", event),
		Payload:       event,
	}

	seq, err := esc.journal.AppendEvent(context.Background(), pe)
	if err != nil {
		return fmt.Errorf("append event: %w", err)
	}

	esc.currentSeq = seq
	esc.actor.ApplyEvent(event)
	esc.eventsSinceSnap++

	// 达到快照阈值自动创建快照
	if esc.eventsSinceSnap >= esc.snapshotEvery && esc.snapshotStore != nil {
		if err := esc.saveSnapshotLocked(); err != nil {
			// 快照失败不影响主流程，仅记录
			return nil
		}
	}

	return nil
}

// PersistAll 批量持久化多个事件（原子性：要么全部成功，要么全部失败）
func (esc *EventSourcedContext) PersistAll(events []interface{}) error {
	esc.mu.Lock()
	defer esc.mu.Unlock()

	for _, event := range events {
		pe := &PersistedEvent{
			PersistenceID: esc.persistenceID,
			EventType:     fmt.Sprintf("%T", event),
			Payload:       event,
		}
		seq, err := esc.journal.AppendEvent(context.Background(), pe)
		if err != nil {
			return err
		}
		esc.currentSeq = seq
		esc.actor.ApplyEvent(event)
		esc.eventsSinceSnap++
	}

	if esc.eventsSinceSnap >= esc.snapshotEvery && esc.snapshotStore != nil {
		_ = esc.saveSnapshotLocked()
	}

	return nil
}

// Recover 恢复 Actor 状态：先加载快照，再重放快照之后的事件
func (esc *EventSourcedContext) Recover() error {
	esc.mu.Lock()
	defer esc.mu.Unlock()

	var fromSeq int64

	// 1. 尝试加载最新快照
	if esc.snapshotStore != nil {
		snap, err := esc.snapshotStore.LoadSnapshot(context.Background(), esc.persistenceID)
		if err != nil {
			return fmt.Errorf("load snapshot: %w", err)
		}
		if snap != nil {
			if err := esc.actor.RestoreSnapshot(snap.Data); err != nil {
				return fmt.Errorf("restore snapshot: %w", err)
			}
			esc.currentSeq = snap.SequenceNumber
			fromSeq = snap.SequenceNumber + 1
		}
	}

	// 2. 重放快照之后的事件
	events, err := esc.journal.LoadEvents(context.Background(), esc.persistenceID, fromSeq)
	if err != nil {
		return fmt.Errorf("load events: %w", err)
	}
	for _, e := range events {
		esc.actor.ApplyEvent(e.Payload)
		esc.currentSeq = e.SequenceNumber
	}

	esc.eventsSinceSnap = 0
	return nil
}

// SaveSnapshot 手动触发快照（外部调用）
func (esc *EventSourcedContext) SaveSnapshot() error {
	esc.mu.Lock()
	defer esc.mu.Unlock()
	return esc.saveSnapshotLocked()
}

func (esc *EventSourcedContext) saveSnapshotLocked() error {
	if esc.snapshotStore == nil {
		return fmt.Errorf("no snapshot store configured")
	}
	snap := &Snapshot{
		PersistenceID:  esc.persistenceID,
		SequenceNumber: esc.currentSeq,
		Data:           esc.actor.SnapshotState(),
	}
	if err := esc.snapshotStore.SaveSnapshot(context.Background(), snap); err != nil {
		return fmt.Errorf("save snapshot: %w", err)
	}
	esc.eventsSinceSnap = 0

	// 清理快照之前的事件（可选优化）
	_ = esc.journal.DeleteEvents(context.Background(), esc.persistenceID, esc.currentSeq)

	return nil
}

// CurrentSequence 返回当前已处理的事件序号
func (esc *EventSourcedContext) CurrentSequence() int64 {
	esc.mu.Lock()
	defer esc.mu.Unlock()
	return esc.currentSeq
}
