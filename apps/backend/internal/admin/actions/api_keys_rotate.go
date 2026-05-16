package actions

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"rustlens/backend/internal/admin/rbac"
	"rustlens/backend/internal/store"
)

type APIKeysRotateParams struct {
	KeyID string `json:"keyId"`
}

type apiKeysRotateAction struct{ pool *pgxpool.Pool }

func NewAPIKeysRotateAction(pool *pgxpool.Pool) AdminAction {
	return &apiKeysRotateAction{pool: pool}
}

func (a *apiKeysRotateAction) Type() string                 { return "api_keys.rotate" }
func (a *apiKeysRotateAction) Permission() string           { return rbac.PermAPIKeysManage }
func (a *apiKeysRotateAction) RequiresReason() bool         { return true }
func (a *apiKeysRotateAction) Dangerous() bool              { return false }
func (a *apiKeysRotateAction) SensitiveParamKeys() []string { return nil }
func (a *apiKeysRotateAction) ResourceType() string         { return "api_key" }

func (a *apiKeysRotateAction) Validate(raw json.RawMessage) (ValidatedParams, error) {
	var p APIKeysRotateParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return ValidatedParams{}, fmt.Errorf("validation: %w", err)
	}
	if p.KeyID == "" {
		return ValidatedParams{}, errors.New("keyId is required")
	}
	return ValidatedParams{ResourceID: p.KeyID}, nil
}

func (a *apiKeysRotateAction) Execute(ctx context.Context, in ActionInput) (ActionResult, error) {
	var p APIKeysRotateParams
	if err := json.Unmarshal(in.Params, &p); err != nil {
		return ActionResult{}, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}

	keys := store.NewAPIKeyStore(a.pool)
	existing, err := keys.GetByID(ctx, p.KeyID)
	if err != nil {
		if errors.Is(err, store.ErrAPIKeyNotFound) {
			return ActionResult{}, fmt.Errorf("%w: api key %s", ErrNotFound, p.KeyID)
		}
		return ActionResult{}, err
	}
	if existing.RevokedAt != nil {
		return ActionResult{}, fmt.Errorf("%w: cannot rotate a revoked key", ErrConflict)
	}

	secret, prefix, last4, err := generateAPIKeySecret()
	if err != nil {
		return ActionResult{}, err
	}
	hash := sha256.Sum256([]byte(secret))
	if err := keys.Rotate(ctx, p.KeyID, prefix, last4, hash[:]); err != nil {
		return ActionResult{}, err
	}

	return ActionResult{
		ResourceID: p.KeyID,
		Payload: map[string]any{
			"apiKey": map[string]any{
				"id":     p.KeyID,
				"prefix": prefix,
				"last4":  last4,
			},
			"secret": secret,
		},
	}, nil
}
