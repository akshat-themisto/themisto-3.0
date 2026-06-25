import { useCallback, useEffect, useMemo, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { api } from '../api/client';
import { useAuth } from '../context/AuthContext';
import { useToast } from '../context/ToastContext';
import { AlertTriangle, ChevronLeft, ChevronRight, ChevronsLeft, ChevronsRight, Code, Eye, FileText, Key, Save, Shield, Tag, X } from 'lucide-react';

const DLP_MODE_RULE_NAME = 'Quick: Corporate DLP Baseline';
const DLP_MODE_RULE_PRIORITY = 6;
const ACCENT_SOFT = '#d8d8df';
const WARNING_COLOR = '#d4d4d8';
const DANGER_COLOR = '#f5f5f5';
const SUCCESS_COLOR = '#f5f5f5';
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
        return <span className="badge badge-strong">Blocked</span>;
    }
    if (value === 'alert') {
        return <span className="badge">Alerted</span>;
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
                background: 'rgba(255,255,255,0.08)',
                color: SUCCESS_COLOR,
                border: '1px solid rgba(255,255,255,0.24)',
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

function ReviewBadge({ status }) {
    const value = normalize(status) || 'unreviewed';
    const label = {
        unreviewed: 'Unreviewed',
        reviewed: 'Reviewed',
        false_positive: 'False Positive',
        escalated: 'Escalated',
    }[value] || value;
    const color = {
        unreviewed: MUTED_COLOR,
        reviewed: SUCCESS_COLOR,
        false_positive: WARNING_COLOR,
        escalated: DANGER_COLOR,
    }[value] || MUTED_COLOR;
    return <span className="badge" style={{ color }}>{label}</span>;
}

function EventDetailPanel({
    event,
    data,
    loading,
    error,
    isAdmin,
    reviewStatus,
    reviewNote,
    reviewSaving,
    onReviewStatusChange,
    onReviewNoteChange,
    onSaveReview,
    onLoadBody,
    onClose,
}) {
    if (!event) return null;

    return (
        <div className="dlp-detail-overlay" onClick={onClose}>
            <aside className="dlp-detail-panel" onClick={(e) => e.stopPropagation()}>
                <div className="dlp-detail-header">
                    <div>
                        <div className="page-kicker">Security Review</div>
                        <h2 className="modal-title">{event.request_host}</h2>
                        <div className="dlp-detail-subtitle">
                            {event.device_name || event.device_id?.substring(0, 8) || 'Unknown device'} / {event.source_app || event.ai_vendor || 'unknown app'}
                        </div>
                    </div>
                    <button className="policy-modal-close" type="button" onClick={onClose} aria-label="Close event details">
                        <X size={16} />
                    </button>
                </div>

                <div className="dlp-detail-body">
                    <section className="dlp-detail-section">
                        <h3>Final Policy Outcome</h3>
                        <div className="dlp-detail-grid">
                            <span>Outcome</span><strong><OutcomeBadge action={event.action_taken} /></strong>
                            <span>Severity</span><strong>{String(event.severity || 'low').toUpperCase()}</strong>
                            <span>Review</span><strong><ReviewBadge status={event.review_status} /></strong>
                            <span>Time</span><strong>{event.timestamp ? new Date(event.timestamp).toLocaleString() : '-'}</strong>
                        </div>
                    </section>

                    <section className="dlp-detail-section">
                        <h3>Detector</h3>
                        <div className="dlp-detection-badges">
                            {(event.match_types || []).map((t) => <MatchTypeBadge key={t} type={t} />)}
                        </div>
                        <p>{(event.matched_patterns || []).join(', ') || 'No pattern details'}</p>
                    </section>

                    <section className="dlp-detail-section">
                        <h3>Semantic Context</h3>
                        <DecisionSourceBadge event={event} />
                        <p>{formatReason(event)}</p>
                    </section>

                    {isAdmin && (
                        <section className="dlp-detail-section">
                            <h3>Review</h3>
                            <div className="dlp-review-form">
                                <select className="form-select" value={reviewStatus} onChange={(e) => onReviewStatusChange(e.target.value)}>
                                    <option value="unreviewed">Unreviewed</option>
                                    <option value="reviewed">Reviewed</option>
                                    <option value="false_positive">False Positive</option>
                                    <option value="escalated">Escalated</option>
                                </select>
                                <textarea
                                    className="form-input"
                                    placeholder="Add a short investigation note"
                                    value={reviewNote}
                                    onChange={(e) => onReviewNoteChange(e.target.value)}
                                />
                                <button className="btn btn-primary" onClick={onSaveReview} disabled={reviewSaving}>
                                    <Save size={14} /> {reviewSaving ? 'Saving...' : 'Save Review'}
                                </button>
                            </div>
                        </section>
                    )}

                    <section className="dlp-detail-section">
                        <h3>Captured Prompt</h3>
                        {!event.has_request_body ? (
                            <p>No prompt body was stored for this event.</p>
                        ) : !data && !loading ? (
                            <button className="btn" onClick={onLoadBody}>
                                <FileText size={14} /> Load Captured Body
                            </button>
                        ) : loading ? (
                            <div style={{ color: 'var(--text-muted)' }}>Loading prompt...</div>
                        ) : error ? (
                            <div className="dlp-inline-error">{error}</div>
                        ) : (
                            <>
                                <div className="dlp-detail-subtitle">
                                    Event ID: {data?.id} / {data?.truncated ? 'Truncated at capture limit' : 'Full capture available'}
                                </div>
                                <pre className="dlp-body-preview">{data?.body || '(No body stored for this event)'}</pre>
                            </>
                        )}
                    </section>
                </div>
            </aside>
        </div>
    );
}

function formatReason(e) {
    if (e.semantic_reason && String(e.semantic_reason).trim()) return e.semantic_reason;
    if (e.reason_detail && String(e.reason_detail).trim()) return e.reason_detail;
    if (e.reason_code && String(e.reason_code).trim()) return e.reason_code;
    return '-';
}

function hasSemanticDecision(e) {
    return Boolean(
        e?.semantic_source
        || e?.semantic_category
        || String(e?.reason_code || '').startsWith('semantic_')
    );
}

function formatConfidence(value) {
    const n = Number(value);
    if (!Number.isFinite(n) || n <= 0) return '';
    return `${Math.round(n * 100)}%`;
}

function formatSemanticLabel(value) {
    const raw = String(value || '').trim();
    if (!raw) return '';
    return raw
        .replace(/^semantic[_-]?/i, '')
        .replace(/[_-]+/g, ' ')
        .replace(/\b\w/g, (m) => m.toUpperCase());
}

function DecisionSourceBadge({ event }) {
    if (!hasSemanticDecision(event)) {
        return (
            <span className="badge" style={{ color: MUTED_COLOR }}>
                Rule Engine
            </span>
        );
    }

    const source = formatSemanticLabel(event.semantic_source) || 'Semantic';
    const category = formatSemanticLabel(event.semantic_category);
    const confidence = formatConfidence(event.semantic_confidence);
    const ambiguous = event.semantic_ambiguous === true;
    const detail = [category, confidence, ambiguous ? 'Ambiguous' : ''].filter(Boolean).join(' / ');

    return (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 5, minWidth: 132 }}>
            <span className="badge" style={{
                background: 'rgba(255,255,255,0.08)',
                borderColor: 'rgba(255,255,255,0.22)',
                color: SUCCESS_COLOR,
                width: 'fit-content',
            }}>
                {source}
            </span>
            {detail && (
                <span style={{ color: 'var(--text-muted)', fontSize: 11, lineHeight: 1.25 }}>
                    {detail}
                </span>
            )}
        </div>
    );
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
    const [pageSize, setPageSize] = useState(25);
    const [loading, setLoading] = useState(true);
    const [loadError, setLoadError] = useState('');
    const [filters, setFilters] = useState({
        host: '',
        ai_vendor: '',
        match_type: '',
        severity: '',
        action_taken: '',
        review_status: '',
        semantic_source: '',
        sort: 'timestamp',
        order: 'desc',
    });

    const [selectedEvent, setSelectedEvent] = useState(null);
    const [promptData, setPromptData] = useState(null);
    const [promptLoading, setPromptLoading] = useState(false);
    const [promptError, setPromptError] = useState('');
    const [reviewStatus, setReviewStatus] = useState('unreviewed');
    const [reviewNote, setReviewNote] = useState('');
    const [reviewSaving, setReviewSaving] = useState(false);

    const [modeAction, setModeAction] = useState('alert');
    const [modeRuleId, setModeRuleId] = useState('');
    const [modeConfigured, setModeConfigured] = useState(false);
    const [modeSaving, setModeSaving] = useState(false);
    const [queryOpenHandled, setQueryOpenHandled] = useState('');

    const capturedPromptCount = useMemo(() => events.filter((e) => e.has_request_body).length, [events]);
    const semanticEventCount = useMemo(() => events.filter(hasSemanticDecision).length, [events]);

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
            api.listDLPEvents({ page, limit: pageSize, ...Object.fromEntries(Object.entries(filters).filter(([, v]) => v)) }),
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
    }, [page, pageSize, filters]);

    useEffect(() => {
        fetchData();
    }, [fetchData]);

    useEffect(() => {
        loadModeState();
    }, [loadModeState]);

    const openDetails = useCallback((event) => {
        if (!event?.id) return;
        setSelectedEvent(event);
        setPromptData(null);
        setPromptError('');
        setPromptLoading(false);
        setReviewStatus(normalize(event.review_status) || 'unreviewed');
        setReviewNote(event.review_note || '');
    }, []);

    const loadPromptBody = useCallback((event = selectedEvent) => {
        if (!event?.id || !event.has_request_body) return;
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
    }, [selectedEvent, toast]);

    const saveReview = async () => {
        if (!selectedEvent?.id || reviewSaving) return;
        setReviewSaving(true);
        try {
            const updated = await api.updateDLPEventReview(selectedEvent.id, {
                review_status: reviewStatus,
                review_note: reviewNote,
            });
            setSelectedEvent(updated);
            setReviewStatus(normalize(updated.review_status) || 'unreviewed');
            setReviewNote(updated.review_note || '');
            setEvents((current) => current.map((event) => Number(event.id) === Number(updated.id) ? updated : event));
            toast.success('Review saved.');
        } catch (err) {
            toast.error(err?.message || 'Failed to save review');
        } finally {
            setReviewSaving(false);
        }
    };

    useEffect(() => {
        const rawID = searchParams.get('event_id');
        const shouldOpen = searchParams.get('open') === '1';
        if (!rawID || !shouldOpen || queryOpenHandled === rawID) return;
        const eventID = Number(rawID);
        if (!Number.isFinite(eventID) || eventID <= 0) return;

        const fromPage = events.find((e) => Number(e.id) === eventID);
        if (fromPage) {
            openDetails(fromPage);
            setQueryOpenHandled(rawID);
            setSearchParams({}, { replace: true });
            return;
        }
        if (loading) return;

        api.getDLPEvent(eventID)
            .then((event) => {
                if (event?.id) {
                    openDetails(event);
                }
            })
            .catch((err) => {
                toast.error(err?.message || 'Failed to locate DLP event');
            })
            .finally(() => {
                setQueryOpenHandled(rawID);
                setSearchParams({}, { replace: true });
            });
    }, [searchParams, queryOpenHandled, events, loading, setSearchParams, toast, openDetails]);

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

    const totalPages = Math.max(1, Math.ceil(total / pageSize));
    const firstVisibleEvent = total === 0 ? 0 : ((page - 1) * pageSize) + 1;
    const lastVisibleEvent = Math.min(page * pageSize, total);

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
                    <span className="dlp-capture-chip">
                        Semantic: {semanticEventCount}
                    </span>
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
                        <select
                            className="form-select dlp-filter-input"
                            value={filters.severity}
                            onChange={(e) => { setFilters((f) => ({ ...f, severity: e.target.value })); setPage(1); }}
                        >
                            <option value="">All severities</option>
                            <option value="critical">Critical</option>
                            <option value="high">High</option>
                            <option value="medium">Medium</option>
                            <option value="low">Low</option>
                        </select>
                        <select
                            className="form-select dlp-filter-input"
                            value={filters.action_taken}
                            onChange={(e) => { setFilters((f) => ({ ...f, action_taken: e.target.value })); setPage(1); }}
                        >
                            <option value="">All outcomes</option>
                            <option value="block">Blocked</option>
                            <option value="alert">Alerted</option>
                            <option value="redact">Redacted</option>
                        </select>
                        <select
                            className="form-select dlp-filter-input"
                            value={filters.review_status}
                            onChange={(e) => { setFilters((f) => ({ ...f, review_status: e.target.value })); setPage(1); }}
                        >
                            <option value="">All review states</option>
                            <option value="unreviewed">Unreviewed</option>
                            <option value="reviewed">Reviewed</option>
                            <option value="false_positive">False Positive</option>
                            <option value="escalated">Escalated</option>
                        </select>
                        <input
                            className="form-input dlp-filter-input"
                            placeholder="Semantic source"
                            value={filters.semantic_source}
                            onChange={(e) => { setFilters((f) => ({ ...f, semantic_source: e.target.value })); setPage(1); }}
                        />
                        <select
                            className="form-select dlp-filter-input"
                            value={`${filters.sort}:${filters.order}`}
                            onChange={(e) => {
                                const [sort, order] = e.target.value.split(':');
                                setFilters((f) => ({ ...f, sort, order }));
                                setPage(1);
                            }}
                        >
                            <option value="timestamp:desc">Newest first</option>
                            <option value="timestamp:asc">Oldest first</option>
                            <option value="severity:desc">Highest severity</option>
                            <option value="review_status:asc">Review state</option>
                            <option value="request_host:asc">Host A-Z</option>
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
                                        <colgroup>
                                            <col className="dlp-col-time" />
                                            <col className="dlp-col-device" />
                                            <col className="dlp-col-destination" />
                                            <col className="dlp-col-detector" />
                                            <col className="dlp-col-semantic" />
                                            <col className="dlp-col-outcome" />
                                            <col className="dlp-col-severity" />
                                            <col className="dlp-col-review" />
                                            {isAdmin && <col className="dlp-col-prompt" />}
                                        </colgroup>
                                        <thead>
                                            <tr>
                                                <th>Time</th>
                                                <th>Device</th>
                                                <th>Destination</th>
                                                <th>Detector</th>
                                                <th>Semantic Context</th>
                                                <th>Final Outcome</th>
                                                <th>Severity</th>
                                                <th>Review</th>
                                                {isAdmin && <th>Prompt</th>}
                                            </tr>
                                        </thead>
                                        <tbody>
                                            {events.map((e, idx) => {
                                                return (
                                                    <tr key={e.id || `${e.timestamp}-${e.request_host}-${e.match_count}-${idx}`}>
                                                        <td className="dlp-meta-cell">{new Date(e.timestamp).toLocaleString()}</td>
                                                        <td className="dlp-meta-cell" style={{ color: 'var(--text-primary)', fontWeight: 500 }}>
                                                            {e.device_name || e.device_id?.substring(0, 8) || 'Unknown device'}
                                                        </td>
                                                        <td className="dlp-destination-cell">
                                                            <strong>{e.request_host}</strong>
                                                            <span>{[e.source_app, e.ai_vendor].filter(Boolean).join(' / ') || 'Unknown application'}</span>
                                                        </td>
                                                        <td className="dlp-detection-cell">
                                                            <div className="dlp-detection-badges">
                                                                {(e.match_types || []).map((t) => (
                                                                    <MatchTypeBadge key={t} type={t} />
                                                                ))}
                                                            </div>
                                                            <span title={(e.matched_patterns || []).join(', ')}>
                                                                {(e.matched_patterns || []).join(', ') || 'No pattern details'}
                                                            </span>
                                                        </td>
                                                        <td className="dlp-decision-cell">
                                                            <DecisionSourceBadge event={e} />
                                                            <span className="dlp-why-text" title={formatReason(e)}>{formatReason(e)}</span>
                                                        </td>
                                                        <td><OutcomeBadge action={e.action_taken} /></td>
                                                        <td className="dlp-severity-cell">{String(e.severity || 'low').toUpperCase()}</td>
                                                        <td><ReviewBadge status={e.review_status} /></td>
                                                        {isAdmin && (
                                                            <td className="dlp-prompt-column">
                                                                <div className="dlp-prompt-cell">
                                                                    <PromptStatusBadge hasRequestBody={!!e.has_request_body} />
                                                                    {e.id ? (
                                                                        <button
                                                                            className="btn btn-sm"
                                                                            onClick={() => openDetails(e)}
                                                                        >
                                                                            <Eye size={14} />
                                                                            Details
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

                        <div className="data-table-footer dlp-pagination-footer">
                            <div className="dlp-page-range">
                                <strong>{firstVisibleEvent}-{lastVisibleEvent}</strong> of {total.toLocaleString()} events
                            </div>
                            <div className="dlp-pagination-controls">
                                <label>
                                    Rows
                                    <select
                                        className="form-select dlp-page-size"
                                        value={pageSize}
                                        onChange={(event) => { setPageSize(Number(event.target.value)); setPage(1); }}
                                    >
                                        <option value={25}>25</option>
                                        <option value={50}>50</option>
                                        <option value={100}>100</option>
                                    </select>
                                </label>
                                <span>Page {page} of {totalPages}</span>
                                <div className="pagination">
                                    <button title="First page" aria-label="First page" onClick={() => setPage(1)} disabled={page === 1}>
                                        <ChevronsLeft size={15} />
                                    </button>
                                    <button title="Previous page" aria-label="Previous page" onClick={() => setPage((p) => Math.max(1, p - 1))} disabled={page === 1}>
                                        <ChevronLeft size={15} />
                                    </button>
                                    <button title="Next page" aria-label="Next page" onClick={() => setPage((p) => Math.min(totalPages, p + 1))} disabled={page === totalPages}>
                                        <ChevronRight size={15} />
                                    </button>
                                    <button title="Last page" aria-label="Last page" onClick={() => setPage(totalPages)} disabled={page === totalPages}>
                                        <ChevronsRight size={15} />
                                    </button>
                                </div>
                            </div>
                        </div>
                    </>
                )}
            </div>

            <EventDetailPanel
                event={selectedEvent}
                data={promptData}
                loading={promptLoading}
                error={promptError}
                isAdmin={isAdmin}
                reviewStatus={reviewStatus}
                reviewNote={reviewNote}
                reviewSaving={reviewSaving}
                onReviewStatusChange={setReviewStatus}
                onReviewNoteChange={setReviewNote}
                onSaveReview={saveReview}
                onLoadBody={() => loadPromptBody()}
                onClose={closePrompt}
            />
        </>
    );
}
