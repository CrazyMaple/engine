package persistence

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// MemoryStorage 内存存储，用于测试
type MemoryStorage struct {
	mu   sync.RWMutex
	data map[string][]byte
}

// NewMemoryStorage 创建内存存储
func NewMemoryStorage() *MemoryStorage {
	return &MemoryStorage{
		data: make(map[string][]byte),
	}
}

func (ms *MemoryStorage) Save(_ context.Context, id string, state interface{}) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	ms.mu.Lock()
	ms.data[id] = data
	ms.mu.Unlock()
	return nil
}

func (ms *MemoryStorage) Load(_ context.Context, id string, target interface{}) error {
	ms.mu.RLock()
	data, ok := ms.data[id]
	ms.mu.RUnlock()
	if !ok {
		return fmt.Errorf("not found: %s", id)
	}
	return json.Unmarshal(data, target)
}

func (ms *MemoryStorage) Delete(_ context.Context, id string) error {
	ms.mu.Lock()
	delete(ms.data, id)
	ms.mu.Unlock()
	return nil
}

// Has 检查是否存在
func (ms *MemoryStorage) Has(id string) bool {
	ms.mu.RLock()
	_, ok := ms.data[id]
	ms.mu.RUnlock()
	return ok
}
