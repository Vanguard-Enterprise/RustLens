package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

var ErrOutboundProxyNotFound = errors.New("outbound proxy not found")
var ErrOutboundProxyAlreadyExists = errors.New("outbound proxy already exists")

type OutboundProxy struct {
	ID               int64
	RedactedEndpoint string
	Ciphertext       []byte
	Nonce            []byte
	KeyID            string
	Enabled          bool
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

func (p *PostgresStore) ListOutboundProxies(ctx context.Context) ([]OutboundProxy, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT id, redacted_endpoint, endpoint_ciphertext, endpoint_nonce, key_id, enabled, created_at, updated_at
		FROM outbound_proxies
		ORDER BY enabled ASC, id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]OutboundProxy, 0)
	for rows.Next() {
		var item OutboundProxy
		if err := rows.Scan(
			&item.ID,
			&item.RedactedEndpoint,
			&item.Ciphertext,
			&item.Nonce,
			&item.KeyID,
			&item.Enabled,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return items, nil
}

func (p *PostgresStore) ListEnabledOutboundProxies(ctx context.Context) ([]OutboundProxy, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT id, redacted_endpoint, endpoint_ciphertext, endpoint_nonce, key_id, enabled, created_at, updated_at
		FROM outbound_proxies
		WHERE enabled = TRUE
		ORDER BY id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]OutboundProxy, 0)
	for rows.Next() {
		var item OutboundProxy
		if err := rows.Scan(
			&item.ID,
			&item.RedactedEndpoint,
			&item.Ciphertext,
			&item.Nonce,
			&item.KeyID,
			&item.Enabled,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return items, nil
}

func (p *PostgresStore) CreateOutboundProxy(ctx context.Context, redactedEndpoint string, ciphertext []byte, nonce []byte, keyID string) (OutboundProxy, error) {
	row := p.pool.QueryRow(ctx, `
		INSERT INTO outbound_proxies (redacted_endpoint, endpoint_ciphertext, endpoint_nonce, key_id)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (redacted_endpoint) DO NOTHING
		RETURNING id, redacted_endpoint, endpoint_ciphertext, endpoint_nonce, key_id, enabled, created_at, updated_at
	`, redactedEndpoint, ciphertext, nonce, keyID)

	var item OutboundProxy
	err := row.Scan(
		&item.ID,
		&item.RedactedEndpoint,
		&item.Ciphertext,
		&item.Nonce,
		&item.KeyID,
		&item.Enabled,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return OutboundProxy{}, ErrOutboundProxyAlreadyExists
	}
	if err != nil {
		return OutboundProxy{}, err
	}

	return item, nil
}

func (p *PostgresStore) DeleteOutboundProxy(ctx context.Context, id int64) error {
	tag, err := p.pool.Exec(ctx, `
		DELETE FROM outbound_proxies
		WHERE id = $1
	`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrOutboundProxyNotFound
	}

	return nil
}

func (p *PostgresStore) SetOutboundProxyEnabled(ctx context.Context, id int64, enabled bool) (OutboundProxy, error) {
	row := p.pool.QueryRow(ctx, `
		UPDATE outbound_proxies
		SET enabled = $2, updated_at = NOW()
		WHERE id = $1
		RETURNING id, redacted_endpoint, endpoint_ciphertext, endpoint_nonce, key_id, enabled, created_at, updated_at
	`, id, enabled)

	var item OutboundProxy
	err := row.Scan(
		&item.ID,
		&item.RedactedEndpoint,
		&item.Ciphertext,
		&item.Nonce,
		&item.KeyID,
		&item.Enabled,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return OutboundProxy{}, ErrOutboundProxyNotFound
	}
	if err != nil {
		return OutboundProxy{}, err
	}

	return item, nil
}

func (p *PostgresStore) DisableOutboundProxyByRedactedEndpoint(ctx context.Context, redactedEndpoint string) (bool, error) {
	tag, err := p.pool.Exec(ctx, `
		UPDATE outbound_proxies
		SET enabled = FALSE, updated_at = NOW()
		WHERE redacted_endpoint = $1 AND enabled = TRUE
	`, redactedEndpoint)
	if err != nil {
		return false, err
	}

	return tag.RowsAffected() > 0, nil
}
