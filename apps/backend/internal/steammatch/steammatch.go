// Package steammatch defines the Steam <-> BattleMetrics ID resolver
// abstraction and a fallback Chain over multiple resolver sources.
//
// Historically this resolution was provided by a single external service
// (bmmatch). That service is no longer guaranteed to exist, so the resolver
// is now an interface with a primary public source (internal/bmsearch) and an
// optional bmmatch fallback. The Chain lets RustLens stay resolvable even if
// one source disappears — the lesson learned from the bmmatch shutdown.
package steammatch

import (
	"context"
	"errors"
	"log"
	"time"
)

// fallbackResolverTimeout bounds a fallback resolver attempt that runs after a
// prior resolver returned ErrPending. The caller's original deadline may be
// exhausted by then, so fallbacks get a fresh budget instead of an already-spent
// context (which would make them fail instantly with context deadline exceeded).
const fallbackResolverTimeout = 4 * time.Second

var (
	// ErrNotFound means no source could resolve the identity.
	ErrNotFound = errors.New("steammatch: identity not found")
	// ErrNotConfigured means no resolver source is configured/enabled.
	ErrNotConfigured = errors.New("steammatch: no resolver configured")
	// ErrPending means a source accepted the lookup but has not resolved it
	// within the caller's deadline. The caller should return a degraded
	// (Steam-only) response now and backfill in the background. Only the
	// Steam->BM direction ever returns this.
	ErrPending = errors.New("steammatch: lookup pending")
)

// Resolver resolves between Steam IDs and BattleMetrics player IDs.
type Resolver interface {
	// SteamToBM resolves a 17-digit Steam ID to a BattleMetrics player ID.
	SteamToBM(ctx context.Context, steamID string) (string, error)
	// BMToSteam resolves a BattleMetrics player ID to a 17-digit Steam ID.
	BMToSteam(ctx context.Context, bmID string) (string, error)
}

// Chain tries each resolver in order, falling through on miss/pending/not
// configured. It is itself a Resolver.
type Chain struct {
	resolvers []Resolver
}

// NewChain builds a Chain from the given resolvers in priority order. Nil
// resolvers are skipped, so callers can pass an optional fallback directly.
func NewChain(resolvers ...Resolver) *Chain {
	filtered := make([]Resolver, 0, len(resolvers))
	for _, r := range resolvers {
		if r != nil {
			filtered = append(filtered, r)
		}
	}
	return &Chain{resolvers: filtered}
}

func (c *Chain) SteamToBM(ctx context.Context, steamID string) (string, error) {
	return c.run(ctx, steamID, "SteamToBM", func(cctx context.Context, r Resolver, id string) (string, error) {
		return r.SteamToBM(cctx, id)
	})
}

func (c *Chain) BMToSteam(ctx context.Context, bmID string) (string, error) {
	return c.run(ctx, bmID, "BMToSteam", func(cctx context.Context, r Resolver, id string) (string, error) {
		return r.BMToSteam(cctx, id)
	})
}

func (c *Chain) run(ctx context.Context, id, op string, call func(context.Context, Resolver, string) (string, error)) (string, error) {
	var (
		pending    bool
		configured bool
	)
	for _, r := range c.resolvers {
		callCtx := ctx
		if pending {
			// A prior resolver accepted the lookup but did not resolve it within
			// the caller's deadline, which may now be exhausted. Give the
			// fallback resolver a fresh bounded budget (detached from the spent
			// parent deadline but preserving its values) so it isn't skipped with
			// an instant "context deadline exceeded".
			var cancel context.CancelFunc
			callCtx, cancel = context.WithTimeout(context.WithoutCancel(ctx), fallbackResolverTimeout)
			defer cancel()
		}
		val, err := call(callCtx, r, id)
		if err == nil && val != "" {
			// A fallback resolving after an ErrPending wins over the pending
			// result — return it instead of ErrPending.
			return val, nil
		}
		switch {
		case errors.Is(err, ErrNotConfigured):
			// Source is off; try the next without marking configured.
			continue
		case errors.Is(err, ErrPending):
			pending = true
			configured = true
			continue
		case errors.Is(err, ErrNotFound), err == nil:
			// err == nil but empty value is treated as a miss.
			configured = true
			continue
		default:
			// Hard error (network, upstream 5xx). Fall through to the next source
			// rather than failing the whole lookup.
			configured = true
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				// Expected timeout/cancellation (e.g. the caller's deadline
				// elapsed). Don't log it as a hard resolver error — it's not
				// actionable and spams logs on every slow/pending lookup.
				continue
			}
			log.Printf("[backend] steammatch %s resolver error: %v", op, err)
			continue
		}
	}
	if pending {
		return "", ErrPending
	}
	if !configured {
		return "", ErrNotConfigured
	}
	return "", ErrNotFound
}

// Translating wraps a Resolver whose sentinel errors differ, mapping them onto
// the canonical steammatch sentinels so a Chain can classify them. Use it to
// adapt the legacy bmmatch client (whose ErrNotFound/ErrNotConfigured live in
// its own package) without coupling this package to that one.
type Translating struct {
	Inner         Resolver
	NotFound      error
	NotConfigured error
}

func (t Translating) SteamToBM(ctx context.Context, steamID string) (string, error) {
	v, err := t.Inner.SteamToBM(ctx, steamID)
	return v, t.translate(err)
}

func (t Translating) BMToSteam(ctx context.Context, bmID string) (string, error) {
	v, err := t.Inner.BMToSteam(ctx, bmID)
	return v, t.translate(err)
}

func (t Translating) translate(err error) error {
	switch {
	case err == nil:
		return nil
	case t.NotFound != nil && errors.Is(err, t.NotFound):
		return ErrNotFound
	case t.NotConfigured != nil && errors.Is(err, t.NotConfigured):
		return ErrNotConfigured
	default:
		return err
	}
}
