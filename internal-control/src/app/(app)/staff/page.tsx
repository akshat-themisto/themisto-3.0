import { PageHeader } from "@/components/ui/PageHeader";
import { DataTable } from "@/components/ui/DataTable";
import { StatusBadge } from "@/components/ui/StatusBadge";
import { createStaffUserAction, deactivateStaffUserAction } from "@/server/actions/staff";
import { requireRole } from "@/server/auth/session";
import { listStaffUsers } from "@/server/queries/staff";
import { formatDateTime } from "@/lib/format";

export default async function StaffPage() {
  await requireRole(["owner", "admin"]);
  const staffUsers = await listStaffUsers();

  return (
    <>
      <PageHeader kicker="Internal access" title="Staff" description="Control who can operate this surface and make every permissioned action attributable." />
      <section className="grid-2">
        <section className="panel" style={{ padding: 24 }}>
          <div className="card-title-row"><div><h3>Create staff user</h3><p>Seed another operator without leaving the control plane.</p></div></div>
          <form action={createStaffUserAction} className="form-grid">
            <div className="form-row">
              <label className="form-label">Name<input className="input" name="name" required /></label>
              <label className="form-label">Email<input className="input" name="email" type="email" required /></label>
            </div>
            <div className="form-row">
              <label className="form-label">Role<select className="select" name="role" defaultValue="operator"><option value="owner">owner</option><option value="admin">admin</option><option value="operator">operator</option><option value="viewer">viewer</option></select></label>
              <label className="form-label">Temporary password<input className="input" name="password" defaultValue="ChangeMe!1234" required /></label>
            </div>
            <button className="btn btn-primary" type="submit">Create staff user</button>
          </form>
        </section>
        <DataTable title="Current staff" description="Operators with access to the internal control plane.">
          <table className="data-table">
            <thead><tr><th>Staff user</th><th>Role</th><th>State</th><th>Last login</th><th>Action</th></tr></thead>
            <tbody>
              {staffUsers.map((staff: any) => (
                <tr key={staff.id}>
                  <td><div className="strong">{staff.name}</div><div className="muted">{staff.email}</div></td>
                  <td>{staff.role}</td>
                  <td><StatusBadge status={staff.is_active ? "active" : "disabled"} /></td>
                  <td>{formatDateTime(staff.last_login_at)}</td>
                  <td>
                    {staff.is_active ? (
                      <form action={deactivateStaffUserAction}>
                        <input type="hidden" name="staffUserId" value={staff.id} />
                        <input type="hidden" name="reason" value="Access removed from staff directory" />
                        <button className="btn btn-secondary btn-small" type="submit">Deactivate</button>
                      </form>
                    ) : null}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </DataTable>
      </section>
    </>
  );
}

