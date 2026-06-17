import { PageHeader } from "@/components/ui/PageHeader";
import { createOrganizationAction } from "@/server/actions/organizations";
import { listCustomers } from "@/server/queries/customers";

export default async function NewOrganizationPage() {
  const customers = await listCustomers();

  return (
    <>
      <PageHeader kicker="Trust setup" title="Create organization" description="Add the operational tenant, link it to a commercial customer, and seed the first deployment record." />
      <section className="panel" style={{ padding: 28 }}>
        <form action={createOrganizationAction} className="form-grid">
          <div className="form-row">
            <label className="form-label">Customer<select className="select" name="customerId" required>{customers.map((customer: any) => <option key={customer.id} value={customer.id}>{customer.name}</option>)}</select></label>
            <label className="form-label">Organization name<input className="input" name="name" required /></label>
          </div>
          <div className="form-row">
            <label className="form-label">Slug<input className="input" name="slug" required placeholder="acme-production" /></label>
            <label className="form-label">External org UUID<input className="input" name="externalOrgId" placeholder="Optional mirror ID from customer backend" /></label>
          </div>
          <div className="form-row">
            <label className="form-label">Region<input className="input" name="region" defaultValue="us-east-1" required /></label>
            <label className="form-label">Environment<select className="select" name="environment" defaultValue="production"><option value="production">production</option><option value="staging">staging</option><option value="development">development</option></select></label>
          </div>
          <label className="form-label">Gateway URL<input className="input" name="gatewayUrl" defaultValue="https://gateway.themisto.local" /></label>
          <div className="inline-actions"><button className="btn btn-primary" type="submit">Create organization</button></div>
        </form>
      </section>
    </>
  );
}

