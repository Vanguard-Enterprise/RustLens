import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Footer } from "@/components/footer";
import { Header } from "@/components/header";
import { getCsrfToken } from "@/lib/csrf";
import Link from "next/link";

function getMessage(error?: string) {
	switch (error) {
		case "reset-password":
			return "Password must be at least 12 characters.";
		case "reset-invalid":
			return "That password reset link is invalid or expired.";
		case "csrf":
			return "Your form expired or came from the wrong origin.";
		default:
			return "";
	}
}

export default async function ResetPasswordPage({
	searchParams,
}: {
	searchParams?: Promise<Record<string, string | string[] | undefined>>;
}) {
	const resolvedSearchParams = (await searchParams) ?? {};
	const token = Array.isArray(resolvedSearchParams.token) ? resolvedSearchParams.token[0] : resolvedSearchParams.token;
	const error = Array.isArray(resolvedSearchParams.error) ? resolvedSearchParams.error[0] : resolvedSearchParams.error;
	const message = getMessage(error);
	const csrfToken = await getCsrfToken();

	return (
		<div className="flex min-h-screen flex-col">
			<Header />
			<main className="flex flex-1 items-center justify-center px-4 py-16">
				<Card className="w-full max-w-sm">
					<CardHeader>
						<CardTitle>Reset password</CardTitle>
					</CardHeader>
					<CardContent className="flex flex-col gap-4">
						{message ? (
							<Badge className="justify-center" variant="destructive">
								{message}
							</Badge>
						) : null}
						{token ? (
							<form action="/auth/password/reset/consume" className="flex flex-col gap-2" method="post">
								<input name="csrfToken" type="hidden" value={csrfToken} />
								<input name="token" type="hidden" value={token} />
								<Input name="password" placeholder="New password" type="password" />
								<Button type="submit">Update password</Button>
							</form>
						) : (
							<p className="text-muted-foreground text-sm">
								A valid reset token is required to update your password.
							</p>
						)}
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
