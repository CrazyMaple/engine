package codec

import (
	"encoding/json"
	"errors"
	"reflect"
)

// JSONCodec JSON编解码器
type JSONCodec struct {
	msgInfo map[string]reflect.Type
}

// NewJSONCodec 创建JSON编解码器
func NewJSONCodec() *JSONCodec {
	return &JSONCodec{
		msgInfo: make(map[string]reflect.Type),
	}
}

// Register 注册消息类型
func (c *JSONCodec) Register(msg interface{}) {
	msgType := reflect.TypeOf(msg)
	if msgType == nil || msgType.Kind() != reflect.Ptr {
		panic("message must be pointer type")
	}
	msgName := msgType.Elem().Name()
	c.msgInfo[msgName] = msgType
}

// Encode 编码消息
func (c *JSONCodec) Encode(msg interface{}) ([]byte, error) {
	return json.Marshal(msg)
}

// Decode 解码消息
func (c *JSONCodec) Decode(data []byte) (interface{}, error) {
	var m map[string]interface{}
	err := json.Unmarshal(data, &m)
	if err != nil {
		return nil, err
	}

	msgType, ok := m["type"].(string)
	if !ok {
		return nil, errors.New("message type not found")
	}

	typ, ok := c.msgInfo[msgType]
	if !ok {
		return nil, errors.New("message type not registered: " + msgType)
	}

	msg := reflect.New(typ.Elem()).Interface()
	err = json.Unmarshal(data, msg)
	return msg, err
}
