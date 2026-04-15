package remote

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// 分片消息协议常量
const (
	// DefaultFragmentThreshold 超过该字节数的消息将被分片
	DefaultFragmentThreshold = 64 * 1024 // 64KB

	// DefaultFragmentSize 每个分片的大小
	DefaultFragmentSize = 32 * 1024 // 32KB

	// MaxFragments 单条消息最大分片数
	MaxFragments = 1024

	// FragmentMagic 分片消息魔数
	FragmentMagic byte = 0xFB

	// DefaultReassemblyTimeout 分片重组超时时间
	DefaultReassemblyTimeout = 30 * time.Second
)

// FragmentHeader 分片头部（固定 14 字节）
//
//	| magic (1) | messageID (8) | seq (2) | total (2) | flags (1) |
type FragmentHeader struct {
	MessageID uint64 // 消息唯一 ID（用于重组识别）
	Sequence  uint16 // 分片序号，从 0 开始
	Total     uint16 // 总分片数
	Flags     uint8  // 标志位（保留）
}

// FragmentHeaderSize 分片头部字节数
const FragmentHeaderSize = 14

// EncodeHeader 将分片头编码到 14 字节的 buffer
func (h *FragmentHeader) EncodeHeader(buf []byte) {
	buf[0] = FragmentMagic
	binary.BigEndian.PutUint64(buf[1:9], h.MessageID)
	binary.BigEndian.PutUint16(buf[9:11], h.Sequence)
	binary.BigEndian.PutUint16(buf[11:13], h.Total)
	buf[13] = h.Flags
}

// DecodeHeader 从 buffer 解码分片头
func DecodeHeader(buf []byte) (*FragmentHeader, error) {
	if len(buf) < FragmentHeaderSize {
		return nil, errors.New("fragment buffer too small")
	}
	if buf[0] != FragmentMagic {
		return nil, errors.New("not a fragment (magic mismatch)")
	}
	return &FragmentHeader{
		MessageID: binary.BigEndian.Uint64(buf[1:9]),
		Sequence:  binary.BigEndian.Uint16(buf[9:11]),
		Total:     binary.BigEndian.Uint16(buf[11:13]),
		Flags:     buf[13],
	}, nil
}

// IsFragment 判断 data 是否为分片消息（通过检查魔数）
func IsFragment(data []byte) bool {
	return len(data) >= FragmentHeaderSize && data[0] == FragmentMagic
}

// Fragmenter 消息分片器
type Fragmenter struct {
	threshold    int // 分片阈值
	fragmentSize int // 单个分片大小
	messageID    uint64
}

// NewFragmenter 创建分片器
func NewFragmenter(threshold, fragmentSize int) *Fragmenter {
	if threshold <= 0 {
		threshold = DefaultFragmentThreshold
	}
	if fragmentSize <= 0 {
		fragmentSize = DefaultFragmentSize
	}
	return &Fragmenter{
		threshold:    threshold,
		fragmentSize: fragmentSize,
	}
}

// ShouldFragment 判断消息是否需要分片
func (f *Fragmenter) ShouldFragment(data []byte) bool {
	return len(data) > f.threshold
}

// Fragment 将消息分片
// 返回：分片后的 [][]byte（每个元素是一个带头的完整分片数据）
func (f *Fragmenter) Fragment(data []byte) ([][]byte, error) {
	if !f.ShouldFragment(data) {
		return [][]byte{data}, nil
	}

	msgID := atomic.AddUint64(&f.messageID, 1)
	totalFragments := (len(data) + f.fragmentSize - 1) / f.fragmentSize
	if totalFragments > MaxFragments {
		return nil, fmt.Errorf("message too large: %d bytes requires %d fragments (max %d)", len(data), totalFragments, MaxFragments)
	}

	fragments := make([][]byte, totalFragments)
	for i := 0; i < totalFragments; i++ {
		start := i * f.fragmentSize
		end := start + f.fragmentSize
		if end > len(data) {
			end = len(data)
		}
		payload := data[start:end]

		frag := make([]byte, FragmentHeaderSize+len(payload))
		h := FragmentHeader{
			MessageID: msgID,
			Sequence:  uint16(i),
			Total:     uint16(totalFragments),
		}
		h.EncodeHeader(frag[:FragmentHeaderSize])
		copy(frag[FragmentHeaderSize:], payload)
		fragments[i] = frag
	}

	return fragments, nil
}

// ReassemblyState 重组状态
type reassemblyState struct {
	fragments  [][]byte
	received   int
	total      int
	createdAt  time.Time
}

// Reassembler 消息重组器
// 接收分片并按 MessageID 重组为完整消息
type Reassembler struct {
	mu       sync.Mutex
	states   map[uint64]*reassemblyState
	timeout  time.Duration
	stopChan chan struct{}
}

// NewReassembler 创建重组器
// timeout 为分片重组超时时间（超时后丢弃未完成的消息）
func NewReassembler(timeout time.Duration) *Reassembler {
	if timeout <= 0 {
		timeout = DefaultReassemblyTimeout
	}
	r := &Reassembler{
		states:   make(map[uint64]*reassemblyState),
		timeout:  timeout,
		stopChan: make(chan struct{}),
	}
	go r.cleanupLoop()
	return r
}

// Feed 输入一个分片
// 返回：完整消息（若已收齐），或 nil
func (r *Reassembler) Feed(data []byte) ([]byte, error) {
	h, err := DecodeHeader(data)
	if err != nil {
		return nil, err
	}

	payload := data[FragmentHeaderSize:]

	r.mu.Lock()
	defer r.mu.Unlock()

	state, ok := r.states[h.MessageID]
	if !ok {
		state = &reassemblyState{
			fragments: make([][]byte, h.Total),
			total:     int(h.Total),
			createdAt: time.Now(),
		}
		r.states[h.MessageID] = state
	}

	if int(h.Sequence) >= state.total {
		return nil, fmt.Errorf("sequence %d exceeds total %d", h.Sequence, state.total)
	}

	if state.fragments[h.Sequence] != nil {
		// 重复分片，忽略
		return nil, nil
	}

	// 拷贝 payload 以避免外部 buffer 复用导致的数据破坏
	frag := make([]byte, len(payload))
	copy(frag, payload)
	state.fragments[h.Sequence] = frag
	state.received++

	if state.received == state.total {
		// 重组完成
		delete(r.states, h.MessageID)
		total := 0
		for _, f := range state.fragments {
			total += len(f)
		}
		result := make([]byte, 0, total)
		for _, f := range state.fragments {
			result = append(result, f...)
		}
		return result, nil
	}

	return nil, nil
}

// cleanupLoop 定期清理超时的未完成消息
func (r *Reassembler) cleanupLoop() {
	ticker := time.NewTicker(r.timeout / 2)
	defer ticker.Stop()
	for {
		select {
		case <-r.stopChan:
			return
		case <-ticker.C:
			r.cleanup()
		}
	}
}

func (r *Reassembler) cleanup() {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	for id, state := range r.states {
		if now.Sub(state.createdAt) > r.timeout {
			delete(r.states, id)
		}
	}
}

// Pending 返回当前待重组的消息数量
func (r *Reassembler) Pending() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.states)
}

// Stop 停止重组器
func (r *Reassembler) Stop() {
	close(r.stopChan)
}
