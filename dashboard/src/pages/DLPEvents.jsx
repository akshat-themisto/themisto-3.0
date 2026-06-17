import { useCallback, useEffect, useMemo, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { api } from '../api/client';
import { useAuth } from '../context/AuthContext';
import { useToast } from '../context/ToastContext';
import { AlertTriangle, Code, Eye, Key, Shield, Tag } from 'lucide-react';

const DLP_MODE_RULE_NAME = 'Quick: Corporate DLP Baseline';
const DLP_MODE_RULE_PRIORITY = 6;
const ACCENT_COLOR = '#f5f5f5';
const ACCENT_SOFT = '#d8d8df';
const WARNING_COLOR = '#c9c9d1';
const DANGER_COLOR = '#ff7a70';
const SUCCESS_COLOR = '#74c89e';
const MUTED_COLOR = '#8b98aa';

const MATCH_TYPE_ICONS = {
    pii: <Shield size={14} color={WARNING_COLOR} />,
    credentials: <Key size={14} color={DANGER_COLOR} />,
    source_code: <Code size={14} color={ACCENT_SOFT} />,
    keyword: <Tag size={14} color={SUCCESS_COLOR} />,
};

const MATCH_TYPE_LABELS = {
    pii: 'PII',
    credentials: 'Credentials',
    source_code: 'Source Code',
    keyword: 'Keyword',
};

function normalize(v) {
    return String(v || '').trim().toLowerCase();
}

function isDLPMatchCondition(condition) {
    return normalize(condition?.field) === 'body_has_dlp_match'
        && normalize(condition?.operator) === 'eq'
        && normalize(condition?.value) === 'true'
        && !condition?.negate;
}

function findManagedModeRule(rules) {
    const list = Array.isArray(rules) ? rules : [];
    const byName = list.find((r) => normalize(r?.name) === normalize(DLP_MODE_RULE_NAME));
    if (byName) return byName;

    return list.find((r) => (r.conditions || []).some(isDLPMatchCondition));
}

function buildModeRulePayload(action) {
    return {
        name: DLP_MODE_RULE_NAME,
        priority: DLP_MODE_RULE_PRIORITY,
        enabled: true,
        action,
        block_reason: action === 'block'
            ? 'Sensitive content blocked by corporate DLP policy.'
            : 'Sensitive content detected by corporate DLP policy (alert mode).',
        conditions: [
            { field: 'body_has_dlp_match', operator: 'eq', value: 'true', negate: false },
        ],
    };
}

function MatchTypeBadge({ type }) {
    const icon = MATCH_TYPE_ICONS[type] || <AlertTriangle size={14} color={WARNING_COLOR} />;
    const label = MATCH_TYPE_LABELS[type] || type;

    return (
        <span style={{
            display: 'inline-flex',
            alignItems: 'center',
            gap: 6,
            padding: '4px 8px',
            borderRadius: 999,
            fontSize: 11,
            background: 'rgba(255,255,255,0.03)',
            border: '1px solid var(--border)',
            color: 'var(--text-secondary)',
            marginRight: 6,
            marginBottom: 4,
            whiteSpace: 'nowrap',
        }}>
            {icon}
            {label}
        </span>
    );
}

function OutcomeBadge({ action }) {
    const value = (action || 'alert').toLowerCase();
    if (value === 'block') {
        return <span className="badge" style={{ background: 'rgba(242,145,118,0.14)', borderColor: 'rgba(242,145,118,0.32)', color: DANGER_COLOR }}>Blocked</span>;
    }
    if (value === 'alert') {
        return <span className="badge" style={{ background: 'rgba(223,182,105,0.14)', borderColor: 'rgba(223,182,105,0.3)', color: WARNING_COLOR }}>Alerted</span>;
    }
    return <span className="badge">{value}</span>;
}

function PromptStatusBadge({ hasRequestBody }) {
    if (hasRequestBody) {
        return (
            <span style={{
                display: 'inline-flex',
                alignItems: 'center',
                gap: 6,
                padding: '4px 9px',
                borderRadius: 999,
                background: 'rgba(116,200,158,0.14)',
                color: SUCCESS_COLOR,
                border: '1px solid rgba(116,200,158,0.3)',
                fontSize: 11,
                fontWeight: 600,
            }}>
                Captured
            </span>
        );
    }

    return (
        <span style={{
            display: 'inline-flex',
            alignItems: 'center',
            gap: 6,
                padding: '4px 9px',
                borderRadius: 999,
                background: 'rgba(255,255,255,0.03)',
                color: MUTED_COLOR,
                border: '1px solid var(--border)',
                fontSize: 11,
                fontWeight: 600,
            }}>
            Not Captured
        </span>
    );
}

function PromptModal({ event, data, loading, error, onClose }) {
    if (!event) return null;

    return (
        <div style={{
            position: 'fixed',
            inset: 0,
            background: 'rgba(5, 5, 5, 0.82)',
            zIndex: 2000,
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            padding: 18,
            backdropFilter: 'blur(2px)',
        }}>
            <div style={{
                width: 'min(1040px, 96vw)',
                maxHeight: '92vh',
                overflow: 'hidden',
                borderRadius: 14,
                border: '1px solid var(--border-light)',
                background: 'linear-gradient(180deg, #171a21 0%, #11151b 100%)',
                boxShadow: '0 25px 80px rgba(0,0,0,0.55)',
                display: 'flex',
                flexDirection: 'column',
            }}>
                <div style={{
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'space-between',
                    padding: '14px 16px',
                    borderBottom: '1px solid var(--border)',
                    background: 'rgba(255,255,255,0.02)',
                }}>
                    <div>
                        <div style={{ fontSize: 15, fontWeight: 700, color: 'var(--text-primary)' }}>Captured Prompt Body</div>
                        <div style={{ marginTop: 4, fontSize: 12, color: 'var(--text-muted)' }}>
                            {event.request_host}  {event.request_method}  {event.source_app || 'unknown app'}
                        </div>
                    </div>
                    <button className="btn btn-secondary" onClick={onClose}>Close</button>
                </div>

                <div style={{ padding: 16, overflow: 'auto' }}>
                    {loading ? (
                        <div style={{ color: 'var(--text-muted)' }}>Loading prompt...</div>
                    ) : error ? (
                        <div style={{
                            color: '#ffd0c6',
                            background: 'rgba(242,145,118,0.12)',
                            border: '1px solid rgba(242,145,118,0.28)',
                            borderRadius: 10,
                            padding: 12,
                            fontSize: 13,
                        }}>
                            {error}
                        </div>
                    ) : (
                        <>
                            <div style={{
                                marginBottom: 12,
                                color: 'var(--text-muted)',
                                fontSize: 12,
                                display: 'flex',
                                gap: 10,
                                flexWrap: 'wrap',
                            }}>
                                <span>Event ID: {data?.id}</span>
                                <span>Time: {data?.timestamp ? new Date(data.timestamp).toLocaleString() : '-'}</span>
                                <span>{data?.truncated ? 'Truncated at capture limit' : 'Full capture available'}</span>
                            </div>

                            <pre style={{
                                margin: 0,
                                whiteSpace: 'pre-wrap',
                                wordBreak: 'break-word',
                                background: '#101319',
                                border: '1px solid var(--border)',
                                borderRadius: 10,
                                padding: 14,
                                color: 'var(--text-secondary)',
                                fontSize: 12,
                                lineHeight: 1.52,
                            }}>
                                {data?.body || '(No body stored for this event)'}
                            </pre>
                        </>
                    )}
                </div>
            </div>
        </div>
    );
}

function formatReason(e) {
    if (e.reason_detail && String(e.reason_detail).trim()) return e.reason_detail;
    if (e.reason_code && String(e.reason_code).trim()) return e.reason_code;
    return '-';
}

export default function DLPEvents() {
    const { user } = useAuth();
    const [searchParams, setSearchParams] = useSearchParams();
    const toast = useToast();
    const isAdmin = user?.role === 'admin' || user?.role === 'owner';

    const [summary, setSummary] = useState(null);
    const [events, setEvents] = useState([]);
    const [total, setTotal] = useState(0);
    const [page, setPage] = useState(1);
    const [loading, setLoading] = useState(true);
    const [loadError, setLoadError] = useState('');
    const [filters, setFilters] = useState({ host: '', ai_vendor: '', match_type: '' });

    const [selectedEvent, setSelectedEvent] = useState(null);
    const [promptData, setPromptData] = useState(null);
    const [promptLoading, setPromptLoading] = useState(false);
    const [promptError, setPromptError] = useState('');

    const [modeAction, setModeAction] = useState('alert');
    const [modeRuleId, setModeRuleId] = useState('');
    const [modeConfigured, setModeConfigured] = useState(false);
    const [modeSaving, setModeSaving] = useState(false);
    const [queryOpenHandled, setQueryOpenHandled] = useState('');

    const capturedPromptCount = useMemo(() => events.filter((e) => e.has_request_body).length, [events]);

    const syncManagedRuleState = (rules) => {
        const rule = findManagedModeRule(rules);
        if (!rule) {
            setModeConfigured(false);
            setModeAction('alert');
            setModeRuleId('');
            return;
        }

        const action = normalize(rule.action) || 'alert';
        setModeConfigured(true);
        setModeAction(action === 'block' ? 'block' : 'alert');
        setModeRuleId(rule.id || '');
    };

    const loadModeState = useCallback(async () => {
        if (!isAdmin) return;
        try {
            const res = await api.listPolicies();
            syncManagedRuleState(res?.rules || []);
        } catch (err) {
            console.error('Failed to load managed DLP mode rule', err);
        }
    }, [isAdmin]);

    const switchMode = async (nextMode) => {
        if (!isAdmin || modeSaving) return;
        if (nextMode !== 'alert' && nextMode !== 'block') return;

        setModeSaving(true);
        try {
            const payload = buildModeRulePayload(nextMode);
            if (modeRuleId) {
                await api.updatePolicy(modeRuleId, payload);
            } else {
                await api.createPolicy(payload);
            }
            await loadModeState();
            toast.success(`DLP mode set to ${nextMode.toUpperCase()}.`);
        } catch (err) {
            toast.error(err?.message || 'Failed to switch DLP mode');
        } finally {
            setModeSaving(false);
        }
    };

    const fetchData = useCallback(() => {
        setLoading(true);
        setLoadError('');
        Promise.all([
            api.dlpSummary(),
            api.listDLPEvents({ page, ...Object.fromEntries(Object.entries(filters).filter(([, v]) => v)) }),
        ])
            .then(([sum, evts]) => {
                setSummary(sum);
                setEvents(evts.events || []);
                setTotal(evts.total || 0);
            })
            .catch((err) => {
                setLoadError(err?.message || 'Failed to load DLP data');
                console.error(err);
            })
            .finally(() => setLoading(false));
    }, [page, filters]);

    useEffect(() => {
        fetchData();
    }, [fetchData]);

    useEffect(() => {
        loadModeState();
    }, [loadModeState]);

    const openPrompt = useCallback((event) => {
        if (!event?.id) return;
        setSelectedEvent(event);
        setPromptData(null);
        setPromptError('');
        setPromptLoading(true);
        api.getDLPEventBody(event.id)
            .then(setPromptData)
            .catch((err) => {
                const message = err.message || 'Failed to load prompt';
                setPromptError(message);
                toast.error(message);
            })
            .finally(() => setPromptLoading(false));
    }, [toast]);

    useEffect(() => {
        const rawID = searchParams.get('event_id');
        const shouldOpen = searchParams.get('open') === '1';
        if (!rawID || !shouldOpen || queryOpenHandled === rawID) return;
        const eventID = Number(rawID);
        if (!Number.isFinite(eventID) || eventID <= 0) return;

        const fromPage = events.find((e) => Number(e.id) === eventID);
        if (fromPage) {
            openPrompt(fromPage);
            setQueryOpenHandled(rawID);
            setSearchParams({}, { replace: true });
            return;
        }
        if (loading) return;

        api.getDLPEvent(eventID)
            .then((event) => {
                if (event?.id) {
                    openPrompt(event);
                }
            })
            .catch((err) => {
                toast.error(err?.message || 'Failed to locate DLP event');
            })
            .finally(() => {
                setQueryOpenHandled(rawID);
                setSearchParams({}, { replace: true });
            });
    }, [searchParams, queryOpenHandled, events, loading, setSearchParams, toast, openPrompt]);

    const closePrompt = () => {
        setSelectedEvent(null);
        setPromptData(null);
        setPromptError('');
    };

    const cards = [
        { label: 'Security Events', value: summary?.total_events ?? 0, icon: <AlertTriangle size={16} />, color: WARNING_COLOR },
        { label: 'PII Detected', value: summary?.pii_events ?? 0, icon: <Shield size={16} />, color: '#ef4444' },
        { label: 'Credentials', value: summary?.credential_events ?? 0, icon: <Key size={16} />, color: '#dc2626' },
        { label: 'Source Code', value: summary?.code_events ?? 0, icon: <Code size={16} />, color: '#c4c4c4' },
    ];

    const totalPages = Math.max(1, Math.ceil(total / 25));

    return (
        <>
            <div className="topbar">
                <div className="topbar-copy">
                    <div className="page-kicker">DLP Review</div>
                    <h1 className="topbar-title">Security Events</h1>
                    <div className="page-subtitle">Sensitive prompt detections, policy outcomes, and captured bodies for security review.</div>
                </div>
                <div className="topbar-actions dlp-toolbar">
                    {isAdmin && (
                        <div className="dlp-mode-control">
                            <span className="dlp-mode-label">
                                DLP Mode: {modeConfigured ? modeAction.toUpperCase() : 'NOT SET'}
                            </span>
                            <button
                                className={`btn btn-sm dlp-mode-button ${modeAction === 'alert' ? 'active' : ''}`}
                                onClick={() => switchMode('alert')}
                                disabled={modeSaving || modeAction === 'alert'}
                            >
                                Alert
                            </button>
                            <button
                                className={`btn btn-sm dlp-mode-button dlp-mode-button-block ${modeAction === 'block' ? 'active' : ''}`}
                                onClick={() => switchMode('block')}
                                disabled={modeSaving || modeAction === 'block'}
                            >
                                Block
                            </button>
                        </div>
                    )}

                    {isAdmin && (
                        <span className="dlp-capture-chip">
                            Prompt Captures: {capturedPromptCount}
                        </span>
                    )}
                    <span className="dlp-window-note">Last 30 days</span>
                </div>
            </div>

            <div className="page-content">
                <div className="stats-grid">
                    {cards.map((c) => (
                        <div key={c.label} className="stat-card">
                            <div className="stat-card-header">
                                <div className="stat-card-label">{c.label}</div>
                                <div className="stat-card-icon" style={{ color: c.color }}>{c.icon}</div>
                            </div>
                            <div className="stat-card-value">{(c.value).toLocaleString()}</div>
                        </div>
                    ))}
                </div>

                <div className="chart-card dlp-filter-panel">
                    <div className="filters-bar dlp-filters-bar">
                        <input
                            className="form-input dlp-filter-input"
                            placeholder="Filter by host"
                            value={filters.host}
                            onChange={(e) => { setFilters((f) => ({ ...f, host: e.target.value })); setPage(1); }}
                        />
                        <input
                            className="form-input dlp-filter-input"
                            placeholder="Filter by AI vendor"
                            value={filters.ai_vendor}
                            onChange={(e) => { setFilters((f) => ({ ...f, ai_vendor: e.target.value })); setPage(1); }}
                        />
                        <select
                            className="form-select dlp-filter-input"
                            value={filters.match_type}
                            onChange={(e) => { setFilters((f) => ({ ...f, match_type: e.target.value })); setPage(1); }}
                        >
                            <option value="">All match types</option>
                            <option value="pii">PII</option>
                            <option value="credentials">Credentials</option>
                            <option value="source_code">Source Code</option>
                            <option value="keyword">Keyword</option>
                        </select>
                    </div>
                </div>

                {loadError && !loading && <div className="panel dlp-inline-error">{loadError}</div>}

                {loading ? (
                    <div className="loading-wrap"><div className="spinner" /></div>
                ) : (
                    <>
                        <div className="data-table-wrap dlp-table-wrap">
                            {events.length === 0 ? (
                                <div className="empty-state dlp-empty-state">
                                    <Shield size={40} className="empty-state-icon" />
                                    <div className="empty-state-text">No security events found</div>
                                    <div className="empty-state-sub">Send test requests through proxy and refresh this page</div>
                                </div>
                            ) : (
                                <div className="dlp-table-scroll">
                                    <table className="data-table dlp-table">
                                        <thead>
                                            <tr>
                                                <th>Time</th>
                                                <th>Host</th>
                                                <th>App</th>
                                                <th>AI Vendor</th>
                                                <th>Detections</th>
                                                <th>Patterns</th>
                                                <th>Outcome</th>
                                                <th>Severity</th>
                                                <th>Why</th>
                                                {isAdmin && <th>Prompt</th>}
                                            </tr>
                                        </thead>
                                        <tbody>
                                            {events.map((e, idx) => {
                                                return (
                                                    <tr key={e.id || `${e.timestamp}-${e.request_host}-${e.match_count}-${idx}`}>
                                                        <td className="dlp-meta-cell">{new Date(e.timestamp).toLocaleString()}</td>
                                                        <td className="dlp-host-cell">{e.request_host}</td>
                                                        <td className="dlp-meta-cell">{e.source_app || '-'}</td>
                                                        <td className="dlp-meta-cell">{e.ai_vendor || '-'}</td>
                                                        <td>
                                                            {(e.match_types || []).map((t) => (
                                                                <MatchTypeBadge key={t} type={t} />
                                                            ))}
                                                        </td>
                                                        <td className="dlp-reason-cell">
                                                            {(e.matched_patterns || []).join(', ') || '-'}
                                                        </td>
                                                        <td><OutcomeBadge action={e.action_taken} /></td>
                                                        <td className="dlp-severity-cell">{String(e.severity || 'low').toUpperCase()}</td>
                                                        <td className="dlp-reason-cell">{formatReason(e)}</td>
                                                        {isAdmin && (
                                                            <td>
                                                                <div className="dlp-prompt-cell">
                                                                    <PromptStatusBadge hasRequestBody={!!e.has_request_body} />
                                                                    {e.id ? (
                                                                        <button
                                                                            className="btn btn-sm"
                                                                            onClick={() => openPrompt(e)}
                                                                        >
                                                                            <Eye size={14} />
                                                                            View
                                                                        </button>
                                                                    ) : (
                                                                        <span className="dlp-no-id">No ID</span>
                                                                    )}
                                                                </div>
                                                            </td>
                                                        )}
                                                    </tr>
                                                );
                                            })}
                                        </tbody>
                                    </table>
                                </div>
                            )}
                        </div>

                        {totalPages > 1 && (
                            <div className="data-table-footer">
                                <span>Page {page} of {totalPages}</span>
                                <div className="pagination">
                                    <button onClick={() => setPage((p) => Math.max(1, p - 1))} disabled={page === 1}>
                                        Previous
                                    </button>
                                    <button onClick={() => setPage((p) => Math.min(totalPages, p + 1))} disabled={page === totalPages}>
                                        Next
                                    </button>
                                </div>
                            </div>
                        )}
                    </>
                )}
            </div>

            <PromptModal
                event={selectedEvent}
                data={promptData}
                loading={promptLoading}
                error={promptError}
                onClose={closePrompt}
            />
        </>
    );
}
