import { useEffect, useMemo, useState } from 'react';
import { BarChart, Bar, XAxis, YAxis, Tooltip, ResponsiveContainer, Cell } from 'recharts';
import { api } from '../api/client';
import { useAuth } from '../context/AuthContext';
import { useToast } from '../context/ToastContext';
import { Bot, Shield, Smartphone, AlertTriangle, Activity, Ban } from 'lucide-react';

const AUTO_BLOCK_RULE_PREFIX = 'auto: block unsanctioned ai - ';

const VENDOR_COLORS = {
    openai: '#10a37f',
    anthropic: '#d97706',
    google: '#4285f4',
    microsoft: '#0078d4',
    github: '#24292e',
    cursor: '#6366f1',
    huggingface: '#ff9d00',
    cohere: '#39c9b3',
    mistral: '#e11d48',
    perplexity: '#22d3ee',
    groq: '#f59e0b',
    xai: '#9333ea',
    windsurf: '#00bfa5',
};

const CATEGORY_LABELS = {
    ai_llm: 'LLM / Chat',
    ai_code: 'AI Coding',
    ai_image: 'AI Image',
    ai_search: 'AI Search',
    multiple: 'Multiple',
};

const SURFACE_LABELS = {
    browser_chromium: 'Chrome',
    browser_firefox: 'Firefox',
    browser_safari: 'Safari',
    claude_code: 'Claude Code',
    cursor: 'Cursor',
    github_copilot: 'GitHub Copilot',
    windsurf: 'Windsurf',
    desktop: 'Desktop',
};

const SURFACE_COLORS = {
    browser_chromium: '#4285f4',
    browser_firefox: '#ff7139',
    browser_safari: '#006cff',
    claude_code: '#d97706',
    cursor: '#6366f1',
    github_copilot: '#24292e',
    windsurf: '#00bfa5',
    desktop: '#737373',
};

export default function AIUsage() {
    const { user } = useAuth();
    const isAdmin = user?.role === 'admin' || user?.role === 'owner';
    const toast = useToast();

    const [data, setData] = useState(null);
    const [policyRules, setPolicyRules] = useState([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState(null);
    const [sanctionFilter, setSanctionFilter] = useState('all');
    const [busyVendor, setBusyVendor] = useState('');
    const [bulkBusy, setBulkBusy] = useState(false);

    const load = () => {
        setLoading(true);
        setError(null);
        Promise.all([api.aiUsage(), api.listPolicies()])
            .then(([usage, policies]) => {
                setData(usage);
                setPolicyRules(policies?.rules || []);
            })
            .catch((e) => setError(e.message))
            .finally(() => setLoading(false));
    };

    useEffect(() => {
        load();
    }, []);

    const summary = data?.summary || {};
    const topDevices = data?.top_devices || [];
    const topViolating = data?.top_violating_devices || [];
    const governance = data?.governance || [];

    const governanceMap = useMemo(() => {
        const map = {};
        for (const g of governance) {
            map[(g.ai_vendor || '').toLowerCase()] = g;
        }
        return map;
    }, [governance]);

    const enforcedVendorSet = useMemo(() => {
        const vendors = new Set();
        for (const rule of policyRules) {
            const name = (rule?.name || '').trim().toLowerCase();
            if (!rule?.enabled || (rule?.action || '').toLowerCase() !== 'block' || !name.startsWith(AUTO_BLOCK_RULE_PREFIX)) {
                continue;
            }
            const vendor = name.slice(AUTO_BLOCK_RULE_PREFIX.length).trim();
            if (vendor) {
                vendors.add(vendor);
            }
        }
        return vendors;
    }, [policyRules]);

    const vendorBreakdown = useMemo(() => {
        const rows = (summary?.vendor_breakdown || []).map((v) => {
            const gov = governanceMap[(v.vendor || '').toLowerCase()];
            return {
                ...v,
                sanctioned: gov ? !!gov.sanctioned : !!v.sanctioned,
                risk_tier: gov?.risk_tier || v.risk_tier || 'unknown',
                notes: gov?.notes || null,
            };
        });

        const byVendor = new Map();
        for (const row of rows) {
            const vendorKey = (row.vendor || '').toLowerCase();
            if (!vendorKey) continue;

            const existing = byVendor.get(vendorKey);
            if (!existing) {
                byVendor.set(vendorKey, {
                    ...row,
                    vendor: row.vendor,
                    category: row.category || 'unknown',
                    categorySet: new Set([row.category || 'unknown']),
                    block_enforced: enforcedVendorSet.has(vendorKey),
                });
                continue;
            }

            existing.request_count = (existing.request_count || 0) + (row.request_count || 0);
            existing.blocked_count = (existing.blocked_count || 0) + (row.blocked_count || 0);
            existing.device_count = Math.max(existing.device_count || 0, row.device_count || 0);
            existing.bytes_sent_total = (existing.bytes_sent_total || 0) + (row.bytes_sent_total || 0);
            existing.categorySet.add(row.category || 'unknown');
            existing.sanctioned = row.sanctioned;
            existing.risk_tier = row.risk_tier || existing.risk_tier;
            existing.notes = row.notes ?? existing.notes;
            existing.block_enforced = enforcedVendorSet.has(vendorKey);
        }

        const aggregated = Array.from(byVendor.values()).map((row) => {
            const categories = Array.from(row.categorySet || []);
            return {
                ...row,
                category: categories.length === 1 ? categories[0] : 'multiple',
            };
        });

        if (sanctionFilter === 'sanctioned') return aggregated.filter((r) => !!r.sanctioned);
        if (sanctionFilter === 'unsanctioned') return aggregated.filter((r) => !r.sanctioned);
        return aggregated;
    }, [summary, governanceMap, sanctionFilter, enforcedVendorSet]);

    const categoryBreakdown = summary?.category_breakdown || [];

    const cards = [
        {
            label: 'AI Requests',
            value: (summary.total_ai_requests ?? 0).toLocaleString(),
            sub: 'Total AI API calls',
            icon: <Bot size={16} />,
        },
        {
            label: 'AI Vendors',
            value: summary.unique_ai_vendors ?? 0,
            sub: 'Unique services detected',
            icon: <Activity size={16} />,
        },
        {
            label: 'Unapproved Tool Use',
            value: (summary.unsanctioned_count ?? 0).toLocaleString(),
            sub: 'Traffic to unapproved AI tools',
            icon: <Ban size={16} color="#f97316" />,
        },
        {
            label: 'Blocked',
            value: (vendorBreakdown.reduce((s, v) => s + (v.blocked_count || 0), 0)).toLocaleString(),
            sub: 'Policy-blocked AI requests',
            icon: <Shield size={16} color="#ef4444" />,
        },
    ];

    const chartData = vendorBreakdown.slice(0, 10).map((v) => ({
        vendor: v.vendor,
        requests: v.request_count,
        blocked: v.blocked_count,
    }));

    const categoryChartData = categoryBreakdown.map((c) => ({
        name: CATEGORY_LABELS[c.category] || c.category,
        value: c.request_count,
    }));

    const surfaceBreakdown = summary?.surface_breakdown || [];
    const surfaceChartData = surfaceBreakdown.map((s) => ({
        name: SURFACE_LABELS[s.surface] || s.surface,
        surface: s.surface,
        value: s.request_count,
        devices: s.device_count,
    }));

    const updateGovernance = async (vendor, patch) => {
        if (!isAdmin) return;
        const current = governanceMap[(vendor || '').toLowerCase()] || {};
        const payload = {
            sanctioned: patch.sanctioned ?? !!current.sanctioned,
            risk_tier: patch.risk_tier || current.risk_tier || 'medium',
            notes: patch.notes ?? current.notes ?? '',
        };

        setBusyVendor(vendor);
        try {
            await api.updateAIGovernanceVendor(vendor, payload);
            toast.success(`Governance updated for ${vendor}.`);
            load();
        } catch (e) {
            toast.error(e.message || 'Failed to update governance');
        } finally {
            setBusyVendor('');
        }
    };

    const enforceUnsanctionedBlock = async (vendor) => {
        if (!isAdmin) return;
        setBusyVendor(vendor);
        try {
            await api.blockUnsanctionedVendor(vendor);
            toast.success(`Block enforcement enabled for ${vendor}.`);
            load();
        } catch (e) {
            toast.error(e.message || 'Failed to enforce block rule');
        } finally {
            setBusyVendor('');
        }
    };

    const unblockVendor = async (vendor) => {
        if (!isAdmin) return;
        setBusyVendor(vendor);
        try {
            await api.unblockVendor(vendor);
            toast.success(`${vendor} unblocked.`);
            load();
        } catch (e) {
            toast.error(e.message || 'Failed to unblock vendor');
        } finally {
            setBusyVendor('');
        }
    };

    const blockAllUnsanctioned = async () => {
        if (!isAdmin) return;
        const vendors = vendorBreakdown.filter((v) => !v.sanctioned).map((v) => v.vendor);
        if (vendors.length === 0) {
            toast.info('No unsanctioned vendors found in the current filter window.');
            return;
        }

        setBulkBusy(true);
        try {
            for (const vendor of vendors) {
                await api.blockUnsanctionedVendor(vendor);
            }
            toast.success(`Enforced block rules for ${vendors.length} unsanctioned vendor(s).`);
            load();
        } catch (e) {
            toast.error(e.message || 'Failed while applying bulk unsanctioned blocks');
        } finally {
            setBulkBusy(false);
        }
    };

    if (loading) return <div className="loading-wrap"><div className="spinner" /></div>;
    if (error) {
        return (
            <>
                <div className="topbar">
                    <div className="topbar-copy">
                        <div className="page-kicker">AI Governance</div>
                        <h1 className="topbar-title">AI Usage</h1>
                    </div>
                </div>
                <div className="page-content">
                    <div style={{ color: 'var(--text-muted)', padding: 32 }}>Failed to load: {error}</div>
                </div>
            </>
        );
    }

    return (
        <>
            <div className="topbar">
                <div className="topbar-copy">
                    <div className="page-kicker">AI Governance</div>
                    <h1 className="topbar-title">AI Usage</h1>
                    <div className="page-subtitle">
                        Track approved and unapproved AI activity by vendor, source surface, and endpoint so security policy stays grounded in real usage.
                    </div>
                </div>
                <div className="topbar-actions">
                    <div className="page-summary-card">
                        <span className="page-summary-label">Risk snapshot</span>
                        <strong>{summary.unsanctioned_count ?? 0} unapproved</strong>
                        <span>{vendorBreakdown.length} vendors across the last 30 days</span>
                    </div>
                    <div className="page-chip-group">
                        <select className="form-select" style={{ width: 180 }} value={sanctionFilter} onChange={(e) => setSanctionFilter(e.target.value)}>
                            <option value="all">All Vendors</option>
                            <option value="sanctioned">Sanctioned Only</option>
                            <option value="unsanctioned">Unsanctioned Only</option>
                        </select>
                        {isAdmin && (
                            <button className="btn btn-danger" onClick={blockAllUnsanctioned} disabled={bulkBusy || busyVendor !== ''}>
                                {bulkBusy ? 'Applying...' : 'Block All Unapproved'}
                            </button>
                        )}
                        <span className="shell-chip">Last 30 days</span>
                    </div>
                </div>
            </div>

            <div className="page-content">
                <div className="stats-grid">
                    {cards.map((c) => (
                        <div key={c.label} className="stat-card">
                            <div className="stat-card-header">
                                <div className="stat-card-label">{c.label}</div>
                                <div className="stat-card-icon">{c.icon}</div>
                            </div>
                            <div className="stat-card-value">{c.value}</div>
                            <div className="stat-card-sub">{c.sub}</div>
                        </div>
                    ))}
                </div>

                <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 16, marginBottom: 16 }}>
                    <div className="chart-card">
                        <div className="chart-card-title">Requests by AI Vendor</div>
                        {chartData.length > 0 ? (
                            <ResponsiveContainer width="100%" height={240}>
                                <BarChart data={chartData} margin={{ top: 8, right: 8, left: -16, bottom: 40 }}>
                                    <XAxis dataKey="vendor" tick={{ fill: '#737373', fontSize: 11 }} angle={-30} textAnchor="end" axisLine={false} tickLine={false} />
                                    <YAxis tick={{ fill: '#737373', fontSize: 11 }} axisLine={false} tickLine={false} />
                                    <Tooltip contentStyle={{ background: '#141414', border: '1px solid #262626', borderRadius: 6, fontSize: 12, color: '#ededed' }} itemStyle={{ color: '#ededed' }} />
                                    <Bar dataKey="requests" radius={[3, 3, 0, 0]}>
                                        {chartData.map((entry) => (
                                            <Cell key={entry.vendor} fill={VENDOR_COLORS[entry.vendor] || '#525252'} />
                                        ))}
                                    </Bar>
                                </BarChart>
                            </ResponsiveContainer>
                        ) : (
                            <div className="empty-state">
                                <Bot size={40} className="empty-state-icon" />
                                <div className="empty-state-text">No AI traffic detected yet</div>
                            </div>
                        )}
                    </div>

                    <div className="chart-card">
                        <div className="chart-card-title">Requests by Category</div>
                        {categoryChartData.length > 0 ? (
                            <ResponsiveContainer width="100%" height={240}>
                                <BarChart data={categoryChartData} layout="vertical" margin={{ top: 8, right: 8, left: 40, bottom: 0 }}>
                                    <XAxis type="number" tick={{ fill: '#737373', fontSize: 11 }} axisLine={false} tickLine={false} />
                                    <YAxis type="category" dataKey="name" tick={{ fill: '#a3a3a3', fontSize: 12 }} axisLine={false} tickLine={false} width={80} />
                                    <Tooltip contentStyle={{ background: '#141414', border: '1px solid #262626', borderRadius: 6, fontSize: 12, color: '#ededed' }} />
                                    <Bar dataKey="value" fill="#525252" radius={[0, 3, 3, 0]} />
                                </BarChart>
                            </ResponsiveContainer>
                        ) : (
                            <div className="empty-state">
                                <Activity size={40} className="empty-state-icon" />
                                <div className="empty-state-text">No category data</div>
                            </div>
                        )}
                    </div>
                </div>

                {surfaceChartData.length > 0 && (
                    <div className="chart-card" style={{ marginBottom: 16 }}>
                        <div className="chart-card-title">Usage by Source</div>
                        <ResponsiveContainer width="100%" height={240}>
                            <BarChart data={surfaceChartData} margin={{ top: 8, right: 8, left: -16, bottom: 40 }}>
                                <XAxis dataKey="name" tick={{ fill: '#737373', fontSize: 11 }} angle={-30} textAnchor="end" axisLine={false} tickLine={false} />
                                <YAxis tick={{ fill: '#737373', fontSize: 11 }} axisLine={false} tickLine={false} />
                                <Tooltip contentStyle={{ background: '#141414', border: '1px solid #262626', borderRadius: 6, fontSize: 12, color: '#ededed' }} itemStyle={{ color: '#ededed' }} />
                                <Bar dataKey="value" name="Requests" radius={[3, 3, 0, 0]}>
                                    {surfaceChartData.map((entry) => (
                                        <Cell key={entry.surface} fill={SURFACE_COLORS[entry.surface] || '#525252'} />
                                    ))}
                                </Bar>
                            </BarChart>
                        </ResponsiveContainer>
                    </div>
                )}

                <div className="chart-card">
                    <div className="chart-card-title">Vendor Governance</div>
                    {vendorBreakdown.length > 0 ? (
                        <div style={{ overflowX: 'auto' }}>
                            <table style={{ width: '100%', minWidth: 980, borderCollapse: 'collapse', fontSize: 13 }}>
                                <colgroup>
                                    <col style={{ width: '18%' }} />
                                    <col style={{ width: '15%' }} />
                                    <col style={{ width: '10%' }} />
                                    <col style={{ width: '10%' }} />
                                    <col style={{ width: '14%' }} />
                                    <col style={{ width: '14%' }} />
                                    {isAdmin && <col style={{ width: '19%' }} />}
                                </colgroup>
                                <thead>
                                    <tr style={{ borderBottom: '1px solid #262626' }}>
                                        <th style={{ textAlign: 'left', padding: '8px 12px', color: 'var(--text-muted)', fontWeight: 500 }}>Vendor</th>
                                        <th style={{ textAlign: 'left', padding: '8px 12px', color: 'var(--text-muted)', fontWeight: 500 }}>Category</th>
                                        <th style={{ textAlign: 'right', padding: '8px 12px', color: 'var(--text-muted)', fontWeight: 500 }}>Requests</th>
                                        <th style={{ textAlign: 'right', padding: '8px 12px', color: 'var(--text-muted)', fontWeight: 500 }}>Blocked</th>
                                        <th style={{ textAlign: 'left', padding: '8px 12px', color: 'var(--text-muted)', fontWeight: 500 }}>Sanction</th>
                                        <th style={{ textAlign: 'left', padding: '8px 12px', color: 'var(--text-muted)', fontWeight: 500 }}>Risk Tier</th>
                                        {isAdmin && <th style={{ textAlign: 'right', padding: '8px 12px', color: 'var(--text-muted)', fontWeight: 500 }}>Enforcement</th>}
                                    </tr>
                                </thead>
                                <tbody>
                                    {vendorBreakdown.map((v, i) => {
                                        const busy = busyVendor === v.vendor;
                                        return (
                                            <tr key={`${v.vendor}-${i}`} style={{ borderBottom: '1px solid #1a1a1a' }}>
                                                <td style={{ padding: '8px 12px', color: 'var(--text-primary)' }}>
                                                    <span style={{ display: 'inline-block', width: 10, height: 10, borderRadius: '50%', background: VENDOR_COLORS[v.vendor] || '#525252', marginRight: 8 }} />
                                                    {v.vendor}
                                                </td>
                                                <td style={{ padding: '8px 12px', color: 'var(--text-muted)' }}>{CATEGORY_LABELS[v.category] || v.category}</td>
                                                <td style={{ padding: '8px 12px', textAlign: 'right', color: 'var(--text-primary)' }}>{(v.request_count || 0).toLocaleString()}</td>
                                                <td style={{ padding: '8px 12px', textAlign: 'right', color: (v.blocked_count || 0) > 0 ? '#ef4444' : 'var(--text-muted)' }}>{v.blocked_count || 0}</td>
                                                <td style={{ padding: '8px 12px' }}>
                                                    <span className="badge" style={{
                                                        background: v.sanctioned ? 'rgba(34,197,94,0.16)' : 'rgba(249,115,22,0.16)',
                                                        borderColor: v.sanctioned ? 'rgba(34,197,94,0.5)' : 'rgba(249,115,22,0.5)',
                                                        color: v.sanctioned ? '#86efac' : '#fdba74',
                                                    }}>
                                                        {v.sanctioned ? 'Sanctioned' : 'Unsanctioned'}
                                                    </span>
                                                </td>
                                                <td style={{ padding: '8px 12px' }}>
                                                    {isAdmin ? (
                                                        <select className="form-select" style={{ height: 34, width: '100%', minWidth: 124 }} value={v.risk_tier || 'medium'} disabled={busy} onChange={(e) => updateGovernance(v.vendor, { risk_tier: e.target.value, sanctioned: v.sanctioned })}>
                                                            <option value="low">low</option>
                                                            <option value="medium">medium</option>
                                                            <option value="high">high</option>
                                                            <option value="critical">critical</option>
                                                        </select>
                                                    ) : (
                                                        <span style={{ color: 'var(--text-secondary)' }}>{v.risk_tier || 'unknown'}</span>
                                                    )}
                                                </td>
                                                {isAdmin && (
                                                    <td style={{ padding: '8px 12px', textAlign: 'right' }}>
                                                        <div style={{ display: 'inline-flex', gap: 8, flexWrap: 'wrap', justifyContent: 'flex-end' }}>
                                                            <button className="btn btn-sm" disabled={busy} onClick={() => updateGovernance(v.vendor, { sanctioned: !v.sanctioned })}>
                                                                {v.sanctioned ? 'Mark Unsanctioned' : 'Mark Sanctioned'}
                                                            </button>
                                                            {v.block_enforced && (
                                                                <button className="btn btn-sm" disabled={busy} onClick={() => unblockVendor(v.vendor)}>
                                                                    Unblock Vendor
                                                                </button>
                                                            )}
                                                            {!v.sanctioned && !v.block_enforced && (
                                                                <button className="btn btn-sm btn-danger" disabled={busy} onClick={() => enforceUnsanctionedBlock(v.vendor)}>
                                                                    Block Vendor
                                                                </button>
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
                    ) : (
                        <div className="empty-state">
                            <AlertTriangle size={40} className="empty-state-icon" />
                            <div className="empty-state-text">No AI vendor data in this period</div>
                        </div>
                    )}
                </div>

                {topDevices.length > 0 && (
                    <div className="chart-card" style={{ marginTop: 16 }}>
                        <div className="chart-card-title">Top Endpoints by AI Usage</div>
                        <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
                            <thead>
                                <tr style={{ borderBottom: '1px solid #262626' }}>
                                    <th style={{ textAlign: 'left', padding: '8px 12px', color: 'var(--text-muted)', fontWeight: 500 }}>Endpoint</th>
                                    <th style={{ textAlign: 'right', padding: '8px 12px', color: 'var(--text-muted)', fontWeight: 500 }}>AI Requests</th>
                                    <th style={{ textAlign: 'right', padding: '8px 12px', color: 'var(--text-muted)', fontWeight: 500 }}>Vendors Used</th>
                                </tr>
                            </thead>
                            <tbody>
                                {topDevices.map((d, i) => (
                                    <tr key={i} style={{ borderBottom: '1px solid #1a1a1a' }}>
                                        <td style={{ padding: '8px 12px', color: 'var(--text-primary)', fontWeight: 500 }}>{d.device_name || d.device_id?.substring(0, 8) || 'Unknown device'}</td>
                                        <td style={{ padding: '8px 12px', textAlign: 'right', color: 'var(--text-primary)' }}>{(d.request_count || 0).toLocaleString()}</td>
                                        <td style={{ padding: '8px 12px', textAlign: 'right', color: 'var(--text-muted)' }}>{d.vendor_count || 0}</td>
                                    </tr>
                                ))}
                            </tbody>
                        </table>
                    </div>
                )}

                {topViolating.length > 0 && (
                    <div className="chart-card" style={{ marginTop: 16 }}>
                        <div className="chart-card-title">Top Unapproved Usage Endpoints</div>
                        <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
                            <thead>
                                <tr style={{ borderBottom: '1px solid #262626' }}>
                                    <th style={{ textAlign: 'left', padding: '8px 12px', color: 'var(--text-muted)', fontWeight: 500 }}>Endpoint</th>
                                    <th style={{ textAlign: 'right', padding: '8px 12px', color: 'var(--text-muted)', fontWeight: 500 }}>Unapproved Events</th>
                                    <th style={{ textAlign: 'right', padding: '8px 12px', color: 'var(--text-muted)', fontWeight: 500 }}>Unique Vendors</th>
                                </tr>
                            </thead>
                            <tbody>
                                {topViolating.map((d, i) => (
                                    <tr key={i} style={{ borderBottom: '1px solid #1a1a1a' }}>
                                        <td style={{ padding: '8px 12px', color: 'var(--text-primary)', fontWeight: 500 }}>{d.device_name || d.device_id?.substring(0, 8) || 'Unknown device'}</td>
                                        <td style={{ padding: '8px 12px', textAlign: 'right', color: '#f59e0b' }}>{(d.unsanctioned_events || 0).toLocaleString()}</td>
                                        <td style={{ padding: '8px 12px', textAlign: 'right', color: 'var(--text-muted)' }}>{d.unique_vendors || 0}</td>
                                    </tr>
                                ))}
                            </tbody>
                        </table>
                    </div>
                )}
            </div>
        </>
    );
}





