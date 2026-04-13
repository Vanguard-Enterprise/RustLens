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
import { Button } from "@/components/ui/button";
import { getCsrfToken } from "@/lib/csrf";
import { getCurrentUser } from "@/lib/rustlens-api";
import { requireSession } from "@/lib/session";
import Link from "next/link";

function getMessage(error?: string, notice?: string) {
	if (error === "verify-request") {
		return { kind: "error" as const, text: "We couldn't create an email verification link." };
	}
	if (error === "email-already-verified") {
		return { kind: "notice" as const, text: "Your email is already verified." };
	}
	if (error === "csrf") {
		return { kind: "error" as const, text: "Your form expired or came from the wrong origin." };
	}
	if (notice === "registered") {
		return { kind: "notice" as const, text: "Account created. Verify your email when you're ready." };
	}
	if (notice === "email-verify-requested") {
		return { kind: "notice" as const, text: "Verification link created. Use the development preview below." };
	}
	if (notice === "email-verified") {
		return { kind: "notice" as const, text: "Email verified." };
	}
	if (notice === "password-reset") {
		return { kind: "notice" as const, text: "Password updated." };
	}
	return null;
}

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
	const preview = Array.isArray(resolvedSearchParams.preview) ? resolvedSearchParams.preview[0] : resolvedSearchParams.preview;
	const message = getMessage(error, notice);
	const csrfToken = await getCsrfToken();

	return (
		<Card>
			<CardHeader>
				<CardTitle>Settings</CardTitle>
				<CardDescription>
					Account verification and preference scaffolding.
				</CardDescription>
			</CardHeader>
			<CardContent className="flex flex-col gap-4">
				{message ? (
					<Badge className="justify-center" variant={message.kind === "error" ? "destructive" : "secondary"}>
						{message.text}
					</Badge>
				) : null}
				<div className="rounded-md border px-4 py-3">
					<p className="font-medium">Primary email</p>
					<p className="text-muted-foreground text-sm">
						{currentUser.primaryEmail?.email ?? "No email linked"}
					</p>
					<p className="mt-1 text-sm">
						Status: {currentUser.primaryEmail?.isVerified ? "Verified" : "Unverified"}
					</p>
				</div>
				{!currentUser.primaryEmail?.isVerified && currentUser.primaryEmail ? (
					<form action="/auth/email/verify/request" method="post">
						<input name="csrfToken" type="hidden" value={csrfToken} />
						<Button type="submit">Send verification link</Button>
					</form>
				) : null}
				{preview ? (
					<Link className="text-sm text-primary underline underline-offset-4" href={preview}>
						Open development verification link
					</Link>
				) : null}
			</CardContent>
		</Card>
	);
}
