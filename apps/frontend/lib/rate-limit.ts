import "server-only";

import { NextResponse } from "next/server";
import { createClient } from "redis";

type RateLimitBucket = {
	count: number;
	resetAt: number;
};

type RateLimitWindow = {
	max: number;
	windowMs: number;
};

type RateLimitResult = {
	allowed: boolean;
	retryAfterSeconds: number;
};

type RateLimitRedisClient = ReturnType<typeof createClient>;

const RATE_LIMIT_STORE_KEY = Symbol.for("rustlens.frontend.rate-limit");
const RATE_LIMIT_REDIS_KEY = Symbol.for("rustlens.frontend.rate-limit.redis");
const TRUST_PROXY_HEADERS = process.env.TRUST_PROXY_HEADERS === "true";
const TRUST_PROXY_IP_HEADER = process.env.TRUST_PROXY_IP_HEADER?.trim().toLowerCase() ?? "";
const REDIS_URL =
	process.env.RATE_LIMIT_REDIS_URL?.trim() || process.env.REDIS_URL?.trim() || "";

function getMemoryStore() {
	const globalStore = globalThis as typeof globalThis & {
		[RATE_LIMIT_STORE_KEY]?: Map<string, RateLimitBucket>;
	};

	if (!globalStore[RATE_LIMIT_STORE_KEY]) {
		globalStore[RATE_LIMIT_STORE_KEY] = new Map<string, RateLimitBucket>();
	}

	return globalStore[RATE_LIMIT_STORE_KEY];
}

function cleanupExpiredBuckets(store: Map<string, RateLimitBucket>, now: number) {
	for (const [key, bucket] of store.entries()) {
		if (bucket.resetAt <= now) {
			store.delete(key);
		}
	}
}

async function consumeMemoryRateLimit(
	key: string,
	window: RateLimitWindow,
	now = Date.now(),
): Promise<RateLimitResult> {
	const store = getMemoryStore();
	if (store.size > 5000) {
		cleanupExpiredBuckets(store, now);
	}

	const bucket = store.get(key);
	if (!bucket || bucket.resetAt <= now) {
		store.set(key, {
			count: 1,
			resetAt: now + window.windowMs,
		});
		return { allowed: true, retryAfterSeconds: 0 };
	}

	if (bucket.count >= window.max) {
		return {
			allowed: false,
			retryAfterSeconds: Math.max(1, Math.ceil((bucket.resetAt - now) / 1000)),
		};
	}

	bucket.count += 1;
	return { allowed: true, retryAfterSeconds: 0 };
}

async function getRedisClient() {
	if (!REDIS_URL) {
		if (process.env.NODE_ENV === "production") {
			throw new Error(
				"RATE_LIMIT_REDIS_URL or REDIS_URL must be configured in production for shared auth rate limiting",
			);
		}
		return null;
	}

	const globalStore = globalThis as typeof globalThis & {
		[RATE_LIMIT_REDIS_KEY]?: Promise<RateLimitRedisClient>;
	};

	if (!globalStore[RATE_LIMIT_REDIS_KEY]) {
		globalStore[RATE_LIMIT_REDIS_KEY] = (async () => {
			const client = createClient({
				url: REDIS_URL,
			});
			client.on("error", (error) => {
				console.error("rate limit redis client error", error);
			});
			await client.connect();
			return client;
		})();
	}

	return globalStore[RATE_LIMIT_REDIS_KEY];
}

async function consumeRedisRateLimit(
	key: string,
	window: RateLimitWindow,
): Promise<RateLimitResult> {
	const client = await getRedisClient();
	if (!client) {
		return consumeMemoryRateLimit(key, window);
	}

	const redisKey = `rate-limit:${key}`;
	const ttlSeconds = Math.max(1, Math.ceil(window.windowMs / 1000));
	const count = await client.incr(redisKey);
	if (count === 1) {
		await client.expire(redisKey, ttlSeconds);
	}

	const ttl = await client.ttl(redisKey);
	if (count > window.max) {
		return {
			allowed: false,
			retryAfterSeconds: ttl > 0 ? ttl : ttlSeconds,
		};
	}

	return { allowed: true, retryAfterSeconds: 0 };
}

export function getClientAddress(request: Request) {
	if (!TRUST_PROXY_HEADERS || !TRUST_PROXY_IP_HEADER) {
		return "unknown";
	}

	const clientIP = request.headers.get(TRUST_PROXY_IP_HEADER)?.trim();
	if (!clientIP) {
		return "unknown";
	}

	return clientIP;
}

export async function consumeRateLimit(
	key: string,
	window: RateLimitWindow,
): Promise<RateLimitResult> {
	return consumeRedisRateLimit(key, window);
}

export function redirectRateLimited(request: Request, location: string, retryAfterSeconds: number) {
	const response = NextResponse.redirect(new URL(location, request.url), {
		status: 303,
	});
	response.headers.set("Retry-After", String(retryAfterSeconds));
	return response;
}
