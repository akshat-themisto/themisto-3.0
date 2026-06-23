import { BrowserRouter, Navigate, Outlet, Route, Routes } from 'react-router-dom';
import { OperatorAuthProvider, useOperatorAuth } from './context/OperatorAuthContext';
import OperatorLayout from './components/OperatorLayout';
import OperatorLogin from './pages/OperatorLogin';
import FleetMonitoring from './pages/FleetMonitoring';

function ProtectedOperations() {
  const { operator, loading } = useOperatorAuth();
  if (loading) return <div className="loading-wrap"><div className="spinner" /></div>;
  return operator ? <Outlet /> : <Navigate to="/login" replace />;
}

export default function OpsApp() {
  return (
    <OperatorAuthProvider>
      <BrowserRouter>
        <Routes>
          <Route path="/login" element={<OperatorLogin />} />
          <Route element={<ProtectedOperations />}>
            <Route element={<OperatorLayout />}>
              <Route index element={<FleetMonitoring />} />
            </Route>
          </Route>
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </BrowserRouter>
    </OperatorAuthProvider>
  );
}
