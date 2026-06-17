import { useEffect, useState } from 'react';
import { Chrome, Clipboard, Globe, LockKeyhole, Play, RefreshCw, RotateCw, ShieldCheck, ShieldQuestion, ShieldX, Wrench } from 'lucide-react';
import { useDiagnostics, useBrowserProtection, useAdminPolicyGuide } from '../hooks/useAgentStatus';
import { useAdminSession } from '../hooks/useAdminSession';

const wailsRuntime = window.runtime;
const wails = window.go?.main?.App;

const browserIcons = {
  Chrome: <Chrome size={18} />,
  Edge: <Globe size={18} />,
  Brave: <Globe size={18} />,
  Firefox: <Globe size={18} />,
};

function statusPill(status) {
  switch (status) {
    case 'deployed':
      return <span className="ops-pill info">Deployed</span>;
    case 'policy_detected':
      return <span className="ops-pill warning">Policy detected</span>;
    case 'not_deployed':
      return <span className="ops-pill warning">Not deployed</span>;
    case 'check_failed':
      return <span className="ops-pill warning">Unknown</span>;
    case 'unsupported':
      return <span className="ops-pill neutral">Unsupported</span>;
    case 'not_installed':
      return <span className="ops-pill neutral">Unavailable</span>;
    default:
      return <span className="ops-pill neutral">{status}</span>;
  }
}

function summaryIcon(status) {
  switch (status) {
    case 'healthy':
      return <ShieldCheck size={18} />;
    case 'starting':
      return <RefreshCw size={18} />;
    case 'degraded':
      return <ShieldQuestion size={18} />;
    default:
      return <ShieldX size={18} />;
  }
}

function deploymentSignal(browser) {
  if (!browser.installed) return '-';
  if (browser.protection_status === 'check_failed') return 'Check failed';
  if (browser.protection_status === 'deployed') return 'Loaded in profile';
  if (browser.protection_status === 'policy_detected') return 'Policy detected';
  if (browser.protection_status === 'unsupported') return 'Unsupported';
  return browser.deployment_detected ? 'Signal detected' : 'Not detected';
}

function buildActionItems(browserList, issues, browserStatusDefs) {
  const items = [];
  const seen = new Set();

  for (const b of browserList) {
    if (b.protection_status === 'not_deployed') {
      const key = `browser-${b.name}-not_deployed`;
      if (seen.has(key)) continue;
      seen.add(key);
      const def = browserStatusDefs[b.protection_status];
      items.push({
        key,
        severity: 'warning',
        title: `${b.name} deployment missing`,
        reason: `Themisto did not detect local ${b.name} rollout policy on this device.`,
        step: def?.admin_action || `Deploy the Themisto extension for ${b.name} through GPO or MDM policy.`,
      });
    }
    if (b.protection_status === 'check_failed') {
      const key = `browser-${b.name}-check_failed`;
      if (seen.has(key)) continue;
      seen.add(key);
      const def = browserStatusDefs[b.protection_status];
      items.push({
        key,
        severity: 'warning',
        title: `${b.name} rollout check failed`,
        reason: b.last_error || `Could not read deployment state for ${b.name}.`,
        step: def?.admin_action || 'Verify local registry/policy access and rerun diagnostics.',
      });
    }
  }

  for (const issue of issues) {
    if (!['error', 'warning'].includes(issue.severity)) continue;
    const key = `diag-${issue.title}`;
    if (seen.has(key)) continue;
    seen.add(key);
    items.push({
      key,
      severity: issue.severity,
      title: issue.title,
      reason: issue.detail,
      step: issue.remediation,
    });
  }

  return items;
}

function buildAdminSummaryText(snapshot, browserList, guide) {
  const lines = [];
  lines.push('=== Themisto Admin Rollout Summary ===');
  lines.push(`Generated: ${new Date().toISOString()}`);
  lines.push(`Device health: ${snapshot.summary_status}`);
  lines.push('');

  lines.push('--- Browser Rollout ---');
  for (const b of browserList) {
    if (!b.installed) {
      lines.push(`${b.name}: not installed`);
    } else if (!b.supported) {
      lines.push(`${b.name}: unsupported`);
    } else {
      lines.push(`${b.name}: ${b.protection_status} (${b.confidence} confidence)`);
    }
  }
  lines.push('');

  const actionItems = buildActionItems(
    browserList,
    snapshot.issues || [],
    Object.fromEntries((guide.browser_status_definitions || []).map((d) => [d.status, d])),
  );
  lines.push('--- Action Required ---');
  if (actionItems.length === 0) {
    lines.push('No action items.');
  } else {
    for (const item of actionItems) {
      lines.push(`- [${item.severity.toUpperCase()}] ${item.title}`);
      lines.push(`  Next step: ${item.step}`);
    }
  }
  lines.push('');

  const issues = snapshot.issues || [];
  if (issues.length > 0) {
    lines.push('--- Diagnostics Issues ---');
    for (const issue of issues) {
      lines.push(`[${issue.severity.toUpperCase()}] ${issue.title}: ${issue.detail}`);
    }
    lines.push('');
  }

  lines.push('--- Limitations ---');
  for (const lim of guide.limitations || []) {
    lines.push(`- ${lim}`);
  }

  return lines.join('\n');
}

export default function Admin() {
  const { snapshot, loading: diagnosticsLoading, collect } = useDiagnostics();
  const { browsers, loading: browserLoading, error: browserError, refresh } = useBrowserProtection();
  const { guide, loading: guideLoading, error: guideError, refresh: refreshGuide } = useAdminPolicyGuide();
  const { lock, adminToken } = useAdminSession();
  const [copyFeedback, setCopyFeedback] = useState('');
  const [controlFeedback, setControlFeedback] = useState('');
  const [controlError, setControlError] = useState('');
  const [controlBusy, setControlBusy] = useState('');
  const [launchOnLogin, setLaunchOnLogin] = useState(false);
  const [launchOnLoginReady, setLaunchOnLoginReady] = useState(false);

  const browserList = browsers || [];
  const supportedInstalled = browserList.filter((b) => b.installed && b.supported);
  const deployedCount = supportedInstalled.filter((b) => b.protection_status === 'deployed').length;
  const pendingCount = supportedInstalled.filter((b) => b.protection_status === 'policy_detected').length;
  const missingCount = supportedInstalled.filter((b) => b.protection_status === 'not_deployed').length;
  const unknownCount = supportedInstalled.filter((b) => b.protection_status === 'check_failed').length;
  const unsupportedCount = browserList.filter((b) => b.installed && !b.supported).length;
  const installedCount = supportedInstalled.length;
  const issues = snapshot?.issues || [];
  const startupIssues = issues.filter((i) => i.protection_impact === 'starting');
  const impactingIssues = issues.filter((i) => ['error', 'degraded'].includes(i.protection_impact));
  const advisoryIssues = issues.filter((i) => i.protection_impact === 'none');
  const errorCount = impactingIssues.filter((i) => i.protection_impact === 'error').length;
  const degradedCount = impactingIssues.filter((i) => i.protection_impact === 'degraded').length;
  const browserStatusDefs = Object.fromEntries((guide?.browser_status_definitions || []).map((d) => [d.status, d]));

  const loading = diagnosticsLoading || browserLoading || guideLoading;
  const systemOS = snapshot?.system?.os || '';
  const isWindows = systemOS.startsWith('windows/');
  const isMac = systemOS.startsWith('darwin/');

  useEffect(() => {
    let active = true;

    async function loadLaunchOnLogin() {
      if (!wails?.GetLaunchOnLogin) {
        if (active) setLaunchOnLoginReady(true);
        return;
      }
      try {
        const enabled = await wails.GetLaunchOnLogin();
        if (active) {
          setLaunchOnLogin(enabled);
        }
      } finally {
        if (active) {
          setLaunchOnLoginReady(true);
        }
      }
    }

    loadLaunchOnLogin();
    return () => {
      active = false;
    };
  }, []);

  function refreshAll() {
    collect();
    refresh();
    refreshGuide();
  }

  async function runAgentControl(action) {
    if (!adminToken) {
      setControlError('Admin authentication is required for agent controls.');
      return;
    }

    setControlBusy(action);
    setControlError('');
    setControlFeedback('');

    try {
      if (wails) {
        if (action === 'start') {
          await wails.StartService(adminToken);
          setControlFeedback('Agent start requested.');
        } else if (action === 'restart') {
          await wails.RestartAgent(adminToken);
          setControlFeedback('Agent restart requested.');
        } else if (action === 'repair') {
          await wails.RepairAgentService(adminToken);
          setControlFeedback('Agent service repaired.');
        }
      } else {
        setControlFeedback(action === 'repair' ? 'Agent service repaired.' : 'Admin action completed.');
      }
      refreshAll();
    } catch (error) {
      setControlError(error?.message || 'Admin action failed.');
    } finally {
      setControlBusy('');
    }
  }

  async function handleLaunchOnLoginToggle() {
    if (!adminToken) {
      setControlError('Admin authentication is required to change desktop launch-on-login.');
      return;
    }
    if (!wails?.SetLaunchOnLogin) {
      setControlError('Desktop startup control is unavailable in this build.');
      return;
    }

    const next = !launchOnLogin;
    setControlBusy('desktop-startup');
    setControlError('');
    setControlFeedback('');

    try {
      await wails.SetLaunchOnLogin(next, adminToken);
      setLaunchOnLogin(next);
      setControlFeedback(next
        ? 'Desktop app will launch in the background when this user signs in.'
        : 'Desktop app launch-on-login disabled for this user.'
      );
    } catch (error) {
      setControlError(error?.message || 'Could not update desktop launch-on-login.');
    } finally {
      setControlBusy('');
    }
  }

  async function handleCopySummary() {
    if (!snapshot || !guide) return;
    setCopyFeedback('Copying...');
    try {
      const text = buildAdminSummaryText(snapshot, browserList, guide);
      if (wailsRuntime?.ClipboardSetText) {
        await wailsRuntime.ClipboardSetText(text);
      } else {
        await navigator.clipboard.writeText(text);
      }
      setCopyFeedback('Copied!');
      setTimeout(() => setCopyFeedback(''), 2000);
    } catch {
      setCopyFeedback('Failed');
      setTimeout(() => setCopyFeedback(''), 2000);
    }
  }

  if (loading) {
    return (
      <div className="page-loading">
        <span>Loading admin rollout view...</span>
      </div>
    );
  }

  if (!guide || !snapshot) {
    return (
      <div className="workspace-page">
        <div className="notice-banner notice-banner-danger">
          <div>
            <div className="notice-title">Could not load admin guidance.</div>
            <div className="notice-copy">{guideError || browserError || 'Diagnostics or policy guide data is unavailable. Try refreshing.'}</div>
          </div>
        </div>
        <div className="page-actions" style={{ marginTop: 16 }}>
          <button className="btn" type="button" onClick={refreshAll}>
            <RefreshCw size={14} />
            Retry
          </button>
        </div>
      </div>
    );
  }

  const actionItems = buildActionItems(browserList, issues, browserStatusDefs);

  return (
    <div className="workspace-page admin-workspace">
      {/* Header */}
      <div className="page-title-row">
        <div>
          <h1 className="page-title">Admin</h1>
          <p className="page-subtitle">Device rollout state, action items, and operational reference.</p>
        </div>
        <div className="admin-actions-bar">
          <button className="btn" type="button" onClick={refreshAll}>
            <RefreshCw size={14} />
            Refresh
          </button>
          <button className="btn" type="button" onClick={handleCopySummary} disabled={!!copyFeedback}>
            <Clipboard size={14} />
            {copyFeedback || 'Copy admin summary'}
          </button>
          <button className="btn" type="button" onClick={lock}>
            <LockKeyhole size={14} />
            Lock
          </button>
        </div>
      </div>

      {(browserError || guideError) && (
        <div className="notice-banner notice-banner-danger">
          <div>
            <div className="notice-title">Some admin data could not be loaded.</div>
            <div className="notice-copy">{[browserError, guideError].filter(Boolean).join(' ')}</div>
          </div>
        </div>
      )}

      {/* Metrics */}
      <div className="metric-panels">
        <div className="metric-panel">
          <div className="metric-label">Deployed</div>
          <div className="metric-value metric-value-success">{deployedCount}</div>
          <div className="metric-meta">{deployedCount}/{installedCount} supported installed browsers with extension evidence in local browser data.</div>
        </div>
        <div className="metric-panel">
          <div className="metric-label">Needing rollout</div>
          <div className={`metric-value ${missingCount > 0 ? 'metric-value-warning' : 'metric-value-neutral'}`}>{missingCount}</div>
          <div className="metric-meta">Installed browsers with no deployment detected.</div>
        </div>
        <div className="metric-panel">
          <div className="metric-label">Policy only</div>
          <div className={`metric-value ${pendingCount > 0 ? 'metric-value-warning' : 'metric-value-neutral'}`}>{pendingCount}</div>
          <div className="metric-meta">Installed browsers where policy exists but profile evidence is still missing.</div>
        </div>
        <div className="metric-panel">
          <div className="metric-label">Unknown</div>
          <div className={`metric-value ${unknownCount > 0 ? 'metric-value-warning' : 'metric-value-neutral'}`}>{unknownCount}</div>
          <div className="metric-meta">Browsers where the rollout check itself failed.</div>
        </div>
        {unsupportedCount > 0 && (
          <div className="metric-panel">
            <div className="metric-label">Unsupported</div>
            <div className="metric-value metric-value-neutral">{unsupportedCount}</div>
            <div className="metric-meta">Detected browsers that Themisto does not currently manage.</div>
          </div>
        )}
        <div className="metric-panel">
          <div className="metric-label">Device health</div>
          <div className={`metric-value ${snapshot.summary_status === 'healthy' ? 'metric-value-success' : snapshot.summary_status === 'starting' ? 'metric-value-neutral' : snapshot.summary_status === 'degraded' ? 'metric-value-warning' : 'metric-value-danger'}`}>
            {summaryIcon(snapshot.summary_status)}
            <span>{guide.health_status_definitions.find((d) => d.status === snapshot.summary_status)?.label || snapshot.summary_status}</span>
          </div>
          <div className="metric-meta">
            {errorCount} blocking issue{errorCount === 1 ? '' : 's'}, {degradedCount} degraded issue{degradedCount === 1 ? '' : 's'}, {startupIssues.length} startup item{startupIssues.length === 1 ? '' : 's'}, {advisoryIssues.length} maintenance advisor{advisoryIssues.length === 1 ? 'y' : 'ies'}
          </div>
        </div>
      </div>

      {startupIssues.length > 0 && (
        <div className="ref-card" style={{ marginBottom: 20 }}>
          <div className="ref-card-title">Startup and retry state</div>
          <div className="ref-card-list">
            {startupIssues.map((issue) => (
              <div key={issue.title}>
                <strong>{issue.title}</strong>
                <div style={{ color: 'var(--text-muted)', fontSize: 12, marginTop: 1 }}>{issue.detail}</div>
                <div style={{ color: 'var(--text-muted)', fontSize: 12, marginTop: 4 }}>{issue.remediation}</div>
              </div>
            ))}
          </div>
        </div>
      )}

      <div className="ref-card" style={{ marginBottom: 20 }}>
        <div className="ref-card-title">Agent controls</div>
        <div className="admin-control-copy">
          Use these admin actions to recover the agent on this device without re-running the installer.
        </div>
        <div className="admin-actions-bar">
          <button className="btn" type="button" onClick={() => runAgentControl('start')} disabled={!!controlBusy}>
            <Play size={14} />
            {controlBusy === 'start' ? 'Starting...' : 'Start agent'}
          </button>
          <button className="btn" type="button" onClick={() => runAgentControl('restart')} disabled={!!controlBusy}>
            <RotateCw size={14} />
            {controlBusy === 'restart' ? 'Restarting...' : 'Restart agent'}
          </button>
          <button className="btn" type="button" onClick={() => runAgentControl('repair')} disabled={!!controlBusy}>
            <Wrench size={14} />
            {controlBusy === 'repair' ? 'Repairing...' : 'Repair agent service'}
          </button>
        </div>
        {controlFeedback && <div className="control-feedback">{controlFeedback}</div>}
        {controlError && <div className="control-feedback control-feedback-error">{controlError}</div>}
      </div>

      {isWindows && (
        <div className="ref-card" style={{ marginBottom: 20 }}>
          <div className="ref-card-title">Desktop startup</div>
          <div className="admin-control-copy">
            Launch the Themisto desktop app in the background when this Windows user signs in. The protection agent still runs separately at boot.
          </div>
          <div className="card-row preference-row" style={{ marginTop: 12 }}>
            <div className="preference-info">
              <span className="card-row-label">Launch on login</span>
              <span className="preference-hint">This is enabled by default on first run. Only Admin can change it.</span>
            </div>
            <button
              className={`toggle-pill ${launchOnLogin ? 'toggle-pill-on' : 'toggle-pill-off'}`}
              onClick={handleLaunchOnLoginToggle}
              disabled={!launchOnLoginReady || !adminToken || controlBusy === 'desktop-startup'}
              type="button"
              aria-pressed={launchOnLogin}
            >
              {controlBusy === 'desktop-startup' ? 'Saving...' : launchOnLogin ? 'On' : 'Off'}
            </button>
          </div>
        </div>
      )}

      {isMac && (
        <div className="ref-card" style={{ marginBottom: 20 }}>
          <div className="ref-card-title">macOS app</div>
          <div className="admin-control-copy">
            Launch the Themisto macOS app in the background when this user signs in. The Themisto agent continues to run separately as a system LaunchDaemon.
          </div>
          <div className="card-row preference-row" style={{ marginTop: 12 }}>
            <div className="preference-info">
              <span className="card-row-label">Launch at login</span>
              <span className="preference-hint">This writes a per-user macOS LaunchAgent and starts the app hidden with <code>--background</code> on sign-in.</span>
            </div>
            <button
              className={`toggle-pill ${launchOnLogin ? 'toggle-pill-on' : 'toggle-pill-off'}`}
              onClick={handleLaunchOnLoginToggle}
              disabled={!launchOnLoginReady || !adminToken || controlBusy === 'desktop-startup'}
              type="button"
              aria-pressed={launchOnLogin}
            >
              {controlBusy === 'desktop-startup' ? 'Saving...' : launchOnLogin ? 'On' : 'Off'}
            </button>
          </div>
        </div>
      )}

      {/* Action Required */}
      {actionItems.length > 0 ? (
        <div className="action-required-list">
          <div className="section-title">Action required</div>
          {actionItems.map((item) => (
            <div key={item.key} className={`action-item${item.severity === 'error' ? ' action-item-error' : ''}`}>
              <div className="action-item-title">{item.title}</div>
              <div className="action-item-reason">{item.reason}</div>
              <div className="action-item-step">Next step: {item.step}</div>
            </div>
          ))}
        </div>
      ) : (
        <div className="all-clear-note">
          No action items detected on this device right now.
        </div>
      )}

      {/* Rollout Table */}
      <div className="table-wrap" style={{ marginBottom: 20 }}>
        <table className="ops-table">
          <thead>
            <tr>
              <th>Browser</th>
              <th>Installed</th>
              <th>Deployment signal</th>
              <th>Status</th>
              <th>Confidence</th>
              <th>Admin action</th>
            </tr>
          </thead>
          <tbody>
            {browserList.map((b) => {
              const def = browserStatusDefs[b.protection_status];
              return (
                <tr key={b.name}>
                  <td>
                    <div className="browser-cell">
                      <span className="browser-icon">{browserIcons[b.name] || <Globe size={18} />}</span>
                      <span>{b.name}</span>
                    </div>
                  </td>
                  <td>{b.installed ? 'Yes' : 'No'}</td>
                  <td className="ops-detail">{deploymentSignal(b)}</td>
                  <td>{statusPill(b.protection_status)}</td>
                  <td>{b.installed ? b.confidence : '-'}</td>
                  <td className="ops-detail">{def?.admin_action || '-'}</td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>

      {/* Reference Cards */}
      <div className="ref-cards">
        <div className="ref-card">
          <div className="ref-card-title">Status glossary</div>
          <ul className="ref-card-list">
            {guide.browser_status_definitions.map((d) => (
              <li key={d.status}>
                <div className="status-line">{statusPill(d.status)} <span>{d.label}</span></div>
                <div style={{ color: 'var(--text-muted)', fontSize: 12, marginTop: 2 }}>{d.meaning}</div>
              </li>
            ))}
          </ul>
          <div className="ref-card-note">Deployed means Themisto found extension evidence in local browser data. Policy detected means rollout policy exists but the browser has not yet exposed extension evidence locally.</div>
        </div>

        <div className="ref-card">
          <div className="ref-card-title">Audit visibility</div>
          <ul className="ref-card-list">
            {guide.audit_visibility.map((a) => (
              <li key={a.title}>
                <strong>{a.title}</strong>
                <div style={{ color: 'var(--text-muted)', fontSize: 12, marginTop: 1 }}>{a.detail}</div>
              </li>
            ))}
          </ul>
        </div>

        <div className="ref-card">
          <div className="ref-card-title">Admin playbook</div>
          <ul className="ref-card-list">
            {guide.troubleshooting.map((t) => (
              <li key={t.title}><strong>{t.title}:</strong> {t.detail}</li>
            ))}
          </ul>
        </div>

        <div className="ref-card">
          <div className="ref-card-title">What to tell employees</div>
          <ul className="ref-card-list">
            {guide.employee_messaging.map((m) => (
              <li key={m.title}><strong>{m.title}:</strong> {m.detail}</li>
            ))}
          </ul>
        </div>
      </div>

      {/* Limitations */}
      <div className="ref-card" style={{ marginBottom: 20 }}>
        <div className="ref-card-title">Limitations</div>
        <ul className="ref-card-list">
          {guide.limitations.map((lim) => (
            <li key={lim}>{lim}</li>
          ))}
        </ul>
      </div>
    </div>
  );
}
