"use client";

import { useState, type ReactNode } from "react";

type ConfirmDialogProps = {
  triggerLabel: string;
  title: string;
  description: string;
  confirmLabel?: string;
  triggerClassName?: string;
  children?: ReactNode;
};

export function ConfirmDialog({ triggerLabel, title, description, confirmLabel = "Save", triggerClassName = "btn btn-secondary btn-small", children }: ConfirmDialogProps) {
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
              <button className="btn btn-ghost" type="button" onClick={() => setOpen(false)}>Cancel</button>
              <button className="btn btn-primary" type="button" onClick={() => setOpen(false)}>{confirmLabel}</button>
            </div>
          </div>
        </div>
      ) : null}
    </>
  );
}

