package bmsearch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"rustlens/backend/internal/steammatch"
)

// fakeAPI is a configurable stand-in for the bmsearch PostgREST API.
type fakeAPI struct {
	profilesBySteam map[string]string // steam_id -> JSON array body
	profilesByBM    map[string]string // battlemetrics_id -> JSON array body
	profilesByID    map[string]string // id -> JSON array body
	lookupBody      string            // POST /rpc/lookup response
	statusBodies    []string          // GET /rpc/lookup_status responses, consumed in order
	statusCalls     int32
}

func (f *fakeAPI) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/profiles", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		write := func(body string) {
			if body == "" {
				body = "[]"
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		}
		if v := q.Get("steam_id"); v != "" {
			write(f.profilesBySteam[strings.TrimPrefix(v, "eq.")])
			return
		}
		if v := q.Get("battlemetrics_id"); v != "" {
			write(f.profilesByBM[strings.TrimPrefix(v, "eq.")])
			return
		}
		if v := q.Get("id"); v != "" {
			write(f.profilesByID[strings.TrimPrefix(v, "eq.")])
			return
		}
		write("[]")
	})
	mux.HandleFunc("/rpc/lookup", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(f.lookupBody))
	})
	mux.HandleFunc("/rpc/lookup_status", func(w http.ResponseWriter, r *http.Request) {
		i := atomic.AddInt32(&f.statusCalls, 1) - 1
		body := `{"status":"pending","profile_id":null,"error":null}`
		if int(i) < len(f.statusBodies) {
			body = f.statusBodies[i]
		} else if len(f.statusBodies) > 0 {
			body = f.statusBodies[len(f.statusBodies)-1]
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	})
	return mux
}

func newTestClient(t *testing.T, api *fakeAPI) *Client {
	t.Helper()
	srv := httptest.NewServer(api.handler())
	t.Cleanup(srv.Close)
	return NewClient(srv.URL, 2*time.Second)
}

func TestSteamToBM_CacheHit(t *testing.T) {
	api := &fakeAPI{
		// large bigint to exercise numeric -> string conversion
		profilesBySteam: map[string]string{"76561198089561331": `[{"battlemetrics_id":30952783}]`},
	}
	c := newTestClient(t, api)
	got, err := c.SteamToBM(context.Background(), "76561198089561331")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "30952783" {
		t.Fatalf("want 30952783, got %q", got)
	}
	if api.statusCalls != 0 {
		t.Fatalf("cache hit should not poll, got %d status calls", api.statusCalls)
	}
}

func TestSteamToBM_EnqueuePollDone(t *testing.T) {
	api := &fakeAPI{
		profilesBySteam: map[string]string{"76561190000000000": `[]`}, // miss
		lookupBody:      `{"queue_id":7,"profile_id":null}`,
		statusBodies: []string{
			`{"status":"pending","profile_id":null,"error":null}`,
			`{"status":"done","profile_id":99,"error":null}`,
		},
		profilesByID: map[string]string{"99": `[{"battlemetrics_id":555}]`},
	}
	c := newTestClient(t, api)
	got, err := c.SteamToBM(context.Background(), "76561190000000000")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "555" {
		t.Fatalf("want 555, got %q", got)
	}
	if api.statusCalls < 2 {
		t.Fatalf("expected at least 2 status polls, got %d", api.statusCalls)
	}
}

func TestSteamToBM_ProfileIDFromEnqueue(t *testing.T) {
	api := &fakeAPI{
		profilesBySteam: map[string]string{},
		lookupBody:      `{"queue_id":7,"profile_id":42}`,
		profilesByID:    map[string]string{"42": `[{"battlemetrics_id":808}]`},
	}
	c := newTestClient(t, api)
	got, err := c.SteamToBM(context.Background(), "76561190000000001")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "808" {
		t.Fatalf("want 808, got %q", got)
	}
	if api.statusCalls != 0 {
		t.Fatalf("profile_id from enqueue should not poll, got %d", api.statusCalls)
	}
}

func TestSteamToBM_Pending(t *testing.T) {
	api := &fakeAPI{
		lookupBody:   `{"queue_id":8,"profile_id":null}`,
		statusBodies: []string{`{"status":"pending","profile_id":null,"error":null}`},
	}
	c := newTestClient(t, api)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err := c.SteamToBM(ctx, "76561190000000002")
	if !errors.Is(err, steammatch.ErrPending) {
		t.Fatalf("want ErrPending, got %v", err)
	}
}

func TestSteamToBM_LookupErrorState(t *testing.T) {
	api := &fakeAPI{
		lookupBody:   `{"queue_id":9,"profile_id":null}`,
		statusBodies: []string{`{"status":"error","profile_id":null,"error":"profile private"}`},
	}
	c := newTestClient(t, api)
	_, err := c.SteamToBM(context.Background(), "76561190000000003")
	if !errors.Is(err, steammatch.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestSteamToBM_NullBmIDTreatedAsMiss(t *testing.T) {
	api := &fakeAPI{
		// private profile: row exists but battlemetrics_id is null -> enqueue,
		// which here also yields null -> ErrNotFound.
		profilesBySteam: map[string]string{"76561190000000004": `[{"battlemetrics_id":null}]`},
		lookupBody:      `{"queue_id":10,"profile_id":null}`,
		statusBodies:    []string{`{"status":"done","profile_id":null,"error":null}`},
	}
	c := newTestClient(t, api)
	_, err := c.SteamToBM(context.Background(), "76561190000000004")
	if !errors.Is(err, steammatch.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestBMToSteam_CacheHitAndMiss(t *testing.T) {
	api := &fakeAPI{
		profilesByBM: map[string]string{"30952783": `[{"steam_id":"76561198089561331"}]`},
	}
	c := newTestClient(t, api)

	got, err := c.BMToSteam(context.Background(), "30952783")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "76561198089561331" {
		t.Fatalf("want steam id, got %q", got)
	}

	_, err = c.BMToSteam(context.Background(), "404404")
	if !errors.Is(err, steammatch.ErrNotFound) {
		t.Fatalf("want ErrNotFound for uncached BM id, got %v", err)
	}
}
