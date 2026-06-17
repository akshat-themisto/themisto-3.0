import { useState } from 'react';
import { Navigate } from 'react-router-dom';
import { ArrowRight, Eye, EyeOff } from 'lucide-react';
import { useAuth } from '../context/AuthContext';

export default function Login() {
    const { user, login } = useAuth();
    const [email, setEmail] = useState('');
    const [password, setPassword] = useState('');
    const [error, setError] = useState('');
    const [loading, setLoading] = useState(false);
    const [showPassword, setShowPassword] = useState(false);

    if (user) return <Navigate to="/" replace />;

    const handleSubmit = async (e) => {
        e.preventDefault();
        setError('');
        setLoading(true);
        try {
            await login(email, password);
        } catch (err) {
            setError(err.message || 'Login failed');
        } finally {
            setLoading(false);
        }
    };

    return (
        <div className="login-page">
            <div className="login-shell">
                <section className="login-panel login-panel-form">
                    <div className="login-panel-inner">
                        <div className="login-copy">
                            <h1>Sign in</h1>
                            <p>Use your organization credentials to continue.</p>
                        </div>

                        {error && <div className="login-error">{error}</div>}

                        <form className="login-form" onSubmit={handleSubmit}>
                            <div className="form-group">
                                <label className="login-label">Email</label>
                                <input
                                    className="login-input"
                                    type="email"
                                    placeholder="admin@themisto.dev"
                                    value={email}
                                    onChange={(e) => setEmail(e.target.value)}
                                    required
                                    autoFocus
                                />
                            </div>

                            <div className="form-group">
                                <label className="login-label">Password</label>
                                <div className="login-password-wrap">
                                    <input
                                        className="login-input login-input-password"
                                        type={showPassword ? 'text' : 'password'}
                                        placeholder="Enter your password"
                                        value={password}
                                        onChange={(e) => setPassword(e.target.value)}
                                        required
                                    />
                                    <button
                                        type="button"
                                        className="login-password-toggle"
                                        onClick={() => setShowPassword((value) => !value)}
                                        aria-label={showPassword ? 'Hide password' : 'Show password'}
                                    >
                                        {showPassword ? <EyeOff size={18} /> : <Eye size={18} />}
                                    </button>
                                </div>
                            </div>

                            <div className="login-meta-row">
                                <span>Protected security console</span>
                                <span>Role-aware access</span>
                            </div>

                            <button className="login-submit" type="submit" disabled={loading}>
                                <span>{loading ? 'Signing in...' : 'Sign in'}</span>
                                <ArrowRight size={18} />
                            </button>
                        </form>

                        <div className="login-footnote">
                            Authorized access only.
                        </div>
                    </div>
                </section>

                <section className="login-panel login-panel-showcase">
                    <div className="login-showcase-center">
                        <div className="login-showcase-orbit login-showcase-orbit-outer" />
                        <div className="login-showcase-orbit login-showcase-orbit-inner" />

                        <div className="login-hero-card">
                            <div className="login-hero-logo-shell">
                                <div className="login-hero-logo-rotation">
                                    <img src="/logo.png" alt="Themisto Labs emblem" className="login-hero-logo" />
                                </div>
                            </div>

                            <div className="login-hero-insight">
                                <span className="login-hero-insight-label">Security console</span>
                                <strong>DLP and AI governance signals with endpoint-level control.</strong>
                            </div>
                        </div>
                    </div>

                    <div className="login-showcase-copy">
                        <h2>Themisto Labs workspace.</h2>
                        <p>AI governance shaped for enterprise security teams.</p>
                    </div>
                </section>
            </div>
        </div>
    );
}
