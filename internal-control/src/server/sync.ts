import { db, withTransaction } from "@/server/db/client";

const IMPORT_REASON_PREFIX = "Imported from local runtime";
const LOCAL_CUSTOMER_SLUG = "local-themisto-runtime";
const LEGACY_DEMO_CUSTOMER_SLUGS = ["acme-corp", "globex", "initech"];

function mapOrgStatus(status: string) {
  if (status === "suspended") return "suspended";
  if (status === "offboarded") return "terminated";
  return "active";
}

function mapDeviceStatus(status: string) {
  if (status === "active") return "active";
  if (status === "decommissioned") return "revoked";
  return "disabled";
}

function mapOperatingSystem(os: string) {
  if (os === "darwin") return "macos";
  if (os === "linux") return "linux";
  return "windows";
}

async function publicTablesAvailable() {
  const { rows } = await db.query<{ organizations: string | null; devices: string | null }>(
    `SELECT
       to_regclass('public.organizations')::text AS organizations,
       to_regclass('public.devices')::text AS devices`
  );

  return Boolean(rows[0]?.organizations && rows[0]?.devices);
}

async function pruneLegacyDemoData(client: any) {
  const customerResult = await client.query(
    `SELECT id FROM customers WHERE slug = ANY($1::text[])`,
    [LEGACY_DEMO_CUSTOMER_SLUGS]
  );
  const customerIds = customerResult.rows.map((row: { id: string }) => row.id);
  if (!customerIds.length) return;

  const organizationResult = await client.query(
    `SELECT id FROM organizations WHERE customer_id = ANY($1::uuid[])`,
    [customerIds]
  );
  const organizationIds = organizationResult.rows.map((row: { id: string }) => row.id);

  if (organizationIds.length) {
    await client.query(`DELETE FROM control_actions WHERE target_type = 'organization' AND target_id = ANY($1::uuid[])`, [organizationIds]);
    await client.query(`DELETE FROM control_actions WHERE target_type = 'device' AND target_id IN (SELECT id FROM devices WHERE organization_id = ANY($1::uuid[]))`, [organizationIds]);
    await client.query(`DELETE FROM deployments WHERE organization_id = ANY($1::uuid[])`, [organizationIds]);
    await client.query(`DELETE FROM devices WHERE organization_id = ANY($1::uuid[])`, [organizationIds]);
    await client.query(`DELETE FROM organizations WHERE id = ANY($1::uuid[])`, [organizationIds]);
  }

  await client.query(`DELETE FROM contracts WHERE customer_id = ANY($1::uuid[])`, [customerIds]);
  await client.query(`DELETE FROM control_actions WHERE target_type = 'customer' AND target_id = ANY($1::uuid[])`, [customerIds]);
  await client.query(`DELETE FROM customers WHERE id = ANY($1::uuid[])`, [customerIds]);
}

export async function syncFromLocalThemisto() {
  const available = await publicTablesAvailable();
  if (!available) {
    return { importedOrganizations: 0, importedDevices: 0, skipped: true };
  }

  const publicOrgRows = await db.query<{
    id: string;
    name: string;
    slug: string;
    status: string;
    created_at: string;
    updated_at: string;
  }>(
    `SELECT id, name, slug, status, created_at, updated_at
     FROM public.organizations
     ORDER BY created_at ASC`
  );

  const publicDeviceRows = await db.query<{
    id: string;
    org_id: string;
    device_name: string;
    os: string;
    agent_version: string | null;
    status: string;
    enrolled_at: string | null;
    created_at: string;
    updated_at: string;
  }>(
    `SELECT id, org_id, device_name, os, agent_version, status, enrolled_at, created_at, updated_at
     FROM public.devices
     ORDER BY created_at ASC`
  );

  let importedOrganizations = 0;
  let importedDevices = 0;

  await withTransaction(async (client) => {
    await pruneLegacyDemoData(client);

    const customerResult = await client.query(
      `INSERT INTO customers (name, slug, primary_contact_email, billing_status, notes)
       VALUES ('Local Themisto Runtime', $1, 'local-runtime@themisto.internal', 'active', 'Auto-generated bridge customer for the current local Themisto database.')
       ON CONFLICT (slug) DO UPDATE
       SET name = EXCLUDED.name,
           primary_contact_email = EXCLUDED.primary_contact_email,
           notes = EXCLUDED.notes
       RETURNING id`,
      [LOCAL_CUSTOMER_SLUG]
    );
    const customerId = customerResult.rows[0].id;

    const existingContract = await client.query(
      `SELECT id FROM contracts WHERE customer_id = $1 AND plan = 'internal-bridge' LIMIT 1`,
      [customerId]
    );
    if (!existingContract.rows[0]) {
      await client.query(
        `INSERT INTO contracts (customer_id, plan, seats, mrr_cents, status, notes)
         VALUES ($1, 'internal-bridge', 0, 0, 'active', 'Bridge contract for local development import.')`,
        [customerId]
      );
    }

    for (const org of publicOrgRows.rows) {
      const mappedStatus = mapOrgStatus(org.status);
      const importedReason = `${IMPORT_REASON_PREFIX}: ${org.status}`;
      const upsertedOrg = await client.query(
        `INSERT INTO organizations (
           customer_id, external_org_id, name, slug, status, status_reason, status_changed_at
         )
         VALUES ($1, $2::uuid, $3, $4, $5::org_status, $6, $7)
         ON CONFLICT (slug) DO UPDATE
         SET name = EXCLUDED.name,
             external_org_id = EXCLUDED.external_org_id,
             customer_id = EXCLUDED.customer_id,
             updated_at = now(),
             status = CASE
               WHEN organizations.status_reason IS NULL OR organizations.status_reason LIKE $8 THEN EXCLUDED.status
               ELSE organizations.status
             END,
             status_reason = CASE
               WHEN organizations.status_reason IS NULL OR organizations.status_reason LIKE $8 THEN EXCLUDED.status_reason
               ELSE organizations.status_reason
             END,
             status_changed_at = CASE
               WHEN organizations.status_reason IS NULL OR organizations.status_reason LIKE $8 THEN EXCLUDED.status_changed_at
               ELSE organizations.status_changed_at
             END
         RETURNING id`,
        [customerId, org.id, org.name, org.slug, mappedStatus, importedReason, org.updated_at, `${IMPORT_REASON_PREFIX}%`]
      );

      const internalOrgId = upsertedOrg.rows[0].id;
      importedOrganizations += 1;

      const existingDeployment = await client.query(
        `SELECT id FROM deployments WHERE organization_id = $1 AND region = 'local-dev' LIMIT 1`,
        [internalOrgId]
      );
      if (!existingDeployment.rows[0]) {
        await client.query(
          `INSERT INTO deployments (
             organization_id, environment, region, gateway_url, agent_version, last_heartbeat_at, health
           )
           VALUES ($1, 'production', 'local-dev', 'https://localhost', '0.1.0', $2, 'healthy')`,
          [internalOrgId, org.updated_at]
        );
      } else {
        await client.query(
          `UPDATE deployments
           SET last_heartbeat_at = $2,
               updated_at = now()
           WHERE id = $1`,
          [existingDeployment.rows[0].id, org.updated_at]
        );
      }
    }

    for (const device of publicDeviceRows.rows) {
      const orgLookup = await client.query(
        `SELECT id FROM organizations WHERE external_org_id = $1::uuid LIMIT 1`,
        [device.org_id]
      );
      const internalOrgId = orgLookup.rows[0]?.id;
      if (!internalOrgId) continue;

      const mappedStatus = mapDeviceStatus(device.status);
      const importedReason = `${IMPORT_REASON_PREFIX}: ${device.status}`;

      await client.query(
        `INSERT INTO devices (
           organization_id, external_device_id, hostname, os, agent_version, status, status_reason,
           last_seen_at, enrolled_at
         )
         VALUES ($1, $2::uuid, $3, $4::operating_system, $5, $6::device_status, $7, $8, $9)
         ON CONFLICT (external_device_id) DO UPDATE
         SET organization_id = EXCLUDED.organization_id,
             hostname = EXCLUDED.hostname,
             os = EXCLUDED.os,
             agent_version = EXCLUDED.agent_version,
             last_seen_at = EXCLUDED.last_seen_at,
             enrolled_at = COALESCE(EXCLUDED.enrolled_at, devices.enrolled_at),
             updated_at = now(),
             status = CASE
               WHEN devices.status_reason IS NULL OR devices.status_reason LIKE $10 THEN EXCLUDED.status
               ELSE devices.status
             END,
             status_reason = CASE
               WHEN devices.status_reason IS NULL OR devices.status_reason LIKE $10 THEN EXCLUDED.status_reason
               ELSE devices.status_reason
             END`,
        [
          internalOrgId,
          device.id,
          device.device_name,
          mapOperatingSystem(device.os),
          device.agent_version,
          mappedStatus,
          importedReason,
          device.updated_at,
          device.enrolled_at,
          `${IMPORT_REASON_PREFIX}%`
        ]
      );
      importedDevices += 1;
    }
  });

  return { importedOrganizations, importedDevices, skipped: false };
}
