"use client";

import { useState, type ReactNode } from "react";

type DangerDialogProps = {
  triggerLabel: string;
  title: string;
  description: string;
  triggerClassName?: string;
  children: ReactNode;
};

export function DangerDialog({ triggerLabel, title, description, triggerClassName = "btn btn-danger btn-small", children }: DangerDialogProps) {
  const [open, setOpen] = useState(false);

  return (
    <>
      <button className={triggerClassName} type="button" onClick={() => setOpen(true)}>{triggerLabel}</button>
      {open ? (
        <div className="modal-backdrop" onClick={() => setOpen(false)}>
          <div className="modal-card" onClick={(event) => event.stopPropagation()}>
            <h3>{title}</h3>
            <p>{description}</p>
            {children}
            <div className="modal-actions">
              <button className="btn btn-ghost" type="button" onClick={() => setOpen(false)}>Close</button>
            </div>
          </div>
        </div>
      ) : null}
    </>
  );
}

