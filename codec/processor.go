package codec

import (
	"fmt"
	"reflect"
)

// SimpleProcessor 简单消息处理器
type SimpleProcessor struct {
	codec       Codec
	msgHandlers map[reflect.Type]interface{}
}

// NewSimpleProcessor 创建简单处理器
func NewSimpleProcessor(codec Codec) *SimpleProcessor {
	return &SimpleProcessor{
		codec:       codec,
		msgHandlers: make(map[reflect.Type]interface{}),
	}
}

// Register 注册消息处理器
func (p *SimpleProcessor) Register(msg interface{}, handler interface{}) {
	msgType := reflect.TypeOf(msg)
	if msgType == nil || msgType.Kind() != reflect.Ptr {
		panic("message must be pointer type")
	}

	handlerType := reflect.TypeOf(handler)
	if handlerType.Kind() != reflect.Func {
		panic("handler must be function")
	}

	p.msgHandlers[msgType] = handler
}

// Unmarshal 解码消息
func (p *SimpleProcessor) Unmarshal(data []byte) (interface{}, error) {
	return p.codec.Decode(data)
}

// Marshal 编码消息
func (p *SimpleProcessor) Marshal(msg interface{}) ([][]byte, error) {
	data, err := p.codec.Encode(msg)
	if err != nil {
		return nil, err
	}
	return [][]byte{data}, nil
}

// Route 路由消息
func (p *SimpleProcessor) Route(msg interface{}, agent interface{}) error {
	msgType := reflect.TypeOf(msg)
	handler, ok := p.msgHandlers[msgType]
	if !ok {
		return fmt.Errorf("handler not found for message type: %s", msgType.String())
	}

	handlerValue := reflect.ValueOf(handler)
	handlerValue.Call([]reflect.Value{
		reflect.ValueOf(msg),
		reflect.ValueOf(agent),
	})

	return nil
}
