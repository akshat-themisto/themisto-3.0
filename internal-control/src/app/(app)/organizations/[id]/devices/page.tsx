import { DataTable } from "@/components/ui/DataTable";
import { PageHeader } from "@/components/ui/PageHeader";
import { DeviceRow } from "@/components/domain/DeviceRow";
import { listDevicesForOrganization } from "@/server/queries/devices";
import { getOrganizationDetail } from "@/server/queries/organizations";

export default async function OrganizationDevicesPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  const devices = await listDevicesForOrganization(id);
  const { organization } = await getOrganizationDetail(id);

  if (!organization) return <div className="empty-state"><h3>Organization not found</h3></div>;

  return (
    <>
      <PageHeader kicker="Endpoint authority" title={`${organization.name} devices`} description="Disable, reactivate, or revoke trust for individual devices without reaching into customer machines." />
      <DataTable title="Devices" description="Per-device lifecycle control under this organization.">
        <table className="data-table">
          <thead>
            <tr>
              <th>Device</th>
              <th>OS</th>
              <th>Status</th>
              <th>Agent</th>
              <th>Last seen</th>
              <th>Enrolled</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody>
            {devices.map((device: any) => <DeviceRow key={device.id} device={device} organizationId={id} />)}
          </tbody>
        </table>
      </DataTable>
    </>
  );
}

