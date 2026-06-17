import { ShieldAlert, ShieldCheck, ShieldX } from "lucide-react";
import { StatusBadge } from "@/components/ui/StatusBadge";
import { formatDateTime, formatStatusReason } from "@/lib/format";

const copy: Record<string, { icon: typeof ShieldCheck; description: string }> = {
  active: {
    icon: ShieldCheck,
    description: "Org is fully authorized. Downstream services should continue enrollment, gateway access, and telemetry ingestion."
  },
  suspended: {
    icon: ShieldAlert,
    description: "Org is paused. Use this when service should fail closed but remain recoverable."
  },
  terminated: {
    icon: ShieldX,
    description: "Org is terminal. This is your strongest contractual shutdown state and should normally be irreversible in the UI."
  }
};

export function OrgStatePanel({ organization }: { organization: any }) {
  const state = copy[organization.status] ?? copy.active;
  const Icon = state.icon;
  const visibleReason = formatStatusReason(organization.status_reason);

  return (
    <section className="panel" style={{ padding: 24 }}>
      <div className="card-title-row">
        <div>
          <h3>Org State</h3>
          <p>What the rest of Themisto should trust about this organization right now.</p>
        </div>
        <StatusBadge status={organization.status} />
      </div>
      <div className="grid-2" style={{ alignItems: "center" }}>
        <div className="list-card" style={{ display: "flex", gap: 16, alignItems: "center" }}>
          <div className="brand-mark" style={{ width: 58, height: 58, borderRadius: 18, padding: 14 }}>
            <Icon size={28} />
          </div>
          <div>
            <div style={{ fontWeight: 700, fontSize: "1.1rem" }}>{organization.status}</div>
            <div style={{ color: "var(--text-secondary)", marginTop: 8 }}>{state.description}</div>
          </div>
        </div>
        <div className="stack" style={{ gap: 10 }}>
          <div className="list-card">
            <div className="sidebar-section-title">Last changed</div>
            <div>{formatDateTime(organization.status_changed_at)}</div>
          </div>
          <div className="list-card">
            <div className="sidebar-section-title">Changed by</div>
            <div>{organization.status_changed_by_name || "System"}</div>
            {visibleReason ? <div className="form-note" style={{ marginTop: 8 }}>{visibleReason}</div> : null}
          </div>
        </div>
      </div>
    </section>
  );
}

