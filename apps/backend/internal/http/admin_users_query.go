package httpapi

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// buildUsersQuery constructs the shared WHERE clause, positional args, and
// ORDER BY for the admin users list + export endpoints. It mirrors the filter
// semantics documented on ListAdminUsers.
//
// The returned args are positional — any caller must append LIMIT/OFFSET args
// after those, and reference them as $(len(args)-1) / $(len(args)) in the
// final SQL.
//
// If an "ids" query value is present (comma-separated), it overrides every
// other predicate (q, role, status, connection filters, emailVerified) and
// scopes the selection to exactly those app_users.id values. This is used by
// the bulk toolbar "Export selected" flow.
func buildUsersQuery(q url.Values) (string, []any, string, error) {
	args := []any{}
	where := []string{}

	if ids := strings.TrimSpace(q.Get("ids")); ids != "" {
		parts := strings.Split(ids, ",")
		cleaned := make([]string, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				cleaned = append(cleaned, p)
			}
		}
		if len(cleaned) == 0 {
			return "", nil, "", errors.New("ids must contain at least one id")
		}
		args = append(args, cleaned)
		p := fmt.Sprintf("$%d", len(args))
		where = append(where, fmt.Sprintf(`u.id = ANY(%s::text[])`, p))
		// Still honour sort but ignore other filters.
		orderBy := orderByClause(q)
		return "WHERE " + strings.Join(where, " AND "), args, orderBy, nil
	}

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
			OR EXISTS (SELECT 1 FROM user_emails e WHERE e.user_id = u.id AND e.email ILIKE %[1]s)
			OR EXISTS (SELECT 1 FROM user_connected_accounts d WHERE d.user_id = u.id AND d.provider='discord' AND (d.username ILIKE %[1]s OR d.provider_account_id ILIKE %[1]s))
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

	switch q.Get("discord") {
	case "", "any":
	case "linked":
		where = append(where, `EXISTS (SELECT 1 FROM user_connected_accounts d WHERE d.user_id = u.id AND d.provider='discord')`)
	case "unlinked":
		where = append(where, `NOT EXISTS (SELECT 1 FROM user_connected_accounts d WHERE d.user_id = u.id AND d.provider='discord')`)
	default:
		return "", nil, "", errors.New("invalid discord")
	}

	switch q.Get("steam") {
	case "", "any":
	case "linked":
		where = append(where, `EXISTS (SELECT 1 FROM user_connected_accounts s WHERE s.user_id = u.id AND s.provider='steam')`)
	case "unlinked":
		where = append(where, `NOT EXISTS (SELECT 1 FROM user_connected_accounts s WHERE s.user_id = u.id AND s.provider='steam')`)
	default:
		return "", nil, "", errors.New("invalid steam")
	}

	switch q.Get("emailVerified") {
	case "", "any":
	case "yes":
		where = append(where, `EXISTS (SELECT 1 FROM user_emails e WHERE e.user_id = u.id AND e.is_primary=true AND e.is_verified=true)`)
	case "no":
		where = append(where, `EXISTS (SELECT 1 FROM user_emails e WHERE e.user_id = u.id AND e.is_primary=true AND e.is_verified=false)`)
	default:
		return "", nil, "", errors.New("invalid emailVerified")
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
