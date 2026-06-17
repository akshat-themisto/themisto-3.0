import { KeyRound, ShieldAlert } from "lucide-react";
import { redirect } from "next/navigation";
import { ChangePasswordForm } from "@/components/ui/ChangePasswordForm";
import { getCurrentStaff } from "@/server/auth/session";

export const dynamic = "force-dynamic";

export default async function ChangePasswordPage() {
  const staff = await getCurrentStaff();
  if (!staff) {
    redirect("/login");
  }
  if (!staff.must_change_password) {
    redirect("/");
  }

  return (
    <main className="login-shell">
      <section className="login-panel">
        <div className="page-kicker">Credential Rotation Required</div>
        <h1>Rotate this password before using the control plane.</h1>
        <p>
          Themisto blocks access to the internal command surface until temporary or seeded staff credentials
          are replaced. This keeps shared bootstrap passwords from lingering in production.
        </p>
        <ChangePasswordForm email={staff.email} />
      </section>

      <section className="login-side">
        <div className="login-orbit" />
        <div className="login-showcase">
          <div className="role-pill">Access Hardening</div>
          <div className="hero-meta" style={{ marginTop: 0 }}>
            <div className="chip"><ShieldAlert size={16} /> Temporary credentials are blocked</div>
            <div className="chip"><KeyRound size={16} /> Session rotates after password change</div>
          </div>
          <h2>Serious surfaces should not run on bootstrap passwords.</h2>
          <p>
            Finish the password rotation once, then continue into the same internal ops workflow with a fresh session.
          </p>
        </div>
      </section>
    </main>
  );
}
