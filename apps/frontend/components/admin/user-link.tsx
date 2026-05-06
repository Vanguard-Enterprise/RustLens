import Link from "next/link";

import { cn } from "@/lib/utils";

type UserLinkProps = {
	userId: string | null | undefined;
	displayName?: string | null;
	avatarUrl?: string | null;
	/** Rendered when userId is null/empty. Defaults to "System". */
	emptyLabel?: string;
	className?: string;
};

/**
 * Server component that renders an admin profile link for a user, showing
 * their friendly display name and (optionally) avatar. Falls back to a
 * truncated UUID when the display name is missing (e.g. purged users) and
 * renders a muted "System" span when there's no user ID at all.
 */
export function UserLink({
	userId,
	displayName,
	avatarUrl,
	emptyLabel = "System",
	className,
}: UserLinkProps) {
	if (!userId) {
		return (
			<span className={cn("text-muted-foreground", className)}>
				{emptyLabel}
			</span>
		);
	}

	const label =
		displayName && displayName.trim() !== ""
			? displayName
			: `${userId.slice(0, 8)}…`;

	return (
		<Link
			href={`/admin/users/${userId}`}
			title={userId}
			className={cn(
				"inline-flex items-center align-middle gap-2 text-foreground hover:underline",
				className,
			)}
		>
			{avatarUrl ? (
				// Intentionally a plain <img> to avoid next/image remote host config.
				// eslint-disable-next-line @next/next/no-img-element
				<img
					src={avatarUrl}
					alt=""
					className="block h-5 w-5 rounded-full object-cover"
				/>
			) : null}
			<span className="truncate leading-none">{label}</span>
		</Link>
	);
}
