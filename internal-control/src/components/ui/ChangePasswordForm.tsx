"use client";

import { useActionState } from "react";
import { changePasswordAction, logoutAction } from "@/server/actions/auth";

export function ChangePasswordForm({ email }: { email: string }) {
  const [state, action, pending] = useActionState(changePasswordAction, undefined);

  return (
    <div className="stack">
      <form action={action} className="login-box form-grid">
        <div>
          <div className="sidebar-section-title">Password rotation</div>
          <div style={{ fontWeight: 700, fontSize: "1.05rem", marginTop: 8 }}>{email}</div>
          <div className="form-note" style={{ marginTop: 8 }}>
            This account still has a temporary or operator-assigned password. Rotate it before using the control plane.
          </div>
        </div>

        <label className="form-label">
          Current password
          <input className="input" type="password" name="currentPassword" autoComplete="current-password" required />
        </label>

        <label className="form-label">
          New password
          <input className="input" type="password" name="newPassword" autoComplete="new-password" minLength={12} required />
        </label>

        <label className="form-label">
          Confirm new password
          <input className="input" type="password" name="confirmPassword" autoComplete="new-password" minLength={12} required />
        </label>

        {state?.error ? <div className="badge danger">{state.error}</div> : null}

        <button className="btn btn-primary" type="submit" disabled={pending}>
          {pending ? "Updating password..." : "Rotate password and continue"}
        </button>
        <div className="form-note">Use a unique password or passphrase with at least 12 characters.</div>
      </form>

      <form action={logoutAction}>
        <button className="btn btn-ghost" type="submit">Sign out instead</button>
      </form>
    </div>
  );
}
