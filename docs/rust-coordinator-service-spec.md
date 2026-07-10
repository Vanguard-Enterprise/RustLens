# Rust `coordinator` service — port spec (Phase 5, final service)

> Historical: the migration completed on `rework/rust-backend`; see `rust-go-removal-plan.md`.

The fifth and last Rust service in the Go→Rust backend migration. New axum 0.8 + sqlx 0.8 + redis binary at `apps/rust/services/coordinator`, **default PORT 8094**, sharing the **same Postgres and same Redis** as the Go backend. **NO schema changes / NO migrations.** JSON responses match the Go handlers **byte-for-byte**, and the **Redis key schemes match byte-for-byte** — the Discord bot workers and other pods share this Redis state (shard leases, rate-limit counters). `cargo clippy --all-targets` warning-clean; parameterized queries only.

## Scope (the leftover bot/shard surface still on Go)

| Cluster | Routes / job | Auth |
|---|---|---|
| Shard coordination | `POST /api/internal/bot/shards/{register,heartbeat,release}`, `GET /assignments` | bot-JWT |
| Owner stats | `GET /api/internal/bot/owner/stats` | bot-JWT |
| Rate-limit consume | `POST /api/internal/bot/setup/rate-limit/consume` | bot-JWT |
| Discord webhook events | `POST /api/discord/webhook-events` | Ed25519 signature (in-handler) |
| Cleanup | hourly retention sweep (background worker) | — |

Shard state is **entirely in Redis** (no Postgres, no background sweeper — dead workers are reaped lazily inside `/assignments`). Owner stats reads the live shard stats from Redis + 7 Postgres counts. Rate-limit is a Redis fixed-window limiter + a low-guild fraud detector. The webhook persists user installs into `bot_user_installs`.

## Global conventions

- Bot-JWT gate = `rustlens-botjwt` (HS256, `BOT_SERVICE_JWT_SECRET`, iss/aud), **plus** the shard/owner handlers require a non-nil claims value (`worker_id` used as the fallback worker id) — an API-key-only caller 401s with `missing auth claims`.
- `writeJSON` → `Content-Type: application/json` + trailing `\n`; `map` responses emit **alphabetically-sorted keys** (matters for `/assignments`), struct responses use field order. `writeError` → `{"error":msg}`. Body cap 1 MB → decode fail = `400 invalid request body`.
- `validateSnowflakeValue` = `^\d{17,20}$`.
- **`/assignments` `shardIds` is `null` (not `[]`) when the worker owns no shards** — serialize `Option<Vec<i64>>`, `None → null`.

## Design decisions

- **DC-1 — service + port.** New `apps/rust/services/coordinator`, `CoordinatorConfig` in `rustlens-config`, default PORT **8094**. Env: POSTGRES_URL, REDIS_URL, BOT_SERVICE_JWT_SECRET + iss/aud, BOT_TARGET_SHARDS (4), BOT_HEARTBEAT_TTL_SECONDS (45), DISCORD_APP_PUBLIC_KEY (webhook), cleanup retentions. *(pattern; no user input)*
- **DC-2 — store split.** Owner-stats + cleanup queries + the rate-limit bypass permission check + `bot_user_installs` upsert/remove → `rustlens-db`. Shard lease store + rate-limit/fraud Redis logic live in the service (Redis, byte-for-byte keys). *(no user input)*
- **DC-3 — cleanup worker locking. ✅ RESOLVED: Redis-locked** (SetNX cadence + lock like the servers/guilds pollers; single-pod fallback when Redis absent). Avoids N replicas duplicating the hourly sweep; correctness holds either way (idempotent DELETEs).
- **DC-4 — shard Redis byte-for-byte (critical, no user input).** Replicate exactly: keys `rustlens:bot:workers` / `:worker:heartbeat:<id>` / `:worker:stats:<id>` / `:leases`, the MULTI/EXEC register/heartbeat/release, the `SetLeases` Lua swap, and the deterministic round-robin `AssignShards`. The bot workers depend on these.
- **DC-5 — Ed25519 webhook.** Verify `X-Signature-Ed25519` over `timestamp-header || raw-body` with a ±5-min replay guard, using `DISCORD_APP_PUBLIC_KEY` (hex 32-byte). Needs an ed25519 verify dep (e.g. `ed25519-dalek`) + `hex`. *(no user input)*
- **DC-6 — rate-limit / fraud faithfulness.** Fixed-window (anchored at first INCR, not sliding) limiter with escalating `base*2^(level-1)` cooldowns capped at MaxCooldown; the low-guild fraud detector (member count in `[1, MaxMemberCount)`). Replicate the exact `rl:cd:`/`rl:q:`/`rl:lvl:` and `fraud:<cmd>:<guild>:{count,alerted}` keys + non-atomic command order.

## Slice plan (one PR each onto `rework/rust-backend`, merged + pruned)

- **C-A — skeleton + shard coordination.** `CoordinatorConfig`, service skeleton, bot-JWT gate; the Redis lease store (workers set / heartbeat / stats / leases hash + Lua swap), the round-robin allocator, and the 4 shard routes (register/heartbeat/release/assignments) + `ActiveWorkerStats`.
- **C-B — owner stats + rate-limit consume.** `GET /owner/stats` (shard stats + 7 DB counts), `POST /rate-limit/consume` (Redis limiter + fraud detector + `DiscordHasPermission` bypass checks). New `rustlens-db` owner-stats + discord-permission helpers.
- **C-C — cleanup service.** The hourly Redis-locked (per DC-3) retention sweep of `wipe_reacts` / `track_analytics_sessions_cache` / `command_usage` with the exact cutoffs. New `rustlens-db::cleanup` purge helpers.
- **C-D — Discord webhook events.** `POST /api/discord/webhook-events` with Ed25519 verification + PING/AUTHORIZED/DEAUTHORIZED handling → `bot_user_installs` upsert/remove (extend the existing `rustlens-db::bot_user_installs` module).

*(After C-D: Phase 5 is code-complete → all bot/app/shard surface is in Rust. Only the infra cutover — Dockerfiles/CI/compose/Traefik for :8090–:8094, bot-path reorg, delete Go handlers — remains, HELD for sign-off. That cutover is the whole migration's finish line.)*

---
## Shard coordination

Logic in internal/shards/{service,allocator,heartbeat}.go + store/redis.go; handlers in handlers.go. **ALL state in Redis; NO Postgres, NO background sweeper — dead-worker reaping is lazy, request-driven in /assignments via heartbeat-key TTL.** shards.NewService(redisStore, cfg.BotTargetShards, cfg.BotHeartbeatTTL).

### Common
Routes `/api/internal/bot/shards`, middleware bridgeAPIKeyPerms→requireBotRouteScope→botJWTMiddleware. Bot JWT: HS256 BOT_SERVICE_JWT_SECRET, issuer BOT_JWT_ISSUER(rustlens-bot), audience BOT_JWT_AUDIENCE(rustlens-backend), 2s leeway, claims {service:"bot", worker_id, ...}; service!="bot"→invalid; fail→401 {"error":<err>}. **Every handler: claims=claimsFromContext; nil→401 {"error":"missing auth claims"} (so API-key callers can't use shard routes).** Body cap 1MB; decode fail→400 {"error":"invalid request body"}. writeJSON trailing \n; map responses = alphabetically-sorted keys.

### POST /register RegisterShardWorker (200)
Req {workerId string}. Empty→fall back to claims.WorkerID; still ""→400 {"error":"worker id required"}. leases.MarkWorker(workerID, ttl).
Resp struct (field order) `{"workerId":<id>,"targetShards":<BOT_TARGET_SHARDS=4>,"heartbeatTtlSeconds":<BOT_HEARTBEAT_TTL_SECONDS=45>}`.
Redis MarkWorker (MULTI/EXEC): `SADD rustlens:bot:workers <id>` + `SETEX rustlens:bot:worker:heartbeat:<id> 45 <unixNano>` (unixNano=FormatInt(Now().UnixNano())).

### POST /heartbeat HeartbeatShardWorker (200)
Req {workerId (fallback claims), shardIds []int (IGNORED — health-only), stats *WorkerStatSnapshot omitempty}. WorkerStatSnapshot {uptimeSec int, rssBytes int64, shards [{shardId int, status string, pingMs int, guilds int, members int64}]}. Empty workerID→400 worker id required. leases.RefreshWorker(workerID, ttl); if stats!=nil json.Marshal + SetWorkerStats(workerID, payload, ttl).
Resp map `{"status":"ok"}`.
Redis RefreshWorker (MULTI/EXEC, same as MarkWorker — re-SADD): SADD workers + SETEX heartbeat 45. SetWorkerStats: `SET rustlens:bot:worker:stats:<id> <json> EX 45`.

### POST /release ReleaseShardWorker (200)
Req {workerId (fallback claims)}. Empty→400 worker id required. leases.RemoveWorker + SetLeases(workerID, nil).
Resp map `{"status":"released"}`.
Redis RemoveWorker (MULTI/EXEC): `SREM rustlens:bot:workers <id>` + `DEL heartbeat:<id>` + `DEL stats:<id>`. SetLeases(nil)=Lua swap count=0 (removes all lease fields owned by worker). Released shards NOT reassigned here (unowned until next /assignments).

### GET /assignments GetShardAssignments (200) — ONLY route that computes+writes canonical lease map
Input workerId from `?workerId=`; empty→claims.WorkerID. Service.Assignments:
1. workerId!=""→leases.RefreshWorker FIRST (protect own lease during transient gap).
2. workers=leases.ActiveWorkers() (reaps dead; sorted).
3. prior=CurrentAssignments() (metric snapshot).
4. assignments=AssignShards(totalShards, workers).
5. per (owner,shardIDs): SetLeases(owner, shardIDs).
6. !equal(prior,assignments)→rebalanceCounter (OTel only).
7. current=CurrentAssignments().
8. owned=current[workerID]; sort.Ints; return (owned, totalShards).
Resp map (sorted keys) `{"shardIds":<[]int or null>,"targetShards":4,"workerId":<id>}`. **shardIds: owned shards→sorted non-empty array; NO shards→nil []int→marshals as `null` (never []). Rust: Option<Vec<i64>> None→null.** workerId echoed unconditionally (not validated vs active set).
Redis: RefreshWorker; ActiveWorkers (`SMEMBERS workers`; per member `EXISTS heartbeat:<m>`; 0→reap: RemoveWorker + RemoveLeasesByWorker(`HGETALL leases`→`HDEL` owned fields); else keep; sort.Strings); CurrentAssignments ×2 (`HGETALL rustlens:bot:leases`→map[worker][]int, field=shard Atoi, sort.Ints); SetLeases per owner (Lua).

### Allocation algorithm
totalShards=BOT_TARGET_SHARDS(4), shard IDs 0..totalShards-1. Active worker = in `rustlens:bot:workers` SET AND heartbeat key exists (heartbeated within TTL). Expired lazily reaped. AssignShards (pure round-robin): totalShards<=0||len(workers)==0→{}; sorted=sort.Strings(workers); for i in 0..totalShards: owner=sorted[i%N]; append i. So shard i→sorted[i%N]. 4 shards/2 workers[A,B]: A=[0,2] B=[1,3]. Worker with 0 shards (N>totalShards) absent from map→SetLeases not called for it→old leases persist until reaped/overwritten (HSET overwrites reassigned shard field). No per-worker claim; coordinator recomputes whole distribution each /assignments. Lease hash `rustlens:bot:leases` has NO TTL (durable); heartbeat key expires (SETEX 45). Reassignment latency ≈ heartbeat TTL + survivors' /assignments poll interval. Atomicity: register/heartbeat/release use MULTI/EXEC pipeline; SetLeases single atomic Lua EVAL per worker; overall /assignments rebalance NOT globally atomic (sequence of per-owner evals+HGETALL) but converges (deterministic round-robin + step-1 self-refresh).

### Redis keys
- `rustlens:bot:workers` SET (worker IDs), no TTL.
- `rustlens:bot:worker:heartbeat:<id>` String=UnixNano, TTL 45s (BOT_HEARTBEAT_TTL_SECONDS).
- `rustlens:bot:worker:stats:<id>` String=JSON WorkerStatSnapshot, TTL 45s.
- `rustlens:bot:leases` Hash field=shard-ID str value=worker ID, no TTL.
SetLeases Lua (KEYS[1]=leases, ARGV[1]=worker, ARGV[2]=count, ARGV[3..]=shard strs): HGETALL; for each pair if value==worker HDEL field; then for i 0..count-1 HSET ARGV[3+i] worker; return 0.
ActiveWorkerStats (owner-stats): GetWorkerStats = `MGET stats:<id1> ...` (nil skipped) over ActiveWorkers.

### Notes
No Postgres, no sweeper (reap only in ActiveWorkers from /assignments). shardIds:null (not []) when owns none. workerId echoed unvalidated. heartbeat shardIds ignored. empty body→400. worker id required only when body+claim both empty. trailing \n; TTLs whole seconds (SETEX 45 / SET EX 45). rebalanceCounter OTel-only (omittable).
## Owner stats + cleanup

### Owner stats
Route `GET /api/internal/bot/owner/stats` -> GetOwnerStats (handlers.go). Group `/api/internal/bot/owner`, bot-JWT (bridgeAPIKeyPerms→requireBotRouteScope→botJWTMiddleware). Handler reads claims; nil→401 `{"error":"missing auth claims"}`. No query params.
Flow: workerStats = shardService.ActiveWorkerStats(ctx) (Redis, map workerId→WorkerStatSnapshot); err→500 `{"error":"failed to load worker stats"}`. counts = postgres.OwnerStats(ctx, now.Unix()); err→500 `{"error":"failed to load db counts"}`. Sort worker IDs; per worker sort shards by shardId asc; accumulate guildCount+=shard.guilds, memberCount+=shard.members. targetShards = cfg.BotTargetShards.
Resp `ownerStatsResponse` (200, all fields present):
- guildCount (int, sum shards[].guilds), memberCount (int64, sum shards[].members), configuredGuilds (int64), steamAccountCount (int64), hardLinkCount (int64), uniqueLinkedUsers (int64), trackedServerCount (int64), onlineTrackedPlayers (int64), activeWipeReacts (int64), targetShards (int = cfg.BotTargetShards), workers (array, [] never null).
- workers[] {workerId (string), uptimeSec (int), rssBytes (int64), shards (array — nil slice→null since `append([]T(nil),...)`)}. shards[] {shardId int, status string, pingMs int, guilds int, members int64}.
Worker/shard data is Redis-sourced (ActiveWorkerStats), NOT Postgres.
SQL (owner_stats.go OwnerStats(nowUnix): 7 sequential QueryRow, first err→zero+err):
- configuredGuilds: `SELECT COUNT(*) FROM guild_settings`
- steamAccountCount: `SELECT COUNT(*) FROM steam_accounts`
- hardLinkCount: `SELECT COUNT(*) FROM steam_discord_links WHERE type = 'hard'`
- uniqueLinkedUsers: `SELECT COUNT(DISTINCT discord_id) FROM guild_steam_discord_links`
- trackedServerCount: `SELECT COUNT(*) FROM tracked_servers`
- onlineTrackedPlayers: `SELECT COUNT(*) FROM player_states WHERE is_online`
- activeWipeReacts: `SELECT COUNT(*) FROM wipe_reacts WHERE scheduled_unix > $1` ($1 = now Unix secs)

### Cleanup service (background goroutine, NOT a route)
Started main.go `cleanup.NewService(postgresStore, cleanup.Config{}).Start(ctx)` — empty Config → all withDefaults. `go s.Run(ctx)`. Run: ticker=NewTicker(Interval=1h); runOnce immediately on start; then loop select ctx.Done/ticker.C→runOnce.
Config defaults (applied when field<=0, all overridable but main overrides none): Interval 1h; WipeReactRetention 7d (7*24h); AnalyticsCacheRetention 48h; CommandUsageRetention 90d (90*24h).
runOnce: now=time.Now() captured once; each cutoff = now-retention. Fixed order:
1. wipe_reacts: cutoff now-7d. `DELETE FROM wipe_reacts WHERE scheduled_unix < $1` ($1 = (now-7d).Unix() int64 SECONDS — note: scheduled_unix is BIGINT epoch seconds, compared to cutoff.Unix()).
2. track_analytics_sessions_cache: cutoff now-48h. `DELETE FROM track_analytics_sessions_cache WHERE fetched_at < $1` ($1 = now-48h TIMESTAMPTZ).
3. command_usage: cutoff now-90d. `DELETE FROM command_usage WHERE created_at < $1` ($1 = now-90d TIMESTAMPTZ). Cascades to command_usage_targets (FK CASCADE) + parent_usage_id (SET NULL) — DB-handled.
Each sweep returns (rows,err). err→log Error, continue (does NOT abort other sweeps). rows>0→log Info; rows==0 no log. runOnce never returns err, never crashes. No retry/backoff (next tick retries). All 3 run every tick.
Cross-domain tables (delete-only, shared DB fine): wipe_reacts, track_analytics_sessions_cache, command_usage(+targets). Long-lived product data never touched.
**Rust: replicate exact cutoffs — wipe_reacts compares int64-seconds col to cutoff.Unix(); other two compare timestamptz to now-retention. Immediate sweep on start, then 1h ticker, all 3/tick, log-and-continue.**
## Rate-limit + Discord webhook

Shared: writeError→{"error":msg}; writeJSON (omitempty/pointer rules govern keys); validateSnowflakeValue=^\d{17,20}$; body cap 1MB. NO Lua — every Redis op an individual command (INCR/PEXPIRE/PTTL/SET PX/DEL/SETNX PX); Rust replicates identical keys+order (bot shares Redis).

### Rate-limit consume
Route `POST /api/internal/bot/setup/rate-limit/consume` -> ConsumeRateLimit (rate_limit_handlers.go). Bot-JWT (setup group).
Req `{discordId (req snowflake), command (req, trim+lower, known key), guildId (opt, snowflake if non-empty), memberCount (opt int)}`.
Validate: decode→400 `invalid request body`; !snowflake(discordId)→400 `discordId must be a valid Discord snowflake`; trim+lower(command)==""→400 `command is required`; command∉configs→400 `unknown rate-limit command`; guildId!="" && !snowflake→400 `guildId must be a valid Discord snowflake`; limiter Consume err→500 `rate limit unavailable`.
Commands (rateLimitCommandConfigs): `player` {Quota 5, Window 60s, BaseCooldown 30s, MaxCooldown 1h, ViolationDecay 1h, fraud=playerFraudConfig}; `view-notadded` {Quota 3, Window 60s, BaseCooldown 60s, MaxCooldown 1h, ViolationDecay 1h, no fraud}. fraudCommandConfigs: only `player` {Window 1h, Threshold 30, MaxMemberCount 25, AlertCooldown 6h}.
Bypass: DiscordHasPermission(discordId, `commands.rate_limit.bypass`)=rateLimitBypass; (discordId, `commands.fraud.bypass`)=fraudBypass. SQL: `SELECT EXISTS(... app_users u JOIN admin_user_roles aur JOIN admin_roles r LEFT JOIN admin_role_permissions rp ON rp.role_id=r.id AND rp.permission_key=$2 WHERE u.discord_id=$1 AND (r.key='superadmin' OR rp.permission_key IS NOT NULL))`. **Err→log only, bool stays false (fail-closed, limits apply).**
Flow: resp={Allowed:true}. If !rateLimitBypass: key=`discordId:cmd`, limiter.Consume→copy Allowed/RetryAfterSeconds/ViolationLevel. If resp.Allowed && !fraudBypass && cmd has fraud: detector.Record(guildId,cmd,memberCount)→err log only; alert.Triggered→resp.FraudAlert. Always writeJSON(200,resp).
Resp `rateLimitConsumeResponse` ALWAYS 200 (no 429): allowed (bool always), retryAfterSeconds (int always, 0 when allowed), violationLevel (int omitempty — omit when 0), fraudAlert (object omitempty — only on trigger). fraudAlert {guildId, command, count (int n), memberCount, windowSecs (Window/1s=3600)}.
Limiter Redis (key=discordId:cmd): keys `rl:cd:<key>`, `rl:q:<key>`, `rl:lvl:<key>`. Guard Quota/Window/BaseCooldown/MaxCooldown<=0→err `ratelimit: invalid config`. Algo: 1 `PTTL rl:cd:<key>` (redis.Nil=no key); ttl>0→in cooldown, return Allowed=false RetryAfterSeconds=secondsCeil(ttl) (no writes/level change). 2 `INCR rl:q:<key>`→n; n==1→`PEXPIRE rl:q:<key> Window` (FIXED window anchored at first hit). 3 n<=Quota→Allowed=true done. 4 overflow: `INCR rl:lvl:<key>`→level; `PEXPIRE rl:lvl:<key> ViolationDecay`. 5 cooldown=scaleCooldown(Base,Max,level). 6 `SET rl:cd:<key> "1" PX <cooldown-ms>`. 7 `DEL rl:q:<key>`. 8 return Allowed=false RetryAfterSeconds=secondsCeil(cooldown) ViolationLevel=level. scaleCooldown(base,max,level): level<1→1; level>30→max; d=base<<(level-1)=base*2^(level-1); d<=0||d>max→max else d (30s,60s,120s… cap 1h). secondsCeil(d)=ceil to whole secs, min 1.
Fraud Redis: keys `fraud:<cmd>:<guild>:count`, `fraud:<cmd>:<guild>:alerted`. Guard Window/Threshold/MaxMemberCount/AlertCooldown<=0→err `fraud: invalid config`. Algo: 1 low-guild gate: skip (no alert,nil) unless guildID!="" && memberCount>0 && memberCount<MaxMemberCount (player: [1,24]). 2 `INCR count`→n; n==1→`PEXPIRE count Window` (FIXED 1h). 3 n<Threshold(30)→no alert. 4 `SETNX alerted "1" PX AlertCooldown(6h)`; false(exists)→no alert (6h suppression). 5 else Alert{Triggered, GuildID, Command, Count:n, MemberCount, WindowSecs:3600}. Fraud only runs when resp.Allowed && cmd in fraudCommandConfigs.
Notes: rate-limited=200 allowed:false (not HTTP err); bypass DB err swallowed fail-closed; fraud err swallowed 200; only limiter err=500. FIXED windows anchored at first incr (NOT sliding). command normalized before config+key.

### Discord webhook events
Route `POST /api/discord/webhook-events` -> DiscordWebhookEvents (discord_webhook_handlers.go). TOP-LEVEL, no auth middleware; Ed25519 verify inside.
Consts: PING type 0, EVENT type 1; integrationTypeUser=1; event strings APPLICATION_AUTHORIZED / APPLICATION_DEAUTHORIZED; pubkey env `DISCORD_APP_PUBLIC_KEY` (cfg.DiscordAppPublicKey, default "", trimmed).
Verify (order): 1 pubkey==""→503 `discord webhook public key not configured`. 2 hex.Decode(pubkey) err or len!=32→log + 503 `discord webhook public key invalid`. 3 headers `X-Signature-Ed25519` (hex sig) + `X-Signature-Timestamp` (unix secs str); either empty→401 `missing signature headers`. 4 hex.Decode(sig) err or len!=64→401 `invalid signature encoding`. 5 ParseInt(ts)→err→401 `invalid signature timestamp`. 6 drift=Since(Unix(ts)); drift>5m||drift<-5m→401 `signature timestamp out of range`. 7 read body (1MB cap); err→400 `failed to read body`. 8 **signedMsg = tsHeader-string + raw-body-bytes**; ed25519.Verify(pubKey,signedMsg,sig) fail→401 `invalid signature`. (verify over RAW body, do not re-serialize.)
Body `discordWebhookPayload` (unmarshal after verify; err→400 `invalid json`): {version int, application_id string, type int, event {type string, timestamp string, data raw}}.
Dispatch: type==0 PING→**204 empty**. type!=1||event==nil→204. switch event.type: APPLICATION_AUTHORIZED→unmarshal data {integration_type int|null, scopes []string, user {id,username,global_name?,avatar?}}; err→400 `invalid authorized data`; if integration_type==nil||*!=1 (not user install)→204; else validate user.id snowflake (400 `user.id must be a Discord snowflake`); UpsertBotUserInstall; err→log+500 `failed to upsert user install`. APPLICATION_DEAUTHORIZED→unmarshal {user}; err→400 `invalid deauthorized data`; validate user.id snowflake; MarkBotUserInstallRemoved(user.id); err→log+500 `failed to mark user install removed`. default→no-op. Success/ignored/unknown→**204 empty**.
DB (bot_user_installs): UpsertBotUserInstall `INSERT (discord_id,username,global_name,avatar_hash,scopes,is_installed,installed_at,left_at,metadata_updated_at) VALUES ($1,$2,$3,$4,$5,TRUE,NOW(),NULL,NOW()) ON CONFLICT (discord_id) DO UPDATE SET username/global_name/avatar_hash/scopes=EXCLUDED.*, is_installed=TRUE, left_at=NULL, metadata_updated_at=NOW()`. nil Scopes→[]string{} (never NULL). MarkBotUserInstallRemoved `UPDATE bot_user_installs SET is_installed=FALSE, left_at=NOW() WHERE discord_id=$1` (0 rows ok→204).
Notes: success ALWAYS 204 empty (PING/ignored/unknown/upsert/remove); errors JSON {"error"}. 503=config vs 401=bad sig. signedMsg=ts-header||raw-body (Discord interaction scheme). ±5min replay. only integration_type==1 persisted (guild installs via bot gateway UpsertBotGuild). scopes TEXT[] empty-slice not NULL.
