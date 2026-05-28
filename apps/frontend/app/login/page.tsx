import type { Metadata } from "next";
import { redirect } from "next/navigation";
import { AuthPage } from "@/components/auth-page";
import { getCsrfToken } from "@/lib/csrf";
import { clearSession, getSession } from "@/lib/session";
import { type SessionRevokedNotice } from "@/lib/session-errors";

export const metadata: Metadata = { title: "Login" };
import { NoticeToast } from "./notice-toast";

type LoginPageProps = {
	searchParams: Promise<{ error?: string | string[]; notice?: string | string[] }>;
};

// Allow-list of accepted ?notice= slugs. Anything else is dropped — the slot
// previously rendered any string as a banner, which is a phishing-friendly
// content-spoofing vector (e.g. /login?notice=Your%20password%20expired,
// %20call%20+1-555-XXXX).
const ALLOWED_NOTICES = new Set<SessionRevokedNotice>([
	"force_logout",
	"suspended",
	"deleted",
	"expired",
	"connection_lost",
	"unknown",
]);

function firstParam(value: string | string[] | undefined) {
	return Array.isArray(value) ? value[0] : value;
}

function sanitizeNotice(raw: string | undefined): SessionRevokedNotice | undefined {
	if (!raw) return undefined;
	return ALLOWED_NOTICES.has(raw as SessionRevokedNotice)
		? (raw as SessionRevokedNotice)
		: undefined;
}

export default async function LoginPage({ searchParams }: LoginPageProps) {
	const params = await searchParams;
	const error = firstParam(params.error);
	const notice = sanitizeNotice(firstParam(params.notice));

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
	return (
		<>
			<NoticeToast notice={notice} />
			<AuthPage error={error} notice={notice} csrfToken={csrfToken} />
		</>
	);
}
