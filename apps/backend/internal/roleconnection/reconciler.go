package roleconnection

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Pusher is the subset of Syncer the reconciler needs. Lets us test with
// a fake without standing up the full Syncer + Discord dependencies.
type Pusher interface {
	Push(ctx context.Context, userID string) error
}

// ReconcilerStore reuses SyncerStore — only ListActiveDiscordRoleConnections
// is actually called, but reusing the interface keeps the fake test setup easy.
type ReconcilerStore interface {
	SyncerStore
}

type ReconcilerConfig struct {
	Store    ReconcilerStore
	Pusher   Pusher
	Sleep    time.Duration // between per-row pushes (be polite to Discord)
	Interval time.Duration // between full passes
	Pool     *pgxpool.Pool // for advisory lock; nil disables locking
}

type Reconciler struct {
	cfg ReconcilerConfig
}

// advisoryLockKey is a constant 64-bit integer for pg_try_advisory_lock so
// only one pod runs the reconciler at a time. Value chosen arbitrarily.
const advisoryLockKey int64 = 7427198631011

func NewReconciler(cfg ReconcilerConfig) *Reconciler {
	if cfg.Interval == 0 {
		cfg.Interval = 6 * time.Hour
	}
	if cfg.Sleep == 0 {
		cfg.Sleep = 200 * time.Millisecond
	}
	return &Reconciler{cfg: cfg}
}

// RunOnce iterates all active role-connection rows and calls Push for each.
// Per-row errors are logged; iteration continues. Holds the advisory lock
// for the duration of the pass when Pool is set.
func (r *Reconciler) RunOnce(ctx context.Context) error {
	if r.cfg.Pool != nil {
		conn, err := r.cfg.Pool.Acquire(ctx)
		if err != nil {
			return err
		}
		defer conn.Release()
		var got bool
		if err := conn.QueryRow(ctx,
			`SELECT pg_try_advisory_lock($1)`, advisoryLockKey).Scan(&got); err != nil {
			return err
		}
		if !got {
			slog.Info("roleconnection reconciler: another pod holds the lock, skipping")
			return nil
		}
		defer func() {
			_, _ = conn.Exec(ctx, `SELECT pg_advisory_unlock($1)`, advisoryLockKey)
		}()
	}
	rows, err := r.cfg.Store.ListActiveDiscordRoleConnections(ctx)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if err := r.cfg.Pusher.Push(ctx, row.UserID); err != nil {
			slog.Warn("roleconnection reconciler push failed",
				slog.String("user_id", row.UserID), slog.Any("error", err))
		}
		if r.cfg.Sleep > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(r.cfg.Sleep):
			}
		}
	}
	return nil
}

// Start runs RunOnce on an Interval ticker until ctx is done. Fires once
// immediately on start so a freshly-deployed pod doesn't wait Interval before
// catching up.
func (r *Reconciler) Start(ctx context.Context) {
	go func() {
		if err := r.RunOnce(ctx); err != nil {
			slog.Warn("roleconnection reconciler initial run failed", slog.Any("error", err))
		}
		t := time.NewTicker(r.cfg.Interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := r.RunOnce(ctx); err != nil {
					slog.Warn("roleconnection reconciler tick failed", slog.Any("error", err))
				}
			}
		}
	}()
}
