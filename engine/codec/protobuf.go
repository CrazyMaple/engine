package codec

import (
	"encoding/binary"
	"errors"
	"fmt"
	"reflect"

	"google.golang.org/protobuf/proto"
)

// ProtobufCodec Protobuf 编解码器
// 消息格式：| msgID (2字节, big-endian) | protobuf payload |
type ProtobufCodec struct {
	idToType map[uint16]reflect.Type
	typeToID map[reflect.Type]uint16
}

// NewProtobufCodec 创建 Protobuf 编解码器
func NewProtobufCodec() *ProtobufCodec {
	return &ProtobufCodec{
		idToType: make(map[uint16]reflect.Type),
		typeToID: make(map[reflect.Type]uint16),
	}
}

// Register 注册消息类型与 ID 的映射
// msg 必须是 proto.Message 的指针类型
func (c *ProtobufCodec) Register(msg interface{}, id uint16) {
	if _, ok := msg.(proto.Message); !ok {
		panic("message must implement proto.Message")
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
func (c *ProtobufCodec) Encode(msg interface{}) ([]byte, error) {
	pbMsg, ok := msg.(proto.Message)
	if !ok {
		return nil, errors.New("message does not implement proto.Message")
	}

	msgType := reflect.TypeOf(msg)
	id, exists := c.typeToID[msgType]
	if !exists {
		return nil, fmt.Errorf("message type %s not registered", msgType)
	}

	payload, err := proto.Marshal(pbMsg)
	if err != nil {
		return nil, err
	}

	data := make([]byte, 2+len(payload))
	binary.BigEndian.PutUint16(data[:2], id)
	copy(data[2:], payload)

	return data, nil
}

// Decode 解码消息
func (c *ProtobufCodec) Decode(data []byte) (interface{}, error) {
	if len(data) < 2 {
		return nil, errors.New("data too short")
	}

	id := binary.BigEndian.Uint16(data[:2])

	msgType, exists := c.idToType[id]
	if !exists {
		return nil, fmt.Errorf("message id %d not registered", id)
	}

	msg := reflect.New(msgType.Elem()).Interface().(proto.Message)
	if err := proto.Unmarshal(data[2:], msg); err != nil {
		return nil, err
	}

	return msg, nil
}
