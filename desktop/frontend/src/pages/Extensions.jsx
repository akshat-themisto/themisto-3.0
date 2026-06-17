import { Chrome, Globe, Puzzle, RefreshCw } from 'lucide-react';
import { useBrowserProtection } from '../hooks/useAgentStatus';

const browserIcons = {
  Chrome: <Chrome size={18} />,
  Edge: <Globe size={18} />,
  Brave: <Globe size={18} />,
  Firefox: <Globe size={18} />,
};

function statusPill(status) {
  switch (status) {
    case 'deployed':
      return <span className="ops-pill success">Protected</span>;
    case 'policy_detected':
      return <span className="ops-pill warning">Pending load</span>;
    case 'not_deployed':
      return <span className="ops-pill warning">Available</span>;
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

function deploymentLabel(browser) {
  if (!browser.installed) return '-';
  if (browser.protection_status === 'check_failed') return 'Check failed';
  if (browser.protection_status === 'deployed') return 'Loaded in profile';
  if (browser.protection_status === 'policy_detected') return 'Policy detected';
  if (browser.protection_status === 'unsupported') return 'Unsupported';
  return 'Not detected';
}

export default function Extensions() {
  const { browsers, loading, error, refresh } = useBrowserProtection();
  const list = browsers || [];
  const supportedInstalled = list.filter((b) => b.installed && b.supported);
  const protectedCount = supportedInstalled.filter((b) => b.protection_status === 'deployed').length;
  const pendingCount = supportedInstalled.filter((b) => b.protection_status === 'policy_detected').length;
  const availableCount = supportedInstalled.filter((b) => b.protection_status === 'not_deployed').length;
  const unknownCount = supportedInstalled.filter((b) => b.protection_status === 'check_failed').length;
  const unsupportedCount = list.filter((b) => b.installed && !b.supported).length;
  const allProtected = supportedInstalled.length > 0 && supportedInstalled.every((b) => b.protection_status === 'deployed');

  return (
    <div className="workspace-page">
      <div className="page-title-row">
        <div>
          <h1 className="page-title">Browser Guard</h1>
          <p className="page-subtitle">Browser protection coverage and local extension rollout for this endpoint.</p>
        </div>
        <div className="page-actions">
          <button className="btn" type="button" onClick={refresh}>
            <RefreshCw size={14} />
            Refresh
          </button>
        </div>
      </div>

      <div className="metric-panels metric-panels-compact">
        <div className="metric-panel">
          <div className="metric-label">Protected</div>
          <div className={`metric-value ${protectedCount > 0 ? 'metric-value-success' : 'metric-value-warning'}`}>{protectedCount}</div>
          <div className="metric-meta">Supported browsers where the Themisto extension is loaded.</div>
        </div>

        <div className="metric-panel">
          <div className="metric-label">Available</div>
          <div className={`metric-value ${availableCount > 0 ? 'metric-value-info' : 'metric-value-neutral'}`}>{availableCount}</div>
          <div className="metric-meta">
            {availableCount === 0 ? 'No browser coverage upgrades detected.' : 'Installed browsers that can receive Themisto protection.'}
          </div>
        </div>

        <div className="metric-panel">
          <div className="metric-label">Pending</div>
          <div className={`metric-value ${pendingCount > 0 ? 'metric-value-warning' : 'metric-value-neutral'}`}>{pendingCount}</div>
          <div className="metric-meta">Policy exists, but the extension has not been confirmed in profile data.</div>
        </div>

        <div className="metric-panel">
          <div className="metric-label">Unknown</div>
          <div className={`metric-value ${unknownCount > 0 ? 'metric-value-warning' : 'metric-value-neutral'}`}>{unknownCount}</div>
          <div className="metric-meta">Status checks that should be refreshed or shared with IT.</div>
        </div>
      </div>

      {!loading && allProtected && (
        <div className="notice-banner notice-banner-success">
          <div>
            <div className="notice-title">All supported detected browsers are protected.</div>
            <div className="notice-copy">Themisto found extension evidence in each supported browser on this endpoint.</div>
          </div>
        </div>
      )}

      {!loading && availableCount > 0 && (
        <div className="notice-banner notice-banner-info">
          <div>
            <div className="notice-title">Additional browser coverage is available.</div>
            <div className="notice-copy">
              {availableCount} supported browser{availableCount === 1 ? '' : 's'} could be added to Themisto protection if users rely on them for AI work.
            </div>
          </div>
        </div>
      )}

      {!loading && pendingCount > 0 && (
        <div className="notice-banner">
          <div>
            <div className="notice-title">Some browsers still need to load the extension.</div>
            <div className="notice-copy">
              Reopen those browsers, then refresh this page to confirm the local rollout.
            </div>
          </div>
        </div>
      )}

      {!loading && unsupportedCount > 0 && (
        <div className="notice-banner">
          <div>
            <div className="notice-title">Unsupported browser detected.</div>
            <div className="notice-copy">Themisto can see the browser, but does not manage extension rollout for it yet.</div>
          </div>
        </div>
      )}

      {!loading && error && (
        <div className="notice-banner notice-banner-danger">
          <div>
            <div className="notice-title">Could not check browser protection status.</div>
            <div className="notice-copy">{error}</div>
          </div>
        </div>
      )}

      {loading ? (
        <div className="page-loading">
          <span>Checking browser coverage...</span>
        </div>
      ) : error ? null : list.length === 0 ? (
        <div className="empty-state">
          <Puzzle size={28} />
          <h2>No browsers were detected</h2>
          <p>
            Themisto did not find any supported browser installations on this endpoint. Refresh after installing or reopening a browser.
          </p>
        </div>
      ) : (
        <div className="table-wrap">
          <table className="ops-table">
            <thead>
              <tr>
                <th>Browser</th>
                <th>Installed</th>
                <th>Deployment</th>
                <th>Status</th>
                <th>Guidance</th>
              </tr>
            </thead>
            <tbody>
              {list.map((browser) => (
                <tr key={browser.name}>
                  <td>
                    <div className="browser-cell">
                      <span className="browser-icon">{browserIcons[browser.name] || <Globe size={18} />}</span>
                      <span>{browser.name}</span>
                    </div>
                  </td>
                  <td>{browser.installed ? 'Detected' : 'Not found'}</td>
                  <td>{deploymentLabel(browser)}</td>
                  <td>{statusPill(browser.protection_status)}</td>
                  <td className="ops-detail">
                    <div>{browser.detail || '-'}</div>
                    {browser.remediation && <div className="issue-remediation issue-remediation-inline">{browser.remediation}</div>}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
