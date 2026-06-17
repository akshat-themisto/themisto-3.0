"use server";

import { revalidatePath } from "next/cache";
import { requireRole } from "@/server/auth/session";
import { syncFromLocalThemisto } from "@/server/sync";

export async function syncLocalRuntimeAction() {
  await requireRole(["owner", "admin", "operator"]);
  await syncFromLocalThemisto();
  revalidatePath("/");
  revalidatePath("/customers");
  revalidatePath("/organizations");
  revalidatePath("/audit");
}
