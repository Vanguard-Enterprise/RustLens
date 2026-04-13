import "server-only";

function normalizeOrigin(value: string, envName: string) {
	const trimmed = value.trim().replace(/\/+$/, "");
	let parsed: URL;
	try {
		parsed = new URL(trimmed);
	} catch {
		throw new Error(`${envName} must be a valid absolute URL`);
	}

	if (parsed.protocol !== "http:" && parsed.protocol !== "https:") {
		throw new Error(`${envName} must use http or https`);
	}

	return parsed.origin;
}

export function getAppOrigin() {
	const configuredOrigin =
		process.env.APP_ORIGIN ?? process.env.NEXT_PUBLIC_APP_URL;
	if (configuredOrigin) {
		return normalizeOrigin(
			configuredOrigin,
			process.env.APP_ORIGIN ? "APP_ORIGIN" : "NEXT_PUBLIC_APP_URL",
		);
	}

	throw new Error(
		"APP_ORIGIN or NEXT_PUBLIC_APP_URL must be configured for auth flows",
	);
}
