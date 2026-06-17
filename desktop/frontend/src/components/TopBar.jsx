import { LockKeyhole, ShieldCheck } from 'lucide-react';
import { useAdminSession } from '../hooks/useAdminSession';

export default function TopBar() {
  const { isAdmin, openGate, lock } = useAdminSession();

  return (
    <header className="topbar">
      <div className="topbar-brand" data-tour="desktop-brand">
        <img className="brand-logo" src="/transparent-logo.png" alt="Themisto Labs logo" />
        <div className="brand-lockup">
          <span className="brand-name">Themisto Labs</span>
          <span className="brand-subtitle">Endpoint</span>
        </div>
        <span className={`brand-pill ${isAdmin ? 'brand-pill-admin' : ''}`}>{isAdmin ? 'Admin' : 'User'}</span>
      </div>
      <div className="topbar-actions topbar-actions-shell">
        {isAdmin ? (
          <button className="btn topbar-session-btn" type="button" onClick={lock} data-tour="desktop-admin-toggle">
            <LockKeyhole size={14} />
            Lock Admin Mode
          </button>
        ) : (
          <button className="btn topbar-session-btn" type="button" onClick={openGate} data-tour="desktop-admin-toggle">
            <ShieldCheck size={14} />
            Unlock Admin Mode
          </button>
        )}
      </div>
    </header>
  );
}
