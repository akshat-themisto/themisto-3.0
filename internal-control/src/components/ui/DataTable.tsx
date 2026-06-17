import type { ReactNode } from "react";

type DataTableProps = {
  title?: string;
  description?: string;
  actions?: ReactNode;
  children: ReactNode;
};

export function DataTable({ title, description, actions, children }: DataTableProps) {
  return (
    <section className="table-shell">
      {title ? (
        <div className="card-title-row">
          <div>
            <h3>{title}</h3>
            {description ? <p>{description}</p> : null}
          </div>
          {actions}
        </div>
      ) : null}
      <div style={{ overflowX: "auto" }}>{children}</div>
    </section>
  );
}

