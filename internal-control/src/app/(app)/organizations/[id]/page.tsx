import Link from "next/link";
import { DataTable } from "@/components/ui/DataTable";
import { PageHeader } from "@/components/ui/PageHeader";
import { StatCard } from "@/components/ui/StatCard";
import { StatusBadge } from "@/components/ui/StatusBadge";
import { OrgStatePanel } from "@/components/domain/OrgStatePanel";
import { OrgDangerZone } from "@/components/domain/OrgDangerZone";
import { AuditTimeline } from "@/components/domain/AuditTimeline";
import { formatDateTime, formatRelative } from "@/lib/format";
import { requireStaff } from "@/server/auth/session";
import { getOrganizationDetail } from "@/server/queries/organizations";

export default async function OrganizationDetailPage({ params }: { params: Promise<{ id: string }> }) {
  const staff = await requireStaff();
  const { id } = await params;
  const { organization, devices, actions, deployments } = await getOrganizationDetail(id);

  if (!organization) return <div className="empty-state"><h3>Organization not found</h3></div>;

  return (
    <>
      <PageHeader
        kicker="Operational authority"
        title={organization.name}
        description="The internal source of truth for trust state, runtime posture, and irreversible actions."
        actions={
          <>
            <StatusBadge status={organization.status} />
            <Link className="btn btn-secondary" href={`/organizations/${organization.id}/devices`}>
              View all devices
            </Link>
          </>
        }
      />

      <section className="stats-grid">
        <StatCard label="Active devices" value={organization.active_devices} meta={`${organization.device_count} total endpoints`} />
        <StatCard label="Revoked devices" value={organization.revoked_devices} meta="Trust already pulled back" />
        <StatCard label="Deployment health" value={organization.deployment_health} meta={`Last heartbeat ${formatRelative(organization.last_heartbeat_at)}`} />
        <StatCard label="Customer" value={organization.customer_name} meta={organization.billing_status} />
      </section>

      <section className="grid-2">
        <OrgStatePanel organization={organization} />
        <section className="panel" style={{ padding: 24 }}>
          <div className="card-title-row">
            <div>
              <h3>Deployment surface</h3>
              <p>Context for what downstream systems should be enforcing right now.</p>
            </div>
          </div>
          <div className="stack">
            {deployments.map((deployment: any) => (
              <div key={deployment.id} className="list-card">
                <div style={{ display: "flex", justifyContent: "space-between", gap: 12, alignItems: "center" }}>
                  <div className="strong">{deployment.environment} - {deployment.region}</div>
                  <StatusBadge status={deployment.health} />
                </div>
                <div className="form-note" style={{ marginTop: 10 }}>{deployment.gateway_url || "No gateway URL set"}</div>
                <div className="form-note" style={{ marginTop: 6 }}>Last heartbeat {formatDateTime(deployment.last_heartbeat_at)}</div>
              </div>
            ))}
          </div>
        </section>
      </section>

      <DataTable
        title="Device preview"
        description="Recent devices under this organization. Use the full page for all per-device actions."
        actions={<Link className="btn btn-secondary btn-small" href={`/organizations/${organization.id}/devices`}>Open device control</Link>}
      >
        <table className="data-table">
          <thead>
            <tr>
              <th>Hostname</th>
              <th>OS</th>
              <th>Status</th>
              <th>Agent</th>
              <th>Last seen</th>
            </tr>
          </thead>
          <tbody>
            {devices.map((device: any) => (
              <tr key={device.id}>
                <td>
                  <div className="strong">{device.hostname}</div>
                  <div className="muted">{device.external_device_id}</div>
                </td>
                <td>{device.os}</td>
                <td><StatusBadge status={device.status} /></td>
                <td>{device.agent_version || "-"}</td>
                <td>{formatRelative(device.last_seen_at)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </DataTable>

      <section className="grid-2">
        <AuditTimeline actions={actions as any[]} />
        <OrgDangerZone organization={organization} role={staff.role} />
      </section>
    </>
  );
}
