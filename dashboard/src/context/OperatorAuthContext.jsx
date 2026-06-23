/* eslint-disable react-refresh/only-export-components */
import { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react';
import { operatorApi } from '../api/operatorClient';

const OperatorAuthContext = createContext(null);

export function OperatorAuthProvider({ children }) {
    const [operator, setOperator] = useState(null);
    const [loading, setLoading] = useState(true);

    const refresh = useCallback(async () => {
        setLoading(true);
        try {
            const data = await operatorApi.me();
            setOperator(data);
            return data;
        } catch {
            setOperator(null);
            return null;
        } finally {
            setLoading(false);
        }
    }, []);

    useEffect(() => {
        localStorage.removeItem('themisto_operator_api_key');
        refresh();
    }, [refresh]);

    const login = useCallback(async (accessKey) => {
        await operatorApi.login(accessKey);
        return refresh();
    }, [refresh]);

    const logout = useCallback(async () => {
        try {
            await operatorApi.logout();
        } finally {
            setOperator(null);
        }
    }, []);

    const value = useMemo(() => ({ operator, loading, login, logout, refresh }), [operator, loading, login, logout, refresh]);
    return <OperatorAuthContext.Provider value={value}>{children}</OperatorAuthContext.Provider>;
}

export function useOperatorAuth() {
    const context = useContext(OperatorAuthContext);
    if (!context) throw new Error('useOperatorAuth must be used inside OperatorAuthProvider');
    return context;
}
