package store

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresStore struct {
	pool *pgxpool.Pool
}

type GuildSetupState struct {
	GuildID          string
	Status           string
	ConfiguredBy     string
	SelectedRoles    []string
	SelectedChannels []string
}

func NewPostgresStore(ctx context.Context, connectionString string) (*PostgresStore, error) {
	pool, err := pgxpool.New(ctx, connectionString)
	if err != nil {
		return nil, err
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}

	return &PostgresStore{pool: pool}, nil
}

func (p *PostgresStore) Close() {
	p.pool.Close()
}

func (p *PostgresStore) Ping(ctx context.Context) error {
	return p.pool.Ping(ctx)
}

func (p *PostgresStore) EnsureSchema(ctx context.Context) error {
	_, err := p.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS bot_worker_events (
			id BIGSERIAL PRIMARY KEY,
			worker_id TEXT NOT NULL,
			event_type TEXT NOT NULL,
			payload JSONB NOT NULL DEFAULT '{}'::jsonb,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE TABLE IF NOT EXISTS guild_setups (
			guild_id TEXT PRIMARY KEY,
			status TEXT NOT NULL,
			configured_by TEXT NOT NULL DEFAULT '',
			selected_roles JSONB NOT NULL DEFAULT '[]'::jsonb,
			selected_channels JSONB NOT NULL DEFAULT '[]'::jsonb,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		ALTER TABLE guild_setups
		ADD COLUMN IF NOT EXISTS selected_channels JSONB NOT NULL DEFAULT '[]'::jsonb;
	`)
	return err
}

func (p *PostgresStore) InsertWorkerEvent(ctx context.Context, workerID string, eventType string, payload any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	_, err = p.pool.Exec(ctx, `
		INSERT INTO bot_worker_events (worker_id, event_type, payload)
		VALUES ($1, $2, $3::jsonb)
	`, workerID, eventType, string(encoded))

	return err
}

func (p *PostgresStore) GetGuildSetupState(ctx context.Context, guildID string) (GuildSetupState, error) {
	var (
		status               string
		configuredBy         string
		selectedJSON         []byte
		selectedChannelsJSON []byte
	)

	err := p.pool.QueryRow(ctx, `
		SELECT status, configured_by, selected_roles, selected_channels
		FROM guild_setups
		WHERE guild_id = $1
	`, guildID).Scan(&status, &configuredBy, &selectedJSON, &selectedChannelsJSON)
	if errors.Is(err, pgx.ErrNoRows) {
		return GuildSetupState{
			GuildID:          guildID,
			Status:           "missing",
			ConfiguredBy:     "",
			SelectedRoles:    []string{},
			SelectedChannels: []string{},
		}, nil
	}
	if err != nil {
		return GuildSetupState{}, err
	}

	var selectedRoles []string
	var selectedChannels []string
	if len(selectedJSON) > 0 {
		if err := json.Unmarshal(selectedJSON, &selectedRoles); err != nil {
			return GuildSetupState{}, err
		}
	}
	if len(selectedChannelsJSON) > 0 {
		if err := json.Unmarshal(selectedChannelsJSON, &selectedChannels); err != nil {
			return GuildSetupState{}, err
		}
	}

	return GuildSetupState{
		GuildID:          guildID,
		Status:           status,
		ConfiguredBy:     configuredBy,
		SelectedRoles:    selectedRoles,
		SelectedChannels: selectedChannels,
	}, nil
}

func (p *PostgresStore) ClaimGuildSetup(ctx context.Context, guildID string) (bool, GuildSetupState, error) {
	tag, err := p.pool.Exec(ctx, `
		INSERT INTO guild_setups (guild_id, status)
		VALUES ($1, 'pending')
		ON CONFLICT (guild_id) DO NOTHING
	`, guildID)
	if err != nil {
		return false, GuildSetupState{}, err
	}

	if tag.RowsAffected() == 1 {
		return true, GuildSetupState{
			GuildID:          guildID,
			Status:           "pending",
			ConfiguredBy:     "",
			SelectedRoles:    []string{},
			SelectedChannels: []string{},
		}, nil
	}

	state, err := p.GetGuildSetupState(ctx, guildID)
	if err != nil {
		return false, GuildSetupState{}, err
	}

	return false, state, nil
}

func (p *PostgresStore) CompleteGuildSetup(ctx context.Context, guildID string, configuredBy string, selectedRoles []string, selectedChannels []string) error {
	encoded, err := json.Marshal(selectedRoles)
	if err != nil {
		return err
	}
	encodedChannels, err := json.Marshal(selectedChannels)
	if err != nil {
		return err
	}

	_, err = p.pool.Exec(ctx, `
		UPDATE guild_setups
		SET status = 'configured',
			configured_by = $2,
			selected_roles = $3::jsonb,
			selected_channels = $4::jsonb,
			updated_at = NOW()
		WHERE guild_id = $1
	`, guildID, configuredBy, string(encoded), string(encodedChannels))

	return err
}

func (p *PostgresStore) ResetGuildSetup(ctx context.Context, guildID string) error {
	_, err := p.pool.Exec(ctx, `
		DELETE FROM guild_setups
		WHERE guild_id = $1
	`, guildID)

	return err
}
