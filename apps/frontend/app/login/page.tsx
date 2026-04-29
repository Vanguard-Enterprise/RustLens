import { redirect } from "next/navigation";
import { AuthPage } from "@/components/auth-page";
import { getCsrfToken } from "@/lib/csrf";
import { clearSession, getSession } from "@/lib/session";

type LoginPageProps = {
	searchParams: Promise<{ error?: string | string[]; notice?: string | string[] }>;
};

function firstParam(value: string | string[] | undefined) {
	return Array.isArray(value) ? value[0] : value;
}

export default async function LoginPage({ searchParams }: LoginPageProps) {
	const params = await searchParams;
	const error = firstParam(params.error);
	const notice = firstParam(params.notice);

	// "session" means the backend rejected our cookie (revoked, or pointing at
	// a user that no longer exists). Clear it so the OAuth flow won't bounce
	// back via the "already signed in" path.
	if (error === "session") {
		await clearSession();
	} else if (!error && !notice) {
		const session = await getSession();
		if (session) {
			redirect("/dashboard");
		}
		redirect("/api/auth/discord");
	}

	const csrfToken = await getCsrfToken();
	return <AuthPage error={error} notice={notice} csrfToken={csrfToken} />;
}
