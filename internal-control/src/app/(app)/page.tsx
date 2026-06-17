import Link from "next/link";
import { Building2, ShieldBan, Wallet } from "lucide-react";
import { PageHeader } from "@/components/ui/PageHeader";
import { StatCard } from "@/components/ui/StatCard";
import { ActionTrendChart } from "@/components/domain/ActionTrendChart";
import { StateMixChart } from "@/components/domain/StateMixChart";
import { AuditTimeline } from "@/components/domain/AuditTimeline";
import { SyncLocalRuntimeButton } from "@/components/ui/SyncLocalRuntimeButton";
import { StatusBadge } from "@/components/ui/StatusBadge";
import { formatCurrency, formatRelative, formatStatusReason } from "@/lib/format";
import { getOverviewMetrics } from "@/server/queries/audit";

export default async function OverviewPage() {
  const { summary, recentActions, feed, orgMix, attention, liveRoster } = await getOverviewMetrics();

  return (
    <>
      <PageHeader
        kicker="Internal Ops"
        title="Control Center"
        description="Watch the real runtime footprint, then intervene with the smallest action that solves the problem."
        actions={<SyncLocalRuntimeButton />}
      />

      <section className="ops-summary-grid">
        <section className="panel ops-summary-card">
          <div className="page-kicker">Now watching</div>
          <h3>Customer runtime and commercial state in one place.</h3>
          <p>Use this page to spot accounts that need intervention, then jump directly into organization-level controls.</p>
          <div className="hero-meta">
            <span className="chip"><Building2 size={16} /> {summary.total_customers} customers</span>
            <span className="chip"><ShieldBan size={16} /> {summary.suspended_orgs} suspended orgs</span>
            <span className="chip"><Wallet size={16} /> {formatCurrency(Number(summary.mrr_cents))} tracked MRR</span>
          </div>
        </section>

        <section className="panel ops-watchlist">
          <div className="card-title-row">
            <div>
              <h3>Needs attention</h3>
              <p>Organizations with a non-active trust state or commercial pressure.</p>
            </div>
            <Link className="btn btn-secondary btn-small" href="/organizations">Open roster</Link>
          </div>
          <div className="stack">
            {attention.length ? attention.map((organization: any) => (
              <div key={organization.id} className="list-card list-card-tight">
                <div style={{ display: "flex", justifyContent: "space-between", gap: 16, alignItems: "center" }}>
                  <div>
                    <Link className="strong" href={`/organizations/${organization.id}`}>{organization.name}</Link>
                    <div className="muted" style={{ marginTop: 6 }}>{organization.customer_name} - {organization.active_devices} active devices</div>
                  </div>
                  <StatusBadge status={organization.status} />
                </div>
                {formatStatusReason(organization.status_reason) ? <div className="table-note">{formatStatusReason(organization.status_reason)}</div> : null}
              </div>
            )) : (
              <div className="empty-state compact-empty-state">
                <h3>Nothing urgent</h3>
                <p>The tracked organizations are currently clear of suspension and contract pressure.</p>
              </div>
            )}
          </div>
        </section>
      </section>

      <section className="stats-grid stats-grid-compact">
        <StatCard label="Active customers" value={summary.active_customers} meta="Commercially healthy accounts" />
        <StatCard label="Suspended orgs" value={summary.suspended_orgs} meta="Fail-closed but reversible" />
        <StatCard label="Active devices" value={summary.active_devices} meta="Live trusted endpoints" />
        <StatCard label="Past-due contracts" value={summary.past_due_contracts} meta="Need billing follow-up" />
      </section>

      <section className="grid-2">
        <section className="chart-card">
          <div className="card-title-row">
            <div>
              <h3>Action volume</h3>
              <p>Critical changes versus total operator activity over the last 7 days.</p>
            </div>
          </div>
          <div className="chart-legend">
            <span><span className="legend-dot" style={{ background: "#c97c85" }} />Total actions</span>
            <span><span className="legend-dot" style={{ background: "#f4a6aa" }} />Critical actions</span>
          </div>
          <ActionTrendChart data={recentActions as any[]} />
        </section>

        <section className="chart-card">
          <div className="card-title-row">
            <div>
              <h3>Organization state mix</h3>
              <p>The current distribution of trust states across the footprint.</p>
            </div>
          </div>
          <StateMixChart data={orgMix as any[]} />
        </section>
      </section>

      <section className="grid-2">
        <section className="table-shell">
          <div className="card-title-row">
            <div>
              <h3>Live roster</h3>
              <p>The most recently active organizations in the runtime.</p>
            </div>
          </div>
          <table className="data-table">
            <thead>
              <tr>
                <th>Organization</th>
                <th>Status</th>
                <th>Devices</th>
                <th>Last seen</th>
              </tr>
            </thead>
            <tbody>
              {liveRoster.map((organization: any) => (
                <tr key={organization.id}>
                  <td>
                    <Link className="strong" href={`/organizations/${organization.id}`}>{organization.name}</Link>
                    <div className="muted">{organization.customer_name}</div>
                  </td>
                  <td><StatusBadge status={organization.status} /></td>
                  <td>{organization.active_devices} active / {organization.device_count} total</td>
                  <td>{formatRelative(organization.last_seen_at)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </section>

        <AuditTimeline actions={feed as any[]} />
      </section>
    </>
  );
}
