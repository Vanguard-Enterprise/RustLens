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
	AutoDisabledAt   *time.Time
}

const outboundProxySelectCols = `id, redacted_endpoint, endpoint_ciphertext, endpoint_nonce, key_id, enabled, created_at, updated_at, auto_disabled_at`

func scanOutboundProxy(s interface {
	Scan(dest ...any) error
}) (OutboundProxy, error) {
	var item OutboundProxy
	err := s.Scan(
		&item.ID,
		&item.RedactedEndpoint,
		&item.Ciphertext,
		&item.Nonce,
		&item.KeyID,
		&item.Enabled,
		&item.CreatedAt,
		&item.UpdatedAt,
		&item.AutoDisabledAt,
	)
	return item, err
}

func (p *PostgresStore) ListOutboundProxies(ctx context.Context) ([]OutboundProxy, error) {
	// Pin auto-disabled (broken) first, then manually disabled, then enabled.
	rows, err := p.pool.Query(ctx, `
		SELECT `+outboundProxySelectCols+`
		FROM outbound_proxies
		ORDER BY (auto_disabled_at IS NOT NULL) DESC, enabled ASC, id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]OutboundProxy, 0)
	for rows.Next() {
		item, err := scanOutboundProxy(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return items, nil
}

type OutboundProxyFilter struct {
	// Q filters on redacted_endpoint (case-insensitive substring) when non-empty.
	Q string
	// Status filters by derived status: "active", "paused", "broken", or "" for any.
	Status string
}

// ListOutboundProxiesPage returns a paginated slice plus the total matching
// the filter. Sort order pins broken proxies first, then disabled, then
// enabled.
func (p *PostgresStore) ListOutboundProxiesPage(ctx context.Context, filter OutboundProxyFilter, limit, offset int) ([]OutboundProxy, int, error) {
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	whereSQL := "WHERE 1=1"
	args := []any{}
	if filter.Q != "" {
		args = append(args, "%"+filter.Q+"%")
		whereSQL += " AND redacted_endpoint ILIKE $" + itoa(len(args))
	}
	switch filter.Status {
	case "active":
		whereSQL += " AND enabled = TRUE AND auto_disabled_at IS NULL"
	case "paused":
		whereSQL += " AND enabled = FALSE AND auto_disabled_at IS NULL"
	case "broken":
		whereSQL += " AND auto_disabled_at IS NOT NULL"
	}

	var total int
	countSQL := "SELECT COUNT(*) FROM outbound_proxies " + whereSQL
	if err := p.pool.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	args = append(args, limit, offset)
	listSQL := "SELECT " + outboundProxySelectCols + " FROM outbound_proxies " + whereSQL +
		" ORDER BY (auto_disabled_at IS NOT NULL) DESC, enabled ASC, id ASC" +
		" LIMIT $" + itoa(len(args)-1) + " OFFSET $" + itoa(len(args))

	rows, err := p.pool.Query(ctx, listSQL, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]OutboundProxy, 0)
	for rows.Next() {
		item, err := scanOutboundProxy(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	return items, total, nil
}

func itoa(n int) string {
	// Small positive integer formatter without importing strconv at every call site.
	if n == 0 {
		return "0"
	}
	digits := [20]byte{}
	i := len(digits)
	for n > 0 {
		i--
		digits[i] = byte('0' + n%10)
		n /= 10
	}
	return string(digits[i:])
}

func (p *PostgresStore) ListEnabledOutboundProxies(ctx context.Context) ([]OutboundProxy, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT `+outboundProxySelectCols+`
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
		item, err := scanOutboundProxy(rows)
		if err != nil {
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
		RETURNING `+outboundProxySelectCols+`
	`, redactedEndpoint, ciphertext, nonce, keyID)

	item, err := scanOutboundProxy(row)
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

// SetOutboundProxyEnabled toggles the manual enabled flag. Enabling a proxy
// also clears auto_disabled_at so it is no longer reported as "broken".
func (p *PostgresStore) SetOutboundProxyEnabled(ctx context.Context, id int64, enabled bool) (OutboundProxy, error) {
	row := p.pool.QueryRow(ctx, `
		UPDATE outbound_proxies
		SET enabled = $2,
		    auto_disabled_at = CASE WHEN $2 THEN NULL ELSE auto_disabled_at END,
		    updated_at = NOW()
		WHERE id = $1
		RETURNING `+outboundProxySelectCols+`
	`, id, enabled)

	item, err := scanOutboundProxy(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return OutboundProxy{}, ErrOutboundProxyNotFound
	}
	if err != nil {
		return OutboundProxy{}, err
	}

	return item, nil
}

// ClearOutboundProxyAutoDisabled clears the auto-disabled marker and
// re-enables the proxy. Used by admin "clear broken" actions.
func (p *PostgresStore) ClearOutboundProxyAutoDisabled(ctx context.Context, id int64) (OutboundProxy, error) {
	row := p.pool.QueryRow(ctx, `
		UPDATE outbound_proxies
		SET enabled = TRUE, auto_disabled_at = NULL, updated_at = NOW()
		WHERE id = $1
		RETURNING `+outboundProxySelectCols+`
	`, id)

	item, err := scanOutboundProxy(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return OutboundProxy{}, ErrOutboundProxyNotFound
	}
	if err != nil {
		return OutboundProxy{}, err
	}

	return item, nil
}

// DisableOutboundProxyByRedactedEndpoint is invoked by the runtime auto-
// disable callback. It both flips enabled = FALSE and records the
// auto_disabled_at timestamp so admins can distinguish broken from paused.
func (p *PostgresStore) DisableOutboundProxyByRedactedEndpoint(ctx context.Context, redactedEndpoint string) (bool, error) {
	tag, err := p.pool.Exec(ctx, `
		UPDATE outbound_proxies
		SET enabled = FALSE,
		    auto_disabled_at = COALESCE(auto_disabled_at, NOW()),
		    updated_at = NOW()
		WHERE redacted_endpoint = $1 AND (enabled = TRUE OR auto_disabled_at IS NULL)
	`, redactedEndpoint)
	if err != nil {
		return false, err
	}

	return tag.RowsAffected() > 0, nil
}
