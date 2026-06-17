import Link from "next/link";
import { Plus } from "lucide-react";
import { PageHeader } from "@/components/ui/PageHeader";
import { DataTable } from "@/components/ui/DataTable";
import { StatusBadge } from "@/components/ui/StatusBadge";
import { SyncLocalRuntimeButton } from "@/components/ui/SyncLocalRuntimeButton";
import { formatCurrency, formatRelative } from "@/lib/format";
import { listCustomers } from "@/server/queries/customers";

export default async function CustomersPage() {
  const customers = await listCustomers();

  return (
    <>
      <PageHeader
        kicker="Commercial controls"
        title="Customers"
        description="The commercial layer above organizations. Use this surface to understand billing posture and the blast radius of lifecycle actions."
        actions={<><SyncLocalRuntimeButton /><Link className="btn btn-primary" href="/customers/new"><Plus size={18} />New customer</Link></>}
      />

      <DataTable title="Customer roster" description="Commercial entities, their active footprint, and current billing state.">
        <table className="data-table">
          <thead>
            <tr>
              <th>Customer</th>
              <th>Billing</th>
              <th>Organizations</th>
              <th>Active devices</th>
              <th>MRR</th>
              <th>Last activity</th>
            </tr>
          </thead>
          <tbody>
            {customers.map((customer: any) => (
              <tr key={customer.id}>
                <td>
                  <Link className="strong" href={`/customers/${customer.id}`}>{customer.name}</Link>
                  <div className="muted">{customer.primary_contact_email || customer.slug}</div>
                </td>
                <td><StatusBadge status={customer.billing_status} /></td>
                <td>{customer.organization_count}</td>
                <td>{customer.active_device_count}</td>
                <td>{formatCurrency(Number(customer.mrr_cents))}</td>
                <td>{formatRelative(customer.last_activity_at)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </DataTable>
    </>
  );
}

