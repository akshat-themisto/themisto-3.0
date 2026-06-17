import type { ReactNode } from "react";

type StatCardProps = {
  label: string;
  value: ReactNode;
  meta?: ReactNode;
};

export function StatCard({ label, value, meta }: StatCardProps) {
  return (
    <section className="stat-card">
      <div className="label">{label}</div>
      <div className="value">{value}</div>
      {meta ? <div className="meta">{meta}</div> : null}
    </section>
  );
}

