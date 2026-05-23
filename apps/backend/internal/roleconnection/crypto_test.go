package roleconnection

import (
	"crypto/rand"
	"encoding/base64"
	"testing"

	"rustlens/backend/internal/proxycrypto"
)

func newTestCrypto(t *testing.T) *proxycrypto.Crypto {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	c, err := proxycrypto.New(base64.StdEncoding.EncodeToString(key))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestTokenCipher_RoundTrip(t *testing.T) {
	c := newTestCrypto(t)
	tc := TokenCipher{Crypto: c}
	ct, nonce, err := tc.Encrypt("secret-access-token")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	got, err := tc.Decrypt(ct, nonce)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if got != "secret-access-token" {
		t.Errorf("got %q", got)
	}
}

func TestTokenCipher_NilCryptoReturnsError(t *testing.T) {
	tc := TokenCipher{Crypto: nil}
	_, _, err := tc.Encrypt("x")
	if err == nil {
		t.Error("expected error from nil crypto")
	}
}
