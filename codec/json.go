package codec

import (
	"encoding/json"
	"reflect"

	engerr "engine/errors"
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
		return nil, &engerr.CodecError{Op: "decode", Cause: engerr.ErrNotFound}
	}

	typ, ok := c.msgInfo[msgType]
	if !ok {
		return nil, &engerr.CodecError{Op: "decode", TypeName: msgType, Cause: engerr.ErrNotFound}
	}

	msg := reflect.New(typ.Elem()).Interface()
	if err = json.Unmarshal(data, msg); err != nil {
		return nil, &engerr.CodecError{Op: "decode", TypeName: msgType, Cause: err}
	}
	return msg, nil
}
