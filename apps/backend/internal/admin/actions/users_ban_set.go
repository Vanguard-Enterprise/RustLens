package actions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"rustlens/backend/internal/admin/rbac"
)

type UsersBanSetParams struct {
	UserID     string   `json:"userId"`
	Categories []string `json:"categories"`
}

type usersBanSetAction struct{ pool *pgxpool.Pool }

func NewUsersBanSetAction(pool *pgxpool.Pool) AdminAction {
	return &usersBanSetAction{pool: pool}
}

func (a *usersBanSetAction) Type() string                 { return "users.ban.set" }
func (a *usersBanSetAction) Permission() string           { return rbac.PermUsersSuspend }
func (a *usersBanSetAction) RequiresReason() bool         { return true }
func (a *usersBanSetAction) Dangerous() bool              { return false }
func (a *usersBanSetAction) SensitiveParamKeys() []string { return nil }
func (a *usersBanSetAction) ResourceType() string         { return "user" }

var userBanCategories = map[string]struct{}{
	"setup":        {},
	"linking":      {},
	"lookup":       {},
	"activity":     {},
	"coordination": {},
	"other":        {},
}

func (a *usersBanSetAction) Validate(raw json.RawMessage) (ValidatedParams, error) {
	var p UsersBanSetParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return ValidatedParams{}, fmt.Errorf("validation: %w", err)
	}
	if p.UserID == "" {
		return ValidatedParams{}, errors.New("userId is required")
	}
	seen := make(map[string]struct{}, len(p.Categories))
	for _, category := range p.Categories {
		if _, ok := userBanCategories[category]; !ok {
			return ValidatedParams{}, fmt.Errorf("invalid ban category: %s", category)
		}
		if _, ok := seen[category]; ok {
			return ValidatedParams{}, fmt.Errorf("duplicate ban category: %s", category)
		}
		seen[category] = struct{}{}
	}
	target := p.UserID
	return ValidatedParams{ResourceID: p.UserID, TargetUserID: &target}, nil
}

func (a *usersBanSetAction) Execute(ctx context.Context, in ActionInput) (ActionResult, error) {
	var p UsersBanSetParams
	if err := json.Unmarshal(in.Params, &p); err != nil {
		return ActionResult{}, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	if err := GuardActingOnSelf(in.ActorUserID, p.UserID); err != nil {
		return ActionResult{}, err
	}

	var discordID string
	err := a.pool.QueryRow(ctx, `
		SELECT discord_id
		FROM app_users
		WHERE id = $1 AND deleted_at IS NULL
	`, p.UserID).Scan(&discordID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ActionResult{}, fmt.Errorf("%w: user missing or deleted", ErrNotFound)
	}
	if err != nil {
		return ActionResult{}, err
	}
	if discordID == "" {
		return ActionResult{}, fmt.Errorf("%w: user has no Discord identity", ErrConflict)
	}

	if len(p.Categories) == 0 {
		if _, err := a.pool.Exec(ctx, `DELETE FROM user_bans WHERE discord_id = $1`, discordID); err != nil {
			return ActionResult{}, err
		}
	} else {
		if _, err := a.pool.Exec(ctx, `
			INSERT INTO user_bans (discord_id, categories, updated_by, updated_at)
			VALUES ($1, $2, $3, NOW())
			ON CONFLICT (discord_id) DO UPDATE
			SET categories = EXCLUDED.categories,
			    updated_by = EXCLUDED.updated_by,
			    updated_at = NOW()
		`, discordID, p.Categories, in.ActorUserID); err != nil {
			return ActionResult{}, err
		}
	}

	target := p.UserID
	return ActionResult{
		ResourceID:   p.UserID,
		TargetUserID: &target,
		Payload: map[string]any{
			"userId":     p.UserID,
			"discordId":  discordID,
			"categories": p.Categories,
		},
	}, nil
}
