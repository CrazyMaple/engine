package remote

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"sync"

	"engine/actor"
	"engine/codec"
	engerr "engine/errors"
)

// ZeroCopyCodec 零拷贝远程编解码器
// 在 RemoteCodec 基础上增加流式编解码能力，避免跨节点传输的多次内存拷贝
// 如果内部 Codec 实现了 StreamCodec，则直接使用；否则走适配器
type ZeroCopyCodec struct {
	remoteCodec *RemoteCodec
	stream      codec.StreamCodec // 流式编解码器（可能通过适配器构造）
}

// NewZeroCopyCodec 创建零拷贝编解码器
func NewZeroCopyCodec(rc *RemoteCodec) *ZeroCopyCodec {
	var stream codec.StreamCodec
	inner := rc.InnerCodec()
	if sc, ok := inner.(codec.StreamCodec); ok {
		stream = sc
	} else if inner != nil {
		stream = codec.NewStreamCodecAdapter(inner)
	} else {
		stream = codec.NewStreamJSONCodec()
	}
	return &ZeroCopyCodec{
		remoteCodec: rc,
		stream:      stream,
	}
}

// EncodeEnvelopeTo 直接将 RemoteMessage 编码到 Writer
// 零拷贝路径：优先使用 WriterTo 接口
func (zc *ZeroCopyCodec) EncodeEnvelopeTo(w io.Writer, msg interface{}) (int, error) {
	// 快路径：消息自身实现 WriterTo 接口
	if wt, ok := msg.(codec.WriterTo); ok {
		n, err := wt.WriteTo(w)
		if err != nil {
			return int(n), &engerr.CodecError{Op: "zc_encode", TypeName: fmt.Sprintf("%T", msg), Cause: err}
		}
		return int(n), nil
	}
	// 回退路径：通过 StreamCodec 编码
	return zc.stream.EncodeTo(w, msg)
}

// DecodeEnvelopeFrom 从 Reader 解码 RemoteMessage
func (zc *ZeroCopyCodec) DecodeEnvelopeFrom(r io.Reader) (interface{}, error) {
	return zc.stream.DecodeFrom(r)
}

// MarshalEnvelopeToBuffer 使用预分配的 buffer 序列化消息
// 避免每次编码都新建 []byte，与现有的 bufferPool 配合使用
func (zc *ZeroCopyCodec) MarshalEnvelopeToBuffer(msg interface{}) (*bytes.Buffer, error) {
	buf := bufferPoolZC.Get().(*bytes.Buffer)
	buf.Reset()

	if _, err := zc.EncodeEnvelopeTo(buf, msg); err != nil {
		bufferPoolZC.Put(buf)
		return nil, err
	}
	return buf, nil
}

// ReleaseBuffer 归还 buffer 到池
func (zc *ZeroCopyCodec) ReleaseBuffer(buf *bytes.Buffer) {
	if buf == nil {
		return
	}
	if buf.Cap() > 1<<20 { // >1MB 不回收避免占用内存
		return
	}
	bufferPoolZC.Put(buf)
}

// bufferPoolZC 零拷贝编码专用 buffer 池
var bufferPoolZC = sync.Pool{
	New: func() interface{} {
		return bytes.NewBuffer(make([]byte, 0, 4096))
	},
}

// ---- 快路径：RemoteMessage WriterTo 实现 ----

// writeVarint 写入变长整数
func writeVarint(w io.Writer, v uint64) (int, error) {
	var buf [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(buf[:], v)
	return w.Write(buf[:n])
}

// writeVarString 写入变长字符串（长度前缀 + 数据）
func writeVarString(w io.Writer, s string) (int, error) {
	n1, err := writeVarint(w, uint64(len(s)))
	if err != nil {
		return n1, err
	}
	n2, err := io.WriteString(w, s)
	return n1 + n2, err
}

// readVarint 读取变长整数
func readVarint(r io.Reader) (uint64, error) {
	br, ok := r.(io.ByteReader)
	if !ok {
		br = &byteReader{r: r}
	}
	return binary.ReadUvarint(br)
}

// readVarString 读取变长字符串
func readVarString(r io.Reader) (string, error) {
	n, err := readVarint(r)
	if err != nil {
		return "", err
	}
	if n > uint64(codec.MaxStreamMessageSize) {
		return "", fmt.Errorf("string length too large: %d", n)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", err
	}
	return string(buf), nil
}

// byteReader 单字节 Reader 适配器
type byteReader struct {
	r   io.Reader
	buf [1]byte
}

func (b *byteReader) ReadByte() (byte, error) {
	if _, err := io.ReadFull(b.r, b.buf[:]); err != nil {
		return 0, err
	}
	return b.buf[0], nil
}

// ---- RemoteMessage 零拷贝编码（紧凑二进制协议） ----

// WriteTo 将 RemoteMessage 直接编码到 Writer
// 二进制格式：
//
//	| type (1) | target_addr (varstr) | target_id (varstr)
//	| sender_addr (varstr) | sender_id (varstr) | typename (varstr)
//	| payload_len (varint) | payload (bytes) |
func (rm *RemoteMessage) WriteTo(w io.Writer) (int64, error) {
	var total int64

	// type
	typeByte := [1]byte{byte(rm.Type)}
	n, err := w.Write(typeByte[:])
	total += int64(n)
	if err != nil {
		return total, err
	}

	// target
	if rm.Target == nil {
		n, _ := writeVarString(w, "")
		total += int64(n)
		n, _ = writeVarString(w, "")
		total += int64(n)
	} else {
		n, err := writeVarString(w, rm.Target.Address)
		total += int64(n)
		if err != nil {
			return total, err
		}
		n, err = writeVarString(w, rm.Target.Id)
		total += int64(n)
		if err != nil {
			return total, err
		}
	}

	// sender
	if rm.Sender == nil {
		n, _ := writeVarString(w, "")
		total += int64(n)
		n, _ = writeVarString(w, "")
		total += int64(n)
	} else {
		n, err := writeVarString(w, rm.Sender.Address)
		total += int64(n)
		if err != nil {
			return total, err
		}
		n, err = writeVarString(w, rm.Sender.Id)
		total += int64(n)
		if err != nil {
			return total, err
		}
	}

	// typename
	n, err = writeVarString(w, rm.TypeName)
	total += int64(n)
	if err != nil {
		return total, err
	}

	// payload（委托给外部编解码器序列化 Message 字段）
	// 这里采用一种简化策略：仅支持已编码为 []byte 的 payload
	if payload, ok := rm.Message.([]byte); ok {
		n, err := writeVarint(w, uint64(len(payload)))
		total += int64(n)
		if err != nil {
			return total, err
		}
		n, err = w.Write(payload)
		total += int64(n)
		return total, err
	}

	// 非 []byte 类型：写入 0 长度表示需要外部 codec 解码
	n, err = writeVarint(w, 0)
	total += int64(n)
	return total, err
}

// ReadRemoteMessageFrom 从 Reader 读取并重建 RemoteMessage
func ReadRemoteMessageFrom(r io.Reader) (*RemoteMessage, error) {
	// type
	var typeByte [1]byte
	if _, err := io.ReadFull(r, typeByte[:]); err != nil {
		return nil, err
	}

	// target
	targetAddr, err := readVarString(r)
	if err != nil {
		return nil, err
	}
	targetID, err := readVarString(r)
	if err != nil {
		return nil, err
	}

	// sender
	senderAddr, err := readVarString(r)
	if err != nil {
		return nil, err
	}
	senderID, err := readVarString(r)
	if err != nil {
		return nil, err
	}

	// typename
	typeName, err := readVarString(r)
	if err != nil {
		return nil, err
	}

	// payload
	payloadLen, err := readVarint(r)
	if err != nil {
		return nil, err
	}

	var payload []byte
	if payloadLen > 0 {
		if payloadLen > uint64(codec.MaxStreamMessageSize) {
			return nil, fmt.Errorf("payload too large: %d", payloadLen)
		}
		payload = make([]byte, payloadLen)
		if _, err := io.ReadFull(r, payload); err != nil {
			return nil, err
		}
	}

	msg := &RemoteMessage{
		Type:     MessageType(typeByte[0]),
		TypeName: typeName,
		Message:  payload,
	}
	if targetAddr != "" || targetID != "" {
		msg.Target = actor.NewPID(targetAddr, targetID)
	}
	if senderAddr != "" || senderID != "" {
		msg.Sender = actor.NewPID(senderAddr, senderID)
	}
	return msg, nil
}
