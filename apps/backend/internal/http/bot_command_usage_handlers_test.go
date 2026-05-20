package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"rustlens/backend/internal/appauth"
	"rustlens/backend/internal/store"
)

type commandUsageTestFixture struct {
	h    *Handler
	pool *pgxpool.Pool
}

// newCommandUsageTestFixture connects to Postgres, ensures the schema, truncates
// command_usage and app_users, and returns a Handler wired with a real store.
func newCommandUsageTestFixture(t *testing.T) commandUsageTestFixture {
	t.Helper()
	dsn := os.Getenv("TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_URL not set")
	}
	ctx := context.Background()
	s, err := store.NewPostgresStore(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(s.Close)
	if err := s.EnsureSchema(ctx); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	if _, err := s.Pool().Exec(ctx, `TRUNCATE command_usage, app_users RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	h := &Handler{
		postgres: s,
		appauth:  appauth.NewRepo(s.Pool()),
	}
	return commandUsageTestFixture{h: h, pool: s.Pool()}
}

func TestRecordCommandUsage_HappyPath(t *testing.T) {
	ctx := context.Background()
	fx := newCommandUsageTestFixture(t)

	if _, err := fx.pool.Exec(ctx, `
		INSERT INTO app_users (id, discord_id, discord_username, display_name)
		VALUES ('usr_h1', '111222333444555666', 'h1', 'h1')
	`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	body := map[string]any{
		"discordId":       "111222333444555666",
		"guildId":         "222333444555666777",
		"channelId":       "333444555666777888",
		"interactionType": "command",
		"command":         "track",
		"subcommand":      "create",
		"target":          "raid",
		"options":         map[string]any{"group_name": "raid"},
		"outcome":         "success",
		"latencyMs":       142,
	}
	b, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(b))
	rr := httptest.NewRecorder()
	fx.h.RecordCommandUsage(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204 got %d body=%s", rr.Code, rr.Body.String())
	}

	var count int
	if err := fx.pool.QueryRow(ctx, `SELECT COUNT(*) FROM command_usage WHERE app_user_id='usr_h1'`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 row got %d", count)
	}
}

func TestRecordCommandUsage_MissingAppUserIsSilentSuccess(t *testing.T) {
	ctx := context.Background()
	fx := newCommandUsageTestFixture(t)

	b, _ := json.Marshal(map[string]any{
		"discordId":       "999888777666555444",
		"interactionType": "command",
		"command":         "player",
		"options":         map[string]any{},
		"outcome":         "success",
		"latencyMs":       1,
	})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(b))
	rr := httptest.NewRecorder()
	fx.h.RecordCommandUsage(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204 (silent drop) got %d body=%s", rr.Code, rr.Body.String())
	}

	var count int
	if err := fx.pool.QueryRow(ctx, `SELECT COUNT(*) FROM command_usage`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 rows (silent drop) got %d", count)
	}
}

func TestRecordCommandUsage_RejectsInvalidSnowflake(t *testing.T) {
	fx := newCommandUsageTestFixture(t)

	b, _ := json.Marshal(map[string]any{
		"discordId":       "not-a-snowflake",
		"interactionType": "command",
		"command":         "player",
		"outcome":         "success",
		"latencyMs":       1,
	})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(b))
	rr := httptest.NewRecorder()
	fx.h.RecordCommandUsage(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "snowflake") {
		t.Fatalf("expected snowflake error, got %s", rr.Body.String())
	}
}

func TestRecordCommandUsage_RejectsBadEnum(t *testing.T) {
	fx := newCommandUsageTestFixture(t)

	b, _ := json.Marshal(map[string]any{
		"discordId":       "111222333444555666",
		"interactionType": "unknown_type",
		"command":         "player",
		"outcome":         "success",
		"latencyMs":       1,
	})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(b))
	rr := httptest.NewRecorder()
	fx.h.RecordCommandUsage(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestRecordCommandUsage_PersistsTargets(t *testing.T) {
	ctx := context.Background()
	fx := newCommandUsageTestFixture(t)

	if _, err := fx.pool.Exec(ctx, `
		INSERT INTO app_users (id, discord_id, discord_username, display_name)
		VALUES ('usr_pt1', '222222222222222222', 'pt1', 'pt1')
	`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	body := `{
		"discordId":"222222222222222222",
		"guildId":"111111111111111111",
		"interactionType":"command",
		"command":"announce",
		"outcome":"success",
		"latencyMs":42,
		"options":{},
		"targets":[
			{"kind":"role","snowflake":"333333333333333333","relation":"scope"},
			{"kind":"user","snowflake":"444444444444444444","displayName":"bob","relation":"recipient"}
		]
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/internal/bot/setup/command-usage", strings.NewReader(body))
	rr := httptest.NewRecorder()
	fx.h.RecordCommandUsage(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("want 204, got %d body=%s", rr.Code, rr.Body.String())
	}

	var count int
	if err := fx.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM command_usage_targets cut
		JOIN command_usage cu ON cu.id = cut.command_usage_id
		WHERE cu.app_user_id = 'usr_pt1'
	`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 2 {
		t.Fatalf("want 2 target rows, got %d", count)
	}
}

func TestRecordCommandUsage_DropsInvalidTargetsSilently(t *testing.T) {
	ctx := context.Background()
	fx := newCommandUsageTestFixture(t)

	if _, err := fx.pool.Exec(ctx, `
		INSERT INTO app_users (id, discord_id, discord_username, display_name)
		VALUES ('usr_dt1', '222222222222222222', 'dt1', 'dt1')
	`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	body := `{
		"discordId":"222222222222222222",
		"interactionType":"command",
		"command":"announce",
		"outcome":"success",
		"latencyMs":1,
		"options":{},
		"targets":[
			{"kind":"banana","snowflake":"333333333333333333","relation":"scope"},
			{"kind":"role","snowflake":"not-a-snowflake","relation":"scope"},
			{"kind":"user","snowflake":"444444444444444444","relation":"recipient"}
		]
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/internal/bot/setup/command-usage", strings.NewReader(body))
	rr := httptest.NewRecorder()
	fx.h.RecordCommandUsage(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("want 204, got %d", rr.Code)
	}

	var count int
	if err := fx.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM command_usage_targets cut
		JOIN command_usage cu ON cu.id = cut.command_usage_id
		WHERE cu.app_user_id = 'usr_dt1'
	`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("want 1 valid target row, got %d", count)
	}
}
