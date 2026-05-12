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

type UsersRoleAssignParams struct {
	UserID  string `json:"userId"`
	RoleKey string `json:"roleKey"`
}

type usersRolesGrantAction struct {
	pool     *pgxpool.Pool
	resolver *rbac.Resolver
	pub      events.Publisher
}

// NewUsersRolesGrantAction grants an admin role to a user. Granting the
// "superadmin" role requires the actor to hold the admins.grant permission
// in addition to the base users.roles.assign permission.
func NewUsersRolesGrantAction(pool *pgxpool.Pool, resolver *rbac.Resolver, pub events.Publisher) AdminAction {
	return &usersRolesGrantAction{pool: pool, resolver: resolver, pub: pub}
}

func (a *usersRolesGrantAction) Type() string                 { return "users.roles.grant" }
func (a *usersRolesGrantAction) Permission() string           { return rbac.PermUsersRolesAssign }
func (a *usersRolesGrantAction) RequiresReason() bool         { return true }
func (a *usersRolesGrantAction) Dangerous() bool              { return false }
func (a *usersRolesGrantAction) SensitiveParamKeys() []string { return nil }
func (a *usersRolesGrantAction) ResourceType() string         { return "user" }

func (a *usersRolesGrantAction) Validate(raw json.RawMessage) (ValidatedParams, error) {
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

func (a *usersRolesGrantAction) Execute(ctx context.Context, in ActionInput) (ActionResult, error) {
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
		ok, err := a.resolver.HasPermission(ctx, in.ActorUserID, rbac.PermAdminsGrant)
		if err != nil {
			return ActionResult{}, err
		}
		if !ok {
			return ActionResult{}, fmt.Errorf("%w: grant of superadmin requires %s", ErrPermissionDenied, rbac.PermAdminsGrant)
		}
	}

	if _, err := a.pool.Exec(ctx, `
		INSERT INTO admin_user_roles (user_id, role_id, granted_by)
		VALUES ($1, $2, $3) ON CONFLICT DO NOTHING
	`, p.UserID, roleID, in.ActorUserID); err != nil {
		return ActionResult{}, err
	}

	if a.pub != nil {
		a.pub.PublishWithFanout(p.UserID, events.Event{Type: events.TypeRolesChanged, Reason: events.ReasonGrant})
	}

	target := p.UserID
	return ActionResult{
		ResourceID:   p.UserID,
		TargetUserID: &target,
		Payload:      map[string]any{"userId": p.UserID, "roleKey": p.RoleKey},
	}, nil
}
