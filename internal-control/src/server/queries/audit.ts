import { db } from "@/server/db/client";
import { syncFromLocalThemisto } from "@/server/sync";

export async function listAuditLog(limit = 120) {
  await syncFromLocalThemisto();
  const { rows } = await db.query(
    `SELECT ca.*, su.name AS actor_name
     FROM control_actions ca
     LEFT JOIN staff_users su ON su.id = ca.actor_staff_id
     ORDER BY ca.created_at DESC
     LIMIT $1`,
    [limit]
  );
  return rows;
}

export async function getOverviewMetrics() {
  await syncFromLocalThemisto();
  const summaryQuery = db.query(`
    SELECT
      (SELECT COUNT(*) FROM customers) AS total_customers,
      (SELECT COUNT(*) FROM customers WHERE billing_status = 'active') AS active_customers,
      (SELECT COUNT(*) FROM organizations WHERE status = 'active') AS active_orgs,
      (SELECT COUNT(*) FROM organizations WHERE status = 'suspended') AS suspended_orgs,
      (SELECT COUNT(*) FROM organizations WHERE status = 'terminated') AS terminated_orgs,
      (SELECT COUNT(*) FROM devices WHERE status = 'active') AS active_devices,
      (SELECT COUNT(*) FROM devices WHERE status = 'disabled') AS disabled_devices,
      (SELECT COUNT(*) FROM devices WHERE status = 'revoked') AS revoked_devices,
      (SELECT COUNT(*) FROM contracts WHERE status = 'past_due') AS past_due_contracts,
      (SELECT COUNT(*) FROM deployments WHERE health = 'down') AS down_deployments,
      (SELECT COALESCE(SUM(mrr_cents), 0) FROM contracts WHERE status IN ('trial', 'active', 'past_due')) AS mrr_cents
  `);

  const recentActionsQuery = db.query(
    `SELECT date_trunc('day', created_at) AS day,
            COUNT(*) FILTER (WHERE action_type IN ('suspend', 'terminate', 'disable_device', 'revoke_device', 'revoke_all_devices')) AS critical_actions,
            COUNT(*) AS total_actions
     FROM control_actions
     WHERE created_at >= now() - interval '7 days'
     GROUP BY 1
     ORDER BY 1 ASC`
  );

  const feedQuery = db.query(
    `SELECT ca.*, su.name AS actor_name
     FROM control_actions ca
     LEFT JOIN staff_users su ON su.id = ca.actor_staff_id
     ORDER BY ca.created_at DESC
     LIMIT 12`
  );

  const orgMixQuery = db.query(
    `SELECT status, COUNT(*) AS count
     FROM organizations
     GROUP BY status
     ORDER BY status ASC`
  );

  const attentionQuery = db.query(
    `SELECT o.id, o.name, o.slug, o.status, o.status_reason,
            c.name AS customer_name, c.billing_status,
            COUNT(DISTINCT d.id) FILTER (WHERE d.status = 'active') AS active_devices,
            COUNT(DISTINCT d.id) AS device_count,
            MAX(d.last_seen_at) AS last_seen_at
     FROM organizations o
     JOIN customers c ON c.id = o.customer_id
     LEFT JOIN devices d ON d.organization_id = o.id
     WHERE o.status <> 'active' OR c.billing_status <> 'active'
     GROUP BY o.id, c.id
     ORDER BY
       CASE o.status WHEN 'suspended' THEN 0 WHEN 'terminated' THEN 1 ELSE 2 END,
       COALESCE(MAX(d.last_seen_at), o.updated_at) DESC
     LIMIT 8`
  );

  const liveRosterQuery = db.query(
    `SELECT o.id, o.name, o.slug, o.status,
            c.name AS customer_name,
            COUNT(DISTINCT d.id) FILTER (WHERE d.status = 'active') AS active_devices,
            COUNT(DISTINCT d.id) AS device_count,
            MAX(d.last_seen_at) AS last_seen_at
     FROM organizations o
     JOIN customers c ON c.id = o.customer_id
     LEFT JOIN devices d ON d.organization_id = o.id
     GROUP BY o.id, c.id
     ORDER BY COALESCE(MAX(d.last_seen_at), o.updated_at) DESC, o.name ASC
     LIMIT 8`
  );

  const [summary, recentActions, feed, orgMix, attention, liveRoster] = await Promise.all([
    summaryQuery,
    recentActionsQuery,
    feedQuery,
    orgMixQuery,
    attentionQuery,
    liveRosterQuery
  ]);

  return {
    summary: summary.rows[0],
    recentActions: recentActions.rows,
    feed: feed.rows,
    orgMix: orgMix.rows,
    attention: attention.rows,
    liveRoster: liveRoster.rows
  };
}

