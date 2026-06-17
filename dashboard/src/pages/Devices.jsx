import { useEffect, useState } from 'react';
import { api } from '../api/client';
import { useAuth } from '../context/AuthContext';
import { Smartphone, RefreshCw, Plus, KeyRound, Copy, Download } from 'lucide-react';

export default function Devices() {
    const { user } = useAuth();
    const [devices, setDevices] = useState([]);
    const [loading, setLoading] = useState(true);
    const [search, setSearch] = useState('');
    const [statusFilter, setStatusFilter] = useState('');
    const [showCreate, setShowCreate] = useState(false);
    const [creating, setCreating] = useState(false);
    const [form, setForm] = useState({ device_name: '', os: 'darwin', agent_version: '1.0.0', token_ttl_hours: 24 });
    const [enrollmentPackage, setEnrollmentPackage] = useState(null);
    const [error, setError] = useState('');

    useEffect(() => {
        loadDevices();
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, []);

    const loadDevices = () => {
        setLoading(true);
        api.listDevices(user?.org_id, '')
            .then((d) => setDevices(d?.devices || []))
            .catch(() => setDevices([]))
            .finally(() => setLoading(false));
    };

    const createEnrollmentPackage = async (e) => {
        e.preventDefault();
        setCreating(true);
        setError('');
        try {
            const pkg = await api.createEnrollmentPackage(form);
            setEnrollmentPackage(pkg);
            setShowCreate(false);
            setForm({ device_name: '', os: 'darwin', agent_version: '1.0.0', token_ttl_hours: 24 });
            loadDevices();
        } catch (err) {
            setError(err?.message || 'Failed to create enrollment package');
        } finally {
            setCreating(false);
        }
    };

    const reissueToken = async (deviceID) => {
        setError('');
        try {
            const pkg = await api.reissueEnrollmentToken(deviceID);
            setEnrollmentPackage(pkg);
        } catch (err) {
            setError(err?.message || 'Failed to reissue token');
        }
    };

    const copy = async (value) => {
        try {
            await navigator.clipboard.writeText(value);
        } catch {
            // noop
        }
    };

    const downloadAgentConfig = (pkg) => {
        if (!pkg?.agent_config || !pkg?.device_id) return;
        const json = `${JSON.stringify(pkg.agent_config, null, 2)}\n`;
        const blob = new Blob([json], { type: 'application/json' });
        const link = document.createElement('a');
        link.href = URL.createObjectURL(blob);
        link.download = `agent-${pkg.device_id}.json`;
        document.body.appendChild(link);
        link.click();
        document.body.removeChild(link);
        URL.revokeObjectURL(link.href);
    };

    const filtered = devices.filter((d) => {
        const deviceName = d.device_name || '';
        const deviceID = d.device_id || '';
        const matchSearch = !search || deviceName.toLowerCase().includes(search.toLowerCase()) || deviceID.toLowerCase().includes(search.toLowerCase());
        const matchStatus = !statusFilter || d.status === statusFilter;
        return matchSearch && matchStatus;
    });

    const activeCount = devices.filter((d) => d.status === 'active').length;
    const pendingCount = devices.filter((d) => d.status === 'pending').length;

    const formatDate = (value) => value
        ? new Date(value).toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' })
        : '-';

    const formatDateTime = (value) => value
        ? new Date(value).toLocaleString('en-US', { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
        : '-';

    return (
        <>
            <div className="topbar">
                <div className="topbar-copy">
                    <div className="page-kicker">Endpoint Access</div>
                    <h1 className="topbar-title">Devices</h1>
                    <div className="page-subtitle">
                        Create enrollment packages, reissue activation links, and keep trust state visible across managed endpoints.
                    </div>
                </div>
                <div className="topbar-actions">
                    <div className="page-summary-card">
                        <span className="page-summary-label">Current fleet</span>
                        <strong>{devices.length} endpoint devices</strong>
                        <span>{activeCount} active / {pendingCount} pending</span>
                    </div>
                    <button className="btn btn-primary" onClick={() => setShowCreate((value) => !value)}>
                        <Plus size={14} /> New Device Setup
                    </button>
                </div>
            </div>

            <div className="page-content">
                {showCreate && (
                    <form className="panel" onSubmit={createEnrollmentPackage} style={{ marginBottom: 18 }}>
                        <div style={{ fontWeight: 600, marginBottom: 12 }}>Create Device Enrollment Package</div>
                        <div className="filters-bar">
                            <input
                                className="form-input"
                                placeholder="Endpoint device name"
                                value={form.device_name}
                                onChange={(e) => setForm({ ...form, device_name: e.target.value })}
                                required
                                style={{ width: 260 }}
                            />
                            <select
                                className="form-select"
                                value={form.os}
                                onChange={(e) => setForm({ ...form, os: e.target.value })}
                            >
                                <option value="darwin">macOS</option>
                                <option value="windows">Windows</option>
                                <option value="linux">Linux</option>
                            </select>
                            <input
                                className="form-input"
                                placeholder="Agent version"
                                value={form.agent_version}
                                onChange={(e) => setForm({ ...form, agent_version: e.target.value })}
                                style={{ width: 140 }}
                            />
                            <select
                                className="form-select"
                                value={form.token_ttl_hours}
                                onChange={(e) => setForm({ ...form, token_ttl_hours: Number(e.target.value) })}
                            >
                                <option value={24}>Token valid 24h</option>
                                <option value={48}>Token valid 48h</option>
                                <option value={72}>Token valid 72h</option>
                                <option value={168}>Token valid 7 days</option>
                            </select>
                            <button className="btn" disabled={creating}>
                                {creating ? 'Creating...' : 'Create'}
                            </button>
                        </div>
                    </form>
                )}

                {error && (
                    <div className="panel" style={{ marginBottom: 18, color: '#ef4444', borderColor: '#7f1d1d' }}>
                        {error}
                    </div>
                )}

                {enrollmentPackage && (
                    <div className="panel" style={{ marginBottom: 18 }}>
                        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: 12, marginBottom: 14, flexWrap: 'wrap' }}>
                            <div style={{ fontWeight: 600 }}>Device Enrollment Package</div>
                            <button className="btn" onClick={() => setEnrollmentPackage(null)}>Close</button>
                        </div>

                        <div style={{ display: 'grid', gap: 8, marginBottom: 12 }}>
                            <div><span style={{ color: 'var(--text-muted)' }}>Device ID:</span> <code>{enrollmentPackage.device_id}</code></div>
                            <div><span style={{ color: 'var(--text-muted)' }}>Token Expires:</span> {formatDateTime(enrollmentPackage.token_expires_at)}</div>
                            {enrollmentPackage.gateway_url && (
                                <div><span style={{ color: 'var(--text-muted)' }}>Gateway URL:</span> <code>{enrollmentPackage.gateway_url}</code></div>
                            )}
                        </div>

                        {enrollmentPackage.enrollment_url && (
                            <div style={{ marginBottom: 12, padding: '12px 16px', background: 'var(--bg-card-soft)', borderRadius: 8, border: '1px solid var(--border)' }}>
                                <div style={{ fontSize: 12, color: 'var(--text-muted)', marginBottom: 6, fontWeight: 600 }}>Enrollment Link</div>
                                <div style={{ fontSize: 12, color: 'var(--text-secondary)', marginBottom: 8 }}>
                                    Share this link with the endpoint owner or IT deployment workflow. On the device, open the Themisto desktop app, go to <strong>Enrollment</strong>, and paste this link.
                                </div>
                                <div style={{ display: 'flex', gap: 8 }}>
                                    <input className="form-input" readOnly value={enrollmentPackage.enrollment_url} style={{ fontFamily: 'monospace', fontSize: 13 }} />
                                    <button className="btn" type="button" onClick={() => copy(enrollmentPackage.enrollment_url)}>
                                        <Copy size={14} /> Copy
                                    </button>
                                </div>
                            </div>
                        )}

                        <div style={{ display: 'flex', gap: 8, marginBottom: 12, flexWrap: 'wrap' }}>
                            <button className="btn" type="button" onClick={() => downloadAgentConfig(enrollmentPackage)}>
                                <Download size={14} /> Download agent.json
                            </button>
                            <span style={{ fontSize: 12, color: 'var(--text-muted)', alignSelf: 'center' }}>
                                Alternative to the enrollment link: the user can import this file from the desktop app's Enrollment page.
                            </span>
                        </div>

                        <div style={{ display: 'grid', gap: 8 }}>
                            <div>
                                <div style={{ fontSize: 12, color: 'var(--text-muted)', marginBottom: 4 }}>Enrollment Token</div>
                                <div style={{ display: 'flex', gap: 8 }}>
                                    <input className="form-input" readOnly value={enrollmentPackage.enrollment_token} />
                                    <button className="btn" type="button" onClick={() => copy(enrollmentPackage.enrollment_token)}>
                                        <Copy size={14} /> Copy
                                    </button>
                                </div>
                            </div>
                        </div>
                    </div>
                )}

                <div className="page-filter-row">
                    <div className="filters-bar">
                        <input className="form-input" placeholder="Search devices..." value={search} onChange={(e) => setSearch(e.target.value)} style={{ width: 260 }} />
                        <select className="form-select" value={statusFilter} onChange={(e) => setStatusFilter(e.target.value)}>
                            <option value="">All Status</option>
                            <option value="active">Active</option>
                            <option value="pending">Pending</option>
                            <option value="suspended">Suspended</option>
                            <option value="decommissioned">Decommissioned</option>
                        </select>
                        <button className="btn" onClick={loadDevices}>
                            <RefreshCw size={14} /> Refresh
                        </button>
                    </div>
                    <div className="page-filter-meta">
                        <span className="shell-chip">{filtered.length} shown</span>
                    </div>
                </div>

                <div className="data-table-wrap">
                    {loading ? (
                        <div className="loading-wrap"><div className="spinner" /></div>
                    ) : filtered.length === 0 ? (
                        <div className="empty-state">
                            <Smartphone size={48} className="empty-state-icon" />
                            <div className="empty-state-text">No endpoint devices found</div>
                        </div>
                    ) : (
                        <table className="data-table">
                            <thead>
                                <tr>
                                    <th>Device</th>
                                    <th>OS</th>
                                    <th>Status</th>
                                    <th>Certificate</th>
                                    <th>Cert Expires</th>
                                    <th>Enrolled</th>
                                    <th>Actions</th>
                                </tr>
                            </thead>
                            <tbody>
                                {filtered.map((device) => (
                                    <tr key={device.device_id}>
                                        <td style={{ color: 'var(--text-primary)', fontWeight: 500 }}>{device.device_name}</td>
                                        <td><span className="badge">{device.os}</span></td>
                                        <td><span className="badge">{device.status}</span></td>
                                        <td style={{ fontFamily: 'monospace', fontSize: 11 }}>
                                            {device.cert_serial ? `${device.cert_serial.substring(0, 16)}...` : '-'}
                                        </td>
                                        <td>
                                            {device.cert_expires_at ? (() => {
                                                const daysLeft = Math.ceil((new Date(device.cert_expires_at) - new Date()) / (1000 * 60 * 60 * 24));
                                                const color = daysLeft <= 0 ? '#ef4444' : daysLeft <= 14 ? '#f59e0b' : 'inherit';
                                                const badge = daysLeft <= 0 ? ' (expired)' : daysLeft <= 14 ? ` (${daysLeft}d left)` : '';
                                                return <span style={{ color }}>{formatDate(device.cert_expires_at)}{badge}</span>;
                                            })() : '-'}
                                        </td>
                                        <td>{formatDate(device.enrolled_at)}</td>
                                        <td>
                                            <button className="btn" onClick={() => reissueToken(device.device_id)}>
                                                <KeyRound size={13} /> Reissue Token
                                            </button>
                                        </td>
                                    </tr>
                                ))}
                            </tbody>
                        </table>
                    )}
                </div>
            </div>
        </>
    );
}
