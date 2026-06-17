import { Navigate, Routes, Route } from 'react-router-dom';
import Layout from './components/Layout';
import Home from './pages/Home';
import Extensions from './pages/Extensions';
import Settings from './pages/Settings';
import Enrollment from './pages/Enrollment';
import Admin from './pages/Admin';
import { AdminSessionProvider, useAdminSession } from './hooks/useAdminSession';

function AppRoutes() {
  const { isAdmin } = useAdminSession();

  return (
    <Routes>
      <Route element={<Layout />}>
        <Route path="/" element={<Home />} />
        <Route path="/extensions" element={<Extensions />} />
        <Route path="/settings" element={<Settings />} />
        <Route path="/enrollment" element={<Enrollment />} />
        <Route path="/admin" element={isAdmin ? <Admin /> : <Navigate replace to="/" />} />
      </Route>
    </Routes>
  );
}

export default function App() {
  return (
    <AdminSessionProvider>
      <AppRoutes />
    </AdminSessionProvider>
  );
}
