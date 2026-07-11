//! Bot service JWT validation for `/api/internal/bot/*`.
//!
//! HS256, byte-compatible with the Go backend (`internal/auth/jwt.go`) and the
//! bot's signer (`apps/bot/src/backend-client.ts`): claims `{service:"bot",
//! worker_id, iss, aud, exp, iat}`, key `BOT_SERVICE_JWT_SECRET`, issuer
//! `rustlens-bot`, audience `rustlens-backend`, 2s leeway, `service` must equal
//! `"bot"`.

use jsonwebtoken::{Algorithm, DecodingKey, Validation, decode};
use serde::Deserialize;

/// The subset of claims we read (registered claims are validated by the lib).
#[derive(Debug, Clone, Deserialize)]
pub struct BotClaims {
    pub service: String,
    #[serde(default)]
    pub worker_id: Option<String>,
}

#[derive(Debug, thiserror::Error)]
pub enum JwtError {
    #[error("invalid authorization header")]
    InvalidHeader,
    #[error("missing bearer token")]
    MissingToken,
    #[error("{0}")]
    Invalid(String),
    #[error("invalid service claim")]
    InvalidService,
}

/// Parse a `Bearer <token>` header value (case-insensitive scheme). Mirrors
/// `ExtractBearerToken`.
pub fn extract_bearer(header_value: &str) -> Result<&str, JwtError> {
    let trimmed = header_value.trim();
    let mut parts = trimmed.splitn(2, ' ');
    let scheme = parts.next().unwrap_or("");
    let token = parts.next().ok_or(JwtError::InvalidHeader)?;
    if !scheme.eq_ignore_ascii_case("Bearer") {
        return Err(JwtError::InvalidHeader);
    }
    if token.is_empty() {
        return Err(JwtError::MissingToken);
    }
    Ok(token)
}

/// Validate a bot service token. Mirrors `Validator.Parse`.
pub fn validate(
    token: &str,
    secret: &str,
    issuer: &str,
    audience: &str,
) -> Result<BotClaims, JwtError> {
    let mut validation = Validation::new(Algorithm::HS256);
    validation.set_issuer(&[issuer]);
    validation.set_audience(&[audience]);
    validation.leeway = 2;
    // `exp` is required and validated by default.
    let data = decode::<BotClaims>(
        token,
        &DecodingKey::from_secret(secret.as_bytes()),
        &validation,
    )
    .map_err(|e| JwtError::Invalid(e.to_string()))?;
    if data.claims.service != "bot" {
        return Err(JwtError::InvalidService);
    }
    Ok(data.claims)
}

#[cfg(test)]
mod tests {
    use super::*;
    use jsonwebtoken::{EncodingKey, Header, encode};
    use serde::Serialize;

    const SECRET: &str = "bot-secret";
    const ISS: &str = "rustlens-bot";
    const AUD: &str = "rustlens-backend";

    #[derive(Serialize)]
    struct Mint {
        service: &'static str,
        worker_id: &'static str,
        iss: &'static str,
        aud: &'static str,
        exp: i64,
        iat: i64,
    }

    fn mint(service: &'static str, iss: &'static str, aud: &'static str, exp_offset: i64) -> String {
        // Fixed base time so tests are deterministic; the lib compares against
        // the real clock, so use a far-future/near exp via the offset.
        let now = 1_900_000_000; // ~2030
        let claims = Mint { service, worker_id: "w1", iss, aud, exp: now + exp_offset, iat: now };
        encode(&Header::new(Algorithm::HS256), &claims, &EncodingKey::from_secret(SECRET.as_bytes())).unwrap()
    }

    #[test]
    fn extract_bearer_cases() {
        assert_eq!(extract_bearer("Bearer abc").unwrap(), "abc");
        assert_eq!(extract_bearer("bearer abc").unwrap(), "abc");
        assert!(matches!(extract_bearer("abc"), Err(JwtError::InvalidHeader)));
        // Trailing space is trimmed away, leaving a single token -> InvalidHeader
        // (matches Go, where MissingToken is effectively unreachable post-trim).
        assert!(matches!(extract_bearer("Bearer "), Err(JwtError::InvalidHeader)));
        assert!(matches!(extract_bearer("Basic abc"), Err(JwtError::InvalidHeader)));
    }

    #[test]
    fn valid_token() {
        // exp far in the future so validate() against the real clock passes.
        let t = mint("bot", ISS, AUD, 10_000_000_000);
        let c = validate(&t, SECRET, ISS, AUD).unwrap();
        assert_eq!(c.service, "bot");
        assert_eq!(c.worker_id.as_deref(), Some("w1"));
    }

    #[test]
    fn rejects_bad_service_secret_issuer_audience() {
        let good = mint("bot", ISS, AUD, 10_000_000_000);
        assert!(matches!(validate(&good, "wrong", ISS, AUD), Err(JwtError::Invalid(_))));
        assert!(validate(&mint("worker", ISS, AUD, 10_000_000_000), SECRET, ISS, AUD).is_err());
        assert!(validate(&mint("bot", "evil", AUD, 10_000_000_000), SECRET, ISS, AUD).is_err());
        assert!(validate(&mint("bot", ISS, "evil", 10_000_000_000), SECRET, ISS, AUD).is_err());
    }

    #[test]
    fn rejects_expired() {
        // exp well in the past (beyond 2s leeway).
        let t = mint("bot", ISS, AUD, -10_000_000_000);
        assert!(validate(&t, SECRET, ISS, AUD).is_err());
    }
}
