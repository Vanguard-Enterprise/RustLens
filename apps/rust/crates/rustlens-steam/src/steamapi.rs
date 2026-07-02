//! Steam Web API client, mirroring the Go `internal/steamapi` client. All
//! requests are sent **directly** (never proxied) with a 10s timeout and the
//! `STEAM_API_KEY`. Steam IDs are validated as `^\d{17}$`.

use std::time::Duration;

use serde::Deserialize;
use url::Url;

/// Steam Web API base (Go `defaultBaseURL`).
pub const DEFAULT_BASE_URL: &str = "https://api.steampowered.com";
const TIMEOUT: Duration = Duration::from_secs(10);

#[derive(Debug, thiserror::Error)]
pub enum SteamError {
    #[error("steam api key required")]
    KeyRequired,
    #[error("steam friend list is private")]
    FriendListPrivate,
    #[error("steam returned status {0}")]
    Status(u16),
    #[error(transparent)]
    Http(#[from] reqwest::Error),
    #[error("decode steam response: {0}")]
    Decode(#[from] serde_json::Error),
    #[error("build url: {0}")]
    Url(#[from] url::ParseError),
}

/// A player profile summary (`GetPlayerSummaries`).
#[derive(Debug, Clone, Deserialize)]
pub struct PlayerSummary {
    #[serde(rename = "steamid")]
    pub steam_id: String,
    #[serde(rename = "personaname", default)]
    pub persona_name: String,
    #[serde(rename = "avatarfull", default)]
    pub avatar_full: String,
    /// `1` = private; consumers skip `get_owned_games` in that case.
    #[serde(rename = "communityvisibilitystate", default)]
    pub community_visibility_state: i32,
}

/// An owned game (`GetOwnedGames`); `playtime_forever` is in minutes.
#[derive(Debug, Clone, Deserialize)]
pub struct OwnedGame {
    #[serde(rename = "appid")]
    pub app_id: i32,
    #[serde(default)]
    pub name: String,
    #[serde(rename = "playtime_forever", default)]
    pub playtime_forever: i64,
}

/// A player ban record (`GetPlayerBans`). Note the PascalCase JSON keys.
#[derive(Debug, Clone, Deserialize)]
pub struct PlayerBan {
    #[serde(rename = "SteamId")]
    pub steam_id: String,
    #[serde(rename = "CommunityBanned", default)]
    pub community_banned: bool,
    #[serde(rename = "VACBanned", default)]
    pub vac_banned: bool,
    #[serde(rename = "NumberOfVACBans", default)]
    pub number_of_vac_bans: i32,
    #[serde(rename = "DaysSinceLastBan", default)]
    pub days_since_last_ban: i32,
    #[serde(rename = "NumberOfGameBans", default)]
    pub number_of_game_bans: i32,
    #[serde(rename = "EconomyBan", default)]
    pub economy_ban: String,
}

/// The Steam Web API client. Cheap to clone (holds a `reqwest::Client`).
#[derive(Clone)]
pub struct Client {
    http: reqwest::Client,
    base_url: String,
    api_key: String,
}

impl Client {
    /// Build a client. An empty `api_key` makes every method return
    /// [`SteamError::KeyRequired`].
    pub fn new(api_key: impl Into<String>) -> Result<Self, SteamError> {
        Ok(Self {
            http: reqwest::Client::builder().timeout(TIMEOUT).build()?,
            base_url: DEFAULT_BASE_URL.to_string(),
            api_key: api_key.into(),
        })
    }

    /// Override the base URL (tests point this at a mock server).
    pub fn with_base_url(mut self, base_url: impl Into<String>) -> Self {
        self.base_url = base_url.into();
        self
    }

    fn require_key(&self) -> Result<&str, SteamError> {
        if self.api_key.is_empty() {
            Err(SteamError::KeyRequired)
        } else {
            Ok(&self.api_key)
        }
    }

    /// `GET /ISteamUser/GetPlayerSummaries/v2/` → the first player summary, or
    /// `None` when the profile isn't returned.
    pub async fn get_player_summaries(
        &self,
        steam_id: &str,
    ) -> Result<Option<PlayerSummary>, SteamError> {
        let key = self.require_key()?;
        let mut url = self.url("/ISteamUser/GetPlayerSummaries/v2/")?;
        url.query_pairs_mut()
            .append_pair("key", key)
            .append_pair("steamids", steam_id);

        #[derive(Deserialize)]
        struct Wrapper {
            response: Inner,
        }
        #[derive(Deserialize, Default)]
        #[serde(default)]
        struct Inner {
            players: Vec<PlayerSummary>,
        }
        let w: Wrapper = self.get_json(url).await?;
        Ok(w.response.players.into_iter().next())
    }

    /// `GET /IPlayerService/GetOwnedGames/v1/` → owned games with playtime.
    pub async fn get_owned_games(&self, steam_id: &str) -> Result<Vec<OwnedGame>, SteamError> {
        let key = self.require_key()?;
        let mut url = self.url("/IPlayerService/GetOwnedGames/v1/")?;
        url.query_pairs_mut()
            .append_pair("key", key)
            .append_pair("steamid", steam_id)
            .append_pair("include_appinfo", "true")
            .append_pair("include_played_free_games", "true")
            .append_pair("format", "json");

        #[derive(Deserialize)]
        struct Wrapper {
            response: Inner,
        }
        #[derive(Deserialize, Default)]
        #[serde(default)]
        struct Inner {
            games: Vec<OwnedGame>,
        }
        let w: Wrapper = self.get_json(url).await?;
        Ok(w.response.games)
    }

    /// `GET /ISteamUser/GetPlayerBans/v1/` → ban records for the given IDs.
    pub async fn get_player_bans(&self, steam_ids: &[&str]) -> Result<Vec<PlayerBan>, SteamError> {
        let key = self.require_key()?;
        let mut url = self.url("/ISteamUser/GetPlayerBans/v1/")?;
        url.query_pairs_mut()
            .append_pair("key", key)
            .append_pair("steamids", &steam_ids.join(","));

        #[derive(Deserialize, Default)]
        #[serde(default)]
        struct Wrapper {
            players: Vec<PlayerBan>,
        }
        let w: Wrapper = self.get_json(url).await?;
        Ok(w.players)
    }

    /// `GET /ISteamUser/GetFriendList/v1/` → the player's friend Steam IDs.
    /// A private friend list responds `401` → [`SteamError::FriendListPrivate`].
    pub async fn get_friend_list(&self, steam_id: &str) -> Result<Vec<String>, SteamError> {
        let key = self.require_key()?;
        let mut url = self.url("/ISteamUser/GetFriendList/v1/")?;
        url.query_pairs_mut()
            .append_pair("key", key)
            .append_pair("steamid", steam_id)
            .append_pair("relationship", "friend");

        let resp = self.http.get(url).send().await?;
        let status = resp.status().as_u16();
        if status == 401 {
            return Err(SteamError::FriendListPrivate);
        }
        if !(200..300).contains(&status) {
            return Err(SteamError::Status(status));
        }
        #[derive(Deserialize, Default)]
        #[serde(default)]
        struct Wrapper {
            friendslist: FriendList,
        }
        #[derive(Deserialize, Default)]
        #[serde(default)]
        struct FriendList {
            friends: Vec<Friend>,
        }
        #[derive(Deserialize)]
        struct Friend {
            steamid: String,
        }
        let bytes = resp.bytes().await?;
        let w: Wrapper = serde_json::from_slice(&bytes)?;
        Ok(w.friendslist.friends.into_iter().map(|f| f.steamid).collect())
    }

    /// `GET /ISteamUser/ResolveVanityURL/v1/` → the resolved 17-digit Steam ID,
    /// or `None` when the vanity name doesn't resolve (`success != 1`).
    pub async fn resolve_vanity_url(&self, vanity: &str) -> Result<Option<String>, SteamError> {
        let key = self.require_key()?;
        let mut url = self.url("/ISteamUser/ResolveVanityURL/v1/")?;
        url.query_pairs_mut()
            .append_pair("key", key)
            .append_pair("vanityurl", vanity);

        #[derive(Deserialize)]
        struct Wrapper {
            response: Inner,
        }
        #[derive(Deserialize, Default)]
        #[serde(default)]
        struct Inner {
            success: i32,
            steamid: String,
        }
        let w: Wrapper = self.get_json(url).await?;
        if w.response.success == 1 && !w.response.steamid.is_empty() {
            Ok(Some(w.response.steamid))
        } else {
            Ok(None)
        }
    }

    fn url(&self, path: &str) -> Result<Url, SteamError> {
        Ok(Url::parse(&format!("{}{}", self.base_url, path))?)
    }

    async fn get_json<T: for<'de> Deserialize<'de>>(&self, url: Url) -> Result<T, SteamError> {
        let resp = self.http.get(url).send().await?;
        let status = resp.status().as_u16();
        if !(200..300).contains(&status) {
            return Err(SteamError::Status(status));
        }
        let bytes = resp.bytes().await?;
        Ok(serde_json::from_slice(&bytes)?)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use wiremock::matchers::{method, path, query_param};
    use wiremock::{Mock, MockServer, ResponseTemplate};

    #[tokio::test]
    async fn empty_key_is_rejected_without_a_request() {
        let c = Client::new("").unwrap();
        assert!(matches!(
            c.get_player_summaries("76561198000000000").await,
            Err(SteamError::KeyRequired)
        ));
    }

    #[tokio::test]
    async fn player_summary_parses_and_sends_key_and_steamids() {
        let server = MockServer::start().await;
        Mock::given(method("GET"))
            .and(path("/ISteamUser/GetPlayerSummaries/v2/"))
            .and(query_param("key", "K"))
            .and(query_param("steamids", "76561198000000000"))
            .respond_with(ResponseTemplate::new(200).set_body_json(serde_json::json!({
                "response": {"players": [
                    {"steamid":"76561198000000000","personaname":"Alice","avatarfull":"http://a","communityvisibilitystate":3}
                ]}
            })))
            .mount(&server)
            .await;
        let c = Client::new("K").unwrap().with_base_url(server.uri());
        let s = c.get_player_summaries("76561198000000000").await.unwrap().unwrap();
        assert_eq!(s.steam_id, "76561198000000000");
        assert_eq!(s.persona_name, "Alice");
        assert_eq!(s.community_visibility_state, 3);
    }

    #[tokio::test]
    async fn private_profile_visibility_state_is_one() {
        let server = MockServer::start().await;
        Mock::given(method("GET"))
            .and(path("/ISteamUser/GetPlayerSummaries/v2/"))
            .respond_with(ResponseTemplate::new(200).set_body_json(serde_json::json!({
                "response": {"players": [{"steamid":"1","communityvisibilitystate":1}]}
            })))
            .mount(&server)
            .await;
        let c = Client::new("K").unwrap().with_base_url(server.uri());
        let s = c.get_player_summaries("1").await.unwrap().unwrap();
        assert_eq!(s.community_visibility_state, 1, "private profile flag");
    }

    #[tokio::test]
    async fn player_bans_parse_pascalcase() {
        let server = MockServer::start().await;
        Mock::given(method("GET"))
            .and(path("/ISteamUser/GetPlayerBans/v1/"))
            .and(query_param("steamids", "1,2"))
            .respond_with(ResponseTemplate::new(200).set_body_json(serde_json::json!({
                "players": [
                    {"SteamId":"1","CommunityBanned":false,"VACBanned":true,"NumberOfVACBans":2,"DaysSinceLastBan":10,"NumberOfGameBans":1,"EconomyBan":"none"}
                ]
            })))
            .mount(&server)
            .await;
        let c = Client::new("K").unwrap().with_base_url(server.uri());
        let bans = c.get_player_bans(&["1", "2"]).await.unwrap();
        assert_eq!(bans.len(), 1);
        assert_eq!(bans[0].steam_id, "1");
        assert!(bans[0].vac_banned);
        assert_eq!(bans[0].number_of_game_bans, 1);
    }

    #[tokio::test]
    async fn friend_list_401_is_private() {
        let server = MockServer::start().await;
        Mock::given(method("GET"))
            .and(path("/ISteamUser/GetFriendList/v1/"))
            .respond_with(ResponseTemplate::new(401))
            .mount(&server)
            .await;
        let c = Client::new("K").unwrap().with_base_url(server.uri());
        assert!(matches!(
            c.get_friend_list("1").await,
            Err(SteamError::FriendListPrivate)
        ));
    }

    #[tokio::test]
    async fn resolve_vanity_success_and_failure() {
        let server = MockServer::start().await;
        Mock::given(method("GET"))
            .and(path("/ISteamUser/ResolveVanityURL/v1/"))
            .and(query_param("vanityurl", "gaben"))
            .respond_with(ResponseTemplate::new(200).set_body_json(serde_json::json!({
                "response": {"success": 1, "steamid": "76561197960287930"}
            })))
            .mount(&server)
            .await;
        let c = Client::new("K").unwrap().with_base_url(server.uri());
        assert_eq!(
            c.resolve_vanity_url("gaben").await.unwrap().as_deref(),
            Some("76561197960287930")
        );

        let server2 = MockServer::start().await;
        Mock::given(method("GET"))
            .and(path("/ISteamUser/ResolveVanityURL/v1/"))
            .respond_with(ResponseTemplate::new(200).set_body_json(serde_json::json!({
                "response": {"success": 42}
            })))
            .mount(&server2)
            .await;
        let c2 = Client::new("K").unwrap().with_base_url(server2.uri());
        assert_eq!(c2.resolve_vanity_url("nope").await.unwrap(), None);
    }
}
