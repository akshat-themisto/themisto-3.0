import { useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { AlertCircle, ArrowRight, CheckCircle2, Link2, Settings2, Upload } from 'lucide-react';

const wails = window.go?.main?.App;

function emptyForm() {
  return {
    token: '',
    orgName: '',
    deviceId: '',
    backendUrl: '',
    gatewayUrl: '',
  };
}

export default function Enrollment() {
  const navigate = useNavigate();
  const [loading, setLoading] = useState(false);
  const [importingLink, setImportingLink] = useState(false);
  const [importingFile, setImportingFile] = useState(false);
  const [showAdvanced, setShowAdvanced] = useState(false);
  const [result, setResult] = useState(null);
  const [sourceInput, setSourceInput] = useState('');
  const [sourceStatus, setSourceStatus] = useState(null);
  const [form, setForm] = useState(emptyForm());

  useEffect(() => {
    let cancelled = false;

    async function loadDefaults() {
      try {
        if (!wails?.GetEnrollmentDefaults) return;
        const defaults = await wails.GetEnrollmentDefaults();
        if (cancelled || !defaults) return;

        applyDefaults(defaults);

        if (defaults.token || defaults.device_id || defaults.backend_url || defaults.org_name) {
          setSourceStatus({
            tone: 'info',
            text: 'Themisto found a pending enrollment configuration on this device. You can continue with one click or refine it below.',
          });
        }
      } catch {
        // Best-effort only; manual setup is still available.
      }
    }

    function applyDefaults(defaults) {
      setForm((prev) => ({
        token: defaults.token || prev.token,
        orgName: defaults.org_name || defaults.orgName || prev.orgName,
        deviceId: defaults.device_id || defaults.deviceId || prev.deviceId,
        backendUrl: defaults.backend_url || defaults.backendUrl || prev.backendUrl,
        gatewayUrl: defaults.gateway_url || defaults.gatewayUrl || prev.gatewayUrl,
      }));
    }

    loadDefaults();
    return () => {
      cancelled = true;
    };
  }, []);

  function update(field, value) {
    setForm((prev) => ({ ...prev, [field]: value }));
  }

  function applyImportedDefaults(defaults) {
    setForm((prev) => ({
      token: defaults?.token || prev.token,
      orgName: defaults?.org_name || defaults?.orgName || prev.orgName,
      deviceId: defaults?.device_id || defaults?.deviceId || prev.deviceId,
      backendUrl: defaults?.backend_url || defaults?.backendUrl || prev.backendUrl,
      gatewayUrl: defaults?.gateway_url || defaults?.gatewayUrl || prev.gatewayUrl,
    }));
  }

  async function handleLoadLink() {
    if (!wails?.UseEnrollmentLink) {
      setSourceStatus({
        tone: 'danger',
        text: 'Link import only works from the installed desktop app.',
      });
      return;
    }

    setImportingLink(true);
    try {
      const res = await wails.UseEnrollmentLink(sourceInput.trim());
      setSourceStatus({
        tone: res?.success ? 'success' : 'danger',
        text: res?.message || 'Could not load the enrollment link.',
      });
      if (res?.defaults) {
        applyImportedDefaults(res.defaults);
      }
      if (res?.success) {
        setShowAdvanced(false);
      }
    } catch (err) {
      setSourceStatus({
        tone: 'danger',
        text: err?.message || String(err),
      });
    } finally {
      setImportingLink(false);
    }
  }

  async function handleImportFile() {
    if (!wails?.ImportEnrollmentConfigFile) {
      setSourceStatus({
        tone: 'danger',
        text: 'Config import only works from the installed desktop app.',
      });
      return;
    }

    setImportingFile(true);
    try {
      const res = await wails.ImportEnrollmentConfigFile();
      setSourceStatus({
        tone: res?.success ? 'success' : res?.message === 'No config file selected.' ? 'info' : 'danger',
        text: res?.message || 'Could not import agent.json.',
      });
      if (res?.defaults) {
        applyImportedDefaults(res.defaults);
      }
      if (res?.success) {
        setShowAdvanced(false);
      }
    } catch (err) {
      setSourceStatus({
        tone: 'danger',
        text: err?.message || String(err),
      });
    } finally {
      setImportingFile(false);
    }
  }

  async function handleEnroll() {
    setLoading(true);
    setSourceStatus(null);
    try {
      if (!wails?.EnrollDevice) {
        setResult({
          success: false,
          message: 'Enrollment only works from the desktop app. The browser preview cannot call the native enrollment binding.',
        });
        return;
      }

      const res = await wails.EnrollDevice(
        form.token.trim(),
        form.backendUrl.trim(),
        form.gatewayUrl.trim(),
        form.orgName.trim(),
        form.deviceId.trim()
      );
      setResult(res);
    } catch (err) {
      setResult({ success: false, message: err?.message || String(err) });
    } finally {
      setLoading(false);
    }
  }

  const readyForOneClick = useMemo(() => {
    return Boolean(
      form.token.trim() &&
      form.orgName.trim() &&
      form.deviceId.trim() &&
      form.backendUrl.trim()
    );
  }, [form]);

  if (result) {
    return (
      <div className="workspace-page">
        <div className="card" style={{ maxWidth: 620, margin: '40px auto', textAlign: 'center', padding: 32 }}>
          {result.success ? (
            <>
              <CheckCircle2 size={48} style={{ color: 'var(--success)', marginBottom: 16 }} />
              <h2 style={{ fontSize: 22, fontWeight: 700, marginBottom: 8 }}>Enrollment complete</h2>
              <p style={{ color: 'var(--text-secondary)', fontSize: 14, marginBottom: 24 }}>
                {result.message}
              </p>
              <button className="btn btn-primary" onClick={() => navigate('/')}>
                Go to Home
              </button>
            </>
          ) : (
            <>
              <AlertCircle size={48} style={{ color: 'var(--danger)', marginBottom: 16 }} />
              <h2 style={{ fontSize: 22, fontWeight: 700, marginBottom: 8 }}>Enrollment failed</h2>
              <p style={{ color: 'var(--text-secondary)', fontSize: 14, marginBottom: 24 }}>
                {result.message}
              </p>
              <div style={{ display: 'flex', justifyContent: 'center', gap: 12 }}>
                <button className="btn" onClick={() => setResult(null)}>
                  Back
                </button>
                <button className="btn btn-primary" onClick={() => navigate('/')}>
                  Go to Home
                </button>
              </div>
            </>
          )}
        </div>
      </div>
    );
  }

  const bannerStyle = sourceStatus?.tone === 'danger'
    ? { borderColor: 'var(--border)', background: 'var(--surface-soft)', color: 'var(--text-primary)' }
    : sourceStatus?.tone === 'success'
      ? { borderColor: 'var(--border)', background: 'var(--surface-soft)', color: 'var(--text-primary)' }
      : { borderColor: 'var(--border)', background: 'var(--surface-soft)', color: 'var(--text-primary)' };

  return (
    <div className="workspace-page">
      <div className="page-title-row">
        <div>
          <h1 className="page-title">Enrollment</h1>
          <p className="page-subtitle">Bring in your link or config once, then let Themisto handle the rest.</p>
        </div>
      </div>

      <div style={{ display: 'grid', gridTemplateColumns: 'minmax(0, 1.2fr) minmax(280px, 0.8fr)', gap: 20 }}>
        <div className="card" style={{ padding: 24 }}>
          <div className="card-title" style={{ marginBottom: 8 }}>Fastest path</div>
          <p style={{ color: 'var(--text-secondary)', fontSize: 13, marginBottom: 18 }}>
            Paste the enrollment link from your admin dashboard, or import the downloaded <code>agent.json</code>.
          </p>

          <div className="form-group">
            <label className="form-label">Enrollment link or short code</label>
            <div style={{ display: 'flex', gap: 10 }}>
              <input
                className="input"
                value={sourceInput}
                onChange={(e) => setSourceInput(e.target.value)}
                placeholder="https://company.example/enroll/abcd1234 or abcd1234"
                autoFocus
              />
              <button className="btn btn-primary" type="button" disabled={importingLink || !sourceInput.trim()} onClick={handleLoadLink}>
                <Link2 size={14} />
                {importingLink ? 'Loading...' : 'Load'}
              </button>
            </div>
          </div>

          <div style={{ display: 'flex', alignItems: 'center', gap: 12, margin: '14px 0 10px' }}>
            <div style={{ flex: 1, height: 1, background: 'var(--border)' }} />
            <span style={{ color: 'var(--text-muted)', fontSize: 12, letterSpacing: '0.08em', textTransform: 'uppercase' }}>or</span>
            <div style={{ flex: 1, height: 1, background: 'var(--border)' }} />
          </div>

          <button className="btn" type="button" disabled={importingFile} onClick={handleImportFile}>
            <Upload size={14} />
            {importingFile ? 'Opening...' : 'Import agent.json'}
          </button>

          {sourceStatus && (
            <div
              style={{
                marginTop: 16,
                border: '1px solid',
                borderRadius: 12,
                padding: '12px 14px',
                fontSize: 13,
                ...bannerStyle,
              }}
            >
              {sourceStatus.text}
            </div>
          )}

          <div style={{ marginTop: 22, display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: 12 }}>
            <div>
              <div style={{ fontSize: 12, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.08em', marginBottom: 4 }}>
                Ready to continue
              </div>
              <div style={{ color: readyForOneClick ? 'var(--text-primary)' : 'var(--text-secondary)', fontSize: 13 }}>
                {readyForOneClick
                  ? 'Themisto has enough information to finish enrollment now.'
                  : 'We still need token, org, device, and backend details. You can fill any missing values below.'}
              </div>
            </div>
            <button className="btn btn-primary" type="button" disabled={loading || !readyForOneClick} onClick={handleEnroll}>
              {loading ? 'Enrolling...' : 'Enroll device'}
              {!loading && <ArrowRight size={14} />}
            </button>
          </div>
        </div>

        <div className="card" style={{ padding: 24 }}>
          <div className="card-title" style={{ marginBottom: 16 }}>What Themisto will do</div>
          <div className="card-row">
            <span className="card-row-label">Enrollment token</span>
            <span className="card-row-value">{form.token ? 'Loaded' : 'Missing'}</span>
          </div>
          <div className="card-row">
            <span className="card-row-label">Organization</span>
            <span className="card-row-value">{form.orgName || 'Waiting'}</span>
          </div>
          <div className="card-row">
            <span className="card-row-label">Device ID</span>
            <span className="card-row-value">{form.deviceId || 'Waiting'}</span>
          </div>
          <div className="card-row">
            <span className="card-row-label">Backend</span>
            <span className="card-row-value">{form.backendUrl || 'Waiting'}</span>
          </div>
          <div className="card-row">
            <span className="card-row-label">Gateway</span>
            <span className="card-row-value">{form.gatewayUrl || 'Optional'}</span>
          </div>

          <div
            style={{
              marginTop: 18,
              borderRadius: 12,
              border: '1px solid var(--border)',
              background: 'var(--surface-soft)',
              padding: 14,
              color: 'var(--text-secondary)',
              fontSize: 13,
            }}
          >
            <div style={{ fontWeight: 600, color: 'var(--text-primary)', marginBottom: 6 }}>After you click enroll</div>
            <div>Themisto will contact your backend, mint device credentials, update local config, and bring the protected runtime online.</div>
          </div>
        </div>
      </div>

      <div className="card" style={{ marginTop: 20, padding: 24 }}>
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 12, marginBottom: showAdvanced ? 18 : 0 }}>
          <div>
            <div className="card-title" style={{ marginBottom: 4 }}>Advanced setup</div>
            <div style={{ color: 'var(--text-secondary)', fontSize: 13 }}>
              Only use this if you need to override or repair missing values manually.
            </div>
          </div>
          <button className="btn" type="button" onClick={() => setShowAdvanced((prev) => !prev)}>
            <Settings2 size={14} />
            {showAdvanced ? 'Hide fields' : 'Edit manually'}
          </button>
        </div>

        {showAdvanced && (
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(2, minmax(0, 1fr))', gap: 16 }}>
            <div className="form-group">
              <label className="form-label">Enrollment token</label>
              <input className="input" value={form.token} onChange={(e) => update('token', e.target.value)} placeholder="Paste token" />
            </div>
            <div className="form-group">
              <label className="form-label">Organization name</label>
              <input className="input" value={form.orgName} onChange={(e) => update('orgName', e.target.value)} placeholder="Acme Corp" />
            </div>
            <div className="form-group">
              <label className="form-label">Device ID</label>
              <input className="input" value={form.deviceId} onChange={(e) => update('deviceId', e.target.value)} placeholder="laptop-jane-001" />
            </div>
            <div className="form-group">
              <label className="form-label">Backend URL</label>
              <input className="input" value={form.backendUrl} onChange={(e) => update('backendUrl', e.target.value)} placeholder="https://api.company.com" />
            </div>
            <div className="form-group" style={{ gridColumn: '1 / -1' }}>
              <label className="form-label">Gateway URL (optional)</label>
              <input className="input" value={form.gatewayUrl} onChange={(e) => update('gatewayUrl', e.target.value)} placeholder="https://gateway.company.com" />
            </div>
          </div>
        )}
      </div>

      <div style={{ marginTop: 16, display: 'flex', justifyContent: 'flex-start' }}>
        <button className="btn" type="button" onClick={() => navigate('/')}>
          Cancel
        </button>
      </div>
    </div>
  );
}
