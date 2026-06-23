import { Activity, LogOut, ShieldCheck } from 'lucide-react';
import { NavLink, Outlet, useNavigate } from 'react-router-dom';
import { useOperatorAuth } from '../context/OperatorAuthContext';

export default function OperatorLayout() {
    const { operator, logout } = useOperatorAuth();
    const navigate = useNavigate();

    async function signOut() {
        await logout();
        navigate('/login', { replace: true });
    }

    return (
        <div className="app-layout operator-layout">
            <header className="shell-topbar">
                <div className="shell-brand">
                    <div className="shell-brand-icon"><img src="/logo.png" alt="Themisto Labs" /></div>
                    <div className="shell-brand-copy"><span className="shell-brand-name">Themisto Labs</span><span className="shell-brand-subtitle">Customer Operations</span></div>
                    <span className="shell-brand-pill">Restricted</span>
                </div>
                <div className="operator-session-state"><ShieldCheck size={15} /><span>{operator?.actor_id || 'Operator session'}</span></div>
            </header>
            <div className="app-body">
                <aside className="sidebar">
                    <nav className="sidebar-nav">
                        <div className="sidebar-section-label">Themisto Operations</div>
                        <NavLink className={({ isActive }) => `nav-link${isActive ? ' active' : ''}`} to="/" end><Activity size={18} /><span>Fleet Monitor</span></NavLink>
                    </nav>
                    <div className="sidebar-footer">
                        <button className="nav-link sidebar-logout" onClick={signOut}><LogOut size={18} /><span>Operator Sign Out</span></button>
                    </div>
                </aside>
                <main className="main-content"><Outlet /></main>
            </div>
        </div>
    );
}
