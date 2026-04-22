package mail

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// MemoryMailStorage 内存邮件存储（开发/测试用，保持现有行为兼容）
type MemoryMailStorage struct {
	mu   sync.RWMutex
	data map[string][]*Mail // playerID -> mails
}

// NewMemoryMailStorage 创建内存邮件存储
func NewMemoryMailStorage() *MemoryMailStorage {
	return &MemoryMailStorage{
		data: make(map[string][]*Mail),
	}
}

func (s *MemoryMailStorage) Save(_ context.Context, playerID string, mail *Mail) error {
	if playerID == "" {
		return fmt.Errorf("player id required")
	}
	if mail == nil {
		return fmt.Errorf("mail required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[playerID] = append(s.data[playerID], mail)
	return nil
}

func (s *MemoryMailStorage) Load(_ context.Context, playerID string) ([]*Mail, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	mails := s.data[playerID]
	result := make([]*Mail, len(mails))
	copy(result, mails)
	return result, nil
}

func (s *MemoryMailStorage) MarkRead(_ context.Context, playerID string, mailID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range s.data[playerID] {
		if m.ID == mailID {
			m.Read = true
			return nil
		}
	}
	return fmt.Errorf("mail %s not found for player %s", mailID, playerID)
}

func (s *MemoryMailStorage) Delete(_ context.Context, playerID string, mailID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	mails := s.data[playerID]
	for i, m := range mails {
		if m.ID == mailID {
			s.data[playerID] = append(mails[:i], mails[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("mail %s not found for player %s", mailID, playerID)
}

func (s *MemoryMailStorage) CleanExpired(_ context.Context) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	total := 0
	for pid, mails := range s.data {
		kept := make([]*Mail, 0, len(mails))
		for _, m := range mails {
			if m.IsExpired(now) && !m.HasAttachments() {
				total++
				continue
			}
			kept = append(kept, m)
		}
		s.data[pid] = kept
	}
	return total, nil
}
