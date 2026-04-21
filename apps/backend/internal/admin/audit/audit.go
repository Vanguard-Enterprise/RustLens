package audit

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Entry struct {
	ActionType        string
	ActorUserID       *uuid.UUID
	ActorRoleSnapshot []string
	ResourceType      string
	ResourceID        string
	TargetUserID      *uuid.UUID
	Params            json.RawMessage
	Reason            *string
	Result            string // "success" or "error"
	ErrorMessage      *string
	IPAddress         *string
	UserAgent         *string
}

type Row struct {
	ID                uuid.UUID
	ActionType        string
	ActorUserID       *uuid.UUID
	ActorRoleSnapshot []string
	ResourceType      string
	ResourceID        string
	TargetUserID      *uuid.UUID
	Params            json.RawMessage
	Reason            *string
	Result            string
	ErrorMessage      *string
	IPAddress         *string
	UserAgent         *string
	CreatedAt         time.Time
}

type QueryFilters struct {
	ActorUserID  *uuid.UUID
	TargetUserID *uuid.UUID
	ActionType   *string
	ResourceType *string
	ResourceID   *string
	Result       *string
	Since        *time.Time
	Until        *time.Time
	Limit        int
}

type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func (s *Store) Record(ctx context.Context, e Entry) error {
	if e.Params == nil {
		e.Params = json.RawMessage(`{}`)
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO audit_events (
			action_type, actor_user_id, actor_role_snapshot,
			resource_type, resource_id, target_user_id,
			params, reason, result, error_message, ip_address, user_agent
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
	`, e.ActionType, e.ActorUserID, e.ActorRoleSnapshot,
		nullIfEmpty(e.ResourceType), nullIfEmpty(e.ResourceID), e.TargetUserID,
		e.Params, e.Reason, e.Result, e.ErrorMessage, e.IPAddress, e.UserAgent,
	)
	return err
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (s *Store) Query(ctx context.Context, f QueryFilters) ([]Row, error) {
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 100
	}
	sql := `SELECT id, action_type, actor_user_id, actor_role_snapshot,
		COALESCE(resource_type,''), COALESCE(resource_id,''), target_user_id,
		params, reason, result, error_message, ip_address::text, user_agent, created_at
		FROM audit_events WHERE 1=1`
	args := []any{}
	add := func(clause string, v any) {
		args = append(args, v)
		sql += " AND " + clause + " = $" + itoa(len(args))
	}
	if f.ActorUserID != nil {
		add("actor_user_id", *f.ActorUserID)
	}
	if f.TargetUserID != nil {
		add("target_user_id", *f.TargetUserID)
	}
	if f.ActionType != nil {
		add("action_type", *f.ActionType)
	}
	if f.ResourceType != nil {
		add("resource_type", *f.ResourceType)
	}
	if f.ResourceID != nil {
		add("resource_id", *f.ResourceID)
	}
	if f.Result != nil {
		add("result", *f.Result)
	}
	if f.Since != nil {
		args = append(args, *f.Since)
		sql += " AND created_at >= $" + itoa(len(args))
	}
	if f.Until != nil {
		args = append(args, *f.Until)
		sql += " AND created_at <= $" + itoa(len(args))
	}
	args = append(args, f.Limit)
	sql += " ORDER BY created_at DESC LIMIT $" + itoa(len(args))

	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Row
	for rows.Next() {
		var r Row
		if err := rows.Scan(
			&r.ID, &r.ActionType, &r.ActorUserID, &r.ActorRoleSnapshot,
			&r.ResourceType, &r.ResourceID, &r.TargetUserID,
			&r.Params, &r.Reason, &r.Result, &r.ErrorMessage, &r.IPAddress, &r.UserAgent, &r.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func itoa(i int) string {
	// small helper to avoid strconv import churn; safe since caller bounds i
	const digits = "0123456789"
	if i < 10 {
		return string(digits[i])
	}
	return itoa(i/10) + string(digits[i%10])
}
