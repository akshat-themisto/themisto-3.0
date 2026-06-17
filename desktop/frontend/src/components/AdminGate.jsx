import { useState } from 'react';

const wails = window.go?.main?.App;
const isDev = !wails;

export default function AdminGate({ title, description, onAuthorized, onCancel }) {
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);

  async function handleSubmit(e) {
    e.preventDefault();
    setError('');
    setLoading(true);
    try {
      if (isDev) {
        // Dev mode: accept any credentials
        onAuthorized('dev-mock-token');
        return;
      }
      const token = await wails?.ValidateAdmin(email, password);
      if (token) {
        onAuthorized(token);
      } else {
        setError('Authentication failed');
      }
    } catch (err) {
      setError(err?.message || String(err) || 'Authentication failed');
    } finally {
      setLoading(false);
    }
  }

  return (
    <div className="modal-overlay" onClick={onCancel}>
      <div className="modal" onClick={(e) => e.stopPropagation()}>
        <div className="modal-title">{title || 'Admin Authentication Required'}</div>
        <div className="modal-desc">
          {description || 'This action requires administrator credentials.'}
        </div>
        <form onSubmit={handleSubmit}>
          <div className="form-group">
            <label className="form-label">Email</label>
            <input
              className="input"
              type="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              placeholder="admin@company.com"
              autoFocus
              required
            />
          </div>
          <div className="form-group">
            <label className="form-label">Password</label>
            <input
              className="input"
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder="Enter password"
              required
            />
          </div>
          {error && <div className="modal-error">{error}</div>}
          <div className="modal-actions">
            <button type="button" className="btn" onClick={onCancel} disabled={loading}>
              Cancel
            </button>
            <button type="submit" className="btn btn-primary" disabled={loading}>
              {loading ? 'Authenticating...' : 'Authenticate'}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
