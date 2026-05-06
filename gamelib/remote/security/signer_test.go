package security

import "testing"

func TestHMACSigner_SignVerify(t *testing.T) {
	signer := NewHMACSigner([]byte("secret-key"))

	data := []byte("hello world")
	sig := signer.Sign(data)

	if len(sig) != signer.SignatureSize() {
		t.Fatalf("expected signature length %d, got %d", signer.SignatureSize(), len(sig))
	}
	if signer.SignatureSize() != 32 {
		t.Fatalf("HMAC-SHA256 signature size must be 32, got %d", signer.SignatureSize())
	}

	if !signer.Verify(data, sig) {
		t.Fatal("valid signature should verify")
	}
}

func TestHMACSigner_Tampered(t *testing.T) {
	signer := NewHMACSigner([]byte("secret-key"))

	data := []byte("hello world")
	sig := signer.Sign(data)

	tampered := []byte("hello worlD")
	if signer.Verify(tampered, sig) {
		t.Fatal("tampered data should not verify")
	}
}

func TestHMACSigner_WrongKey(t *testing.T) {
	signer1 := NewHMACSigner([]byte("key1"))
	signer2 := NewHMACSigner([]byte("key2"))

	data := []byte("hello world")
	sig := signer1.Sign(data)

	if signer2.Verify(data, sig) {
		t.Fatal("different key should not verify")
	}
}

func TestHMACSigner_Deterministic(t *testing.T) {
	signer := NewHMACSigner([]byte("key"))
	data := []byte("test data")

	sig1 := signer.Sign(data)
	sig2 := signer.Sign(data)

	if string(sig1) != string(sig2) {
		t.Fatal("same data should produce same signature")
	}
}
