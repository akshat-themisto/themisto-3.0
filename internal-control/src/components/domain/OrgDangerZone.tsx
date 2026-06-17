import type { StaffRole } from "@/server/db/enums";
import { OrgActionControls } from "@/components/domain/OrgActionControls";

export function OrgDangerZone({ organization, role }: { organization: any; role: StaffRole }) {
  return (
    <section className="panel panel-danger" style={{ padding: 24 }}>
      <div className="card-title-row">
        <div>
          <h3>Control Actions</h3>
          <p>Apply suspension, device revocation, contract end, or full termination from the same surface.</p>
        </div>
      </div>
      <OrgActionControls organization={organization} role={role} />
    </section>
  );
}
