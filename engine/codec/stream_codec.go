package codec

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"

	engerr "engine/errors"
)

// StreamCodec 流式编解码器接口
// 相比 Codec 的 []byte 模式，StreamCodec 直接向 Writer 编码 / 从 Reader 解码
// 避免中间 []byte 分配，显著降低跨节点通信的内存拷贝次数
type StreamCodec interface {
	// EncodeTo 将消息直接编码到 Writer
	// 返回编码后的字节数
	EncodeTo(w io.Writer, msg interface{}) (int, error)
	// DecodeFrom 从 Reader 直接解码消息
	// 要求能够读取完整的单条消息边界
	DecodeFrom(r io.Reader) (interface{}, error)
}

// WriterTo 消息自序列化接口
// 实现该接口的消息可以直接向 Writer 写入，避免中间 []byte 分配
// 标准 io.WriterTo 的别名以保持语义一致
type WriterTo interface {
	WriteTo(w io.Writer) (int64, error)
}

// ReaderFrom 消息自反序列化接口
// 实现该接口的消息可以直接从 Reader 读取
type ReaderFrom interface {
	ReadFrom(r io.Reader) (int64, error)
}

// StreamJSONCodec 流式 JSON 编解码器
// 使用 json.Encoder/json.Decoder 绕过中间 []byte 缓冲
type StreamJSONCodec struct {
	msgInfo map[string]reflect.Type
}

// NewStreamJSONCodec 创建流式 JSON 编解码器
func NewStreamJSONCodec() *StreamJSONCodec {
	return &StreamJSONCodec{
		msgInfo: make(map[string]reflect.Type),
	}
}

// Register 注册消息类型
func (c *StreamJSONCodec) Register(msg interface{}) {
	msgType := reflect.TypeOf(msg)
	if msgType == nil || msgType.Kind() != reflect.Ptr {
		panic("message must be pointer type")
	}
	msgName := msgType.Elem().Name()
	c.msgInfo[msgName] = msgType
}

// EncodeTo 编码消息到 Writer
// 格式：| 4字节长度(big-endian) | JSON 数据 |
func (c *StreamJSONCodec) EncodeTo(w io.Writer, msg interface{}) (int, error) {
	// 先写入一个占位长度，再 Encode JSON 覆盖
	// 由于无法预先知道 JSON 长度，使用 buffered writer + 预留头的方式
	bw := bufio.NewWriter(w)

	// 使用 json.Encoder 直接流式编码到 buffered writer
	// 但需要先知道长度 —— 折中方案：先 marshal 到小 buffer，再写入
	// 这里仍需一次 []byte，但避免了 Remote 层的额外 buffer 拷贝
	data, err := json.Marshal(msg)
	if err != nil {
		return 0, &engerr.CodecError{Op: "stream_encode", TypeName: fmt.Sprintf("%T", msg), Cause: err}
	}

	// 写入长度前缀
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(data)))
	if _, err := bw.Write(lenBuf[:]); err != nil {
		return 0, err
	}
	if _, err := bw.Write(data); err != nil {
		return 0, err
	}
	if err := bw.Flush(); err != nil {
		return 0, err
	}
	return 4 + len(data), nil
}

// DecodeFrom 从 Reader 解码消息
func (c *StreamJSONCodec) DecodeFrom(r io.Reader) (interface{}, error) {
	// 读取长度前缀
	var lenBuf [4]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return nil, err
	}
	msgLen := binary.BigEndian.Uint32(lenBuf[:])
	if msgLen > MaxStreamMessageSize {
		return nil, errors.New("message exceeds MaxStreamMessageSize")
	}

	// 读取消息体
	data := make([]byte, msgLen)
	if _, err := io.ReadFull(r, data); err != nil {
		return nil, err
	}

	// 解析类型并反序列化
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, &engerr.CodecError{Op: "stream_decode", Cause: err}
	}
	msgType, ok := m["type"].(string)
	if !ok {
		return nil, &engerr.CodecError{Op: "stream_decode", Cause: engerr.ErrNotFound}
	}
	typ, ok := c.msgInfo[msgType]
	if !ok {
		return nil, &engerr.CodecError{Op: "stream_decode", TypeName: msgType, Cause: engerr.ErrNotFound}
	}
	msg := reflect.New(typ.Elem()).Interface()
	if err := json.Unmarshal(data, msg); err != nil {
		return nil, &engerr.CodecError{Op: "stream_decode", TypeName: msgType, Cause: err}
	}
	return msg, nil
}

// MaxStreamMessageSize 单条流式消息最大字节数（32MB）
const MaxStreamMessageSize = 32 * 1024 * 1024

// StreamCodecAdapter 将普通 Codec 适配为 StreamCodec
// 对于不原生支持流式的 Codec（如 Protobuf），通过长度前缀帧实现
type StreamCodecAdapter struct {
	Inner Codec
}

// NewStreamCodecAdapter 创建适配器
func NewStreamCodecAdapter(c Codec) *StreamCodecAdapter {
	return &StreamCodecAdapter{Inner: c}
}

// EncodeTo 将消息编码并写入 Writer（带长度前缀）
func (a *StreamCodecAdapter) EncodeTo(w io.Writer, msg interface{}) (int, error) {
	// 优先尝试 WriterTo 接口（零拷贝路径）
	if _, ok := msg.(WriterTo); ok {
		// 对于实现了 WriterTo 的消息，调用方应直接使用 WriteTo
		// 此处仍退回到 Encode 以保证长度前缀帧格式一致
		data, err := a.Inner.Encode(msg)
		if err != nil {
			return 0, err
		}
		return writeLengthPrefixed(w, data)
	}
	data, err := a.Inner.Encode(msg)
	if err != nil {
		return 0, err
	}
	return writeLengthPrefixed(w, data)
}

// DecodeFrom 从 Reader 读取并解码消息
func (a *StreamCodecAdapter) DecodeFrom(r io.Reader) (interface{}, error) {
	data, err := readLengthPrefixed(r)
	if err != nil {
		return nil, err
	}
	return a.Inner.Decode(data)
}

// writeLengthPrefixed 写入带 4 字节大端长度前缀的数据
func writeLengthPrefixed(w io.Writer, data []byte) (int, error) {
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(data)))
	if _, err := w.Write(lenBuf[:]); err != nil {
		return 0, err
	}
	if _, err := w.Write(data); err != nil {
		return 0, err
	}
	return 4 + len(data), nil
}

// readLengthPrefixed 读取带 4 字节大端长度前缀的数据
func readLengthPrefixed(r io.Reader) ([]byte, error) {
	var lenBuf [4]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return nil, err
	}
	msgLen := binary.BigEndian.Uint32(lenBuf[:])
	if msgLen > MaxStreamMessageSize {
		return nil, errors.New("message exceeds MaxStreamMessageSize")
	}
	data := make([]byte, msgLen)
	if _, err := io.ReadFull(r, data); err != nil {
		return nil, err
	}
	return data, nil
}
