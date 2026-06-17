import { formatCurrency, formatDateTime } from "@/lib/format";
import { StatusBadge } from "@/components/ui/StatusBadge";

export function ContractCard({ contract }: { contract: any }) {
  return (
    <div className="list-card">
      <div style={{ display: "flex", justifyContent: "space-between", gap: 12, alignItems: "center" }}>
        <div>
          <div className="strong" style={{ fontSize: "1.05rem" }}>{contract.plan}</div>
          <div className="muted" style={{ marginTop: 6 }}>{contract.seats} seats · {formatCurrency(Number(contract.mrr_cents))}</div>
        </div>
        <StatusBadge status={contract.status} />
      </div>
      <div className="form-note" style={{ marginTop: 14 }}>Started {formatDateTime(contract.starts_at)}{contract.ends_at ? ` · Ended ${formatDateTime(contract.ends_at)}` : ""}</div>
      {contract.notes ? <div className="form-note" style={{ marginTop: 10 }}>{contract.notes}</div> : null}
    </div>
  );
}

