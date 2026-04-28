package proxycrypto

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	key := base64.StdEncoding.EncodeToString([]byte("12345678901234567890123456789012"))
	c, err := New(key)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	ciphertext, nonce, err := c.Encrypt("http://user:pass@proxy-one:8080")
	if err != nil {
		t.Fatalf("Encrypt returned error: %v", err)
	}
	if strings.Contains(string(ciphertext), "user") || strings.Contains(string(ciphertext), "pass") {
		t.Fatal("ciphertext contains plaintext credentials")
	}

	decrypted, err := c.Decrypt(ciphertext, nonce)
	if err != nil {
		t.Fatalf("Decrypt returned error: %v", err)
	}
	if decrypted != "http://user:pass@proxy-one:8080" {
		t.Fatalf("Decrypt = %q", decrypted)
	}
}

func TestNewRejectsWrongKeyLength(t *testing.T) {
	key := base64.StdEncoding.EncodeToString([]byte("too-short"))
	_, err := New(key)
	if err == nil {
		t.Fatal("expected invalid key length error")
	}
}
