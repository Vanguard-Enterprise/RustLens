import { NextResponse } from "next/server";

import { routeHitCounter } from "@/lib/observability/metrics";

const ROUTE = "/api/healthz";

export async function GET() {
    routeHitCounter.add(1, { route: ROUTE });
    return NextResponse.json({ status: "ok" });
}
