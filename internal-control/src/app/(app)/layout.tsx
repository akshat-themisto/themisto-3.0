import type { ReactNode } from "react";
import { redirect } from "next/navigation";
import { Sidebar } from "@/components/shell/Sidebar";
import { Topbar } from "@/components/shell/Topbar";
import { requireStaff } from "@/server/auth/session";

export const dynamic = "force-dynamic";

export default async function AppLayout({ children }: { children: ReactNode }) {
  const staff = await requireStaff();
  if (staff.must_change_password) {
    redirect("/change-password");
  }

  return (
    <div className="app-shell">
      <Sidebar staffName={staff.name} role={staff.role} />
      <div className="layout-content">
        <Topbar title="Themisto Control" description="Internal authority surface for customer lifecycle and trust state." role={staff.role} />
        <main className="page-wrap">{children}</main>
      </div>
    </div>
  );
}

