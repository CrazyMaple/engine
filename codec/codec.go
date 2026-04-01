package codec

// Codec 编解码器接口
type Codec interface {
	Encode(msg interface{}) ([]byte, error)
	Decode(data []byte) (interface{}, error)
}

// MessageRouter 消息路由器
type MessageRouter interface {
	Register(msgType interface{}, handler interface{})
	Route(msg interface{}, agent interface{}) error
}
