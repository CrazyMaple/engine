package network

import (
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync"
	"time"
)

// KCP 协议命令字段
const (
	kcpCmdPush    uint8 = 1 // 数据包
	kcpCmdAck     uint8 = 2 // 确认包
	kcpCmdPing    uint8 = 3 // 保活包（仅携带 una，无 payload）
	kcpHeaderSize       = 15
)

// kcpSegment KCP 协议段
//
// 帧格式（小端）：
//   | conv(4) | cmd(1) | sn(4) | una(4) | len(2) | payload(len) |
type kcpSegment struct {
	conv    uint32
	cmd     uint8
	sn      uint32
	una     uint32
	data    []byte
	sendTs  time.Time
	rto     time.Duration
	xmit    uint32
	fastack uint32
}

func encodeKCPSegment(seg *kcpSegment) []byte {
	buf := make([]byte, kcpHeaderSize+len(seg.data))
	binary.LittleEndian.PutUint32(buf[0:], seg.conv)
	buf[4] = seg.cmd
	binary.LittleEndian.PutUint32(buf[5:], seg.sn)
	binary.LittleEndian.PutUint32(buf[9:], seg.una)
	binary.LittleEndian.PutUint16(buf[13:], uint16(len(seg.data)))
	copy(buf[15:], seg.data)
	return buf
}

func decodeKCPSegment(buf []byte) (*kcpSegment, error) {
	if len(buf) < kcpHeaderSize {
		return nil, errors.New("kcp: segment too short")
	}
	dataLen := int(binary.LittleEndian.Uint16(buf[13:]))
	if dataLen+kcpHeaderSize > len(buf) {
		return nil, errors.New("kcp: payload length mismatch")
	}
	seg := &kcpSegment{
		conv: binary.LittleEndian.Uint32(buf[0:]),
		cmd:  buf[4],
		sn:   binary.LittleEndian.Uint32(buf[5:]),
		una:  binary.LittleEndian.Uint32(buf[9:]),
	}
	if dataLen > 0 {
		data := make([]byte, dataLen)
		copy(data, buf[15:15+dataLen])
		seg.data = data
	}
	return seg, nil
}

// KCPConn KCP 连接，实现 network.Conn 接口
//
// 单会话模型：
//   - 服务端：多个 KCPConn 共用同一个 UDP socket，通过 conv + remote addr demux
//   - 客户端：每个 KCPConn 独占一个 UDP socket
//
// 上层使用方式与 TCPConn 一致（ReadMsg/WriteMsg），KCP 保证可靠按序到达。
type KCPConn struct {
	mu     sync.Mutex
	udp    net.PacketConn
	remote net.Addr
	conv   uint32
	config KCPConfig

	// 发送状态
	sendNext   uint32
	sendBuffer []*kcpSegment

	// 接收状态
	rcvNext   uint32
	rcvBuffer []*kcpSegment // 乱序到达，按 sn 排序
	rcvQueue  [][]byte      // 已就绪可读取的消息

	// ACK 待发送列表
	pendingAcks []uint32

	// RTT 测量
	srtt   time.Duration
	rttvar time.Duration
	rto    time.Duration

	// 同步原语
	readCond  *sync.Cond
	closeChan chan struct{}
	closeOnce sync.Once
	closed    bool

	lastRecv time.Time

	// 服务端模式
	serverMode bool
	inputChan  chan []byte
	onClose    func()
}

func newKCPConn(udp net.PacketConn, remote net.Addr, conv uint32, config KCPConfig, serverMode bool) *KCPConn {
	if config.Interval <= 0 {
		config = DefaultKCPConfig()
	}
	c := &KCPConn{
		udp:        udp,
		remote:     remote,
		conv:       conv,
		config:     config,
		rto:        config.RTOMin,
		srtt:       config.RTOMin,
		rttvar:     config.RTOMin / 2,
		closeChan:  make(chan struct{}),
		lastRecv:   time.Now(),
		serverMode: serverMode,
		inputChan:  make(chan []byte, 256),
	}
	c.readCond = sync.NewCond(&c.mu)
	go c.updateLoop()
	if serverMode {
		go c.dispatchLoop()
	} else {
		go c.clientRecvLoop()
	}
	return c
}

// clientRecvLoop 客户端模式：直接读 UDP socket
func (c *KCPConn) clientRecvLoop() {
	buf := make([]byte, c.config.Mtu*2)
	for {
		select {
		case <-c.closeChan:
			return
		default:
		}
		_ = c.udp.SetReadDeadline(time.Now().Add(c.config.Interval * 4))
		n, _, err := c.udp.ReadFrom(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			continue // 超时或临时错误，继续
		}
		dup := make([]byte, n)
		copy(dup, buf[:n])
		c.handleInput(dup)
	}
}

// dispatchLoop 服务端模式：从 inputChan 取数据
func (c *KCPConn) dispatchLoop() {
	for {
		select {
		case <-c.closeChan:
			return
		case buf := <-c.inputChan:
			c.handleInput(buf)
		}
	}
}

// inputForServer 由 KCPServer demux 调用，把原始数据塞入处理队列
func (c *KCPConn) inputForServer(buf []byte) {
	select {
	case c.inputChan <- buf:
	case <-c.closeChan:
	default:
		// inputChan 满，丢弃；KCP 会通过重传恢复
	}
}

func (c *KCPConn) handleInput(buf []byte) {
	seg, err := decodeKCPSegment(buf)
	if err != nil || seg.conv != c.conv {
		return
	}

	c.mu.Lock()
	c.lastRecv = time.Now()
	c.removeAckedLocked(seg.una)

	switch seg.cmd {
	case kcpCmdPush:
		c.pendingAcks = append(c.pendingAcks, seg.sn)
		c.acceptDataLocked(seg)
	case kcpCmdAck:
		c.handleAckLocked(seg)
	case kcpCmdPing:
		// una 已处理，无 payload
	}
	c.mu.Unlock()

	// 立即触发一次 flush，发送 ACK 并检查重传
	c.flush()
}

func (c *KCPConn) removeAckedLocked(una uint32) {
	if len(c.sendBuffer) == 0 {
		return
	}
	n := 0
	for _, s := range c.sendBuffer {
		if s.sn >= una {
			c.sendBuffer[n] = s
			n++
		}
	}
	c.sendBuffer = c.sendBuffer[:n]
}

func (c *KCPConn) handleAckLocked(seg *kcpSegment) {
	var rttSample time.Duration
	found := false
	n := 0
	for _, s := range c.sendBuffer {
		if s.sn == seg.sn {
			rttSample = time.Since(s.sendTs)
			found = true
			continue
		}
		if s.sn < seg.sn {
			s.fastack++
		}
		c.sendBuffer[n] = s
		n++
	}
	c.sendBuffer = c.sendBuffer[:n]
	if found && rttSample > 0 {
		c.updateRTOLocked(rttSample)
	}
}

func (c *KCPConn) updateRTOLocked(sample time.Duration) {
	if c.srtt == 0 {
		c.srtt = sample
		c.rttvar = sample / 2
	} else {
		delta := sample - c.srtt
		if delta < 0 {
			delta = -delta
		}
		c.rttvar = (3*c.rttvar + delta) / 4
		c.srtt = (7*c.srtt + sample) / 8
	}
	rto := c.srtt + 4*c.rttvar
	if rto < c.config.RTOMin {
		rto = c.config.RTOMin
	}
	if rto > c.config.RTOMax {
		rto = c.config.RTOMax
	}
	c.rto = rto
}

func (c *KCPConn) acceptDataLocked(seg *kcpSegment) {
	if seg.sn < c.rcvNext {
		return // 已交付，重复包
	}
	// 重复检测
	for _, e := range c.rcvBuffer {
		if e.sn == seg.sn {
			return
		}
	}
	if len(c.rcvBuffer) >= c.config.RecvWindow {
		return // 接收窗口满，丢弃（依赖重传）
	}
	// 按 sn 插入有序位置
	inserted := false
	for i, e := range c.rcvBuffer {
		if seg.sn < e.sn {
			c.rcvBuffer = append(c.rcvBuffer, nil)
			copy(c.rcvBuffer[i+1:], c.rcvBuffer[i:])
			c.rcvBuffer[i] = seg
			inserted = true
			break
		}
	}
	if !inserted {
		c.rcvBuffer = append(c.rcvBuffer, seg)
	}
	// 按序提交到 rcvQueue
	for len(c.rcvBuffer) > 0 && c.rcvBuffer[0].sn == c.rcvNext {
		c.rcvQueue = append(c.rcvQueue, c.rcvBuffer[0].data)
		c.rcvBuffer = c.rcvBuffer[1:]
		c.rcvNext++
	}
	if len(c.rcvQueue) > 0 {
		c.readCond.Signal()
	}
}

// updateLoop 周期性扫描发送缓冲，处理重传与会话超时
func (c *KCPConn) updateLoop() {
	ticker := time.NewTicker(c.config.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-c.closeChan:
			return
		case <-ticker.C:
			c.flush()
			c.mu.Lock()
			timeout := c.config.SessionTimeout > 0 && time.Since(c.lastRecv) > c.config.SessionTimeout
			c.mu.Unlock()
			if timeout {
				c.Close()
				return
			}
		}
	}
}

// flush 发送 ACK + 检查重传
func (c *KCPConn) flush() {
	c.mu.Lock()
	una := c.rcvNext
	acks := c.pendingAcks
	c.pendingAcks = nil

	now := time.Now()
	var retransmits []*kcpSegment
	deadLink := false
	for _, s := range c.sendBuffer {
		needResend := false
		if now.Sub(s.sendTs) >= s.rto {
			needResend = true
			if c.config.NoDelay == 0 {
				s.rto *= 2
			} else {
				s.rto = s.rto + s.rto/2
			}
			if s.rto > c.config.RTOMax {
				s.rto = c.config.RTOMax
			}
			s.xmit++
		} else if c.config.Resend > 0 && s.fastack >= uint32(c.config.Resend) {
			needResend = true
			s.fastack = 0
			s.xmit++
		}
		if needResend {
			s.una = una
			s.sendTs = now
			retransmits = append(retransmits, s)
		}
		if c.config.DeadLink > 0 && s.xmit > uint32(c.config.DeadLink) {
			deadLink = true
		}
	}
	c.mu.Unlock()

	if deadLink {
		c.Close()
		return
	}
	for _, sn := range acks {
		seg := &kcpSegment{
			conv: c.conv,
			cmd:  kcpCmdAck,
			sn:   sn,
			una:  una,
		}
		c.sendRaw(seg)
	}
	for _, s := range retransmits {
		c.sendRaw(s)
	}
}

func (c *KCPConn) sendRaw(seg *kcpSegment) {
	buf := encodeKCPSegment(seg)
	_, _ = c.udp.WriteTo(buf, c.remote)
}

func (c *KCPConn) pushSegment(data []byte) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return errors.New("kcp: connection closed")
	}
	if len(c.sendBuffer) >= c.config.SendWindow {
		c.mu.Unlock()
		return errors.New("kcp: send window full")
	}
	sn := c.sendNext
	c.sendNext++
	una := c.rcvNext
	seg := &kcpSegment{
		conv:   c.conv,
		cmd:    kcpCmdPush,
		sn:     sn,
		una:    una,
		data:   data,
		sendTs: time.Now(),
		rto:    c.rto,
	}
	c.sendBuffer = append(c.sendBuffer, seg)
	c.mu.Unlock()
	c.sendRaw(seg)
	return nil
}

// ReadMsg 阻塞读取一个完整消息
func (c *KCPConn) ReadMsg() ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for len(c.rcvQueue) == 0 {
		if c.closed {
			return nil, io.EOF
		}
		c.readCond.Wait()
	}
	msg := c.rcvQueue[0]
	c.rcvQueue = c.rcvQueue[1:]
	return msg, nil
}

// WriteMsg 写入消息（多段拼接为单个 KCP 段）
//
// 注意：单条消息长度需小于 Mtu - kcpHeaderSize；超长消息需上层分片。
func (c *KCPConn) WriteMsg(args ...[]byte) error {
	total := 0
	for _, a := range args {
		total += len(a)
	}
	maxPayload := c.config.Mtu - kcpHeaderSize
	if total > maxPayload {
		return errors.New("kcp: message exceeds MTU payload limit")
	}
	if total == 0 {
		return errors.New("kcp: empty message")
	}
	data := make([]byte, total)
	n := 0
	for _, a := range args {
		n += copy(data[n:], a)
	}
	return c.pushSegment(data)
}

func (c *KCPConn) LocalAddr() net.Addr {
	if c.udp != nil {
		return c.udp.LocalAddr()
	}
	return nil
}

func (c *KCPConn) RemoteAddr() net.Addr {
	return c.remote
}

func (c *KCPConn) Close() {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.closed = true
		c.mu.Unlock()
		close(c.closeChan)
		c.readCond.Broadcast()
		if c.onClose != nil {
			c.onClose()
		}
		// 仅客户端模式拥有独占的 UDP socket
		if !c.serverMode && c.udp != nil {
			c.udp.Close()
		}
	})
}

func (c *KCPConn) Destroy() {
	c.Close()
}

// Conv 返回会话 ID（调试/统计用）
func (c *KCPConn) Conv() uint32 { return c.conv }
