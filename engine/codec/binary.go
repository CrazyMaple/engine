package codec

import (
	"encoding"
	"encoding/binary"
	"errors"
	"fmt"
	"reflect"
)

// BinaryCodec 基于 encoding.BinaryMarshaler/BinaryUnmarshaler 的编解码器
// 消息格式：| msgID (2字节, big-endian) | binary payload |
// 适用于手写二进制编码的消息（不依赖 protoc 代码生成）
type BinaryCodec struct {
	idToType map[uint16]reflect.Type
	typeToID map[reflect.Type]uint16
}

// NewBinaryCodec 创建二进制编解码器
func NewBinaryCodec() *BinaryCodec {
	return &BinaryCodec{
		idToType: make(map[uint16]reflect.Type),
		typeToID: make(map[reflect.Type]uint16),
	}
}

// Register 注册消息类型与 ID 的映射
// msg 必须实现 encoding.BinaryMarshaler 和 encoding.BinaryUnmarshaler
func (c *BinaryCodec) Register(msg interface{}, id uint16) {
	if _, ok := msg.(encoding.BinaryMarshaler); !ok {
		panic("message must implement encoding.BinaryMarshaler")
	}
	if _, ok := msg.(encoding.BinaryUnmarshaler); !ok {
		panic("message must implement encoding.BinaryUnmarshaler")
	}

	msgType := reflect.TypeOf(msg)
	if msgType == nil || msgType.Kind() != reflect.Ptr {
		panic("message must be pointer type")
	}

	if _, exists := c.idToType[id]; exists {
		panic(fmt.Sprintf("message id %d already registered", id))
	}
	if _, exists := c.typeToID[msgType]; exists {
		panic(fmt.Sprintf("message type %s already registered", msgType))
	}

	c.idToType[id] = msgType
	c.typeToID[msgType] = id
}

// Encode 编码消息
func (c *BinaryCodec) Encode(msg interface{}) ([]byte, error) {
	bm, ok := msg.(encoding.BinaryMarshaler)
	if !ok {
		return nil, errors.New("message does not implement BinaryMarshaler")
	}

	msgType := reflect.TypeOf(msg)
	id, exists := c.typeToID[msgType]
	if !exists {
		return nil, fmt.Errorf("message type %s not registered", msgType)
	}

	payload, err := bm.MarshalBinary()
	if err != nil {
		return nil, err
	}

	data := make([]byte, 2+len(payload))
	binary.BigEndian.PutUint16(data[:2], id)
	copy(data[2:], payload)

	return data, nil
}

// Decode 解码消息
func (c *BinaryCodec) Decode(data []byte) (interface{}, error) {
	if len(data) < 2 {
		return nil, errors.New("data too short")
	}

	id := binary.BigEndian.Uint16(data[:2])

	msgType, exists := c.idToType[id]
	if !exists {
		return nil, fmt.Errorf("message id %d not registered", id)
	}

	msg := reflect.New(msgType.Elem()).Interface()
	bu, ok := msg.(encoding.BinaryUnmarshaler)
	if !ok {
		return nil, fmt.Errorf("message type %s does not implement BinaryUnmarshaler", msgType)
	}

	if err := bu.UnmarshalBinary(data[2:]); err != nil {
		return nil, err
	}

	return msg, nil
}
