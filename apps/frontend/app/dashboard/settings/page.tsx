import {
	Badge,
} from "@/components/ui/badge";
import {
	Card,
	CardContent,
	CardDescription,
	CardHeader,
	CardTitle,
} from "@/components/ui/card";
import { getCurrentUser } from "@/lib/rustlens-api";
import { requireSession } from "@/lib/session";

function getMessage(error?: string, notice?: string) {
	if (error === "csrf") {
		return { kind: "error" as const, text: "Your form expired or came from the wrong origin." };
	}
	if (notice === "discord-linked") {
		return { kind: "notice" as const, text: "Discord account connected." };
	}
	return null;
}

// Task 11 will replace this panel with a Discord-only settings view. This is
// a transitional shim that compiles after the email/connected-account model
// was removed from CurrentUser.
export default async function DashboardSettingsPage({
	searchParams,
}: {
	searchParams?: Promise<Record<string, string | string[] | undefined>>;
}) {
	await requireSession();
	const currentUser = await getCurrentUser();
	const resolvedSearchParams = (await searchParams) ?? {};
	const error = Array.isArray(resolvedSearchParams.error) ? resolvedSearchParams.error[0] : resolvedSearchParams.error;
	const notice = Array.isArray(resolvedSearchParams.notice) ? resolvedSearchParams.notice[0] : resolvedSearchParams.notice;
	const message = getMessage(error, notice);

	return (
		<Card>
			<CardHeader>
				<CardTitle>Settings</CardTitle>
				<CardDescription>
					Account preference scaffolding.
				</CardDescription>
			</CardHeader>
			<CardContent className="flex flex-col gap-4">
				{message ? (
					<Badge className="justify-center" variant={message.kind === "error" ? "destructive" : "secondary"}>
						{message.text}
					</Badge>
				) : null}
				<div className="rounded-md border px-4 py-3">
					<p className="font-medium">Discord</p>
					<p className="text-sm">
						Signed in as <span className="font-medium">@{currentUser.user.discordUsername}</span>
					</p>
					<p className="text-muted-foreground text-sm">
						Discord ID: {currentUser.user.discordId}
					</p>
				</div>
			</CardContent>
		</Card>
	);
}
