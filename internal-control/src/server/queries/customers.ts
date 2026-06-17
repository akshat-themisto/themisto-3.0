import { db } from "@/server/db/client";
import { syncFromLocalThemisto } from "@/server/sync";

export async function listCustomers() {
  await syncFromLocalThemisto();
  const { rows } = await db.query(
    `SELECT c.id, c.name, c.slug, c.primary_contact_email, c.billing_status, c.notes,
            c.created_at, c.updated_at,
            COUNT(DISTINCT o.id) AS organization_count,
            COUNT(DISTINCT d.id) FILTER (WHERE d.status = 'active') AS active_device_count,
            COALESCE(SUM(ct.mrr_cents) FILTER (WHERE ct.status IN ('trial', 'active', 'past_due')), 0) AS mrr_cents,
            MAX(d.last_seen_at) AS last_activity_at
     FROM customers c
     LEFT JOIN organizations o ON o.customer_id = c.id
     LEFT JOIN devices d ON d.organization_id = o.id
     LEFT JOIN contracts ct ON ct.customer_id = c.id
     GROUP BY c.id
     ORDER BY MAX(d.last_seen_at) DESC NULLS LAST, c.updated_at DESC, c.name ASC`
  );
  return rows;
}

export async function getCustomerDetail(id: string) {
  await syncFromLocalThemisto();
  const customerQuery = db.query(
    `SELECT c.*, 
            COUNT(DISTINCT o.id) AS organization_count,
            COUNT(DISTINCT d.id) FILTER (WHERE d.status = 'active') AS active_device_count,
            COUNT(DISTINCT d.id) FILTER (WHERE d.status = 'revoked') AS revoked_device_count,
            MAX(d.last_seen_at) AS last_activity_at
     FROM customers c
     LEFT JOIN organizations o ON o.customer_id = c.id
     LEFT JOIN devices d ON d.organization_id = o.id
     WHERE c.id = $1
     GROUP BY c.id`,
    [id]
  );

  const orgsQuery = db.query(
    `SELECT o.id, o.name, o.slug, o.status, o.status_reason, o.created_at,
            COUNT(d.id) AS device_count,
            MAX(d.last_seen_at) AS last_seen_at
     FROM organizations o
     LEFT JOIN devices d ON d.organization_id = o.id
     WHERE o.customer_id = $1
     GROUP BY o.id
     ORDER BY o.name`,
    [id]
  );

  const contractsQuery = db.query(
    `SELECT * FROM contracts WHERE customer_id = $1 ORDER BY starts_at DESC`,
    [id]
  );

  const actionsQuery = db.query(
    `SELECT * FROM control_actions
     WHERE (target_type = 'customer' AND target_id = $1)
        OR target_id IN (SELECT id FROM organizations WHERE customer_id = $1)
     ORDER BY created_at DESC
     LIMIT 20`,
    [id]
  );

  const [customer, orgs, contracts, actions] = await Promise.all([customerQuery, orgsQuery, contractsQuery, actionsQuery]);
  return {
    customer: customer.rows[0] ?? null,
    organizations: orgs.rows,
    contracts: contracts.rows,
    actions: actions.rows
  };
}

