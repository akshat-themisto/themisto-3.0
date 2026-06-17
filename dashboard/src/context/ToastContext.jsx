/* eslint-disable react-refresh/only-export-components */
import { createContext, useCallback, useContext, useMemo, useState } from 'react';
import { AlertTriangle, CheckCircle2, Info, X, XCircle } from 'lucide-react';

const ToastContext = createContext(null);

let nextToastId = 1;

function ToastItem({ toast, onClose }) {
    const icon = toast.type === 'success'
        ? <CheckCircle2 size={16} />
        : toast.type === 'error'
            ? <XCircle size={16} />
            : toast.type === 'warning'
                ? <AlertTriangle size={16} />
                : <Info size={16} />;

    return (
        <div className={`toast toast-${toast.type || 'info'}`}>
            <div className="toast-icon">{icon}</div>
            <div className="toast-content">
                {toast.title && <div className="toast-title">{toast.title}</div>}
                <div className="toast-message">{toast.message}</div>
            </div>
            <button
                type="button"
                className="toast-close"
                onClick={() => onClose(toast.id)}
                aria-label="Close notification"
            >
                <X size={14} />
            </button>
        </div>
    );
}

export function ToastProvider({ children }) {
    const [toasts, setToasts] = useState([]);

    const dismiss = useCallback((id) => {
        setToasts((prev) => prev.filter((t) => t.id !== id));
    }, []);

    const push = useCallback((payload) => {
        const id = nextToastId++;
        const toast = {
            id,
            type: payload?.type || 'info',
            title: payload?.title || '',
            message: payload?.message || '',
            duration: Math.max(1200, Number(payload?.duration) || 3200),
        };

        setToasts((prev) => [...prev, toast]);
        window.setTimeout(() => {
            setToasts((prev) => prev.filter((t) => t.id !== id));
        }, toast.duration);

        return id;
    }, []);

    const api = useMemo(() => ({
        push,
        success: (message, title = 'Success', duration) => push({ type: 'success', title, message, duration }),
        error: (message, title = 'Error', duration) => push({ type: 'error', title, message, duration }),
        warning: (message, title = 'Warning', duration) => push({ type: 'warning', title, message, duration }),
        info: (message, title = 'Info', duration) => push({ type: 'info', title, message, duration }),
    }), [push]);

    return (
        <ToastContext.Provider value={api}>
            {children}
            <div className="toast-viewport" aria-live="polite" aria-atomic="true">
                {toasts.map((toast) => (
                    <ToastItem key={toast.id} toast={toast} onClose={dismiss} />
                ))}
            </div>
        </ToastContext.Provider>
    );
}

export function useToast() {
    const ctx = useContext(ToastContext);
    if (!ctx) {
        throw new Error('useToast must be used inside ToastProvider');
    }
    return ctx;
}

