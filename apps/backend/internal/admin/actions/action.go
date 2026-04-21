package actions

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
)

type ValidatedParams struct {
	ResourceID           string
	TargetUserID         *uuid.UUID
	ConfirmationExpected string // required match when action is Dangerous
}

type ActionInput struct {
	ActorUserID       uuid.UUID
	ActorRoleSnapshot []string
	Params            json.RawMessage
	Validated         ValidatedParams
	Reason            string
	TypedConfirmation string
}

type ActionResult struct {
	ResourceID   string
	TargetUserID *uuid.UUID
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
