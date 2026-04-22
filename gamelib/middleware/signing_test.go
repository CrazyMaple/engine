package middleware

import (
	"testing"
)

func TestMessageSignerSignAndVerify(t *testing.T) {
	key := []byte("test-secret-key-32bytes-long!!!!!")
	signer := NewMessageSigner(key)

	data := []byte("hello world")
	sig := signer.Sign(data)

	if !signer.Verify(data, sig) {
		t.Fatal("signature verification should pass")
	}
}

func TestMessageSignerVerifyFail(t *testing.T) {
	key := []byte("test-secret-key")
	signer := NewMessageSigner(key)

	data := []byte("hello world")
	sig := signer.Sign(data)

	// 篡改数据
	tampered := []byte("hello world!")
	if signer.Verify(tampered, sig) {
		t.Fatal("signature verification should fail for tampered data")
	}
}

func TestMessageSignerDifferentKeys(t *testing.T) {
	signer1 := NewMessageSigner([]byte("key1"))
	signer2 := NewMessageSigner([]byte("key2"))

	data := []byte("hello world")
	sig := signer1.Sign(data)

	if signer2.Verify(data, sig) {
		t.Fatal("different keys should produce different signatures")
	}
}

func TestWrapSigned(t *testing.T) {
	signer := NewMessageSigner([]byte("secret"))
	payload := []byte(`{"cmd":"move","x":100}`)
	inner := map[string]interface{}{"cmd": "move", "x": 100}

	signed := WrapSigned(signer, payload, inner)

	if signed.Inner == nil {
		t.Fatal("inner message should not be nil")
	}
	if !signer.Verify(signed.Payload, signed.Signature) {
		t.Fatal("signed message should be verifiable")
	}
}
