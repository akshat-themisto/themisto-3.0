import { BrowserRouter, Routes, Route, Navigate, useLocation } from 'react-router-dom';
import { AuthProvider, useAuth } from './context/AuthContext';
import { ToastProvider } from './context/ToastContext';
import Layout from './components/Layout';
import Login from './pages/Login';
import Overview from './pages/Overview';
import Devices from './pages/Devices';
import AuditLog from './pages/AuditLog';
import Policies from './pages/Policies';
import Telemetry from './pages/Telemetry';
import Settings from './pages/Settings';
import AIUsage from './pages/AIUsage';
import DLPEvents from './pages/DLPEvents';
import OperatorControl from './pages/OperatorControl';

function ProtectedRoute({ children }) {
  const { user, loading } = useAuth();
  const location = useLocation();
  if (loading) return <div className="loading-wrap"><div className="spinner" /></div>;
  if (!user) return <Navigate to="/login" replace />;
  if (user.must_change_password && location.pathname !== '/settings') {
    return <Navigate to="/settings" replace />;
  }
  return children;
}

function App() {
  return (
    <AuthProvider>
      <ToastProvider>
        <BrowserRouter>
          <Routes>
            <Route path="/login" element={<Login />} />
            <Route path="/" element={<ProtectedRoute><Layout /></ProtectedRoute>}>
              <Route index element={<Overview />} />
              <Route path="devices" element={<Devices />} />
              <Route path="audit" element={<AuditLog />} />
              <Route path="policies" element={<Policies />} />
              <Route path="telemetry" element={<Telemetry />} />
              <Route path="settings" element={<Settings />} />
              <Route path="ai-usage" element={<AIUsage />} />
              <Route path="dlp" element={<DLPEvents />} />
              <Route path="operator" element={<OperatorControl />} />
            </Route>
          </Routes>
        </BrowserRouter>
      </ToastProvider>
    </AuthProvider>
  );
}

export default App;
