//go:build ignore

package main

import (
	"fmt"
	"time"

	"engine/actor"
	"engine/pubsub"
)

// --- PubSub 发布订阅示例 ---
// 演示如何使用 PubSub 模块实现 Topic 消息广播

// ChatMsg 聊天消息
type ChatMsg struct {
	Channel string
	Sender  string
	Text    string
}

// SystemNotice 系统通知
type SystemNotice struct {
	Text string
}

// subscriber 订阅者 Actor
type subscriber struct {
	name string
}

func (s *subscriber) Receive(ctx actor.Context) {
	switch msg := ctx.Message().(type) {
	case *actor.Started:
		fmt.Printf("[%s] 已上线\n", s.name)
	case *ChatMsg:
		fmt.Printf("[%s] 收到 [%s] 频道消息 - %s: %s\n",
			s.name, msg.Channel, msg.Sender, msg.Text)
	case *SystemNotice:
		fmt.Printf("[%s] 系统通知: %s\n", s.name, msg.Text)
	}
}

func main() {
	fmt.Println("=== PubSub 发布订阅示例 ===")

	system := actor.NewActorSystem()
	ps := pubsub.NewPubSub(system)

	// 创建订阅者
	alice := system.Root.SpawnNamed(
		actor.PropsFromProducer(func() actor.Actor { return &subscriber{name: "Alice"} }),
		"alice",
	)
	bob := system.Root.SpawnNamed(
		actor.PropsFromProducer(func() actor.Actor { return &subscriber{name: "Bob"} }),
		"bob",
	)
	charlie := system.Root.SpawnNamed(
		actor.PropsFromProducer(func() actor.Actor { return &subscriber{name: "Charlie"} }),
		"charlie",
	)
	time.Sleep(50 * time.Millisecond)

	// 订阅 Topic
	fmt.Println("\n--- 订阅频道 ---")
	ps.Subscribe("world-chat", alice)   // Alice 订阅世界频道
	ps.Subscribe("world-chat", bob)     // Bob 订阅世界频道
	ps.Subscribe("world-chat", charlie) // Charlie 订阅世界频道
	ps.Subscribe("guild-chat", alice)   // Alice 也订阅公会频道
	ps.Subscribe("guild-chat", bob)     // Bob 也订阅公会频道
	ps.Subscribe("system", alice)       // 三人都订阅系统通知
	ps.Subscribe("system", bob)
	ps.Subscribe("system", charlie)

	fmt.Printf("world-chat 订阅者: %d\n", ps.SubscriberCount("world-chat"))
	fmt.Printf("guild-chat 订阅者: %d\n", ps.SubscriberCount("guild-chat"))
	fmt.Printf("system 订阅者: %d\n", ps.SubscriberCount("system"))

	// 发布消息
	fmt.Println("\n--- 世界频道消息 ---")
	ps.Publish("world-chat", &ChatMsg{
		Channel: "world-chat",
		Sender:  "Alice",
		Text:    "大家好！",
	})
	time.Sleep(50 * time.Millisecond)

	fmt.Println("\n--- 公会频道消息 ---")
	ps.Publish("guild-chat", &ChatMsg{
		Channel: "guild-chat",
		Sender:  "Bob",
		Text:    "今晚团本有人吗？",
	})
	time.Sleep(50 * time.Millisecond)

	fmt.Println("\n--- 系统通知 ---")
	ps.Publish("system", &SystemNotice{Text: "服务器将于 10 分钟后维护"})
	time.Sleep(50 * time.Millisecond)

	// 取消订阅
	fmt.Println("\n--- Charlie 退出世界频道 ---")
	ps.Unsubscribe("world-chat", charlie)
	ps.Publish("world-chat", &ChatMsg{
		Channel: "world-chat",
		Sender:  "Alice",
		Text:    "Charlie 已离开",
	})
	time.Sleep(50 * time.Millisecond)

	fmt.Println("\n=== 示例结束 ===")
}
