import type { PoolClient } from "pg";
import type { ActionType, TargetType } from "@/server/db/enums";

export async function recordAction(
  client: PoolClient,
  input: {
    actorStaffId?: string | null;
    actorEmail: string;
    targetType: TargetType;
    targetId: string;
    actionType: ActionType;
    reason?: string | null;
    metadata?: Record<string, unknown>;
  }
) {
  await client.query(
    `INSERT INTO control_actions (actor_staff_id, actor_email, target_type, target_id, action_type, reason, metadata)
     VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb)`,
    [
      input.actorStaffId ?? null,
      input.actorEmail,
      input.targetType,
      input.targetId,
      input.actionType,
      input.reason ?? null,
      JSON.stringify(input.metadata ?? {})
    ]
  );
}

