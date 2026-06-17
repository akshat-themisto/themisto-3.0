import { NavLink } from 'react-router-dom';
import { Home, Globe, MonitorSmartphone, ShieldCheck } from 'lucide-react';
import { useAdminSession } from '../hooks/useAdminSession';

export default function Sidebar() {
  const { isAdmin } = useAdminSession();
  const links = [
    { to: '/', icon: <Home size={18} />, label: 'Runtime' },
    { to: '/extensions', icon: <Globe size={18} />, label: 'Browsers' },
    { to: '/settings', icon: <MonitorSmartphone size={18} />, label: 'Device' },
  ];

  if (isAdmin) {
    links.push({ to: '/admin', icon: <ShieldCheck size={18} />, label: 'Admin' });
  }

  return (
    <aside className="sidebar" data-tour="desktop-sidebar">
      <nav className="sidebar-nav">
        {links.map((link) => (
          <NavLink
            key={link.to}
            to={link.to}
            end={link.to === '/'}
            className={({ isActive }) => `nav-item${isActive ? ' active' : ''}`}
          >
            {link.icon}
            <span>{link.label}</span>
          </NavLink>
        ))}
      </nav>
    </aside>
  );
}
