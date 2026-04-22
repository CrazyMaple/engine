package replay

import (
	"encoding/binary"
	"errors"
	"io"
)

const (
	replayMagic   = 0x52504C59 // "RPLY"
	replayVersion = 1
)

// ReplayEvent 回放事件
type ReplayEvent struct {
	Timestamp int64  // UnixNano 时间戳
	Type      uint16 // 事件类型（业务层定义）
	Data      []byte // 事件数据
}

// ReplayData 完整回放数据
type ReplayData struct {
	Version   uint16
	RoomID    string
	StartTime int64 // UnixNano
	Duration  int64 // 纳秒
	Events    []ReplayEvent
}

// Encode 将回放数据编码为紧凑二进制格式
// 格式:
//
//	| magic(4) | version(2) | roomID_len(2) | roomID | startTime(8) | duration(8) |
//	| event_count(4) |
//	| delta_ts(varint) | type(2) | data_len(varint) | data | ...
func Encode(data *ReplayData) ([]byte, error) {
	if data == nil {
		return nil, errors.New("nil replay data")
	}

	// 估算缓冲区大小
	size := 4 + 2 + 2 + len(data.RoomID) + 8 + 8 + 4
	for _, e := range data.Events {
		size += 10 + 2 + 10 + len(e.Data) // varint 最大 10 字节
	}
	buf := make([]byte, 0, size)

	// Header
	buf = appendUint32(buf, replayMagic)
	buf = appendUint16(buf, data.Version)
	buf = appendUint16(buf, uint16(len(data.RoomID)))
	buf = append(buf, data.RoomID...)
	buf = appendInt64(buf, data.StartTime)
	buf = appendInt64(buf, data.Duration)

	// Event count
	buf = appendUint32(buf, uint32(len(data.Events)))

	// Events（时间戳使用增量编码）
	var prevTs int64
	if len(data.Events) > 0 {
		prevTs = data.StartTime
	}
	for _, e := range data.Events {
		deltaTs := e.Timestamp - prevTs
		prevTs = e.Timestamp

		buf = appendVarint(buf, deltaTs)
		buf = appendUint16(buf, e.Type)
		buf = appendVarint(buf, int64(len(e.Data)))
		buf = append(buf, e.Data...)
	}

	return buf, nil
}

// Decode 从二进制数据解码回放
func Decode(raw []byte) (*ReplayData, error) {
	if len(raw) < 28 { // 最小 header 大小
		return nil, errors.New("data too short")
	}

	r := &reader{data: raw}

	// Header
	magic, err := r.readUint32()
	if err != nil {
		return nil, err
	}
	if magic != replayMagic {
		return nil, errors.New("invalid replay magic")
	}

	version, err := r.readUint16()
	if err != nil {
		return nil, err
	}

	roomIDLen, err := r.readUint16()
	if err != nil {
		return nil, err
	}
	roomID, err := r.readBytes(int(roomIDLen))
	if err != nil {
		return nil, err
	}

	startTime, err := r.readInt64()
	if err != nil {
		return nil, err
	}
	duration, err := r.readInt64()
	if err != nil {
		return nil, err
	}

	eventCount, err := r.readUint32()
	if err != nil {
		return nil, err
	}

	// Events
	events := make([]ReplayEvent, 0, eventCount)
	prevTs := startTime
	for i := uint32(0); i < eventCount; i++ {
		deltaTs, err := r.readVarint()
		if err != nil {
			return nil, err
		}
		ts := prevTs + deltaTs
		prevTs = ts

		evType, err := r.readUint16()
		if err != nil {
			return nil, err
		}

		dataLen, err := r.readVarint()
		if err != nil {
			return nil, err
		}
		evData, err := r.readBytes(int(dataLen))
		if err != nil {
			return nil, err
		}

		events = append(events, ReplayEvent{
			Timestamp: ts,
			Type:      evType,
			Data:      evData,
		})
	}

	return &ReplayData{
		Version:   version,
		RoomID:    string(roomID),
		StartTime: startTime,
		Duration:  duration,
		Events:    events,
	}, nil
}

// --- 编码辅助 ---

func appendUint16(buf []byte, v uint16) []byte {
	return append(buf, byte(v>>8), byte(v))
}

func appendUint32(buf []byte, v uint32) []byte {
	return append(buf, byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}

func appendInt64(buf []byte, v int64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(v))
	return append(buf, b...)
}

func appendVarint(buf []byte, v int64) []byte {
	b := make([]byte, binary.MaxVarintLen64)
	n := binary.PutVarint(b, v)
	return append(buf, b[:n]...)
}

// --- 解码辅助 ---

type reader struct {
	data []byte
	pos  int
}

func (r *reader) readBytes(n int) ([]byte, error) {
	if r.pos+n > len(r.data) {
		return nil, io.ErrUnexpectedEOF
	}
	b := make([]byte, n)
	copy(b, r.data[r.pos:r.pos+n])
	r.pos += n
	return b, nil
}

func (r *reader) readUint16() (uint16, error) {
	if r.pos+2 > len(r.data) {
		return 0, io.ErrUnexpectedEOF
	}
	v := uint16(r.data[r.pos])<<8 | uint16(r.data[r.pos+1])
	r.pos += 2
	return v, nil
}

func (r *reader) readUint32() (uint32, error) {
	if r.pos+4 > len(r.data) {
		return 0, io.ErrUnexpectedEOF
	}
	v := uint32(r.data[r.pos])<<24 | uint32(r.data[r.pos+1])<<16 |
		uint32(r.data[r.pos+2])<<8 | uint32(r.data[r.pos+3])
	r.pos += 4
	return v, nil
}

func (r *reader) readInt64() (int64, error) {
	if r.pos+8 > len(r.data) {
		return 0, io.ErrUnexpectedEOF
	}
	v := binary.BigEndian.Uint64(r.data[r.pos : r.pos+8])
	r.pos += 8
	return int64(v), nil
}

func (r *reader) readVarint() (int64, error) {
	v, n := binary.Varint(r.data[r.pos:])
	if n <= 0 {
		return 0, io.ErrUnexpectedEOF
	}
	r.pos += n
	return v, nil
}
