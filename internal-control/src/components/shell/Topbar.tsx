import { BellDot, LogOut, ShieldCheck } from "lucide-react";
import { logoutAction } from "@/server/actions/auth";

type TopbarProps = {
  title: string;
  description: string;
  role: string;
};

export function Topbar({ title, description, role }: TopbarProps) {
  return (
    <header className="topbar">
      <div>
        <p className="topbar-eyebrow">Internal Ops</p>
        <h2>{title}</h2>
        <p>{description}</p>
      </div>
      <div className="topbar-actions">
        <div className="chip">
          <ShieldCheck size={16} />
          {role}
        </div>
        <div className="chip">
          <BellDot size={16} />
          Staff Only
        </div>
        <form action={logoutAction}>
          <button className="btn btn-secondary btn-small" type="submit">
            <LogOut size={16} />
            Sign out
          </button>
        </form>
      </div>
    </header>
  );
}

