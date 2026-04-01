package pubsub

import (
	"time"

	"engine/actor"
	"engine/cluster"
	"engine/grain"
	"engine/log"
	"engine/remote"
)

// TopicKindName PubSub 使用的内部 Grain Kind 名称
const TopicKindName = "$topic"

// PubSub 跨节点发布订阅管理器
type PubSub struct {
	cluster     *cluster.Cluster
	grainClient *grain.GrainClient
	started     bool
}

// NewPubSub 创建 PubSub 管理器
func NewPubSub(c *cluster.Cluster, grainClient *grain.GrainClient) *PubSub {
	return &PubSub{
		cluster:     c,
		grainClient: grainClient,
	}
}

// Start 启动 PubSub，注册 Topic Kind
func (ps *PubSub) Start(kinds *grain.KindRegistry) {
	if ps.started {
		return
	}

	// 注册消息类型
	remote.RegisterType(&SubscribeRequest{})
	remote.RegisterType(&UnsubscribeRequest{})
	remote.RegisterType(&PublishRequest{})
	remote.RegisterType(&SubscribeResponse{})

	// 注册 Topic 为一个 Grain Kind
	topicKind := grain.NewKind(TopicKindName, newTopicActor).
		WithTTL(5 * time.Minute)
	kinds.Register(topicKind)

	ps.started = true
	log.Info("PubSub started")
}

// Stop 停止 PubSub
func (ps *PubSub) Stop() {
	ps.started = false
	log.Info("PubSub stopped")
}

// Subscribe 订阅 topic
func (ps *PubSub) Subscribe(topic string, subscriber *actor.PID) error {
	_, err := ps.grainClient.Request(TopicKindName, topic, &SubscribeRequest{
		Topic:      topic,
		Subscriber: subscriber,
	}, 5*time.Second)
	return err
}

// Unsubscribe 取消订阅
func (ps *PubSub) Unsubscribe(topic string, subscriber *actor.PID) error {
	_, err := ps.grainClient.Request(TopicKindName, topic, &UnsubscribeRequest{
		Topic:      topic,
		Subscriber: subscriber,
	}, 5*time.Second)
	return err
}

// Publish 向 topic 发布消息
func (ps *PubSub) Publish(topic string, message interface{}) error {
	return ps.grainClient.Send(TopicKindName, topic, &PublishRequest{
		Topic:   topic,
		Message: message,
	})
}
