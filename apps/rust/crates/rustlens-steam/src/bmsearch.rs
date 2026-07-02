//! bmsearch.io resolver — the **primary** Steam↔BM source (public, no-auth
//! Supabase/PostgREST API). Mirrors the Go `internal/bmsearch` client, including
//! the HYBRID cold-lookup: a cache miss enqueues an async lookup and polls until
//! done or the inline budget elapses, returning [`MatchError::Pending`] on
//! timeout so the caller can detach a background backfill. Sent **directly**.

use std::time::Duration;

use async_trait::async_trait;
use serde::Deserialize;
use url::Url;

use crate::steammatch::{MatchError, Resolver};

/// bmsearch.io API base (Go `BM_SEARCH_URL` default).
pub const DEFAULT_BASE_URL: &str = "https://bmsearch.io/api";
/// Poll cadence for `lookup_status` (Go `pollInterval`).
pub const POLL_INTERVAL: Duration = Duration::from_millis(700);
const CLIENT_TIMEOUT: Duration = Duration::from_secs(15);

fn is_terminal_failure(status: &str) -> bool {
    matches!(
        status.to_ascii_lowercase().as_str(),
        "failed" | "no_match" | "not_found" | "error"
    )
}

fn number_to_id(n: &Option<serde_json::Number>) -> String {
    n.as_ref()
        .and_then(|x| x.as_i64())
        .map(|v| v.to_string())
        .unwrap_or_default()
}

#[derive(Deserialize, Default)]
struct ProfileRow {
    #[serde(default)]
    battlemetrics_id: Option<serde_json::Number>,
    #[serde(default)]
    steam_id: Option<String>,
}

#[derive(Deserialize, Default)]
struct LookupResponse {
    #[serde(default)]
    queue_id: Option<serde_json::Number>,
    #[serde(default)]
    profile_id: Option<serde_json::Number>,
}

#[derive(Deserialize, Default)]
struct LookupStatusResponse {
    #[serde(default)]
    status: String,
    #[serde(default)]
    profile_id: Option<serde_json::Number>,
    #[serde(default)]
    error: Option<String>,
}

/// The bmsearch.io client. `default_wait` bounds cold-lookup polling (Go's
/// `BM_SEARCH_INLINE_TIMEOUT_MS`).
#[derive(Clone)]
pub struct Client {
    http: reqwest::Client,
    base_url: String,
    default_wait: Duration,
    poll_interval: Duration,
}

impl Client {
    /// Build a client. `base_url` has any trailing `/` trimmed; `default_wait`
    /// is the inline polling budget.
    pub fn new(base_url: impl Into<String>, default_wait: Duration) -> Result<Self, MatchError> {
        let http = reqwest::Client::builder()
            .timeout(CLIENT_TIMEOUT)
            .build()
            .map_err(|e| MatchError::Other(e.to_string()))?;
        Ok(Self {
            http,
            base_url: base_url.into().trim_end_matches('/').to_string(),
            default_wait,
            poll_interval: POLL_INTERVAL,
        })
    }

    /// Override the poll interval (tests use a tiny value).
    pub fn with_poll_interval(mut self, interval: Duration) -> Self {
        self.poll_interval = interval;
        self
    }

    fn url(&self, path: &str) -> Result<Url, MatchError> {
        Url::parse(&format!("{}{}", self.base_url, path)).map_err(|e| MatchError::Other(e.to_string()))
    }

    /// Fetch the first `/profiles` row matching a single `eq.` filter, with the
    /// given `select`.
    async fn first_profile(&self, filter_key: &str, id: &str, select: &str) -> Result<Option<ProfileRow>, MatchError> {
        let mut url = self.url("/profiles")?;
        url.query_pairs_mut()
            .append_pair(filter_key, &format!("eq.{id}"))
            .append_pair("select", select)
            .append_pair("limit", "1");
        let rows: Vec<ProfileRow> = self.get_json(url).await?;
        Ok(rows.into_iter().next())
    }

    async fn profile_bmid_by_steam(&self, steam_id: &str) -> Result<String, MatchError> {
        Ok(self
            .first_profile("steam_id", steam_id, "battlemetrics_id")
            .await?
            .map(|r| number_to_id(&r.battlemetrics_id))
            .unwrap_or_default())
    }

    async fn profile_bmid_by_id(&self, profile_id: &str) -> Result<String, MatchError> {
        Ok(self
            .first_profile("id", profile_id, "battlemetrics_id")
            .await?
            .map(|r| number_to_id(&r.battlemetrics_id))
            .unwrap_or_default())
    }

    async fn profile_steam_by_bmid(&self, bm_id: &str) -> Result<String, MatchError> {
        Ok(self
            .first_profile("battlemetrics_id", bm_id, "steam_id")
            .await?
            .and_then(|r| r.steam_id)
            .unwrap_or_default())
    }

    async fn enqueue_lookup(&self, steam_id: &str) -> Result<(String, String), MatchError> {
        let url = self.url("/rpc/lookup")?;
        let resp = self
            .http
            .post(url)
            .json(&serde_json::json!({ "p_steam_input": steam_id, "p_force": false }))
            .send()
            .await
            .map_err(|e| MatchError::Other(e.to_string()))?;
        let status = resp.status().as_u16();
        if !(200..300).contains(&status) {
            return Err(MatchError::Other(format!("bmsearch lookup status {status}")));
        }
        let bytes = resp.bytes().await.map_err(|e| MatchError::Other(e.to_string()))?;
        let r: LookupResponse =
            serde_json::from_slice(&bytes).map_err(|e| MatchError::Other(e.to_string()))?;
        Ok((number_to_id(&r.queue_id), number_to_id(&r.profile_id)))
    }

    async fn lookup_status(&self, queue_id: &str) -> Result<LookupStatusResponse, MatchError> {
        let mut url = self.url("/rpc/lookup_status")?;
        url.query_pairs_mut().append_pair("p_queue_id", queue_id);
        self.get_json(url).await
    }

    /// Poll `lookup_status` until done/terminal, bounded by `default_wait`. On
    /// timeout → [`MatchError::Pending`] (the caller backfills).
    async fn poll_lookup(&self, queue_id: &str) -> Result<String, MatchError> {
        if queue_id.is_empty() {
            return Err(MatchError::NotFound);
        }
        match tokio::time::timeout(self.default_wait, self.poll_loop(queue_id)).await {
            Ok(r) => r,
            Err(_) => Err(MatchError::Pending),
        }
    }

    async fn poll_loop(&self, queue_id: &str) -> Result<String, MatchError> {
        loop {
            match self.lookup_status(queue_id).await {
                Ok(st) => {
                    if st.error.as_deref().is_some_and(|e| !e.is_empty()) {
                        return Err(MatchError::NotFound);
                    }
                    if is_terminal_failure(&st.status) {
                        return Err(MatchError::NotFound);
                    }
                    if st.status == "done" {
                        let profile_id = number_to_id(&st.profile_id);
                        if profile_id.is_empty() {
                            return Err(MatchError::NotFound);
                        }
                        let bm = self.profile_bmid_by_id(&profile_id).await?;
                        return if bm.is_empty() {
                            Err(MatchError::NotFound)
                        } else {
                            Ok(bm)
                        };
                    }
                    // Non-terminal (pending/queued): keep polling.
                }
                // Transient status error: keep polling until the deadline.
                Err(MatchError::Other(_)) => {}
                Err(e) => return Err(e),
            }
            tokio::time::sleep(self.poll_interval).await;
        }
    }

    async fn get_json<T: for<'de> Deserialize<'de>>(&self, url: Url) -> Result<T, MatchError> {
        let resp = self
            .http
            .get(url)
            .send()
            .await
            .map_err(|e| MatchError::Other(e.to_string()))?;
        let status = resp.status().as_u16();
        if !(200..300).contains(&status) {
            return Err(MatchError::Other(format!("bmsearch status {status}")));
        }
        let bytes = resp.bytes().await.map_err(|e| MatchError::Other(e.to_string()))?;
        serde_json::from_slice(&bytes).map_err(|e| MatchError::Other(e.to_string()))
    }
}

#[async_trait]
impl Resolver for Client {
    async fn steam_to_bm(&self, steam_id: &str) -> Result<String, MatchError> {
        let steam_id = steam_id.trim();
        if steam_id.is_empty() {
            return Err(MatchError::NotFound);
        }
        // 1. Cache read.
        let cached = self.profile_bmid_by_steam(steam_id).await?;
        if !cached.is_empty() {
            return Ok(cached);
        }
        // 2. Enqueue an async lookup; a resolved profile may come back instantly.
        let (queue_id, profile_id) = self.enqueue_lookup(steam_id).await?;
        if !profile_id.is_empty() {
            if let Ok(bm) = self.profile_bmid_by_id(&profile_id).await {
                if !bm.is_empty() {
                    return Ok(bm);
                }
            }
        }
        // 3. Poll until done or the inline budget elapses (→ Pending).
        self.poll_lookup(&queue_id).await
    }

    async fn bm_to_steam(&self, bm_id: &str) -> Result<String, MatchError> {
        // Cache-only: the lookup RPC accepts Steam input only.
        let bm_id = bm_id.trim();
        if bm_id.is_empty() {
            return Err(MatchError::NotFound);
        }
        let steam = self.profile_steam_by_bmid(bm_id).await?;
        if steam.is_empty() {
            Err(MatchError::NotFound)
        } else {
            Ok(steam)
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use wiremock::matchers::{body_partial_json, method, path, query_param};
    use wiremock::{Mock, MockServer, ResponseTemplate};

    fn client(uri: String, wait_ms: u64) -> Client {
        Client::new(uri, Duration::from_millis(wait_ms))
            .unwrap()
            .with_poll_interval(Duration::from_millis(5))
    }

    #[tokio::test]
    async fn steam_to_bm_cache_hit() {
        let server = MockServer::start().await;
        Mock::given(method("GET"))
            .and(path("/profiles"))
            .and(query_param("steam_id", "eq.76561198000000000"))
            .and(query_param("select", "battlemetrics_id"))
            .respond_with(
                ResponseTemplate::new(200).set_body_json(serde_json::json!([{"battlemetrics_id": 12345}])),
            )
            .mount(&server)
            .await;
        let c = client(server.uri(), 1000);
        assert_eq!(c.steam_to_bm("76561198000000000").await.unwrap(), "12345");
    }

    #[tokio::test]
    async fn bm_to_steam_cache_only() {
        let server = MockServer::start().await;
        Mock::given(method("GET"))
            .and(path("/profiles"))
            .and(query_param("battlemetrics_id", "eq.777"))
            .and(query_param("select", "steam_id"))
            .respond_with(
                ResponseTemplate::new(200)
                    .set_body_json(serde_json::json!([{"steam_id": "76561198000000000"}])),
            )
            .mount(&server)
            .await;
        let c = client(server.uri(), 1000);
        assert_eq!(c.bm_to_steam("777").await.unwrap(), "76561198000000000");
    }

    #[tokio::test]
    async fn cold_lookup_enqueue_poll_done_resolves() {
        let server = MockServer::start().await;
        // cache miss for steam_id
        Mock::given(method("GET"))
            .and(path("/profiles"))
            .and(query_param("steam_id", "eq.1"))
            .respond_with(ResponseTemplate::new(200).set_body_json(serde_json::json!([])))
            .mount(&server)
            .await;
        // enqueue -> queue_id 5 (no immediate profile_id)
        Mock::given(method("POST"))
            .and(path("/rpc/lookup"))
            .and(body_partial_json(serde_json::json!({"p_steam_input":"1","p_force":false})))
            .respond_with(ResponseTemplate::new(200).set_body_json(serde_json::json!({"queue_id": 5})))
            .mount(&server)
            .await;
        // status -> done with profile_id 99
        Mock::given(method("GET"))
            .and(path("/rpc/lookup_status"))
            .and(query_param("p_queue_id", "5"))
            .respond_with(
                ResponseTemplate::new(200).set_body_json(serde_json::json!({"status":"done","profile_id":99})),
            )
            .mount(&server)
            .await;
        // profile by id 99 -> bm 77
        Mock::given(method("GET"))
            .and(path("/profiles"))
            .and(query_param("id", "eq.99"))
            .respond_with(ResponseTemplate::new(200).set_body_json(serde_json::json!([{"battlemetrics_id": 77}])))
            .mount(&server)
            .await;
        let c = client(server.uri(), 2000);
        assert_eq!(c.steam_to_bm("1").await.unwrap(), "77");
    }

    #[tokio::test]
    async fn cold_lookup_terminal_failure_is_not_found() {
        let server = MockServer::start().await;
        Mock::given(method("GET"))
            .and(path("/profiles"))
            .and(query_param("steam_id", "eq.1"))
            .respond_with(ResponseTemplate::new(200).set_body_json(serde_json::json!([])))
            .mount(&server)
            .await;
        Mock::given(method("POST"))
            .and(path("/rpc/lookup"))
            .respond_with(ResponseTemplate::new(200).set_body_json(serde_json::json!({"queue_id": 5})))
            .mount(&server)
            .await;
        Mock::given(method("GET"))
            .and(path("/rpc/lookup_status"))
            .respond_with(ResponseTemplate::new(200).set_body_json(serde_json::json!({"status":"no_match"})))
            .mount(&server)
            .await;
        let c = client(server.uri(), 2000);
        assert!(matches!(c.steam_to_bm("1").await, Err(MatchError::NotFound)));
    }

    #[tokio::test]
    async fn cold_lookup_pending_past_deadline() {
        let server = MockServer::start().await;
        Mock::given(method("GET"))
            .and(path("/profiles"))
            .and(query_param("steam_id", "eq.1"))
            .respond_with(ResponseTemplate::new(200).set_body_json(serde_json::json!([])))
            .mount(&server)
            .await;
        Mock::given(method("POST"))
            .and(path("/rpc/lookup"))
            .respond_with(ResponseTemplate::new(200).set_body_json(serde_json::json!({"queue_id": 5})))
            .mount(&server)
            .await;
        // always pending -> never resolves -> deadline hits -> Pending
        Mock::given(method("GET"))
            .and(path("/rpc/lookup_status"))
            .respond_with(ResponseTemplate::new(200).set_body_json(serde_json::json!({"status":"pending"})))
            .mount(&server)
            .await;
        let c = client(server.uri(), 60); // tiny inline budget
        assert!(matches!(c.steam_to_bm("1").await, Err(MatchError::Pending)));
    }
}
