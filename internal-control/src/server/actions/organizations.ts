"use server";

import { redirect } from "next/navigation";
import { revalidatePath } from "next/cache";
import { organizationSchema, orgActionSchema } from "@/lib/zod-schemas";
import { requireRole, requireStaff } from "@/server/auth/session";
import { withTransaction } from "@/server/db/client";
import { recordAction } from "@/server/audit";

export async function createOrganizationAction(formData: FormData) {
  const staff = await requireRole(["owner", "admin", "operator"]);
  const parsed = organizationSchema.safeParse({
    customerId: formData.get("customerId"),
    name: formData.get("name"),
    slug: formData.get("slug"),
    externalOrgId: formData.get("externalOrgId"),
    region: formData.get("region"),
    environment: formData.get("environment"),
    gatewayUrl: formData.get("gatewayUrl")
  });

  if (!parsed.success) {
    throw new Error(parsed.error.issues[0]?.message ?? "Unable to create organization");
  }

  const organizationId = await withTransaction(async (client) => {
    const org = await client.query(
      `INSERT INTO organizations (customer_id, external_org_id, name, slug, status, status_changed_by, status_changed_at)
       VALUES ($1, COALESCE(NULLIF($2, '')::uuid, gen_random_uuid()), $3, $4, 'active', $5, now())
       RETURNING id`,
      [parsed.data.customerId, parsed.data.externalOrgId || null, parsed.data.name, parsed.data.slug, staff.id]
    );

    const id = org.rows[0].id as string;
    await client.query(
      `INSERT INTO deployments (organization_id, environment, region, gateway_url, health)
       VALUES ($1, $2, $3, $4, 'unknown')`,
      [id, parsed.data.environment, parsed.data.region, parsed.data.gatewayUrl || null]
    );

    await recordAction(client, {
      actorStaffId: staff.id,
      actorEmail: staff.email,
      targetType: "organization",
      targetId: id,
      actionType: "create",
      reason: "Created organization",
      metadata: { slug: parsed.data.slug, environment: parsed.data.environment }
    });
    return id;
  });

  revalidatePath("/organizations");
  revalidatePath(`/customers/${parsed.data.customerId}`);
  redirect(`/organizations/${organizationId}`);
}

export async function updateOrganizationStateAction(formData: FormData) {
  const staff = await requireStaff();
  const parsed = orgActionSchema.safeParse({
    organizationId: formData.get("organizationId"),
    action: formData.get("action"),
    reason: formData.get("reason"),
    confirmation: formData.get("confirmation")
  });

  if (!parsed.success) {
    throw new Error(parsed.error.issues[0]?.message ?? "Unable to update organization state");
  }

  const operatorActions = ["suspend", "reactivate", "revoke_all_devices"] as const;
  const adminActions = ["terminate", "mark_contract_ended"] as const;

  if (adminActions.includes(parsed.data.action as (typeof adminActions)[number])) {
    await requireRole(["owner", "admin"]);
  } else if (operatorActions.includes(parsed.data.action as (typeof operatorActions)[number])) {
    await requireRole(["owner", "admin", "operator"]);
  }

  await withTransaction(async (client) => {
    const orgResult = await client.query(
      `SELECT id, slug, customer_id, status, status_reason FROM organizations WHERE id = $1 LIMIT 1`,
      [parsed.data.organizationId]
    );
    const org = orgResult.rows[0];
    if (!org) throw new Error("Organization not found");

    if (["terminate", "revoke_all_devices"].includes(parsed.data.action) && parsed.data.confirmation !== org.slug) {
      throw new Error(`Type ${org.slug} to confirm this action.`);
    }

    if (parsed.data.action === "suspend" && org.status !== "active") {
      throw new Error("Only active organizations can be suspended.");
    }

    if (parsed.data.action === "reactivate" && org.status !== "suspended") {
      throw new Error("Only suspended organizations can be reactivated.");
    }

    if (["terminate", "revoke_all_devices", "mark_contract_ended"].includes(parsed.data.action) && org.status === "terminated") {
      throw new Error("This organization is already terminated.");
    }

    if (parsed.data.action === "suspend") {
      await client.query(
        `UPDATE organizations
         SET status = 'suspended', status_reason = $2, status_changed_by = $3, status_changed_at = now()
         WHERE id = $1`,
        [org.id, parsed.data.reason, staff.id]
      );
      await recordAction(client, {
        actorStaffId: staff.id,
        actorEmail: staff.email,
        targetType: "organization",
        targetId: org.id,
        actionType: "suspend",
        reason: parsed.data.reason,
        metadata: { previous_status: org.status }
      });
    }

    if (parsed.data.action === "reactivate") {
      await client.query(
        `UPDATE organizations
         SET status = 'active', status_reason = $2, status_changed_by = $3, status_changed_at = now()
         WHERE id = $1`,
        [org.id, parsed.data.reason, staff.id]
      );
      await recordAction(client, {
        actorStaffId: staff.id,
        actorEmail: staff.email,
        targetType: "organization",
        targetId: org.id,
        actionType: "reactivate",
        reason: parsed.data.reason,
        metadata: { previous_status: org.status }
      });
    }

    if (parsed.data.action === "terminate") {
      await client.query(
        `UPDATE organizations
         SET status = 'terminated', status_reason = $2, status_changed_by = $3, status_changed_at = now()
         WHERE id = $1`,
        [org.id, parsed.data.reason, staff.id]
      );
      await client.query(
        `UPDATE devices
         SET status = 'revoked', status_reason = $2, revoked_at = now()
         WHERE organization_id = $1 AND status <> 'revoked'`,
        [org.id, parsed.data.reason]
      );
      await client.query(
        `UPDATE deployments
         SET health = 'down', updated_at = now()
         WHERE organization_id = $1`,
        [org.id]
      );
      await recordAction(client, {
        actorStaffId: staff.id,
        actorEmail: staff.email,
        targetType: "organization",
        targetId: org.id,
        actionType: "terminate",
        reason: parsed.data.reason,
        metadata: { revoked_devices: true }
      });
    }

    if (parsed.data.action === "revoke_all_devices") {
      await client.query(
        `UPDATE devices
         SET status = 'revoked', status_reason = $2, revoked_at = now()
         WHERE organization_id = $1 AND status <> 'revoked'`,
        [org.id, parsed.data.reason]
      );
      await recordAction(client, {
        actorStaffId: staff.id,
        actorEmail: staff.email,
        targetType: "organization",
        targetId: org.id,
        actionType: "revoke_all_devices",
        reason: parsed.data.reason,
        metadata: { org_slug: org.slug }
      });
    }

    if (parsed.data.action === "mark_contract_ended") {
      await client.query(
        `UPDATE contracts
         SET status = 'ended', ends_at = now(), notes = COALESCE(notes, '') || CASE WHEN COALESCE(notes, '') = '' THEN '' ELSE E'\n' END || $2
         WHERE customer_id = $1 AND status <> 'ended'`,
        [org.customer_id, parsed.data.reason]
      );
      await recordAction(client, {
        actorStaffId: staff.id,
        actorEmail: staff.email,
        targetType: "organization",
        targetId: org.id,
        actionType: "mark_contract_ended",
        reason: parsed.data.reason,
        metadata: { customer_id: org.customer_id }
      });
    }
  });

  revalidatePath(`/organizations/${parsed.data.organizationId}`);
  revalidatePath("/organizations");
  revalidatePath("/customers");
  revalidatePath(`/organizations/${parsed.data.organizationId}/devices`);
  revalidatePath("/");
}

