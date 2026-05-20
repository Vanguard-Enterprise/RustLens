package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"rustlens/backend/internal/store"
)

type adminCommandUsageTestFixture struct {
	h    *Handler
	pool *pgxpool.Pool
}

// newAdminCommandUsageFixture connects to Postgres, ensures the schema,
// truncates command_usage and app_users, and returns a Handler wired with a
// real store — matching the commandUsageTestFixture pattern from
// bot_command_usage_handlers_test.go.
func newAdminCommandUsageFixture(t *testing.T) adminCommandUsageTestFixture {
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
	h := &Handler{postgres: s}
	return adminCommandUsageTestFixture{h: h, pool: s.Pool()}
}

func TestAdminGetUserCommandUsage_ReturnsItemsAndSummary(t *testing.T) {
	ctx := context.Background()
	fx := newAdminCommandUsageFixture(t)

	if _, err := fx.pool.Exec(ctx, `
		INSERT INTO app_users (id, discord_id, discord_username, display_name)
		VALUES ('usr_adminread', '777', 'reader', 'reader')
	`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	for i := 0; i < 3; i++ {
		if _, err := fx.pool.Exec(ctx, `
			INSERT INTO command_usage (
				app_user_id, discord_id, interaction_type, command,
				options, outcome, latency_ms
			) VALUES ('usr_adminread', '777', 'command', 'player', '{}'::jsonb, 'success', 1)
		`); err != nil {
			t.Fatalf("seed row %d: %v", i, err)
		}
	}
	if _, err := fx.pool.Exec(ctx, `
		INSERT INTO command_usage (
			app_user_id, discord_id, interaction_type, command,
			options, outcome, error_code, latency_ms
		) VALUES ('usr_adminread', '777', 'command', 'track', '{}'::jsonb, 'error', 'boom', 1)
	`); err != nil {
		t.Fatalf("seed err row: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/users/usr_adminread/command-usage?page=1&pageSize=10", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "usr_adminread")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rr := httptest.NewRecorder()
	fx.h.AdminGetUserCommandUsage(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Items    []map[string]any `json:"items"`
		Total    int64            `json:"total"`
		Page     int              `json:"page"`
		PageSize int              `json:"pageSize"`
		Summary  struct {
			TotalCount  int64   `json:"totalCount"`
			ErrorCount  int64   `json:"errorCount"`
			ErrorRate   float64 `json:"errorRate"`
			TopCommands []struct {
				Command string `json:"command"`
				Count   int64  `json:"count"`
			} `json:"topCommands"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Total != 4 {
		t.Fatalf("expected total=4 got %d", resp.Total)
	}
	if len(resp.Items) != 4 {
		t.Fatalf("expected 4 items got %d", len(resp.Items))
	}
	if resp.Summary.TotalCount != 4 || resp.Summary.ErrorCount != 1 {
		t.Fatalf("wrong summary: %+v", resp.Summary)
	}
	wantRate := 0.25
	if resp.Summary.ErrorRate < wantRate-0.001 || resp.Summary.ErrorRate > wantRate+0.001 {
		t.Fatalf("wrong error rate: %v", resp.Summary.ErrorRate)
	}
	if len(resp.Summary.TopCommands) == 0 || resp.Summary.TopCommands[0].Command != "player" {
		t.Fatalf("wrong top commands: %+v", resp.Summary.TopCommands)
	}
}

func TestAdminGetUserCommandUsage_PaginationParams(t *testing.T) {
	ctx := context.Background()
	fx := newAdminCommandUsageFixture(t)

	if _, err := fx.pool.Exec(ctx, `
		INSERT INTO app_users (id, discord_id, discord_username, display_name)
		VALUES ('usr_pag', '888', 'pag', 'pag')
	`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	for i := 0; i < 5; i++ {
		_, _ = fx.pool.Exec(ctx, fmt.Sprintf(`
			INSERT INTO command_usage (
				app_user_id, discord_id, interaction_type, command,
				options, outcome, latency_ms
			) VALUES ('usr_pag', '888', 'command', 'p%d', '{}'::jsonb, 'success', 1)
		`, i))
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/users/usr_pag/command-usage?page=2&pageSize=2", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "usr_pag")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rr := httptest.NewRecorder()
	fx.h.AdminGetUserCommandUsage(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d body=%s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Items    []map[string]any `json:"items"`
		Total    int64            `json:"total"`
		Page     int              `json:"page"`
		PageSize int              `json:"pageSize"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.Page != 2 || resp.PageSize != 2 || len(resp.Items) != 2 || resp.Total != 5 {
		t.Fatalf("bad pagination: page=%d pageSize=%d items=%d total=%d", resp.Page, resp.PageSize, len(resp.Items), resp.Total)
	}
}
