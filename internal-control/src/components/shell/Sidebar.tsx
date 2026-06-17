"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { ActivitySquare, Building2, CircleUserRound, LayoutDashboard, ReceiptText, ShieldBan, Users } from "lucide-react";
import { cn } from "@/lib/format";

type SidebarProps = {
  staffName: string;
  role: string;
};

const navItems = [
  { href: "/", label: "Overview", icon: LayoutDashboard },
  { href: "/organizations", label: "Organizations", icon: ShieldBan },
  { href: "/audit", label: "Audit Log", icon: ReceiptText },
  { href: "/customers", label: "Customers", icon: Building2 },
  { href: "/staff", label: "Staff", icon: Users }
];

export function Sidebar({ staffName, role }: SidebarProps) {
  const pathname = usePathname();

  return (
    <aside className="sidebar">
      <div className="brand-block">
        <div className="brand-mark">
          <img src="/transparent-logo.png" alt="Themisto" />
        </div>
        <div className="brand-copy">
          <h1>Themisto</h1>
          <p>Internal Control</p>
        </div>
      </div>

      <div className="role-pill">{role}</div>

      <div>
        <div className="sidebar-section-title">Command Surface</div>
        <nav className="sidebar-nav">
          {navItems.map((item) => {
            const Icon = item.icon;
            const isActive = item.href === "/" ? pathname === "/" : pathname.startsWith(item.href);
            return (
              <Link key={item.href} className={cn("nav-link", isActive && "active")} href={item.href}>
                <Icon size={20} />
                <span>{item.label}</span>
              </Link>
            );
          })}
        </nav>
      </div>

      <div className="sidebar-footer">
        <div className="sidebar-section-title">Signed In</div>
        <div className="brand-block" style={{ gap: 12 }}>
          <div className="brand-mark" style={{ width: 48, height: 48, borderRadius: 14, padding: 10 }}>
            <CircleUserRound size={26} />
          </div>
          <div>
            <div style={{ fontWeight: 700 }}>{staffName}</div>
            <div style={{ color: "var(--text-muted)", marginTop: 4 }}>Themisto operator</div>
          </div>
        </div>
      </div>
    </aside>
  );
}

