//! Legacy `bmmatch` resolver — an optional **fallback** Steam↔BM source behind a
//! bearer token (Go `internal/bmmatch`). Disabled (→ `NotConfigured`) when the
//! base URL or token is empty. Sent **directly**. 10s timeout.

use std::collections::HashMap;
use std::time::Duration;

use async_trait::async_trait;
use url::Url;

use crate::steammatch::{MatchError, Resolver};

const TIMEOUT: Duration = Duration::from_secs(10);

/// The bmmatch client. Empty `base_url` or `token` disables it.
#[derive(Clone)]
pub struct Client {
    http: reqwest::Client,
    base_url: String,
    token: String,
}

impl Client {
    /// Build a client (empty `base_url`/`token` → disabled).
    pub fn new(base_url: impl Into<String>, token: impl Into<String>) -> Result<Self, MatchError> {
        let http = reqwest::Client::builder()
            .timeout(TIMEOUT)
            .build()
            .map_err(|e| MatchError::Other(e.to_string()))?;
        Ok(Self {
            http,
            base_url: base_url.into().trim_end_matches('/').to_string(),
            token: token.into(),
        })
    }

    fn configured(&self) -> bool {
        !self.base_url.is_empty() && !self.token.is_empty()
    }

    /// GET `{base}{path}` with the bearer token, returning the first non-empty
    /// string value of the single-entry JSON map response. 404 → `NotFound`.
    async fn lookup(&self, path: &str) -> Result<String, MatchError> {
        if !self.configured() {
            return Err(MatchError::NotConfigured);
        }
        let url = Url::parse(&format!("{}{}", self.base_url, path))
            .map_err(|e| MatchError::Other(e.to_string()))?;
        let resp = self
            .http
            .get(url)
            .bearer_auth(&self.token)
            .send()
            .await
            .map_err(|e| MatchError::Other(e.to_string()))?;
        let status = resp.status().as_u16();
        if status == 404 {
            return Err(MatchError::NotFound);
        }
        if !(200..300).contains(&status) {
            return Err(MatchError::Other(format!("bmmatch: upstream returned status {status}")));
        }
        let bytes = resp.bytes().await.map_err(|e| MatchError::Other(e.to_string()))?;
        let map: HashMap<String, String> =
            serde_json::from_slice(&bytes).map_err(|e| MatchError::Other(e.to_string()))?;
        let value = map
            .into_values()
            .map(|v| v.trim().to_string())
            .find(|v| !v.is_empty())
            .unwrap_or_default();
        if value.is_empty() {
            Err(MatchError::NotFound)
        } else {
            Ok(value)
        }
    }
}

#[async_trait]
impl Resolver for Client {
    async fn steam_to_bm(&self, steam_id: &str) -> Result<String, MatchError> {
        self.lookup(&format!("/steam-to-bm/{steam_id}")).await
    }
    async fn bm_to_steam(&self, bm_id: &str) -> Result<String, MatchError> {
        self.lookup(&format!("/bm-to-steam/{bm_id}")).await
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use wiremock::matchers::{header, method, path};
    use wiremock::{Mock, MockServer, ResponseTemplate};

    #[tokio::test]
    async fn unconfigured_is_not_configured_without_request() {
        let c = Client::new("", "").unwrap();
        assert!(matches!(c.steam_to_bm("1").await, Err(MatchError::NotConfigured)));
        let c2 = Client::new("http://x", "").unwrap();
        assert!(matches!(c2.steam_to_bm("1").await, Err(MatchError::NotConfigured)));
    }

    #[tokio::test]
    async fn steam_to_bm_returns_first_value_with_bearer() {
        let server = MockServer::start().await;
        Mock::given(method("GET"))
            .and(path("/steam-to-bm/76561198000000000"))
            .and(header("authorization", "Bearer tok"))
            .respond_with(ResponseTemplate::new(200).set_body_json(serde_json::json!({"bm_id":"918543318"})))
            .mount(&server)
            .await;
        let c = Client::new(server.uri(), "tok").unwrap();
        assert_eq!(c.steam_to_bm("76561198000000000").await.unwrap(), "918543318");
    }

    #[tokio::test]
    async fn not_found_maps_to_not_found() {
        let server = MockServer::start().await;
        Mock::given(method("GET"))
            .and(path("/bm-to-steam/1"))
            .respond_with(ResponseTemplate::new(404))
            .mount(&server)
            .await;
        let c = Client::new(server.uri(), "tok").unwrap();
        assert!(matches!(c.bm_to_steam("1").await, Err(MatchError::NotFound)));
    }
}
