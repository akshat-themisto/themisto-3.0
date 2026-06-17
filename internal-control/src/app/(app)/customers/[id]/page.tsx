import Link from "next/link";
import { updateCustomerBillingAction } from "@/server/actions/customers";
import { ContractCard } from "@/components/domain/ContractCard";
import { AuditTimeline } from "@/components/domain/AuditTimeline";
import { PageHeader } from "@/components/ui/PageHeader";
import { StatusBadge } from "@/components/ui/StatusBadge";
import { getCustomerDetail } from "@/server/queries/customers";

export default async function CustomerDetailPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  const { customer, organizations, contracts, actions } = await getCustomerDetail(id);

  if (!customer) return <div className="empty-state"><h3>Customer not found</h3></div>;

  return (
    <>
      <PageHeader
        kicker="Customer detail"
        title={customer.name}
        description="Commercial state, related organizations, and the latest control actions tied to this customer footprint."
        actions={<StatusBadge status={customer.billing_status} />}
      />

      <section className="grid-2">
        <section className="panel" style={{ padding: 24 }}>
          <div className="card-title-row">
            <div><h3>Customer summary</h3><p>Billing and footprint at a glance.</p></div>
          </div>
          <div className="grid-3">
            <div className="list-card"><div className="sidebar-section-title">Organizations</div><div className="strong">{customer.organization_count}</div></div>
            <div className="list-card"><div className="sidebar-section-title">Active devices</div><div className="strong">{customer.active_device_count}</div></div>
            <div className="list-card"><div className="sidebar-section-title">Revoked devices</div><div className="strong">{customer.revoked_device_count}</div></div>
          </div>
          <div className="form-note" style={{ marginTop: 18 }}>{customer.notes || "No special notes on file."}</div>
        </section>
        <section className="panel" style={{ padding: 24 }}>
          <div className="card-title-row"><div><h3>Billing controls</h3><p>Move the commercial state without jumping directly to destructive operational action.</p></div></div>
          <div className="inline-actions">
            <form action={updateCustomerBillingAction} className="form-grid" style={{ width: "100%" }}>
              <input type="hidden" name="customerId" value={customer.id} />
              <label className="form-label">Reason<textarea className="textarea" name="reason" required minLength={10} placeholder="Why are we changing billing state?" /></label>
              <div className="inline-actions">
                <button className="btn btn-secondary" name="action" value="reactivate" type="submit">Mark active</button>
                <button className="btn btn-secondary" name="action" value="mark_overdue" type="submit">Mark overdue</button>
                <button className="btn btn-danger" name="action" value="mark_churned" type="submit">Mark churned</button>
              </div>
            </form>
          </div>
        </section>
      </section>

      <section className="grid-2">
        <section className="panel" style={{ padding: 24 }}>
          <div className="card-title-row"><div><h3>Organizations</h3><p>Operational surfaces under this customer.</p></div></div>
          <div className="stack">
            {organizations.map((organization: any) => (
              <div key={organization.id} className="list-card" style={{ display: "flex", justifyContent: "space-between", gap: 18, alignItems: "center" }}>
                <div>
                  <Link className="strong" href={`/organizations/${organization.id}`}>{organization.name}</Link>
                  <div className="muted" style={{ marginTop: 6 }}>{organization.device_count} devices - {organization.slug}</div>
                </div>
                <StatusBadge status={organization.status} />
              </div>
            ))}
          </div>
        </section>
        <section className="panel" style={{ padding: 24 }}>
          <div className="card-title-row"><div><h3>Contracts</h3><p>Commercial agreements currently attached to this customer.</p></div></div>
          <div className="stack">
            {contracts.map((contract: any) => <ContractCard key={contract.id} contract={contract} />)}
          </div>
        </section>
      </section>

      <AuditTimeline actions={actions as any[]} />
    </>
  );
}
