import { db } from "@/server/db/client";
import { syncFromLocalThemisto } from "@/server/sync";

export async function listDevicesForOrganization(organizationId: string) {
  await syncFromLocalThemisto();
  const { rows } = await db.query(
    `SELECT d.*, o.name AS organization_name, o.slug AS organization_slug
     FROM devices d
     JOIN organizations o ON o.id = d.organization_id
     WHERE d.organization_id = $1
     ORDER BY d.last_seen_at DESC NULLS LAST, d.hostname ASC`,
    [organizationId]
  );
  return rows;
}

export async function getDeviceStatusByExternalId(externalDeviceId: string) {
  await syncFromLocalThemisto();
  const { rows } = await db.query(
    `SELECT d.status, d.status_reason, d.revoked_at, d.disabled_at, o.status AS organization_status
     FROM devices d
     JOIN organizations o ON o.id = d.organization_id
     WHERE d.external_device_id = $1::uuid
     LIMIT 1`,
    [externalDeviceId]
  );
  return rows[0] ?? null;
}

