package roleconnection

import "rustlens/backend/internal/proxycrypto"

// TokenCipher is a thin wrapper over proxycrypto.Crypto for OAuth tokens.
// It exists so call sites don't accidentally swap ciphertext and nonce
// between access and refresh tokens.
type TokenCipher struct {
	Crypto *proxycrypto.Crypto
}

func (t TokenCipher) Encrypt(plaintext string) (ciphertext, nonce []byte, err error) {
	return t.Crypto.Encrypt(plaintext)
}

func (t TokenCipher) Decrypt(ciphertext, nonce []byte) (string, error) {
	return t.Crypto.Decrypt(ciphertext, nonce)
}
