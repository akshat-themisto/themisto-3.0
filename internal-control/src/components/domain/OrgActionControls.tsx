"use client";

import { StatusBadge } from "@/components/ui/StatusBadge";
import { DangerDialog } from "@/components/ui/DangerDialog";
import { updateOrganizationStateAction } from "@/server/actions/organizations";
import type { StaffRole } from "@/server/db/enums";

type OrgLike = {
  id: string;
  slug: string;
  status: string;
  customer_name?: string;
};

function OrgActionForm({
  organizationId,
  action,
  title,
  confirmationLabel
}: {
  organizationId: string;
  action: string;
  title: string;
  confirmationLabel?: string;
}) {
  return (
    <form action={updateOrganizationStateAction} className="form-grid">
      <input type="hidden" name="organizationId" value={organizationId} />
      <input type="hidden" name="action" value={action} />
      <label className="form-label">
        Reason
        <textarea
          className="textarea"
          name="reason"
          placeholder="Leave a clear, audit-grade reason."
          required
          minLength={10}
        />
      </label>
      {confirmationLabel ? (
        <label className="form-label">
          Type <strong>{confirmationLabel}</strong> to confirm
          <input className="input" type="text" name="confirmation" required />
        </label>
      ) : null}
      <button className="btn btn-danger" type="submit">
        Confirm {title}
      </button>
    </form>
  );
}

function allow(role: StaffRole, action: "suspend" | "reactivate" | "revoke_all_devices" | "mark_contract_ended" | "terminate") {
  if (role === "owner" || role === "admin") return true;
  if (role === "operator") {
    return ["suspend", "reactivate", "revoke_all_devices"].includes(action);
  }
  return false;
}

export function OrgActionControls({
  organization,
  role,
  compact = false
}: {
  organization: OrgLike;
  role: StaffRole;
  compact?: boolean;
}) {
  const triggerBase = compact ? "btn btn-secondary btn-small" : "btn btn-secondary";
  const destructiveBase = compact ? "btn btn-danger btn-small" : "btn btn-danger";

  return (
    <div className={compact ? "inline-actions org-action-bar org-action-bar-compact" : "stack"} style={compact ? undefined : { gap: 12 }}>
      {!compact ? (
        <div className="org-action-status">
          <div>
            <div className="sidebar-section-title">Current trust state</div>
            <div className="org-action-status-copy">
              <strong>{organization.customer_name || "Organization"}</strong>
              <span>{organization.slug}</span>
            </div>
          </div>
          <StatusBadge status={organization.status} />
        </div>
      ) : null}

      <div className="inline-actions">
        {organization.status === "active" && allow(role, "suspend") ? (
          <DangerDialog
            triggerLabel="Suspend"
            title="Suspend organization"
            description="Stop service cleanly while leaving a path back to normal operation."
            triggerClassName={triggerBase}
          >
            <OrgActionForm organizationId={organization.id} action="suspend" title="suspension" />
          </DangerDialog>
        ) : null}

        {organization.status === "suspended" && allow(role, "reactivate") ? (
          <DangerDialog
            triggerLabel="Reactivate"
            title="Reactivate organization"
            description="Return this organization to an active and trusted runtime state."
            triggerClassName={triggerBase}
          >
            <OrgActionForm organizationId={organization.id} action="reactivate" title="reactivation" />
          </DangerDialog>
        ) : null}

        {organization.status !== "terminated" && allow(role, "revoke_all_devices") ? (
          <DangerDialog
            triggerLabel="Revoke devices"
            title="Revoke all device access"
            description="Use this when you need the org to go dark quickly without touching the customer machine directly."
            triggerClassName={destructiveBase}
          >
            <OrgActionForm
              organizationId={organization.id}
              action="revoke_all_devices"
              title="device revocation"
              confirmationLabel={organization.slug}
            />
          </DangerDialog>
        ) : null}

        {organization.status !== "terminated" && allow(role, "mark_contract_ended") ? (
          <DangerDialog
            triggerLabel="End contract"
            title="Mark contract ended"
            description="This moves the customer relationship into an ended commercial state without deleting anything on endpoint."
            triggerClassName={triggerBase}
          >
            <OrgActionForm organizationId={organization.id} action="mark_contract_ended" title="contract ending" />
          </DangerDialog>
        ) : null}

        {organization.status !== "terminated" && allow(role, "terminate") ? (
          <DangerDialog
            triggerLabel="Terminate"
            title="Terminate organization"
            description="Terminate is the strongest state. Use it when the relationship is over and the org should be operationally dead."
            triggerClassName={destructiveBase}
          >
            <OrgActionForm
              organizationId={organization.id}
              action="terminate"
              title="termination"
              confirmationLabel={organization.slug}
            />
          </DangerDialog>
        ) : null}
      </div>
    </div>
  );
}
