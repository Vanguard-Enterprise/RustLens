package actions_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/go-chi/chi/v5"

	"rustlens/backend/internal/admin/actions"
)

// countingAction overrides its rate-limit so tests don't have to consume 30
// tokens to hit the limiter.
type countingAction struct {
	typ      string
	perm     string
	capacity int
	refill   float64
	calls    atomic.Int32
}

func (a *countingAction) Type() string                 { return a.typ }
func (a *countingAction) Permission() string           { return a.perm }
func (a *countingAction) RequiresReason() bool         { return false }
func (a *countingAction) Dangerous() bool              { return false }
func (a *countingAction) SensitiveParamKeys() []string { return nil }
func (a *countingAction) ResourceType() string         { return "user" }
func (a *countingAction) RateLimit() (int, float64)    { return a.capacity, a.refill }
func (a *countingAction) Validate(raw json.RawMessage) (actions.ValidatedParams, error) {
	return actions.ValidatedParams{ResourceID: "r"}, nil
}
func (a *countingAction) Execute(ctx context.Context, in actions.ActionInput) (actions.ActionResult, error) {
	a.calls.Add(1)
	return actions.ActionResult{ResourceID: "r", Payload: map[string]any{"n": a.calls.Load()}}, nil
}

func newRateLimitSrv(t *testing.T, a actions.AdminAction) *httptest.Server {
	t.Helper()
	reg := actions.NewRegistry(func(string) bool { return true })
	if err := reg.Register(a); err != nil {
		t.Fatalf("register: %v", err)
	}
	au := &fakeAudit{}
	perms := &fakePermResolver{allow: map[string]bool{a.Permission(): true}}
	d := actions.NewDispatcher(reg, perms, au, fixedActor)
	r := chi.NewRouter()
	r.Post("/api/app/admin/actions/{type}", d.ServeHTTP)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}

func TestDispatcher_RateLimit_Exceeded(t *testing.T) {
	a := &countingAction{typ: "test.rl", perm: "users.edit", capacity: 3, refill: 0}
	srv := newRateLimitSrv(t, a)

	body := []byte(`{"params":{}}`)
	url := srv.URL + "/api/app/admin/actions/test.rl"
	var last *http.Response
	for i := 0; i < 4; i++ {
		r, err := http.Post(url, "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("post %d: %v", i, err)
		}
		last = r
	}
	if last.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429 on 4th call, got %d", last.StatusCode)
	}
	if last.Header.Get("Retry-After") == "" {
		t.Errorf("expected Retry-After header")
	}
	if a.calls.Load() != 3 {
		t.Errorf("expected 3 executions before 429, got %d", a.calls.Load())
	}
}

func TestDispatcher_IdempotencyCache_ReplaysWithoutReExecute(t *testing.T) {
	a := &countingAction{typ: "test.idem", perm: "users.edit", capacity: 100, refill: 10}
	srv := newRateLimitSrv(t, a)

	url := srv.URL + "/api/app/admin/actions/test.idem"
	doReq := func(key string) *http.Response {
		req, _ := http.NewRequest("POST", url, strings.NewReader(`{"params":{}}`))
		req.Header.Set("Content-Type", "application/json")
		if key != "" {
			req.Header.Set("Idempotency-Key", key)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("post: %v", err)
		}
		return resp
	}

	r1 := doReq("abc")
	b1 := readBody(r1)
	if r1.StatusCode != 200 {
		t.Fatalf("first call status %d: %s", r1.StatusCode, b1)
	}
	if a.calls.Load() != 1 {
		t.Fatalf("expected 1 call after first req, got %d", a.calls.Load())
	}

	r2 := doReq("abc")
	b2 := readBody(r2)
	if r2.StatusCode != 200 {
		t.Fatalf("replay status %d: %s", r2.StatusCode, b2)
	}
	if a.calls.Load() != 1 {
		t.Errorf("expected still 1 call after replay, got %d", a.calls.Load())
	}
	if b1 != b2 {
		t.Errorf("replay body differs:\n%s\nvs\n%s", b1, b2)
	}
	if r2.Header.Get("Idempotent-Replay") != "true" {
		t.Errorf("expected Idempotent-Replay header on cached response")
	}

	// Different key re-executes.
	r3 := doReq("xyz")
	_ = readBody(r3)
	if a.calls.Load() != 2 {
		t.Errorf("expected 2 calls after different idempotency key, got %d", a.calls.Load())
	}

	// No key at all also re-executes.
	r4 := doReq("")
	_ = readBody(r4)
	if a.calls.Load() != 3 {
		t.Errorf("expected 3 calls after no-key request, got %d", a.calls.Load())
	}
}
