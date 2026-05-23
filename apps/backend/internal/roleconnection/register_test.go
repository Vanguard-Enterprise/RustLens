package roleconnection

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"rustlens/backend/internal/oauth/discord"
)

func TestRegisterSchema_PutsExpectedRecords(t *testing.T) {
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/applications/APP/role-connections/metadata" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bot tok" {
			t.Errorf("auth = %q", got)
		}
		body, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()
	c := discord.NewClient("cid", "csec", discord.WithBaseURL(srv.URL))
	if err := RegisterSchema(context.Background(), c, "APP", "tok"); err != nil {
		t.Fatalf("RegisterSchema: %v", err)
	}
	if !strings.Contains(string(body), `"key":"is_pro"`) {
		t.Errorf("body missing is_pro: %s", string(body))
	}
	if !strings.Contains(string(body), `"key":"pro_since"`) {
		t.Errorf("body missing pro_since: %s", string(body))
	}
}
