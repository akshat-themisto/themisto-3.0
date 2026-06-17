import { createContext, useContext, useMemo, useState } from 'react';

const AdminSessionContext = createContext(null);

export function AdminSessionProvider({ children }) {
  const [adminToken, setAdminToken] = useState('');
  const [gateOpen, setGateOpen] = useState(false);

  const value = useMemo(() => ({
    adminToken,
    isAdmin: adminToken.length > 0,
    gateOpen,
    openGate() {
      setGateOpen(true);
    },
    closeGate() {
      setGateOpen(false);
    },
    authorize(token) {
      setAdminToken(token);
      setGateOpen(false);
    },
    lock() {
      setAdminToken('');
      setGateOpen(false);
    },
  }), [adminToken, gateOpen]);

  return (
    <AdminSessionContext.Provider value={value}>
      {children}
    </AdminSessionContext.Provider>
  );
}

export function useAdminSession() {
  const context = useContext(AdminSessionContext);
  if (!context) {
    throw new Error('useAdminSession must be used inside AdminSessionProvider');
  }
  return context;
}
