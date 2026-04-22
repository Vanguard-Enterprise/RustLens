package actions

import (
	"context"
	"encoding/json"
)

type ValidatedParams struct {
	ResourceID           string
	TargetUserID         *string
	ConfirmationExpected string // required match when action is Dangerous
}

type ActionInput struct {
	ActorUserID       string
	ActorRoleSnapshot []string
	Params            json.RawMessage
	Validated         ValidatedParams
	Reason            string
	TypedConfirmation string
}

type ActionResult struct {
	ResourceID   string
	TargetUserID *string
	Payload      any
}

type AdminAction interface {
	Type() string
	Permission() string
	RequiresReason() bool
	Dangerous() bool
	SensitiveParamKeys() []string
	ResourceType() string
	Validate(params json.RawMessage) (ValidatedParams, error)
	Execute(ctx context.Context, in ActionInput) (ActionResult, error)
}
