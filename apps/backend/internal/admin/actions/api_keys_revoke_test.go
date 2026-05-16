package actions

import (
	"context"
	"encoding/json"
	"testing"

	"rustlens/backend/internal/store"
)

func TestAPIKeysRevokeHappyAndIdempotent(t *testing.T) {
	pool := newTestPool(t)
	defer pool.Close()
	userID := seedTestUser(t, pool)
	created, _ := NewAPIKeysCreateAction(pool, &stubPermLoader{perms: []string{"users.read"}}).
		Execute(context.Background(), ActionInput{
			ActorUserID: userID,
			Params:      json.RawMessage(`{"name":"k","permissions":["users.read"]}`),
			Reason:      "x",
		})
	keyID := created.Payload.(map[string]any)["apiKey"].(map[string]any)["id"].(string)

	a := NewAPIKeysRevokeAction(pool)
	if _, err := a.Execute(context.Background(), ActionInput{
		ActorUserID: userID,
		Params:      json.RawMessage(`{"keyId":"` + keyID + `"}`),
		Reason:      "x",
	}); err != nil {
		t.Fatalf("first revoke: %v", err)
	}

	got, _ := store.NewAPIKeyStore(pool).GetByID(context.Background(), keyID)
	if got.RevokedAt == nil || got.RevokedReason == nil || *got.RevokedReason != "manual" {
		t.Fatalf("expected manual revoke, got %+v", got)
	}

	if _, err := a.Execute(context.Background(), ActionInput{
		ActorUserID: userID,
		Params:      json.RawMessage(`{"keyId":"` + keyID + `"}`),
		Reason:      "x",
	}); err != nil {
		t.Fatalf("second revoke (idempotent): %v", err)
	}
}
