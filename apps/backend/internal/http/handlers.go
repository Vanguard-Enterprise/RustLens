package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"rustlens/backend/internal/admin/actions"
	"rustlens/backend/internal/admin/audit"
	"rustlens/backend/internal/admin/rbac"
	"rustlens/backend/internal/appauth"
	"rustlens/backend/internal/auth"
	"rustlens/backend/internal/battlemetrics"
	"rustlens/backend/internal/bmmatch"
	"rustlens/backend/internal/clientip"
	"rustlens/backend/internal/config"
	"rustlens/backend/internal/events"
	"rustlens/backend/internal/outboundhttp"
	"rustlens/backend/internal/platformsync"
	"rustlens/backend/internal/players"
	"rustlens/backend/internal/proxycrypto"
	"rustlens/backend/internal/shards"
	"rustlens/backend/internal/steamapi"
	"rustlens/backend/internal/store"
)

var snowflakePattern = regexp.MustCompile(`^\d{17,20}$`)
var steamIDPattern = regexp.MustCompile(`^\d{17}$`)
var bmIDPattern = regexp.MustCompile(`^\d{1,16}$`)

const maxRequestBodyBytes = 1 << 20 // 1 MB
const steamAccountCacheTTL = 24 * time.Hour
const steamGameBanCacheTTL = 6 * time.Hour
const steamPlayerBanBatchSize = 100
const deepGameBanPlatformSyncLimit = 100

type Handler struct {
	cfg                 config.Config
	postgres            *store.PostgresStore
	redis               *store.RedisStore
	shardService        *shards.Service
	jwtValidator        *auth.Validator
	steamClient         *steamapi.Client
	steamSummaryClient  *steamapi.Client
	battleMetricsClient *battlemetrics.Client
	bmMatch             *bmmatch.Client
	platformSync        *platformsync.Client
	rbacStore           *rbac.Store
	rbacResolver        *rbac.Resolver
	auditStore          *audit.Store
	auditSink           actions.AuditSink
	actionRegistry      *actions.Registry
	actionDispatcher    *actions.Dispatcher
	outboundProvider    *outboundhttp.Provider
	proxyCrypto         *proxycrypto.Crypto
	publisher           events.Publisher
	players             *players.Service
	appauth             *appauth.Repo
}

func NewHandler(
	cfg config.Config,
	postgres *store.PostgresStore,
	redis *store.RedisStore,
	shardService *shards.Service,
	steamClient *steamapi.Client,
	steamSummaryClient *steamapi.Client,
	battleMetricsClient *battlemetrics.Client,
	bmMatch *bmmatch.Client,
	platformSync *platformsync.Client,
	outboundProvider *outboundhttp.Provider,
	proxyCrypto *proxycrypto.Crypto,
	publisher events.Publisher,
	appauthRepo *appauth.Repo,
	roleSyncHook actions.RoleSyncHook,
) *Handler {
	if publisher == nil {
		publisher = events.NopPublisher{}
	}
	rbacStore := rbac.NewStore(postgres.Pool())
	if err := rbacStore.SeedPermissionsAndRoles(context.Background()); err != nil {
		panic(fmt.Errorf("rbac seed: %w", err))
	}
	resolver := rbac.NewResolver(rbacStore)
	auditStore := audit.NewStore(postgres.Pool())
	sink := actions.AuditSink(auditSinkAdapter{auditStore})
	registry := actions.NewRegistry(rbac.IsValidPermission)
	if err := registerAdminActions(registry, postgres.Pool(), resolver, publisher, cfg.AppBaseURL, roleSyncHook); err != nil {
		panic(fmt.Errorf("register admin actions: %w", err))
	}
	actorResolver := func(r *http.Request) (string, []string, error) {
		if k := actions.APIKeyAuthFromContext(r.Context()); k != nil {
			return k.UserID, nil, nil
		}
		return appSessionActor(r)
	}

	dispatcher := actions.NewDispatcher(registry, resolver, sink, actorResolver)
	dispatcher.SetClientIPHeaders(clientip.Headers{
		Trust:         cfg.TrustProxyHeaders,
		IPHeader:      cfg.TrustProxyIPHeader,
		CountryHeader: cfg.TrustProxyGeoHeader,
	})
	// The bulk wrapper needs a reference to the dispatcher so it can invoke
	// registered sub-actions through the shared Dispatch() helper.
	if err := registry.Register(actions.NewUsersBulkAction(dispatcher)); err != nil {
		panic(fmt.Errorf("register users.bulk: %w", err))
	}

	return &Handler{
		cfg:                 cfg,
		postgres:            postgres,
		redis:               redis,
		shardService:        shardService,
		jwtValidator:        auth.NewValidator(cfg.BotServiceJWT, cfg.AllowedJWTIssuer, cfg.AllowedJWTAudience),
		steamClient:         steamClient,
		steamSummaryClient:  steamSummaryClient,
		battleMetricsClient: battleMetricsClient,
		bmMatch:             bmMatch,
		platformSync:        platformSync,
		rbacStore:           rbacStore,
		rbacResolver:        resolver,
		auditStore:          auditStore,
		auditSink:           sink,
		actionRegistry:      registry,
		actionDispatcher:    dispatcher,
		outboundProvider:    outboundProvider,
		proxyCrypto:         proxyCrypto,
		publisher:           publisher,
		players:             players.NewService(postgres, steamClient, bmMatch),
		appauth:             appauthRepo,
	}
}

// apiKeyUserLoader loads app_users status for the API-key middleware.
type apiKeyUserLoader struct{ pool *pgxpool.Pool }

func (l *apiKeyUserLoader) LoadUserStatus(ctx context.Context, userID string) (active, suspended, deleted bool, err error) {
	var s, d *time.Time
	err = l.pool.QueryRow(ctx,
		`SELECT suspended_at, deleted_at FROM app_users WHERE id = $1`, userID,
	).Scan(&s, &d)
	if err != nil {
		return false, false, false, err
	}
	suspended = s != nil
	deleted = d != nil
	active = !suspended && !deleted
	return
}

// auditSinkAdapter bridges *audit.Store to the actions.AuditSink interface.
type auditSinkAdapter struct{ s *audit.Store }

func (a auditSinkAdapter) RecordEntry(ctx context.Context, e actions.AuditEntryInput) error {
	return a.s.Record(ctx, audit.Entry{
		ActionType:        e.ActionType,
		ActorUserID:       &e.ActorUserID,
		ActorRoleSnapshot: e.ActorRoleSnapshot,
		ResourceType:      e.ResourceType,
		ResourceID:        e.ResourceID,
		TargetUserID:      e.TargetUserID,
		Params:            e.Params,
		Reason:            e.Reason,
		Result:            e.Result,
		ErrorMessage:      e.ErrorMessage,
		IPAddress:         e.IPAddress,
		UserAgent:         e.UserAgent,
		ViaAPIKeyID:       e.ViaAPIKeyID,
		ViaAPIKeyName:     e.ViaAPIKeyName,
	})
}

// appSessionActor resolves the acting user for admin action dispatch.
// It reads the validated session attached by the appauth.RequireSession
// middleware. Returns an error if the middleware has not populated the
// context, which the dispatcher translates to 401.
func appSessionActor(r *http.Request) (string, []string, error) {
	if session := appauth.SessionFromContext(r.Context()); session != nil && session.UserID != "" {
		return session.UserID, session.Roles, nil
	}
	return "", nil, fmt.Errorf("no app session")
}

func (h *Handler) Healthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	dbErr := h.postgres.Ping(ctx)
	redisErr := h.redis.Ping(ctx)

	status := http.StatusOK
	response := map[string]any{
		"status": "ok",
		"db":     "ok",
		"redis":  "ok",
	}

	if dbErr != nil {
		status = http.StatusServiceUnavailable
		response["status"] = "degraded"
		response["db"] = "unavailable"
		log.Printf("[backend] healthz db ping failed: %v", dbErr)
	}

	if redisErr != nil {
		status = http.StatusServiceUnavailable
		response["status"] = "degraded"
		response["redis"] = "unavailable"
		log.Printf("[backend] healthz redis ping failed: %v", redisErr)
	}

	writeJSON(w, status, response)
}

type registerRequest struct {
	WorkerID string `json:"workerId"`
}

func (h *Handler) RegisterShardWorker(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "missing auth claims")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var request registerRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if request.WorkerID == "" {
		request.WorkerID = claims.WorkerID
	}

	response, err := h.shardService.Register(r.Context(), request.WorkerID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, response)
}

type heartbeatRequest struct {
	WorkerID string                     `json:"workerId"`
	ShardIDs []int                      `json:"shardIds"`
	Stats    *shards.WorkerStatSnapshot `json:"stats,omitempty"`
}

func (h *Handler) HeartbeatShardWorker(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "missing auth claims")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var request heartbeatRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if request.WorkerID == "" {
		request.WorkerID = claims.WorkerID
	}

	if err := h.shardService.Heartbeat(r.Context(), request.WorkerID, request.ShardIDs, request.Stats); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type releaseRequest struct {
	WorkerID string `json:"workerId"`
}

func (h *Handler) ReleaseShardWorker(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "missing auth claims")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var request releaseRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if request.WorkerID == "" {
		request.WorkerID = claims.WorkerID
	}

	if err := h.shardService.Release(r.Context(), request.WorkerID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "released"})
}

func (h *Handler) GetShardAssignments(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "missing auth claims")
		return
	}

	workerID := r.URL.Query().Get("workerId")
	if workerID == "" {
		workerID = claims.WorkerID
	}

	shards, targetShards, err := h.shardService.Assignments(r.Context(), workerID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"workerId":     workerID,
		"targetShards": targetShards,
		"shardIds":     shards,
	})
}

type ownerStatsWorker struct {
	WorkerID  string                     `json:"workerId"`
	UptimeSec int                        `json:"uptimeSec"`
	RSSBytes  int64                      `json:"rssBytes"`
	Shards    []shards.ShardStatSnapshot `json:"shards"`
}

type ownerStatsResponse struct {
	GuildCount           int                `json:"guildCount"`
	MemberCount          int64              `json:"memberCount"`
	ConfiguredGuilds     int64              `json:"configuredGuilds"`
	SteamAccountCount    int64              `json:"steamAccountCount"`
	HardLinkCount        int64              `json:"hardLinkCount"`
	UniqueLinkedUsers    int64              `json:"uniqueLinkedUsers"`
	TrackedServerCount   int64              `json:"trackedServerCount"`
	OnlineTrackedPlayers int64              `json:"onlineTrackedPlayers"`
	ActiveWipeReacts     int64              `json:"activeWipeReacts"`
	TargetShards         int                `json:"targetShards"`
	Workers              []ownerStatsWorker `json:"workers"`
}

func (h *Handler) GetOwnerStats(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "missing auth claims")
		return
	}

	workerStats, err := h.shardService.ActiveWorkerStats(r.Context())
	if err != nil {
		log.Printf("[backend] GetOwnerStats worker stats error: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to load worker stats")
		return
	}

	counts, err := h.postgres.OwnerStats(r.Context(), time.Now().Unix())
	if err != nil {
		log.Printf("[backend] GetOwnerStats db counts error: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to load db counts")
		return
	}

	workerIDs := make([]string, 0, len(workerStats))
	for id := range workerStats {
		workerIDs = append(workerIDs, id)
	}
	sort.Strings(workerIDs)

	resp := ownerStatsResponse{
		ConfiguredGuilds:     counts.ConfiguredGuilds,
		SteamAccountCount:    counts.SteamAccounts,
		HardLinkCount:        counts.HardLinks,
		UniqueLinkedUsers:    counts.UniqueLinkedDiscords,
		TrackedServerCount:   counts.TrackedServers,
		OnlineTrackedPlayers: counts.OnlineTrackedPlayers,
		ActiveWipeReacts:     counts.ActiveWipeReacts,
		TargetShards:         h.cfg.BotTargetShards,
		Workers:              make([]ownerStatsWorker, 0, len(workerIDs)),
	}

	for _, workerID := range workerIDs {
		snapshot := workerStats[workerID]
		shardSnapshots := append([]shards.ShardStatSnapshot(nil), snapshot.Shards...)
		sort.Slice(shardSnapshots, func(i, j int) bool {
			return shardSnapshots[i].ShardID < shardSnapshots[j].ShardID
		})

		for _, s := range shardSnapshots {
			resp.GuildCount += s.Guilds
			resp.MemberCount += s.Members
		}

		resp.Workers = append(resp.Workers, ownerStatsWorker{
			WorkerID:  workerID,
			UptimeSec: snapshot.UptimeSec,
			RSSBytes:  snapshot.RSSBytes,
			Shards:    shardSnapshots,
		})
	}

	writeJSON(w, http.StatusOK, resp)
}

func validateGuildID(w http.ResponseWriter, r *http.Request) (string, bool) {
	guildID := chi.URLParam(r, "guildID")
	if guildID == "" {
		writeError(w, http.StatusBadRequest, "guildID is required")
		return "", false
	}
	if !snowflakePattern.MatchString(guildID) {
		writeError(w, http.StatusBadRequest, "guildID must be a valid Discord snowflake")
		return "", false
	}
	return guildID, true
}

func validateSnowflakePathParam(w http.ResponseWriter, r *http.Request, paramName string) (string, bool) {
	value := chi.URLParam(r, paramName)
	if value == "" {
		writeError(w, http.StatusBadRequest, paramName+" is required")
		return "", false
	}
	if !snowflakePattern.MatchString(value) {
		writeError(w, http.StatusBadRequest, paramName+" must be a valid Discord snowflake")
		return "", false
	}
	return value, true
}

func writeGuildSettings(w http.ResponseWriter, settings store.GuildSettings) {
	writeJSON(w, http.StatusOK, map[string]any{
		"guildId":                   settings.GuildID,
		"wipeReactLogChannelId":     settings.WipeReactLogChannelID,
		"linkingLogChannelId":       settings.LinkingLogChannelID,
		"trackedServerLogChannelId": settings.TrackedServerLogChannelID,
		"trackedUsersCategoryId":    settings.TrackedUsersCategoryID,
		"adminRoleId":               settings.AdminRoleID,
		"alertRoleId":               settings.AlertRoleID,
		"limited":                   settings.Limited,
	})
}

func trackedServerToMap(item store.TrackedServer) map[string]any {
	result := map[string]any{
		"bmId":          item.BmID,
		"name":          item.Name,
		"isVitality":    item.IsVitality,
		"isAtlas":       item.IsAtlas,
		"isVital":       item.IsVital,
		"isModded":      item.IsModded,
		"isOfficial":    item.IsOfficial,
		"isBuild":       item.IsBuild,
		"highFrequency": item.HighFrequency,
	}
	if item.LastPolledAt != nil {
		result["lastPolledAt"] = *item.LastPolledAt
	} else {
		result["lastPolledAt"] = nil
	}
	result["lastPollError"] = item.LastPollError
	return result
}

func guildTrackedServerToMap(item store.GuildTrackedServer) map[string]any {
	return map[string]any{
		"guildId": item.GuildID,
		"bmId":    item.BmID,
		"name":    item.Name,
	}
}

func writeTrackedServerList(w http.ResponseWriter, items []store.TrackedServer) {
	response := make([]map[string]any, 0, len(items))
	for _, item := range items {
		response = append(response, trackedServerToMap(item))
	}

	writeJSON(w, http.StatusOK, map[string]any{"items": response})
}

func writeGuildTrackedServerList(w http.ResponseWriter, items []store.GuildTrackedServer) {
	response := make([]map[string]any, 0, len(items))
	for _, item := range items {
		response = append(response, guildTrackedServerToMap(item))
	}

	writeJSON(w, http.StatusOK, map[string]any{"items": response})
}

func playerStateToMap(item store.PlayerState) map[string]any {
	result := map[string]any{
		"serverBmId": item.ServerBmID,
		"serverName": item.ServerName,
		"playerBmId": item.PlayerBmID,
		"isOnline":   item.IsOnline,
		"updatedAt":  item.UpdatedAt,
	}
	if item.OnlineSince != nil {
		result["onlineSince"] = *item.OnlineSince
	} else {
		result["onlineSince"] = nil
	}
	if item.OfflineSince != nil {
		result["offlineSince"] = *item.OfflineSince
	} else {
		result["offlineSince"] = nil
	}
	return result
}

func writePlayerStateList(w http.ResponseWriter, items []store.PlayerState) {
	response := make([]map[string]any, 0, len(items))
	for _, item := range items {
		response = append(response, playerStateToMap(item))
	}

	writeJSON(w, http.StatusOK, map[string]any{"items": response})
}

func wipeReactToMap(record store.WipeReact) map[string]any {
	return map[string]any{
		"guildId":           record.GuildID,
		"channelId":         record.ChannelID,
		"messageId":         record.MessageID,
		"creatorId":         record.CreatorID,
		"name":              record.Name,
		"description":       record.Description,
		"timeZone":          record.TimeZone,
		"scheduledUnix":     record.ScheduledUnix,
		"askLateReason":     record.AskLateReason,
		"askDeclinedReason": record.AskDeclinedReason,
		"acceptedUserIds":   record.AcceptedUserIDs,
		"declinedUserIds":   record.DeclinedUserIDs,
		"lateUserIds":       record.LateUserIDs,
		"embedTimestamp":    record.EmbedTimestamp,
	}
}

func writeWipeReact(w http.ResponseWriter, record store.WipeReact) {
	writeJSON(w, http.StatusOK, wipeReactToMap(record))
}

func writeWipeReactList(w http.ResponseWriter, records []store.WipeReact) {
	items := make([]map[string]any, 0, len(records))
	for _, record := range records {
		items = append(items, wipeReactToMap(record))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func writeWipeReactReactionUpdate(w http.ResponseWriter, update store.WipeReactReactionUpdate) {
	response := map[string]any{
		"changed":   update.Changed,
		"wipeReact": wipeReactToMap(update.WipeReact),
	}
	if update.PreviousType != nil {
		response["previousType"] = string(*update.PreviousType)
	} else {
		response["previousType"] = nil
	}
	writeJSON(w, http.StatusOK, response)
}

func writeWipeReactReactionRemoval(w http.ResponseWriter, removal store.WipeReactReactionRemoval) {
	response := map[string]any{
		"changed":   removal.Changed,
		"wipeReact": wipeReactToMap(removal.WipeReact),
	}
	if removal.RemovedType != nil {
		response["removedType"] = string(*removal.RemovedType)
	} else {
		response["removedType"] = nil
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) GetGuildSettings(w http.ResponseWriter, r *http.Request) {
	guildID, ok := validateGuildID(w, r)
	if !ok {
		return
	}

	settings, err := h.postgres.GetGuildSettings(r.Context(), guildID)
	if err != nil {
		log.Printf("[backend] GetGuildSettings error for guild %s: %v", guildID, err)
		writeError(w, http.StatusInternalServerError, "failed to fetch guild settings")
		return
	}

	writeGuildSettings(w, settings)
}

type updateGuildSettingsRequest struct {
	WipeReactLogChannelID     *string `json:"wipeReactLogChannelId"`
	LinkingLogChannelID       *string `json:"linkingLogChannelId"`
	TrackedServerLogChannelID *string `json:"trackedServerLogChannelId"`
	TrackedUsersCategoryID    *string `json:"trackedUsersCategoryId"`
	AdminRoleID               *string `json:"adminRoleId"`
	AlertRoleID               *string `json:"alertRoleId"`
	Limited                   *bool   `json:"limited"`
}

func validateSnowflakeOptionalField(w http.ResponseWriter, value *string, fieldName string) bool {
	if value == nil {
		return true
	}

	if *value == "" {
		return true
	}

	if !snowflakePattern.MatchString(*value) {
		writeError(w, http.StatusBadRequest, fieldName+" must be a valid Discord snowflake")
		return false
	}

	return true
}

func (h *Handler) UpdateGuildSettings(w http.ResponseWriter, r *http.Request) {
	guildID, ok := validateGuildID(w, r)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var request updateGuildSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if !validateSnowflakeOptionalField(w, request.WipeReactLogChannelID, "wipeReactLogChannelId") {
		return
	}
	if !validateSnowflakeOptionalField(w, request.LinkingLogChannelID, "linkingLogChannelId") {
		return
	}
	if !validateSnowflakeOptionalField(w, request.TrackedServerLogChannelID, "trackedServerLogChannelId") {
		return
	}
	if !validateSnowflakeOptionalField(w, request.TrackedUsersCategoryID, "trackedUsersCategoryId") {
		return
	}
	if !validateSnowflakeOptionalField(w, request.AdminRoleID, "adminRoleId") {
		return
	}
	if !validateSnowflakeOptionalField(w, request.AlertRoleID, "alertRoleId") {
		return
	}

	settings, err := h.postgres.UpdateGuildSettings(r.Context(), guildID, store.GuildSettingsPatch{
		WipeReactLogChannelID:     request.WipeReactLogChannelID,
		LinkingLogChannelID:       request.LinkingLogChannelID,
		TrackedServerLogChannelID: request.TrackedServerLogChannelID,
		TrackedUsersCategoryID:    request.TrackedUsersCategoryID,
		AdminRoleID:               request.AdminRoleID,
		AlertRoleID:               request.AlertRoleID,
		Limited:                   request.Limited,
	})
	if err != nil {
		log.Printf("[backend] UpdateGuildSettings error for guild %s: %v", guildID, err)
		writeError(w, http.StatusInternalServerError, "failed to save guild settings")
		return
	}

	writeGuildSettings(w, settings)
}

type wipeReactPayload struct {
	ChannelID         string   `json:"channelId"`
	MessageID         string   `json:"messageId"`
	CreatorID         string   `json:"creatorId"`
	Name              string   `json:"name"`
	Description       string   `json:"description"`
	TimeZone          string   `json:"timeZone"`
	ScheduledUnix     int64    `json:"scheduledUnix"`
	AskLateReason     bool     `json:"askLateReason"`
	AskDeclinedReason bool     `json:"askDeclinedReason"`
	AcceptedUserIDs   []string `json:"acceptedUserIds"`
	DeclinedUserIDs   []string `json:"declinedUserIds"`
	LateUserIDs       []string `json:"lateUserIds"`
	EmbedTimestamp    string   `json:"embedTimestamp"`
}

func validateSnowflakeValue(value string) bool {
	return value != "" && snowflakePattern.MatchString(value)
}

func validateWipeReactPayload(w http.ResponseWriter, payload wipeReactPayload, requireMessageID bool) bool {
	if !validateSnowflakeValue(payload.ChannelID) {
		writeError(w, http.StatusBadRequest, "channelId must be a valid Discord snowflake")
		return false
	}
	if requireMessageID && !validateSnowflakeValue(payload.MessageID) {
		writeError(w, http.StatusBadRequest, "messageId must be a valid Discord snowflake")
		return false
	}
	if !validateSnowflakeValue(payload.CreatorID) {
		writeError(w, http.StatusBadRequest, "creatorId must be a valid Discord snowflake")
		return false
	}
	if payload.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return false
	}
	if payload.TimeZone == "" {
		writeError(w, http.StatusBadRequest, "timeZone is required")
		return false
	}
	if payload.ScheduledUnix <= 0 {
		writeError(w, http.StatusBadRequest, "scheduledUnix must be greater than zero")
		return false
	}
	if payload.EmbedTimestamp == "" {
		writeError(w, http.StatusBadRequest, "embedTimestamp is required")
		return false
	}

	const maxReactionUserIDs = 10000

	totalReactionUserIDs := len(payload.AcceptedUserIDs) + len(payload.DeclinedUserIDs) + len(payload.LateUserIDs)
	if totalReactionUserIDs > maxReactionUserIDs {
		writeError(w, http.StatusBadRequest, "reaction user id lists exceed maximum allowed size")
		return false
	}

	reactionUserIDs := append([]string{}, payload.AcceptedUserIDs...)
	reactionUserIDs = append(reactionUserIDs, payload.DeclinedUserIDs...)
	reactionUserIDs = append(reactionUserIDs, payload.LateUserIDs...)
	for _, userID := range reactionUserIDs {
		if !validateSnowflakeValue(userID) {
			writeError(w, http.StatusBadRequest, "reaction user ids must be valid Discord snowflakes")
			return false
		}
	}
	return true
}

func wipeReactFromPayload(guildID string, payload wipeReactPayload) store.WipeReact {
	return store.WipeReact{
		GuildID:           guildID,
		ChannelID:         payload.ChannelID,
		MessageID:         payload.MessageID,
		CreatorID:         payload.CreatorID,
		Name:              payload.Name,
		Description:       payload.Description,
		TimeZone:          payload.TimeZone,
		ScheduledUnix:     payload.ScheduledUnix,
		AskLateReason:     payload.AskLateReason,
		AskDeclinedReason: payload.AskDeclinedReason,
		AcceptedUserIDs:   payload.AcceptedUserIDs,
		DeclinedUserIDs:   payload.DeclinedUserIDs,
		LateUserIDs:       payload.LateUserIDs,
		EmbedTimestamp:    payload.EmbedTimestamp,
	}
}

func (h *Handler) ListActiveWipeReacts(w http.ResponseWriter, r *http.Request) {
	guildID, ok := validateGuildID(w, r)
	if !ok {
		return
	}

	records, err := h.postgres.ListActiveWipeReacts(r.Context(), guildID, time.Now().Unix())
	if err != nil {
		log.Printf("[backend] ListActiveWipeReacts error for guild %s: %v", guildID, err)
		writeError(w, http.StatusInternalServerError, "failed to list active wipe reacts")
		return
	}

	writeWipeReactList(w, records)
}

func (h *Handler) ListWipeReacts(w http.ResponseWriter, r *http.Request) {
	guildID, ok := validateGuildID(w, r)
	if !ok {
		return
	}

	records, err := h.postgres.ListWipeReacts(r.Context(), guildID)
	if err != nil {
		log.Printf("[backend] ListWipeReacts error for guild %s: %v", guildID, err)
		writeError(w, http.StatusInternalServerError, "failed to list wipe reacts")
		return
	}

	writeWipeReactList(w, records)
}

func (h *Handler) GetWipeReact(w http.ResponseWriter, r *http.Request) {
	guildID, ok := validateGuildID(w, r)
	if !ok {
		return
	}
	messageID, ok := validateSnowflakePathParam(w, r, "messageID")
	if !ok {
		return
	}

	record, err := h.postgres.GetWipeReact(r.Context(), guildID, messageID)
	if errors.Is(err, store.ErrWipeReactNotFound) {
		writeError(w, http.StatusNotFound, "wipe react not found")
		return
	}
	if err != nil {
		log.Printf("[backend] GetWipeReact error for guild %s message %s: %v", guildID, messageID, err)
		writeError(w, http.StatusInternalServerError, "failed to fetch wipe react")
		return
	}

	writeWipeReact(w, record)
}

func (h *Handler) CreateWipeReact(w http.ResponseWriter, r *http.Request) {
	guildID, ok := validateGuildID(w, r)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var payload wipeReactPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !validateWipeReactPayload(w, payload, true) {
		return
	}

	record, err := h.postgres.CreateWipeReact(r.Context(), wipeReactFromPayload(guildID, payload))
	if err != nil {
		log.Printf("[backend] CreateWipeReact error for guild %s message %s: %v", guildID, payload.MessageID, err)
		writeError(w, http.StatusInternalServerError, "failed to create wipe react")
		return
	}

	writeWipeReact(w, record)
}

func (h *Handler) UpdateWipeReact(w http.ResponseWriter, r *http.Request) {
	guildID, ok := validateGuildID(w, r)
	if !ok {
		return
	}
	messageID, ok := validateSnowflakePathParam(w, r, "messageID")
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var payload wipeReactPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	payload.MessageID = messageID
	if !validateWipeReactPayload(w, payload, true) {
		return
	}

	record, err := h.postgres.UpdateWipeReact(r.Context(), guildID, messageID, wipeReactFromPayload(guildID, payload))
	if errors.Is(err, store.ErrWipeReactNotFound) {
		writeError(w, http.StatusNotFound, "wipe react not found")
		return
	}
	if err != nil {
		log.Printf("[backend] UpdateWipeReact error for guild %s message %s: %v", guildID, messageID, err)
		writeError(w, http.StatusInternalServerError, "failed to update wipe react")
		return
	}

	writeWipeReact(w, record)
}

func (h *Handler) DeleteWipeReact(w http.ResponseWriter, r *http.Request) {
	guildID, ok := validateGuildID(w, r)
	if !ok {
		return
	}
	messageID, ok := validateSnowflakePathParam(w, r, "messageID")
	if !ok {
		return
	}

	if err := h.postgres.DeleteWipeReact(r.Context(), guildID, messageID); errors.Is(err, store.ErrWipeReactNotFound) {
		writeError(w, http.StatusNotFound, "wipe react not found")
		return
	} else if err != nil {
		log.Printf("[backend] DeleteWipeReact error for guild %s message %s: %v", guildID, messageID, err)
		writeError(w, http.StatusInternalServerError, "failed to delete wipe react")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

type setWipeReactReactionRequest struct {
	UserID string `json:"userId"`
	Type   string `json:"type"`
}

func (h *Handler) SetWipeReactReaction(w http.ResponseWriter, r *http.Request) {
	guildID, ok := validateGuildID(w, r)
	if !ok {
		return
	}
	messageID, ok := validateSnowflakePathParam(w, r, "messageID")
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var payload setWipeReactReactionRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !validateSnowflakeValue(payload.UserID) {
		writeError(w, http.StatusBadRequest, "userId must be a valid Discord snowflake")
		return
	}

	var reactionType store.WipeReactReactionType
	switch payload.Type {
	case string(store.WipeReactReactionAccepted):
		reactionType = store.WipeReactReactionAccepted
	case string(store.WipeReactReactionDeclined):
		reactionType = store.WipeReactReactionDeclined
	case string(store.WipeReactReactionLate):
		reactionType = store.WipeReactReactionLate
	default:
		writeError(w, http.StatusBadRequest, "type must be one of accepted, declined, or late")
		return
	}

	update, err := h.postgres.SetWipeReactReaction(r.Context(), guildID, messageID, payload.UserID, reactionType)
	if errors.Is(err, store.ErrWipeReactNotFound) {
		writeError(w, http.StatusNotFound, "wipe react not found")
		return
	}
	if err != nil {
		log.Printf("[backend] SetWipeReactReaction error for guild %s message %s: %v", guildID, messageID, err)
		writeError(w, http.StatusInternalServerError, "failed to update wipe react reaction")
		return
	}

	writeWipeReactReactionUpdate(w, update)
}

func (h *Handler) RemoveWipeReactReaction(w http.ResponseWriter, r *http.Request) {
	guildID, ok := validateGuildID(w, r)
	if !ok {
		return
	}
	messageID, ok := validateSnowflakePathParam(w, r, "messageID")
	if !ok {
		return
	}
	userID, ok := validateSnowflakePathParam(w, r, "userID")
	if !ok {
		return
	}

	removal, err := h.postgres.RemoveWipeReactReaction(r.Context(), guildID, messageID, userID)
	if errors.Is(err, store.ErrWipeReactNotFound) {
		writeError(w, http.StatusNotFound, "wipe react not found")
		return
	}
	if err != nil {
		log.Printf("[backend] RemoveWipeReactReaction error for guild %s message %s user %s: %v", guildID, messageID, userID, err)
		writeError(w, http.StatusInternalServerError, "failed to remove wipe react reaction")
		return
	}

	writeWipeReactReactionRemoval(w, removal)
}

func wipeReactScheduleToMap(record store.WipeReactSchedule) map[string]any {
	return map[string]any{
		"id":                record.ID,
		"guildId":           record.GuildID,
		"days":              record.Days,
		"includesForceWipe": record.IncludesForceWipe,
		"time":              record.Time,
		"timeZone":          record.TimeZone,
		"repeat":            record.Repeat,
		"postBefore":        record.PostBefore,
		"name":              record.Name,
		"description":       record.Description,
		"askLateReason":     record.AskLateReason,
		"askDeclinedReason": record.AskDeclinedReason,
		"channelId":         record.ChannelID,
		"lastPostedForUnix": record.LastPostedForUnix,
		"createdBy":         record.CreatedBy,
	}
}

func validateScheduleIDParam(w http.ResponseWriter, r *http.Request) (int64, bool) {
	raw := chi.URLParam(r, "scheduleID")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "scheduleID must be a positive integer")
		return 0, false
	}
	return id, true
}

var timePattern = regexp.MustCompile(`^\d{2}:\d{2}$`)

type wipeReactSchedulePayload struct {
	Days              []string `json:"days"`
	IncludesForceWipe bool     `json:"includesForceWipe"`
	Time              string   `json:"time"`
	TimeZone          string   `json:"timeZone"`
	Repeat            string   `json:"repeat"`
	PostBefore        string   `json:"postBefore"`
	Name              string   `json:"name"`
	Description       string   `json:"description"`
	AskLateReason     bool     `json:"askLateReason"`
	AskDeclinedReason bool     `json:"askDeclinedReason"`
	ChannelID         string   `json:"channelId"`
	CreatedBy         string   `json:"createdBy"`
}

var validDays = map[string]bool{
	"monday": true, "tuesday": true, "wednesday": true, "thursday": true,
	"friday": true, "saturday": true, "sunday": true,
}

var validPostBefore = map[string]bool{
	"1d": true, "2d": true, "3d": true, "4d": true, "1w": true, "2w": true,
}

var validRepeat = map[string]bool{
	"weekly": true, "biweekly": true, "monthly": true,
}

func validateWipeReactSchedulePayload(w http.ResponseWriter, payload wipeReactSchedulePayload) bool {
	for _, day := range payload.Days {
		if !validDays[day] {
			writeError(w, http.StatusBadRequest, "each day must be one of monday, tuesday, wednesday, thursday, friday, saturday, sunday")
			return false
		}
	}

	if payload.TimeZone == "" {
		writeError(w, http.StatusBadRequest, "timeZone is required")
		return false
	}

	if !validPostBefore[payload.PostBefore] {
		writeError(w, http.StatusBadRequest, "postBefore must be one of 1d, 2d, 3d, 4d, 1w, 2w")
		return false
	}

	if !validateSnowflakeValue(payload.CreatedBy) {
		writeError(w, http.StatusBadRequest, "createdBy must be a valid Discord snowflake")
		return false
	}

	if !payload.IncludesForceWipe && len(payload.Days) == 0 {
		writeError(w, http.StatusBadRequest, "days must not be empty when includesForceWipe is false")
		return false
	}

	if len(payload.Days) > 0 {
		if !timePattern.MatchString(payload.Time) {
			writeError(w, http.StatusBadRequest, "time must match HH:MM format")
			return false
		}
		if !validRepeat[payload.Repeat] {
			writeError(w, http.StatusBadRequest, "repeat must be one of weekly, biweekly, monthly")
			return false
		}
	}

	return true
}

func wipeReactScheduleFromPayload(guildID string, payload wipeReactSchedulePayload) store.WipeReactSchedule {
	days := payload.Days
	if days == nil {
		days = []string{}
	}
	return store.WipeReactSchedule{
		GuildID:           guildID,
		Days:              days,
		IncludesForceWipe: payload.IncludesForceWipe,
		Time:              payload.Time,
		TimeZone:          payload.TimeZone,
		Repeat:            payload.Repeat,
		PostBefore:        payload.PostBefore,
		Name:              payload.Name,
		Description:       payload.Description,
		AskLateReason:     payload.AskLateReason,
		AskDeclinedReason: payload.AskDeclinedReason,
		ChannelID:         payload.ChannelID,
		CreatedBy:         payload.CreatedBy,
	}
}

func wipeReactSchedulePostToMap(record store.WipeReactSchedulePost) map[string]any {
	return map[string]any{
		"id":                record.ID,
		"scheduleId":        record.ScheduleID,
		"guildId":           record.GuildID,
		"channelId":         record.ChannelID,
		"wipeUnix":          record.WipeUnix,
		"postUnix":          record.PostUnix,
		"timeZone":          record.TimeZone,
		"name":              record.Name,
		"description":       record.Description,
		"askLateReason":     record.AskLateReason,
		"askDeclinedReason": record.AskDeclinedReason,
		"createdBy":         record.CreatedBy,
	}
}

type wipeReactSchedulePostWorkerRequest struct {
	WorkerID string `json:"workerId"`
}

type completeWipeReactSchedulePostRequest struct {
	WorkerID  string `json:"workerId"`
	MessageID string `json:"messageId"`
	ChannelID string `json:"channelId"`
}

func (h *Handler) ListWipeReactSchedules(w http.ResponseWriter, r *http.Request) {
	guildID, ok := validateGuildID(w, r)
	if !ok {
		return
	}

	records, err := h.postgres.ListWipeReactSchedules(r.Context(), guildID)
	if err != nil {
		log.Printf("[backend] ListWipeReactSchedules error for guild %s: %v", guildID, err)
		writeError(w, http.StatusInternalServerError, "failed to list wipe react schedules")
		return
	}

	items := make([]map[string]any, 0, len(records))
	for _, record := range records {
		items = append(items, wipeReactScheduleToMap(record))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) GetWipeReactSchedule(w http.ResponseWriter, r *http.Request) {
	guildID, ok := validateGuildID(w, r)
	if !ok {
		return
	}
	scheduleID, ok := validateScheduleIDParam(w, r)
	if !ok {
		return
	}

	record, err := h.postgres.GetWipeReactSchedule(r.Context(), guildID, scheduleID)
	if errors.Is(err, store.ErrWipeReactScheduleNotFound) {
		writeError(w, http.StatusNotFound, "wipe react schedule not found")
		return
	}
	if err != nil {
		log.Printf("[backend] GetWipeReactSchedule error for guild %s schedule %d: %v", guildID, scheduleID, err)
		writeError(w, http.StatusInternalServerError, "failed to fetch wipe react schedule")
		return
	}

	writeJSON(w, http.StatusOK, wipeReactScheduleToMap(record))
}

func (h *Handler) CreateWipeReactSchedule(w http.ResponseWriter, r *http.Request) {
	guildID, ok := validateGuildID(w, r)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var payload wipeReactSchedulePayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !validateWipeReactSchedulePayload(w, payload) {
		return
	}

	record, err := h.postgres.CreateWipeReactSchedule(r.Context(), wipeReactScheduleFromPayload(guildID, payload))
	if errors.Is(err, store.ErrWipeReactScheduleLimitReached) {
		writeError(w, http.StatusConflict, "wipe react schedule limit reached")
		return
	}
	if err != nil {
		log.Printf("[backend] CreateWipeReactSchedule error for guild %s: %v", guildID, err)
		writeError(w, http.StatusInternalServerError, "failed to create wipe react schedule")
		return
	}

	writeJSON(w, http.StatusCreated, wipeReactScheduleToMap(record))
}

func (h *Handler) UpdateWipeReactSchedule(w http.ResponseWriter, r *http.Request) {
	guildID, ok := validateGuildID(w, r)
	if !ok {
		return
	}
	scheduleID, ok := validateScheduleIDParam(w, r)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var payload wipeReactSchedulePayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !validateWipeReactSchedulePayload(w, payload) {
		return
	}

	record, err := h.postgres.UpdateWipeReactSchedule(r.Context(), guildID, scheduleID, wipeReactScheduleFromPayload(guildID, payload))
	if errors.Is(err, store.ErrWipeReactScheduleNotFound) {
		writeError(w, http.StatusNotFound, "wipe react schedule not found")
		return
	}
	if err != nil {
		log.Printf("[backend] UpdateWipeReactSchedule error for guild %s schedule %d: %v", guildID, scheduleID, err)
		writeError(w, http.StatusInternalServerError, "failed to update wipe react schedule")
		return
	}

	writeJSON(w, http.StatusOK, wipeReactScheduleToMap(record))
}

func (h *Handler) DeleteWipeReactSchedule(w http.ResponseWriter, r *http.Request) {
	guildID, ok := validateGuildID(w, r)
	if !ok {
		return
	}
	scheduleID, ok := validateScheduleIDParam(w, r)
	if !ok {
		return
	}

	if err := h.postgres.DeleteWipeReactSchedule(r.Context(), guildID, scheduleID); errors.Is(err, store.ErrWipeReactScheduleNotFound) {
		writeError(w, http.StatusNotFound, "wipe react schedule not found")
		return
	} else if err != nil {
		log.Printf("[backend] DeleteWipeReactSchedule error for guild %s schedule %d: %v", guildID, scheduleID, err)
		writeError(w, http.StatusInternalServerError, "failed to delete wipe react schedule")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *Handler) ClaimNextWipeReactSchedulePost(w http.ResponseWriter, r *http.Request) {
	guildID, ok := validateGuildID(w, r)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var payload wipeReactSchedulePostWorkerRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(payload.WorkerID) == "" {
		writeError(w, http.StatusBadRequest, "workerId is required")
		return
	}

	record, err := h.postgres.ClaimNextWipeReactSchedulePost(r.Context(), guildID, payload.WorkerID, time.Now().Unix(), time.Now().Add(-5*time.Minute))
	if errors.Is(err, store.ErrWipeReactSchedulePostNotFound) {
		writeJSON(w, http.StatusOK, map[string]any{"item": nil})
		return
	}
	if err != nil {
		log.Printf("[backend] ClaimNextWipeReactSchedulePost error for guild %s: %v", guildID, err)
		writeError(w, http.StatusInternalServerError, "failed to claim wipe react schedule post")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"item": wipeReactSchedulePostToMap(record)})
}

func (h *Handler) CompleteWipeReactSchedulePost(w http.ResponseWriter, r *http.Request) {
	guildID, ok := validateGuildID(w, r)
	if !ok {
		return
	}
	postID, ok := validateScheduleIDParam(w, r)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var payload completeWipeReactSchedulePostRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(payload.WorkerID) == "" {
		writeError(w, http.StatusBadRequest, "workerId is required")
		return
	}
	if strings.TrimSpace(payload.MessageID) == "" {
		writeError(w, http.StatusBadRequest, "messageId is required")
		return
	}
	if strings.TrimSpace(payload.ChannelID) == "" {
		writeError(w, http.StatusBadRequest, "channelId is required")
		return
	}

	if err := h.postgres.CompleteWipeReactSchedulePost(r.Context(), guildID, postID, payload.WorkerID, payload.MessageID, payload.ChannelID); errors.Is(err, store.ErrWipeReactSchedulePostNotFound) {
		writeError(w, http.StatusNotFound, "wipe react schedule post not found")
		return
	} else if err != nil {
		log.Printf("[backend] CompleteWipeReactSchedulePost error for guild %s post %d: %v", guildID, postID, err)
		writeError(w, http.StatusInternalServerError, "failed to complete wipe react schedule post")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) ReleaseWipeReactSchedulePost(w http.ResponseWriter, r *http.Request) {
	guildID, ok := validateGuildID(w, r)
	if !ok {
		return
	}
	postID, ok := validateScheduleIDParam(w, r)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var payload wipeReactSchedulePostWorkerRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(payload.WorkerID) == "" {
		writeError(w, http.StatusBadRequest, "workerId is required")
		return
	}

	if err := h.postgres.ReleaseWipeReactSchedulePost(r.Context(), guildID, postID, payload.WorkerID); errors.Is(err, store.ErrWipeReactSchedulePostNotFound) {
		writeError(w, http.StatusNotFound, "wipe react schedule post not found")
		return
	} else if err != nil {
		log.Printf("[backend] ReleaseWipeReactSchedulePost error for guild %s post %d: %v", guildID, postID, err)
		writeError(w, http.StatusInternalServerError, "failed to release wipe react schedule post")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type createTrackedServerRequest struct {
	BmID          string `json:"bmId"`
	Name          string `json:"name"`
	IsVitality    bool   `json:"isVitality"`
	IsAtlas       bool   `json:"isAtlas"`
	IsVital       bool   `json:"isVital"`
	IsModded      bool   `json:"isModded"`
	IsOfficial    bool   `json:"isOfficial"`
	IsBuild       bool   `json:"isBuild"`
	HighFrequency bool   `json:"highFrequency"`
}

func (h *Handler) ListTrackedServers(w http.ResponseWriter, r *http.Request) {
	items, err := h.postgres.ListTrackedServers(r.Context())
	if err != nil {
		log.Printf("[backend] ListTrackedServers error: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to list tracked servers")
		return
	}

	writeTrackedServerList(w, items)
}

func (h *Handler) CreateTrackedServer(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var request createTrackedServerRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	request.BmID = strings.TrimSpace(request.BmID)
	request.Name = strings.Join(strings.Fields(request.Name), " ")

	if request.BmID == "" {
		writeError(w, http.StatusBadRequest, "bmId is required")
		return
	}
	if request.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	item, err := h.postgres.CreateTrackedServer(
		r.Context(),
		request.BmID,
		request.Name,
		request.IsVitality,
		request.IsAtlas,
		request.IsVital,
		request.IsModded,
		request.IsOfficial,
		request.IsBuild,
		request.HighFrequency,
	)
	if errors.Is(err, store.ErrTrackedServerAlreadyExists) {
		writeError(w, http.StatusConflict, "tracked server already exists")
		return
	}
	if err != nil {
		log.Printf("[backend] CreateTrackedServer error for %s: %v", request.BmID, err)
		writeError(w, http.StatusInternalServerError, "failed to create tracked server")
		return
	}

	writeJSON(w, http.StatusCreated, trackedServerToMap(item))
}

type updateTrackedServerRequest struct {
	BmID          string `json:"bmId"`
	Name          string `json:"name"`
	IsVitality    bool   `json:"isVitality"`
	IsAtlas       bool   `json:"isAtlas"`
	IsVital       bool   `json:"isVital"`
	IsModded      bool   `json:"isModded"`
	IsOfficial    bool   `json:"isOfficial"`
	IsBuild       bool   `json:"isBuild"`
	HighFrequency bool   `json:"highFrequency"`
}

func (h *Handler) UpdateTrackedServer(w http.ResponseWriter, r *http.Request) {
	currentBmID := strings.TrimSpace(chi.URLParam(r, "bmID"))
	if currentBmID == "" {
		writeError(w, http.StatusBadRequest, "bmID is required")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var request updateTrackedServerRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	request.BmID = strings.TrimSpace(request.BmID)
	request.Name = strings.Join(strings.Fields(request.Name), " ")

	if request.BmID == "" {
		writeError(w, http.StatusBadRequest, "bmId is required")
		return
	}
	if request.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	item, err := h.postgres.UpdateTrackedServer(
		r.Context(),
		currentBmID,
		request.BmID,
		request.Name,
		request.IsVitality,
		request.IsAtlas,
		request.IsVital,
		request.IsModded,
		request.IsOfficial,
		request.IsBuild,
		request.HighFrequency,
	)
	if errors.Is(err, store.ErrTrackedServerNotFound) {
		writeError(w, http.StatusNotFound, "tracked server not found")
		return
	}
	if errors.Is(err, store.ErrTrackedServerAlreadyExists) {
		writeError(w, http.StatusConflict, "tracked server already exists")
		return
	}
	if err != nil {
		log.Printf("[backend] UpdateTrackedServer error for %s -> %s: %v", currentBmID, request.BmID, err)
		writeError(w, http.StatusInternalServerError, "failed to update tracked server")
		return
	}

	writeJSON(w, http.StatusOK, trackedServerToMap(item))
}

func (h *Handler) DeleteTrackedServer(w http.ResponseWriter, r *http.Request) {
	bmID := strings.TrimSpace(chi.URLParam(r, "bmID"))
	if bmID == "" {
		writeError(w, http.StatusBadRequest, "bmID is required")
		return
	}

	if err := h.postgres.DeleteTrackedServer(r.Context(), bmID); errors.Is(err, store.ErrTrackedServerNotFound) {
		writeError(w, http.StatusNotFound, "tracked server not found")
		return
	} else if err != nil {
		log.Printf("[backend] DeleteTrackedServer error for %s: %v", bmID, err)
		writeError(w, http.StatusInternalServerError, "failed to delete tracked server")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

type replaceGuildTrackedServersRequest struct {
	BmIDs []string `json:"bmIds"`
}

func (h *Handler) ListGuildTrackedServers(w http.ResponseWriter, r *http.Request) {
	guildID, ok := validateGuildID(w, r)
	if !ok {
		return
	}

	items, err := h.postgres.ListGuildTrackedServers(r.Context(), guildID)
	if err != nil {
		log.Printf("[backend] ListGuildTrackedServers error for guild %s: %v", guildID, err)
		writeError(w, http.StatusInternalServerError, "failed to list guild tracked servers")
		return
	}

	writeGuildTrackedServerList(w, items)
}

func (h *Handler) ReplaceGuildTrackedServers(w http.ResponseWriter, r *http.Request) {
	guildID, ok := validateGuildID(w, r)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var request replaceGuildTrackedServersRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if len(request.BmIDs) > 100 {
		writeError(w, http.StatusBadRequest, "bmIds exceeds maximum allowed size")
		return
	}

	seen := make(map[string]struct{}, len(request.BmIDs))
	bmIDs := make([]string, 0, len(request.BmIDs))
	for _, bmID := range request.BmIDs {
		trimmed := strings.TrimSpace(bmID)
		if trimmed == "" {
			writeError(w, http.StatusBadRequest, "bmIds must not contain empty values")
			return
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		bmIDs = append(bmIDs, trimmed)
	}

	if len(bmIDs) > 0 {
		trackedServers, err := h.postgres.ListTrackedServers(r.Context())
		if err != nil {
			log.Printf("[backend] ListTrackedServers validation error for guild %s: %v", guildID, err)
			writeError(w, http.StatusInternalServerError, "failed to validate tracked servers")
			return
		}

		available := make(map[string]struct{}, len(trackedServers))
		for _, item := range trackedServers {
			available[item.BmID] = struct{}{}
		}

		for _, bmID := range bmIDs {
			if _, ok := available[bmID]; !ok {
				writeError(w, http.StatusBadRequest, "bmIds must reference existing tracked servers")
				return
			}
		}
	}

	if err := h.postgres.ReplaceGuildTrackedServers(r.Context(), guildID, bmIDs); err != nil {
		log.Printf("[backend] ReplaceGuildTrackedServers error for guild %s: %v", guildID, err)
		writeError(w, http.StatusInternalServerError, "failed to save guild tracked servers")
		return
	}

	items, err := h.postgres.ListGuildTrackedServers(r.Context(), guildID)
	if err != nil {
		log.Printf("[backend] ListGuildTrackedServers (post-update) error for guild %s: %v", guildID, err)
		writeError(w, http.StatusInternalServerError, "failed to list guild tracked servers")
		return
	}

	writeGuildTrackedServerList(w, items)
}

func (h *Handler) AddGuildTrackedServer(w http.ResponseWriter, r *http.Request) {
	guildID, ok := validateGuildID(w, r)
	if !ok {
		return
	}
	bmID := strings.TrimSpace(chi.URLParam(r, "bmID"))
	if bmID == "" {
		writeError(w, http.StatusBadRequest, "bmID is required")
		return
	}

	err := h.postgres.AddGuildTrackedServer(r.Context(), guildID, bmID)
	if errors.Is(err, store.ErrTrackedServerNotFound) {
		writeError(w, http.StatusNotFound, "tracked server not found")
		return
	}
	if errors.Is(err, store.ErrGuildTrackedServerAlreadyEnabled) {
		writeError(w, http.StatusConflict, "tracked server already enabled for guild")
		return
	}
	if err != nil {
		log.Printf("[backend] AddGuildTrackedServer error for guild %s server %s: %v", guildID, bmID, err)
		writeError(w, http.StatusInternalServerError, "failed to enable tracked server for guild")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "enabled"})
}

func (h *Handler) RemoveGuildTrackedServer(w http.ResponseWriter, r *http.Request) {
	guildID, ok := validateGuildID(w, r)
	if !ok {
		return
	}
	bmID := strings.TrimSpace(chi.URLParam(r, "bmID"))
	if bmID == "" {
		writeError(w, http.StatusBadRequest, "bmID is required")
		return
	}

	err := h.postgres.RemoveGuildTrackedServer(r.Context(), guildID, bmID)
	if errors.Is(err, store.ErrGuildTrackedServerNotEnabled) {
		writeError(w, http.StatusNotFound, "tracked server not enabled for guild")
		return
	}
	if err != nil {
		log.Printf("[backend] RemoveGuildTrackedServer error for guild %s server %s: %v", guildID, bmID, err)
		writeError(w, http.StatusInternalServerError, "failed to disable tracked server for guild")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "disabled"})
}

func (h *Handler) ListOnlinePlayerStatesByServer(w http.ResponseWriter, r *http.Request) {
	serverID := strings.TrimSpace(chi.URLParam(r, "serverID"))
	if serverID == "" {
		writeError(w, http.StatusBadRequest, "serverID is required")
		return
	}

	items, err := h.postgres.ListOnlinePlayerStatesByServer(r.Context(), serverID)
	if err != nil {
		log.Printf("[backend] ListOnlinePlayerStatesByServer error for %s: %v", serverID, err)
		writeError(w, http.StatusInternalServerError, "failed to list player states")
		return
	}

	writePlayerStateList(w, items)
}

func (h *Handler) ListPlayerStatesByPlayer(w http.ResponseWriter, r *http.Request) {
	playerID := strings.TrimSpace(chi.URLParam(r, "playerID"))
	if playerID == "" {
		writeError(w, http.StatusBadRequest, "playerID is required")
		return
	}

	items, err := h.postgres.ListPlayerStatesByPlayer(r.Context(), playerID)
	if err != nil {
		log.Printf("[backend] ListPlayerStatesByPlayer error for %s: %v", playerID, err)
		writeError(w, http.StatusInternalServerError, "failed to list player states")
		return
	}

	writePlayerStateList(w, items)
}

func (h *Handler) SearchPlayerStatesOnServer(w http.ResponseWriter, r *http.Request) {
	serverID := strings.TrimSpace(chi.URLParam(r, "serverID"))
	if serverID == "" {
		writeError(w, http.StatusBadRequest, "serverID is required")
		return
	}

	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		writeError(w, http.StatusBadRequest, "q is required")
		return
	}

	limit := 20
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			writeError(w, http.StatusBadRequest, "limit must be a positive integer")
			return
		}
		// Cap at 21 (not 20) so callers can request one extra row purely to
		// detect that more than the 20-result display cap matched. The bot's
		// /search command relies on this overflow signal for its
		// "scope too wide" guard while still rendering at most 20 results.
		if parsed > 21 {
			parsed = 21
		}
		limit = parsed
	}

	includeOffline := false
	if raw := strings.TrimSpace(r.URL.Query().Get("includeOffline")); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "includeOffline must be a boolean")
			return
		}
		includeOffline = parsed
	}

	items, err := h.postgres.SearchPlayerStatesByName(r.Context(), serverID, query, limit, includeOffline)
	if err != nil {
		log.Printf("[backend] SearchPlayerStatesByName error for %s/%q: %v", serverID, query, err)
		writeError(w, http.StatusInternalServerError, "failed to search player states")
		return
	}

	response := make([]map[string]any, 0, len(items))
	for _, item := range items {
		response = append(response, map[string]any{
			"serverBmId": item.ServerBmID,
			"serverName": item.ServerName,
			"playerBmId": item.PlayerBmID,
			"steamId":    item.SteamID,
			"bmName":     item.BmName,
			"isOnline":   item.IsOnline,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{"items": response})
}

func (h *Handler) GetPlayerState(w http.ResponseWriter, r *http.Request) {
	serverID := strings.TrimSpace(chi.URLParam(r, "serverID"))
	if serverID == "" {
		writeError(w, http.StatusBadRequest, "serverID is required")
		return
	}

	playerID := strings.TrimSpace(chi.URLParam(r, "playerID"))
	if playerID == "" {
		writeError(w, http.StatusBadRequest, "playerID is required")
		return
	}

	item, err := h.postgres.GetPlayerState(r.Context(), serverID, playerID)
	if errors.Is(err, store.ErrPlayerStateNotFound) {
		writeError(w, http.StatusNotFound, "player state not found")
		return
	}
	if err != nil {
		log.Printf("[backend] GetPlayerState error for %s/%s: %v", serverID, playerID, err)
		writeError(w, http.StatusInternalServerError, "failed to fetch player state")
		return
	}

	writeJSON(w, http.StatusOK, playerStateToMap(item))
}

func steamAccountToMap(record store.SteamAccount) map[string]any {
	result := map[string]any{
		"steamId":     record.SteamID,
		"bmName":      record.BmName,
		"avatarImage": record.AvatarImage,
		"firstSeenAt": record.FirstSeenAt,
		"createdAt":   record.CreatedAt,
		"updatedAt":   record.UpdatedAt,
	}
	if record.BmID != nil {
		result["bmId"] = *record.BmID
	} else {
		result["bmId"] = nil
	}
	return result
}

type steamProfileHours struct {
	Vitality int64  `json:"vitality"`
	Atlas    int64  `json:"atlas"`
	Vital    int64  `json:"vital"`
	Modded   int64  `json:"modded"`
	Official int64  `json:"official"`
	Build    int64  `json:"build"`
	BM       int64  `json:"bm"`
	Rust     *int64 `json:"rust"`
}

type steamProfileResponse struct {
	SteamID       string              `json:"steamId"`
	BmID          string              `json:"bmId"`
	BmName        string              `json:"bmName"`
	PersonaName   string              `json:"personaName"`
	AvatarImage   string              `json:"avatarImage"`
	FirstSeenUnix *int64              `json:"firstSeenUnix"`
	Hours         steamProfileHours   `json:"hours"`
	ServersPlayed int                 `json:"serversPlayed"`
	NameGaps      []steamAnalyticsGap `json:"nameGaps"`
	Status        *steamProfileStatus `json:"status,omitempty"`
}

type steamProfileStatus struct {
	IsOnline    bool   `json:"isOnline"`
	ServerBmID  string `json:"serverBmId"`
	ServerName  string `json:"serverName"`
	OnlineUnix  *int64 `json:"onlineUnix"`
	OfflineUnix *int64 `json:"offlineUnix"`
}

func toWholeHours(seconds int64) int64 {
	if seconds <= 0 {
		return 0
	}
	return seconds / 3600
}

func steamProfileResponseFromCache(account store.SteamAccount, steamCache store.SteamAccountCache, bmCache store.BmProfileCache, gaps []steamAnalyticsGap) steamProfileResponse {
	response := steamProfileResponse{
		SteamID:       account.SteamID,
		BmName:        bmCache.BmName,
		PersonaName:   steamCache.PersonaName,
		AvatarImage:   steamCache.AvatarImage,
		ServersPlayed: bmCache.BmServersPlayed,
		NameGaps:      gaps,
		Hours: steamProfileHours{
			Vitality: bmCache.VitalityHours,
			Atlas:    bmCache.AtlasHours,
			Vital:    bmCache.VitalHours,
			Modded:   bmCache.ModdedHours,
			Official: bmCache.OfficialHours,
			Build:    bmCache.BuildHours,
			BM:       bmCache.BmHours,
			Rust:     steamCache.RustHours,
		},
	}
	if account.BmID != nil {
		response.BmID = *account.BmID
	}
	if response.BmName == "" {
		response.BmName = account.BmName
	}
	if bmCache.FirstSeenAt != nil {
		firstSeenUnix := bmCache.FirstSeenAt.Unix()
		response.FirstSeenUnix = &firstSeenUnix
	} else if account.FirstSeenAt != nil {
		firstSeenUnix := account.FirstSeenAt.Unix()
		response.FirstSeenUnix = &firstSeenUnix
	}
	return response
}

func steamProfileStatusFromStates(states []store.PlayerState) *steamProfileStatus {
	var selected *store.PlayerState
	for i := range states {
		state := &states[i]
		if state.IsOnline {
			if selected == nil || !selected.IsOnline || isLaterTime(state.OnlineSince, selected.OnlineSince) {
				selected = state
			}
			continue
		}

		if selected == nil || (!selected.IsOnline && isLaterTime(state.OfflineSince, selected.OfflineSince)) {
			selected = state
		}
	}
	if selected == nil {
		return nil
	}

	status := &steamProfileStatus{
		IsOnline:   selected.IsOnline,
		ServerBmID: selected.ServerBmID,
		ServerName: selected.ServerName,
	}
	if selected.OnlineSince != nil {
		onlineUnix := selected.OnlineSince.Unix()
		status.OnlineUnix = &onlineUnix
	}
	if selected.OfflineSince != nil {
		offlineUnix := selected.OfflineSince.Unix()
		status.OfflineUnix = &offlineUnix
	}
	return status
}

func isLaterTime(candidate *time.Time, current *time.Time) bool {
	if candidate == nil {
		return false
	}
	if current == nil {
		return true
	}
	return candidate.After(*current)
}

func (h *Handler) steamProfileResponseWithStatus(ctx context.Context, account store.SteamAccount, steamCache store.SteamAccountCache, bmCache store.BmProfileCache) steamProfileResponse {
	bmID := bmCache.BmID
	if bmID == "" && account.BmID != nil {
		bmID = *account.BmID
	}
	response := steamProfileResponseFromCache(account, steamCache, bmCache, h.nameGapsForBmID(ctx, bmID))
	if account.BmID == nil {
		return response
	}

	states, err := h.postgres.ListPlayerStatesByPlayer(ctx, *account.BmID)
	if err != nil {
		log.Printf("[backend] GetSteamProfile status error for %s: %v", account.SteamID, err)
		return response
	}
	response.Status = steamProfileStatusFromStates(states)
	return response
}

func (h *Handler) GetSteamProfile(w http.ResponseWriter, r *http.Request) {
	steamID := chi.URLParam(r, "steamID")
	if steamID == "" || !steamIDPattern.MatchString(steamID) {
		writeError(w, http.StatusBadRequest, "steamID must be a 17-digit number")
		return
	}

	account, err := h.postgres.GetSteamAccountBySteamID(r.Context(), steamID)
	if errors.Is(err, store.ErrSteamAccountNotFound) {
		writeError(w, http.StatusNotFound, "steam account not found")
		return
	}
	if err != nil {
		log.Printf("[backend] GetSteamProfile account error for %s: %v", steamID, err)
		writeError(w, http.StatusInternalServerError, "failed to fetch steam account")
		return
	}
	if account.BmID == nil || strings.TrimSpace(*account.BmID) == "" {
		writeError(w, http.StatusNotFound, "steam account has no linked battlemetrics id")
		return
	}

	cachedSteam, steamCacheErr := h.postgres.GetSteamAccountCache(r.Context(), steamID)
	if steamCacheErr != nil && !errors.Is(steamCacheErr, store.ErrSteamAccountNotFound) {
		log.Printf("[backend] GetSteamProfile steam cache error for %s: %v", steamID, steamCacheErr)
		writeError(w, http.StatusInternalServerError, "failed to fetch steam account cache")
		return
	}
	cachedBm, bmCacheErr := h.postgres.GetBmProfileCache(r.Context(), *account.BmID)
	if bmCacheErr != nil && !errors.Is(bmCacheErr, store.ErrSteamAccountNotFound) {
		log.Printf("[backend] GetSteamProfile bm cache error for %s: %v", steamID, bmCacheErr)
		writeError(w, http.StatusInternalServerError, "failed to fetch bm profile cache")
		return
	}

	steamFresh := steamCacheErr == nil && time.Since(cachedSteam.UpdatedAt) < steamAccountCacheTTL
	bmFresh := bmCacheErr == nil && time.Since(cachedBm.UpdatedAt) < steamAccountCacheTTL
	if steamFresh && bmFresh {
		writeJSON(w, http.StatusOK, h.steamProfileResponseWithStatus(r.Context(), account, cachedSteam, cachedBm))
		return
	}

	refreshedSteam, refreshedBm, refreshErr := h.fetchFreshSteamCache(r.Context(), account)
	canUseCached := steamCacheErr == nil && bmCacheErr == nil
	if refreshErr != nil {
		log.Printf("[backend] GetSteamProfile refresh error for %s: %v", steamID, refreshErr)
		if errors.Is(refreshErr, battlemetrics.ErrPlayerNotFound) {
			writeError(w, http.StatusNotFound, "battlemetrics profile private")
			return
		}

		if canUseCached {
			writeJSON(w, http.StatusOK, h.steamProfileResponseWithStatus(r.Context(), account, cachedSteam, cachedBm))
			return
		}

		writeError(w, http.StatusInternalServerError, "failed to refresh steam profile")
		return
	}

	updatedSteam, err := h.postgres.UpsertSteamAccountCache(r.Context(), refreshedSteam)
	if err != nil {
		log.Printf("[backend] GetSteamProfile steam cache update error for %s: %v", steamID, err)
		if canUseCached {
			writeJSON(w, http.StatusOK, h.steamProfileResponseWithStatus(r.Context(), account, cachedSteam, cachedBm))
			return
		}

		writeError(w, http.StatusInternalServerError, "failed to cache steam profile")
		return
	}
	updatedBm, err := h.postgres.UpsertBmProfileCache(r.Context(), refreshedBm)
	if err != nil {
		log.Printf("[backend] GetSteamProfile bm cache update error for %s: %v", steamID, err)
		if canUseCached {
			writeJSON(w, http.StatusOK, h.steamProfileResponseWithStatus(r.Context(), account, cachedSteam, cachedBm))
			return
		}

		writeError(w, http.StatusInternalServerError, "failed to cache bm profile")
		return
	}

	writeJSON(w, http.StatusOK, h.steamProfileResponseWithStatus(r.Context(), account, updatedSteam, updatedBm))
}

func (h *Handler) steamAnalyticsResponseFromCache(ctx context.Context, account store.SteamAccount, steamCache store.SteamAccountCache, bmCache store.BmProfileCache) steamAnalyticsResponse {
	bmID := bmCache.BmID
	if bmID == "" && account.BmID != nil {
		bmID = *account.BmID
	}
	bmName := bmCache.BmName
	if bmName == "" {
		bmName = account.BmName
	}
	avatarImage := steamCache.AvatarImage
	if avatarImage == "" {
		avatarImage = account.AvatarImage
	}
	response := steamAnalyticsResponse{
		SteamID:      account.SteamID,
		BmID:         bmID,
		BmName:       bmName,
		AvatarImage:  avatarImage,
		TotalGames:   steamCache.TotalGames,
		TopGames:     make([]steamAnalyticsGame, 0, len(steamCache.TopGames)),
		TotalServers: bmCache.BmServersPlayed,
		TopServers:   make([]steamAnalyticsServer, 0, len(bmCache.TopServers)),
		Gaps:         h.nameGapsForBmID(ctx, bmID),
	}
	for _, game := range steamCache.TopGames {
		response.TopGames = append(response.TopGames, steamAnalyticsGame{AppID: game.AppID, Name: game.Name, Hours: game.Hours})
	}
	for _, server := range bmCache.TopServers {
		response.TopServers = append(response.TopServers, steamAnalyticsServer{BmID: server.BmID, Name: server.Name, Hours: server.Hours})
	}
	return response
}

func (h *Handler) buildSteamCacheFromProfile(ctx context.Context, account store.SteamAccount, profile battlemetrics.PlayerProfile) (store.SteamAccountCache, store.BmProfileCache, error) {
	avatarImage := account.AvatarImage
	personaName := ""
	summary, summaryErr := h.steamSummaryClient.GetPlayerSummary(ctx, account.SteamID)
	steamProfilePrivate := false
	if summaryErr != nil {
		log.Printf("[backend] fetchFreshSteamProfile steam summary error for %s: %v", account.SteamID, summaryErr)
	} else {
		steamProfilePrivate = summary.CommunityVisibilityState == 1
		if summary.AvatarFull != "" {
			avatarImage = summary.AvatarFull
		}
		personaName = summary.PersonaName
	}

	if profile.BmName == "" {
		profile.BmName = account.BmName
	}

	trackedServers, err := h.postgres.ListTrackedServers(ctx)
	if err != nil {
		return store.SteamAccountCache{}, store.BmProfileCache{}, fmt.Errorf("load tracked servers: %w", err)
	}

	trackedByID := make(map[string]store.TrackedServer, len(trackedServers))
	for _, server := range trackedServers {
		trackedByID[server.BmID] = server
	}

	steamCache := store.SteamAccountCache{SteamID: account.SteamID, PersonaName: personaName, AvatarImage: avatarImage}
	bmCache := store.BmProfileCache{BmID: *account.BmID, BmName: profile.BmName}
	if profile.CreatedAt != nil {
		bmCache.FirstSeenAt = profile.CreatedAt
	}

	if !steamProfilePrivate {
		ownedGames, ownedGamesErr := h.steamSummaryClient.GetOwnedGames(ctx, account.SteamID)
		if ownedGamesErr != nil {
			// Owned games can be private even when the summary visibility is not 1
			// (e.g. summary lookup failed, or Steam reports the profile as public
			// but the games list specifically is restricted). Treat as private:
			// keep the BM name + cache, just skip game stats.
			log.Printf("[backend] buildSteamCacheFromProfile owned games error for %s: %v", account.SteamID, ownedGamesErr)
		} else {
			sort.Slice(ownedGames, func(i, j int) bool {
				return ownedGames[i].PlaytimeForever > ownedGames[j].PlaytimeForever
			})
			totalGames := len(ownedGames)
			steamCache.TotalGames = &totalGames
			steamCache.TopGames = make([]store.AnalyticsGame, 0, min(5, totalGames))
			for i := range min(5, totalGames) {
				game := ownedGames[i]
				steamCache.TopGames = append(steamCache.TopGames, store.AnalyticsGame{AppID: game.AppID, Name: game.Name, Hours: game.PlaytimeForever / 60})
			}
			for _, game := range ownedGames {
				if game.AppID != 252490 {
					continue
				}

				rustHours := game.PlaytimeForever / 60
				steamCache.RustHours = &rustHours
				break
			}
		}
	}

	bmCache = applyBmProfileToCache(bmCache, profile, trackedByID)
	return steamCache, bmCache, nil
}

// applyBmProfileToCache fills the BattleMetrics-derived hour buckets, server list,
// and summary data on a BmProfileCache from a fetched BattleMetrics profile.
func applyBmProfileToCache(bmCache store.BmProfileCache, profile battlemetrics.PlayerProfile, trackedByID map[string]store.TrackedServer) store.BmProfileCache {
	var vitalitySeconds, atlasSeconds, vitalSeconds, moddedSeconds, officialSeconds, buildSeconds, bmSeconds int64
	rustServers := make([]battlemetrics.PlayerServer, 0, len(profile.Servers))

	for _, server := range profile.Servers {
		if server.Game != "" && server.Game != "rust" {
			continue
		}
		rustServers = append(rustServers, server)
		bmSeconds += server.TimePlayed

		switch server.RustType {
		case "modded":
			moddedSeconds += server.TimePlayed
		case "official":
			officialSeconds += server.TimePlayed
		}

		tracked, ok := trackedByID[server.ServerID]
		if !ok {
			continue
		}
		if tracked.IsVitality {
			vitalitySeconds += server.TimePlayed
		}
		if tracked.IsAtlas {
			atlasSeconds += server.TimePlayed
		}
		if tracked.IsVital {
			vitalSeconds += server.TimePlayed
		}
		if tracked.IsBuild {
			buildSeconds += server.TimePlayed
		}
	}

	bmCache.VitalityHours = toWholeHours(vitalitySeconds)
	bmCache.AtlasHours = toWholeHours(atlasSeconds)
	bmCache.VitalHours = toWholeHours(vitalSeconds)
	bmCache.ModdedHours = toWholeHours(moddedSeconds)
	bmCache.OfficialHours = toWholeHours(officialSeconds)
	bmCache.BuildHours = toWholeHours(buildSeconds)
	bmCache.BmHours = toWholeHours(bmSeconds)
	bmCache.BmServersPlayed = len(rustServers)
	sort.Slice(rustServers, func(i, j int) bool {
		return rustServers[i].TimePlayed > rustServers[j].TimePlayed
	})
	bmCache.TopServers = make([]store.AnalyticsServer, 0, min(5, len(rustServers)))
	for i := range min(5, len(rustServers)) {
		server := rustServers[i]
		bmCache.TopServers = append(bmCache.TopServers, store.AnalyticsServer{BmID: server.ServerID, Name: server.ServerName, Hours: toWholeHours(server.TimePlayed)})
	}
	return bmCache
}

func (h *Handler) fetchFreshSteamCache(ctx context.Context, account store.SteamAccount) (store.SteamAccountCache, store.BmProfileCache, error) {
	if account.BmID == nil || strings.TrimSpace(*account.BmID) == "" {
		return store.SteamAccountCache{}, store.BmProfileCache{}, store.ErrSteamAccountNotFound
	}

	profile, err := h.battleMetricsClient.FetchPlayerProfile(ctx, *account.BmID)
	if err != nil {
		return store.SteamAccountCache{}, store.BmProfileCache{}, fmt.Errorf("fetch battlemetrics profile: %w", err)
	}

	steamCache, bmCache, err := h.buildSteamCacheFromProfile(ctx, account, profile)
	if err != nil {
		return store.SteamAccountCache{}, store.BmProfileCache{}, err
	}
	if err := h.upsertBmNameHistoryFromProfile(ctx, profile); err != nil {
		log.Printf("[backend] fetchFreshSteamCache bm name history update error for %s: %v", *account.BmID, err)
	}
	return steamCache, bmCache, nil
}

// refreshSteamOnlyCache fetches the Steam-side data (persona name, avatar,
// owned games / Rust hours) without touching BattleMetrics. Used when the BM
// profile is unreachable so /view user can still show steam-side stats and a
// display name.
func (h *Handler) refreshSteamOnlyCache(ctx context.Context, account store.SteamAccount) (store.SteamAccountCache, error) {
	cache := store.SteamAccountCache{SteamID: account.SteamID, AvatarImage: account.AvatarImage}
	summary, err := h.steamSummaryClient.GetPlayerSummary(ctx, account.SteamID)
	if err != nil {
		return cache, err
	}
	cache.PersonaName = summary.PersonaName
	if summary.AvatarFull != "" {
		cache.AvatarImage = summary.AvatarFull
	}

	if summary.CommunityVisibilityState == 1 {
		return cache, nil
	}

	ownedGames, ownedGamesErr := h.steamSummaryClient.GetOwnedGames(ctx, account.SteamID)
	if ownedGamesErr != nil {
		// Games list can be private even when visibility isn't 1; tolerate.
		log.Printf("[backend] refreshSteamOnlyCache owned games error for %s: %v", account.SteamID, ownedGamesErr)
		return cache, nil
	}
	sort.Slice(ownedGames, func(i, j int) bool {
		return ownedGames[i].PlaytimeForever > ownedGames[j].PlaytimeForever
	})
	totalGames := len(ownedGames)
	cache.TotalGames = &totalGames
	cache.TopGames = make([]store.AnalyticsGame, 0, min(5, totalGames))
	for i := range min(5, totalGames) {
		game := ownedGames[i]
		cache.TopGames = append(cache.TopGames, store.AnalyticsGame{AppID: game.AppID, Name: game.Name, Hours: game.PlaytimeForever / 60})
	}
	for _, game := range ownedGames {
		if game.AppID != 252490 {
			continue
		}
		rustHours := game.PlaytimeForever / 60
		cache.RustHours = &rustHours
		break
	}
	return cache, nil
}

func (h *Handler) upsertResolvedSteamAccount(ctx context.Context, steamID string, profile battlemetrics.PlayerProfile) (store.SteamAccount, error) {
	bmID := strings.TrimSpace(profile.BmID)
	if steamID == "" || bmID == "" {
		return store.SteamAccount{}, store.ErrSteamAccountNotFound
	}

	account, err := h.postgres.UpsertSteamAccount(ctx, steamID, &bmID)
	if err != nil {
		return store.SteamAccount{}, err
	}
	account.BmID = &bmID
	account.BmName = profile.BmName

	steamCache, bmCache, err := h.buildSteamCacheFromProfile(ctx, account, profile)
	if err != nil {
		return store.SteamAccount{}, err
	}
	if _, err := h.postgres.UpsertSteamAccountCache(ctx, steamCache); err != nil {
		return store.SteamAccount{}, err
	}
	if _, err := h.postgres.UpsertBmProfileCache(ctx, bmCache); err != nil {
		return store.SteamAccount{}, err
	}
	if err := h.upsertBmNameHistoryFromProfile(ctx, profile); err != nil {
		log.Printf("[backend] upsertResolvedSteamAccount bm name history update error for %s: %v", bmID, err)
	}

	return h.postgres.GetSteamAccountBySteamID(ctx, steamID)
}

func (h *Handler) resolveSteamAccountBySteamID(ctx context.Context, steamID string) (store.SteamAccount, error) {
	bmID, err := h.bmMatch.SteamToBM(ctx, steamID)
	if err != nil {
		if errors.Is(err, bmmatch.ErrNotFound) || errors.Is(err, bmmatch.ErrNotConfigured) {
			return store.SteamAccount{}, store.ErrSteamAccountNotFound
		}
		return store.SteamAccount{}, err
	}
	bmID = strings.TrimSpace(bmID)
	if bmID == "" {
		return store.SteamAccount{}, store.ErrSteamAccountNotFound
	}

	fullProfile, err := h.battleMetricsClient.FetchPlayerProfile(ctx, bmID)
	if errors.Is(err, battlemetrics.ErrPlayerNotFound) {
		// bmmatch resolved the linkage but BM doesn't see the player yet.
		// Record the bare steam↔bm linkage and warm the Steam-only cache so
		// the immediately-following GetSteamProfile call doesn't have to do a
		// cold Steam fetch under interaction latency. The BM cache stays empty
		// until BM indexes the player; a later refresh will warm it.
		bmIDCopy := bmID
		account, upsertErr := h.postgres.UpsertSteamAccount(ctx, steamID, &bmIDCopy)
		if upsertErr != nil {
			return store.SteamAccount{}, upsertErr
		}
		if steamCache, warmErr := h.refreshSteamOnlyCache(ctx, account); warmErr != nil {
			log.Printf("[backend] resolveSteamAccountBySteamID steam-only warm error for %s: %v", steamID, warmErr)
		} else if _, cacheErr := h.postgres.UpsertSteamAccountCache(ctx, steamCache); cacheErr != nil {
			log.Printf("[backend] resolveSteamAccountBySteamID steam cache upsert error for %s: %v", steamID, cacheErr)
		}
		return h.postgres.GetSteamAccountBySteamID(ctx, steamID)
	}
	if err != nil {
		return store.SteamAccount{}, err
	}
	if fullProfile.SteamID == "" {
		fullProfile.SteamID = steamID
	}
	if fullProfile.BmID == "" {
		fullProfile.BmID = bmID
	}

	return h.upsertResolvedSteamAccount(ctx, fullProfile.SteamID, fullProfile)
}

func (h *Handler) resolveSteamAccountByBmID(ctx context.Context, bmID string) (store.SteamAccount, error) {
	profile, err := h.battleMetricsClient.FetchPlayerProfile(ctx, bmID)
	if errors.Is(err, battlemetrics.ErrPlayerNotFound) {
		// BM doesn't see this player at all (e.g. brand-new BM ID not yet
		// indexed by the public /players/{id} endpoint). Fall back to the
		// matching service so we can still record the Steam linkage.
		return h.resolveSteamAccountFromBmMatch(ctx, bmID)
	}
	if err != nil {
		return store.SteamAccount{}, err
	}

	steamID := strings.TrimSpace(profile.SteamID)
	if steamID == "" {
		// The unauthenticated BM /players/{id} response may omit identifiers,
		// so fall back to the matching service for the Steam ID.
		resolved, lookupErr := h.bmMatch.BMToSteam(ctx, bmID)
		if lookupErr != nil {
			if errors.Is(lookupErr, bmmatch.ErrNotFound) || errors.Is(lookupErr, bmmatch.ErrNotConfigured) {
				return store.SteamAccount{}, store.ErrSteamAccountNotFound
			}
			return store.SteamAccount{}, lookupErr
		}
		steamID = strings.TrimSpace(resolved)
		if steamID == "" {
			return store.SteamAccount{}, store.ErrSteamAccountNotFound
		}
		profile.SteamID = steamID
	}

	return h.upsertResolvedSteamAccount(ctx, profile.SteamID, profile)
}

// resolveSteamAccountFromBmMatch resolves a Steam ID for a BM ID via the
// bmmatch service when BattleMetrics itself can't see the player, then records
// the bare linkage in steam_accounts. Profile/cache rows are left empty — a
// later refresh will warm them once BM does see the player.
func (h *Handler) resolveSteamAccountFromBmMatch(ctx context.Context, bmID string) (store.SteamAccount, error) {
	resolved, err := h.bmMatch.BMToSteam(ctx, bmID)
	if err != nil {
		if errors.Is(err, bmmatch.ErrNotFound) || errors.Is(err, bmmatch.ErrNotConfigured) {
			return store.SteamAccount{}, store.ErrSteamAccountNotFound
		}
		return store.SteamAccount{}, err
	}
	steamID := strings.TrimSpace(resolved)
	if steamID == "" {
		return store.SteamAccount{}, store.ErrSteamAccountNotFound
	}
	bmIDCopy := bmID
	if _, err := h.postgres.UpsertSteamAccount(ctx, steamID, &bmIDCopy); err != nil {
		return store.SteamAccount{}, err
	}
	return h.postgres.GetSteamAccountBySteamID(ctx, steamID)
}

type steamAnalyticsGame struct {
	AppID int    `json:"appId"`
	Name  string `json:"name"`
	Hours int64  `json:"hours"`
}

type steamAnalyticsServer struct {
	BmID  string `json:"bmId"`
	Name  string `json:"name"`
	Hours int64  `json:"hours"`
}

type steamAnalyticsGap struct {
	FromUnix int64 `json:"fromUnix"`
	ToUnix   int64 `json:"toUnix"`
}

type steamAnalyticsResponse struct {
	SteamID      string                 `json:"steamId"`
	BmID         string                 `json:"bmId"`
	BmName       string                 `json:"bmName"`
	AvatarImage  string                 `json:"avatarImage"`
	TotalGames   *int                   `json:"totalGames"`
	TopGames     []steamAnalyticsGame   `json:"topGames"`
	TotalServers int                    `json:"totalServers"`
	TopServers   []steamAnalyticsServer `json:"topServers"`
	Gaps         []steamAnalyticsGap    `json:"gaps"`
}

const analyticsGapThreshold = 30 * 24 * time.Hour // ~1 month

func (h *Handler) GetSteamAnalytics(w http.ResponseWriter, r *http.Request) {
	steamID := chi.URLParam(r, "steamID")
	if steamID == "" || !steamIDPattern.MatchString(steamID) {
		writeError(w, http.StatusBadRequest, "steamID must be a 17-digit number")
		return
	}

	account, err := h.postgres.GetSteamAccountBySteamID(r.Context(), steamID)
	if errors.Is(err, store.ErrSteamAccountNotFound) {
		writeError(w, http.StatusNotFound, "steam account not found")
		return
	}
	if err != nil {
		log.Printf("[backend] GetSteamAnalytics account error for %s: %v", steamID, err)
		writeError(w, http.StatusInternalServerError, "failed to fetch steam account")
		return
	}
	if account.BmID == nil || strings.TrimSpace(*account.BmID) == "" {
		writeError(w, http.StatusNotFound, "steam account has no linked battlemetrics id")
		return
	}

	cachedSteam, steamCacheErr := h.postgres.GetSteamAccountCache(r.Context(), steamID)
	cachedBm, bmCacheErr := h.postgres.GetBmProfileCache(r.Context(), *account.BmID)
	canUseCached := steamCacheErr == nil && bmCacheErr == nil
	if canUseCached && time.Since(cachedSteam.UpdatedAt) < steamAccountCacheTTL && time.Since(cachedBm.UpdatedAt) < steamAccountCacheTTL {
		writeJSON(w, http.StatusOK, h.steamAnalyticsResponseFromCache(r.Context(), account, cachedSteam, cachedBm))
		return
	}

	refreshedSteam, refreshedBm, err := h.fetchFreshSteamCache(r.Context(), account)
	if err != nil {
		log.Printf("[backend] GetSteamAnalytics refresh error for %s: %v", steamID, err)
		if canUseCached {
			writeJSON(w, http.StatusOK, h.steamAnalyticsResponseFromCache(r.Context(), account, cachedSteam, cachedBm))
			return
		}
		writeError(w, http.StatusBadGateway, "failed to fetch analytics")
		return
	}

	updatedSteam, err := h.postgres.UpsertSteamAccountCache(r.Context(), refreshedSteam)
	if err != nil {
		log.Printf("[backend] GetSteamAnalytics steam cache update error for %s: %v", steamID, err)
		if canUseCached {
			writeJSON(w, http.StatusOK, h.steamAnalyticsResponseFromCache(r.Context(), account, cachedSteam, cachedBm))
			return
		}
		writeJSON(w, http.StatusOK, h.steamAnalyticsResponseFromCache(r.Context(), account, refreshedSteam, refreshedBm))
		return
	}
	updatedBm, err := h.postgres.UpsertBmProfileCache(r.Context(), refreshedBm)
	if err != nil {
		log.Printf("[backend] GetSteamAnalytics bm cache update error for %s: %v", steamID, err)
		if canUseCached {
			writeJSON(w, http.StatusOK, h.steamAnalyticsResponseFromCache(r.Context(), account, cachedSteam, cachedBm))
			return
		}
		writeJSON(w, http.StatusOK, h.steamAnalyticsResponseFromCache(r.Context(), account, updatedSteam, refreshedBm))
		return
	}

	writeJSON(w, http.StatusOK, h.steamAnalyticsResponseFromCache(r.Context(), account, updatedSteam, updatedBm))
}

func (h *Handler) upsertBmNameHistoryFromProfile(ctx context.Context, profile battlemetrics.PlayerProfile) error {
	bmID := strings.TrimSpace(profile.BmID)
	if bmID == "" || len(profile.NameIdentifiers) == 0 {
		return nil
	}

	items := make([]store.BmNameHistoryInput, 0, len(profile.NameIdentifiers))
	for _, identifier := range profile.NameIdentifiers {
		name := strings.Join(strings.Fields(identifier.Name), " ")
		if name == "" || identifier.LastSeen == nil {
			continue
		}
		items = append(items, store.BmNameHistoryInput{Name: name, LastSeenAt: *identifier.LastSeen})
	}
	return h.postgres.UpsertBmNameHistory(ctx, bmID, items)
}

func (h *Handler) nameGapsForBmID(ctx context.Context, bmID string) []steamAnalyticsGap {
	if bmID == "" {
		return []steamAnalyticsGap{}
	}
	history, err := h.postgres.ListBmNameHistory(ctx, bmID)
	if err != nil {
		log.Printf("[backend] bm name history load error for %s: %v", bmID, err)
		return []steamAnalyticsGap{}
	}
	return computeNameGaps(history)
}

func computeNameGaps(history []store.BmNameHistory) []steamAnalyticsGap {
	gaps := []steamAnalyticsGap{}
	filtered := make([]store.BmNameHistory, 0, len(history))
	for _, item := range history {
		if item.LastSeenAt.IsZero() {
			continue
		}
		filtered = append(filtered, item)
	}
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].LastSeenAt.Before(filtered[j].LastSeenAt)
	})

	for i := 0; i+1 < len(filtered); i++ {
		current := filtered[i]
		next := filtered[i+1]
		if next.LastSeenAt.Sub(current.LastSeenAt) < analyticsGapThreshold {
			continue
		}
		gaps = append(gaps, steamAnalyticsGap{
			FromUnix: current.LastSeenAt.Unix(),
			ToUnix:   next.LastSeenAt.Unix(),
		})
	}
	return gaps
}

func steamDiscordLinkToMap(link store.SteamDiscordLink) map[string]any {
	return map[string]any{
		"steamId":   link.SteamID,
		"discordId": link.DiscordID,
		"type":      link.Type,
		"source":    link.Source,
		"createdAt": link.CreatedAt,
	}
}

func hiddenDiscordLinkIdentityToMap(record store.HiddenDiscordLinkIdentity) map[string]any {
	return map[string]any{
		"discordId": record.DiscordID,
		"reason":    record.Reason,
		"createdBy": record.CreatedBy,
		"createdAt": record.CreatedAt,
		"updatedAt": record.UpdatedAt,
	}
}

func writeSteamAccount(w http.ResponseWriter, record store.SteamAccount) {
	writeJSON(w, http.StatusOK, steamAccountToMap(record))
}

func writeSteamDiscordLinkList(w http.ResponseWriter, links []store.SteamDiscordLink) {
	items := make([]map[string]any, 0, len(links))
	for _, link := range links {
		items = append(items, steamDiscordLinkToMap(link))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func setSteamCandidateToMap(candidate store.SetSteamCandidate) map[string]any {
	return map[string]any{
		"steamId":     candidate.SteamID,
		"type":        candidate.Type,
		"displayName": candidate.DisplayName,
		"createdAt":   candidate.CreatedAt,
	}
}

func writeSetSteamCandidateList(w http.ResponseWriter, candidates []store.SetSteamCandidate) {
	items := make([]map[string]any, 0, len(candidates))
	for _, candidate := range candidates {
		items = append(items, setSteamCandidateToMap(candidate))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

type hideDiscordLinkIdentityRequest struct {
	Reason    string `json:"reason"`
	CreatedBy string `json:"createdBy"`
}

func (h *Handler) GetSteamAccountCount(w http.ResponseWriter, r *http.Request) {
	count, err := h.postgres.CountSteamAccounts(r.Context())
	if err != nil {
		log.Printf("[backend] GetSteamAccountCount error: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to count steam accounts")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"count": count})
}

func (h *Handler) GetSteamAccount(w http.ResponseWriter, r *http.Request) {
	steamID := chi.URLParam(r, "steamID")
	if steamID == "" || !steamIDPattern.MatchString(steamID) {
		writeError(w, http.StatusBadRequest, "steamID must be a 17-digit number")
		return
	}

	account, err := h.postgres.GetSteamAccountBySteamID(r.Context(), steamID)
	if errors.Is(err, store.ErrSteamAccountNotFound) {
		account, err = h.resolveSteamAccountBySteamID(r.Context(), steamID)
		if errors.Is(err, store.ErrSteamAccountNotFound) || errors.Is(err, battlemetrics.ErrPlayerNotFound) {
			writeError(w, http.StatusNotFound, "steam account not found")
			return
		}
		if err != nil {
			log.Printf("[backend] GetSteamAccount resolve error for %s: %v", steamID, err)
			writeError(w, http.StatusInternalServerError, "failed to fetch steam account")
			return
		}
	}
	if err != nil {
		log.Printf("[backend] GetSteamAccount error for %s: %v", steamID, err)
		writeError(w, http.StatusInternalServerError, "failed to fetch steam account")
		return
	}
	if account.BmID == nil || strings.TrimSpace(*account.BmID) == "" {
		account, err = h.resolveSteamAccountBySteamID(r.Context(), steamID)
		if errors.Is(err, store.ErrSteamAccountNotFound) || errors.Is(err, battlemetrics.ErrPlayerNotFound) {
			writeError(w, http.StatusNotFound, "steam account not found")
			return
		}
		if err != nil {
			log.Printf("[backend] GetSteamAccount backfill error for %s: %v", steamID, err)
			writeError(w, http.StatusInternalServerError, "failed to fetch steam account")
			return
		}
	}

	writeSteamAccount(w, account)
}

func (h *Handler) GetSteamAccountByBmID(w http.ResponseWriter, r *http.Request) {
	bmID := chi.URLParam(r, "bmID")
	if bmID == "" {
		writeError(w, http.StatusBadRequest, "bmID is required")
		return
	}

	account, err := h.postgres.GetSteamAccountByBmID(r.Context(), bmID)
	if errors.Is(err, store.ErrSteamAccountNotFound) {
		account, err = h.resolveSteamAccountByBmID(r.Context(), bmID)
		if errors.Is(err, store.ErrSteamAccountNotFound) || errors.Is(err, battlemetrics.ErrPlayerNotFound) {
			writeError(w, http.StatusNotFound, "steam account not found")
			return
		}
		if err != nil {
			log.Printf("[backend] GetSteamAccountByBmID resolve error for %s: %v", bmID, err)
			writeError(w, http.StatusInternalServerError, "failed to fetch steam account")
			return
		}
	}
	if err != nil {
		log.Printf("[backend] GetSteamAccountByBmID error for %s: %v", bmID, err)
		writeError(w, http.StatusInternalServerError, "failed to fetch steam account")
		return
	}

	writeSteamAccount(w, account)
}

// GetBmPlayerAnalytics mirrors GetSteamAnalytics for BM-only flows. It uses
// the BM-keyed cache for BattleMetrics analytics fields and pulls Steam-only
// fields (top games, total games) from steam_account_cache when a Steam
// linkage exists.
func (h *Handler) GetBmPlayerAnalytics(w http.ResponseWriter, r *http.Request) {
	bmID := strings.TrimSpace(chi.URLParam(r, "bmID"))
	if !bmIDPattern.MatchString(bmID) {
		writeError(w, http.StatusBadRequest, "bmID must be a numeric BattleMetrics player ID")
		return
	}

	var account store.SteamAccount
	var steamCache store.SteamAccountCache
	if linked, err := h.postgres.GetSteamAccountByBmID(r.Context(), bmID); err == nil {
		account = linked
		if cache, cacheErr := h.postgres.GetSteamAccountCache(r.Context(), linked.SteamID); cacheErr == nil {
			steamCache = cache
		}
	} else if !errors.Is(err, store.ErrSteamAccountNotFound) {
		log.Printf("[backend] GetBmPlayerAnalytics steam lookup error for %s: %v", bmID, err)
	}
	bmIDCopy := bmID
	if account.SteamID == "" {
		account.BmID = &bmIDCopy
	}

	cachedBm, bmCacheErr := h.postgres.GetBmProfileCache(r.Context(), bmID)
	if bmCacheErr != nil && !errors.Is(bmCacheErr, store.ErrSteamAccountNotFound) {
		log.Printf("[backend] GetBmPlayerAnalytics bm cache error for %s: %v", bmID, bmCacheErr)
		writeError(w, http.StatusInternalServerError, "failed to fetch bm profile cache")
		return
	}
	if bmCacheErr == nil && time.Since(cachedBm.UpdatedAt) < steamAccountCacheTTL {
		writeJSON(w, http.StatusOK, h.steamAnalyticsResponseFromCache(r.Context(), account, steamCache, cachedBm))
		return
	}

	profile, err := h.battleMetricsClient.FetchPlayerProfile(r.Context(), bmID)
	if errors.Is(err, battlemetrics.ErrPlayerNotFound) {
		writeError(w, http.StatusNotFound, "battlemetrics player not found")
		return
	}
	if err != nil {
		log.Printf("[backend] GetBmPlayerAnalytics fetch error for %s: %v", bmID, err)
		if bmCacheErr == nil {
			writeJSON(w, http.StatusOK, h.steamAnalyticsResponseFromCache(r.Context(), account, steamCache, cachedBm))
			return
		}
		writeError(w, http.StatusBadGateway, "failed to fetch battlemetrics profile")
		return
	}
	if profile.BmID == "" {
		profile.BmID = bmID
	}

	trackedServers, listErr := h.postgres.ListTrackedServers(r.Context())
	if listErr != nil {
		log.Printf("[backend] GetBmPlayerAnalytics load tracked servers error for %s: %v", bmID, listErr)
		writeError(w, http.StatusInternalServerError, "failed to load tracked servers")
		return
	}
	trackedByID := make(map[string]store.TrackedServer, len(trackedServers))
	for _, server := range trackedServers {
		trackedByID[server.BmID] = server
	}
	freshBm := store.BmProfileCache{BmID: bmID, BmName: profile.BmName}
	if profile.CreatedAt != nil {
		freshBm.FirstSeenAt = profile.CreatedAt
	}
	freshBm = applyBmProfileToCache(freshBm, profile, trackedByID)
	updatedBm, err := h.postgres.UpsertBmProfileCache(r.Context(), freshBm)
	if err != nil {
		log.Printf("[backend] GetBmPlayerAnalytics cache update error for %s: %v", bmID, err)
		writeJSON(w, http.StatusOK, h.steamAnalyticsResponseFromCache(r.Context(), account, steamCache, freshBm))
		return
	}
	if err := h.upsertBmNameHistoryFromProfile(r.Context(), profile); err != nil {
		log.Printf("[backend] GetBmPlayerAnalytics bm name history update error for %s: %v", bmID, err)
	}

	writeJSON(w, http.StatusOK, h.steamAnalyticsResponseFromCache(r.Context(), account, steamCache, updatedBm))
}

// GetBmPlayerProfile returns the BattleMetrics profile for a player by BM ID
// using the BM-keyed cache, falling back to a fresh BattleMetrics fetch when
// the cache is missing or stale. The response uses the same shape as the
// steam-linked profile endpoint so the bot can render a single profile card
// for both flows. Steam-only fields (rust hours, avatar) are populated only
// when a Steam linkage exists.
func (h *Handler) GetBmPlayerProfile(w http.ResponseWriter, r *http.Request) {
	bmID := strings.TrimSpace(chi.URLParam(r, "bmID"))
	if !bmIDPattern.MatchString(bmID) {
		writeError(w, http.StatusBadRequest, "bmID must be a numeric BattleMetrics player ID")
		return
	}

	// Try to find an existing Steam linkage for steam-only enrichment.
	var account store.SteamAccount
	var steamCache store.SteamAccountCache
	if linked, err := h.postgres.GetSteamAccountByBmID(r.Context(), bmID); err == nil {
		account = linked
		if cache, cacheErr := h.postgres.GetSteamAccountCache(r.Context(), linked.SteamID); cacheErr == nil {
			steamCache = cache
		}
	} else if !errors.Is(err, store.ErrSteamAccountNotFound) {
		log.Printf("[backend] GetBmPlayerProfile steam lookup error for %s: %v", bmID, err)
	}
	bmIDCopy := bmID
	if account.SteamID == "" {
		account.BmID = &bmIDCopy
	}

	cachedBm, bmCacheErr := h.postgres.GetBmProfileCache(r.Context(), bmID)
	if bmCacheErr != nil && !errors.Is(bmCacheErr, store.ErrSteamAccountNotFound) {
		log.Printf("[backend] GetBmPlayerProfile bm cache error for %s: %v", bmID, bmCacheErr)
		writeError(w, http.StatusInternalServerError, "failed to fetch bm profile cache")
		return
	}
	if bmCacheErr == nil && time.Since(cachedBm.UpdatedAt) < steamAccountCacheTTL {
		writeJSON(w, http.StatusOK, h.steamProfileResponseWithStatus(r.Context(), account, steamCache, cachedBm))
		return
	}

	profile, err := h.battleMetricsClient.FetchPlayerProfile(r.Context(), bmID)
	if errors.Is(err, battlemetrics.ErrPlayerNotFound) {
		// BM doesn't see this player. If we have a stale BM cache, prefer it.
		// Otherwise try bmmatch to recover a Steam linkage so we can still
		// serve Steam-side enrichment instead of 404'ing the bot.
		if bmCacheErr == nil {
			writeJSON(w, http.StatusOK, h.steamProfileResponseWithStatus(r.Context(), account, steamCache, cachedBm))
			return
		}
		if account.SteamID == "" {
			if linked, lookupErr := h.resolveSteamAccountFromBmMatch(r.Context(), bmID); lookupErr == nil {
				account = linked
				if cache, cacheErr := h.postgres.GetSteamAccountCache(r.Context(), linked.SteamID); cacheErr == nil {
					steamCache = cache
				}
			} else if !errors.Is(lookupErr, store.ErrSteamAccountNotFound) {
				log.Printf("[backend] GetBmPlayerProfile bmmatch lookup error for %s: %v", bmID, lookupErr)
			}
		}
		if account.SteamID != "" {
			writeJSON(w, http.StatusOK, h.steamProfileResponseWithStatus(r.Context(), account, steamCache, store.BmProfileCache{BmID: bmID}))
			return
		}
		writeError(w, http.StatusNotFound, "battlemetrics player not found")
		return
	}
	if err != nil {
		log.Printf("[backend] GetBmPlayerProfile error for %s: %v", bmID, err)
		if bmCacheErr == nil {
			writeJSON(w, http.StatusOK, h.steamProfileResponseWithStatus(r.Context(), account, steamCache, cachedBm))
			return
		}
		writeError(w, http.StatusBadGateway, "failed to fetch battlemetrics profile")
		return
	}
	if profile.BmID == "" {
		profile.BmID = bmID
	}

	trackedServers, listErr := h.postgres.ListTrackedServers(r.Context())
	if listErr != nil {
		log.Printf("[backend] GetBmPlayerProfile load tracked servers error for %s: %v", bmID, listErr)
		writeError(w, http.StatusInternalServerError, "failed to load tracked servers")
		return
	}
	trackedByID := make(map[string]store.TrackedServer, len(trackedServers))
	for _, server := range trackedServers {
		trackedByID[server.BmID] = server
	}

	freshBm := store.BmProfileCache{BmID: bmID, BmName: profile.BmName}
	if profile.CreatedAt != nil {
		freshBm.FirstSeenAt = profile.CreatedAt
	}
	freshBm = applyBmProfileToCache(freshBm, profile, trackedByID)
	updatedBm, err := h.postgres.UpsertBmProfileCache(r.Context(), freshBm)
	if err != nil {
		log.Printf("[backend] GetBmPlayerProfile cache update error for %s: %v", bmID, err)
		writeJSON(w, http.StatusOK, h.steamProfileResponseWithStatus(r.Context(), account, steamCache, freshBm))
		return
	}
	if err := h.upsertBmNameHistoryFromProfile(r.Context(), profile); err != nil {
		log.Printf("[backend] GetBmPlayerProfile bm name history update error for %s: %v", bmID, err)
	}

	writeJSON(w, http.StatusOK, h.steamProfileResponseWithStatus(r.Context(), account, steamCache, updatedBm))
}

func (h *Handler) GetSteamFriends(w http.ResponseWriter, r *http.Request) {
	steamID := chi.URLParam(r, "steamID")
	if steamID == "" || !steamIDPattern.MatchString(steamID) {
		writeError(w, http.StatusBadRequest, "steamID must be a 17-digit number")
		return
	}

	friends, err := h.steamClient.GetFriendList(r.Context(), steamID)
	if errors.Is(err, steamapi.ErrFriendListPrivate) {
		writeJSON(w, http.StatusOK, map[string]any{"items": []string{}, "private": true})
		return
	}
	if errors.Is(err, steamapi.ErrSteamAPIKeyRequired) {
		writeError(w, http.StatusServiceUnavailable, "steam api key is not configured")
		return
	}
	if err != nil {
		log.Printf("[backend] GetSteamFriends error for %s: %v", steamID, err)
		writeError(w, http.StatusInternalServerError, "failed to fetch friend list")
		return
	}

	if friends == nil {
		friends = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": friends, "private": false})
}

func (h *Handler) ListLinksBySteamID(w http.ResponseWriter, r *http.Request) {
	steamID := chi.URLParam(r, "steamID")
	if steamID == "" || !steamIDPattern.MatchString(steamID) {
		writeError(w, http.StatusBadRequest, "steamID must be a 17-digit number")
		return
	}

	links, err := h.postgres.ListLinksBySteamID(r.Context(), steamID)
	if err != nil {
		log.Printf("[backend] ListLinksBySteamID error for %s: %v", steamID, err)
		writeError(w, http.StatusInternalServerError, "failed to fetch links")
		return
	}

	writeSteamDiscordLinkList(w, links)
}

func (h *Handler) ListLinksByDiscordID(w http.ResponseWriter, r *http.Request) {
	discordID := chi.URLParam(r, "discordID")
	if discordID == "" || !snowflakePattern.MatchString(discordID) {
		writeError(w, http.StatusBadRequest, "discordID must be a valid Discord snowflake")
		return
	}

	links, err := h.postgres.ListLinksByDiscordID(r.Context(), discordID)
	if err != nil {
		log.Printf("[backend] ListLinksByDiscordID error for %s: %v", discordID, err)
		writeError(w, http.StatusInternalServerError, "failed to fetch links")
		return
	}

	writeSteamDiscordLinkList(w, links)
}

func (h *Handler) HideDiscordLinkIdentity(w http.ResponseWriter, r *http.Request) {
	discordID := chi.URLParam(r, "discordID")
	if discordID == "" || !snowflakePattern.MatchString(discordID) {
		writeError(w, http.StatusBadRequest, "discordID must be a valid Discord snowflake")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var request hideDiscordLinkIdentityRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	record, err := h.postgres.HideDiscordLinkIdentity(
		r.Context(),
		discordID,
		strings.TrimSpace(request.Reason),
		strings.TrimSpace(request.CreatedBy),
	)
	if err != nil {
		log.Printf("[backend] HideDiscordLinkIdentity error for %s: %v", discordID, err)
		writeError(w, http.StatusInternalServerError, "failed to hide discord link identity")
		return
	}

	writeJSON(w, http.StatusOK, hiddenDiscordLinkIdentityToMap(record))
}

func (h *Handler) UnhideDiscordLinkIdentity(w http.ResponseWriter, r *http.Request) {
	discordID := chi.URLParam(r, "discordID")
	if discordID == "" || !snowflakePattern.MatchString(discordID) {
		writeError(w, http.StatusBadRequest, "discordID must be a valid Discord snowflake")
		return
	}

	if err := h.postgres.UnhideDiscordLinkIdentity(r.Context(), discordID); err != nil {
		log.Printf("[backend] UnhideDiscordLinkIdentity error for %s: %v", discordID, err)
		writeError(w, http.StatusInternalServerError, "failed to unhide discord link identity")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *Handler) ListSetSteamCandidates(w http.ResponseWriter, r *http.Request) {
	discordID := chi.URLParam(r, "discordID")
	if discordID == "" || !snowflakePattern.MatchString(discordID) {
		writeError(w, http.StatusBadRequest, "discordID must be a valid Discord snowflake")
		return
	}

	candidates, err := h.postgres.ListSetSteamCandidates(r.Context(), discordID)
	if err != nil {
		log.Printf("[backend] ListSetSteamCandidates error for %s: %v", discordID, err)
		writeError(w, http.StatusInternalServerError, "failed to fetch setsteam candidates")
		return
	}

	writeSetSteamCandidateList(w, candidates)
}

func (h *Handler) IsDiscordWebsiteAdmin(w http.ResponseWriter, r *http.Request) {
	discordID := chi.URLParam(r, "discordID")
	if discordID == "" || !snowflakePattern.MatchString(discordID) {
		writeError(w, http.StatusBadRequest, "discordID must be a valid Discord snowflake")
		return
	}

	isAdmin, err := h.postgres.IsDiscordWebsiteAdmin(r.Context(), discordID)
	if err != nil {
		log.Printf("[backend] IsDiscordWebsiteAdmin error for %s: %v", discordID, err)
		writeError(w, http.StatusInternalServerError, "failed to check website admin status")
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"isAdmin": isAdmin})
}

func (h *Handler) HasDiscordPRORole(w http.ResponseWriter, r *http.Request) {
	discordID, ok := validateSnowflakePathParam(w, r, "discordID")
	if !ok {
		return
	}

	hasRole, err := h.postgres.DiscordUserHasEntitlement(r.Context(), discordID, store.EntitlementPRO)
	if err != nil {
		log.Printf("[backend] HasDiscordPRORole error for %s: %v", discordID, err)
		writeError(w, http.StatusInternalServerError, "failed to check PRO role")
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"hasRole": hasRole})
}

type createSoftLinkRequest struct {
	SteamID   string `json:"steamId"`
	DiscordID string `json:"discordId"`
	Source    string `json:"source"`
}

type resolveSteamIDRequest struct {
	Input string `json:"input"`
}

func (h *Handler) ResolveSteamID(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var request resolveSteamIDRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	steamID, err := h.steamClient.ResolveSteamID(r.Context(), request.Input)
	if errors.Is(err, steamapi.ErrSteamAPIKeyRequired) {
		writeError(w, http.StatusBadRequest, "vanity URL resolution is not available")
		return
	}
	if errors.Is(err, steamapi.ErrUnsupportedProfileInput) || errors.Is(err, steamapi.ErrVanityNotFound) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		log.Printf("[backend] ResolveSteamID error: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to resolve steam id")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"steamId": steamID})
}

func (h *Handler) CreateSoftLink(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var request createSoftLinkRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if request.SteamID == "" || !steamIDPattern.MatchString(request.SteamID) {
		writeError(w, http.StatusBadRequest, "steamId must be a 17-digit number")
		return
	}
	if !validateSnowflakeValue(request.DiscordID) {
		writeError(w, http.StatusBadRequest, "discordId must be a valid Discord snowflake")
		return
	}
	if request.Source == "" {
		writeError(w, http.StatusBadRequest, "source is required")
		return
	}

	link, err := h.postgres.CreateSteamDiscordLink(r.Context(), store.SteamDiscordLink{
		SteamID:   request.SteamID,
		DiscordID: request.DiscordID,
		Type:      "soft",
		Source:    request.Source,
	})
	if errors.Is(err, store.ErrLinkAlreadyExists) {
		writeError(w, http.StatusConflict, "soft link already exists for this steam and discord pair")
		return
	}
	if err != nil {
		log.Printf("[backend] CreateSoftLink error: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to create soft link")
		return
	}

	writeJSON(w, http.StatusCreated, steamDiscordLinkToMap(link))
}

type bulkHardLinkEntry struct {
	SteamID   string  `json:"steamId"`
	BmID      *string `json:"bmId"`
	DiscordID string  `json:"discordId"`
	Source    string  `json:"source"`
}

type bulkHardLinksRequest struct {
	Links []bulkHardLinkEntry `json:"links"`
}

func (h *Handler) BulkUpsertHardLinks(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var request bulkHardLinksRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if len(request.Links) == 0 {
		writeError(w, http.StatusBadRequest, "links array must not be empty")
		return
	}

	if len(request.Links) > 1000 {
		writeError(w, http.StatusBadRequest, "links array must not exceed 1000 entries")
		return
	}

	storeLinks := make([]store.BulkHardLink, 0, len(request.Links))
	for i, entry := range request.Links {
		if entry.SteamID == "" || !steamIDPattern.MatchString(entry.SteamID) {
			writeError(w, http.StatusBadRequest, "links["+strconv.Itoa(i)+"].steamId must be a 17-digit number")
			return
		}
		if !validateSnowflakeValue(entry.DiscordID) {
			writeError(w, http.StatusBadRequest, "links["+strconv.Itoa(i)+"].discordId must be a valid Discord snowflake")
			return
		}
		if entry.Source == "" {
			writeError(w, http.StatusBadRequest, "links["+strconv.Itoa(i)+"].source is required")
			return
		}
		storeLinks = append(storeLinks, store.BulkHardLink{
			SteamID:   entry.SteamID,
			BmID:      entry.BmID,
			DiscordID: entry.DiscordID,
			Source:    entry.Source,
		})
	}

	if err := h.postgres.BulkUpsertHardLinks(r.Context(), storeLinks); err != nil {
		log.Printf("[backend] BulkUpsertHardLinks error: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to upsert hard links")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "count": len(storeLinks)})
}

func (h *Handler) DeleteSoftLink(w http.ResponseWriter, r *http.Request) {
	steamID := chi.URLParam(r, "steamID")
	if steamID == "" || !steamIDPattern.MatchString(steamID) {
		writeError(w, http.StatusBadRequest, "steamID must be a 17-digit number")
		return
	}

	discordID := chi.URLParam(r, "discordID")
	if discordID == "" || !snowflakePattern.MatchString(discordID) {
		writeError(w, http.StatusBadRequest, "discordID must be a valid Discord snowflake")
		return
	}

	err := h.postgres.DeleteSoftLink(r.Context(), steamID, discordID)
	if errors.Is(err, store.ErrSteamAccountNotFound) {
		writeError(w, http.StatusNotFound, "soft link not found")
		return
	}
	if err != nil {
		log.Printf("[backend] DeleteSoftLink error for %s/%s: %v", steamID, discordID, err)
		writeError(w, http.StatusInternalServerError, "failed to delete soft link")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func guildSteamDiscordLinkToMap(link store.GuildSteamDiscordLink) map[string]any {
	return map[string]any{
		"guildId":   link.GuildID,
		"discordId": link.DiscordID,
		"steamId":   link.SteamID,
		"createdAt": link.CreatedAt,
	}
}

func writeGuildSteamDiscordLinkList(w http.ResponseWriter, links []store.GuildSteamDiscordLink) {
	items := make([]map[string]any, 0, len(links))
	for _, link := range links {
		items = append(items, guildSteamDiscordLinkToMap(link))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func guildNicknameTargetToMap(item store.GuildNicknameTarget) map[string]any {
	result := map[string]any{
		"guildId":   item.GuildID,
		"discordId": item.DiscordID,
		"steamId":   item.SteamID,
	}
	if item.BmID != nil {
		result["bmId"] = *item.BmID
	} else {
		result["bmId"] = nil
	}
	if item.BmName != nil {
		result["bmName"] = *item.BmName
	} else {
		result["bmName"] = nil
	}
	return result
}

func writeGuildNicknameTargetList(w http.ResponseWriter, items []store.GuildNicknameTarget) {
	response := make([]map[string]any, 0, len(items))
	for _, item := range items {
		response = append(response, guildNicknameTargetToMap(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": response})
}

func userGuildLinkToMap(link store.UserGuildLink) map[string]any {
	result := map[string]any{
		"discordId":   link.DiscordID,
		"guildId":     link.GuildID,
		"isActive":    link.IsActive,
		"firstSeenAt": link.FirstSeenAt,
		"lastSeenAt":  link.LastSeenAt,
	}
	if link.LeftAt != nil {
		result["leftAt"] = *link.LeftAt
	} else {
		result["leftAt"] = nil
	}
	return result
}

func writeUserGuildLinkList(w http.ResponseWriter, links []store.UserGuildLink) {
	items := make([]map[string]any, 0, len(links))
	for _, link := range links {
		items = append(items, userGuildLinkToMap(link))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func guildOnlineSummaryItemToMap(item store.GuildOnlineSummaryItem) map[string]any {
	return map[string]any{
		"guildId":    item.GuildID,
		"discordId":  item.DiscordID,
		"steamId":    item.SteamID,
		"playerBmId": item.PlayerBmID,
		"bmName":     item.BmName,
		"serverBmId": item.ServerBmID,
		"serverName": item.ServerName,
		"onlineUnix": item.OnlineUnix,
	}
}

func writeGuildOnlineSummaryList(w http.ResponseWriter, items []store.GuildOnlineSummaryItem) {
	response := make([]map[string]any, 0, len(items))
	for _, item := range items {
		response = append(response, guildOnlineSummaryItemToMap(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": response})
}

func guildRecentGameBanToMap(item store.GuildRecentGameBan) map[string]any {
	return map[string]any{
		"guildId":        item.GuildID,
		"discordId":      item.DiscordID,
		"steamId":        item.SteamID,
		"gameBannedAt":   item.GameBannedAt,
		"gameBannedUnix": item.GameBannedAt.Unix(),
		"source":         item.Source,
	}
}

func writeGuildRecentGameBanList(w http.ResponseWriter, items []store.GuildRecentGameBan) {
	response := make([]map[string]any, 0, len(items))
	for _, item := range items {
		response = append(response, guildRecentGameBanToMap(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": response})
}

type createGuildLinkRequest struct {
	DiscordID string `json:"discordId"`
	SteamID   string `json:"steamId"`
}

type createUserGuildLinkRequest struct {
	DiscordID string `json:"discordId"`
	GuildID   string `json:"guildId"`
}

func (h *Handler) CreateGuildSteamDiscordLink(w http.ResponseWriter, r *http.Request) {
	guildID, ok := validateSnowflakePathParam(w, r, "guildID")
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var request createGuildLinkRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if !validateSnowflakeValue(request.DiscordID) {
		writeError(w, http.StatusBadRequest, "discordId must be a valid Discord snowflake")
		return
	}
	if request.SteamID == "" || !steamIDPattern.MatchString(request.SteamID) {
		writeError(w, http.StatusBadRequest, "steamId must be a 17-digit number")
		return
	}

	link, err := h.postgres.CreateGuildSteamDiscordLink(r.Context(), guildID, request.DiscordID, request.SteamID)
	if errors.Is(err, store.ErrLinkAlreadyExists) {
		writeError(w, http.StatusConflict, "guild link already exists")
		return
	}
	if err != nil {
		log.Printf("[backend] CreateGuildSteamDiscordLink error: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to create guild link")
		return
	}

	writeJSON(w, http.StatusCreated, guildSteamDiscordLinkToMap(link))
}

func (h *Handler) CreateUserGuildLink(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var request createUserGuildLinkRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if !validateSnowflakeValue(request.DiscordID) {
		writeError(w, http.StatusBadRequest, "discordId must be a valid Discord snowflake")
		return
	}
	if !validateSnowflakeValue(request.GuildID) {
		writeError(w, http.StatusBadRequest, "guildId must be a valid Discord snowflake")
		return
	}

	link, err := h.postgres.CreateUserGuildLink(r.Context(), request.DiscordID, request.GuildID)
	if err != nil {
		log.Printf("[backend] CreateUserGuildLink error for %s/%s: %v", request.DiscordID, request.GuildID, err)
		writeError(w, http.StatusInternalServerError, "failed to create user guild link")
		return
	}

	writeJSON(w, http.StatusCreated, userGuildLinkToMap(link))
}

func (h *Handler) MarkUserGuildLeft(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var request createUserGuildLinkRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if !validateSnowflakeValue(request.DiscordID) {
		writeError(w, http.StatusBadRequest, "discordId must be a valid Discord snowflake")
		return
	}
	if !validateSnowflakeValue(request.GuildID) {
		writeError(w, http.StatusBadRequest, "guildId must be a valid Discord snowflake")
		return
	}

	link, err := h.postgres.MarkUserGuildLeft(r.Context(), request.DiscordID, request.GuildID)
	if err != nil {
		log.Printf("[backend] MarkUserGuildLeft error for %s/%s: %v", request.DiscordID, request.GuildID, err)
		writeError(w, http.StatusInternalServerError, "failed to update user guild link")
		return
	}

	writeJSON(w, http.StatusOK, userGuildLinkToMap(link))
}

func (h *Handler) ListGuildLinks(w http.ResponseWriter, r *http.Request) {
	guildID, ok := validateSnowflakePathParam(w, r, "guildID")
	if !ok {
		return
	}

	links, err := h.postgres.ListGuildLinks(r.Context(), guildID)
	if err != nil {
		log.Printf("[backend] ListGuildLinks error for %s: %v", guildID, err)
		writeError(w, http.StatusInternalServerError, "failed to fetch guild links")
		return
	}

	writeGuildSteamDiscordLinkList(w, links)
}

func (h *Handler) ListGuildNicknameTargets(w http.ResponseWriter, r *http.Request) {
	guildID, ok := validateSnowflakePathParam(w, r, "guildID")
	if !ok {
		return
	}

	items, err := h.postgres.ListGuildNicknameTargets(r.Context(), guildID)
	if err != nil {
		log.Printf("[backend] ListGuildNicknameTargets error for %s: %v", guildID, err)
		writeError(w, http.StatusInternalServerError, "failed to fetch guild nickname targets")
		return
	}

	writeGuildNicknameTargetList(w, items)
}

func (h *Handler) ListGuildOnlineSummary(w http.ResponseWriter, r *http.Request) {
	guildID, ok := validateSnowflakePathParam(w, r, "guildID")
	if !ok {
		return
	}

	items, err := h.postgres.ListGuildOnlineSummary(r.Context(), guildID)
	if err != nil {
		log.Printf("[backend] ListGuildOnlineSummary error for %s: %v", guildID, err)
		writeError(w, http.StatusInternalServerError, "failed to fetch guild online summary")
		return
	}

	writeGuildOnlineSummaryList(w, items)
}

func (h *Handler) CheckGuildGameBans(w http.ResponseWriter, r *http.Request) {
	guildID, ok := validateSnowflakePathParam(w, r, "guildID")
	if !ok {
		return
	}
	method := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("method")))
	if method == "" {
		method = "light"
	}
	if method != "light" && method != "deep" {
		writeError(w, http.StatusBadRequest, "method must be light or deep")
		return
	}
	timeframe, err := parseGameBanTimeframe(r.URL.Query().Get("timeframe"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	links, err := h.postgres.ListGuildLinks(r.Context(), guildID)
	if err != nil {
		log.Printf("[backend] ListGuildLinks error for game ban scan %s: %v", guildID, err)
		writeError(w, http.StatusInternalServerError, "failed to fetch guild links")
		return
	}
	scopeDiscordIDs, err := gameBanScopeDiscordIDs(r.URL.Query()["scopeDiscordId"])
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(scopeDiscordIDs) > 0 {
		links = filterGuildLinksByDiscordIDs(links, scopeDiscordIDs)
	}

	candidates := guildLinksToGameBanCandidates(links)
	if method == "deep" {
		deepDiscordIDs, err := h.deepGameBanDiscordIDs(r.Context(), guildID, links, scopeDiscordIDs)
		if err != nil {
			log.Printf("[backend] deep game ban guild member error for guild %s: %v", guildID, err)
			writeError(w, http.StatusInternalServerError, "failed to prepare deep game ban scan")
			return
		}
		deepCandidates, err := h.deepGameBanCandidates(r.Context(), guildID, deepDiscordIDs)
		if errors.Is(err, errDeepGameBanScopeTooLarge) {
			writeError(w, http.StatusUnprocessableEntity, "More than 100 people in the current scope need a deep lookup. Try setting a smaller scope or use light mode.")
			return
		}
		if err != nil {
			log.Printf("[backend] deep game ban candidate error for guild %s: %v", guildID, err)
			writeError(w, http.StatusInternalServerError, "failed to prepare deep game ban scan")
			return
		}
		candidates = append(candidates, deepCandidates...)
	}
	candidates = dedupeGameBanCandidates(candidates)

	steamIDs := uniqueGameBanCandidateSteamIDs(candidates)
	if len(steamIDs) == 0 {
		writeGuildRecentGameBanList(w, []store.GuildRecentGameBan{})
		return
	}

	now := time.Now().UTC()
	cacheBySteamID, err := h.gameBanCacheForSteamIDs(r.Context(), guildID, steamIDs, now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to refresh game ban cache")
		return
	}

	recent := recentGameBansFromCandidates(candidates, cacheBySteamID, now, timeframe)
	sort.SliceStable(recent, func(i, j int) bool {
		return recent[i].GameBannedAt.After(recent[j].GameBannedAt)
	})

	writeGuildRecentGameBanList(w, recent)
}

type gameBanCandidate struct {
	GuildID   string
	DiscordID string
	SteamID   string
	Source    string
}

var errDeepGameBanScopeTooLarge = errors.New("deep game ban scope too large")

const (
	gameBanSourceDirectGuildLink = "direct_guild_link"
	gameBanSourceLinkedAccount   = "linked_account"
)

type gameBanTimeframe struct {
	Value  string
	Window time.Duration
}

func parseGameBanTimeframe(value string) (gameBanTimeframe, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "90d":
		return gameBanTimeframe{Value: "90d", Window: 90 * 24 * time.Hour}, nil
	case "30d":
		return gameBanTimeframe{Value: "30d", Window: 30 * 24 * time.Hour}, nil
	case "180d":
		return gameBanTimeframe{Value: "180d", Window: 180 * 24 * time.Hour}, nil
	case "all":
		return gameBanTimeframe{Value: "all"}, nil
	default:
		return gameBanTimeframe{}, errors.New("timeframe must be 30d, 90d, 180d, or all")
	}
}

func gameBanScopeDiscordIDs(values []string) (map[string]bool, error) {
	if len(values) == 0 {
		return nil, nil
	}
	scope := make(map[string]bool, len(values))
	for _, discordID := range values {
		discordID = strings.TrimSpace(discordID)
		if !validateSnowflakeValue(discordID) {
			return nil, fmt.Errorf("scopeDiscordId must contain valid Discord snowflakes")
		}
		scope[discordID] = true
	}
	return scope, nil
}

func filterGuildLinksByDiscordIDs(links []store.GuildSteamDiscordLink, scope map[string]bool) []store.GuildSteamDiscordLink {
	filtered := make([]store.GuildSteamDiscordLink, 0, len(links))
	for _, link := range links {
		if scope[link.DiscordID] {
			filtered = append(filtered, link)
		}
	}
	return filtered
}

func (h *Handler) deepGameBanDiscordIDs(ctx context.Context, guildID string, links []store.GuildSteamDiscordLink, scope map[string]bool) ([]string, error) {
	if len(scope) > 0 {
		return mapKeys(scope), nil
	}

	activeDiscordIDs, err := h.postgres.ListActiveUserGuildDiscordIDs(ctx, guildID)
	if err != nil {
		return nil, err
	}

	return uniqueStrings(append(activeDiscordIDs, uniqueGuildDiscordIDs(links)...)), nil
}

func guildLinksToGameBanCandidates(links []store.GuildSteamDiscordLink) []gameBanCandidate {
	candidates := make([]gameBanCandidate, 0, len(links))
	for _, link := range links {
		candidates = append(candidates, gameBanCandidate{
			GuildID:   link.GuildID,
			DiscordID: link.DiscordID,
			SteamID:   link.SteamID,
			Source:    gameBanSourceDirectGuildLink,
		})
	}
	return candidates
}

func (h *Handler) deepGameBanCandidates(ctx context.Context, guildID string, discordIDs []string) ([]gameBanCandidate, error) {
	if len(discordIDs) == 0 {
		return nil, nil
	}
	eligibleForSync, err := h.stalePlatformSyncDiscordIDs(ctx, discordIDs, time.Now().UTC())
	if err != nil {
		return nil, err
	}

	if len(eligibleForSync) <= deepGameBanPlatformSyncLimit {
		for _, discordID := range eligibleForSync {
			if _, _, err := h.syncPlatformIdentity(ctx, "discord", discordID); err != nil {
				log.Printf("[backend] deep game ban platformsync skipped for guild=%s discord=%s: %v", guildID, discordID, err)
			}
		}
	} else {
		return nil, errDeepGameBanScopeTooLarge
	}

	hardLinks, err := h.postgres.ListVisibleHardLinksByDiscordIDs(ctx, discordIDs)
	if err != nil {
		return nil, err
	}
	candidates := make([]gameBanCandidate, 0, len(hardLinks))
	for _, link := range hardLinks {
		candidates = append(candidates, gameBanCandidate{
			GuildID:   guildID,
			DiscordID: link.DiscordID,
			SteamID:   link.SteamID,
			Source:    gameBanSourceLinkedAccount,
		})
	}
	return candidates, nil
}

func (h *Handler) stalePlatformSyncDiscordIDs(ctx context.Context, discordIDs []string, now time.Time) ([]string, error) {
	stale := make([]string, 0, len(discordIDs))
	for _, discordID := range discordIDs {
		state, err := h.postgres.GetPlatformSyncState(ctx, "discord", discordID)
		if errors.Is(err, store.ErrSteamAccountNotFound) {
			stale = append(stale, discordID)
			continue
		}
		if err != nil {
			return nil, err
		}
		if now.Sub(state.LastPlatformSyncAt) >= platformSyncTTL {
			stale = append(stale, discordID)
		}
	}
	return stale, nil
}

func (h *Handler) gameBanCacheForSteamIDs(ctx context.Context, guildID string, steamIDs []string, now time.Time) (map[string]store.SteamGameBanCache, error) {
	cacheRows, err := h.postgres.ListSteamGameBanCache(ctx, steamIDs)
	if err != nil {
		log.Printf("[backend] ListSteamGameBanCache error for guild %s: %v", guildID, err)
		return nil, err
	}

	cacheBySteamID := make(map[string]store.SteamGameBanCache, len(cacheRows))
	for _, row := range cacheRows {
		cacheBySteamID[row.SteamID] = row
	}

	staleSteamIDs := make([]string, 0)
	for _, steamID := range steamIDs {
		cache, ok := cacheBySteamID[steamID]
		if !ok || cache.GameBanCheckedAt == nil || now.Sub(*cache.GameBanCheckedAt) >= steamGameBanCacheTTL {
			staleSteamIDs = append(staleSteamIDs, steamID)
		}
	}

	if len(staleSteamIDs) > 0 {
		refreshed, refreshErr := h.refreshSteamGameBanCache(ctx, staleSteamIDs, now)
		if refreshErr != nil {
			log.Printf("[backend] refreshSteamGameBanCache error for guild %s: %v", guildID, refreshErr)
			return nil, refreshErr
		}
		for steamID, cache := range refreshed {
			cacheBySteamID[steamID] = cache
		}
	}

	return cacheBySteamID, nil
}

func recentGameBansFromCandidates(candidates []gameBanCandidate, cacheBySteamID map[string]store.SteamGameBanCache, now time.Time, timeframe gameBanTimeframe) []store.GuildRecentGameBan {
	recent := make([]store.GuildRecentGameBan, 0)
	for _, candidate := range candidates {
		cache, ok := cacheBySteamID[candidate.SteamID]
		if !ok || !cache.GameBanned || cache.GameBannedAt == nil {
			continue
		}
		if timeframe.Window > 0 && now.Sub(*cache.GameBannedAt) > timeframe.Window {
			continue
		}
		recent = append(recent, store.GuildRecentGameBan{
			GuildID:      candidate.GuildID,
			DiscordID:    candidate.DiscordID,
			SteamID:      candidate.SteamID,
			GameBannedAt: *cache.GameBannedAt,
			Source:       candidate.Source,
		})
	}
	return recent
}

func uniqueGuildDiscordIDs(links []store.GuildSteamDiscordLink) []string {
	seen := make(map[string]struct{}, len(links))
	discordIDs := make([]string, 0, len(links))
	for _, link := range links {
		if _, ok := seen[link.DiscordID]; ok {
			continue
		}
		seen[link.DiscordID] = struct{}{}
		discordIDs = append(discordIDs, link.DiscordID)
	}
	sort.Strings(discordIDs)
	return discordIDs
}

func mapKeys(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	unique := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		unique = append(unique, value)
	}
	sort.Strings(unique)
	return unique
}

func dedupeGameBanCandidates(candidates []gameBanCandidate) []gameBanCandidate {
	seen := make(map[string]struct{}, len(candidates))
	deduped := make([]gameBanCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		key := candidate.GuildID + ":" + candidate.DiscordID + ":" + candidate.SteamID
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		deduped = append(deduped, candidate)
	}
	return deduped
}

func uniqueGameBanCandidateSteamIDs(candidates []gameBanCandidate) []string {
	seen := make(map[string]struct{}, len(candidates))
	steamIDs := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if _, ok := seen[candidate.SteamID]; ok {
			continue
		}
		seen[candidate.SteamID] = struct{}{}
		steamIDs = append(steamIDs, candidate.SteamID)
	}
	sort.Strings(steamIDs)
	return steamIDs
}

func (h *Handler) refreshSteamGameBanCache(
	ctx context.Context,
	steamIDs []string,
	now time.Time,
) (map[string]store.SteamGameBanCache, error) {
	refreshed := make(map[string]store.SteamGameBanCache, len(steamIDs))
	for start := 0; start < len(steamIDs); start += steamPlayerBanBatchSize {
		end := start + steamPlayerBanBatchSize
		if end > len(steamIDs) {
			end = len(steamIDs)
		}

		batch := steamIDs[start:end]
		bans, err := h.steamClient.GetPlayerBans(ctx, batch)
		if err != nil {
			return nil, err
		}

		banBySteamID := make(map[string]steamapi.PlayerBan, len(bans))
		for _, ban := range bans {
			banBySteamID[ban.SteamID] = ban
		}

		for _, steamID := range batch {
			ban, ok := banBySteamID[steamID]
			gameBannedAt := (*time.Time)(nil)
			gameBanned := ok && ban.NumberOfGameBans > 0
			if gameBanned {
				banTime := now.Add(-time.Duration(ban.DaysSinceLastBan) * 24 * time.Hour)
				gameBannedAt = &banTime
			}

			if err := h.postgres.UpsertSteamAccountGameBanCache(ctx, steamID, gameBanned, gameBannedAt, now); err != nil {
				return nil, err
			}

			refreshed[steamID] = store.SteamGameBanCache{
				SteamID:          steamID,
				GameBanned:       gameBanned,
				GameBannedAt:     gameBannedAt,
				GameBanCheckedAt: &now,
			}
		}
	}

	return refreshed, nil
}

type trackedServerLogEventResponse struct {
	ServerBmID   string `json:"serverBmId"`
	ServerName   string `json:"serverName"`
	PlayerBmID   string `json:"playerBmId"`
	SteamID      string `json:"steamId"`
	DiscordID    string `json:"discordId"`
	BmName       string `json:"bmName"`
	AvatarImage  string `json:"avatarImage"`
	IsOnline     bool   `json:"isOnline"`
	OccurredUnix int64  `json:"occurredUnix"`
}

type trackedServerLogEventsResponse struct {
	Items []trackedServerLogEventResponse `json:"items"`
}

func (h *Handler) ListGuildTrackedServerLogEvents(w http.ResponseWriter, r *http.Request) {
	guildID, ok := validateSnowflakePathParam(w, r, "guildID")
	if !ok {
		return
	}

	states, err := h.postgres.ListGuildTrackedServerLogStates(r.Context(), guildID)
	if err != nil {
		log.Printf("[backend] ListGuildTrackedServerLogStates error for %s: %v", guildID, err)
		writeError(w, http.StatusInternalServerError, "failed to fetch tracked server log states")
		return
	}

	response := trackedServerLogEventsResponse{
		Items: make([]trackedServerLogEventResponse, 0, len(states)),
	}
	for _, state := range states {
		response.Items = append(response.Items, trackedServerLogEventResponse{
			ServerBmID:   state.ServerBmID,
			ServerName:   state.ServerName,
			PlayerBmID:   state.PlayerBmID,
			SteamID:      state.SteamID,
			DiscordID:    state.DiscordID,
			BmName:       state.BmName,
			AvatarImage:  state.AvatarImage,
			IsOnline:     state.IsOnline,
			OccurredUnix: state.StateChangedAt.Unix(),
		})
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) ListGuildLinksByDiscord(w http.ResponseWriter, r *http.Request) {
	guildID, ok := validateSnowflakePathParam(w, r, "guildID")
	if !ok {
		return
	}

	discordID := chi.URLParam(r, "discordID")
	if discordID == "" || !snowflakePattern.MatchString(discordID) {
		writeError(w, http.StatusBadRequest, "discordID must be a valid Discord snowflake")
		return
	}

	links, err := h.postgres.ListGuildLinksByGuildAndDiscord(r.Context(), guildID, discordID)
	if err != nil {
		log.Printf("[backend] ListGuildLinksByDiscord error for %s/%s: %v", guildID, discordID, err)
		writeError(w, http.StatusInternalServerError, "failed to fetch guild links")
		return
	}

	writeGuildSteamDiscordLinkList(w, links)
}

func (h *Handler) ListUserGuildLinksByDiscord(w http.ResponseWriter, r *http.Request) {
	discordID, ok := validateSnowflakePathParam(w, r, "discordID")
	if !ok {
		return
	}

	links, err := h.postgres.ListUserGuildLinksByDiscordID(r.Context(), discordID)
	if err != nil {
		log.Printf("[backend] ListUserGuildLinksByDiscord error for %s: %v", discordID, err)
		writeError(w, http.StatusInternalServerError, "failed to fetch user guild links")
		return
	}

	writeUserGuildLinkList(w, links)
}

func (h *Handler) ListGuildLinksBySteam(w http.ResponseWriter, r *http.Request) {
	guildID, ok := validateSnowflakePathParam(w, r, "guildID")
	if !ok {
		return
	}

	steamID := chi.URLParam(r, "steamID")
	if steamID == "" || !steamIDPattern.MatchString(steamID) {
		writeError(w, http.StatusBadRequest, "steamID must be a 17-digit number")
		return
	}

	links, err := h.postgres.ListGuildLinksByGuildAndSteam(r.Context(), guildID, steamID)
	if err != nil {
		log.Printf("[backend] ListGuildLinksBySteam error for %s/%s: %v", guildID, steamID, err)
		writeError(w, http.StatusInternalServerError, "failed to fetch guild links")
		return
	}

	writeGuildSteamDiscordLinkList(w, links)
}

func (h *Handler) DeleteGuildSteamDiscordLink(w http.ResponseWriter, r *http.Request) {
	guildID, ok := validateSnowflakePathParam(w, r, "guildID")
	if !ok {
		return
	}

	discordID := chi.URLParam(r, "discordID")
	if discordID == "" || !snowflakePattern.MatchString(discordID) {
		writeError(w, http.StatusBadRequest, "discordID must be a valid Discord snowflake")
		return
	}

	steamID := chi.URLParam(r, "steamID")
	if steamID == "" || !steamIDPattern.MatchString(steamID) {
		writeError(w, http.StatusBadRequest, "steamID must be a 17-digit number")
		return
	}

	err := h.postgres.DeleteGuildSteamDiscordLink(r.Context(), guildID, discordID, steamID)
	if errors.Is(err, store.ErrGuildLinkNotFound) {
		writeError(w, http.StatusNotFound, "guild link not found")
		return
	}
	if err != nil {
		log.Printf("[backend] DeleteGuildSteamDiscordLink error for %s/%s/%s: %v", guildID, discordID, steamID, err)
		writeError(w, http.StatusInternalServerError, "failed to delete guild link")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// adminRoleItem is the JSON shape for a single role in the ListAdminRoles response.
type adminRoleItem struct {
	ID          string   `json:"id"`
	Key         string   `json:"key"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	IsSystem    bool     `json:"isSystem"`
	Permissions []string `json:"permissions"`
	MemberCount int      `json:"memberCount"`
}

// ListAdminRoles handles GET /api/app/admin/roles.
// For the superadmin role the full permission list is returned in-process
// (admin_role_permissions has no explicit rows for superadmin). For all other
// roles the permissions are queried from admin_role_permissions.
func (h *Handler) ListAdminRoles(w http.ResponseWriter, r *http.Request) {
	roles, err := h.rbacStore.ListRolesWithMemberCount(r.Context())
	if err != nil {
		log.Printf("[backend] ListAdminRoles: ListRolesWithMemberCount error: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to list roles")
		return
	}

	items := make([]adminRoleItem, 0, len(roles))
	for _, rl := range roles {
		var perms []string
		if rl.Role.Key == rbac.RoleSuperadmin {
			all := rbac.AllPermissions()
			perms = make([]string, len(all))
			for i, p := range all {
				perms[i] = p.Key
			}
		} else {
			perms, err = h.rbacStore.PermissionsForRole(r.Context(), rl.Role.ID)
			if err != nil {
				log.Printf("[backend] ListAdminRoles: PermissionsForRole(%s) error: %v", rl.Role.Key, err)
				writeError(w, http.StatusInternalServerError, "failed to load role permissions")
				return
			}
		}
		if perms == nil {
			perms = []string{}
		}
		items = append(items, adminRoleItem{
			ID:          rl.Role.ID.String(),
			Key:         rl.Role.Key,
			Name:        rl.Role.Name,
			Description: rl.Role.Description,
			IsSystem:    rl.Role.IsSystem,
			Permissions: perms,
			MemberCount: rl.MemberCount,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// adminPermissionItem is the JSON shape for a single permission in the
// ListAdminPermissions response.
type adminPermissionItem struct {
	Key         string `json:"key"`
	Description string `json:"description"`
}

// ListAdminPermissions handles GET /api/app/admin/permissions. Returns the
// canonical permission catalog from rbac.AllPermissions(). Static; no DB read.
func (h *Handler) ListAdminPermissions(w http.ResponseWriter, r *http.Request) {
	perms := rbac.AllPermissions()
	items := make([]adminPermissionItem, len(perms))
	for i, p := range perms {
		items[i] = adminPermissionItem{Key: p.Key, Description: p.Description}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

var banCategoryAllowlist = map[string]struct{}{
	"setup":        {},
	"linking":      {},
	"lookup":       {},
	"activity":     {},
	"coordination": {},
	"other":        {},
}

func validateDiscordIDPathParam(w http.ResponseWriter, r *http.Request) (string, bool) {
	return validateSnowflakePathParam(w, r, "discordID")
}

func writeUserBan(w http.ResponseWriter, ban store.UserBan) {
	categories := ban.Categories
	if categories == nil {
		categories = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"discordId":  ban.DiscordID,
		"categories": categories,
		"suspended":  ban.Suspended,
	})
}

func (h *Handler) GetUserBan(w http.ResponseWriter, r *http.Request) {
	discordID, ok := validateDiscordIDPathParam(w, r)
	if !ok {
		return
	}

	ban, err := h.postgres.GetUserBan(r.Context(), discordID)
	if err != nil {
		log.Printf("[backend] GetUserBan error for %s: %v", discordID, err)
		writeError(w, http.StatusInternalServerError, "failed to fetch user ban")
		return
	}

	writeUserBan(w, ban)
}

type updateUserBanRequest struct {
	Categories []string `json:"categories"`
	UpdatedBy  string   `json:"updatedBy"`
}

func (h *Handler) UpdateUserBan(w http.ResponseWriter, r *http.Request) {
	discordID, ok := validateDiscordIDPathParam(w, r)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var request updateUserBanRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	seen := map[string]struct{}{}
	categories := make([]string, 0, len(request.Categories))
	for _, raw := range request.Categories {
		category := strings.ToLower(strings.TrimSpace(raw))
		if category == "" {
			continue
		}
		if _, ok := banCategoryAllowlist[category]; !ok {
			writeError(w, http.StatusBadRequest, "invalid ban category: "+category)
			return
		}
		if _, dup := seen[category]; dup {
			continue
		}
		seen[category] = struct{}{}
		categories = append(categories, category)
	}

	ban, err := h.postgres.UpsertUserBan(r.Context(), discordID, categories, request.UpdatedBy)
	if err != nil {
		log.Printf("[backend] UpdateUserBan error for %s: %v", discordID, err)
		writeError(w, http.StatusInternalServerError, "failed to update user ban")
		return
	}

	writeUserBan(w, ban)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
