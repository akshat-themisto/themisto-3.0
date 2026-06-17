import { useState, useEffect } from 'react';
import { api } from '../api/client';
import { useAuth } from '../context/AuthContext';
import { Check, Plus, AlertTriangle, LogOut } from 'lucide-react';
import { triggerDashboardOnboarding } from '../lib/onboarding';

export default function Settings() {
    const { user, refreshUser, logout } = useAuth();
    const [users, setUsers] = useState([]);
    const [loading, setLoading] = useState(true);
    const [orgForm, setOrgForm] = useState({ name: '', slug: '' });
    const [showAddUser, setShowAddUser] = useState(false);
    const [newUser, setNewUser] = useState({ email: '', name: '', password: '', role: 'viewer' });
    const [showPassword, setShowPassword] = useState(user?.must_change_password || false);
    const [pwForm, setPwForm] = useState({ current_password: '', new_password: '' });
    const [message, setMessage] = useState('');

    function loadSettings() {
        setLoading(true);
        api.getSettings()
            .then(d => {
                setOrgForm({ name: d.organization?.name || '', slug: d.organization?.slug || '' });
                setUsers(d.users || []);
            })
            .catch(console.error)
            .finally(() => setLoading(false));
    }

    useEffect(() => {
        api.getSettings()
            .then(d => {
                setOrgForm({ name: d.organization?.name || '', slug: d.organization?.slug || '' });
                setUsers(d.users || []);
            })
            .catch(console.error)
            .finally(() => setLoading(false));
    }, []);

    const saveOrg = async () => {
        try {
            await api.updateSettings(orgForm);
            setMessage('Organization updated');
            setTimeout(() => setMessage(''), 3000);
        } catch (err) {
            alert(err.message);
        }
    };

    const addUser = async () => {
        try {
            await api.createUser(newUser);
            setShowAddUser(false);
            setNewUser({ email: '', name: '', password: '', role: 'viewer' });
            loadSettings();
        } catch (err) {
            alert(err.message);
        }
    };

    const removeUser = async (id) => {
        if (!confirm('Remove this user?')) return;
        try {
            await api.deleteUser(id);
            loadSettings();
        } catch (err) {
            alert(err.message);
        }
    };

    const changePassword = async () => {
        try {
            await api.changePassword(pwForm);
            setShowPassword(false);
            setPwForm({ current_password: '', new_password: '' });
            setMessage('Password updated');
            setTimeout(() => setMessage(''), 3000);
            if (refreshUser) refreshUser();
        } catch (err) {
            alert(err.message);
        }
    };

    const exportPolicySnapshot = async () => {
        try {
            const snapshot = await api.evidencePolicySnapshot();
            const blob = new Blob([JSON.stringify(snapshot, null, 2)], { type: 'application/json' });
            const url = URL.createObjectURL(blob);
            const a = document.createElement('a');
            a.href = url;
            a.download = 'policy-snapshot.json';
            a.click();
            URL.revokeObjectURL(url);
        } catch (err) {
            alert(err.message || 'Failed to export policy snapshot');
        }
    };

    if (loading) return <div className="loading-wrap"><div className="spinner" /></div>;

    const isOwner = user?.role === 'owner';
    const isAdmin = user?.role === 'admin' || isOwner;
    const passwordLocked = Boolean(user?.must_change_password);

    return (
        <>
            <div className="topbar">
                <div className="topbar-copy">
                    <div className="page-kicker">Workspace Controls</div>
                    <h1 className="topbar-title">Settings</h1>
                    <div className="page-subtitle">
                        Manage organization details, admin access, password rotation, and export controls for this workspace.
                    </div>
                </div>
                <div className="topbar-actions">
                    {message && (
                        <span className="shell-chip shell-chip-accent" style={{ gap: 6 }}>
                            <Check size={14} /> {message}
                        </span>
                    )}
                    <button className="btn" onClick={triggerDashboardOnboarding}>
                        Show Welcome Guide
                    </button>
                    <button className="btn" onClick={logout}>
                        <LogOut size={14} /> Log Out
                    </button>
                </div>
            </div>
            <div className="page-content">
                {user?.must_change_password && (
                    <div style={{
                        background: 'rgba(251, 191, 36, 0.1)',
                        border: '1px solid rgba(251, 191, 36, 0.3)',
                        borderRadius: 8,
                        padding: '14px 20px',
                        marginBottom: 24,
                        display: 'flex',
                        alignItems: 'center',
                        gap: 10,
                        color: '#fbbf24',
                        fontSize: 14,
                    }}>
                        <AlertTriangle size={18} />
                        <span>You must change your password before continuing. Please update it below.</span>
                    </div>
                )}
                {/* Org Settings */}
                <div className="section">
                    <h2 className="section-title">Organization</h2>
                    <div className="data-table-wrap" style={{ padding: 32 }}>
                        <div style={{ display: 'flex', gap: 24, marginBottom: 20 }}>
                            <div className="form-group" style={{ flex: 1 }}>
                                <label className="form-label">Name</label>
                                <input className="form-input" value={orgForm.name} onChange={e => setOrgForm(f => ({ ...f, name: e.target.value }))} disabled={!isAdmin || passwordLocked} />
                            </div>
                            <div className="form-group" style={{ flex: 1 }}>
                                <label className="form-label">Slug</label>
                                <input className="form-input" value={orgForm.slug} onChange={e => setOrgForm(f => ({ ...f, slug: e.target.value }))} disabled={!isAdmin || passwordLocked} />
                            </div>
                        </div>
                        {passwordLocked && (
                            <div style={{ color: 'var(--text-muted)', fontSize: 13, marginTop: 8 }}>
                                Change your password first to unlock organization changes.
                            </div>
                        )}
                        {isAdmin && !passwordLocked && <button className="btn btn-primary" onClick={saveOrg}>Save Changes</button>}
                    </div>
                </div>

                {/* Users */}
                <div className="section">
                    <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 16 }}>
                        <h2 className="section-title" style={{ margin: 0 }}>Admin Users</h2>
                        {isOwner && !passwordLocked && <button className="btn btn-primary" onClick={() => setShowAddUser(true)}>
                            <Plus size={16} /> Add User
                        </button>}
                    </div>
                    {passwordLocked && (
                        <div style={{ color: 'var(--text-muted)', fontSize: 13, marginBottom: 12 }}>
                            User management unlocks after you finish the required password update.
                        </div>
                    )}
                    <div className="data-table-wrap">
                        <table className="data-table">
                            <thead>
                                <tr>
                                    <th>Name</th>
                                    <th>Email</th>
                                    <th>Role</th>
                                    {isOwner && <th>Actions</th>}
                                </tr>
                            </thead>
                            <tbody>
                                {users.map(u => (
                                    <tr key={u.id}>
                                        <td style={{ color: 'var(--text-primary)', fontWeight: 500 }}>
                                            {u.name} {u.id === user?.id && <span style={{ fontSize: 11, color: 'var(--text-muted)', marginLeft: 6 }}>(you)</span>}
                                        </td>
                                        <td style={{ color: 'var(--text-secondary)' }}>{u.email}</td>
                                        <td><span className="badge">{u.role}</span></td>
                                        {isOwner && !passwordLocked && (
                                            <td>
                                                {u.id !== user?.id && (
                                                    <button className="btn btn-sm btn-danger" style={{ background: 'transparent', borderColor: 'transparent' }} onClick={() => removeUser(u.id)}>Remove</button>
                                                )}
                                            </td>
                                        )}
                                    </tr>
                                ))}
                            </tbody>
                        </table>
                    </div>
                </div>

                {/* Password */}
                <div className="section">
                    <h2 className="section-title">Security</h2>
                    <div className="data-table-wrap" style={{ padding: 32 }}>
                        {!showPassword ? (
                            <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap' }}>
                                <button className="btn" onClick={() => setShowPassword(true)}>Change Password</button>
                                <button className="btn" onClick={logout}>
                                    <LogOut size={14} /> Log Out
                                </button>
                            </div>
                        ) : (
                            <div>
                                <div style={{ display: 'flex', gap: 24, marginBottom: 20 }}>
                                    <div className="form-group" style={{ flex: 1 }}>
                                        <label className="form-label">Current Password</label>
                                        <input className="form-input" type="password" value={pwForm.current_password} onChange={e => setPwForm(f => ({ ...f, current_password: e.target.value }))} />
                                    </div>
                                    <div className="form-group" style={{ flex: 1 }}>
                                        <label className="form-label">New Password</label>
                                        <input className="form-input" type="password" value={pwForm.new_password} onChange={e => setPwForm(f => ({ ...f, new_password: e.target.value }))} />
                                    </div>
                                </div>
                                <div style={{ display: 'flex', gap: 12 }}>
                                    <button className="btn btn-primary" onClick={changePassword}>Update Password</button>
                                    {!passwordLocked && <button className="btn" onClick={() => setShowPassword(false)} style={{ border: 'none', background: 'transparent' }}>Cancel</button>}
                                </div>
                            </div>
                        )}
                    </div>
                </div>
                {/* Evidence Exports */}
                {isAdmin && !passwordLocked && (
                    <div className="section">
                        <h2 className="section-title">Compliance Evidence (Audit Support)</h2>
                        <div className="data-table-wrap" style={{ padding: 24 }}>
                            <div style={{ color: 'var(--text-muted)', fontSize: 13, marginBottom: 12 }}>
                                Export policy snapshots, DLP events, and audit history for external reviews and internal governance.
                            </div>
                            <div style={{ display: 'flex', gap: 10, flexWrap: 'wrap' }}>
                                <button className="btn" onClick={exportPolicySnapshot}>Export Policy Snapshot (JSON)</button>
                                <button className="btn" onClick={() => window.open(api.evidenceDLPCSVUrl(), '_blank', 'noopener')}>Export Security Events (CSV)</button>
                                <button className="btn" onClick={() => window.open(api.evidenceAuditCSVUrl(), '_blank', 'noopener')}>Export Audit Log (CSV)</button>
                            </div>
                        </div>
                    </div>
                )}

                <div className="section">
                    <h2 className="section-title">Workspace Tour</h2>
                    <div className="data-table-wrap" style={{ padding: 24 }}>
                        <div style={{ color: 'var(--text-muted)', fontSize: 13, marginBottom: 12 }}>
                            Replay the first-run welcome screen and guided tour whenever you want a quick orientation for yourself or a teammate.
                        </div>
                        <button className="btn" onClick={triggerDashboardOnboarding}>Replay Welcome Guide</button>
                    </div>
                </div>

                {/* Add User Modal */}
                {showAddUser && (
                    <div className="modal-overlay" onClick={() => setShowAddUser(false)}>
                        <div className="modal" onClick={e => e.stopPropagation()}>
                            <h2 className="modal-title">Add Admin User</h2>
                            <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
                                <div className="form-group">
                                    <label className="form-label">Name</label>
                                    <input className="form-input" value={newUser.name} onChange={e => setNewUser(u => ({ ...u, name: e.target.value }))} />
                                </div>
                                <div className="form-group">
                                    <label className="form-label">Email</label>
                                    <input className="form-input" type="email" value={newUser.email} onChange={e => setNewUser(u => ({ ...u, email: e.target.value }))} />
                                </div>
                                <div className="form-group">
                                    <label className="form-label">Password</label>
                                    <input className="form-input" type="password" value={newUser.password} onChange={e => setNewUser(u => ({ ...u, password: e.target.value }))} />
                                </div>
                                <div className="form-group">
                                    <label className="form-label">Role</label>
                                    <select className="form-select" value={newUser.role} onChange={e => setNewUser(u => ({ ...u, role: e.target.value }))}>
                                        <option value="viewer">Viewer</option>
                                        <option value="admin">Admin</option>
                                        <option value="owner">Owner</option>
                                    </select>
                                </div>
                            </div>
                            <div className="modal-actions">
                                <button className="btn" onClick={() => setShowAddUser(false)} style={{ border: 'none', background: 'transparent' }}>Cancel</button>
                                <button className="btn btn-primary" onClick={addUser}>Add User</button>
                            </div>
                        </div>
                    </div>
                )}
            </div>
        </>
    );
}
