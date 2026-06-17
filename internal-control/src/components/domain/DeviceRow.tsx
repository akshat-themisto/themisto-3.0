import { StatusBadge } from "@/components/ui/StatusBadge";
import { DangerDialog } from "@/components/ui/DangerDialog";
import { updateDeviceStateAction } from "@/server/actions/devices";
import { formatDateTime, formatRelative } from "@/lib/format";

function DeviceActionForm({ deviceId, organizationId, action, label }: { deviceId: string; organizationId: string; action: string; label: string }) {
  return (
    <form action={updateDeviceStateAction} className="form-grid">
      <input type="hidden" name="deviceId" value={deviceId} />
      <input type="hidden" name="organizationId" value={organizationId} />
      <input type="hidden" name="action" value={action} />
      <label className="form-label">
        Reason
        <textarea className="textarea" name="reason" required minLength={10} placeholder="Explain the device state change." />
      </label>
      <button className="btn btn-danger" type="submit">Confirm {label}</button>
    </form>
  );
}

export function DeviceRow({ device, organizationId }: { device: any; organizationId: string }) {
  return (
    <tr>
      <td>
        <div className="strong">{device.hostname}</div>
        <div className="muted">{device.external_device_id}</div>
      </td>
      <td>{device.os}</td>
      <td><StatusBadge status={device.status} /></td>
      <td>{device.agent_version || "-"}</td>
      <td>{formatRelative(device.last_seen_at)}</td>
      <td>{formatDateTime(device.enrolled_at)}</td>
      <td>
        <div className="inline-actions">
          {device.status === "active" ? (
            <DangerDialog
              triggerLabel="Disable"
              title={`Disable ${device.hostname}`}
              description="Use this when the device should be paused but potentially restored later."
              triggerClassName="btn btn-secondary btn-small"
            >
              <DeviceActionForm deviceId={device.id} organizationId={organizationId} action="disable" label="disable" />
            </DangerDialog>
          ) : null}
          {device.status === "disabled" ? (
            <DangerDialog
              triggerLabel="Reactivate"
              title={`Reactivate ${device.hostname}`}
              description="Return this device to active service."
              triggerClassName="btn btn-secondary btn-small"
            >
              <DeviceActionForm deviceId={device.id} organizationId={organizationId} action="reactivate" label="reactivation" />
            </DangerDialog>
          ) : null}
          {device.status !== "revoked" ? (
            <DangerDialog
              triggerLabel="Revoke"
              title={`Revoke ${device.hostname}`}
              description="This permanently invalidates the device for downstream enforcement."
              triggerClassName="btn btn-danger btn-small"
            >
              <DeviceActionForm deviceId={device.id} organizationId={organizationId} action="revoke" label="revocation" />
            </DangerDialog>
          ) : null}
        </div>
      </td>
    </tr>
  );
}

