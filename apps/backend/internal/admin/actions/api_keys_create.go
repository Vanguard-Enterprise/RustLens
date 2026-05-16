package actions

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"rustlens/backend/internal/admin/rbac"
	"rustlens/backend/internal/store"
)

const apiKeySecretBytes = 32
const apiKeyPrefixLen = 16 // "rl_admin_" (9) + 7 random chars
const apiKeyPrefixWord = "rl_admin_"

// ActorPermissionsLoader returns the actor's *current* permission set.
type ActorPermissionsLoader interface {
	PermissionsForUser(ctx context.Context, userID string) ([]string, error)
}

type APIKeysCreateParams struct {
	Name        string   `json:"name"`
	Permissions []string `json:"permissions"`
	ExpiresAt   *string  `json:"expiresAt,omitempty"` // RFC3339
}

type apiKeysCreateAction struct {
	pool  *pgxpool.Pool
	perms ActorPermissionsLoader
}

func NewAPIKeysCreateAction(pool *pgxpool.Pool, perms ActorPermissionsLoader) AdminAction {
	return &apiKeysCreateAction{pool: pool, perms: perms}
}

func (a *apiKeysCreateAction) Type() string                 { return "api_keys.create" }
func (a *apiKeysCreateAction) Permission() string           { return rbac.PermAPIKeysManage }
func (a *apiKeysCreateAction) RequiresReason() bool         { return true }
func (a *apiKeysCreateAction) Dangerous() bool              { return false }
func (a *apiKeysCreateAction) SensitiveParamKeys() []string { return nil }
func (a *apiKeysCreateAction) ResourceType() string         { return "api_key" }

func (a *apiKeysCreateAction) Validate(raw json.RawMessage) (ValidatedParams, error) {
	var p APIKeysCreateParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return ValidatedParams{}, fmt.Errorf("validation: %w", err)
	}
	if p.Name == "" {
		return ValidatedParams{}, errors.New("name is required")
	}
	if len(p.Name) > 64 {
		return ValidatedParams{}, errors.New("name must be <= 64 chars")
	}
	for _, perm := range p.Permissions {
		if !rbac.IsValidPermission(perm) {
			return ValidatedParams{}, fmt.Errorf("unknown permission %q", perm)
		}
	}
	if p.ExpiresAt != nil && *p.ExpiresAt != "" {
		if _, err := time.Parse(time.RFC3339, *p.ExpiresAt); err != nil {
			return ValidatedParams{}, fmt.Errorf("expiresAt must be RFC3339: %w", err)
		}
	}
	return ValidatedParams{}, nil
}

func (a *apiKeysCreateAction) Execute(ctx context.Context, in ActionInput) (ActionResult, error) {
	var p APIKeysCreateParams
	if err := json.Unmarshal(in.Params, &p); err != nil {
		return ActionResult{}, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}

	actorPerms, err := a.perms.PermissionsForUser(ctx, in.ActorUserID)
	if err != nil {
		return ActionResult{}, err
	}
	if err := AssertPermissionsSubsetOfActor(p.Permissions, actorPerms); err != nil {
		return ActionResult{}, err
	}

	secret, prefix, last4, err := generateAPIKeySecret()
	if err != nil {
		return ActionResult{}, err
	}
	hash := sha256.Sum256([]byte(secret))

	var expires *time.Time
	if p.ExpiresAt != nil && *p.ExpiresAt != "" {
		t, _ := time.Parse(time.RFC3339, *p.ExpiresAt)
		expires = &t
	}

	keys := store.NewAPIKeyStore(a.pool)
	key, err := keys.Create(ctx, store.APIKeyCreate{
		Name:            p.Name,
		Prefix:          prefix,
		Last4:           last4,
		SecretHash:      hash[:],
		CreatedByUserID: in.ActorUserID,
		Permissions:     p.Permissions,
		ExpiresAt:       expires,
	})
	if err != nil {
		return ActionResult{}, err
	}

	return ActionResult{
		ResourceID: key.ID,
		Payload: map[string]any{
			"apiKey": map[string]any{
				"id":          key.ID,
				"name":        key.Name,
				"prefix":      key.Prefix,
				"last4":       key.Last4,
				"permissions": key.Permissions,
				"expiresAt":   key.ExpiresAt,
				"createdAt":   key.CreatedAt,
			},
			"secret": secret,
		},
	}, nil
}

// generateAPIKeySecret returns (secret, prefix-16-chars, last4).
func generateAPIKeySecret() (string, string, string, error) {
	const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	buf := make([]byte, apiKeySecretBytes)
	for i := range buf {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			return "", "", "", err
		}
		buf[i] = alphabet[n.Int64()]
	}
	secret := apiKeyPrefixWord + string(buf)
	prefix := secret[:apiKeyPrefixLen]
	last4 := secret[len(secret)-4:]
	return secret, prefix, last4, nil
}
