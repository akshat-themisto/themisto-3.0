import { useState, useEffect } from 'react';
import { AreaChart, Area, BarChart, Bar, XAxis, YAxis, Tooltip, ResponsiveContainer } from 'recharts';
import { api } from '../api/client';
import { Activity, RefreshCw } from 'lucide-react';

const ACCENT_COLOR = '#f5f5f5';
const BLOCKED_COLOR = '#ff7a70';
const SOFT_ACCENT_COLOR = '#d8d8df';
const AXIS_COLOR = '#92929b';
const TOOLTIP_BACKGROUND = '#101012';
const TOOLTIP_BORDER = '#34343a';

export default function Telemetry() {
    const [events, setEvents] = useState([]);
    const [total, setTotal] = useState(0);
    const [page, setPage] = useState(1);
    const [timeseries, setTimeseries] = useState([]);
    const [topHosts, setTopHosts] = useState([]);
    const [loading, setLoading] = useState(true);
    const [decision, setDecision] = useState('');
    const [host, setHost] = useState('');
    const limit = 20;

    function loadAll() {
        setLoading(true);
        Promise.all([
            api.listTelemetry({ page, limit, decision, host }),
            api.telemetryTimeSeries({ interval: 'hour' }),
        ])
            .then(([ev, ts]) => {
                setEvents(ev.events || []);
                setTotal(ev.total || 0);
                setTimeseries(ts.timeseries || []);
                setTopHosts(ts.top_hosts || []);
            })
            .catch(console.error)
            .finally(() => setLoading(false));
    }

    useEffect(() => {
        loadAll();
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [page, decision, host]);

    const totalPages = Math.ceil(total / limit) || 1;
    const formatTime = (value) => new Date(value).toLocaleString('en-US', { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' });
    const formatBucketLabel = (value) => new Date(value).toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit' });

    const chartData = timeseries.map((point) => ({
        bucket: point.bucket,
        allowed: point.allowed,
        blocked: point.blocked,
    }));

    return (
        <>
            <div className="topbar">
                <div className="topbar-copy">
                    <div className="page-kicker">Network Signals</div>
                    <h1 className="topbar-title">Signals</h1>
                    <div className="page-subtitle">
                        Inspect AI activity flow, guidance outcomes, and high-volume learning destinations.
                    </div>
                </div>
                <div className="topbar-actions">
                    <div className="page-summary-card">
                        <span className="page-summary-label">Current window</span>
                        <strong>{total} signals</strong>
                        <span>{topHosts.length} active hosts in view</span>
                    </div>
                </div>
            </div>

            <div className="page-content">
                <div className="grid-2" style={{ marginBottom: 24 }}>
                    <div className="chart-card">
                        <div className="chart-card-title">Request Volume</div>
                        {chartData.length > 0 ? (
                            <ResponsiveContainer width="100%" height={240}>
                                <AreaChart data={chartData} margin={{ top: 8, right: 8, left: -16, bottom: 0 }}>
                                    <defs>
                                        <linearGradient id="gA" x1="0" y1="0" x2="0" y2="1">
                                            <stop offset="0%" stopColor={ACCENT_COLOR} stopOpacity={0.6} />
                                            <stop offset="100%" stopColor={ACCENT_COLOR} stopOpacity={0} />
                                        </linearGradient>
                                    </defs>
                                    <XAxis
                                        dataKey="bucket"
                                        tickFormatter={formatBucketLabel}
                                        minTickGap={20}
                                        tick={{ fill: AXIS_COLOR, fontSize: 11 }}
                                        axisLine={false}
                                        tickLine={false}
                                    />
                                    <YAxis tick={{ fill: AXIS_COLOR, fontSize: 11 }} axisLine={false} tickLine={false} />
                                    <Tooltip
                                        labelFormatter={formatTime}
                                        contentStyle={{ background: TOOLTIP_BACKGROUND, border: `1px solid ${TOOLTIP_BORDER}`, borderRadius: 6, fontSize: 12 }}
                                    />
                                    <Area type="monotone" dataKey="allowed" stroke={ACCENT_COLOR} fill="url(#gA)" strokeWidth={2} />
                                    <Area type="monotone" dataKey="blocked" stroke={BLOCKED_COLOR} fill="none" strokeWidth={2} strokeDasharray="4 4" />
                                </AreaChart>
                            </ResponsiveContainer>
                        ) : (
                            <div className="empty-state"><div className="empty-state-text">No data</div></div>
                        )}
                    </div>

                    <div className="chart-card">
                        <div className="chart-card-title">Top Hosts</div>
                        {topHosts.length > 0 ? (
                            <ResponsiveContainer width="100%" height={240}>
                                <BarChart data={topHosts.slice(0, 8)} margin={{ top: 8, right: 8, left: -16, bottom: 0 }} layout="vertical">
                                    <XAxis type="number" tick={{ fill: AXIS_COLOR, fontSize: 11 }} axisLine={false} tickLine={false} />
                                    <YAxis type="category" dataKey="host" tick={{ fill: SOFT_ACCENT_COLOR, fontSize: 11 }} axisLine={false} tickLine={false} width={120} />
                                    <Tooltip contentStyle={{ background: TOOLTIP_BACKGROUND, border: `1px solid ${TOOLTIP_BORDER}`, borderRadius: 6, fontSize: 12 }} />
                                    <Bar dataKey="request_count" fill={ACCENT_COLOR} radius={[0, 4, 4, 0]} />
                                </BarChart>
                            </ResponsiveContainer>
                        ) : (
                            <div className="empty-state"><div className="empty-state-text">No data</div></div>
                        )}
                    </div>
                </div>

                <div className="page-filter-row">
                    <div className="filters-bar">
                        <input className="form-input" placeholder="Filter by host..." value={host} onChange={(e) => { setHost(e.target.value); setPage(1); }} style={{ width: 220 }} />
                        <select className="form-select" value={decision} onChange={(e) => { setDecision(e.target.value); setPage(1); }}>
                            <option value="">All Decisions</option>
                            <option value="allow">Allow</option>
                            <option value="block">Block</option>
                            <option value="log_only">Log Only</option>
                        </select>
                        <button className="btn" onClick={loadAll}>
                            <RefreshCw size={14} /> Refresh
                        </button>
                    </div>
                    <div className="page-filter-meta">
                        <span className="shell-chip">{events.length} shown</span>
                    </div>
                </div>

                <div className="data-table-wrap">
                    {loading ? (
                        <div className="loading-wrap"><div className="spinner" /></div>
                    ) : events.length === 0 ? (
                        <div className="empty-state">
                            <Activity size={48} className="empty-state-icon" />
                            <div className="empty-state-text">No network signals</div>
                        </div>
                    ) : (
                        <>
                            <table className="data-table">
                                <thead>
                                    <tr>
                                        <th>Time</th>
                                        <th>Method</th>
                                        <th>Host</th>
                                        <th>Status</th>
                                        <th>Latency</th>
                                        <th>Decision</th>
                                        <th>Device</th>
                                    </tr>
                                </thead>
                                <tbody>
                                    {events.map((event) => (
                                        <tr key={event.id}>
                                            <td style={{ whiteSpace: 'nowrap', color: 'var(--text-secondary)' }}>{formatTime(event.timestamp)}</td>
                                            <td><span style={{ fontFamily: 'monospace', fontSize: 12, color: 'var(--text-primary)' }}>{event.request_method}</span></td>
                                            <td style={{ fontFamily: 'monospace', fontSize: 12, maxWidth: 180, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{event.request_host}</td>
                                            <td>{event.response_status || '-'}</td>
                                            <td style={{ color: 'var(--text-secondary)' }}>{event.latency_ms}ms</td>
                                            <td><span className="badge">{event.policy_decision}</span></td>
                                            <td style={{ fontSize: 11, fontFamily: 'monospace', color: 'var(--text-muted)' }}>{event.device_id?.substring(0, 8)}</td>
                                        </tr>
                                    ))}
                                </tbody>
                            </table>
                            <div className="data-table-footer">
                                <span>Page {page} of {totalPages}</span>
                                <div className="pagination">
                                    <button disabled={page <= 1} onClick={() => setPage((value) => value - 1)}>Prev</button>
                                    <button disabled={page >= totalPages} onClick={() => setPage((value) => value + 1)}>Next</button>
                                </div>
                            </div>
                        </>
                    )}
                </div>
            </div>
        </>
    );
}
