"use server";

import { redirect } from "next/navigation";
import { revalidatePath } from "next/cache";
import { customerSchema, reasonSchema } from "@/lib/zod-schemas";
import { requireRole } from "@/server/auth/session";
import { withTransaction } from "@/server/db/client";
import { recordAction } from "@/server/audit";

export async function createCustomerAction(formData: FormData) {
  const staff = await requireRole(["owner", "admin", "operator"]);
  const parsed = customerSchema.safeParse({
    name: formData.get("name"),
    slug: formData.get("slug"),
    primaryContactEmail: formData.get("primaryContactEmail"),
    plan: formData.get("plan"),
    seats: formData.get("seats"),
    mrrCents: formData.get("mrrCents"),
    notes: formData.get("notes")
  });

  if (!parsed.success) {
    throw new Error(parsed.error.issues[0]?.message ?? "Unable to create customer");
  }

  const customerId = await withTransaction(async (client) => {
    const customer = await client.query(
      `INSERT INTO customers (name, slug, primary_contact_email, notes)
       VALUES ($1, $2, $3, $4)
       RETURNING id`,
      [
        parsed.data.name,
        parsed.data.slug,
        parsed.data.primaryContactEmail || null,
        parsed.data.notes || null
      ]
    );

    const id = customer.rows[0].id as string;
    await client.query(
      `INSERT INTO contracts (customer_id, plan, seats, mrr_cents, status, notes)
       VALUES ($1, $2, $3, $4, 'active', $5)`,
      [id, parsed.data.plan, parsed.data.seats, parsed.data.mrrCents, parsed.data.notes || null]
    );

    await recordAction(client, {
      actorStaffId: staff.id,
      actorEmail: staff.email,
      targetType: "customer",
      targetId: id,
      actionType: "create",
      reason: "Created customer workspace",
      metadata: { slug: parsed.data.slug, plan: parsed.data.plan }
    });

    return id;
  });

  revalidatePath("/customers");
  redirect(`/customers/${customerId}`);
}

export async function updateCustomerBillingAction(formData: FormData) {
  const staff = await requireRole(["owner", "admin"]);
  const customerId = String(formData.get("customerId") ?? "");
  const action = String(formData.get("action") ?? "");
  const reason = reasonSchema.parse(String(formData.get("reason") ?? ""));

  const nextStatus = action === "mark_overdue" ? "overdue" : action === "mark_churned" ? "churned" : "active";
  const actionType = action === "mark_overdue" ? "mark_overdue" : action === "mark_churned" ? "mark_churned" : "update";

  await withTransaction(async (client) => {
    await client.query(`UPDATE customers SET billing_status = $1 WHERE id = $2`, [nextStatus, customerId]);
    await recordAction(client, {
      actorStaffId: staff.id,
      actorEmail: staff.email,
      targetType: "customer",
      targetId: customerId,
      actionType: actionType as "mark_overdue" | "mark_churned" | "update",
      reason,
      metadata: { billing_status: nextStatus }
    });
  });

  revalidatePath(`/customers/${customerId}`);
  revalidatePath("/customers");
}

