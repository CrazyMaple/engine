package remote

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
)

// 消息加密格式：
//   [4B KeyID (BigEndian) | 12B Nonce | Ciphertext+16B GCM Tag]
//
// 固定 4 字节 KeyID 便于接收侧在密钥轮换窗口期内用正确的密钥解密。
// Nonce 长度遵循 AES-GCM 推荐的 12 字节；每次加密随机生成。

const (
	aesKeySize    = 32 // AES-256
	keyIDSize     = 4
	gcmNonceSize  = 12
	gcmOverhead   = 16
	cipherMinSize = keyIDSize + gcmNonceSize + gcmOverhead
)

// MessageCipher 消息加密器接口。
// 实现应当对明文做 AEAD 加密，并在密文头部写入 4 字节 KeyID 以便接收方识别密钥。
type MessageCipher interface {
	Encrypt(plaintext []byte) ([]byte, error)
	Decrypt(ciphertext []byte) ([]byte, error)
	KeyID() uint32
}

// AESGCMCipher 基于 AES-256-GCM 的消息加密器（单密钥）。
type AESGCMCipher struct {
	aead  cipher.AEAD
	keyID uint32
}

// NewAESGCMCipher 使用 32 字节密钥创建 AES-256-GCM 加密器。
func NewAESGCMCipher(key []byte, keyID uint32) (*AESGCMCipher, error) {
	if len(key) != aesKeySize {
		return nil, fmt.Errorf("aes-256-gcm requires %d-byte key, got %d", aesKeySize, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &AESGCMCipher{aead: aead, keyID: keyID}, nil
}

// KeyID 返回当前密钥标识。
func (c *AESGCMCipher) KeyID() uint32 { return c.keyID }

// Encrypt 加密明文。输出格式 [4B KeyID | 12B Nonce | Ciphertext+Tag]。
func (c *AESGCMCipher) Encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	out := make([]byte, keyIDSize+c.aead.NonceSize(), keyIDSize+c.aead.NonceSize()+len(plaintext)+c.aead.Overhead())
	binary.BigEndian.PutUint32(out[:keyIDSize], c.keyID)
	copy(out[keyIDSize:], nonce)
	out = c.aead.Seal(out, nonce, plaintext, nil)
	return out, nil
}

// Decrypt 解密密文。密文的 KeyID 必须与当前密钥一致。
func (c *AESGCMCipher) Decrypt(data []byte) ([]byte, error) {
	if len(data) < cipherMinSize {
		return nil, errors.New("ciphertext too short")
	}
	keyID := binary.BigEndian.Uint32(data[:keyIDSize])
	if keyID != c.keyID {
		return nil, fmt.Errorf("key id mismatch: got %d want %d", keyID, c.keyID)
	}
	nonce := data[keyIDSize : keyIDSize+c.aead.NonceSize()]
	ciphertext := data[keyIDSize+c.aead.NonceSize():]
	return c.aead.Open(nil, nonce, ciphertext, nil)
}

// CipherRing 支持密钥轮换的加密器。
// 当前密钥用于加密；旧密钥保留在解密池中用于解密轮换窗口期内到达的老密文。
type CipherRing struct {
	mu      sync.RWMutex
	current *AESGCMCipher
	old     map[uint32]*AESGCMCipher
}

// NewCipherRing 使用初始密钥创建 CipherRing。
func NewCipherRing(key []byte, keyID uint32) (*CipherRing, error) {
	c, err := NewAESGCMCipher(key, keyID)
	if err != nil {
		return nil, err
	}
	return &CipherRing{current: c, old: make(map[uint32]*AESGCMCipher)}, nil
}

// Rotate 切换到新密钥。旧密钥自动加入解密池，调用 RetireKey 可清理。
func (r *CipherRing) Rotate(key []byte, keyID uint32) error {
	c, err := NewAESGCMCipher(key, keyID)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.current != nil {
		if r.current.keyID == keyID {
			return fmt.Errorf("key id %d already in use", keyID)
		}
		r.old[r.current.keyID] = r.current
	}
	r.current = c
	return nil
}

// RetireKey 从解密池中移除指定 KeyID 的旧密钥。
func (r *CipherRing) RetireKey(keyID uint32) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.old, keyID)
}

// ActiveKeys 返回当前密钥池中所有可用于解密的 KeyID。
func (r *CipherRing) ActiveKeys() []uint32 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]uint32, 0, len(r.old)+1)
	if r.current != nil {
		ids = append(ids, r.current.keyID)
	}
	for id := range r.old {
		ids = append(ids, id)
	}
	return ids
}

// KeyID 返回当前加密密钥 ID。
func (r *CipherRing) KeyID() uint32 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.current == nil {
		return 0
	}
	return r.current.keyID
}

// Encrypt 使用当前密钥加密。
func (r *CipherRing) Encrypt(plaintext []byte) ([]byte, error) {
	r.mu.RLock()
	c := r.current
	r.mu.RUnlock()
	if c == nil {
		return nil, errors.New("cipher ring has no active key")
	}
	return c.Encrypt(plaintext)
}

// Decrypt 根据密文头部 KeyID 自动选择当前或旧密钥。
func (r *CipherRing) Decrypt(data []byte) ([]byte, error) {
	if len(data) < keyIDSize {
		return nil, errors.New("ciphertext too short")
	}
	keyID := binary.BigEndian.Uint32(data[:keyIDSize])
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.current != nil && r.current.keyID == keyID {
		return r.current.Decrypt(data)
	}
	if c, ok := r.old[keyID]; ok {
		return c.Decrypt(data)
	}
	return nil, fmt.Errorf("unknown key id: %d", keyID)
}

// X25519Keypair X25519 密钥对（用于 Diffie-Hellman 密钥协商）。
type X25519Keypair struct {
	Private *ecdh.PrivateKey
	Public  *ecdh.PublicKey
}

// GenerateX25519Keypair 生成新的 X25519 密钥对。
func GenerateX25519Keypair() (*X25519Keypair, error) {
	priv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	return &X25519Keypair{Private: priv, Public: priv.PublicKey()}, nil
}

// DeriveSharedKey 使用 X25519 ECDH 计算共享密钥，再经 SHA-256 派生为 32 字节 AES-256 密钥。
// info 作为域分隔参数混入派生，避免相同共享密钥在不同上下文被误用。
func DeriveSharedKey(priv *ecdh.PrivateKey, peerPub *ecdh.PublicKey, info []byte) ([]byte, error) {
	shared, err := priv.ECDH(peerPub)
	if err != nil {
		return nil, err
	}
	h := sha256.New()
	h.Write(shared)
	h.Write(info)
	return h.Sum(nil), nil
}

// DerivedCipher 便捷包装：直接从 X25519 协商结果生成 AES-256-GCM 加密器。
func DerivedCipher(priv *ecdh.PrivateKey, peerPub *ecdh.PublicKey, info []byte, keyID uint32) (*AESGCMCipher, error) {
	key, err := DeriveSharedKey(priv, peerPub, info)
	if err != nil {
		return nil, err
	}
	return NewAESGCMCipher(key, keyID)
}
