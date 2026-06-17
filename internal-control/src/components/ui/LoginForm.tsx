"use client";

import { useActionState } from "react";
import { loginAction } from "@/server/actions/auth";

export function LoginForm() {
  const [state, action, pending] = useActionState(loginAction, undefined);
  const defaultEmail = process.env.NODE_ENV === "development" ? "admin@themisto.local" : "";
  const defaultPassword = process.env.NODE_ENV === "development" ? "ChangeMe!123" : "";

  return (
    <form action={action} className="login-box">
      <label className="form-label">
        Work email
        <input className="input" type="email" name="email" defaultValue={defaultEmail} autoComplete="username" required />
      </label>
      <label className="form-label">
        Password
        <input className="input" type="password" name="password" defaultValue={defaultPassword} autoComplete="current-password" required />
      </label>
      {state?.error ? <div className="badge danger">{state.error}</div> : null}
      <button className="btn btn-primary" type="submit" disabled={pending}>{pending ? "Signing in..." : "Sign in to control plane"}</button>
      <div className="form-note">This surface is for Themisto staff only. Every action is audited.</div>
    </form>
  );
}

