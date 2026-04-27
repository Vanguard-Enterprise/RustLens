import { Footer } from "@/components/footer";
import { Header } from "@/components/header";
import { NotFoundPage } from "@/components/not-found";
import { getCsrfToken } from "@/lib/csrf";
import { getSession } from "@/lib/session";

export default async function NotFound() {
	const session = await getSession();
	const csrfToken = session ? await getCsrfToken() : undefined;
	const user = session
		? {
				displayName: session.displayName,
				avatarUrl: session.avatarUrl,
				isInternalAdmin:
					(session.adminRoles?.length ?? 0) > 0 ||
					session.roles.includes("internal_admin"),
			}
		: null;

	return (
		<div className="flex min-h-screen flex-col">
			<Header csrfToken={csrfToken} user={user} />
			<main className="flex flex-1">
				<NotFoundPage />
			</main>
			<Footer />
		</div>
	);
}
