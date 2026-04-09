package saga

import (
	"context"
	"fmt"
	"sync"
)

// MemorySagaStore 内存 Saga 状态存储（开发/测试用）
type MemorySagaStore struct {
	mu    sync.RWMutex
	store map[string]*SagaExecution
}

// NewMemorySagaStore 创建内存存储
func NewMemorySagaStore() *MemorySagaStore {
	return &MemorySagaStore{
		store: make(map[string]*SagaExecution),
	}
}

func (s *MemorySagaStore) Save(_ context.Context, execution *SagaExecution) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.store[execution.ID] = execution
	return nil
}

func (s *MemorySagaStore) Load(_ context.Context, sagaID string) (*SagaExecution, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	exec, ok := s.store[sagaID]
	if !ok {
		return nil, fmt.Errorf("saga %q not found", sagaID)
	}
	return exec, nil
}

// All 返回所有执行记录（调试用）
func (s *MemorySagaStore) All() []*SagaExecution {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*SagaExecution, 0, len(s.store))
	for _, exec := range s.store {
		result = append(result, exec)
	}
	return result
}

// SliceLogger 将日志记录到切片（测试用）
type SliceLogger struct {
	mu   sync.Mutex
	Logs []SagaLog
}

func NewSliceLogger() *SliceLogger {
	return &SliceLogger{}
}

func (l *SliceLogger) Log(entry SagaLog) {
	l.mu.Lock()
	l.Logs = append(l.Logs, entry)
	l.mu.Unlock()
}

// Entries 返回所有日志条目的副本
func (l *SliceLogger) Entries() []SagaLog {
	l.mu.Lock()
	defer l.mu.Unlock()
	result := make([]SagaLog, len(l.Logs))
	copy(result, l.Logs)
	return result
}
