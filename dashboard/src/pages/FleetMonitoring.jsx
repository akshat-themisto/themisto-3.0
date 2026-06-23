import { useCallback, useEffect, useMemo, useState } from 'react';
import {
    Activity,
    AlertTriangle,
    CheckCircle2,
    Cpu,
    MonitorCog,
    Radio,
    RefreshCw,
    Search,
    ShieldAlert,
    ShieldCheck,
    Wifi,
    WifiOff,
} from 'lucide-react';
import { operatorApi } from '../api/operatorClient';

const statusMeta = {
    connected: { label: 'Connected', icon: Wifi },
    degraded: { label: 'Degraded', icon: AlertTriangle },
    offline: { label: 'Offline', icon: WifiOff },
    never_seen: { label: 'Never Seen', icon: Radio },
    suspected_tamper: { label: 'Suspected Tamper', icon: ShieldAlert },
    managed_inactive: { label: 'Managed Inactive', icon: MonitorCog },
};

function formatDate(value) {
    if (!value) return 'Never';
    return new Date(value).toLocaleString('en-US', {
        month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', second: '2-digit',
    });
}

function relativeTime(value) {
    if (!value) return 'no signal';
    const seconds = Math.max(0, Math.round((Date.now() - new Date(value).getTime()) / 1000));
    if (seconds < 60) return `${seconds}s ago`;
    if (seconds < 3600) return `${Math.floor(seconds / 60)}m ago`;
    if (seconds < 86400) return `${Math.floor(seconds / 3600)}h ago`;
    return `${Math.floor(seconds / 86400)}d ago`;
}

function StatusBadge({ value }) {
    const meta = statusMeta[value] || statusMeta.offline;
    const Icon = meta.icon;
    return (
        <span className={`fleet-status ${value || 'offline'}`}>
            <Icon size={13} /> {meta.label}
        </span>
    );
}

function ComponentState({ label, value, healthyValues = ['healthy', 'ok', true] }) {
    const healthy = healthyValues.includes(value);
    return (
        <span className={`fleet-component ${healthy ? 'healthy' : 'unhealthy'}`} title={`${label}: ${String(value || 'unknown')}`}>
            {healthy ? <CheckCircle2 size={12} /> : <AlertTriangle size={12} />}
            {label}
        </span>
    );
}

function signalLabel(type) {
    return ({
        'proxy.tamper_detected': 'Proxy settings changed outside Themisto',
        'proxy.listener_unreachable': 'Agent proxy listener became unreachable',
        'proxy.remediated': 'Agent restored managed proxy settings',
        'agent.integrity_error': 'Agent could not verify integrity',
        'agent.stopped': 'Agent reported a clean shutdown',
    })[type] || type;
}

export default function FleetMonitoring() {
    const [data, setData] = useState({ summary: {}, devices: [], signals: [] });
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState('');
    const [query, setQuery] = useState('');
    const [status, setStatus] = useState('');
    const [autoRefresh, setAutoRefresh] = useState(true);

    const loadFleet = useCallback(async (quiet = false) => {
        if (!quiet) setLoading(true);
        setError('');
        try {
            const response = await operatorApi.fleet({ signal_limit: 100 });
            setData(response || { summary: {}, devices: [], signals: [] });
        } catch (err) {
            setError(err.message || 'Could not load fleet monitoring');
        } finally {
            if (!quiet) setLoading(false);
        }
    }, []);

    useEffect(() => {
        loadFleet();
    }, [loadFleet]);

    useEffect(() => {
        if (!autoRefresh) return undefined;
        const timer = window.setInterval(() => loadFleet(true), 15000);
        return () => window.clearInterval(timer);
    }, [autoRefresh, loadFleet]);

    const filteredDevices = useMemo(() => {
        const needle = query.trim().toLowerCase();
        return (data.devices || []).filter((device) => {
            if (status && device.connectivity !== status) return false;
            if (!needle) return true;
            return [device.device_name, device.device_id, device.os, device.agent_version]
                .some((value) => String(value || '').toLowerCase().includes(needle));
        });
    }, [data.devices, query, status]);

    const summary = data.summary || {};

    return (
        <>
            <div className="topbar">
                <div className="topbar-copy">
                    <div className="page-kicker">Endpoint Operations</div>
                    <h1 className="topbar-title">Fleet Monitor</h1>
                    <div className="page-subtitle">Agent availability, component health, integrity evidence, and remediation for this customer deployment.</div>
                </div>
                <div className="topbar-actions">
                    <button className="btn" onClick={() => loadFleet()} disabled={loading}>
                        <RefreshCw size={14} className={loading ? 'spin' : ''} /> Refresh
                    </button>
                </div>
            </div>

            <div className="page-content operator-page fleet-page">
                <div className="fleet-refresh-row">
                    <label className="fleet-auto-refresh"><input type="checkbox" checked={autoRefresh} onChange={(e) => setAutoRefresh(e.target.checked)} /> Live refresh</label>
                </div>

                {error && <div className="alert alert-danger">{error}</div>}

                <section className="fleet-summary-grid">
                    <button className={`fleet-summary ${status === '' ? 'selected' : ''}`} onClick={() => setStatus('')}>
                        <Activity size={18} /><span>Managed Devices</span><strong>{summary.total || 0}</strong>
                    </button>
                    <button className={`fleet-summary connected ${status === 'connected' ? 'selected' : ''}`} onClick={() => setStatus('connected')}>
                        <Wifi size={18} /><span>Connected</span><strong>{summary.connected || 0}</strong>
                    </button>
                    <button className={`fleet-summary degraded ${status === 'degraded' ? 'selected' : ''}`} onClick={() => setStatus('degraded')}>
                        <AlertTriangle size={18} /><span>Degraded</span><strong>{summary.degraded || 0}</strong>
                    </button>
                    <button className={`fleet-summary offline ${status === 'offline' ? 'selected' : ''}`} onClick={() => setStatus('offline')}>
                        <WifiOff size={18} /><span>Offline</span><strong>{summary.offline || 0}</strong>
                    </button>
                    <button className={`fleet-summary tamper ${status === 'suspected_tamper' ? 'selected' : ''}`} onClick={() => setStatus('suspected_tamper')}>
                        <ShieldAlert size={18} /><span>Suspected Tamper</span><strong>{summary.suspected_tamper || 0}</strong>
                    </button>
                    <button className={`fleet-summary ${status === 'never_seen' ? 'selected' : ''}`} onClick={() => setStatus('never_seen')}>
                        <Radio size={18} /><span>Never Seen</span><strong>{summary.never_seen || 0}</strong>
                    </button>
                </section>

                <section className="fleet-workspace">
                    <div className="fleet-section-head">
                        <div><h2>Device Health</h2><span>{filteredDevices.length} devices shown · generated {formatDate(data.generated_at)}</span></div>
                        <div className="fleet-search"><Search size={15} /><input value={query} onChange={(e) => setQuery(e.target.value)} placeholder="Search device, OS, version" /></div>
                    </div>
                    <div className="data-table-wrap fleet-table-wrap">
                        <table className="data-table fleet-table">
                            <thead><tr><th>Device</th><th>State</th><th>Last Signal</th><th>Components</th><th>Version</th><th>Certificate</th></tr></thead>
                            <tbody>
                                {filteredDevices.map((device) => (
                                    <tr key={device.device_id}>
                                        <td><strong>{device.device_name}</strong><span>{device.os}</span></td>
                                        <td><StatusBadge value={device.connectivity} /><small>{device.health_reason}</small></td>
                                        <td><strong>{relativeTime(device.last_seen_at)}</strong><span>{formatDate(device.last_seen_at)}</span></td>
                                        <td><div className="fleet-components">
                                            <ComponentState label="Gateway" value={device.gateway_connected} />
                                            <ComponentState label="Proxy" value={device.proxy_listener_alive} />
                                            <ComponentState label="Capture" value={device.prompt_capture} />
                                            <ComponentState label="Classifier" value={device.semantic_classifier} healthyValues={['healthy', 'disabled']} />
											<ComponentState label="Service" value={device.service_status} healthyValues={['running']} />
                                            <ComponentState label="Integrity" value={device.proxy_integrity} />
                                        </div></td>
                                        <td><strong>{device.agent_version || '-'}</strong><span>{device.policy_version || 'No policy version'}</span></td>
                                        <td><strong>{device.certificate_status || 'none'}</strong><span>{formatDate(device.certificate_expires_at)}</span></td>
                                    </tr>
                                ))}
                                {!filteredDevices.length && <tr><td colSpan="6"><div className="empty-state"><Cpu size={24} /><div>No devices match this view.</div></div></td></tr>}
                            </tbody>
                        </table>
                    </div>
                </section>

                <section className="fleet-workspace">
                    <div className="fleet-section-head">
                        <div><h2>Integrity Timeline</h2><span>Warnings, tamper evidence, clean shutdowns, and automatic remediation</span></div>
                        <ShieldCheck size={20} />
                    </div>
                    <div className="fleet-signal-list">
                        {(data.signals || []).map((signal) => (
                            <article className={`fleet-signal ${signal.severity}`} key={signal.id}>
                                <span className="fleet-signal-icon">{signal.severity === 'critical' ? <ShieldAlert size={16} /> : <AlertTriangle size={16} />}</span>
                                <div><strong>{signalLabel(signal.event_type)}</strong><span>{signal.device_name}</span></div>
                                <time title={formatDate(signal.timestamp)}>{relativeTime(signal.timestamp)}</time>
                            </article>
                        ))}
                        {!(data.signals || []).length && <div className="empty-state"><ShieldCheck size={26} /><div>No integrity warnings have been reported.</div></div>}
                    </div>
                </section>
            </div>
        </>
    );
}
