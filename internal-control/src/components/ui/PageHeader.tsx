import type { ReactNode } from "react";

type PageHeaderProps = {
  kicker: string;
  title: string;
  description: string;
  actions?: ReactNode;
};

export function PageHeader({ kicker, title, description, actions }: PageHeaderProps) {
  return (
    <div className="page-header">
      <div>
        <div className="page-kicker">{kicker}</div>
        <h1>{title}</h1>
        <p>{description}</p>
      </div>
      {actions ? <div className="inline-actions">{actions}</div> : null}
    </div>
  );
}

