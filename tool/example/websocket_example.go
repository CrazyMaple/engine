//go:build ignore

package main

import (
	"encoding/json"
	"fmt"
	"time"

	"engine/actor"
	"engine/codec"
	"gamelib/gate"
)

// --- WebSocket Gate 接入示例 ---
// 演示如何使用 Gate 模块通过 WebSocket 接入客户端

// ChatMessage 聊天消息
type ChatMessage struct {
	From    string `json:"from"`
	Content string `json:"content"`
}

// chatHandler 处理聊天消息
func chatHandler(msg interface{}, agent interface{}) {
	a := agent.(*gate.Agent)
	switch m := msg.(type) {
	case *ChatMessage:
		fmt.Printf("[WS] 收到来自 %s 的消息: %s\n", a.RemoteAddr(), m.Content)
		// 回复消息
		reply := &ChatMessage{
			From:    "server",
			Content: "收到: " + m.Content,
		}
		if err := a.WriteMsg(reply); err != nil {
			fmt.Printf("[WS] 发送失败: %v\n", err)
		}
	}
}

func main() {
	fmt.Println("=== WebSocket Gate 示例 ===")

	system := actor.NewActorSystem()

	// 创建 JSON 编解码器和消息处理器
	jsonCodec := codec.NewJSONCodec()
	jsonCodec.Register(&ChatMessage{})

	processor := codec.NewSimpleProcessor(jsonCodec)
	processor.Register(&ChatMessage{}, chatHandler)

	// 创建 Gate（同时支持 TCP 和 WebSocket）
	g := gate.NewGate(system)
	g.TCPAddr = "127.0.0.1:8801"       // TCP 接入
	g.WSAddr = "127.0.0.1:8802"        // WebSocket 接入
	g.MaxConnNum = 1000
	g.MaxMsgLen = 4096
	g.Processor = processor

	g.Start()
	fmt.Println("Gate 已启动:")
	fmt.Println("  TCP:       127.0.0.1:8801")
	fmt.Println("  WebSocket: ws://127.0.0.1:8802")
	fmt.Println()
	fmt.Println("可以使用 wscat 测试:")
	fmt.Println("  wscat -c ws://127.0.0.1:8802")
	fmt.Printf("  发送: %s\n", mustJSON(&ChatMessage{From: "test", Content: "hello"}))

	// 保持运行
	time.Sleep(60 * time.Second)
	g.Close()
}

func mustJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}
