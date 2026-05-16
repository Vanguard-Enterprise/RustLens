package rbac

import (
	"context"
)

type PermissionLoader interface {
	PermissionsForUser(ctx context.Context, userID string) ([]string, error)
}

type Resolver struct{ loader PermissionLoader }

func NewResolver(loader PermissionLoader) *Resolver { return &Resolver{loader: loader} }

type requestCacheKey struct{}

type requestCache struct {
	entries map[string]map[string]bool
}

type apiKeyPermsKey struct{}

// WithAPIKeyPermissions stamps the API key's snapshot permissions onto the
// context. The resolver intersects the user's current role permissions with
// this snapshot, so the effective set is the floor of the two — meaning
// revoking a permission from the user immediately constrains every key they
// minted without touching the api_keys table.
func WithAPIKeyPermissions(ctx context.Context, perms []string) context.Context {
	set := make(map[string]bool, len(perms))
	for _, p := range perms {
		set[p] = true
	}
	return context.WithValue(ctx, apiKeyPermsKey{}, set)
}

func apiKeyPermsFromContext(ctx context.Context) (map[string]bool, bool) {
	v, ok := ctx.Value(apiKeyPermsKey{}).(map[string]bool)
	return v, ok
}

func (r *Resolver) WithRequest(ctx context.Context) context.Context {
	return context.WithValue(ctx, requestCacheKey{}, &requestCache{entries: map[string]map[string]bool{}})
}

func (r *Resolver) HasPermission(ctx context.Context, userID string, key string) (bool, error) {
	set, err := r.loadSet(ctx, userID)
	if err != nil {
		return false, err
	}
	return set[key], nil
}

func (r *Resolver) loadSet(ctx context.Context, userID string) (map[string]bool, error) {
	cache, _ := ctx.Value(requestCacheKey{}).(*requestCache)
	if cache != nil {
		if set, ok := cache.entries[userID]; ok {
			return set, nil
		}
	}
	perms, err := r.loader.PermissionsForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	set := make(map[string]bool, len(perms))
	for _, p := range perms {
		set[p] = true
	}
	if keySet, ok := apiKeyPermsFromContext(ctx); ok {
		filtered := make(map[string]bool, len(set))
		for k := range set {
			if keySet[k] {
				filtered[k] = true
			}
		}
		set = filtered
	}
	if cache != nil {
		cache.entries[userID] = set
	}
	return set, nil
}

func (r *Resolver) PermissionsForUser(ctx context.Context, userID string) ([]string, error) {
	set, err := r.loadSet(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	return out, nil
}
