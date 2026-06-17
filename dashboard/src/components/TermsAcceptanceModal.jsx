import { useState } from 'react';
import { ArrowRight, ShieldCheck } from 'lucide-react';
import { useAuth } from '../context/AuthContext';

export default function TermsAcceptanceModal() {
    const { user, acceptTerms } = useAuth();
    const [checked, setChecked] = useState(false);
    const [submitting, setSubmitting] = useState(false);
    const [error, setError] = useState('');

    if (!user || user.must_change_password || user.terms_accepted_at) {
        return null;
    }

    const handleAccept = async () => {
        if (!checked || submitting) return;
        setError('');
        setSubmitting(true);
        try {
            await acceptTerms();
        } catch (err) {
            setError(err.message || 'Could not record acceptance. Please try again.');
        } finally {
            setSubmitting(false);
        }
    };

    return (
        <div className="terms-modal-overlay" role="presentation">
            <section className="terms-modal" role="dialog" aria-modal="true" aria-labelledby="terms-modal-title">
                <div className="terms-modal-icon">
                    <ShieldCheck size={22} />
                </div>
                <div className="terms-modal-copy">
                    <p className="terms-modal-kicker">First sign-in requirement</p>
                    <h2 id="terms-modal-title">Terms and Conditions</h2>
                    <p>
                        Before using Themisto Labs, confirm that you understand the security and compliance responsibilities attached to this workspace.
                    </p>
                </div>

                <ul className="terms-modal-list">
                    <li>Use Themisto only for authorized organization security and AI governance work.</li>
                    <li>Prompt capture and DLP events may include sensitive content; review and export them only when permitted.</li>
                    <li>Do not attempt to bypass endpoint protection, browser extension coverage, or policy controls.</li>
                    <li>You are responsible for following your organization&apos;s privacy, security, and compliance requirements.</li>
                </ul>

                <label className="terms-modal-check">
                    <input
                        type="checkbox"
                        checked={checked}
                        onChange={(event) => setChecked(event.target.checked)}
                    />
                    <span>I have read and agree to the Themisto Labs Terms and Conditions.</span>
                </label>

                {error && <div className="terms-modal-error">{error}</div>}

                <button className="btn btn-primary terms-modal-action" type="button" disabled={!checked || submitting} onClick={handleAccept}>
                    {submitting ? 'Recording...' : 'Accept and Continue'}
                    <ArrowRight size={15} />
                </button>
            </section>
        </div>
    );
}
