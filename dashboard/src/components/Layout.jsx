import { NavLink, Outlet } from 'react-router-dom';
import { useAuth } from '../context/AuthContext';
import { Activity, Bot, BriefcaseBusiness, LayoutDashboard, LogOut, ScrollText, Settings, Shield, ShieldAlert, Users } from 'lucide-react';
import AlertBell from './AlertBell';
import OnboardingExperience from './OnboardingExperience';
import TermsAcceptanceModal from './TermsAcceptanceModal';

const links = [
    { to: '/', icon: <LayoutDashboard size={18} />, label: 'Overview', tourId: 'dashboard-nav-overview' },
    { to: '/devices', icon: <Users size={18} />, label: 'Devices' },
    { to: '/audit', icon: <ScrollText size={18} />, label: 'Activity Log' },
    { to: '/policies', icon: <Shield size={18} />, label: 'Policies' },
    { to: '/telemetry', icon: <Activity size={18} />, label: 'Signals' },
    { to: '/ai-usage', icon: <Bot size={18} />, label: 'AI Usage' },
    { to: '/dlp', icon: <ShieldAlert size={18} />, label: 'Security Events' },
    { to: '/settings', icon: <Settings size={18} />, label: 'Settings', tourId: 'dashboard-nav-settings' },
];

function roleLabel(role) {
    if (!role) return 'Viewer';
    return String(role).charAt(0).toUpperCase() + String(role).slice(1);
}

export default function Layout() {
    const { user, logout } = useAuth();

    return (
        <div className="app-layout">
            <header className="shell-topbar">
                <div className="shell-brand" data-tour="dashboard-brand">
                    <div className="shell-brand-icon">
                        <img src="/logo.png" alt="Themisto Labs Logo" />
                    </div>
                    <div className="shell-brand-copy">
                        <span className="shell-brand-name">Themisto Labs</span>
                        <span className="shell-brand-subtitle">Enterprise AI Governance</span>
                    </div>
                    <span className="shell-brand-pill">{roleLabel(user?.role)}</span>
                </div>

                <div className="shell-topbar-actions">
                    <div data-tour="dashboard-alerts">
                        <AlertBell />
                    </div>
                </div>
            </header>

            <div className="app-body">
                <aside className="sidebar" data-tour="dashboard-sidebar">
                    <nav className="sidebar-nav">
                        <div className="sidebar-section-label">
                            Security Ops
                        </div>
                        {links.map((l) => (
                            <NavLink key={l.to} to={l.to} end={l.to === '/'} data-tour={l.tourId} className={({ isActive }) => `nav-link${isActive ? ' active' : ''}`}>
                                <span className="nav-icon">{l.icon}</span>
                                {l.label}
                            </NavLink>
                        ))}
                    </nav>

                    <div className="sidebar-footer">
                        <div className="sidebar-user">
                            <div className="sidebar-user-avatar">
                                <BriefcaseBusiness size={16} />
                            </div>
                            <div className="sidebar-user-info">
                                <div className="sidebar-user-name">{user?.name || 'Operator'}</div>
                                <div className="sidebar-user-role">{user?.role || 'viewer'}</div>
                            </div>
                        </div>
                        <button className="nav-link sidebar-logout" onClick={logout}>
                            <span className="nav-icon"><LogOut size={18} /></span>
                            Sign Out
                        </button>
                    </div>
                </aside>

                <div className="main-content">
                    <Outlet />
                </div>
            </div>
            <OnboardingExperience disabled={Boolean(user?.must_change_password || !user?.terms_accepted_at)} />
            <TermsAcceptanceModal />
        </div>
    );
}
