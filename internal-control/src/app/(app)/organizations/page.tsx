import Link from "next/link";
import { Plus, Search } from "lucide-react";
import { PageHeader } from "@/components/ui/PageHeader";
import { DataTable } from "@/components/ui/DataTable";
import { StatusBadge } from "@/components/ui/StatusBadge";
import { SyncLocalRuntimeButton } from "@/components/ui/SyncLocalRuntimeButton";
import { StatCard } from "@/components/ui/StatCard";
import { OrgActionControls } from "@/components/domain/OrgActionControls";
import { formatRelative, formatStatusReason } from "@/lib/format";
import { requireStaff } from "@/server/auth/session";
import { listOrganizations } from "@/server/queries/organizations";

type SearchParams = Promise<{
  q?: string;
  status?: string;
}>;

export default async function OrganizationsPage({ searchParams }: { searchParams: SearchParams }) {
  const staff = await requireStaff();
  const filters = await searchParams;
  const organizations = await listOrganizations({
    search: filters.q?.trim() || undefined,
    status: filters.status || "all"
  });

  const suspendedCount = organizations.filter((organization: any) => organization.status === "suspended").length;
  const terminatedCount = organizations.filter((organization: any) => organization.status === "terminated").length;
  const activeDevices = organizations.reduce((sum: number, organization: any) => sum + Number(organization.active_devices), 0);

  return (
    <>
      <PageHeader
        kicker="Trust controls"
        title="Organizations"
        description="Operate lifecycle state where it actually matters: suspend, reactivate, revoke trust, and terminate from the same roster."
        actions={
          <>
            <SyncLocalRuntimeButton />
            <Link className="btn btn-primary" href="/organizations/new">
              <Plus size={18} />
              New organization
            </Link>
          </>
        }
      />

      <section className="stats-grid stats-grid-compact">
        <StatCard label="Organizations" value={organizations.length} meta="All organizations currently known to control" />
        <StatCard label="Suspended" value={suspendedCount} meta="Fail-closed but reversible" />
        <StatCard label="Terminated" value={terminatedCount} meta="Operationally dead orgs" />
        <StatCard label="Active devices" value={activeDevices} meta="Trusted endpoints across all orgs" />
      </section>

      <section className="panel panel-toolbar">
        <form className="toolbar toolbar-form" method="get">
          <div className="toolbar-filters">
            <label className="toolbar-search">
              <Search size={16} />
              <input className="input" type="text" name="q" defaultValue={filters.q || ""} placeholder="Search organization, slug, or customer" />
            </label>
            <select className="select" name="status" defaultValue={filters.status || "all"}>
              <option value="all">All states</option>
              <option value="active">Active</option>
              <option value="suspended">Suspended</option>
              <option value="terminated">Terminated</option>
            </select>
          </div>
          <div className="inline-actions">
            <button className="btn btn-secondary" type="submit">Apply filters</button>
            <Link className="btn btn-ghost" href="/organizations">Reset</Link>
          </div>
        </form>
      </section>

      <DataTable title="Operational roster" description="This list is the working surface. Open a detail page when you need context, not when you simply need to act.">
        <table className="data-table data-table-ops">
          <thead>
            <tr>
              <th>Organization</th>
              <th>Status</th>
              <th>Runtime</th>
              <th>Last activity</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody>
            {organizations.length ? organizations.map((organization: any) => (
              <tr key={organization.id}>
                <td>
                  <Link className="strong" href={`/organizations/${organization.id}`}>{organization.name}</Link>
                  <div className="muted" style={{ marginTop: 6 }}>{organization.customer_name} - {organization.slug}</div>
                </td>
                <td>
                  <StatusBadge status={organization.status} />
                  {formatStatusReason(organization.status_reason) ? <div className="table-note">{formatStatusReason(organization.status_reason)}</div> : null}
                </td>
                <td>
                  <div className="runtime-stack">
                    <StatusBadge status={organization.environment} />
                    <span className="muted">{organization.active_devices} active / {organization.device_count} total</span>
                    {Number(organization.revoked_devices) ? <span className="muted">{organization.revoked_devices} revoked</span> : null}
                  </div>
                </td>
                <td>{formatRelative(organization.last_seen_at)}</td>
                <td>
                  <div className="ops-row-actions">
                    <OrgActionControls organization={organization} role={staff.role} compact />
                    <Link className="btn btn-ghost btn-small" href={`/organizations/${organization.id}`}>Details</Link>
                  </div>
                </td>
              </tr>
            )) : (
              <tr>
                <td colSpan={5}>
                  <div className="empty-state">
                    <h3>No organizations yet</h3>
                    <p>Run a local sync or create an organization to start controlling lifecycle state from here.</p>
                  </div>
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </DataTable>
    </>
  );
}
