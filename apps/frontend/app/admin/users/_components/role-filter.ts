/**
 * Returns the role-key filter the admin users page should send to the
 * backend. Never widens the page beyond admin roles: if the URL is empty
 * or only references non-admin role keys, returns the full admin set.
 */
export function effectiveRoleFilter(
	urlRoles: string[],
	adminRoleKeys: string[],
): string[] {
	if (urlRoles.length === 0) return adminRoleKeys;
	const adminSet = new Set(adminRoleKeys);
	const intersection = urlRoles.filter((k) => adminSet.has(k));
	return intersection.length > 0 ? intersection : adminRoleKeys;
}
