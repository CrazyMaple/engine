package remote

import (
	"sync"
	"time"

	"engine/log"
)

// RetryQueueConfig 消息重发队列配置（At-Least-Once 语义可选）
type RetryQueueConfig struct {
	// Enabled 是否启用消息重发
	Enabled bool
	// MaxRetries 最大重试次数（默认 3）
	MaxRetries int
	// RetryDelay 重试间隔（默认 1s）
	RetryDelay time.Duration
	// MaxQueueSize 队列最大长度（默认 1000）
	MaxQueueSize int
}

func (c *RetryQueueConfig) defaults() {
	if c.MaxRetries <= 0 {
		c.MaxRetries = 3
	}
	if c.RetryDelay <= 0 {
		c.RetryDelay = time.Second
	}
	if c.MaxQueueSize <= 0 {
		c.MaxQueueSize = 1000
	}
}

type retryEntry struct {
	msg       *RemoteMessage
	attempts  int
	nextRetry time.Time
}

// retryQueue 消息重发队列
type retryQueue struct {
	config RetryQueueConfig
	queue  []retryEntry
	mu     sync.Mutex
}

func newRetryQueue(cfg RetryQueueConfig) *retryQueue {
	cfg.defaults()
	return &retryQueue{
		config: cfg,
		queue:  make([]retryEntry, 0, 64),
	}
}

// Add 将发送失败的消息加入重发队列
func (rq *retryQueue) Add(msg *RemoteMessage) {
	rq.mu.Lock()
	defer rq.mu.Unlock()

	if len(rq.queue) >= rq.config.MaxQueueSize {
		log.Debug("Retry queue full, dropping oldest message")
		rq.queue = rq.queue[1:]
	}

	rq.queue = append(rq.queue, retryEntry{
		msg:       msg,
		attempts:  1,
		nextRetry: time.Now().Add(rq.config.RetryDelay),
	})
}

// Drain 尝试重发队列中到期的消息
// sendFn 返回 nil 表示发送成功，非 nil 表示发送失败需继续重试
func (rq *retryQueue) Drain(sendFn func(*RemoteMessage) error) {
	rq.mu.Lock()
	if len(rq.queue) == 0 {
		rq.mu.Unlock()
		return
	}

	now := time.Now()
	var remaining []retryEntry

	// 取出当前队列
	entries := rq.queue
	rq.queue = make([]retryEntry, 0, len(entries))
	rq.mu.Unlock()

	for i := range entries {
		e := &entries[i]
		if now.Before(e.nextRetry) {
			remaining = append(remaining, *e)
			continue
		}

		if err := sendFn(e.msg); err != nil {
			e.attempts++
			if e.attempts <= rq.config.MaxRetries {
				e.nextRetry = now.Add(rq.config.RetryDelay * time.Duration(e.attempts))
				remaining = append(remaining, *e)
			} else {
				log.Debug("Retry exhausted for message after %d attempts", e.attempts-1)
			}
		}
		// 发送成功，不放回队列
	}

	if len(remaining) > 0 {
		rq.mu.Lock()
		rq.queue = append(rq.queue, remaining...)
		rq.mu.Unlock()
	}
}

// Len 返回队列长度
func (rq *retryQueue) Len() int {
	rq.mu.Lock()
	n := len(rq.queue)
	rq.mu.Unlock()
	return n
}
