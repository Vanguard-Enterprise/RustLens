package httpapi

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// buildUsersQuery constructs the shared WHERE clause, positional args, and
// ORDER BY for the admin users list endpoint. It mirrors the filter
// semantics documented on ListAdminUsers.
//
// The returned args are positional — any caller must append LIMIT/OFFSET args
// after those, and reference them as $(len(args)-1) / $(len(args)) in the
// final SQL.
func buildUsersQuery(q url.Values) (string, []any, string, error) {
	args := []any{}
	where := []string{}

	status := q.Get("status")
	if status == "" {
		status = "active"
	}
	switch status {
	case "active":
		where = append(where, "u.suspended_at IS NULL AND u.deleted_at IS NULL")
	case "suspended":
		where = append(where, "u.suspended_at IS NOT NULL AND u.deleted_at IS NULL")
	case "deleted":
		where = append(where, "u.deleted_at IS NOT NULL")
	case "any":
		// no filter
	default:
		return "", nil, "", errors.New("invalid status")
	}

	if s := strings.TrimSpace(q.Get("q")); s != "" {
		args = append(args, "%"+s+"%")
		p := fmt.Sprintf("$%d", len(args))
		where = append(where, fmt.Sprintf(`(
			u.display_name ILIKE %[1]s
			OR u.id ILIKE %[1]s
			OR u.discord_username ILIKE %[1]s
			OR u.discord_global_name ILIKE %[1]s
			OR u.discord_id ILIKE %[1]s
		)`, p))
	}

	if roles, ok := q["role"]; ok && len(roles) > 0 {
		args = append(args, roles)
		p := fmt.Sprintf("$%d", len(args))
		where = append(where, fmt.Sprintf(`EXISTS (
			SELECT 1 FROM admin_user_roles aur JOIN admin_roles r ON r.id = aur.role_id
			WHERE aur.user_id = u.id AND r.key = ANY(%s)
		)`, p))
	}

	// Discord identity is now intrinsic to app_users — every account has a
	// discord_id NOT NULL. The "linked" filter therefore matches every active
	// user; "unlinked" matches none. Kept for backward-compat with bookmarked
	// querystrings.
	switch q.Get("discord") {
	case "", "any":
	case "linked":
		where = append(where, `u.discord_id IS NOT NULL AND u.discord_id <> ''`)
	case "unlinked":
		where = append(where, `(u.discord_id IS NULL OR u.discord_id = '')`)
	default:
		return "", nil, "", errors.New("invalid discord")
	}

	// Steam connections were previously tracked via user_connected_accounts,
	// which the Discord-only migration removed. The filter is accepted but
	// has no effect; "linked" returns nothing and "unlinked" returns all.
	switch q.Get("steam") {
	case "", "any":
	case "linked":
		where = append(where, `FALSE`)
	case "unlinked":
		// no-op
	default:
		return "", nil, "", errors.New("invalid steam")
	}

	orderBy := orderByClause(q)
	if orderBy == "" {
		return "", nil, "", errors.New("invalid sort")
	}

	whereSQL := ""
	if len(where) > 0 {
		whereSQL = "WHERE " + strings.Join(where, " AND ")
	}
	return whereSQL, args, orderBy, nil
}

func orderByClause(q url.Values) string {
	sort := q.Get("sort")
	desc := strings.HasPrefix(sort, "-")
	sortField := strings.TrimPrefix(sort, "-")
	orderBy := ""
	switch sortField {
	case "", "createdAt":
		orderBy = "u.created_at"
	case "lastLoginAt":
		orderBy = "u.last_login_at"
	case "displayName":
		orderBy = "u.display_name"
	default:
		return ""
	}
	if desc || sortField == "" {
		orderBy += " DESC"
	} else {
		orderBy += " ASC"
	}
	return orderBy
}
