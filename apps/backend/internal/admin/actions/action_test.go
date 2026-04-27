package actions

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

type fakeAction struct{}

func (fakeAction) Type() string                  { return "test.noop" }
func (fakeAction) Permission() string            { return "audit.read" }
func (fakeAction) RequiresReason() bool          { return false }
func (fakeAction) Dangerous() bool               { return false }
func (fakeAction) SensitiveParamKeys() []string  { return nil }
func (fakeAction) ResourceType() string          { return "user" }
func (fakeAction) Validate(json.RawMessage) (ValidatedParams, error) {
	return ValidatedParams{ResourceID: "r1"}, nil
}
func (fakeAction) Execute(context.Context, ActionInput) (ActionResult, error) {
	return ActionResult{ResourceID: "r1"}, nil
}

func TestAction_Interface(t *testing.T) {
	var a AdminAction = fakeAction{}
	if a.Type() != "test.noop" {
		t.Errorf("Type = %q", a.Type())
	}
	vp, err := a.Validate(nil)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if vp.ResourceID != "r1" {
		t.Errorf("ResourceID = %q", vp.ResourceID)
	}
	_ = uuid.New()
}
