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

type GuildSettings struct {
	GuildID      string
	LogChannelID string
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

		CREATE TABLE IF NOT EXISTS guild_settings (
			guild_id TEXT PRIMARY KEY,
			log_channel_id TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE TABLE IF NOT EXISTS wipe_reacts (
			message_id TEXT PRIMARY KEY,
			guild_id TEXT NOT NULL,
			channel_id TEXT NOT NULL,
			creator_id TEXT NOT NULL,
			name TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			time_zone TEXT NOT NULL,
			scheduled_unix BIGINT NOT NULL,
			ask_late_reason BOOLEAN NOT NULL DEFAULT FALSE,
			ask_declined_reason BOOLEAN NOT NULL DEFAULT FALSE,
			accepted_user_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
			declined_user_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
			late_user_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
			embed_timestamp TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE INDEX IF NOT EXISTS wipe_reacts_guild_scheduled_idx
		ON wipe_reacts (guild_id, scheduled_unix DESC);

		CREATE TABLE IF NOT EXISTS wipe_react_schedules (
			id BIGSERIAL PRIMARY KEY,
			guild_id TEXT NOT NULL,
			days JSONB NOT NULL DEFAULT '[]'::jsonb,
			includes_force_wipe BOOLEAN NOT NULL DEFAULT FALSE,
			time TEXT NOT NULL DEFAULT '',
			time_zone TEXT NOT NULL,
			repeat TEXT NOT NULL DEFAULT '',
			post_before TEXT NOT NULL,
			name TEXT NOT NULL DEFAULT '',
			description TEXT NOT NULL DEFAULT '',
			ask_late_reason BOOLEAN NOT NULL DEFAULT FALSE,
			ask_declined_reason BOOLEAN NOT NULL DEFAULT FALSE,
			channel_id TEXT NOT NULL DEFAULT '',
			last_posted_for_unix BIGINT NOT NULL DEFAULT 0,
			created_by TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE INDEX IF NOT EXISTS wipe_react_schedules_guild_idx
		ON wipe_react_schedules (guild_id);

		ALTER TABLE wipe_react_schedules ADD COLUMN IF NOT EXISTS name TEXT NOT NULL DEFAULT '';
		ALTER TABLE wipe_react_schedules ADD COLUMN IF NOT EXISTS description TEXT NOT NULL DEFAULT '';
		ALTER TABLE wipe_react_schedules ADD COLUMN IF NOT EXISTS ask_late_reason BOOLEAN NOT NULL DEFAULT FALSE;
		ALTER TABLE wipe_react_schedules ADD COLUMN IF NOT EXISTS ask_declined_reason BOOLEAN NOT NULL DEFAULT FALSE;
		ALTER TABLE wipe_react_schedules ADD COLUMN IF NOT EXISTS channel_id TEXT NOT NULL DEFAULT '';
		ALTER TABLE wipe_react_schedules ADD COLUMN IF NOT EXISTS last_posted_for_unix BIGINT NOT NULL DEFAULT 0;

		CREATE TABLE IF NOT EXISTS steam_accounts (
			steam_id TEXT PRIMARY KEY,
			bm_id TEXT UNIQUE,
			avatar_image TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE TABLE IF NOT EXISTS steam_discord_links (
			steam_id TEXT NOT NULL REFERENCES steam_accounts(steam_id),
			discord_id TEXT NOT NULL,
			type TEXT NOT NULL CHECK (type IN ('soft', 'hard')),
			source TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (steam_id, discord_id, type)
		);

		CREATE INDEX IF NOT EXISTS steam_discord_links_discord_idx
		ON steam_discord_links (discord_id);

		CREATE TABLE IF NOT EXISTS guild_steam_discord_links (
			guild_id TEXT NOT NULL,
			discord_id TEXT NOT NULL,
			steam_id TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (guild_id, discord_id, steam_id)
		);

		CREATE INDEX IF NOT EXISTS guild_steam_discord_links_guild_discord_idx
		ON guild_steam_discord_links (guild_id, discord_id);

		CREATE INDEX IF NOT EXISTS guild_steam_discord_links_guild_steam_idx
		ON guild_steam_discord_links (guild_id, steam_id);
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

	tag, err := p.pool.Exec(ctx, `
		UPDATE guild_setups
		SET status = 'configured',
			configured_by = $2,
			selected_roles = $3::jsonb,
			selected_channels = $4::jsonb,
			updated_at = NOW()
		WHERE guild_id = $1 AND status = 'pending'
	`, guildID, configuredBy, string(encoded), string(encodedChannels))
	if err != nil {
		return err
	}

	if tag.RowsAffected() == 0 {
		return errors.New("guild setup is not in pending state")
	}

	return nil
}

func (p *PostgresStore) ResetGuildSetup(ctx context.Context, guildID string) error {
	_, err := p.pool.Exec(ctx, `
		DELETE FROM guild_setups
		WHERE guild_id = $1
	`, guildID)

	return err
}

func (p *PostgresStore) GetGuildSettings(ctx context.Context, guildID string) (GuildSettings, error) {
	var logChannelID string

	err := p.pool.QueryRow(ctx, `
		SELECT log_channel_id
		FROM guild_settings
		WHERE guild_id = $1
	`, guildID).Scan(&logChannelID)
	if errors.Is(err, pgx.ErrNoRows) {
		return GuildSettings{
			GuildID:      guildID,
			LogChannelID: "",
		}, nil
	}
	if err != nil {
		return GuildSettings{}, err
	}

	return GuildSettings{
		GuildID:      guildID,
		LogChannelID: logChannelID,
	}, nil
}

func (p *PostgresStore) SetGuildLogChannel(ctx context.Context, guildID string, logChannelID string) (GuildSettings, error) {
	_, err := p.pool.Exec(ctx, `
		INSERT INTO guild_settings (guild_id, log_channel_id)
		VALUES ($1, $2)
		ON CONFLICT (guild_id) DO UPDATE
		SET log_channel_id = EXCLUDED.log_channel_id,
			updated_at = NOW()
	`, guildID, logChannelID)
	if err != nil {
		return GuildSettings{}, err
	}

	return GuildSettings{
		GuildID:      guildID,
		LogChannelID: logChannelID,
	}, nil
}
