package pubsub

import "engine/actor"

// SubscribeRequest 订阅请求
type SubscribeRequest struct {
	Topic      string
	Subscriber *actor.PID
}

// UnsubscribeRequest 取消订阅请求
type UnsubscribeRequest struct {
	Topic      string
	Subscriber *actor.PID
}

// PublishRequest 发布消息请求
type PublishRequest struct {
	Topic   string
	Message interface{}
}

// SubscribeResponse 订阅响应
type SubscribeResponse struct {
	Success bool
}
