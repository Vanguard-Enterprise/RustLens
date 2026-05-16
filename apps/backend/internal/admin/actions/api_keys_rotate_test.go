package actions

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestAPIKeysRotateValidate(t *testing.T) {
	a := NewAPIKeysRotateAction(nil)
	if _, err := a.Validate(json.RawMessage(`{}`)); err == nil {
		t.Error("expected error when keyId missing")
	}
}

func TestAPIKeysRotateHappyPath(t *testing.T) {
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
	oldSecret := created.Payload.(map[string]any)["secret"].(string)

	a := NewAPIKeysRotateAction(pool)
	rotated, err := a.Execute(context.Background(), ActionInput{
		ActorUserID: userID,
		Params:      json.RawMessage(`{"keyId":"` + keyID + `"}`),
		Reason:      "x",
	})
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	newSecret := rotated.Payload.(map[string]any)["secret"].(string)
	if !strings.HasPrefix(newSecret, "rl_admin_") {
		t.Errorf("bad new secret: %q", newSecret)
	}
	if newSecret == oldSecret {
		t.Error("new secret equals old")
	}
}
