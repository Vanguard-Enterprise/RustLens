package actions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"rustlens/backend/internal/admin/rbac"
	"rustlens/backend/internal/store"
)

type APIKeysRevokeParams struct {
	KeyID string `json:"keyId"`
}

type apiKeysRevokeAction struct{ pool *pgxpool.Pool }

func NewAPIKeysRevokeAction(pool *pgxpool.Pool) AdminAction {
	return &apiKeysRevokeAction{pool: pool}
}

func (a *apiKeysRevokeAction) Type() string                 { return "api_keys.revoke" }
func (a *apiKeysRevokeAction) Permission() string           { return rbac.PermAPIKeysManage }
func (a *apiKeysRevokeAction) RequiresReason() bool         { return true }
func (a *apiKeysRevokeAction) Dangerous() bool              { return false }
func (a *apiKeysRevokeAction) SensitiveParamKeys() []string { return nil }
func (a *apiKeysRevokeAction) ResourceType() string         { return "api_key" }

func (a *apiKeysRevokeAction) Validate(raw json.RawMessage) (ValidatedParams, error) {
	var p APIKeysRevokeParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return ValidatedParams{}, fmt.Errorf("validation: %w", err)
	}
	if p.KeyID == "" {
		return ValidatedParams{}, errors.New("keyId is required")
	}
	return ValidatedParams{ResourceID: p.KeyID}, nil
}

func (a *apiKeysRevokeAction) Execute(ctx context.Context, in ActionInput) (ActionResult, error) {
	var p APIKeysRevokeParams
	if err := json.Unmarshal(in.Params, &p); err != nil {
		return ActionResult{}, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	keys := store.NewAPIKeyStore(a.pool)
	if err := keys.Revoke(ctx, p.KeyID, "manual"); err != nil {
		return ActionResult{}, err
	}
	return ActionResult{
		ResourceID: p.KeyID,
		Payload:    map[string]any{"keyId": p.KeyID},
	}, nil
}
