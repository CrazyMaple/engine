package bench

import (
	"crypto/rand"
	"testing"
	"time"

	"engine/actor"
	"engine/remote"
)

// 端到端基准：Remote Endpoint 加密模式 vs 非加密端到端对比。
// 由于完整 TCP/TLS 链路依赖真实端口，这里以本地 in-memory pipe 模拟
// Endpoint 的发送侧核心路径：序列化 → 可选加密 → 反向解密 → 反序列化。
//
// 对比口径与 encryption_bench_test.go 不同：
//   - 本文件包含编解码开销（JSON 兜底），反映 Endpoint 真实路径；
//   - 加密基准只测 Cipher 单次 Encrypt/Decrypt，不包含编解码；

func sampleRemoteMessage(size int) *remote.RemoteMessage {
	payload := make([]byte, size)
	rand.Read(payload)
	return &remote.RemoteMessage{
		Target:   actor.NewPID("local", "bench-target"),
		Sender:   actor.NewPID("local", "bench-sender"),
		Message:  map[string]interface{}{"payload": payload, "seq": 1},
		Type:     remote.MessageTypeUser,
		TypeName: "BenchPayload",
	}
}

// BenchmarkEndpointRoundtrip Endpoint 发送→接收端到端单消息耗时。
// 子基准 plaintext-* 不启用加密；aes-gcm-* 启用 AES-256-GCM。
func BenchmarkEndpointRoundtrip(b *testing.B) {
	codec := remote.DefaultRemoteCodec()
	sizes := []int{128, 1024}

	for _, size := range sizes {
		msg := sampleRemoteMessage(size)

		b.Run(sizeTag("plaintext", size), func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(size))
			rec := NewLatencyRecorder(b.N)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				t0 := time.Now()
				raw, err := codec.MarshalEnvelope(msg)
				if err != nil {
					b.Fatal(err)
				}
				isBatch, _, single, err := codec.UnmarshalEnvelope(raw)
				if err != nil || isBatch || single == nil {
					b.Fatalf("decode: err=%v batch=%v nil=%v", err, isBatch, single == nil)
				}
				rec.Add(time.Since(t0).Nanoseconds())
			}
			reportPercentiles(b, rec)
		})

		b.Run(sizeTag("aes-gcm", size), func(b *testing.B) {
			cipher := makeCipher(b, 1)
			b.ReportAllocs()
			b.SetBytes(int64(size))
			rec := NewLatencyRecorder(b.N)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				t0 := time.Now()
				raw, err := codec.MarshalEnvelope(msg)
				if err != nil {
					b.Fatal(err)
				}
				ct, err := cipher.Encrypt(raw)
				if err != nil {
					b.Fatal(err)
				}
				pt, err := cipher.Decrypt(ct)
				if err != nil {
					b.Fatal(err)
				}
				isBatch, _, single, err := codec.UnmarshalEnvelope(pt)
				if err != nil || isBatch || single == nil {
					b.Fatalf("decode: err=%v batch=%v nil=%v", err, isBatch, single == nil)
				}
				rec.Add(time.Since(t0).Nanoseconds())
			}
			reportPercentiles(b, rec)
		})
	}
}

// BenchmarkEndpointBatchedRoundtrip 批量发送路径（Endpoint 默认批处理 64 条）
func BenchmarkEndpointBatchedRoundtrip(b *testing.B) {
	codec := remote.DefaultRemoteCodec()
	batch := &remote.RemoteMessageBatch{Messages: make([]*remote.RemoteMessage, 0, 64)}
	for i := 0; i < 64; i++ {
		batch.Messages = append(batch.Messages, sampleRemoteMessage(128))
	}

	b.Run("plaintext-batch64", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(128 * 64))
		rec := NewLatencyRecorder(b.N)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			t0 := time.Now()
			raw, err := codec.MarshalEnvelope(batch)
			if err != nil {
				b.Fatal(err)
			}
			isBatch, dec, _, err := codec.UnmarshalEnvelope(raw)
			if err != nil || !isBatch || dec == nil || len(dec.Messages) != 64 {
				b.Fatalf("decode failed: err=%v batch=%v", err, isBatch)
			}
			rec.Add(time.Since(t0).Nanoseconds())
		}
		reportPercentiles(b, rec)
	})

	b.Run("aes-gcm-batch64", func(b *testing.B) {
		cipher := makeCipher(b, 1)
		b.ReportAllocs()
		b.SetBytes(int64(128 * 64))
		rec := NewLatencyRecorder(b.N)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			t0 := time.Now()
			raw, err := codec.MarshalEnvelope(batch)
			if err != nil {
				b.Fatal(err)
			}
			ct, err := cipher.Encrypt(raw)
			if err != nil {
				b.Fatal(err)
			}
			pt, err := cipher.Decrypt(ct)
			if err != nil {
				b.Fatal(err)
			}
			isBatch, dec, _, err := codec.UnmarshalEnvelope(pt)
			if err != nil || !isBatch || dec == nil || len(dec.Messages) != 64 {
				b.Fatalf("decode failed: err=%v batch=%v", err, isBatch)
			}
			rec.Add(time.Since(t0).Nanoseconds())
		}
		reportPercentiles(b, rec)
	})
}
