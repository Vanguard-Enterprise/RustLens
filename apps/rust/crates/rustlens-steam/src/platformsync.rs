//! platformsync.io client (Go `internal/platformsync`) — cross-platform identity
//! discovery. Returns the current Steam↔Discord pair plus any previous IDs, all
//! upserted as hard links by the consumer. Sent **directly**. Disabled
//! (→ `NotConfigured`) when the token is empty; any upstream HTTP error also maps
//! to `NotConfigured` (matching Go).

use std::time::Duration;

use serde::Deserialize;
use url::Url;

/// The hard-coded upstream (Go `defaultBaseURL`); overridable for tests.
pub const DEFAULT_BASE_URL: &str = "https://link.platformsync.io/api.php";
const TIMEOUT: Duration = Duration::from_secs(10);

#[derive(Debug, thiserror::Error)]
pub enum PlatformSyncError {
    #[error("platformsync: client not configured")]
    NotConfigured,
    #[error("platformsync: identity not linked")]
    NotLinked,
}

/// A resolved cross-platform identity (Go `Lookup`).
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct Lookup {
    pub linked: bool,
    pub steam_id: String,
    pub discord_id: String,
    pub previous_steam_ids: Vec<String>,
    pub previous_discord_ids: Vec<String>,
}

#[derive(Deserialize, Default)]
#[serde(default)]
struct RawResponse {
    linked: bool,
    steam_id: String,
    discord_id: String,
    previous_steam_ids: String,
    previous_discord_ids: String,
}

/// Split a `previous_*_ids` field: literal `"NA"` or CSV. Trims, drops empties
/// and `"NA"` entries; returns an empty vec when nothing remains.
fn split_previous(raw: &str) -> Vec<String> {
    raw.split(',')
        .map(str::trim)
        .filter(|s| !s.is_empty() && !s.eq_ignore_ascii_case("NA"))
        .map(str::to_string)
        .collect()
}

/// The platformsync client. Empty `token` disables it.
#[derive(Clone)]
pub struct Client {
    http: reqwest::Client,
    base_url: String,
    token: String,
}

impl Client {
    /// Build a client against the default upstream. Empty `token` → disabled.
    pub fn new(token: impl Into<String>) -> Result<Self, PlatformSyncError> {
        Self::with_base_url(DEFAULT_BASE_URL, token)
    }

    /// Build a client against a custom base URL (tests point this at a mock).
    pub fn with_base_url(
        base_url: impl Into<String>,
        token: impl Into<String>,
    ) -> Result<Self, PlatformSyncError> {
        let http = reqwest::Client::builder()
            .timeout(TIMEOUT)
            .build()
            .map_err(|_| PlatformSyncError::NotConfigured)?;
        Ok(Self {
            http,
            base_url: base_url.into(),
            token: token.into(),
        })
    }

    fn configured(&self) -> bool {
        !self.token.is_empty()
    }

    /// Look up a Steam or Discord ID. `linked: false` → `NotLinked`; any HTTP
    /// error → `NotConfigured` (matching Go).
    pub async fn lookup(&self, id: &str) -> Result<Lookup, PlatformSyncError> {
        if !self.configured() {
            return Err(PlatformSyncError::NotConfigured);
        }
        let mut url = Url::parse(&self.base_url).map_err(|_| PlatformSyncError::NotConfigured)?;
        url.query_pairs_mut()
            .append_pair("id", id)
            .append_pair("token", &self.token);

        let resp = self
            .http
            .get(url)
            .send()
            .await
            .map_err(|_| PlatformSyncError::NotConfigured)?;
        if !resp.status().is_success() {
            return Err(PlatformSyncError::NotConfigured);
        }
        let bytes = resp.bytes().await.map_err(|_| PlatformSyncError::NotConfigured)?;
        let raw: RawResponse =
            serde_json::from_slice(&bytes).map_err(|_| PlatformSyncError::NotConfigured)?;

        let lookup = Lookup {
            linked: raw.linked,
            steam_id: raw.steam_id,
            discord_id: raw.discord_id,
            previous_steam_ids: split_previous(&raw.previous_steam_ids),
            previous_discord_ids: split_previous(&raw.previous_discord_ids),
        };
        if !lookup.linked {
            return Err(PlatformSyncError::NotLinked);
        }
        Ok(lookup)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use wiremock::matchers::{method, query_param};
    use wiremock::{Mock, MockServer, ResponseTemplate};

    #[test]
    fn split_previous_handles_na_and_csv() {
        assert_eq!(split_previous("NA"), Vec::<String>::new());
        assert_eq!(split_previous(""), Vec::<String>::new());
        assert_eq!(split_previous("1, 2 ,3"), vec!["1", "2", "3"]);
        assert_eq!(split_previous("1,NA,,2"), vec!["1", "2"]);
    }

    #[tokio::test]
    async fn empty_token_is_not_configured() {
        let c = Client::new("").unwrap();
        assert!(matches!(c.lookup("1").await, Err(PlatformSyncError::NotConfigured)));
    }

    #[tokio::test]
    async fn lookup_parses_previous_ids_and_sends_id_token() {
        let server = MockServer::start().await;
        Mock::given(method("GET"))
            .and(query_param("id", "76561198000000000"))
            .and(query_param("token", "tok"))
            .respond_with(ResponseTemplate::new(200).set_body_json(serde_json::json!({
                "linked": true,
                "steam_id": "76561198000000000",
                "discord_id": "123",
                "previous_steam_ids": "76561198000000001,76561198000000002",
                "previous_discord_ids": "NA"
            })))
            .mount(&server)
            .await;
        let c = Client::with_base_url(server.uri(), "tok").unwrap();
        let l = c.lookup("76561198000000000").await.unwrap();
        assert!(l.linked);
        assert_eq!(l.discord_id, "123");
        assert_eq!(
            l.previous_steam_ids,
            vec!["76561198000000001", "76561198000000002"]
        );
        assert!(l.previous_discord_ids.is_empty(), "NA -> empty");
    }

    #[tokio::test]
    async fn not_linked_maps_to_not_linked() {
        let server = MockServer::start().await;
        Mock::given(method("GET"))
            .respond_with(ResponseTemplate::new(200).set_body_json(serde_json::json!({"linked": false})))
            .mount(&server)
            .await;
        let c = Client::with_base_url(server.uri(), "tok").unwrap();
        assert!(matches!(c.lookup("1").await, Err(PlatformSyncError::NotLinked)));
    }
}
