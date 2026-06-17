import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { useNavigate } from 'react-router-dom';
import { Bell, CircleAlert } from 'lucide-react';
import { api } from '../api/client';
import { useAuth } from '../context/AuthContext';

function severityColor(severity) {
    switch ((severity || '').toLowerCase()) {
        case 'critical':
            return '#ff7a70';
        case 'high':
            return '#ff9a7a';
        case 'medium':
            return '#c9c9d1';
        default:
            return '#f5f5f5';
    }
}

function actionLabel(action) {
    return (action || 'alert').toLowerCase() === 'block' ? 'Blocked' : 'Alert';
}

function joinParts(parts) {
    return parts.filter(Boolean).join(' | ');
}

export default function AlertBell() {
    const { user } = useAuth();
    const navigate = useNavigate();
    const isAdmin = user?.role === 'admin' || user?.role === 'owner';

    const [open, setOpen] = useState(false);
    const [alerts, setAlerts] = useState([]);
    const [unread, setUnread] = useState(0);
    const [loading, setLoading] = useState(false);
    const [menuPosition, setMenuPosition] = useState({ top: 0, right: 0 });
    const rootRef = useRef(null);
    const menuRef = useRef(null);

    const refresh = useCallback(async () => {
        if (!isAdmin) return;
        setLoading(true);
        try {
            const res = await api.listAlerts({ page: 1, limit: 20 });
            setAlerts(res?.alerts || []);
            setUnread(res?.unread || 0);
        } catch (err) {
            console.error('Failed to load alerts', err);
        } finally {
            setLoading(false);
        }
    }, [isAdmin]);

    useEffect(() => {
        if (!isAdmin) return undefined;
        refresh();
        const timer = window.setInterval(refresh, 15000);
        return () => window.clearInterval(timer);
    }, [isAdmin, refresh]);

    useEffect(() => {
        if (!open) return undefined;
        const onClick = (event) => {
            if (!rootRef.current?.contains(event.target) && !menuRef.current?.contains(event.target)) {
                setOpen(false);
            }
        };
        window.addEventListener('mousedown', onClick);
        return () => window.removeEventListener('mousedown', onClick);
    }, [open]);

    const updateMenuPosition = useCallback(() => {
        const rect = rootRef.current?.getBoundingClientRect();
        if (!rect) return;
        setMenuPosition({
            top: Math.round(rect.bottom + 10),
            right: Math.max(16, Math.round(window.innerWidth - rect.right)),
        });
    }, []);

    useEffect(() => {
        if (!open) return undefined;
        updateMenuPosition();
        window.addEventListener('resize', updateMenuPosition);
        window.addEventListener('scroll', updateMenuPosition, true);
        return () => {
            window.removeEventListener('resize', updateMenuPosition);
            window.removeEventListener('scroll', updateMenuPosition, true);
        };
    }, [open, updateMenuPosition]);

    const unreadIDs = useMemo(() => alerts.filter((alert) => alert.unread).map((alert) => alert.event_id), [alerts]);

    const markRead = async (eventIDs) => {
        if (!eventIDs?.length) return;
        try {
            await api.markAlertsRead(eventIDs);
            setAlerts((prev) => prev.map((alert) => (eventIDs.includes(alert.event_id) ? { ...alert, unread: false } : alert)));
            setUnread((prev) => Math.max(0, prev - eventIDs.length));
        } catch (err) {
            console.error('Failed to mark alerts read', err);
        }
    };

    const openAlert = async (alert) => {
        if (!alert?.event_id) return;
        if (alert.unread) {
            await markRead([alert.event_id]);
        }
        setOpen(false);
        navigate(`/dlp?event_id=${alert.event_id}&open=1`);
    };

    if (!isAdmin) return null;

    const menu = open ? createPortal(
        <div
            ref={menuRef}
            style={{
                width: 420,
                maxWidth: 'min(420px, calc(100vw - 32px))',
                maxHeight: 'min(70vh, calc(100vh - 88px))',
                overflow: 'auto',
                borderRadius: 12,
                border: '1px solid var(--border-light)',
                background: 'linear-gradient(180deg, rgba(18, 18, 20, 0.99), rgba(8, 8, 9, 0.99))',
                boxShadow: '0 24px 70px rgba(0,0,0,0.58)',
                position: 'fixed',
                top: menuPosition.top,
                right: menuPosition.right,
                zIndex: 6000,
            }}
        >
            <div
                style={{
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'space-between',
                    padding: '12px 12px 10px',
                    borderBottom: '1px solid var(--border)',
                }}
            >
                <div style={{ fontSize: 13, color: 'var(--text-primary)', fontWeight: 700 }}>Notifications</div>
                <button
                    className="btn btn-sm btn-secondary"
                    style={{ height: 28, padding: '0 10px' }}
                    disabled={!unreadIDs.length}
                    onClick={() => markRead(unreadIDs)}
                >
                    Mark all read
                </button>
            </div>

            {loading && alerts.length === 0 ? (
                <div style={{ padding: 14, color: 'var(--text-muted)', fontSize: 13 }}>Loading notifications...</div>
            ) : alerts.length === 0 ? (
                <div style={{ padding: 14, color: 'var(--text-muted)', fontSize: 13 }}>No notifications yet.</div>
            ) : (
                <div>
                    {alerts.map((alert) => {
                        const title = joinParts([alert.request_host, alert.reason_detail]);
                        const meta = joinParts([
                            alert.source_app || 'unknown app',
                            alert.ai_vendor,
                            alert.unread ? 'unread' : '',
                        ]);

                        return (
                            <button
                                key={alert.event_id}
                                onClick={() => openAlert(alert)}
                                style={{
                                    width: '100%',
                                    textAlign: 'left',
                                    background: alert.unread ? 'rgba(255, 255, 255, 0.035)' : 'transparent',
                                    border: 'none',
                                    borderBottom: '1px solid rgba(255, 255, 255, 0.06)',
                                    color: 'var(--text-primary)',
                                    padding: '10px 12px',
                                    cursor: 'pointer',
                                    display: 'block',
                                }}
                            >
                                <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 10 }}>
                                    <div style={{ display: 'inline-flex', alignItems: 'center', gap: 8 }}>
                                        <CircleAlert size={14} color={severityColor(alert.severity)} />
                                        <span style={{ fontSize: 12, fontWeight: 700 }}>{actionLabel(alert.action_taken)}</span>
                                        <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>{(alert.severity || 'low').toUpperCase()}</span>
                                    </div>
                                    <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>
                                        {new Date(alert.timestamp).toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' })}
                                    </span>
                                </div>
                                <div style={{ marginTop: 6, fontSize: 12, color: 'var(--text-secondary)' }}>
                                    {title || 'Policy notification'}
                                </div>
                                <div style={{ marginTop: 4, fontSize: 11, color: 'var(--text-muted)' }}>
                                    {meta}
                                </div>
                            </button>
                        );
                    })}
                </div>
            )}
        </div>,
        document.body,
    ) : null;

    return (
        <div
            className="alert-bell-root"
            ref={rootRef}
            style={{
                position: 'relative',
                zIndex: 2200,
            }}
        >
            <button
                className="btn btn-secondary"
                onClick={() => {
                    const nextOpen = !open;
                    if (nextOpen) {
                        updateMenuPosition();
                    }
                    setOpen(nextOpen);
                    if (nextOpen) {
                        refresh();
                    }
                }}
                style={{
                    height: 36,
                    width: 36,
                    borderRadius: 999,
                    padding: 0,
                    display: 'inline-flex',
                    alignItems: 'center',
                    justifyContent: 'center',
                    position: 'relative',
                    background: 'rgba(255, 255, 255, 0.04)',
                    border: '1px solid rgba(255, 255, 255, 0.1)',
                    color: 'rgba(255, 255, 255, 0.95)',
                }}
                aria-label="Open notifications"
            >
                <Bell size={16} />
                {unread > 0 && (
                    <span
                        style={{
                            position: 'absolute',
                            top: -4,
                            right: -4,
                            minWidth: 18,
                            height: 18,
                            borderRadius: 999,
                            background: 'var(--danger)',
                            color: '#20120e',
                            fontSize: 10,
                            fontWeight: 700,
                            display: 'inline-flex',
                            alignItems: 'center',
                            justifyContent: 'center',
                            padding: '0 4px',
                        }}
                    >
                        {unread > 99 ? '99+' : unread}
                    </span>
                )}
            </button>
            {menu}
        </div>
    );
}
