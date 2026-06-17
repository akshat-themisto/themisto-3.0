import { createCustomerAction } from "@/server/actions/customers";
import { PageHeader } from "@/components/ui/PageHeader";

export default function NewCustomerPage() {
  return (
    <>
      <PageHeader kicker="Commercial setup" title="Create customer" description="Stand up the commercial entity first, then attach organizations and contract posture to it." />
      <section className="panel" style={{ padding: 28 }}>
        <form action={createCustomerAction} className="form-grid">
          <div className="form-row">
            <label className="form-label">Customer name<input className="input" name="name" required /></label>
            <label className="form-label">Slug<input className="input" name="slug" required placeholder="acme-corp" /></label>
          </div>
          <div className="form-row">
            <label className="form-label">Primary contact email<input className="input" name="primaryContactEmail" type="email" /></label>
            <label className="form-label">Plan<input className="input" name="plan" defaultValue="enterprise" required /></label>
          </div>
          <div className="form-row">
            <label className="form-label">Seats<input className="input" name="seats" type="number" defaultValue="50" min="1" required /></label>
            <label className="form-label">Monthly recurring revenue (cents)<input className="input" name="mrrCents" type="number" defaultValue="120000" min="0" required /></label>
          </div>
          <label className="form-label">Notes<textarea className="textarea" name="notes" placeholder="Contract notes, exceptions, deployment context, or billing caveats." /></label>
          <div className="inline-actions"><button className="btn btn-primary" type="submit">Create customer</button></div>
        </form>
      </section>
    </>
  );
}

