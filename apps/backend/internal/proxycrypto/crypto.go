package proxycrypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

var ErrKeyRequired = errors.New("proxy encryption key is required")

type Crypto struct {
	gcm cipher.AEAD
}

func New(base64Key string) (*Crypto, error) {
	if base64Key == "" {
		return nil, ErrKeyRequired
	}

	key, err := base64.StdEncoding.DecodeString(base64Key)
	if err != nil {
		return nil, fmt.Errorf("decode proxy encryption key: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("proxy encryption key must decode to 32 bytes, got %d", len(key))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create AES cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create AES-GCM: %w", err)
	}

	return &Crypto{gcm: gcm}, nil
}

func (c *Crypto) Encrypt(plaintext string) ([]byte, []byte, error) {
	if c == nil {
		return nil, nil, ErrKeyRequired
	}
	nonce := make([]byte, c.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, fmt.Errorf("generate proxy nonce: %w", err)
	}
	ciphertext := c.gcm.Seal(nil, nonce, []byte(plaintext), nil)
	return ciphertext, nonce, nil
}

func (c *Crypto) Decrypt(ciphertext []byte, nonce []byte) (string, error) {
	if c == nil {
		return "", ErrKeyRequired
	}
	plaintext, err := c.gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt proxy endpoint: %w", err)
	}
	return string(plaintext), nil
}
