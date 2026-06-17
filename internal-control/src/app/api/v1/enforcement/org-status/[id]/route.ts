import { NextResponse } from "next/server";
import { getOrganizationStatusByExternalId } from "@/server/queries/organizations";
import { verifyInternalApiToken } from "@/server/auth/internal-api";
import { takeRateLimit } from "@/server/security/rate-limit";

export async function GET(request: Request, { params }: { params: Promise<{ id: string }> }) {
  const ip = request.headers.get("x-forwarded-for")?.split(",")[0]?.trim() ?? "local";
  const limit = takeRateLimit(`api:org-status:${ip}`, 240, 60_000);
  if (!limit.allowed) {
    return NextResponse.json({ error: "Too many requests" }, { status: 429, headers: { "Retry-After": String(limit.retryAfterSeconds) } });
  }

  const authHeader = request.headers.get("authorization");
  const token = authHeader?.replace(/^Bearer\s+/i, "") ?? null;
  if (!verifyInternalApiToken(token)) {
    return NextResponse.json({ error: "Unauthorized" }, { status: 401 });
  }

  const { id } = await params;
  const organization = await getOrganizationStatusByExternalId(id);
  if (!organization) {
    return NextResponse.json({ error: "Organization not found" }, { status: 404 });
  }

  return NextResponse.json({
    organizationId: organization.id,
    externalOrgId: organization.external_org_id,
    status: organization.status,
    reason: organization.status_reason,
    changedAt: organization.status_changed_at
  });
}

