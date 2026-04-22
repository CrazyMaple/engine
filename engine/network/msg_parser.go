package network

import (
	"encoding/binary"
	"errors"
	"io"
	"math"
	"sync"
)

// msgBufPool 消息缓冲区对象池，复用常见大小的消息缓冲区以减少 GC 压力
const msgBufPoolMaxSize = 4096

var msgBufPool = sync.Pool{
	New: func() interface{} {
		buf := make([]byte, msgBufPoolMaxSize)
		return &buf
	},
}

// acquireMsgBuf 从池中获取消息缓冲区
func acquireMsgBuf(size int) []byte {
	if size <= msgBufPoolMaxSize {
		bufPtr := msgBufPool.Get().(*[]byte)
		return (*bufPtr)[:size]
	}
	return make([]byte, size)
}

// ReleaseMsgBuf 归还消息缓冲区到对象池（仅 <= 4KB 的缓冲区会被回收）
func ReleaseMsgBuf(buf []byte) {
	if cap(buf) >= msgBufPoolMaxSize {
		b := buf[:msgBufPoolMaxSize]
		msgBufPool.Put(&b)
	}
}

// MsgParser 消息分帧器
// 消息格式: | len | data |
type MsgParser struct {
	lenMsgLen    int    // 长度字段的字节数 (1, 2, 4)
	minMsgLen    uint32 // 最小消息长度
	maxMsgLen    uint32 // 最大消息长度
	littleEndian bool   // 是否小端序
}

// NewMsgParser 创建消息解析器
func NewMsgParser() *MsgParser {
	return &MsgParser{
		lenMsgLen:    2,
		minMsgLen:    1,
		maxMsgLen:    4096,
		littleEndian: false,
	}
}

// SetMsgLen 设置消息长度参数
func (p *MsgParser) SetMsgLen(lenMsgLen int, minMsgLen uint32, maxMsgLen uint32) {
	if lenMsgLen == 1 || lenMsgLen == 2 || lenMsgLen == 4 {
		p.lenMsgLen = lenMsgLen
	}
	if minMsgLen != 0 {
		p.minMsgLen = minMsgLen
	}
	if maxMsgLen != 0 {
		p.maxMsgLen = maxMsgLen
	}

	var max uint32
	switch p.lenMsgLen {
	case 1:
		max = math.MaxUint8
	case 2:
		max = math.MaxUint16
	case 4:
		max = math.MaxUint32
	}
	if p.minMsgLen > max {
		p.minMsgLen = max
	}
	if p.maxMsgLen > max {
		p.maxMsgLen = max
	}
}

// SetByteOrder 设置字节序
func (p *MsgParser) SetByteOrder(littleEndian bool) {
	p.littleEndian = littleEndian
}

// Read 读取消息
func (p *MsgParser) Read(conn *TCPConn) ([]byte, error) {
	var b [4]byte
	bufMsgLen := b[:p.lenMsgLen]

	// 读取长度字段
	if _, err := io.ReadFull(conn, bufMsgLen); err != nil {
		return nil, err
	}

	// 解析长度
	var msgLen uint32
	switch p.lenMsgLen {
	case 1:
		msgLen = uint32(bufMsgLen[0])
	case 2:
		if p.littleEndian {
			msgLen = uint32(binary.LittleEndian.Uint16(bufMsgLen))
		} else {
			msgLen = uint32(binary.BigEndian.Uint16(bufMsgLen))
		}
	case 4:
		if p.littleEndian {
			msgLen = binary.LittleEndian.Uint32(bufMsgLen)
		} else {
			msgLen = binary.BigEndian.Uint32(bufMsgLen)
		}
	}

	// 检查长度
	if msgLen > p.maxMsgLen {
		return nil, errors.New("message too long")
	} else if msgLen < p.minMsgLen {
		return nil, errors.New("message too short")
	}

	// 读取数据（使用对象池减少分配）
	msgData := acquireMsgBuf(int(msgLen))
	if _, err := io.ReadFull(conn, msgData); err != nil {
		return nil, err
	}

	return msgData, nil
}

// Write 写入消息
func (p *MsgParser) Write(conn *TCPConn, args ...[]byte) error {
	// 计算总长度
	var msgLen uint32
	for i := 0; i < len(args); i++ {
		msgLen += uint32(len(args[i]))
	}

	// 检查长度
	if msgLen > p.maxMsgLen {
		return errors.New("message too long")
	} else if msgLen < p.minMsgLen {
		return errors.New("message too short")
	}

	msg := make([]byte, uint32(p.lenMsgLen)+msgLen)

	// 写入长度字段
	switch p.lenMsgLen {
	case 1:
		msg[0] = byte(msgLen)
	case 2:
		if p.littleEndian {
			binary.LittleEndian.PutUint16(msg, uint16(msgLen))
		} else {
			binary.BigEndian.PutUint16(msg, uint16(msgLen))
		}
	case 4:
		if p.littleEndian {
			binary.LittleEndian.PutUint32(msg, msgLen)
		} else {
			binary.BigEndian.PutUint32(msg, msgLen)
		}
	}

	// 写入数据
	l := p.lenMsgLen
	for i := 0; i < len(args); i++ {
		copy(msg[l:], args[i])
		l += len(args[i])
	}

	conn.Write(msg)
	return nil
}
