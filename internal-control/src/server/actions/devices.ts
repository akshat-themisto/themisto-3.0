"use server";

import { revalidatePath } from "next/cache";
import { deviceActionSchema } from "@/lib/zod-schemas";
import { requireRole } from "@/server/auth/session";
import { withTransaction } from "@/server/db/client";
import { recordAction } from "@/server/audit";

export async function updateDeviceStateAction(formData: FormData) {
  const staff = await requireRole(["owner", "admin", "operator"]);
  const parsed = deviceActionSchema.safeParse({
    deviceId: formData.get("deviceId"),
    organizationId: formData.get("organizationId"),
    action: formData.get("action"),
    reason: formData.get("reason")
  });

  if (!parsed.success) {
    throw new Error(parsed.error.issues[0]?.message ?? "Unable to update device state");
  }

  await withTransaction(async (client) => {
    const deviceResult = await client.query(
      `SELECT id, status FROM devices WHERE id = $1 LIMIT 1`,
      [parsed.data.deviceId]
    );
    const device = deviceResult.rows[0];
    if (!device) throw new Error("Device not found");

    if (parsed.data.action === "disable") {
      if (device.status !== "active") {
        throw new Error("Only active devices can be disabled.");
      }
      await client.query(
        `UPDATE devices SET status = 'disabled', status_reason = $2, disabled_at = now() WHERE id = $1`,
        [parsed.data.deviceId, parsed.data.reason]
      );
      await recordAction(client, {
        actorStaffId: staff.id,
        actorEmail: staff.email,
        targetType: "device",
        targetId: parsed.data.deviceId,
        actionType: "disable_device",
        reason: parsed.data.reason,
        metadata: { previous_status: device.status }
      });
    }

    if (parsed.data.action === "reactivate") {
      if (device.status !== "disabled") {
        throw new Error("Only disabled devices can be reactivated.");
      }
      await client.query(
        `UPDATE devices SET status = 'active', status_reason = $2, disabled_at = null WHERE id = $1`,
        [parsed.data.deviceId, parsed.data.reason]
      );
      await recordAction(client, {
        actorStaffId: staff.id,
        actorEmail: staff.email,
        targetType: "device",
        targetId: parsed.data.deviceId,
        actionType: "reactivate_device",
        reason: parsed.data.reason,
        metadata: { previous_status: device.status }
      });
    }

    if (parsed.data.action === "revoke") {
      if (device.status === "revoked") {
        throw new Error("This device is already revoked.");
      }
      await client.query(
        `UPDATE devices SET status = 'revoked', status_reason = $2, revoked_at = now() WHERE id = $1`,
        [parsed.data.deviceId, parsed.data.reason]
      );
      await recordAction(client, {
        actorStaffId: staff.id,
        actorEmail: staff.email,
        targetType: "device",
        targetId: parsed.data.deviceId,
        actionType: "revoke_device",
        reason: parsed.data.reason,
        metadata: { previous_status: device.status }
      });
    }
  });

  revalidatePath(`/organizations/${parsed.data.organizationId}`);
  revalidatePath(`/organizations/${parsed.data.organizationId}/devices`);
  revalidatePath("/organizations");
  revalidatePath("/customers");
  revalidatePath("/");
}

