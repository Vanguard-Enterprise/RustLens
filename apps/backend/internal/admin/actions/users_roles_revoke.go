package actions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"rustlens/backend/internal/admin/rbac"
	"rustlens/backend/internal/events"
)

type usersRolesRevokeAction struct {
	pool *pgxpool.Pool
	pub  events.Publisher
}

// NewUsersRolesRevokeAction revokes an admin role from a user. Revoking the
// last remaining "superadmin" is blocked with a 409 conflict so the system
// cannot be locked out of elevated access.
func NewUsersRolesRevokeAction(pool *pgxpool.Pool, pub events.Publisher) AdminAction {
	return &usersRolesRevokeAction{pool: pool, pub: pub}
}

func (a *usersRolesRevokeAction) Type() string                 { return "users.roles.revoke" }
func (a *usersRolesRevokeAction) Permission() string           { return rbac.PermUsersRolesAssign }
func (a *usersRolesRevokeAction) RequiresReason() bool         { return true }
func (a *usersRolesRevokeAction) Dangerous() bool              { return false }
func (a *usersRolesRevokeAction) SensitiveParamKeys() []string { return nil }
func (a *usersRolesRevokeAction) ResourceType() string         { return "user" }

func (a *usersRolesRevokeAction) Validate(raw json.RawMessage) (ValidatedParams, error) {
	var p UsersRoleAssignParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return ValidatedParams{}, fmt.Errorf("validation: %w", err)
	}
	if p.UserID == "" {
		return ValidatedParams{}, errors.New("userId is required")
	}
	if p.RoleKey == "" {
		return ValidatedParams{}, errors.New("roleKey is required")
	}
	target := p.UserID
	return ValidatedParams{ResourceID: p.UserID, TargetUserID: &target}, nil
}

func (a *usersRolesRevokeAction) Execute(ctx context.Context, in ActionInput) (ActionResult, error) {
	var p UsersRoleAssignParams
	if err := json.Unmarshal(in.Params, &p); err != nil {
		return ActionResult{}, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}

	var roleID string
	err := a.pool.QueryRow(ctx, `SELECT id::text FROM admin_roles WHERE key=$1`, p.RoleKey).Scan(&roleID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ActionResult{}, fmt.Errorf("%w: role %s", ErrNotFound, p.RoleKey)
	}
	if err != nil {
		return ActionResult{}, err
	}

	if p.RoleKey == rbac.RoleSuperadmin {
		var count int
		if err := a.pool.QueryRow(ctx, `
			SELECT COUNT(*) FROM admin_user_roles aur
			JOIN admin_roles r ON r.id = aur.role_id
			WHERE r.key = $1
		`, rbac.RoleSuperadmin).Scan(&count); err != nil {
			return ActionResult{}, err
		}
		if count <= 1 {
			return ActionResult{}, fmt.Errorf("%w: cannot revoke the last superadmin", ErrConflict)
		}
	}

	if _, err := a.pool.Exec(ctx, `
		DELETE FROM admin_user_roles WHERE user_id=$1 AND role_id=$2
	`, p.UserID, roleID); err != nil {
		return ActionResult{}, err
	}

	if a.pub != nil {
		a.pub.PublishWithFanout(p.UserID, events.Event{Type: events.TypeRolesChanged, Reason: events.ReasonRevoke})
	}

	target := p.UserID
	return ActionResult{
		ResourceID:   p.UserID,
		TargetUserID: &target,
		Payload:      map[string]any{"userId": p.UserID, "roleKey": p.RoleKey},
	}, nil
}
