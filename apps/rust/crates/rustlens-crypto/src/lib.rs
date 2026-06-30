//! AES-256-GCM authenticated encryption for secrets at rest.
//!
//! Byte-compatible with the Go backend's `internal/proxycrypto` so the same
//! `PROXY_ENCRYPTION_KEY` and the same `discord_role_connections` ciphertext are
//! interchangeable across services:
//!   - key: `base64.StdEncoding` of exactly 32 bytes (AES-256),
//!   - nonce: 12 random bytes, returned alongside the ciphertext,
//!   - AEAD: AES-GCM with **no** additional authenticated data.

use aes_gcm::aead::{Aead, KeyInit};
use aes_gcm::{Aes256Gcm, Nonce};
use base64::Engine;
use base64::engine::general_purpose::STANDARD;
use rand::RngCore;

#[derive(Debug, thiserror::Error)]
pub enum CryptoError {
    #[error("encryption key is required")]
    KeyRequired,
    #[error("decode encryption key: {0}")]
    KeyDecode(String),
    #[error("encryption key must decode to 32 bytes, got {0}")]
    KeyLength(usize),
    #[error("encryption failed")]
    Encrypt,
    #[error("decryption failed")]
    Decrypt,
}

/// An AES-256-GCM cipher bound to a single key.
#[derive(Clone)]
pub struct Cipher {
    gcm: Aes256Gcm,
}

impl Cipher {
    /// Build from a base64-standard-encoded 32-byte key (the Go
    /// `proxycrypto.New` contract).
    pub fn from_base64_key(base64_key: &str) -> Result<Self, CryptoError> {
        if base64_key.is_empty() {
            return Err(CryptoError::KeyRequired);
        }
        let key = STANDARD
            .decode(base64_key)
            .map_err(|e| CryptoError::KeyDecode(e.to_string()))?;
        if key.len() != 32 {
            return Err(CryptoError::KeyLength(key.len()));
        }
        let gcm = Aes256Gcm::new_from_slice(&key).map_err(|_| CryptoError::KeyLength(key.len()))?;
        Ok(Self { gcm })
    }

    /// Encrypt `plaintext`, returning `(ciphertext, nonce)` (a fresh 12-byte
    /// random nonce per call), matching Go's `Encrypt`.
    pub fn encrypt(&self, plaintext: &str) -> Result<(Vec<u8>, Vec<u8>), CryptoError> {
        let mut nonce_bytes = [0u8; 12];
        rand::thread_rng().fill_bytes(&mut nonce_bytes);
        let nonce = Nonce::from_slice(&nonce_bytes);
        let ciphertext = self
            .gcm
            .encrypt(nonce, plaintext.as_bytes())
            .map_err(|_| CryptoError::Encrypt)?;
        Ok((ciphertext, nonce_bytes.to_vec()))
    }

    /// Decrypt a `(ciphertext, nonce)` pair, matching Go's `Decrypt`. A nonce of
    /// the wrong length returns an error rather than panicking (Go's `gcm.Open`
    /// tolerates any input), so a corrupted row can't crash a handler.
    pub fn decrypt(&self, ciphertext: &[u8], nonce: &[u8]) -> Result<String, CryptoError> {
        if nonce.len() != 12 {
            return Err(CryptoError::Decrypt);
        }
        let plaintext = self
            .gcm
            .decrypt(Nonce::from_slice(nonce), ciphertext)
            .map_err(|_| CryptoError::Decrypt)?;
        String::from_utf8(plaintext).map_err(|_| CryptoError::Decrypt)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    // 32 zero bytes, base64-standard encoded.
    const KEY: &str = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=";

    #[test]
    fn round_trip() {
        let c = Cipher::from_base64_key(KEY).unwrap();
        let (ct, nonce) = c.encrypt("hello secret token").unwrap();
        assert_eq!(nonce.len(), 12);
        assert_ne!(ct, b"hello secret token");
        assert_eq!(c.decrypt(&ct, &nonce).unwrap(), "hello secret token");
    }

    #[test]
    fn tamper_is_rejected() {
        let c = Cipher::from_base64_key(KEY).unwrap();
        let (mut ct, nonce) = c.encrypt("x").unwrap();
        ct[0] ^= 0xff;
        assert!(c.decrypt(&ct, &nonce).is_err());
    }

    #[test]
    fn wrong_nonce_length_errors_not_panics() {
        let c = Cipher::from_base64_key(KEY).unwrap();
        // 1-byte nonce (e.g. a corrupted DB row) must return Err, not panic.
        assert!(c.decrypt(&[0u8; 16], &[0u8]).is_err());
    }

    #[test]
    fn bad_key_length() {
        // 16 bytes base64 -> wrong length.
        let short = STANDARD.encode([0u8; 16]);
        assert!(matches!(
            Cipher::from_base64_key(&short),
            Err(CryptoError::KeyLength(16))
        ));
        assert!(matches!(
            Cipher::from_base64_key(""),
            Err(CryptoError::KeyRequired)
        ));
    }
}
