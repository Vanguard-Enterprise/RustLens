package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

var ErrGuildTrackedServerAlreadyEnabled = errors.New("guild tracked server already enabled")
var ErrGuildTrackedServerNotEnabled = errors.New("guild tracked server not enabled")

type GuildTrackedServer struct {
	GuildID string
	BmID    string
	Name    string
}

func (p *PostgresStore) ListGuildTrackedServers(ctx context.Context, guildID string) ([]GuildTrackedServer, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT gts.guild_id, gts.server_bm_id, ts.name
		FROM guild_tracked_servers gts
		JOIN tracked_servers ts ON ts.bm_id = gts.server_bm_id
		WHERE gts.guild_id = $1
		ORDER BY ts.name ASC, gts.server_bm_id ASC
	`, guildID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]GuildTrackedServer, 0)
	for rows.Next() {
		var item GuildTrackedServer
		if err := rows.Scan(&item.GuildID, &item.BmID, &item.Name); err != nil {
			return nil, err
		}
		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return items, nil
}

func (p *PostgresStore) ReplaceGuildTrackedServers(ctx context.Context, guildID string, bmIDs []string) error {
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	if _, err := tx.Exec(ctx, `
		DELETE FROM guild_tracked_servers
		WHERE guild_id = $1
	`, guildID); err != nil {
		return err
	}

	for _, bmID := range bmIDs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO guild_tracked_servers (guild_id, server_bm_id)
			VALUES ($1, $2)
		`, guildID, bmID); err != nil {
			return err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}

	return nil
}

// AddGuildTrackedServer enables one tracked server for the guild. Returns
// ErrGuildTrackedServerAlreadyEnabled if the server is already enabled and
// ErrTrackedServerNotFound if the bm_id is not present in tracked_servers.
func (p *PostgresStore) AddGuildTrackedServer(ctx context.Context, guildID string, bmID string) error {
	var exists bool
	if err := p.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM tracked_servers WHERE bm_id = $1)`, bmID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrTrackedServerNotFound
	}

	tag, err := p.pool.Exec(ctx, `
		INSERT INTO guild_tracked_servers (guild_id, server_bm_id)
		VALUES ($1, $2)
		ON CONFLICT (guild_id, server_bm_id) DO NOTHING
	`, guildID, bmID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrGuildTrackedServerAlreadyEnabled
	}
	return nil
}

// RemoveGuildTrackedServer disables one tracked server for the guild. Returns
// ErrGuildTrackedServerNotEnabled if the server was not enabled.
func (p *PostgresStore) RemoveGuildTrackedServer(ctx context.Context, guildID string, bmID string) error {
	tag, err := p.pool.Exec(ctx, `
		DELETE FROM guild_tracked_servers
		WHERE guild_id = $1 AND server_bm_id = $2
	`, guildID, bmID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrGuildTrackedServerNotEnabled
	}
	return nil
}
