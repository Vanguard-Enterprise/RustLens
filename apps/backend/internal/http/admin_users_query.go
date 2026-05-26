package httpapi

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
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

	// Joined-date window. Accepts RFC3339 or plain YYYY-MM-DD. `joinedSince` is
	// inclusive; `joinedBefore` is exclusive. Empty params mean unbounded.
	if v := strings.TrimSpace(q.Get("joinedSince")); v != "" {
		t, err := parseDateBound(v, false)
		if err != nil {
			return "", nil, "", errors.New("invalid joinedSince")
		}
		args = append(args, t)
		where = append(where, fmt.Sprintf("u.created_at >= $%d", len(args)))
	}
	if v := strings.TrimSpace(q.Get("joinedBefore")); v != "" {
		t, err := parseDateBound(v, true)
		if err != nil {
			return "", nil, "", errors.New("invalid joinedBefore")
		}
		args = append(args, t)
		where = append(where, fmt.Sprintf("u.created_at < $%d", len(args)))
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

	// User-app install state. "installed" / "not_installed" check
	// bot_user_installs for a live (is_installed = TRUE) row keyed on the
	// user's discord_id; users without a discord_id can never match.
	switch q.Get("userApp") {
	case "", "any":
	case "installed":
		where = append(where, `EXISTS (
			SELECT 1 FROM bot_user_installs bui
			WHERE bui.discord_id = u.discord_id
			  AND bui.is_installed = TRUE
		)`)
	case "not_installed":
		where = append(where, `NOT EXISTS (
			SELECT 1 FROM bot_user_installs bui
			WHERE bui.discord_id = u.discord_id
			  AND bui.is_installed = TRUE
		)`)
	default:
		return "", nil, "", errors.New("invalid userApp")
	}

	// Entitlement filter. Single value; matches active (non-expired) rows in
	// user_entitlements keyed on this exact key (e.g. "pro").
	if e := strings.TrimSpace(q.Get("entitlement")); e != "" {
		args = append(args, e)
		p := fmt.Sprintf("$%d", len(args))
		where = append(where, fmt.Sprintf(`EXISTS (
			SELECT 1 FROM user_entitlements ue
			WHERE ue.user_id = u.id
			  AND ue.key = %s
			  AND (ue.expires_at IS NULL OR ue.expires_at > NOW())
		)`, p))
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

// membersOnlyClause is an extra SQL fragment that, when appended via AND to
// the WHERE clause built by buildUsersQuery, restricts results to users who
// hold no admin role (i.e. no role that has any row in admin_role_permissions).
// It takes no positional arguments — safe to concatenate without shifting
// argument indices.
const membersOnlyClause = `NOT EXISTS (
    SELECT 1
    FROM admin_user_roles aur
    JOIN admin_role_permissions arp ON arp.role_id = aur.role_id
    WHERE aur.user_id = u.id
)`

// parseDateBound accepts YYYY-MM-DD or RFC3339. When the input is a plain date,
// the upperBound flag rolls it forward 24h so callers can use it as an
// exclusive upper bound covering the entire selected day.
func parseDateBound(v string, upperBound bool) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse("2006-01-02", v); err == nil {
		if upperBound {
			t = t.Add(24 * time.Hour)
		}
		return t.UTC(), nil
	}
	return time.Time{}, errors.New("invalid date")
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
