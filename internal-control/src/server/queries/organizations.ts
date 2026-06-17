import { db } from "@/server/db/client";
import { syncFromLocalThemisto } from "@/server/sync";

export async function listOrganizations(filters?: { search?: string; status?: string }) {
  await syncFromLocalThemisto();
  const params: any[] = [];
  const where: string[] = [];

  if (filters?.status && filters.status !== "all") {
    params.push(filters.status);
    where.push(`o.status = $${params.length}`);
  }

  if (filters?.search) {
    params.push(`%${filters.search.toLowerCase()}%`);
    where.push(`(LOWER(o.name) LIKE $${params.length} OR LOWER(o.slug) LIKE $${params.length} OR LOWER(c.name) LIKE $${params.length})`);
  }

  const { rows } = await db.query(
    `SELECT o.id, o.name, o.slug, o.external_org_id, o.status, o.status_reason, o.updated_at,
            c.name AS customer_name, c.id AS customer_id,
            COUNT(DISTINCT d.id) AS device_count,
            COUNT(DISTINCT d.id) FILTER (WHERE d.status = 'active') AS active_devices,
            COUNT(DISTINCT d.id) FILTER (WHERE d.status = 'revoked') AS revoked_devices,
            MAX(d.last_seen_at) AS last_seen_at,
            COALESCE(MAX(dep.health::text), 'unknown') AS health,
            COALESCE(MAX(dep.environment::text), 'production') AS environment
     FROM organizations o
     JOIN customers c ON c.id = o.customer_id
     LEFT JOIN devices d ON d.organization_id = o.id
     LEFT JOIN deployments dep ON dep.organization_id = o.id
     ${where.length ? `WHERE ${where.join(" AND ")}` : ""}
     GROUP BY o.id, c.id
     ORDER BY
       CASE o.status WHEN 'suspended' THEN 0 WHEN 'terminated' THEN 1 ELSE 2 END,
       COALESCE(MAX(d.last_seen_at), o.updated_at) DESC,
       o.name ASC`,
    params
  );
  return rows;
}

export async function getOrganizationDetail(id: string) {
  await syncFromLocalThemisto();
  const orgQuery = db.query(
    `SELECT o.*, c.name AS customer_name, c.id AS customer_id, c.billing_status,
            su.name AS status_changed_by_name,
            COUNT(DISTINCT d.id) AS device_count,
            COUNT(DISTINCT d.id) FILTER (WHERE d.status = 'active') AS active_devices,
            COUNT(DISTINCT d.id) FILTER (WHERE d.status = 'revoked') AS revoked_devices,
            MAX(d.last_seen_at) AS last_seen_at,
            MAX(dep.last_heartbeat_at) AS last_heartbeat_at,
            COALESCE(MAX(dep.health::text), 'unknown') AS deployment_health,
            COALESCE(MAX(dep.environment::text), 'production') AS deployment_environment,
            MAX(dep.gateway_url) AS gateway_url,
            MAX(dep.agent_version) AS deployment_agent_version
     FROM organizations o
     JOIN customers c ON c.id = o.customer_id
     LEFT JOIN staff_users su ON su.id = o.status_changed_by
     LEFT JOIN devices d ON d.organization_id = o.id
     LEFT JOIN deployments dep ON dep.organization_id = o.id
     WHERE o.id = $1
     GROUP BY o.id, c.id, su.id`,
    [id]
  );

  const devicesQuery = db.query(
    `SELECT id, external_device_id, hostname, os, agent_version, status, status_reason,
            disabled_at, revoked_at, last_seen_at, enrolled_at
     FROM devices
     WHERE organization_id = $1
     ORDER BY last_seen_at DESC NULLS LAST, hostname ASC
     LIMIT 20`,
    [id]
  );

  const actionsQuery = db.query(
    `SELECT ca.*, su.name AS actor_name
     FROM control_actions ca
     LEFT JOIN staff_users su ON su.id = ca.actor_staff_id
     WHERE ca.target_id = $1
     ORDER BY ca.created_at DESC
     LIMIT 50`,
    [id]
  );

  const deploymentsQuery = db.query(
    `SELECT * FROM deployments WHERE organization_id = $1 ORDER BY created_at DESC`,
    [id]
  );

  const [org, devices, actions, deployments] = await Promise.all([orgQuery, devicesQuery, actionsQuery, deploymentsQuery]);
  return {
    organization: org.rows[0] ?? null,
    devices: devices.rows,
    actions: actions.rows,
    deployments: deployments.rows
  };
}

export async function getOrganizationStatusByExternalId(externalOrgId: string) {
  await syncFromLocalThemisto();
  const { rows } = await db.query(
    `SELECT o.id, o.external_org_id, o.status, o.status_reason, o.status_changed_at
     FROM organizations o
     WHERE o.external_org_id = $1::uuid
     LIMIT 1`,
    [externalOrgId]
  );
  return rows[0] ?? null;
}

