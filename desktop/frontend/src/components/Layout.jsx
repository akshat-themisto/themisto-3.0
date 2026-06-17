import { Outlet, useNavigate } from 'react-router-dom';
import Sidebar from './Sidebar';
import TopBar from './TopBar';
import StatusBar from './StatusBar';
import AdminGate from './AdminGate';
import { useAdminSession } from '../hooks/useAdminSession';
import OnboardingExperience from './OnboardingExperience';

export default function Layout() {
  const navigate = useNavigate();
  const { gateOpen, closeGate, authorize } = useAdminSession();

  return (
    <>
      <div className="app-shell">
        <TopBar />
        <div className="app-body">
          <Sidebar />
          <div className="main-content">
            <Outlet />
          </div>
        </div>
        <StatusBar />
      </div>
      {gateOpen && (
        <AdminGate
          title="Unlock Admin Mode"
          description="Unlock policy rollout, audit visibility, and troubleshooting guidance for this desktop session."
          onAuthorized={(token) => {
            authorize(token);
            navigate('/admin');
          }}
          onCancel={closeGate}
        />
      )}
      <OnboardingExperience disabled={gateOpen} />
    </>
  );
}
