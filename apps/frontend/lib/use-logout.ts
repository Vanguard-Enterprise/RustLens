"use client";

import { usePathname, useRouter } from "next/navigation";
import { useCallback, useTransition } from "react";

const GATED_PREFIXES = ["/admin", "/dashboard"];

export function useLogout(csrfToken: string) {
	const router = useRouter();
	const pathname = usePathname();
	const [isLoggingOut, startLogout] = useTransition();

	const logout = useCallback(() => {
		startLogout(async () => {
			const formData = new FormData();
			formData.append("csrfToken", csrfToken);
			try {
				await fetch("/logout", {
					method: "POST",
					body: formData,
				});
			} catch {
				// Network failure: still refresh/redirect so the UI doesn't keep
				// rendering logged-in state. The cookie may or may not be cleared,
				// but the next server render will surface the actual auth state.
			}
			const onGated = GATED_PREFIXES.some((prefix) =>
				pathname === prefix || pathname.startsWith(`${prefix}/`),
			);
			if (onGated) {
				router.replace("/");
			}
			router.refresh();
		});
	}, [csrfToken, pathname, router]);

	return { logout, isLoggingOut };
}
