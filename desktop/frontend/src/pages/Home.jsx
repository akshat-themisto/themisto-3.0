import { useMemo } from 'react';
import { useNavigate } from 'react-router-dom';
import { AlertCircle, AlertTriangle, ArrowRight, Info, RefreshCw, Shield, ShieldAlert, ShieldOff } from 'lucide-react';
import { useAgentStatus, useAgentConfig, useDiagnostics, useProxyRecovery } from '../hooks/useAgentStatus';

export default function Home() {
  const navigate = useNavigate();
  const { status, loading, refresh } = useAgentStatus();
  const { config } = useAgentConfig();
  const { snapshot, collect } = useDiagnostics(2000);
  const { recovery, resetting, resetResult, resetProxy } = useProxyRecovery();

  const running = status?.running;
  const enrolled = config?.enrolled;
  const gatewayConnected = status?.gateway_connected;
  const proxyState = status?.proxy_state;
  const gatewayState = status?.gateway_state;

  // Derive protection summary from snapshot issues (single source of truth).
  let summaryTone = 'metric-value-danger';
  let summaryTitle = 'Disconnected';
  let summaryCopy = 'The local agent is not currently running on this device.';
  let summaryIcon = <ShieldOff size={18} />;
  const issues = snapshot?.issues || [];
  const startupIssues = issues.filter((issue) => issue.protection_impact === 'starting');
  const protectionIssues = issues.filter((issue) => ['error', 'degraded'].includes(issue.protection_impact));
  const maintenanceIssues = issues.filter((issue) => issue.protection_impact === 'none');

  if (snapshot) {
    const firstError = protectionIssues.find((i) => i.protection_impact === 'error') || issues.find((i) => i.severity === 'error');
    const firstWarning = protectionIssues.find((i) => i.protection_impact === 'degraded') || issues.find((i) => i.severity === 'warning');
    const firstStartup = startupIssues[0];
    const firstAdvisory = maintenanceIssues[0];

    if (snapshot.summary_status === 'healthy') {
      summaryTone = 'metric-value-success';
      summaryTitle = 'Protected';
      if (firstAdvisory?.title === 'Startup persistence missing') {
        summaryCopy = 'Protected right now. Startup persistence should be repaired before the next reboot.';
      } else if (snapshot.advisory_count > 0) {
        summaryCopy = `Protected right now, with ${snapshot.advisory_count} maintenance advisory${snapshot.advisory_count === 1 ? '' : 'ies'} for follow-up.`;
      } else {
        summaryCopy = 'The Themisto agent is healthy and the secure gateway is reachable.';
      }
      summaryIcon = <Shield size={18} />;
    } else if (snapshot.summary_status === 'starting') {
      summaryTone = 'metric-value-neutral';
      summaryTitle = firstStartup?.title === 'Gateway retrying after startup' ? 'Retrying gateway' : 'Starting';
      summaryCopy = firstStartup?.detail || 'Themisto is still bringing the protection stack online.';
      summaryIcon = <RefreshCw size={18} />;
    } else if (snapshot.summary_status === 'degraded') {
      summaryTone = 'metric-value-warning';
      summaryTitle = 'Degraded';
      summaryCopy = firstWarning?.detail || 'Some subsystems need attention.';
      summaryIcon = <ShieldAlert size={18} />;
    } else if (snapshot.summary_status === 'error') {
      summaryTone = 'metric-value-danger';
      summaryTitle = firstError?.title || 'Error';
      summaryCopy = firstError?.detail || 'One or more critical issues detected.';
      summaryIcon = <ShieldOff size={18} />;
    }
  }

  const rows = useMemo(() => {
    const data = [
      {
        name: 'Agent Service',
        state: running ? 'running' : 'stopped',
        detail: status?.agent_id || '-',
        updated: status?.uptime || '-',
      },
      {
        name: 'Gateway Tunnel',
        state: gatewayConnected
          ? 'connected'
          : gatewayState === 'retrying'
            ? 'retrying'
            : gatewayState === 'starting'
              ? 'starting'
              : running
                ? 'offline'
                : 'idle',
        detail: config?.gateway_url || '-',
        updated: running ? 'live' : '-',
      },
      {
        name: 'Proxy Engine',
        state: proxyState || 'unknown',
        detail: config?.listen_addr ? `Listening on ${config.listen_addr}` : 'No listen address configured',
        updated: status?.uptime || '-',
      },
      {
        name: 'Enrollment',
        state: config?.enrollment_health === 'cert_expired' ? 'expired'
             : config?.enrollment_health === 'cert_missing' ? 'missing'
             : config?.enrollment_health === 'cert_invalid' ? 'invalid'
             : enrolled ? 'active' : 'pending',
        detail: config?.enrollment_health === 'cert_expired'
             ? `Certificate expired (${config.cert_expiry || 'unknown'})`
             : config?.enrollment_health === 'cert_missing'
             ? 'Certificate files missing from disk'
             : config?.enrollment_health === 'cert_invalid'
             ? 'Certificate files are invalid or corrupted'
             : enrolled ? config?.org_name || '-' : 'Enrollment required',
        updated: enrolled ? 'configured' : '-',
      },
    ];

    return data;
  }, [config, enrolled, gatewayConnected, gatewayState, proxyState, running, status]);

  function getStateTone(state) {
    if (['running', 'connected', 'active'].includes(state)) return 'success';
    if (['retrying', 'starting'].includes(state)) return 'info';
    if (['offline', 'pending', 'idle'].includes(state)) return 'warning';
    if (['stopped', 'unknown', 'expired', 'missing', 'invalid', 'degraded'].includes(state)) return 'danger';
    return 'neutral';
  }

  function severityIcon(severity) {
    if (severity === 'error') return <AlertCircle size={14} />;
    if (severity === 'warning') return <AlertTriangle size={14} />;
    return <Info size={14} />;
  }

  if (loading) {
    return (
      <div className="page-loading">
        <span>Connecting to local agent...</span>
      </div>
    );
  }

  return (
    <div className="workspace-page">
      <div className="page-title-row">
        <div>
          <h1 className="page-title">Runtime</h1>
          <p className="page-subtitle">View live protection, gateway reachability, and service health for this device.</p>
        </div>
        <div className="page-actions">
          <button className="btn" type="button" onClick={() => { refresh(); collect(); }}>
            <RefreshCw size={14} />
            Refresh
          </button>
        </div>
      </div>

      {!enrolled && (
        <div className="notice-banner">
          <div>
            <div className="notice-title">Enrollment still needs to be completed.</div>
            <div className="notice-copy">
              {running
                ? 'Finish setup to attach this device to your organization policy.'
                : 'Finish setup to attach this device to your organization policy. If the local agent is stopped, Themisto will try to start it during enrollment.'}
            </div>
          </div>
          <button className="btn btn-primary" type="button" onClick={() => navigate('/enrollment')}>
            Finish Enrollment
            <ArrowRight size={14} />
          </button>
        </div>
      )}

      {recovery?.should_offer && (
        <div className="notice-banner notice-banner-danger">
          <div>
            <div className="notice-title">Internet recovery available</div>
            <div className="notice-copy">{recovery.reason}</div>
            {resetResult && (
              <div className={`notice-copy ${resetResult.pending ? 'text-info' : resetResult.success ? 'text-success' : 'text-danger'}`}>
                {resetResult.message}
              </div>
            )}
          </div>
          <button
            className="btn btn-danger"
            type="button"
            disabled={resetting}
            onClick={resetProxy}
          >
            {resetting ? 'Restoring...' : 'Restore Internet'}
          </button>
        </div>
      )}

      <div className="metric-panels" data-tour="desktop-runtime-summary">
        <div className="metric-panel">
          <div className="metric-label">Protection status</div>
          <div className={`metric-value ${summaryTone}`}>
            {summaryIcon}
            <span>{summaryTitle}</span>
          </div>
          <div className="metric-meta">{summaryCopy}</div>
        </div>

        <div className="metric-panel">
          <div className="metric-label">Device runtime</div>
          <div className="metric-value metric-value-neutral">{status?.uptime || '-'}</div>
          <div className="metric-meta">
            {gatewayConnected
              ? 'Gateway connected'
              : gatewayState === 'retrying'
                ? 'Gateway retrying'
                : gatewayState === 'starting'
                  ? 'Gateway starting'
                  : 'Gateway disconnected'} / Proxy {proxyState || 'unknown'} / Buffer{' '}
            {status?.buffer_size ?? 0}
          </div>
        </div>
      </div>

      {startupIssues.length > 0 && (
        <div className="maintenance-section">
          <div className="diagnostics-section-title">Startup and retry state</div>
          <div className="issues-list">
            {startupIssues.map((issue, idx) => (
              <div key={idx} className={`issue-row issue-${issue.severity}`}>
                <div className="issue-header">
                  {severityIcon(issue.severity)}
                  <span className="issue-title">{issue.title}</span>
                </div>
                <div className="issue-detail">{issue.detail}</div>
                <div className="issue-remediation">{issue.remediation}</div>
              </div>
            ))}
          </div>
        </div>
      )}

      {protectionIssues.length > 0 && (
        <div className="issues-list">
          {protectionIssues.map((issue, idx) => (
            <div key={idx} className={`issue-row issue-${issue.severity}`}>
              <div className="issue-header">
                {severityIcon(issue.severity)}
                <span className="issue-title">{issue.title}</span>
              </div>
              <div className="issue-detail">{issue.detail}</div>
              <div className="issue-remediation">{issue.remediation}</div>
            </div>
          ))}
        </div>
      )}

      {maintenanceIssues.length > 0 && (
        <div className="maintenance-section">
          <div className="diagnostics-section-title">Maintenance notes</div>
          <div className="maintenance-list">
            {maintenanceIssues.map((issue, idx) => (
              <div key={idx} className="maintenance-item">
                <div className="maintenance-title">{issue.title}</div>
                <div className="maintenance-copy">{issue.detail}</div>
                <div className="maintenance-remediation">{issue.remediation}</div>
              </div>
            ))}
          </div>
        </div>
      )}

      <div className="table-wrap" data-tour="desktop-runtime-table">
        <table className="ops-table">
          <thead>
            <tr>
              <th>Subsystem</th>
              <th>Status</th>
              <th>Details</th>
              <th>Last updated</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((row) => (
              <tr key={row.name}>
                <td>{row.name}</td>
                <td>
                  <span className={`ops-pill ${getStateTone(row.state)}`}>{row.state}</span>
                </td>
                <td className="ops-detail">{row.detail}</td>
                <td>{row.updated}</td>
              </tr>
            ))}
            {rows.length === 0 && (
              <tr>
                <td colSpan={4} className="table-empty">
                  No subsystems matched your filter.
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
}
