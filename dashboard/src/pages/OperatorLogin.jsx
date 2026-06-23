import { useState } from 'react';
import { Navigate } from 'react-router-dom';
import { ArrowRight, Eye, EyeOff, ShieldCheck } from 'lucide-react';
import { useOperatorAuth } from '../context/OperatorAuthContext';

export default function OperatorLogin() {
    const { operator, loading: authLoading, login } = useOperatorAuth();
    const [accessKey, setAccessKey] = useState('');
    const [showKey, setShowKey] = useState(false);
    const [error, setError] = useState('');
    const [loading, setLoading] = useState(false);

    if (!authLoading && operator) return <Navigate to="/" replace />;

    async function submit(event) {
        event.preventDefault();
        setError('');
        setLoading(true);
        try {
            await login(accessKey);
        } catch (err) {
            setError(err.message || 'Operator sign-in failed');
        } finally {
            setLoading(false);
        }
    }

    return (
        <div className="login-page operator-login-page">
            <div className="operator-login-shell">
                <div className="operator-login-brand">
                    <img src="/logo.png" alt="Themisto Labs" />
                    <div><strong>Themisto Labs</strong><span>Customer Operations</span></div>
                </div>
                <div className="operator-login-heading">
                    <ShieldCheck size={22} />
                    <div><h1>Operations sign in</h1><p>Authorized Themisto operations personnel only.</p></div>
                </div>
                {error && <div className="login-error">{error}</div>}
                <form className="login-form" onSubmit={submit}>
                    <div className="form-group">
                        <label className="login-label" htmlFor="operator-access-key">Operator access key</label>
                        <div className="login-password-wrap">
                            <input
                                id="operator-access-key"
                                className="login-input login-input-password"
                                type={showKey ? 'text' : 'password'}
                                value={accessKey}
                                onChange={(event) => setAccessKey(event.target.value)}
                                autoComplete="current-password"
                                required
                                autoFocus
                            />
                            <button type="button" className="login-password-toggle" onClick={() => setShowKey((value) => !value)} aria-label={showKey ? 'Hide access key' : 'Show access key'}>
                                {showKey ? <EyeOff size={18} /> : <Eye size={18} />}
                            </button>
                        </div>
                    </div>
                    <button className="login-submit" type="submit" disabled={loading || authLoading}>
                        <span>{loading ? 'Verifying...' : 'Continue'}</span><ArrowRight size={18} />
                    </button>
                </form>
                <div className="operator-login-footnote">Session expires after 30 minutes.</div>
            </div>
        </div>
    );
}
