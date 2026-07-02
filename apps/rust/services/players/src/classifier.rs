//! Search-box input classification + identifier extraction, mirroring the Go
//! `internal/players/classifier.go`. Order matters: URL forms before bare digits,
//! 17-digit = Steam even though it fits the BM range.

use url::Url;

/// The shape of a raw search-box input.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum QueryKind {
    Empty,
    SteamId,
    BmId,
    SteamUrl,
    SteamVanity,
    Name,
}

/// A parsed steamcommunity URL.
pub enum SteamUrl {
    /// `/profiles/{steam64}`.
    Profiles(String),
    /// `/id/{vanity}` — needs Steam API resolution.
    Vanity(String),
}

fn is_digits(s: &str, min: usize, max: usize) -> bool {
    (min..=max).contains(&s.len()) && s.bytes().all(|b| b.is_ascii_digit())
}

fn is_vanity_seg(s: &str) -> bool {
    (1..=32).contains(&s.len())
        && s.bytes().all(|b| b.is_ascii_alphanumeric() || b == b'_' || b == b'-')
}

/// A bare Steam vanity token: starts with a letter, total length 3-32,
/// alphanumerics plus `_`/`-`.
fn is_bare_vanity(s: &str) -> bool {
    (3..=32).contains(&s.len())
        && s.as_bytes()[0].is_ascii_alphabetic()
        && s.bytes().all(|b| b.is_ascii_alphanumeric() || b == b'_' || b == b'-')
}

fn host_segments(q: &str, allowed_hosts: &[&str]) -> Option<Vec<String>> {
    let u = Url::parse(q.trim()).ok()?;
    if !matches!(u.scheme(), "http" | "https") {
        return None;
    }
    let host = u.host_str()?;
    if !allowed_hosts.contains(&host) {
        return None;
    }
    Some(u.path_segments()?.filter(|s| !s.is_empty()).map(String::from).collect())
}

/// Parse a steamcommunity `/profiles/{id}` or `/id/{vanity}` URL.
pub fn parse_steam_url(q: &str) -> Option<SteamUrl> {
    let segs = host_segments(q, &["steamcommunity.com", "www.steamcommunity.com"])?;
    match segs.as_slice() {
        [kind, value] if kind == "profiles" && is_digits(value, 17, 17) => {
            Some(SteamUrl::Profiles(value.clone()))
        }
        [kind, value] if kind == "id" && is_vanity_seg(value) => Some(SteamUrl::Vanity(value.clone())),
        _ => None,
    }
}

/// Parse a battlemetrics `/players/{id}` (or `/rcon/players/{id}`) URL → BM ID.
pub fn parse_bm_url(q: &str) -> Option<String> {
    let segs = host_segments(q, &["battlemetrics.com", "www.battlemetrics.com"])?;
    let id = match segs.as_slice() {
        [players, id] if players == "players" => id,
        [rcon, players, id] if rcon == "rcon" && players == "players" => id,
        _ => return None,
    };
    is_digits(id, 6, 16).then(|| id.clone())
}

/// Classify a raw query.
pub fn classify(input: &str) -> QueryKind {
    let q = input.trim();
    if q.is_empty() {
        return QueryKind::Empty;
    }
    if parse_steam_url(q).is_some() {
        return QueryKind::SteamUrl;
    }
    if parse_bm_url(q).is_some() {
        return QueryKind::BmId;
    }
    if is_digits(q, 17, 17) {
        return QueryKind::SteamId;
    }
    if is_digits(q, 6, 16) {
        return QueryKind::BmId;
    }
    if is_bare_vanity(q) {
        return QueryKind::SteamVanity;
    }
    QueryKind::Name
}

/// Extract the BM ID from bare digits or a battlemetrics URL.
pub fn extract_bm_id(q: &str) -> Option<String> {
    let q = q.trim();
    if is_digits(q, 6, 16) {
        return Some(q.to_string());
    }
    parse_bm_url(q)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn classification_order() {
        assert_eq!(classify(""), QueryKind::Empty);
        assert_eq!(classify("   "), QueryKind::Empty);
        assert_eq!(classify("76561198000000000"), QueryKind::SteamId); // 17 digits
        assert_eq!(classify("123456"), QueryKind::BmId); // 6-16 digits
        assert_eq!(classify("https://steamcommunity.com/profiles/76561198000000000"), QueryKind::SteamUrl);
        assert_eq!(classify("https://steamcommunity.com/id/gaben/"), QueryKind::SteamUrl);
        assert_eq!(classify("https://www.battlemetrics.com/players/123456"), QueryKind::BmId);
        assert_eq!(classify("https://battlemetrics.com/rcon/players/123456"), QueryKind::BmId);
        assert_eq!(classify("gaben"), QueryKind::SteamVanity);
        assert_eq!(classify("ash"), QueryKind::SteamVanity); // 3 chars, letter-first
        assert_eq!(classify("hi"), QueryKind::Name); // too short for vanity
        assert_eq!(classify("some player name"), QueryKind::Name); // spaces
        assert_eq!(classify("123"), QueryKind::Name); // <6 digits, not bm/steam
    }

    #[test]
    fn steam_url_extracts_profiles_and_vanity() {
        match parse_steam_url("https://steamcommunity.com/profiles/76561198000000000/") {
            Some(SteamUrl::Profiles(id)) => assert_eq!(id, "76561198000000000"),
            _ => panic!("expected profiles"),
        }
        match parse_steam_url("http://steamcommunity.com/id/gaben") {
            Some(SteamUrl::Vanity(v)) => assert_eq!(v, "gaben"),
            _ => panic!("expected vanity"),
        }
        assert!(parse_steam_url("https://evil.com/profiles/76561198000000000").is_none());
    }

    #[test]
    fn bm_id_extraction() {
        assert_eq!(extract_bm_id("123456").as_deref(), Some("123456"));
        assert_eq!(
            extract_bm_id("https://battlemetrics.com/players/987654").as_deref(),
            Some("987654")
        );
        assert_eq!(extract_bm_id("notanid").as_deref(), None);
    }
}
