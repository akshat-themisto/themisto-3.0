import { RefreshCcw } from "lucide-react";
import { syncLocalRuntimeAction } from "@/server/actions/sync";

export function SyncLocalRuntimeButton() {
  return (
    <form action={syncLocalRuntimeAction}>
      <button className="btn btn-secondary" type="submit">
        <RefreshCcw size={16} />
        Sync Local Runtime
      </button>
    </form>
  );
}
