import { NextResponse } from "next/server";
import { listAuditLog } from "@/server/queries/audit";
import { verifyInternalApiToken } from "@/server/auth/internal-api";
import { takeRateLimit } from "@/server/security/rate-limit";

export async function GET(request: Request) {
  const ip = request.headers.get("x-forwarded-for")?.split(",")[0]?.trim() ?? "local";
  const rateLimit = takeRateLimit(`api:audit:${ip}`, 120, 60_000);
  if (!rateLimit.allowed) {
    return NextResponse.json({ error: "Too many requests" }, { status: 429, headers: { "Retry-After": String(rateLimit.retryAfterSeconds) } });
  }

  const authHeader = request.headers.get("authorization");
  const token = authHeader?.replace(/^Bearer\s+/i, "") ?? null;
  if (!verifyInternalApiToken(token)) {
    return NextResponse.json({ error: "Unauthorized" }, { status: 401 });
  }

  const requestedLimit = Number(new URL(request.url).searchParams.get("limit") ?? "100");
  const actions = await listAuditLog(requestedLimit);
  return NextResponse.json({ actions });
}

