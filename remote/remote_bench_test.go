package remote

import (
	"encoding/json"
	"testing"

	"engine/actor"
)

// BenchmarkRemoteSerialize 测试远程消息序列化性能
func BenchmarkRemoteSerialize(b *testing.B) {
	msg := &RemoteMessage{
		Target:   actor.NewPID("localhost:8080", "actor1"),
		Sender:   actor.NewPID("localhost:8081", "actor2"),
		Message:  map[string]interface{}{"cmd": "move", "x": 100, "y": 200},
		Type:     MessageTypeUser,
		TypeName: "MoveCommand",
	}

	b.Run("marshal", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, err := json.Marshal(msg)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	data, _ := json.Marshal(msg)
	b.Run("unmarshal", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			var rm RemoteMessage
			if err := json.Unmarshal(data, &rm); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkBatchSerialize 测试批量消息序列化性能
func BenchmarkBatchSerialize(b *testing.B) {
	msgs := make([]*RemoteMessage, 64)
	for i := range msgs {
		msgs[i] = &RemoteMessage{
			Target:   actor.NewPID("localhost:8080", "actor1"),
			Sender:   actor.NewPID("localhost:8081", "actor2"),
			Message:  map[string]interface{}{"id": i},
			Type:     MessageTypeUser,
			TypeName: "TestMsg",
		}
	}

	b.Run("individual", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			for _, msg := range msgs {
				json.Marshal(msg)
			}
		}
	})

	b.Run("batch", func(b *testing.B) {
		batch := &RemoteMessageBatch{Messages: msgs}
		for i := 0; i < b.N; i++ {
			json.Marshal(batch)
		}
	})
}

// BenchmarkBufferPoolInRemote 测试远程层使用缓冲池的性能
func BenchmarkBufferPoolInRemote(b *testing.B) {
	msg := &RemoteMessage{
		Target:   actor.NewPID("localhost:8080", "actor1"),
		Message:  "hello",
		Type:     MessageTypeUser,
		TypeName: "string",
	}

	b.Run("with_pool", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			buf := actor.AcquireBuffer()
			data, _ := json.Marshal(msg)
			*buf = append(*buf, data...)
			actor.ReleaseBuffer(buf)
		}
	})

	b.Run("without_pool", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			data, _ := json.Marshal(msg)
			_ = data
		}
	})
}
