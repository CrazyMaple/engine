package remote

import (
	"bytes"
	"crypto/rand"
	"testing"
)

func randomKey(t *testing.T) []byte {
	t.Helper()
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	return k
}

func TestAESGCMCipher_RoundTrip(t *testing.T) {
	c, err := NewAESGCMCipher(randomKey(t), 1)
	if err != nil {
		t.Fatalf("NewAESGCMCipher: %v", err)
	}

	plain := []byte("hello encrypted world with plenty of text for safety")
	ct, err := c.Encrypt(plain)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if bytes.Contains(ct, plain) {
		t.Fatalf("ciphertext should not contain plaintext")
	}
	got, err := c.Decrypt(ct)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatalf("roundtrip mismatch: %q != %q", got, plain)
	}
}

func TestAESGCMCipher_WrongKeyFails(t *testing.T) {
	c1, _ := NewAESGCMCipher(randomKey(t), 1)
	c2, _ := NewAESGCMCipher(randomKey(t), 1) // 相同 KeyID 不同 key
	ct, _ := c1.Encrypt([]byte("secret"))
	if _, err := c2.Decrypt(ct); err == nil {
		t.Fatal("expected decrypt failure with wrong key")
	}
}

func TestAESGCMCipher_KeyIDMismatch(t *testing.T) {
	key := randomKey(t)
	c1, _ := NewAESGCMCipher(key, 1)
	c2, _ := NewAESGCMCipher(key, 2)
	ct, _ := c1.Encrypt([]byte("secret"))
	if _, err := c2.Decrypt(ct); err == nil {
		t.Fatal("expected key id mismatch error")
	}
}

func TestAESGCMCipher_TamperDetection(t *testing.T) {
	c, _ := NewAESGCMCipher(randomKey(t), 1)
	ct, _ := c.Encrypt([]byte("confidential"))
	// 篡改密文尾部（GCM Tag 区域）
	ct[len(ct)-1] ^= 0x01
	if _, err := c.Decrypt(ct); err == nil {
		t.Fatal("expected GCM tag verification failure")
	}
}

func TestAESGCMCipher_InvalidKeySize(t *testing.T) {
	if _, err := NewAESGCMCipher(make([]byte, 16), 1); err == nil {
		t.Fatal("should reject 16-byte key")
	}
	if _, err := NewAESGCMCipher(make([]byte, 31), 1); err == nil {
		t.Fatal("should reject 31-byte key")
	}
}

func TestAESGCMCipher_ShortCiphertext(t *testing.T) {
	c, _ := NewAESGCMCipher(randomKey(t), 1)
	if _, err := c.Decrypt([]byte{1, 2, 3}); err == nil {
		t.Fatal("expected too-short error")
	}
}

func TestCipherRing_RotationPreservesOldDecryption(t *testing.T) {
	ring, err := NewCipherRing(randomKey(t), 1)
	if err != nil {
		t.Fatalf("NewCipherRing: %v", err)
	}

	oldCt, err := ring.Encrypt([]byte("old payload"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	// 轮换到新密钥
	if err := ring.Rotate(randomKey(t), 2); err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	if ring.KeyID() != 2 {
		t.Fatalf("KeyID after rotation: want 2, got %d", ring.KeyID())
	}

	// 旧密文仍能解密
	got, err := ring.Decrypt(oldCt)
	if err != nil {
		t.Fatalf("Decrypt old ct: %v", err)
	}
	if string(got) != "old payload" {
		t.Fatalf("old payload mismatch: %s", got)
	}

	// 新加密使用新密钥
	newCt, _ := ring.Encrypt([]byte("new payload"))
	got2, _ := ring.Decrypt(newCt)
	if string(got2) != "new payload" {
		t.Fatalf("new payload mismatch: %s", got2)
	}
}

func TestCipherRing_RetireKey(t *testing.T) {
	ring, _ := NewCipherRing(randomKey(t), 1)
	oldCt, _ := ring.Encrypt([]byte("x"))
	_ = ring.Rotate(randomKey(t), 2)

	ring.RetireKey(1)
	if _, err := ring.Decrypt(oldCt); err == nil {
		t.Fatal("expected decrypt failure after retiring old key")
	}
}

func TestCipherRing_DuplicateKeyIDRejected(t *testing.T) {
	ring, _ := NewCipherRing(randomKey(t), 1)
	if err := ring.Rotate(randomKey(t), 1); err == nil {
		t.Fatal("should reject rotation to same key id")
	}
}

func TestCipherRing_ActiveKeys(t *testing.T) {
	ring, _ := NewCipherRing(randomKey(t), 1)
	_ = ring.Rotate(randomKey(t), 2)
	_ = ring.Rotate(randomKey(t), 3)

	ids := ring.ActiveKeys()
	if len(ids) != 3 {
		t.Fatalf("expected 3 active keys, got %d", len(ids))
	}
	seen := map[uint32]bool{}
	for _, id := range ids {
		seen[id] = true
	}
	for _, want := range []uint32{1, 2, 3} {
		if !seen[want] {
			t.Errorf("missing key id %d", want)
		}
	}
}

func TestCipherRing_UnknownKeyID(t *testing.T) {
	ring, _ := NewCipherRing(randomKey(t), 1)
	other, _ := NewAESGCMCipher(randomKey(t), 99)
	ct, _ := other.Encrypt([]byte("x"))
	if _, err := ring.Decrypt(ct); err == nil {
		t.Fatal("expected unknown key id error")
	}
}

func TestX25519_DeriveSharedKey(t *testing.T) {
	alice, err := GenerateX25519Keypair()
	if err != nil {
		t.Fatalf("GenerateX25519Keypair: %v", err)
	}
	bob, err := GenerateX25519Keypair()
	if err != nil {
		t.Fatalf("GenerateX25519Keypair: %v", err)
	}

	aliceKey, err := DeriveSharedKey(alice.Private, bob.Public, []byte("engine-remote-v1"))
	if err != nil {
		t.Fatalf("DeriveSharedKey alice: %v", err)
	}
	bobKey, err := DeriveSharedKey(bob.Private, alice.Public, []byte("engine-remote-v1"))
	if err != nil {
		t.Fatalf("DeriveSharedKey bob: %v", err)
	}
	if !bytes.Equal(aliceKey, bobKey) {
		t.Fatal("shared keys should match")
	}
	if len(aliceKey) != 32 {
		t.Fatalf("shared key size: want 32, got %d", len(aliceKey))
	}
}

func TestX25519_DifferentInfoDerivesDifferentKey(t *testing.T) {
	alice, _ := GenerateX25519Keypair()
	bob, _ := GenerateX25519Keypair()

	k1, _ := DeriveSharedKey(alice.Private, bob.Public, []byte("context-1"))
	k2, _ := DeriveSharedKey(alice.Private, bob.Public, []byte("context-2"))
	if bytes.Equal(k1, k2) {
		t.Fatal("different info should derive different keys")
	}
}

func TestDerivedCipher_RoundTrip(t *testing.T) {
	alice, _ := GenerateX25519Keypair()
	bob, _ := GenerateX25519Keypair()

	aliceCipher, err := DerivedCipher(alice.Private, bob.Public, []byte("test"), 1)
	if err != nil {
		t.Fatalf("DerivedCipher alice: %v", err)
	}
	bobCipher, err := DerivedCipher(bob.Private, alice.Public, []byte("test"), 1)
	if err != nil {
		t.Fatalf("DerivedCipher bob: %v", err)
	}

	ct, _ := aliceCipher.Encrypt([]byte("hello"))
	pt, err := bobCipher.Decrypt(ct)
	if err != nil {
		t.Fatalf("bob decrypt: %v", err)
	}
	if string(pt) != "hello" {
		t.Fatalf("expected hello, got %s", pt)
	}
}

// 基准测试：AES-256-GCM 加解密吞吐

func BenchmarkAESGCMEncrypt_64B(b *testing.B) {
	benchEncrypt(b, 64)
}

func BenchmarkAESGCMEncrypt_1KB(b *testing.B) {
	benchEncrypt(b, 1024)
}

func BenchmarkAESGCMEncrypt_16KB(b *testing.B) {
	benchEncrypt(b, 16*1024)
}

func BenchmarkAESGCMDecrypt_1KB(b *testing.B) {
	benchDecrypt(b, 1024)
}

func benchEncrypt(b *testing.B, size int) {
	b.Helper()
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	c, _ := NewAESGCMCipher(key, 1)
	plain := make([]byte, size)
	_, _ = rand.Read(plain)
	b.SetBytes(int64(size))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := c.Encrypt(plain); err != nil {
			b.Fatal(err)
		}
	}
}

func benchDecrypt(b *testing.B, size int) {
	b.Helper()
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	c, _ := NewAESGCMCipher(key, 1)
	plain := make([]byte, size)
	_, _ = rand.Read(plain)
	ct, _ := c.Encrypt(plain)
	b.SetBytes(int64(size))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := c.Decrypt(ct); err != nil {
			b.Fatal(err)
		}
	}
}
