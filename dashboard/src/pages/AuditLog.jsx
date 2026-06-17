import { useEffect, useState } from 'react';
import { api } from '../api/client';
import { ScrollText, RefreshCw } from 'lucide-react';

export default function AuditLog() {
    const [entries, setEntries] = useState([]);
    const [total, setTotal] = useState(0);
    const [page, setPage] = useState(1);
    const [loading, setLoading] = useState(true);
    const [action, setAction] = useState('');
    const [actorType, setActorType] = useState('');
    const limit = 20;

    function loadAudit() {
        setLoading(true);
        api.listAudit({ page, limit, action, actor_type: actorType })
            .then((data) => {
                setEntries(data.entries || []);
                setTotal(data.total || 0);
            })
            .catch(() => {
                setEntries([]);
                setTotal(0);
            })
            .finally(() => setLoading(false));
    }

    useEffect(() => {
        loadAudit();
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [page, action, actorType]);

    const totalPages = Math.ceil(total / limit) || 1;
    const formatDate = (value) => new Date(value).toLocaleString('en-US', { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' });

    return (
        <>
            <div className="topbar">
                <div className="topbar-copy">
                    <div className="page-kicker">Change History</div>
                    <h1 className="topbar-title">Audit Log</h1>
                    <div className="page-subtitle">
                        Review device registrations, certificate lifecycle events, and administrative actions across the workspace.
                    </div>
                </div>
                <div className="topbar-actions">
                    <div className="page-summary-card">
                        <span className="page-summary-label">Recorded activity</span>
                        <strong>{total} entries</strong>
                        <span>Page {page} of {totalPages}</span>
                    </div>
                </div>
            </div>

            <div className="page-content">
                <div className="page-filter-row">
                    <div className="filters-bar">
                        <select className="form-select" value={action} onChange={(e) => { setAction(e.target.value); setPage(1); }}>
                            <option value="">All Actions</option>
                            <option value="device.registered">Device Registered</option>
                            <option value="cert.issued">Cert Issued</option>
                            <option value="cert.revoked">Cert Revoked</option>
                            <option value="policy.blocked">Policy Blocked</option>
                        </select>
                        <select className="form-select" value={actorType} onChange={(e) => { setActorType(e.target.value); setPage(1); }}>
                            <option value="">All Actors</option>
                            <option value="admin">Admin</option>
                            <option value="system">System</option>
                            <option value="device">Device</option>
                            <option value="gateway">Gateway</option>
                        </select>
                        <button className="btn" onClick={loadAudit}>
                            <RefreshCw size={14} /> Refresh
                        </button>
                    </div>
                    <div className="page-filter-meta">
                        <span className="shell-chip">{entries.length} visible</span>
                    </div>
                </div>

                <div className="data-table-wrap">
                    {loading ? (
                        <div className="loading-wrap"><div className="spinner" /></div>
                    ) : entries.length === 0 ? (
                        <div className="empty-state">
                            <ScrollText size={48} className="empty-state-icon" />
                            <div className="empty-state-text">No audit entries</div>
                        </div>
                    ) : (
                        <>
                            <table className="data-table">
                                <thead>
                                    <tr>
                                        <th>Timestamp</th>
                                        <th>Action</th>
                                        <th>Actor</th>
                                        <th>Resource</th>
                                        <th>Details</th>
                                    </tr>
                                </thead>
                                <tbody>
                                    {entries.map((entry) => (
                                        <tr key={entry.id}>
                                            <td style={{ whiteSpace: 'nowrap', color: 'var(--text-secondary)' }}>{formatDate(entry.timestamp)}</td>
                                            <td><span className="badge">{entry.action}</span></td>
                                            <td style={{ color: 'var(--text-muted)' }}>
                                                {entry.actor_type}:<span style={{ color: 'var(--text-secondary)' }}>{entry.actor_id}</span>
                                            </td>
                                            <td style={{ color: 'var(--text-muted)' }}>
                                                {entry.resource_type}:<span style={{ color: 'var(--text-secondary)' }}>{entry.resource_id?.substring(0, 8)}</span>
                                            </td>
                                            <td style={{ maxWidth: 240, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', color: 'var(--text-muted)', fontSize: 12 }}>
                                                {entry.details ? JSON.stringify(entry.details) : '-'}
                                            </td>
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
