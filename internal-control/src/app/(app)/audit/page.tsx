import { PageHeader } from "@/components/ui/PageHeader";
import { DataTable } from "@/components/ui/DataTable";
import { StatusBadge } from "@/components/ui/StatusBadge";
import { formatDateTime } from "@/lib/format";
import { listAuditLog } from "@/server/queries/audit";

export default async function AuditPage() {
  const actions = await listAuditLog();

  return (
    <>
      <PageHeader kicker="Full accountability" title="Audit log" description="Every state change flows through here so your team always knows who touched what and why." />
      <DataTable title="Recent actions" description="Audit rows are append-only and written alongside the actual state mutation.">
        <table className="data-table">
          <thead>
            <tr>
              <th>When</th>
              <th>Actor</th>
              <th>Action</th>
              <th>Target</th>
              <th>Reason</th>
            </tr>
          </thead>
          <tbody>
            {actions.map((action: any) => (
              <tr key={action.id}>
                <td>{formatDateTime(action.created_at)}</td>
                <td>{action.actor_name || action.actor_email}</td>
                <td><StatusBadge status={action.action_type} /></td>
                <td>{action.target_type}<div className="muted">{action.target_id}</div></td>
                <td>{action.reason || "-"}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </DataTable>
    </>
  );
}

