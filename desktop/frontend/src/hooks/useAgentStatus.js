import { useState, useEffect, useCallback } from 'react';

// Wails runtime bindings — undefined when running standalone via Vite
const wails = window.go?.main?.App;

const DEV_PLATFORM = (() => {
  const raw = `${window.navigator?.userAgentData?.platform || window.navigator?.platform || ''}`.toLowerCase();
  if (raw.includes('mac')) return 'darwin';
  if (raw.includes('win')) return 'windows';
  return 'linux';
})();

const DEV_HOME = DEV_PLATFORM === 'windows' ? 'C:\\Users\\Demo' : '/Users/demo';
const DEV_LOCAL_DATA = DEV_PLATFORM === 'windows'
  ? `${DEV_HOME}\\AppData\\Local`
  : `${DEV_HOME}/Library/Application Support`;
const DEV_PROGRAM_FILES = DEV_PLATFORM === 'windows'
  ? 'C:\\Program Files'
  : '/Applications';
const DEV_OS_LABEL = DEV_PLATFORM === 'windows' ? 'windows/amd64' : 'darwin/arm64';
const DEV_HOSTNAME = DEV_PLATFORM === 'windows' ? 'DESKTOP-DEMO' : 'demo-macbook';

// Dev mock data used when running outside Wails (npm run dev)
const MOCK_STATUS = {
  running: true,
  agent_id: 'demo-device-001',
  gateway_connected: true,
  gateway_state: 'connected',
  uptime: '2h15m30s',
  uptime_seconds: 8130,
  proxy_state: 'running',
  proxy_listener_ready: true,
  runtime_phase: 'ready',
  version: '0.1.0',
  buffer_size: 42,
};

const MOCK_CONFIG = {
  listen_addr: '127.0.0.1:9090',
  gateway_url: 'https://gateway.example.com',
  enrolled: true,
  org_name: 'Demo Organization',
  agent_id: 'demo-device-001',
};

const MOCK_SYSTEM_INFO = {
  os: DEV_OS_LABEL,
  hostname: DEV_HOSTNAME,
  version: '0.1.0',
  go_version: 'go1.23.0',
};

const MOCK_ADMIN_POLICY_GUIDE = {
  supported_browsers: ['Chrome', 'Edge', 'Brave', 'Firefox'],
  deployment_methods: [
    {
      title: 'Chromium browsers',
      detail: 'Chrome, Edge, and Brave use ExtensionInstallForcelist policy entries pointing at the Themisto Chromium update source.',
    },
    {
      title: 'Firefox',
      detail: 'Firefox uses enterprise extension registration for themisto-prompt-capture@themisto.local. The desktop app only checks the local registration signal and does not verify signed XPI delivery.',
    },
  ],
  local_checks: [
    {
      browser: 'Chrome',
      check: 'Extension deployment',
      location: 'HKLM\\SOFTWARE\\Policies\\Google\\Chrome\\ExtensionInstallForcelist',
      detail: 'Themisto checks the Chrome force-install policy list for the Themisto extension ID.',
    },
    {
      browser: 'Firefox',
      check: 'Extension deployment',
      location: 'HKLM\\SOFTWARE\\Mozilla\\Firefox\\Extensions [themisto-prompt-capture@themisto.local]',
      detail: 'Themisto checks the Firefox enterprise extensions key for the Themisto extension registration.',
    },
  ],
  browser_status_definitions: [
    {
      status: 'deployed',
      label: 'Deployment detected',
      meaning: 'Themisto found the local policy or registry signal for this browser. This does not prove runtime prompt interception.',
      admin_action: 'If users still report issues, ask them to reopen the browser and confirm device diagnostics before escalating.',
    },
    {
      status: 'not_deployed',
      label: 'Not deployed',
      meaning: 'The browser is installed, but Themisto did not find the expected local deployment signal.',
      admin_action: 'Deploy the Themisto extension through GPO, MDM, or the expected browser policy for this browser.',
    },
    {
      status: 'check_failed',
      label: 'Check failed',
      meaning: 'Themisto could not reliably read the local rollout signal and cannot make a trustworthy claim yet.',
      admin_action: 'Verify local registry or policy access, then rerun diagnostics.',
    },
    {
      status: 'not_installed',
      label: 'Browser not installed',
      meaning: 'Themisto did not find this browser on the device.',
      admin_action: 'No action is needed unless this browser is expected in your environment.',
    },
  ],
  health_status_definitions: [
    {
      status: 'healthy',
      label: 'Healthy',
      meaning: 'Diagnostics found no current local issues.',
      admin_action: 'Treat healthy diagnostics as device health only, not proof of browser runtime interception.',
    },
    {
      status: 'degraded',
      label: 'Degraded',
      meaning: 'Diagnostics found warning-level issues that still need attention.',
      admin_action: 'Resolve the warning before broadening rollout claims.',
    },
    {
      status: 'error',
      label: 'Error',
      meaning: 'Diagnostics found a critical local issue such as agent, service, or enrollment failure.',
      admin_action: 'Repair core device health first.',
    },
  ],
  audit_visibility: [
    {
      title: 'Local rollout visibility',
      detail: 'This desktop app can observe agent health, enrollment state, agent service status, file presence, and browser rollout signals derived from local policy and registry checks.',
    },
    {
      title: 'Diagnostics export',
      detail: 'Diagnostics can be copied or exported from the desktop app to share device-local evidence with IT support.',
    },
    {
      title: 'Server-side audit history',
      detail: 'This Step 3 surface explains audit visibility, but it does not fetch live backend audit entries.',
    },
  ],
  limitations: [
    'Runtime browser extension activity is not fully verified in this phase.',
    'Policy and registry detection show rollout signals, not guaranteed prompt interception.',
    'This desktop admin page does not replace backend audit history.',
  ],
  troubleshooting: [
    {
      title: 'When deployment is missing',
      detail: 'Treat not_deployed as a rollout gap on that device and confirm the correct browser policy is targeted.',
    },
    {
      title: 'When a check fails',
      detail: 'Treat check_failed as uncertainty, not absence, until the local check succeeds.',
    },
  ],
  employee_messaging: [
    {
      title: 'What to tell employees when rollout is missing',
      detail: 'Tell them IT is updating browser policy and ask them to reopen the browser after the change.',
    },
    {
      title: 'What to tell employees when state is unknown',
      detail: 'Ask them to export diagnostics and explain that Themisto could not confirm the local rollout state yet.',
    },
  ],
};

const MOCK_BROWSER_PROTECTION = [
  {
    name: 'Chrome', supported: true, installed: true, deployment_detected: true,
    protection_status: 'deployed', confidence: 'medium',
    detail: 'Chrome is installed and Themisto policy deployment was detected. Protection cannot be fully verified until the browser loads the extension.',
    remediation: 'No action needed. If issues persist, reload the browser or contact IT.',
  },
  {
    name: 'Edge', supported: true, installed: true, deployment_detected: false,
    protection_status: 'not_deployed', confidence: 'high',
    detail: 'Edge is installed, but the Themisto extension has not been deployed.',
    remediation: 'Contact IT to deploy the Themisto browser extension for Edge.',
  },
  {
    name: 'Brave', supported: true, installed: false, deployment_detected: false,
    protection_status: 'not_installed', confidence: 'high',
    detail: 'This browser is not installed on this device.',
    remediation: 'No action is needed unless you plan to use it.',
  },
  {
    name: 'Firefox', supported: true, installed: true, deployment_detected: false,
    protection_status: 'not_deployed', confidence: 'high',
    detail: 'Firefox is installed, but the Themisto extension has not been deployed.',
    remediation: 'Contact IT to deploy the Themisto browser extension for Firefox.',
  },
];

const MOCK_DIAGNOSTICS = {
  timestamp: new Date().toISOString(),
  app_version: '0.1.0',
  summary_status: 'healthy',
  status: {
    running: true,
    agent_id: 'demo-device-001',
    gateway_connected: true,
    gateway_state: 'connected',
    uptime: '2h15m30s',
    uptime_seconds: 8130,
    proxy_state: 'running',
    proxy_listener_ready: true,
    runtime_phase: 'ready',
    version: '0.1.0',
    buffer_size: 42,
  },
  config: {
    listen_addr: '127.0.0.1:9090',
    gateway_url: 'https://gateway.example.com',
    enrolled: true,
    org_name: 'Demo Organization',
    agent_id: 'demo-device-001',
    enrollment_health: 'healthy',
    cert_expiry: '2027-06-15T00:00:00Z',
    config_issues: [],
  },
  installed: {
    binary_exists: true,
    config_exists: true,
    config_valid: true,
    has_agent_id: true,
    has_gateway_url: true,
    certs_exist: true,
    service_installed: true,
    summary: 'installed_ready',
  },
  system: {
    os: DEV_OS_LABEL,
    hostname: DEV_HOSTNAME,
    version: '0.1.0',
    go_version: 'go1.23.0',
  },
  browsers: [
    { name: 'Chrome', installed: true, extension_installed: true },
    { name: 'Edge', installed: true, extension_installed: false },
    { name: 'Brave', installed: false, extension_installed: false },
    { name: 'Firefox', installed: true, extension_installed: false },
  ],
  browser_protection: MOCK_BROWSER_PROTECTION,
  path_checks: DEV_PLATFORM === 'windows'
    ? [
      { path: 'C:\\Program Files\\Themisto\\themisto-agent.exe', label: 'Agent binary', exists: true },
      { path: 'C:\\Program Files\\Themisto\\themisto-desktop.exe', label: 'Desktop app binary', exists: true },
      { path: 'C:\\ProgramData\\Themisto\\agent.json', label: 'Agent config', exists: true },
      { path: 'C:\\ProgramData\\Themisto\\themisto-status-api.token', label: 'Status API token', exists: true },
      { path: 'C:\\ProgramData\\Themisto\\certs\\device.crt', label: 'Device certificate', exists: true },
      { path: 'C:\\ProgramData\\Themisto\\certs\\device.key', label: 'Device private key', exists: true },
      { path: 'C:\\ProgramData\\Themisto\\certs\\ca-chain.pem', label: 'CA certificate', exists: true },
    ]
    : [
      { path: '/usr/local/bin/themisto-agent', label: 'Agent binary', exists: true },
      { path: `${DEV_PROGRAM_FILES}/Themisto.app/Contents/MacOS/themisto-desktop`, label: 'Desktop app binary', exists: true },
      { path: '/etc/themisto/agent.json', label: 'Agent config', exists: true },
      { path: '/tmp/themisto-status-api.token', label: 'Status API token', exists: true },
      { path: '/etc/themisto/identity/device.crt', label: 'Device certificate', exists: true },
      { path: '/etc/themisto/identity/device.key', label: 'Device private key', exists: true },
      { path: '/etc/themisto/identity/ca-chain.pem', label: 'CA certificate', exists: true },
    ],
  service_state: 'running',
  service_detail: DEV_PLATFORM === 'windows'
    ? 'ThemistoAgent Windows Service is running.'
    : 'Themisto launchd agent is running.',
  advisory_count: 0,
  issues: [],
  collection_errors: [],
};

const MOCK_EXPORT_RESULT = {
  success: true,
  path: DEV_PLATFORM === 'windows'
    ? `${DEV_LOCAL_DATA}\\Temp\\Themisto\\diagnostics\\themisto-diagnostics-20260320-143000`
    : `/tmp/Themisto/diagnostics/themisto-diagnostics-20260320-143000`,
};

const MOCK_REPORT_TEXT = `=== Themisto Diagnostics Report ===\nGenerated: (mock)\nOverall Status: healthy\n\n--- System ---\nHostname: ${DEV_HOSTNAME}\nOS: ${DEV_OS_LABEL}\n`;

const isDev = !wails;

export function useAgentStatus(interval = 2000) {
  const [status, setStatus] = useState(null);
  const [loading, setLoading] = useState(true);

  const refresh = useCallback(async () => {
    try {
      if (wails?.GetAgentStatus) {
        const s = await wails.GetAgentStatus();
        setStatus(s);
      } else if (isDev) {
        setStatus(MOCK_STATUS);
      }
    } catch {
      setStatus({ running: false, error: 'Could not reach agent' });
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    refresh();
    const id = setInterval(refresh, interval);
    return () => clearInterval(id);
  }, [refresh, interval]);

  return { status, loading, refresh };
}

export function useAgentConfig() {
  const [config, setConfig] = useState(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    async function load() {
      try {
        if (wails?.GetConfig) {
          const c = await wails.GetConfig();
          setConfig(c);
        } else if (isDev) {
          setConfig(MOCK_CONFIG);
        }
      } catch {
        setConfig({ error: 'Could not reach agent' });
      } finally {
        setLoading(false);
      }
    }
    load();
  }, []);

  return { config, loading };
}

export function useInstalledState() {
  const [state, setState] = useState(null);

  useEffect(() => {
    async function load() {
      try {
        if (wails?.CheckInstalledState) {
          const s = await wails.CheckInstalledState();
          setState(s);
        } else if (isDev) {
          setState({
            binary_exists: true,
            config_exists: true,
            config_valid: true,
            has_agent_id: true,
            has_gateway_url: true,
            certs_exist: true,
            service_installed: true,
            summary: 'installed_ready',
          });
        }
      } catch {
        // ignore
      }
    }
    load();
  }, []);

  return state;
}

export function useSystemInfo() {
  const [info, setInfo] = useState(null);

  useEffect(() => {
    async function load() {
      try {
        if (wails?.GetSystemInfo) {
          const i = await wails.GetSystemInfo();
          setInfo(i);
        } else if (isDev) {
          setInfo(MOCK_SYSTEM_INFO);
        }
      } catch {
        // ignore
      }
    }
    load();
  }, []);

  return info;
}

export function useDiagnostics(interval = null) {
  const [snapshot, setSnapshot] = useState(null);
  const [loading, setLoading] = useState(false);

  const collect = useCallback(async () => {
    setLoading(true);
    try {
      if (wails?.GetDiagnosticsSnapshot) {
        setSnapshot(await wails.GetDiagnosticsSnapshot());
      } else if (isDev) {
        setSnapshot(MOCK_DIAGNOSTICS);
      }
    } catch {
      setSnapshot({ summary_status: 'error', issues: [], collection_errors: ['Failed to collect diagnostics'] });
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    collect();
    if (!interval) {
      return undefined;
    }

    const id = setInterval(collect, interval);
    return () => clearInterval(id);
  }, [collect, interval]);

  return { snapshot, loading, collect };
}

export function useBrowserProtection() {
  const [browsers, setBrowsers] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  const refresh = useCallback(async () => {
    setError('');
    try {
      if (wails?.GetBrowserProtectionStatus) {
        setBrowsers(await wails.GetBrowserProtectionStatus());
      } else if (isDev) {
        setBrowsers(MOCK_BROWSER_PROTECTION);
      }
    } catch (error) {
      setBrowsers([]);
      setError(error?.message || 'Could not load browser protection status.');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    refresh();
  }, [refresh]);

  return { browsers, loading, error, refresh };
}

export function useAdminPolicyGuide() {
  const [guide, setGuide] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  const refresh = useCallback(async () => {
    setError('');
    try {
      if (wails?.GetAdminPolicyGuide) {
        setGuide(await wails.GetAdminPolicyGuide());
      } else if (isDev) {
        setGuide(MOCK_ADMIN_POLICY_GUIDE);
      }
    } catch (error) {
      setGuide(null);
      setError(error?.message || 'Could not load admin policy guidance.');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    refresh();
  }, [refresh]);

  return { guide, loading, error, refresh };
}

export function useProxyRecovery() {
  const [recovery, setRecovery] = useState(null);
  const [loading, setLoading] = useState(true);
  const [resetting, setResetting] = useState(false);
  const [resetResult, setResetResult] = useState(null);

  const refresh = useCallback(async () => {
    try {
      if (wails?.GetProxyRecoveryStatus) {
        setRecovery(await wails.GetProxyRecoveryStatus());
      } else if (isDev) {
        setRecovery({ should_offer: false, reason: '', proxy_host: '', proxy_port: 0 });
      }
    } catch {
      setRecovery(null);
    } finally {
      setLoading(false);
    }
  }, []);

  const resetProxy = useCallback(async () => {
    setResetting(true);
    setResetResult(null);
    try {
      if (wails?.ResetSystemProxy) {
        const result = await wails.ResetSystemProxy();
        setResetResult(result);
        // Refresh recovery status after reset.
        setTimeout(() => refresh(), 1500);
      }
    } catch (error) {
      setResetResult({ success: false, partial: false, message: error?.message || 'Reset failed.' });
    } finally {
      setResetting(false);
    }
  }, [refresh]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  return { recovery, loading, resetting, resetResult, resetProxy, refresh };
}

export async function buildDiagnosticsReport() {
  if (wails?.BuildDiagnosticsReport) {
    return await wails.BuildDiagnosticsReport();
  }
  return MOCK_REPORT_TEXT;
}

export async function exportDiagnosticsBundle() {
  if (wails?.ExportDiagnosticsBundle) {
    return await wails.ExportDiagnosticsBundle();
  }
  return MOCK_EXPORT_RESULT;
}
