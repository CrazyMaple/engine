package bench

import (
	"crypto/rand"
	"testing"
	"time"

	"engine/remote"
)

// 加密路径基准：对比 AES-256-GCM 加密/解密与明文 copy 的单消息延迟/吞吐。
//
// 运行方式：
//
//	go test ./bench/ -bench=BenchmarkEncryption -benchmem
//
// 输出同时导出 p50-ns/op / p95-ns/op / p99-ns/op 自定义指标，
// 可通过 ParseBenchOutput 解析后与基线对比。

func makeCipher(b *testing.B, keyID uint32) *remote.AESGCMCipher {
	b.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		b.Fatal(err)
	}
	c, err := remote.NewAESGCMCipher(key, keyID)
	if err != nil {
		b.Fatal(err)
	}
	return c
}

// BenchmarkEncryption 加密/解密路径在不同消息尺寸下的性能。
// 子基准名 "plaintext-{size}" 和 "aes-gcm-{size}" 会被解析为 Tag，
// 便于与基线按尺寸逐档对比。
func BenchmarkEncryption(b *testing.B) {
	sizes := []int{64, 256, 1024, 4096}
	for _, size := range sizes {
		payload := make([]byte, size)
		rand.Read(payload)

		b.Run(sizeTag("plaintext", size), func(b *testing.B) {
			b.SetBytes(int64(size))
			b.ReportAllocs()
			rec := NewLatencyRecorder(b.N)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				t0 := time.Now()
				dst := make([]byte, len(payload))
				copy(dst, payload)
				rec.Add(time.Since(t0).Nanoseconds())
			}
			reportPercentiles(b, rec)
		})

		b.Run(sizeTag("aes-gcm-encrypt", size), func(b *testing.B) {
			c := makeCipher(b, 1)
			b.SetBytes(int64(size))
			b.ReportAllocs()
			rec := NewLatencyRecorder(b.N)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				t0 := time.Now()
				if _, err := c.Encrypt(payload); err != nil {
					b.Fatal(err)
				}
				rec.Add(time.Since(t0).Nanoseconds())
			}
			reportPercentiles(b, rec)
		})

		b.Run(sizeTag("aes-gcm-roundtrip", size), func(b *testing.B) {
			c := makeCipher(b, 1)
			b.SetBytes(int64(size))
			b.ReportAllocs()
			rec := NewLatencyRecorder(b.N)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				t0 := time.Now()
				ct, err := c.Encrypt(payload)
				if err != nil {
					b.Fatal(err)
				}
				if _, err := c.Decrypt(ct); err != nil {
					b.Fatal(err)
				}
				rec.Add(time.Since(t0).Nanoseconds())
			}
			reportPercentiles(b, rec)
		})
	}
}

// BenchmarkCipherRingRotation 密钥轮换期间解密性能（命中旧密钥）
func BenchmarkCipherRingRotation(b *testing.B) {
	ring := makeRing(b)
	payload := make([]byte, 256)
	rand.Read(payload)

	// 先用 keyID=1 加密
	old, err := remote.NewAESGCMCipher(ringInitialKey, 1)
	if err != nil {
		b.Fatal(err)
	}
	ct, err := old.Encrypt(payload)
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := ring.Decrypt(ct); err != nil {
			b.Fatal(err)
		}
	}
}

var ringInitialKey = func() []byte {
	k := make([]byte, 32)
	rand.Read(k)
	return k
}()

func makeRing(b *testing.B) *remote.CipherRing {
	b.Helper()
	ring, err := remote.NewCipherRing(ringInitialKey, 1)
	if err != nil {
		b.Fatal(err)
	}
	newKey := make([]byte, 32)
	rand.Read(newKey)
	if err := ring.Rotate(newKey, 2); err != nil {
		b.Fatal(err)
	}
	return ring
}

func sizeTag(prefix string, size int) string {
	switch {
	case size < 1024:
		return prefix + "-" + itoa(size) + "B"
	default:
		return prefix + "-" + itoa(size/1024) + "KB"
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := make([]byte, 0, 8)
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	return string(buf)
}

func reportPercentiles(b *testing.B, rec *LatencyRecorder) {
	b.Helper()
	p := rec.Percentiles()
	b.ReportMetric(p.P50, "p50-ns/op")
	b.ReportMetric(p.P95, "p95-ns/op")
	b.ReportMetric(p.P99, "p99-ns/op")
}
