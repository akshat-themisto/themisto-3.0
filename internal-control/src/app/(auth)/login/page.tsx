import { ShieldBan } from "lucide-react";
import { LoginForm } from "@/components/ui/LoginForm";
import { getBootstrapStatus } from "@/server/setup";

export const dynamic = "force-dynamic";

export default async function LoginPage() {
  const bootstrap = await getBootstrapStatus();

  return (
    <main className="login-shell">
      <section className="login-panel">
        <div className="page-kicker">Themisto Staff Only</div>
        <h1>Control the service, not just the UI.</h1>
        <p>
          This is the internal command surface for organization lifecycle, device access,
          contract state, and emergency deauthorization. It is intentionally separate from the customer dashboard.
        </p>
        {!bootstrap.initialized ? (
          <div className="panel" style={{ padding: 20, marginBottom: 24, borderColor: "rgba(169, 74, 85, 0.28)" }}>
            <div className="sidebar-section-title">Setup required</div>
            <div style={{ fontWeight: 700, marginTop: 8 }}>The internal control database is not initialized yet.</div>
            <div className="form-note" style={{ marginTop: 10 }}>
              Run `npm run db:migrate` and then `npm run db:seed` inside `internal-control` after your local Postgres is up.
            </div>
          </div>
        ) : null}
        <LoginForm />
      </section>

      <section className="login-side">
        <div className="login-orbit" />
        <div className="login-showcase">
          <div className="role-pill">Internal Ops</div>
          <img className="spinning-logo" src="/transparent-logo.png" alt="Themisto mark" />
          <h2>Quiet authority for the moments that matter.</h2>
          <p>
            Suspend organizations, revoke device trust, and track who changed what with a control plane that matches the seriousness of the job.
          </p>
          <div className="hero-meta">
            <div className="chip"><ShieldBan size={16} /> Fail closed, not destructively</div>
          </div>
        </div>
      </section>
    </main>
  );
}

