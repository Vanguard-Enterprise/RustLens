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
		"guildId":         "g1",
		"channelId":       "c1",
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
