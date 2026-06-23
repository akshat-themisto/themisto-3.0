/* eslint-disable react-refresh/only-export-components */
import { createContext, useContext, useState, useEffect } from 'react';
import { api } from '../api/client';

const AuthContext = createContext(null);

export function AuthProvider({ children }) {
    const [user, setUser] = useState(null);
    const [loading, setLoading] = useState(true);

    const refreshUser = () => {
        return api.me().then(setUser).catch(() => setUser(null));
    };

    useEffect(() => {
        localStorage.removeItem('themisto_operator_api_key');
        refreshUser()
            .finally(() => setLoading(false));
    }, []);

    useEffect(() => {
        if (!user) return;

        const revalidate = () => {
            refreshUser().catch(() => { });
        };

        const handleVisibilityChange = () => {
            if (document.visibilityState === 'visible') {
                revalidate();
            }
        };

        window.addEventListener('focus', revalidate);
        document.addEventListener('visibilitychange', handleVisibilityChange);

        return () => {
            window.removeEventListener('focus', revalidate);
            document.removeEventListener('visibilitychange', handleVisibilityChange);
        };
    }, [user]);

    const login = async (email, password) => {
        const data = await api.login(email, password);
        setUser(data.user);
        return data;
    };

    const logout = async () => {
        await api.logout().catch(() => { });
        setUser(null);
    };

    const acceptTerms = async () => {
        const data = await api.acceptTerms();
        const nextUser = data?.user || data;
        setUser(nextUser);
        return nextUser;
    };

    return (
        <AuthContext.Provider value={{ user, loading, login, logout, refreshUser, acceptTerms }}>
            {children}
        </AuthContext.Provider>
    );
}

export function useAuth() {
    const ctx = useContext(AuthContext);
    if (!ctx) throw new Error('useAuth must be used within AuthProvider');
    return ctx;
}
