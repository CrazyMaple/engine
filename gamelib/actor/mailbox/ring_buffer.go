package mailbox

import (
	"sync/atomic"
)

// mpscRingBuffer 无锁 MPSC 环形缓冲区
// 多生产者通过 CAS 预留写入位置，单消费者顺序读取
// 所有槽位预分配，避免 MPSC 链表队列的 per-node 分配开销
type mpscRingBuffer struct {
	buffer []interface{} // 预分配槽位
	mask   uint64        // capacity - 1（容量必须为 2 的幂）
	_pad0  [48]byte      // 避免 false sharing

	writePos uint64   // 原子：下一个写入位置（CAS 预留）
	_pad1    [56]byte // 避免 false sharing

	commitPos uint64   // 原子：已提交的最高位置 + 1（按序递增）
	_pad2     [56]byte // 避免 false sharing

	readPos uint64   // 仅消费者使用：当前读取位置
	_pad3   [56]byte // 避免 false sharing
}

// newMPSCRingBuffer 创建 MPSC 环形缓冲区
// capacity 将向上取整为 2 的幂，最小为 64
func newMPSCRingBuffer(capacity int) *mpscRingBuffer {
	capacity = nextPowerOf2(capacity)
	if capacity < 64 {
		capacity = 64
	}

	return &mpscRingBuffer{
		buffer: make([]interface{}, capacity),
		mask:   uint64(capacity - 1),
	}
}

func (rb *mpscRingBuffer) Push(val interface{}) bool {
	for {
		wp := atomic.LoadUint64(&rb.writePos)
		rp := atomic.LoadUint64(&rb.readPos)

		if wp-rp >= uint64(len(rb.buffer)) {
			return false
		}

		if atomic.CompareAndSwapUint64(&rb.writePos, wp, wp+1) {
			rb.buffer[wp&rb.mask] = val

			for !atomic.CompareAndSwapUint64(&rb.commitPos, wp, wp+1) {
			}
			return true
		}
	}
}

func (rb *mpscRingBuffer) Pop() interface{} {
	rp := rb.readPos
	cp := atomic.LoadUint64(&rb.commitPos)

	if rp >= cp {
		return nil
	}

	idx := rp & rb.mask
	val := rb.buffer[idx]
	rb.buffer[idx] = nil

	atomic.StoreUint64(&rb.readPos, rp+1)
	return val
}

func (rb *mpscRingBuffer) PopBatch(dst []interface{}, max int) []interface{} {
	rp := rb.readPos
	cp := atomic.LoadUint64(&rb.commitPos)

	available := cp - rp
	if available == 0 {
		return dst
	}
	n := int(available)
	if n > max {
		n = max
	}

	for i := 0; i < n; i++ {
		idx := (rp + uint64(i)) & rb.mask
		dst = append(dst, rb.buffer[idx])
		rb.buffer[idx] = nil
	}

	atomic.StoreUint64(&rb.readPos, rp+uint64(n))
	return dst
}

func (rb *mpscRingBuffer) Empty() bool {
	return rb.readPos >= atomic.LoadUint64(&rb.commitPos)
}

func (rb *mpscRingBuffer) Len() int {
	cp := atomic.LoadUint64(&rb.commitPos)
	rp := atomic.LoadUint64(&rb.readPos)
	if cp <= rp {
		return 0
	}
	return int(cp - rp)
}

func (rb *mpscRingBuffer) Cap() int {
	return len(rb.buffer)
}

func nextPowerOf2(n int) int {
	if n <= 1 {
		return 1
	}
	n--
	n |= n >> 1
	n |= n >> 2
	n |= n >> 4
	n |= n >> 8
	n |= n >> 16
	n |= n >> 32
	return n + 1
}
