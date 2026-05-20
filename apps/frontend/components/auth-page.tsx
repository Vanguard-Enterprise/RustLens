import { IconBrandDiscordFilled } from "@tabler/icons-react";
import { Button } from "@/components/ui/button";

type AuthPageProps = {
	error?: string;
	notice?: string;
	csrfToken: string;
};

const NOTICE_BANNER: Record<string, string> = {
	force_logout: "You were signed out by an administrator.",
	suspended: "Your account has been suspended.",
	deleted: "Your account has been deleted.",
	expired: "Your session expired, please sign in again.",
	connection_lost: "Lost connection to RustLens. Please sign in again.",
	unknown: "Your session ended. Please sign in again.",
};

function getNoticeMessage(notice?: string): string | null {
	if (!notice) return null;
	return NOTICE_BANNER[notice] ?? null;
}

function getErrorMessage(error?: string): string | null {
	switch (error) {
		case "cancelled":
			return "Sign-in cancelled.";
		case "unavailable":
			return "Discord sign-in is temporarily unavailable, try again.";
		case "disabled":
			return "Account disabled.";
		case "discord-config":
			return "Discord OAuth is not configured yet.";
		case "discord-state":
			return "The Discord sign-in state was invalid or expired.";
		case "discord-token":
		case "discord-profile":
		case "discord-auth":
			return "Discord sign-in failed. Try again.";
		case "discord-already-signed-in":
			return "You're already signed in.";
		default:
			return error ? "Sign-in failed. Try again." : null;
	}
}

export function AuthPage({ error, notice, csrfToken }: AuthPageProps) {
	const errorMessage = getErrorMessage(error);
	const noticeMessage = getNoticeMessage(notice);
	return (
		<main className="flex min-h-screen flex-col items-center justify-center gap-6 p-6">
			<h1 className="text-2xl font-semibold">Sign in</h1>
			{errorMessage ? (
				<div role="alert" className="rounded-md bg-destructive/10 px-4 py-2 text-sm text-destructive">
					{errorMessage}
				</div>
			) : null}
			{noticeMessage ? (
				<div role="status" className="rounded-md bg-muted px-4 py-2 text-sm">
					{noticeMessage}
				</div>
			) : null}
			<Button asChild size="lg">
				<a href={`/api/auth/discord?csrf=${encodeURIComponent(csrfToken)}`}>
					<IconBrandDiscordFilled className="mr-2 size-5" />
					Sign in with Discord
				</a>
			</Button>
		</main>
	);
}
