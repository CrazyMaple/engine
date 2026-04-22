package dashboard

import (
	"sync"
	"time"
)

const defaultAuditSize = 200

// AuditEntry 审计日志条目
type AuditEntry struct {
	Time     time.Time `json:"time"`
	Action   string    `json:"action"`
	Detail   string    `json:"detail"`
	Source   string    `json:"source"`    // 操作来源（如 "dashboard", "api"）
	Operator string    `json:"operator"`  // 操作人标识
	SourceIP string    `json:"source_ip"` // 来源 IP 地址
}

// AuditLog 审计日志，环形缓冲
type AuditLog struct {
	mu      sync.RWMutex
	entries []AuditEntry
	head    int
	count   int
	maxSize int
}

// NewAuditLog 创建审计日志
func NewAuditLog() *AuditLog {
	return &AuditLog{
		entries: make([]AuditEntry, defaultAuditSize),
		maxSize: defaultAuditSize,
	}
}

// Record 记录审计条目
func (a *AuditLog) Record(action, detail, source, operator, sourceIP string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.entries[a.head] = AuditEntry{
		Time:     time.Now(),
		Action:   action,
		Detail:   detail,
		Source:   source,
		Operator: operator,
		SourceIP: sourceIP,
	}
	a.head = (a.head + 1) % a.maxSize
	if a.count < a.maxSize {
		a.count++
	}
}

// Recent 返回最近 n 条审计记录（按时间倒序）
func (a *AuditLog) Recent(n int) []AuditEntry {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if n <= 0 || a.count == 0 {
		return nil
	}
	if n > a.count {
		n = a.count
	}

	result := make([]AuditEntry, n)
	for i := 0; i < n; i++ {
		idx := (a.head - 1 - i + a.maxSize) % a.maxSize
		result[i] = a.entries[idx]
	}
	return result
}
