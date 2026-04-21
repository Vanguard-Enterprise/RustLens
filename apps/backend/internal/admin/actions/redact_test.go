package actions

import (
	"encoding/json"
	"testing"
)

func TestRedact_MasksListedKeys(t *testing.T) {
	in := json.RawMessage(`{"user":"alice","password":"hunter2","nested":{"token":"abc","ok":"keep"}}`)
	out, err := RedactParams(in, []string{"password", "token"})
	if err != nil {
		t.Fatalf("redact: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["password"] != "***" {
		t.Errorf("password not redacted: %v", got["password"])
	}
	if got["user"] != "alice" {
		t.Errorf("user was modified: %v", got["user"])
	}
	nested := got["nested"].(map[string]any)
	if nested["token"] != "***" {
		t.Errorf("nested token not redacted")
	}
	if nested["ok"] != "keep" {
		t.Errorf("unrelated key altered")
	}
}

func TestRedact_NilInputReturnsEmptyObject(t *testing.T) {
	out, err := RedactParams(nil, []string{"x"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if string(out) != "{}" {
		t.Errorf("got %q want {}", string(out))
	}
}
