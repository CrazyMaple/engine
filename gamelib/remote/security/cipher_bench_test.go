package security

import (
	"crypto/rand"
	"testing"
)

// 加密路径基准：AES-256-GCM 加 / 解密在不同消息尺寸下的吞吐与单次延迟。
//
// 运行方式：
//
//	go test ./remote/security/ -bench=BenchmarkEncryption -benchmem

func makeCipher(b *testing.B, keyID uint32) *AESGCMCipher {
	b.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		b.Fatal(err)
	}
	c, err := NewAESGCMCipher(key, keyID)
	if err != nil {
		b.Fatal(err)
	}
	return c
}

func BenchmarkEncryption(b *testing.B) {
	sizes := []int{64, 256, 1024, 4096}
	for _, size := range sizes {
		payload := make([]byte, size)
		_, _ = rand.Read(payload)

		b.Run(sizeTag("plaintext", size), func(b *testing.B) {
			b.SetBytes(int64(size))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				dst := make([]byte, len(payload))
				copy(dst, payload)
			}
		})

		b.Run(sizeTag("aes-gcm-encrypt", size), func(b *testing.B) {
			c := makeCipher(b, 1)
			b.SetBytes(int64(size))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := c.Encrypt(payload); err != nil {
					b.Fatal(err)
				}
			}
		})

		b.Run(sizeTag("aes-gcm-roundtrip", size), func(b *testing.B) {
			c := makeCipher(b, 1)
			b.SetBytes(int64(size))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				ct, err := c.Encrypt(payload)
				if err != nil {
					b.Fatal(err)
				}
				if _, err := c.Decrypt(ct); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkCipherRingRotation 密钥轮换期间解密性能（命中旧密钥）
func BenchmarkCipherRingRotation(b *testing.B) {
	ring := makeRing(b)
	payload := make([]byte, 256)
	_, _ = rand.Read(payload)

	old, err := NewAESGCMCipher(ringInitialKey, 1)
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
	_, _ = rand.Read(k)
	return k
}()

func makeRing(b *testing.B) *CipherRing {
	b.Helper()
	ring, err := NewCipherRing(ringInitialKey, 1)
	if err != nil {
		b.Fatal(err)
	}
	newKey := make([]byte, 32)
	_, _ = rand.Read(newKey)
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
