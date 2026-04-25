import { redirect } from "next/navigation";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
	Card,
	CardContent,
	CardHeader,
	CardTitle,
} from "@/components/ui/card";
import { CSRF_FIELD_NAME, getCsrfToken } from "@/lib/csrf";
import { getCurrentUserSafe } from "@/lib/rustlens-api";

function discordAvatarUrl(discordId: string, hash?: string): string | undefined {
	if (!hash) return undefined;
	return `https://cdn.discordapp.com/avatars/${discordId}/${hash}.png`;
}

function getMessage(error?: string) {
	if (error === "csrf") {
		return {
			kind: "error" as const,
			text: "Your form expired or came from the wrong origin.",
		};
	}
	return null;
}

export default async function DashboardSettingsPage({
	searchParams,
}: {
	searchParams?: Promise<Record<string, string | string[] | undefined>>;
}) {
	const [currentUser, csrfToken] = await Promise.all([
		getCurrentUserSafe(),
		getCsrfToken(),
	]);

	if (!currentUser) {
		redirect("/logout?reason=session");
	}

	const resolvedSearchParams = (await searchParams) ?? {};
	const error = Array.isArray(resolvedSearchParams.error)
		? resolvedSearchParams.error[0]
		: resolvedSearchParams.error;
	const message = getMessage(error);

	const u = currentUser.user;
	const cdnAvatar = discordAvatarUrl(u.discordId, u.discordAvatar);
	const avatarSrc = cdnAvatar ?? u.avatarUrl;
	const fallback = (u.displayName || u.discordUsername).charAt(0).toUpperCase();
	const displayLine =
		u.discordGlobalName && u.discordGlobalName !== u.discordUsername
			? `${u.discordGlobalName} — @${u.discordUsername}`
			: `@${u.discordUsername}`;

	return (
		<main className="grid gap-6">
			<h1 className="font-semibold text-2xl">Settings</h1>

			{message ? (
				<Badge
					className="justify-center"
					variant={message.kind === "error" ? "destructive" : "secondary"}
				>
					{message.text}
				</Badge>
			) : null}

			<Card>
				<CardHeader>
					<CardTitle>Account</CardTitle>
				</CardHeader>
				<CardContent className="flex items-center gap-4">
					<Avatar className="size-16">
						{avatarSrc ? (
							<AvatarImage src={avatarSrc} alt={u.displayName} />
						) : null}
						<AvatarFallback>{fallback}</AvatarFallback>
					</Avatar>
					<div className="flex-1">
						<div className="font-semibold text-lg">
							{u.displayName || u.discordUsername}
						</div>
						<div className="text-muted-foreground text-sm">{displayLine}</div>
						<div className="mt-1 text-muted-foreground text-xs">
							Signed in via Discord
						</div>
					</div>
				</CardContent>
			</Card>

			<Card>
				<CardHeader>
					<CardTitle>Sign out</CardTitle>
				</CardHeader>
				<CardContent>
					<form action="/logout" method="post">
						<input name={CSRF_FIELD_NAME} type="hidden" value={csrfToken} />
						<Button type="submit" variant="destructive">
							Sign out
						</Button>
					</form>
				</CardContent>
			</Card>
		</main>
	);
}
