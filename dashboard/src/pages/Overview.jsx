import { useEffect, useState } from 'react';
import {
    Area,
    AreaChart,
    Pie,
    PieChart,
    ResponsiveContainer,
    Tooltip,
    XAxis,
    YAxis,
    Cell,
} from 'recharts';
import { api } from '../api/client';
import { useAuth } from '../context/AuthContext';
import { Activity, AlertTriangle, Shield, Smartphone } from 'lucide-react';

const ACCENT_COLOR = '#f5f5f5';
const SOFT_ACCENT_COLOR = '#d8d8df';
const DANGER_COLOR = '#ff7a70';
const WARNING_COLOR = '#c9c9d1';
const AXIS_COLOR = '#8b98aa';
const TOOLTIP_BACKGROUND = '#101012';
const TOOLTIP_BORDER = '#34343a';
const PIE_COLORS = [ACCENT_COLOR, DANGER_COLOR];

function percentChange(prev, next) {
    if (!prev || prev <= 0) return '+0%';
    const pct = ((next - prev) / prev) * 100;
    const sign = pct >= 0 ? '+' : '';
    return `${sign}${pct.toFixed(1)}%`;
}

export default function Overview() {
    const { user } = useAuth();
    const [stats, setStats] = useState(null);
    const [timeseries, setTimeseries] = useState([]);
    const [topHosts, setTopHosts] = useState([]);
    const [loading, setLoading] = useState(true);

    useEffect(() => {
        Promise.all([api.stats(), api.telemetryTimeSeries({ interval: 'day' })])
            .then(([s, ts]) => {
                setStats(s);
                setTimeseries(ts.timeseries || []);
                setTopHosts(ts.top_hosts || []);
            })
            .catch(console.error)
            .finally(() => setLoading(false));
    }, []);

    if (loading) return <div className="loading-wrap"><div className="spinner" /></div>;

    const formatBucketLabel = (value) => new Date(value).toLocaleDateString('en-US', { month: 'short', day: 'numeric' });

    const chartData = (timeseries || []).map((p) => ({
        bucket: p.bucket,
        total: p.total || 0,
        blocked: p.blocked || 0,
        allowed: p.allowed || 0,
    }));

    const latest = chartData[chartData.length - 1] || { total: 0, blocked: 0, allowed: 0 };
    const prev = chartData[chartData.length - 2] || { total: latest.total, blocked: latest.blocked, allowed: latest.allowed };

    const totalRequests = stats?.total_requests || 0;
    const blockedRate = !totalRequests
        ? '0.0%'
        : `${(((stats?.blocked_requests || 0) / totalRequests) * 100).toFixed(1)}%`;

    const cards = [
        {
            label: 'Endpoint Devices',
            value: stats?.total_devices ?? 0,
            sub: `${stats?.active_devices ?? 0} active endpoints`,
            trend: percentChange(prev.allowed, latest.allowed),
            icon: <Smartphone size={16} />,
        },
        {
            label: 'Trust Health',
            value: stats?.active_certs ?? 0,
            sub: `${stats?.expiring_certs ?? 0} expiring soon`,
            trend: stats?.expiring_certs > 0 ? 'attention' : 'stable',
            icon: <Shield size={16} />,
        },
        {
            label: 'AI Sessions Today',
            value: stats?.requests_today ?? 0,
            sub: `${stats?.total_requests ?? 0} total governance signals`,
            trend: percentChange(prev.total, latest.total),
            icon: <Activity size={16} />,
        },
        {
            label: 'Security Interventions',
            value: stats?.blocked_requests ?? 0,
            sub: `Intervention rate ${blockedRate}`,
            trend: percentChange(prev.blocked, latest.blocked),
            icon: <AlertTriangle size={16} color={DANGER_COLOR} />,
        },
    ];

    const decisionMix = [
        { name: 'Allowed', value: Math.max((stats?.total_requests || 0) - (stats?.blocked_requests || 0), 0) },
        { name: 'Blocked', value: stats?.blocked_requests || 0 },
    ];

    const healthRows = [
        {
            title: 'Endpoint Relay',
            value: (stats?.active_devices || 0) > 0 ? 'Online' : 'No active endpoints',
            tone: (stats?.active_devices || 0) > 0 ? ACCENT_COLOR : WARNING_COLOR,
        },
        {
            title: 'Policy Engine',
            value: (stats?.blocked_requests || 0) > 0 ? 'Enforcing policy' : 'Observing',
            tone: (stats?.blocked_requests || 0) > 0 ? DANGER_COLOR : SOFT_ACCENT_COLOR,
        },
        {
            title: 'Device Trust',
            value: (stats?.expiring_certs || 0) > 0 ? `${stats.expiring_certs} expiring soon` : 'All healthy',
            tone: (stats?.expiring_certs || 0) > 0 ? WARNING_COLOR : SOFT_ACCENT_COLOR,
        },
        {
            title: 'Signal Feed',
            value: chartData.length > 0 ? 'Live' : 'No recent data',
            tone: chartData.length > 0 ? ACCENT_COLOR : WARNING_COLOR,
        },
    ];

    return (
        <>
            <div className="topbar">
                <div className="topbar-copy">
                    <div className="page-kicker">Security Overview</div>
                    <h1 className="topbar-title">AI Governance Center</h1>
                    <div className="page-subtitle">
                        A compact view of endpoint AI activity, device trust, DLP signals, and policy intervention patterns.
                    </div>
                </div>
                <div className="topbar-actions overview-topbar-actions">
                    <div className="overview-hero-panel" data-tour="dashboard-overview-hero">
                        <span className="overview-hero-label">Live fleet signal</span>
                        <strong>{stats?.active_devices ?? 0} active endpoints</strong>
                        <span>{stats?.blocked_requests ?? 0} interventions - {blockedRate} rate</span>
                    </div>
                    <div className="overview-chip-group">
                        <span className="kpi-chip">Workspace: {user?.name || 'Operator'}</span>
                        <span className="kpi-chip">Window: Last 7 Days</span>
                    </div>
                </div>
            </div>

            <div className="page-content">
                <div className="stats-grid" data-tour="dashboard-stats-grid">
                    {cards.map((c) => (
                        <div key={c.label} className="stat-card">
                            <div className="stat-card-header">
                                <div className="stat-card-label">{c.label}</div>
                                <div className="stat-card-icon">{c.icon}</div>
                            </div>
                            <div className="stat-card-value">{c.value.toLocaleString()}</div>
                            <div className="stat-card-sub">{c.sub}</div>
                            <div className="stat-card-trend">
                                Trend: {c.trend}
                            </div>
                        </div>
                    ))}
                </div>

                <div className="overview-layout">
                    <div className="chart-card">
                        <div className="chart-card-title">AI Activity Trend</div>
                        {chartData.length > 0 ? (
                            <ResponsiveContainer width="100%" height={240}>
                                <AreaChart data={chartData} margin={{ top: 8, right: 8, left: -18, bottom: 0 }}>
                                    <defs>
                                        <linearGradient id="overviewTotal" x1="0" y1="0" x2="0" y2="1">
                                            <stop offset="0%" stopColor={ACCENT_COLOR} stopOpacity={0.38} />
                                            <stop offset="100%" stopColor={ACCENT_COLOR} stopOpacity={0.02} />
                                        </linearGradient>
                                        <linearGradient id="overviewBlocked" x1="0" y1="0" x2="0" y2="1">
                                            <stop offset="0%" stopColor={DANGER_COLOR} stopOpacity={0.35} />
                                            <stop offset="100%" stopColor={DANGER_COLOR} stopOpacity={0.01} />
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
                                        labelFormatter={formatBucketLabel}
                                        contentStyle={{
                                            background: TOOLTIP_BACKGROUND,
                                            border: `1px solid ${TOOLTIP_BORDER}`,
                                            borderRadius: 10,
                                            fontSize: 12,
                                            color: '#f5f5f5',
                                        }}
                                        itemStyle={{ color: '#ededed' }}
                                    />
                                    <Area type="monotone" dataKey="total" stroke={ACCENT_COLOR} fill="url(#overviewTotal)" strokeWidth={2.2} />
                                    <Area type="monotone" dataKey="blocked" stroke={DANGER_COLOR} fill="url(#overviewBlocked)" strokeWidth={1.8} />
                                </AreaChart>
                            </ResponsiveContainer>
                        ) : (
                            <div className="empty-state">
                                <Activity size={40} className="empty-state-icon" />
                                <div className="empty-state-text">No AI activity yet</div>
                            </div>
                        )}
                    </div>

                    <div className="chart-card">
                        <div className="chart-card-title">Fleet Health</div>
                        <div className="health-panel">
                            {healthRows.map((row) => (
                                <div key={row.title} className="health-item">
                                    <div>
                                        <strong>{row.title}</strong>
                                        <span style={{ display: 'block', marginTop: 2 }}>{row.value}</span>
                                    </div>
                                    <span style={{ width: 10, height: 10, borderRadius: '50%', background: row.tone, boxShadow: `0 0 10px ${row.tone}` }} />
                                </div>
                            ))}
                        </div>
                        <div style={{ marginTop: 16 }}>
                            <div className="chart-card-title" style={{ marginBottom: 10 }}>Policy Mix</div>
                            <ResponsiveContainer width="100%" height={150}>
                                <PieChart>
                                    <Pie
                                        data={decisionMix}
                                        dataKey="value"
                                        nameKey="name"
                                        cx="50%"
                                        cy="50%"
                                        innerRadius={42}
                                        outerRadius={66}
                                        paddingAngle={3}
                                    >
                                        {decisionMix.map((entry, idx) => (
                                            <Cell key={entry.name} fill={PIE_COLORS[idx % PIE_COLORS.length]} />
                                        ))}
                                    </Pie>
                                    <Tooltip
                                        contentStyle={{
                                            background: TOOLTIP_BACKGROUND,
                                            border: `1px solid ${TOOLTIP_BORDER}`,
                                            borderRadius: 10,
                                            fontSize: 12,
                                            color: '#f5f5f5',
                                        }}
                                    />
                                </PieChart>
                            </ResponsiveContainer>
                        </div>
                    </div>
                </div>

                <div className="chart-card" style={{ marginBottom: 0 }}>
                    <div className="chart-card-title">Top AI Destinations</div>
                    {topHosts.length > 0 ? (
                        <div className="data-table-wrap" style={{ border: 'none', background: 'transparent' }}>
                            <table className="data-table">
                                <thead>
                                    <tr>
                                        <th>Host</th>
                                        <th>Requests</th>
                                        <th>Blocked</th>
                                        <th>Avg Latency</th>
                                    </tr>
                                </thead>
                                <tbody>
                                    {topHosts.slice(0, 8).map((h, idx) => (
                                        <tr key={`${h.host}-${idx}`}>
                                            <td style={{ fontFamily: 'monospace', fontSize: 12 }}>{h.host}</td>
                                            <td>{(h.request_count || 0).toLocaleString()}</td>
                                            <td style={{ color: (h.blocked_count || 0) > 0 ? DANGER_COLOR : 'var(--text-secondary)' }}>{h.blocked_count || 0}</td>
                                            <td>{h.avg_latency_ms || 0}ms</td>
                                        </tr>
                                    ))}
                                </tbody>
                            </table>
                        </div>
                    ) : (
                        <div className="empty-state">
                            <div className="empty-state-text">No host telemetry yet</div>
                        </div>
                    )}
                </div>
            </div>
        </>
    );
}
