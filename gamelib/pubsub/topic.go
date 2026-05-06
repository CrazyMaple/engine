package pubsub

import (
	"engine/actor"
	"engine/grain"
	"engine/log"
)

// topicActor 主题 Actor，管理一个 topic 的订阅者
// 作为 Grain 运行，自动分布在集群中
type topicActor struct {
	topic       string
	subscribers map[string]*actor.PID // pid.String() -> PID
}

func newTopicActor() actor.Actor {
	return &topicActor{
		subscribers: make(map[string]*actor.PID),
	}
}

func (t *topicActor) Receive(ctx actor.Context) {
	switch msg := ctx.Message().(type) {
	case *actor.Started:
		// 初始化

	case *grain.GrainInit:
		t.topic = msg.Identity.Identity
		log.Debug("TopicActor started: %s", t.topic)

	case *SubscribeRequest:
		key := msg.Subscriber.String()
		t.subscribers[key] = msg.Subscriber
		log.Debug("TopicActor %s: subscriber added %s, total: %d", t.topic, key, len(t.subscribers))
		ctx.Respond(&SubscribeResponse{Success: true})

	case *UnsubscribeRequest:
		key := msg.Subscriber.String()
		delete(t.subscribers, key)
		log.Debug("TopicActor %s: subscriber removed %s, total: %d", t.topic, key, len(t.subscribers))
		ctx.Respond(&SubscribeResponse{Success: true})

	case *PublishRequest:
		// 广播给所有订阅者
		for _, sub := range t.subscribers {
			ctx.Send(sub, msg.Message)
		}

	case *actor.ReceiveTimeout:
		// 没有订阅者时自动去激活
		if len(t.subscribers) == 0 {
			log.Debug("TopicActor %s: no subscribers, deactivating", t.topic)
			ctx.StopActor(ctx.Self())
		}

	case *actor.Terminated:
		// 被 Watch 的订阅者停止了
		// 当前简化实现：不 Watch 订阅者，由发布时自然发现无效 PID
	}
}
