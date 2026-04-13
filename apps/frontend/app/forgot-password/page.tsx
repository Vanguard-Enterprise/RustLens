import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Footer } from "@/components/footer";
import { Header } from "@/components/header";
import { getCsrfToken } from "@/lib/csrf";
import Link from "next/link";

function getMessage(error?: string, notice?: string) {
	if (error === "reset-request") {
		return { kind: "error" as const, text: "We couldn't create a reset link right now." };
	}
	if (error === "csrf") {
		return { kind: "error" as const, text: "Your form expired or came from the wrong origin." };
	}
	if (notice === "reset-requested") {
		return { kind: "notice" as const, text: "Password reset created. Use the preview link below in development." };
	}
	return null;
}

export default async function ForgotPasswordPage({
	searchParams,
}: {
	searchParams?: Promise<Record<string, string | string[] | undefined>>;
}) {
	const resolvedSearchParams = (await searchParams) ?? {};
	const error = Array.isArray(resolvedSearchParams.error) ? resolvedSearchParams.error[0] : resolvedSearchParams.error;
	const notice = Array.isArray(resolvedSearchParams.notice) ? resolvedSearchParams.notice[0] : resolvedSearchParams.notice;
	const preview = Array.isArray(resolvedSearchParams.preview) ? resolvedSearchParams.preview[0] : resolvedSearchParams.preview;
	const message = getMessage(error, notice);
	const csrfToken = await getCsrfToken();

	return (
		<div className="flex min-h-screen flex-col">
			<Header />
			<main className="flex flex-1 items-center justify-center px-4 py-16">
				<Card className="w-full max-w-sm">
					<CardHeader>
						<CardTitle>Forgot password</CardTitle>
					</CardHeader>
					<CardContent className="flex flex-col gap-4">
						{message ? (
							<Badge className="justify-center" variant={message.kind === "error" ? "destructive" : "secondary"}>
								{message.text}
							</Badge>
						) : null}
						<form action="/auth/password/reset/request" className="flex flex-col gap-2" method="post">
							<input name="csrfToken" type="hidden" value={csrfToken} />
							<Input name="email" placeholder="your.email@example.com" type="email" />
							<Button type="submit">Send reset link</Button>
						</form>
						{preview ? (
							<Link className="text-center text-sm text-primary underline underline-offset-4" href={preview}>
								Open development reset link
							</Link>
						) : null}
						<Link className="text-center text-sm text-muted-foreground underline underline-offset-4" href="/login">
							Back to login
						</Link>
					</CardContent>
				</Card>
			</main>
			<Footer />
		</div>
	);
}
