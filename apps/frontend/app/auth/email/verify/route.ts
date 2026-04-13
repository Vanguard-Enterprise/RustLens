import { NextResponse } from "next/server";
import { consumeEmailVerification } from "@/lib/rustlens-api";

export async function GET(request: Request) {
	const token = new URL(request.url).searchParams.get("token");
	if (!token) {
		return NextResponse.redirect(new URL("/login?error=verify-invalid", request.url), {
			status: 303,
		});
	}

	try {
		await consumeEmailVerification(token);
		return NextResponse.redirect(new URL("/login?notice=email-verified", request.url), {
			status: 303,
		});
	} catch {
		return NextResponse.redirect(new URL("/login?error=verify-invalid", request.url), {
			status: 303,
		});
	}
}
