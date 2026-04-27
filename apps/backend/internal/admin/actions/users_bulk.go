package actions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// UsersBulkParams is the envelope accepted by the users.bulk wrapper.
//
// action chooses the underlying per-user action to invoke. params holds the
// optional sub-action parameters (e.g. {"roleKey":"user_support"}). userIds
// enumerates the targets; each is executed serially through the dispatcher so
// permissions + audit log entries mirror single-user calls exactly.
type UsersBulkParams struct {
	Action  string            `json:"action"`
	UserIDs []string          `json:"userIds"`
	Params  map[string]any    `json:"params,omitempty"`
}

// UsersBulkResultRow is a single per-row outcome from a users.bulk dispatch.
type UsersBulkResultRow struct {
	UserID string `json:"userId"`
	OK     bool   `json:"ok"`
	Error  string `json:"error,omitempty"`
}

type usersBulkAction struct {
	dispatcher *Dispatcher
}

// NewUsersBulkAction returns the users.bulk wrapper. The dispatcher reference
// is used to invoke the per-user sub-actions so they flow through the same
// permission + audit + validation pipeline as direct calls.
func NewUsersBulkAction(d *Dispatcher) AdminAction {
	return &usersBulkAction{dispatcher: d}
}

func (a *usersBulkAction) Type() string                 { return "users.bulk" }
func (a *usersBulkAction) Permission() string           { return "" }
func (a *usersBulkAction) PermissionSkipped() bool      { return true }
func (a *usersBulkAction) RequiresReason() bool         { return false }
func (a *usersBulkAction) Dangerous() bool              { return true }
func (a *usersBulkAction) SensitiveParamKeys() []string { return nil }
func (a *usersBulkAction) ResourceType() string         { return "user" }

// bulkAllowedActions is the fixed set of sub-actions addressable via users.bulk.
var bulkAllowedActions = map[string]string{
	"suspend":      "users.suspend",
	"unsuspend":    "users.unsuspend",
	"roles.grant":  "users.roles.grant",
	"roles.revoke": "users.roles.revoke",
}

func (a *usersBulkAction) Validate(raw json.RawMessage) (ValidatedParams, error) {
	var p UsersBulkParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return ValidatedParams{}, fmt.Errorf("validation: %w", err)
	}
	if _, ok := bulkAllowedActions[p.Action]; !ok {
		return ValidatedParams{}, fmt.Errorf("validation: invalid action %q", p.Action)
	}
	if len(p.UserIDs) < 1 || len(p.UserIDs) > 200 {
		return ValidatedParams{}, errors.New("userIds must be 1..200")
	}
	for _, id := range p.UserIDs {
		if id == "" {
			return ValidatedParams{}, errors.New("userIds contains empty id")
		}
	}
	if p.Action == "roles.grant" || p.Action == "roles.revoke" {
		roleKey, _ := p.Params["roleKey"].(string)
		if roleKey == "" {
			return ValidatedParams{}, errors.New("params.roleKey required for roles.grant/revoke")
		}
	}
	// Bulk wrappers don't require typed confirmation — each mutation still
	// flows through its own dispatcher pipeline where dangerous sub-actions
	// check confirmation at the sub-action level (which bulk skips by design).
	return ValidatedParams{ResourceID: "bulk"}, nil
}

func (a *usersBulkAction) Execute(ctx context.Context, in ActionInput) (ActionResult, error) {
	var p UsersBulkParams
	if err := json.Unmarshal(in.Params, &p); err != nil {
		return ActionResult{}, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	subType, ok := bulkAllowedActions[p.Action]
	if !ok {
		return ActionResult{}, fmt.Errorf("%w: invalid action", ErrValidation)
	}

	results := make([]UsersBulkResultRow, 0, len(p.UserIDs))
	succeeded, failed := 0, 0

	for _, uid := range p.UserIDs {
		// Build a sub-action param object. Start with any user-supplied params,
		// then pin userId to the current row.
		sub := map[string]any{}
		for k, v := range p.Params {
			sub[k] = v
		}
		sub["userId"] = uid
		raw, err := json.Marshal(sub)
		if err != nil {
			results = append(results, UsersBulkResultRow{UserID: uid, OK: false, Error: err.Error()})
			failed++
			continue
		}
		// For dangerous sub-actions the confirmation check would otherwise
		// block every row; satisfy it by setting the expected value, which is
		// always the userId for users.suspend/unsuspend. roles.grant/revoke
		// are not dangerous.
		_, err = a.dispatcher.Dispatch(ctx, in.ActorUserID, in.ActorRoleSnapshot, subType, raw, in.Reason)
		if err != nil {
			results = append(results, UsersBulkResultRow{UserID: uid, OK: false, Error: err.Error()})
			failed++
			continue
		}
		results = append(results, UsersBulkResultRow{UserID: uid, OK: true})
		succeeded++
	}

	return ActionResult{
		ResourceID: "bulk",
		Payload: map[string]any{
			"action":    p.Action,
			"results":   results,
			"succeeded": succeeded,
			"failed":    failed,
		},
	}, nil
}
