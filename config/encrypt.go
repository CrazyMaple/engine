package config

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"reflect"
	"strings"
)

const encryptedPrefix = "ENC:"

// Encrypt 使用 AES-256-GCM 加密明文字符串
// key 必须为 32 字节（AES-256）
// 返回 "ENC:" + base64 编码的密文
func Encrypt(plaintext string, key []byte) (string, error) {
	if len(key) != 32 {
		return "", fmt.Errorf("encryption key must be 32 bytes, got %d", len(key))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return encryptedPrefix + base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt 解密 AES-256-GCM 加密的字符串
// 输入必须以 "ENC:" 为前缀
func Decrypt(ciphertext string, key []byte) (string, error) {
	if !IsEncrypted(ciphertext) {
		return "", fmt.Errorf("not an encrypted value (missing ENC: prefix)")
	}

	if len(key) != 32 {
		return "", fmt.Errorf("encryption key must be 32 bytes, got %d", len(key))
	}

	data, err := base64.StdEncoding.DecodeString(ciphertext[len(encryptedPrefix):])
	if err != nil {
		return "", fmt.Errorf("base64 decode: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	nonce, encrypted := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, encrypted, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}

	return string(plaintext), nil
}

// IsEncrypted 检查字符串是否是加密值（以 "ENC:" 为前缀）
func IsEncrypted(s string) bool {
	return strings.HasPrefix(s, encryptedPrefix)
}

// DecryptConfigFields 自动解密结构体中带有 `encrypt:"true"` 标签的字符串字段
// target 必须为结构体指针
func DecryptConfigFields(target interface{}, key []byte) error {
	v := reflect.ValueOf(target)
	if v.Kind() != reflect.Ptr || v.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("target must be a pointer to struct")
	}
	v = v.Elem()
	t := v.Type()

	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		fieldType := t.Field(i)

		// 只处理 string 字段
		if field.Kind() != reflect.String {
			continue
		}

		// 检查 encrypt tag
		tag := fieldType.Tag.Get("encrypt")
		if tag != "true" {
			continue
		}

		val := field.String()
		if !IsEncrypted(val) {
			continue
		}

		decrypted, err := Decrypt(val, key)
		if err != nil {
			return fmt.Errorf("decrypt field %s: %w", fieldType.Name, err)
		}
		field.SetString(decrypted)
	}

	return nil
}
