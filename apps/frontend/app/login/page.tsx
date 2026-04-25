import { AuthPage } from "@/components/auth-page";
import { getCsrfToken } from "@/lib/csrf";

export default async function LoginPage({
	searchParams,
}: {
	searchParams?: Promise<Record<string, string | string[] | undefined>>;
}) {
	const resolvedSearchParams = (await searchParams) ?? {};
	const error = Array.isArray(resolvedSearchParams.error)
		? resolvedSearchParams.error[0]
		: resolvedSearchParams.error;
	const notice = Array.isArray(resolvedSearchParams.notice)
		? resolvedSearchParams.notice[0]
		: resolvedSearchParams.notice;
	const csrfToken = await getCsrfToken();

	return (
		<AuthPage csrfToken={csrfToken} error={error} notice={notice} />
	);
}
