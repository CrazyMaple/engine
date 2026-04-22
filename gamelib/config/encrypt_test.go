package config

import (
	"testing"
)

func TestEncryptDecrypt(t *testing.T) {
	key := []byte("01234567890123456789012345678901") // 32 bytes

	original := "my-secret-password"
	encrypted, err := Encrypt(original, key)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	if !IsEncrypted(encrypted) {
		t.Errorf("encrypted value should have ENC: prefix, got %s", encrypted)
	}

	decrypted, err := Decrypt(encrypted, key)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}

	if decrypted != original {
		t.Errorf("decrypted = %q, want %q", decrypted, original)
	}
}

func TestEncryptInvalidKey(t *testing.T) {
	_, err := Encrypt("test", []byte("short"))
	if err == nil {
		t.Fatal("expected error for short key")
	}
}

func TestDecryptWrongKey(t *testing.T) {
	key1 := []byte("01234567890123456789012345678901")
	key2 := []byte("abcdefghijklmnopqrstuvwxyz012345")

	encrypted, err := Encrypt("secret", key1)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	_, err = Decrypt(encrypted, key2)
	if err == nil {
		t.Fatal("expected error decrypting with wrong key")
	}
}

func TestDecryptConfigFields(t *testing.T) {
	key := []byte("01234567890123456789012345678901")

	type DBConfig struct {
		Host     string `json:"host"`
		Password string `json:"password" encrypt:"true"`
		APIKey   string `json:"api_key" encrypt:"true"`
		Port     int    `json:"port"`
	}

	encPass, _ := Encrypt("db-pass-123", key)
	encKey, _ := Encrypt("api-key-456", key)

	cfg := &DBConfig{
		Host:     "localhost",
		Password: encPass,
		APIKey:   encKey,
		Port:     5432,
	}

	if err := DecryptConfigFields(cfg, key); err != nil {
		t.Fatalf("DecryptConfigFields failed: %v", err)
	}

	if cfg.Password != "db-pass-123" {
		t.Errorf("Password = %q, want db-pass-123", cfg.Password)
	}
	if cfg.APIKey != "api-key-456" {
		t.Errorf("APIKey = %q, want api-key-456", cfg.APIKey)
	}
	if cfg.Host != "localhost" {
		t.Errorf("Host should not change, got %q", cfg.Host)
	}
}

func TestIsEncrypted(t *testing.T) {
	if IsEncrypted("plain") {
		t.Error("plain text should not be encrypted")
	}
	if !IsEncrypted("ENC:abc") {
		t.Error("ENC: prefix should be detected")
	}
}
