package proto

// 轻量级二进制编码工具函数，用于手写 proto 消息的序列化
// 使用 varint + length-prefixed 格式，兼容 Protocol Buffers wire format

import (
	"encoding/binary"
	"fmt"
	"math"
)

// encodeVarint 编码变长整数
func encodeVarint(buf []byte, v uint64) []byte {
	for v >= 0x80 {
		buf = append(buf, byte(v)|0x80)
		v >>= 7
	}
	return append(buf, byte(v))
}

// decodeVarint 解码变长整数，返回值和消耗的字节数
func decodeVarint(data []byte) (uint64, int, error) {
	var v uint64
	for i, b := range data {
		if i >= binary.MaxVarintLen64 {
			return 0, 0, fmt.Errorf("varint too long")
		}
		v |= uint64(b&0x7F) << (7 * uint(i))
		if b < 0x80 {
			return v, i + 1, nil
		}
	}
	return 0, 0, fmt.Errorf("unexpected end of varint")
}

// encodeString 编码字符串：length(varint) + data
func encodeString(buf []byte, s string) []byte {
	buf = encodeVarint(buf, uint64(len(s)))
	return append(buf, s...)
}

// decodeString 解码字符串
func decodeString(data []byte) (string, int, error) {
	length, n, err := decodeVarint(data)
	if err != nil {
		return "", 0, err
	}
	end := n + int(length)
	if end > len(data) {
		return "", 0, fmt.Errorf("string length exceeds data")
	}
	return string(data[n:end]), end, nil
}

// encodeBytes 编码字节切片：length(varint) + data
func encodeBytes(buf []byte, b []byte) []byte {
	buf = encodeVarint(buf, uint64(len(b)))
	return append(buf, b...)
}

// decodeBytes 解码字节切片
func decodeBytes(data []byte) ([]byte, int, error) {
	length, n, err := decodeVarint(data)
	if err != nil {
		return nil, 0, err
	}
	end := n + int(length)
	if end > len(data) {
		return nil, 0, fmt.Errorf("bytes length exceeds data")
	}
	result := make([]byte, length)
	copy(result, data[n:end])
	return result, end, nil
}

// encodeUint64 编码 uint64 为固定 8 字节（小端）
func encodeUint64(buf []byte, v uint64) []byte {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, v)
	return append(buf, b...)
}

// decodeUint64 解码固定 8 字节 uint64
func decodeUint64(data []byte) (uint64, int, error) {
	if len(data) < 8 {
		return 0, 0, fmt.Errorf("insufficient data for uint64")
	}
	return binary.LittleEndian.Uint64(data[:8]), 8, nil
}

// encodeInt32 编码 int32 为 varint
func encodeInt32(buf []byte, v int32) []byte {
	return encodeVarint(buf, uint64(uint32(v)))
}

// decodeInt32 解码 int32
func decodeInt32(data []byte) (int32, int, error) {
	v, n, err := decodeVarint(data)
	if err != nil {
		return 0, 0, err
	}
	if v > math.MaxUint32 {
		return 0, 0, fmt.Errorf("int32 overflow")
	}
	return int32(uint32(v)), n, nil
}

// encodeStringSlice 编码字符串切片：count(varint) + 逐个字符串
func encodeStringSlice(buf []byte, ss []string) []byte {
	buf = encodeVarint(buf, uint64(len(ss)))
	for _, s := range ss {
		buf = encodeString(buf, s)
	}
	return buf
}

// decodeStringSlice 解码字符串切片
func decodeStringSlice(data []byte) ([]string, int, error) {
	count, offset, err := decodeVarint(data)
	if err != nil {
		return nil, 0, err
	}
	result := make([]string, 0, count)
	for i := uint64(0); i < count; i++ {
		s, n, err := decodeString(data[offset:])
		if err != nil {
			return nil, 0, err
		}
		result = append(result, s)
		offset += n
	}
	return result, offset, nil
}
