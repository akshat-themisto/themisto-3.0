import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { AlertCircle, AlertTriangle, Clipboard, Download, Info } from 'lucide-react';
import { useAgentConfig, useSystemInfo, useDiagnostics, useProxyRecovery, buildDiagnosticsReport, exportDiagnosticsBundle } from '../hooks/useAgentStatus';
import { triggerDesktopOnboarding } from '../lib/onboarding';

const wailsRuntime = window.runtime;

export default function Settings() {
  const navigate = useNavigate();
  const { config, loading } = useAgentConfig();
  const systemInfo = useSystemInfo();
  const { snapshot } = useDiagnostics();
  const { recovery, resetting, resetResult, resetProxy } = useProxyRecovery();

  const [copyFeedback, setCopyFeedback] = useState('');
  const [exporting, setExporting] = useState(false);
  const [exportPath, setExportPath] = useState('');
  const [exportError, setExportError] = useState('');
  async function handleCopy() {
    setCopyFeedback('Collecting...');
    try {
      const text = await buildDiagnosticsReport();
      if (wailsRuntime?.ClipboardSetText) {
        await wailsRuntime.ClipboardSetText(text);
      } else {
        await navigator.clipboard.writeText(text);
      }
      setCopyFeedback('Copied!');
      setTimeout(() => setCopyFeedback(''), 2000);
    } catch {
      setCopyFeedback('Failed to copy');
      setTimeout(() => setCopyFeedback(''), 2000);
    }
  }

  async function handleExport() {
    setExporting(true);
    setExportPath('');
    setExportError('');
    try {
      const result = await exportDiagnosticsBundle();
      if (result.success) {
        setExportPath(result.path);
      } else {
        setExportError(result.error || 'Export failed');
      }
    } catch {
      setExportError('Could not export diagnostics bundle.');
    } finally {
      setExporting(false);
    }
  }

  function severityIcon(severity) {
    if (severity === 'error') return <AlertCircle size={14} />;
    if (severity === 'warning') return <AlertTriangle size={14} />;
    return <Info size={14} />;
  }

  if (loading) {
    return (
      <div className="page-loading">
        <span>Loading device details...</span>
      </div>
    );
  }

  const issues = snapshot?.issues || [];
  const startupIssues = issues.filter((issue) => issue.protection_impact === 'starting');
  const protectionIssues = issues.filter((issue) => ['error', 'degraded'].includes(issue.protection_impact));
  const maintenanceIssues = issues.filter((issue) => issue.protection_impact === 'none');
  const configIssues = snapshot?.config?.config_issues || [];
  const collectionErrors = snapshot?.collection_errors || [];
  const pathChecks = snapshot?.path_checks || [];
  const browserProtection = snapshot?.browser_protection || [];
  const deployedBrowsers = browserProtection.filter((b) => b.protection_status === 'deployed').length;
  const installedBrowsers = browserProtection.filter((b) => b.installed).length;
  const unknownBrowsers = browserProtection.filter((b) => b.protection_status === 'check_failed').length;
  const serviceTone = !snapshot
    ? 'neutral'
    : snapshot.service_state === 'running'
      ? 'success'
      : snapshot.status?.running
        ? 'warning'
        : 'danger';
  const isWindows = (systemInfo?.os || '').startsWith('windows/');

  return (
    <div className="workspace-page">
      <div className="page-title-row">
        <div>
          <h1 className="page-title">Settings</h1>
          <p className="page-subtitle">Device details, diagnostics, recovery tools, and the welcome guide all live here.</p>
        </div>
        <div className="page-actions">
          <button className="btn" type="button" onClick={triggerDesktopOnboarding}>
            Replay Welcome Guide
          </button>
          {!config?.enrolled && (
            <button className="btn btn-primary" type="button" onClick={() => navigate('/enrollment')}>
              Finish Enrollment
            </button>
          )}
        </div>
      </div>

      <div className="metric-panels">
        <div className="metric-panel">
          <div className="metric-label">Enrollment</div>
          <div className={`metric-value ${config?.enrolled ? 'metric-value-success' : 'metric-value-warning'}`}>
            {config?.enrolled ? 'Active' : 'Pending'}
          </div>
          <div className="metric-meta">
            {config?.enrolled
              ? `Connected to ${config?.org_name || 'your organization'}.`
              : 'This device still needs to complete enrollment.'}
          </div>
        </div>

        <div className="metric-panel">
          <div className="metric-label">Proxy listener</div>
          <div className="metric-value metric-value-neutral">{config?.listen_addr || '-'}</div>
          <div className="metric-meta">Traffic inspection uses this local address when the agent is active.</div>
        </div>
      </div>

      {isWindows && (
        <div className="card">
          <div className="card-title">Emergency internet recovery</div>
          <div className="card-row">
            <span className="card-row-label">Status</span>
            <span className="card-row-value">
              <span className={`ops-pill ${recovery?.should_offer ? 'warning' : 'neutral'}`}>
                {recovery?.should_offer ? 'Action available' : 'Ready if needed'}
              </span>
            </span>
          </div>
          <div className="maintenance-copy" style={{ marginTop: 8 }}>
            {recovery?.should_offer
              ? recovery.reason
              : 'If Themisto ever leaves your browser or system proxy stuck, use this action to clear Themisto-owned proxy settings and restore connectivity.'}
          </div>
          <div className="maintenance-remediation" style={{ marginTop: 8 }}>
            Manual fallback: <code>"C:\Program Files\Themisto\themisto-agent.exe" -proxy-off</code>
          </div>
          {resetResult && (
            <div className={`maintenance-copy ${resetResult.pending ? 'text-info' : resetResult.success ? 'text-success' : 'text-danger'}`} style={{ marginTop: 8 }}>
              {resetResult.message}
            </div>
          )}
          <div className="diagnostics-buttons" style={{ marginTop: 12 }}>
            <button
              className={recovery?.should_offer ? 'btn btn-danger' : 'btn'}
              type="button"
              disabled={resetting || !recovery?.should_offer}
              onClick={resetProxy}
            >
              {resetting ? 'Restoring...' : 'Restore Internet'}
            </button>
          </div>
        </div>
      )}

      <div className="info-grid">
        <div className="card">
          <div className="card-title">Agent configuration</div>
          <div className="card-row">
            <span className="card-row-label">Agent ID</span>
            <span className="card-row-value">{config?.agent_id || '-'}</span>
          </div>
          <div className="card-row">
            <span className="card-row-label">Gateway URL</span>
            <span className="card-row-value">{config?.gateway_url || '-'}</span>
          </div>
          <div className="card-row">
            <span className="card-row-label">Organization</span>
            <span className="card-row-value">{config?.org_name || '-'}</span>
          </div>
          <div className="card-row">
            <span className="card-row-label">Enrollment state</span>
            <span className="card-row-value">{config?.enrolled ? 'Enrolled' : 'Not enrolled'}</span>
          </div>
        </div>

        <div className="card">
          <div className="card-title">System information</div>
          <div className="card-row">
            <span className="card-row-label">Hostname</span>
            <span className="card-row-value">{systemInfo?.hostname || '-'}</span>
          </div>
          <div className="card-row">
            <span className="card-row-label">Operating system</span>
            <span className="card-row-value">{systemInfo?.os || '-'}</span>
          </div>
          <div className="card-row">
            <span className="card-row-label">App version</span>
            <span className="card-row-value">{systemInfo?.version || '-'}</span>
          </div>
          <div className="card-row">
            <span className="card-row-label">Runtime</span>
            <span className="card-row-value">{systemInfo?.go_version || '-'}</span>
          </div>
        </div>
      </div>
      {/* Diagnostics highlights */}
      <div className="card diagnostics-card">
        <div className="card-title">Diagnostics</div>

        {snapshot && (
          <div className="diagnostics-highlights">
            <div className="card-row">
              <span className="card-row-label">Overall status</span>
              <span className="card-row-value">
                <span className={`ops-pill ${snapshot.summary_status === 'healthy' ? 'success' : snapshot.summary_status === 'starting' ? 'info' : snapshot.summary_status === 'degraded' ? 'warning' : 'danger'}`}>
                  {snapshot.summary_status}
                </span>
              </span>
            </div>

            {snapshot.summary_status === 'starting' && (
              <div className="card-row">
                <span className="card-row-label">Startup state</span>
                <span className="card-row-value">
                  <span className="ops-pill info">
                    {snapshot.status?.runtime_phase || 'starting'}
                  </span>
                </span>
              </div>
            )}

            {snapshot.advisory_count > 0 && (
              <div className="card-row">
                <span className="card-row-label">Follow-up items</span>
                <span className="card-row-value">
                  {snapshot.advisory_count} maintenance advisory{snapshot.advisory_count === 1 ? '' : 'ies'}
                </span>
              </div>
            )}

            <div className="card-row">
              <span className="card-row-label">Agent service</span>
              <span className="card-row-value">
                <span className={`ops-pill ${serviceTone}`}>
                  {snapshot.service_state || 'unknown'}
                </span>
              </span>
            </div>

            <div className="card-row">
              <span className="card-row-label">Extension coverage</span>
              <span className="card-row-value">
                {deployedBrowsers}/{installedBrowsers} browsers with deployment
                {unknownBrowsers > 0 && (
                  <span className="path-missing-hint"> - {unknownBrowsers} unknown</span>
                )}
              </span>
            </div>

            {pathChecks.length > 0 && (
              <div className="card-row">
                <span className="card-row-label">File paths</span>
                <span className="card-row-value">
                  {pathChecks.filter((p) => p.exists).length}/{pathChecks.length} present
                  {pathChecks.some((p) => !p.exists) && (
                    <span className="path-missing-hint">
                      {' '} - missing: {pathChecks.filter((p) => !p.exists).map((p) => p.label).join(', ')}
                    </span>
                  )}
                </span>
              </div>
            )}
          </div>
        )}

        {startupIssues.length > 0 && (
          <div className="diagnostics-section">
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
          <div className="diagnostics-section">
            <div className="diagnostics-section-title">Protection issues</div>
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
          </div>
        )}

        {maintenanceIssues.length > 0 && (
          <div className="diagnostics-section">
            <div className="diagnostics-section-title">Maintenance advisories</div>
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

        {configIssues.length > 0 && (
          <div className="diagnostics-section">
            <div className="diagnostics-section-title">Config issues</div>
            <ul className="diagnostics-list">
              {configIssues.map((ci, idx) => (
                <li key={idx}>{ci}</li>
              ))}
            </ul>
          </div>
        )}

        {collectionErrors.length > 0 && (
          <div className="diagnostics-section">
            <div className="diagnostics-section-title">Collection errors</div>
            <ul className="diagnostics-list diagnostics-list-errors">
              {collectionErrors.map((ce, idx) => (
                <li key={idx}>{ce}</li>
              ))}
            </ul>
          </div>
        )}

        <div className="diagnostics-actions">
          <p className="support-copy">Collect diagnostic information to share with IT support.</p>
          <div className="diagnostics-buttons">
            <button className="btn" type="button" onClick={handleCopy} disabled={!!copyFeedback}>
              <Clipboard size={14} />
              {copyFeedback || 'Copy to clipboard'}
            </button>
            <button className="btn" type="button" onClick={handleExport} disabled={exporting}>
              <Download size={14} />
              {exporting ? 'Exporting...' : 'Export diagnostics bundle'}
            </button>
          </div>
        </div>

        {exportPath && (
          <div className="notice-banner" style={{ marginTop: 12 }}>
            <div>
              <div className="notice-title">Diagnostics exported successfully.</div>
              <div className="notice-copy">Saved to: {exportPath}</div>
            </div>
          </div>
        )}

        {exportError && (
          <div className="notice-banner notice-banner-danger" style={{ marginTop: 12 }}>
            <div>
              <div className="notice-title">Export failed.</div>
              <div className="notice-copy">{exportError}</div>
            </div>
          </div>
        )}
      </div>

      <div className="card">
        <div className="card-title">Workspace tour</div>
        <div className="maintenance-copy">
          Replay the first-run welcome guide whenever you want a quick walkthrough of the desktop shell, runtime view, and admin controls.
        </div>
        <div className="diagnostics-buttons" style={{ marginTop: 12 }}>
          <button className="btn" type="button" onClick={triggerDesktopOnboarding}>
            Replay Welcome Guide
          </button>
        </div>
      </div>
    </div>
  );
}


