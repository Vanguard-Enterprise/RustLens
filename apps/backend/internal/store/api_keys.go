package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// APIKey is a row in the api_keys table.
type APIKey struct {
	ID              string
	Name            string
	Prefix          string
	Last4           string
	SecretHash      []byte
	CreatedByUserID string
	Permissions     []string
	ExpiresAt       *time.Time
	RevokedAt       *time.Time
	RevokedReason   *string
	LastUsedAt      *time.Time
	LastUsedIP      *string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// APIKeyCreate is the input for Store.Create.
type APIKeyCreate struct {
	Name            string
	Prefix          string
	Last4           string
	SecretHash      []byte
	CreatedByUserID string
	Permissions     []string
	ExpiresAt       *time.Time
}

// APIKeyStore is the persistence layer for api_keys.
type APIKeyStore struct{ pool *pgxpool.Pool }

func NewAPIKeyStore(pool *pgxpool.Pool) *APIKeyStore { return &APIKeyStore{pool: pool} }

// ErrAPIKeyNotFound is returned by GetByPrefix / GetByID when no row matches.
var ErrAPIKeyNotFound = errors.New("api key not found")

func (s *APIKeyStore) Create(ctx context.Context, in APIKeyCreate) (APIKey, error) {
	row := s.pool.QueryRow(ctx, `
		INSERT INTO api_keys
		    (name, prefix, last4, secret_hash, created_by_user_id, permissions, expires_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		RETURNING id::text, created_at, updated_at
	`, in.Name, in.Prefix, in.Last4, in.SecretHash, in.CreatedByUserID, in.Permissions, in.ExpiresAt)
	var k APIKey
	k.Name = in.Name
	k.Prefix = in.Prefix
	k.Last4 = in.Last4
	k.SecretHash = in.SecretHash
	k.CreatedByUserID = in.CreatedByUserID
	k.Permissions = in.Permissions
	k.ExpiresAt = in.ExpiresAt
	if err := row.Scan(&k.ID, &k.CreatedAt, &k.UpdatedAt); err != nil {
		return APIKey{}, err
	}
	return k, nil
}

func (s *APIKeyStore) GetByPrefix(ctx context.Context, prefix string) (APIKey, error) {
	return s.scanOne(ctx, `WHERE prefix = $1`, prefix)
}

func (s *APIKeyStore) GetByID(ctx context.Context, id string) (APIKey, error) {
	return s.scanOne(ctx, `WHERE id::text = $1`, id)
}

func (s *APIKeyStore) scanOne(ctx context.Context, whereClause string, arg any) (APIKey, error) {
	var k APIKey
	var ipStr *string
	err := s.pool.QueryRow(ctx, `
		SELECT id::text, name, prefix, last4, secret_hash, created_by_user_id,
		       permissions, expires_at, revoked_at, revoked_reason,
		       last_used_at, last_used_ip::text, created_at, updated_at
		  FROM api_keys `+whereClause, arg,
	).Scan(
		&k.ID, &k.Name, &k.Prefix, &k.Last4, &k.SecretHash, &k.CreatedByUserID,
		&k.Permissions, &k.ExpiresAt, &k.RevokedAt, &k.RevokedReason,
		&k.LastUsedAt, &ipStr, &k.CreatedAt, &k.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return APIKey{}, ErrAPIKeyNotFound
	}
	if err != nil {
		return APIKey{}, err
	}
	k.LastUsedIP = ipStr
	return k, nil
}

// Revoke marks the key revoked. Idempotent: no-op if already revoked.
func (s *APIKeyStore) Revoke(ctx context.Context, id, reason string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE api_keys
		   SET revoked_at = NOW(), revoked_reason = $2, updated_at = NOW()
		 WHERE id::text = $1 AND revoked_at IS NULL
	`, id, reason)
	return err
}

// Rotate replaces secret material in place. Preserves id, name, permissions,
// created_by_user_id, expires_at, and created_at.
func (s *APIKeyStore) Rotate(ctx context.Context, id, newPrefix, newLast4 string, newHash []byte) error {
	res, err := s.pool.Exec(ctx, `
		UPDATE api_keys
		   SET prefix = $2, last4 = $3, secret_hash = $4, updated_at = NOW()
		 WHERE id::text = $1 AND revoked_at IS NULL
	`, id, newPrefix, newLast4, newHash)
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		return ErrAPIKeyNotFound
	}
	return nil
}

// UpdateLastUsed records the most recent successful auth.
func (s *APIKeyStore) UpdateLastUsed(ctx context.Context, id, ip string) error {
	var ipArg any
	if ip != "" {
		ipArg = ip
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE api_keys
		   SET last_used_at = NOW(), last_used_ip = $2
		 WHERE id::text = $1
	`, id, ipArg)
	return err
}

// APIKeyListQuery is the input for Store.List.
type APIKeyListQuery struct {
	Q              string
	IncludeRevoked bool
	Page           int
	PageSize       int
}

// APIKeyListItem is a row plus the creator's display name.
type APIKeyListItem struct {
	APIKey
	CreatedByDisplayName string
}

func (s *APIKeyStore) List(ctx context.Context, q APIKeyListQuery) ([]APIKeyListItem, int, error) {
	page := q.Page
	if page < 1 {
		page = 1
	}
	size := q.PageSize
	if size <= 0 {
		size = 20
	}
	if size > 100 {
		size = 100
	}
	offset := (page - 1) * size

	args := []any{}
	where := "WHERE 1=1"
	if !q.IncludeRevoked {
		where += " AND k.revoked_at IS NULL"
	}
	if q.Q != "" {
		args = append(args, "%"+q.Q+"%")
		where += " AND (k.name ILIKE $1 OR k.prefix ILIKE $1)"
	}
	countArgs := append([]any(nil), args...)

	var total int
	if err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM api_keys k `+where, countArgs...,
	).Scan(&total); err != nil {
		return nil, 0, err
	}

	args = append(args, size, offset)
	rows, err := s.pool.Query(ctx, `
		SELECT k.id::text, k.name, k.prefix, k.last4, k.secret_hash, k.created_by_user_id,
		       k.permissions, k.expires_at, k.revoked_at, k.revoked_reason,
		       k.last_used_at, k.last_used_ip::text, k.created_at, k.updated_at,
		       COALESCE(u.display_name, u.discord_username, '')
		  FROM api_keys k
		  LEFT JOIN app_users u ON u.id = k.created_by_user_id
		`+where+`
		ORDER BY k.created_at DESC
		LIMIT $`+itoa(len(args)-1)+` OFFSET $`+itoa(len(args)),
		args...,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []APIKeyListItem
	for rows.Next() {
		var it APIKeyListItem
		var ipStr *string
		if err := rows.Scan(
			&it.ID, &it.Name, &it.Prefix, &it.Last4, &it.SecretHash, &it.CreatedByUserID,
			&it.Permissions, &it.ExpiresAt, &it.RevokedAt, &it.RevokedReason,
			&it.LastUsedAt, &ipStr, &it.CreatedAt, &it.UpdatedAt,
			&it.CreatedByDisplayName,
		); err != nil {
			return nil, 0, err
		}
		it.LastUsedIP = ipStr
		out = append(out, it)
	}
	return out, total, rows.Err()
}
