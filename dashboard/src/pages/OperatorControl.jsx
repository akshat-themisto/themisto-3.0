import { useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import { operatorApi } from '../api/operatorClient';
import { useToast } from '../context/ToastContext';
import { Activity, Building2, CheckCircle2, Copy, Download, PackageCheck, PlugZap, Power, ShieldOff, Terminal, Wifi, X } from 'lucide-react';

const emptyCreateForm = {
    name: '',
    slug: '',
    owner_email: '',
    owner_name: '',
    owner_password: '',
    public_backend_url: '',
    public_gateway_url: '',
};

function statusClass(status) {
    switch (status) {
        case 'active':
            return 'operator-status active';
        case 'suspended':
            return 'operator-status suspended';
        case 'offboarded':
            return 'operator-status offboarded';
        default:
            return 'operator-status';
    }
}

function formatDateTime(value) {
    return value ? new Date(value).toLocaleString('en-US', { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' }) : '-';
}

export default function OperatorControl() {
    const toast = useToast();
    const [orgs, setOrgs] = useState([]);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState('');
    const [savingOrg, setSavingOrg] = useState('');
    const [showRegister, setShowRegister] = useState(false);
    const [createForm, setCreateForm] = useState(emptyCreateForm);
    const [lastCreated, setLastCreated] = useState(null);
    const [packageOptions, setPackageOptions] = useState({});
    const [deploymentPackage, setDeploymentPackage] = useState(null);

    const totals = useMemo(() => {
        return orgs.reduce((acc, org) => {
            acc.total += 1;
            if (org.status === 'active') acc.active += 1;
            if (org.status === 'suspended') acc.suspended += 1;
            acc.devices += org.active_devices || 0;
            acc.certs += org.active_certs || 0;
            return acc;
        }, { total: 0, active: 0, suspended: 0, devices: 0, certs: 0 });
    }, [orgs]);

    async function loadOrgs() {
        setLoading(true);
        setError('');
        try {
            const data = await operatorApi.listOrgs();
            setOrgs(data.organizations || []);
        } catch (err) {
            setError(err.message || 'Could not load operator organizations');
        } finally {
            setLoading(false);
        }
    }

    useEffect(() => {
        loadOrgs();
    }, []);

    async function createOrg(e) {
        e.preventDefault();
        setSavingOrg('new');
        setError('');
        setLastCreated(null);
        try {
            const data = await operatorApi.createOrg(createForm);
            setLastCreated(data);
            setCreateForm(emptyCreateForm);
            setShowRegister(false);
            await loadOrgs();
            toast.success('Organization registered');
        } catch (err) {
            setError(err.message || 'Could not register organization');
        } finally {
            setSavingOrg('');
        }
    }

    async function updateProvisioning(org) {
        setSavingOrg(org.id);
        setError('');
        try {
            const backend = document.getElementById(`backend-${org.id}`)?.value || '';
            const gateway = document.getElementById(`gateway-${org.id}`)?.value || '';
            await operatorApi.updateProvisioning(org.id, {
                public_backend_url: backend.trim(),
                public_gateway_url: gateway.trim(),
            });
            await loadOrgs();
            toast.success('Provisioning URLs updated');
        } catch (err) {
            setError(err.message || 'Could not update provisioning');
        } finally {
            setSavingOrg('');
        }
    }

    async function changeStatus(org, status) {
        let reason = '';
        if (status !== 'active') {
            reason = window.prompt(status === 'suspended' ? 'Why are we suspending this org?' : 'Why are we offboarding this org?', org.status_reason || '');
            if (reason === null) return;
            reason = reason.trim();
            if (!reason) {
                setError('A reason is required for suspend/offboard.');
                return;
            }
        }
        const message = status === 'offboarded'
            ? 'Offboard this org? This revokes active certificates, expires enrollment, and decommissions devices.'
            : status === 'suspended'
                ? 'Suspend this org? Gateway access will stop after the control-plane cache expires.'
                : 'Reactivate this org? Gateway access resumes for valid active certificates.';
        if (!window.confirm(message)) return;

        setSavingOrg(org.id);
        setError('');
        try {
            await operatorApi.updateStatus(org.id, { status, reason });
            await loadOrgs();
            toast.success(`Organization ${status}`);
        } catch (err) {
            setError(err.message || 'Could not update organization status');
        } finally {
            setSavingOrg('');
        }
    }

    async function revokeCerts(org) {
        if (!window.confirm(`Revoke all active certificates for ${org.name}? Devices will need re-enrollment.`)) return;
        setSavingOrg(org.id);
        setError('');
        try {
            const res = await operatorApi.revokeOrgCerts(org.id);
            await loadOrgs();
            toast.success(`${res.revoked_certs || 0} certificates revoked`);
        } catch (err) {
            setError(err.message || 'Could not revoke certificates');
        } finally {
            setSavingOrg('');
        }
    }

    function updatePackageOption(orgId, key, value) {
        setPackageOptions((prev) => ({
            ...prev,
            [orgId]: {
                ...(prev[orgId] || {}),
                [key]: value,
            },
        }));
    }

    async function createDeploymentPackage(org) {
        setSavingOrg(`package-${org.id}`);
        setError('');
        try {
            const backend = document.getElementById(`backend-${org.id}`)?.value || org.public_backend_url || '';
            const gateway = document.getElementById(`gateway-${org.id}`)?.value || org.public_gateway_url || '';
            const options = packageOptions[org.id] || {};
            const data = await operatorApi.createDeploymentPackage(org.id, {
                device_name: (options.device_name || '').trim(),
                os: options.os || 'windows',
                agent_version: (options.agent_version || '1.0.0').trim(),
                token_ttl_hours: Number(options.token_ttl_hours || 24),
                public_backend_url: backend.trim(),
                public_gateway_url: gateway.trim(),
            });
            setDeploymentPackage(data);
            if (data.organization) {
                setOrgs((prev) => prev.map((item) => (item.id === data.organization.id ? data.organization : item)));
            } else {
                await loadOrgs();
            }
            toast.success('Deployment package prepared');
        } catch (err) {
            setError(err.message || 'Could not create deployment package');
        } finally {
            setSavingOrg('');
        }
    }

    async function copyText(value) {
        await navigator.clipboard.writeText(value || '');
        toast.success('Copied');
    }

    function downloadText(filename, value, type = 'text/plain') {
        const blob = new Blob([value || ''], { type });
        const url = URL.createObjectURL(blob);
        const link = document.createElement('a');
        link.href = url;
        link.download = filename;
        document.body.appendChild(link);
        link.click();
        link.remove();
        URL.revokeObjectURL(url);
    }

    return (
        <>
            <div className="topbar">
                <div className="topbar-copy">
                    <div className="page-kicker">Themisto Operations</div>
                    <h1 className="topbar-title">Control Panel</h1>
                    <div className="page-subtitle">
                        Register corporate workspaces, route them to the right VPS, and cut off access when a trial, contract, or deployment needs to stop.
                    </div>
                </div>
                <div className="topbar-actions">
                    <div className="page-summary-card">
                        <span className="page-summary-label">Coverage</span>
                        <strong>{totals.active} active orgs</strong>
                        <span>{totals.devices} active devices / {totals.certs} certs</span>
                    </div>
                    <button className="btn btn-primary" onClick={() => setShowRegister((value) => !value)}>
                        <Building2 size={14} /> Register Org
                    </button>
					<Link className="btn" to="/operator/fleet">
						<Activity size={14} /> Fleet Monitor
					</Link>
                </div>
            </div>

            <div className="page-content operator-page">
                {error && <div className="alert alert-danger">{error}</div>}

                {lastCreated?.api_key && (
                    <section className="operator-created-key">
                        <div>
                            <strong>New org API key</strong>
                            <span>Show this once. Store it for backend/device provisioning workflows.</span>
                        </div>
                        <code>{lastCreated.api_key}</code>
                    </section>
                )}

                {showRegister && (
                    <form className="operator-register-panel" onSubmit={createOrg}>
                        <div className="operator-panel-heading">
                            <h2>Register organization</h2>
                            <span>Create the customer workspace only when they are approved to use Themisto Labs.</span>
                        </div>
                        <div className="operator-register-grid">
                            <label>
                                Organization name
								<input className="form-input" value={createForm.name} onChange={(e) => setCreateForm((f) => ({ ...f, name: e.target.value }))} placeholder="Acme Corporation" />
                            </label>
                            <label>
                                Slug
                                <input className="form-input" value={createForm.slug} onChange={(e) => setCreateForm((f) => ({ ...f, slug: e.target.value.toLowerCase() }))} placeholder="lincoln-high" />
                            </label>
                            <label>
                                Owner email
                                <input className="form-input" value={createForm.owner_email} onChange={(e) => setCreateForm((f) => ({ ...f, owner_email: e.target.value }))} placeholder="admin@company.com" />
                            </label>
                            <label>
                                Owner name
                                <input className="form-input" value={createForm.owner_name} onChange={(e) => setCreateForm((f) => ({ ...f, owner_name: e.target.value }))} placeholder="Security Admin" />
                            </label>
                            <label>
                                Temporary owner password
                                <input className="form-input" type="password" value={createForm.owner_password} onChange={(e) => setCreateForm((f) => ({ ...f, owner_password: e.target.value }))} placeholder="Set initial password" />
                            </label>
                            <label>
                                Backend URL
                                <input className="form-input" value={createForm.public_backend_url} onChange={(e) => setCreateForm((f) => ({ ...f, public_backend_url: e.target.value }))} placeholder="https://api.company.themisto.ai" />
                            </label>
                            <label>
                                Gateway URL
                                <input className="form-input" value={createForm.public_gateway_url} onChange={(e) => setCreateForm((f) => ({ ...f, public_gateway_url: e.target.value }))} placeholder="https://gateway.company.themisto.ai" />
                            </label>
                        </div>
                        <div className="operator-register-actions">
                            <button type="button" className="btn" onClick={() => setShowRegister(false)}>Cancel</button>
                            <button className="btn btn-primary" disabled={savingOrg === 'new'}>
                                <CheckCircle2 size={14} /> {savingOrg === 'new' ? 'Registering...' : 'Register and Enable'}
                            </button>
                        </div>
                    </form>
                )}

                <section className="operator-grid">
                    {orgs.map((org) => {
                        const packageBusy = savingOrg === `package-${org.id}`;
                        const busy = savingOrg === org.id || packageBusy;
                        return (
                            <article className="operator-org-card" key={org.id}>
                                <div className="operator-org-head">
                                    <div>
                                        <div className="operator-org-title">{org.name}</div>
                                        <div className="operator-org-meta">{org.slug} · {org.id}</div>
                                    </div>
                                    <span className={statusClass(org.status)}>{org.status}</span>
                                </div>

                                <div className="operator-org-stats">
                                    <span><strong>{org.active_devices || 0}</strong> active devices</span>
                                    <span><strong>{org.active_certs || 0}</strong> active certs</span>
                                    <span><strong>{formatDateTime(org.last_seen_at)}</strong> last seen</span>
                                </div>

                                {org.status_reason && (
                                    <div className="operator-status-reason">
                                        {org.status_reason}
                                    </div>
                                )}

                                <div className="operator-url-grid">
                                    <label>
                                        Backend VPS URL
                                        <input id={`backend-${org.id}`} className="form-input" defaultValue={org.public_backend_url || ''} placeholder="https://api.company.themisto.ai" />
                                    </label>
                                    <label>
                                        Gateway VPS URL
                                        <input id={`gateway-${org.id}`} className="form-input" defaultValue={org.public_gateway_url || ''} placeholder="https://gateway.company.themisto.ai" />
                                    </label>
                                </div>

                                <div className="operator-deployment-box">
                                    <div className="operator-deployment-head">
                                        <strong>Deployment package</strong>
                                        <span>Create a real enrollment payload for installer embedding, MDM, or a demo workstation.</span>
                                    </div>
                                    <div className="operator-package-grid">
                                        <label>
                                            Device label
                                            <input
                                                className="form-input"
                                                value={packageOptions[org.id]?.device_name || ''}
                                                onChange={(e) => updatePackageOption(org.id, 'device_name', e.target.value)}
                                                placeholder="deployment-lab-01"
                                            />
                                        </label>
                                        <label>
                                            OS
                                            <select
                                                className="form-input"
                                                value={packageOptions[org.id]?.os || 'windows'}
                                                onChange={(e) => updatePackageOption(org.id, 'os', e.target.value)}
                                            >
                                                <option value="windows">Windows</option>
                                                <option value="darwin">macOS</option>
                                                <option value="linux">Linux</option>
                                            </select>
                                        </label>
                                        <label>
                                            Token life
                                            <select
                                                className="form-input"
                                                value={packageOptions[org.id]?.token_ttl_hours || 24}
                                                onChange={(e) => updatePackageOption(org.id, 'token_ttl_hours', e.target.value)}
                                            >
                                                <option value={24}>24 hours</option>
                                                <option value={72}>3 days</option>
                                                <option value={168}>7 days</option>
                                            </select>
                                        </label>
                                    </div>
                                </div>

                                <div className="operator-action-row">
                                    <button className="btn btn-sm" disabled={busy} onClick={() => updateProvisioning(org)}>
                                        <Wifi size={13} /> Save VPS
                                    </button>
                                    <button className="btn btn-sm btn-primary" disabled={busy} onClick={() => createDeploymentPackage(org)}>
                                        <PackageCheck size={13} /> {packageBusy ? 'Preparing...' : 'Prepare Package'}
                                    </button>
                                    {org.status !== 'active' ? (
                                        <button className="btn btn-sm" disabled={busy} onClick={() => changeStatus(org, 'active')}>
                                            <Power size={13} /> Reactivate
                                        </button>
                                    ) : (
                                        <button className="btn btn-sm" disabled={busy} onClick={() => changeStatus(org, 'suspended')}>
                                            <PlugZap size={13} /> Suspend
                                        </button>
                                    )}
                                    <button className="btn btn-sm btn-danger" disabled={busy || org.status === 'offboarded'} onClick={() => changeStatus(org, 'offboarded')}>
                                        <ShieldOff size={13} /> Offboard
                                    </button>
                                    <button className="btn btn-sm btn-danger" disabled={busy || !org.active_certs} onClick={() => revokeCerts(org)}>
                                        Revoke Certs
                                    </button>
                                </div>
                            </article>
                        );
                    })}
                    {!loading && orgs.length === 0 && (
                        <div className="empty-state">No organizations loaded yet.</div>
                    )}
                </section>
            </div>

            {deploymentPackage && (
                <div className="modal-overlay operator-package-overlay" onClick={() => setDeploymentPackage(null)}>
                    <div className="modal operator-package-modal" onClick={(e) => e.stopPropagation()}>
                        <div className="operator-package-modal-head">
                            <div>
                                <div className="page-kicker">Deployment package</div>
                                <h2 className="modal-title">{deploymentPackage.organization?.name || deploymentPackage.package?.org_name}</h2>
                                <p>
                                    One enrollment package was created for {deploymentPackage.package?.device_name}. Token expires {formatDateTime(deploymentPackage.package?.token_expires_at)}.
                                </p>
                            </div>
                            <button className="policy-modal-close" type="button" onClick={() => setDeploymentPackage(null)} aria-label="Close deployment package">
                                <X size={18} />
                            </button>
                        </div>

                        <div className="operator-preflight-grid">
                            {(deploymentPackage.preflight || []).map((check) => (
                                <div className={`operator-preflight-card ${check.status}`} key={check.key}>
                                    <CheckCircle2 size={15} />
                                    <div>
                                        <strong>{check.label}</strong>
                                        <span>{check.message}</span>
                                    </div>
                                </div>
                            ))}
                        </div>

                        <div className="operator-artifact-grid">
                            <section className="operator-artifact-card">
                                <div className="operator-artifact-head">
                                    <strong>agent.json</strong>
                                    <div>
                                        <button className="btn btn-sm" onClick={() => copyText(deploymentPackage.agent_config_json)}>
                                            <Copy size={13} /> Copy
                                        </button>
                                        <button className="btn btn-sm" onClick={() => downloadText(`agent-${deploymentPackage.package?.device_id}.json`, deploymentPackage.agent_config_json, 'application/json')}>
                                            <Download size={13} /> Download
                                        </button>
                                    </div>
                                </div>
                                <pre>{deploymentPackage.agent_config_json}</pre>
                            </section>

                            <section className="operator-artifact-card">
                                <div className="operator-artifact-head">
                                    <strong>Windows install helper</strong>
                                    <div>
                                        <button className="btn btn-sm" onClick={() => copyText(deploymentPackage.windows_install_ps1)}>
                                            <Copy size={13} /> Copy
                                        </button>
                                        <button className="btn btn-sm" onClick={() => downloadText('install-themisto.ps1', deploymentPackage.windows_install_ps1)}>
                                            <Download size={13} /> Download
                                        </button>
                                    </div>
                                </div>
                                <pre>{deploymentPackage.windows_install_ps1}</pre>
                            </section>

                            <section className="operator-artifact-card">
                                <div className="operator-artifact-head">
                                    <strong>Chromium force-install policy</strong>
                                    <button className="btn btn-sm" onClick={() => copyText(deploymentPackage.chromium_policy_json)}>
                                        <Copy size={13} /> Copy
                                    </button>
                                </div>
                                <pre>{deploymentPackage.chromium_policy_json}</pre>
                            </section>

                            <section className="operator-artifact-card">
                                <div className="operator-artifact-head">
                                    <strong>Activation commands</strong>
                                    <Terminal size={15} />
                                </div>
                                <pre>{deploymentPackage.package?.activate_windows_command}</pre>
                                <pre>{deploymentPackage.package?.activate_macos_command}</pre>
                            </section>
                        </div>

                        <div className="operator-notes">
                            {(deploymentPackage.mdm_notes || []).map((note) => (
                                <div key={note}>{note}</div>
                            ))}
                        </div>
                    </div>
                </div>
            )}
        </>
    );
}
