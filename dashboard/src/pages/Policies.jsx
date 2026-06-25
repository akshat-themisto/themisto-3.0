import { useEffect, useMemo, useState } from 'react';
import { api } from '../api/client';
import { useAuth } from '../context/AuthContext';
import { useToast } from '../context/ToastContext';
import { Bot, Globe2, Monitor, Play, Shield, ShieldCheck, Plus, X } from 'lucide-react';
import AppSelect from '../components/AppSelect';

const FIELD_OPTIONS = [
    { value: 'host', label: 'Host' },
    { value: 'path', label: 'Path' },
    { value: 'method', label: 'Method' },
    { value: 'scheme', label: 'Scheme' },
    { value: 'process_name', label: 'Process Name' },
    { value: 'process_path', label: 'Process Path' },
    { value: 'process_user', label: 'Process User' },
    { value: 'process_bundle', label: 'Process Bundle' },
    { value: 'process_signer', label: 'Process Signer' },
    { value: 'process_signed', label: 'Process Signed' },
    { value: 'service_category', label: 'Service Category' },
    { value: 'ai_vendor', label: 'AI Vendor' },
    { value: 'body_contains_pii', label: 'Body Contains PII' },
    { value: 'body_contains_credentials', label: 'Body Contains Credentials' },
    { value: 'body_contains_source_code', label: 'Body Contains Source Code' },
    { value: 'body_has_dlp_match', label: 'Body Has DLP Match' },
];

const OP_OPTIONS = [
    { value: 'eq', label: 'Equals' },
    { value: 'contains', label: 'Contains' },
    { value: 'prefix', label: 'Starts With' },
    { value: 'suffix', label: 'Ends With' },
    { value: 'glob', label: 'Glob' },
    { value: 'regex', label: 'Regex' },
];

function emptyCondition() {
    return { field: 'host', operator: 'contains', value: '', negate: false };
}

function emptyForm(priority = 10) {
    return {
        name: '',
        priority,
        enabled: true,
        action: 'alert',
        block_reason: '',
        conditions: [emptyCondition()],
    };
}

function emptySimpleForm() {
    return {
        name: '',
        target_type: 'governance_ai',
        target_value: '',
        expectation: '',
        priority: 50,
        enabled: true,
        message: '',
    };
}

function defaultPriorityForAction(action) {
    switch ((action || '').trim().toLowerCase()) {
        case 'allow':
            return 150;
        case 'alert':
            return 100;
        case 'block':
        default:
            return 50;
    }
}

function defaultSimpleRuleName(simpleForm) {
    if (simpleForm.target_type === 'governance_ai') {
        const expectation = (simpleForm.expectation || '').trim();
        return expectation ? `AI policy: ${expectation.slice(0, 72)}` : 'Smart AI governance policy';
    }
    if (simpleForm.target_type === 'ai') return 'Block detected AI tools';
    const target = (simpleForm.target_value || '').trim();
    if (!target) return 'Policy rule';
    return `Block ${target}`;
}

function defaultSimpleMessage(simpleForm) {
    if (simpleForm.target_type === 'governance_ai') {
        const expectation = (simpleForm.expectation || '').trim();
        return expectation || 'This AI request is not allowed by your organization policy.';
    }
    if (simpleForm.target_type === 'ai') {
        return 'AI tools are blocked by your organization.';
    }
    const target = (simpleForm.target_value || '').trim();
    const noun = simpleForm.target_type === 'tool' ? 'tool' : 'website';
    if (!target) {
        return `This ${noun} is blocked by your organization.`;
    }
    return `${target} is blocked by your organization.`;
}

function buildEditorFormFromSimple(simpleForm) {
    const targetType = simpleForm.target_type === 'governance_ai' ? 'governance_ai' : simpleForm.target_type === 'ai' ? 'ai' : simpleForm.target_type === 'tool' ? 'tool' : 'website';
    const targetField = targetType === 'governance_ai' || targetType === 'ai' ? 'service_category' : targetType === 'tool' ? 'ai_vendor' : 'host';
    const targetValue = targetType === 'governance_ai' || targetType === 'ai' ? 'ai_llm' : (simpleForm.target_value || '').trim();

    return {
        name: (simpleForm.name || '').trim() || defaultSimpleRuleName(simpleForm),
        priority: Number(simpleForm.priority) || defaultPriorityForAction('block'),
        enabled: !!simpleForm.enabled,
        action: targetType === 'governance_ai' ? 'alert' : 'block',
        block_reason: (simpleForm.message || '').trim() || defaultSimpleMessage(simpleForm),
        conditions: [
            {
                field: targetField,
                operator: targetType === 'ai' ? 'eq' : 'contains',
                value: targetValue,
                negate: false,
            },
        ],
    };
}

function deriveSimpleForm(form) {
    const conditions = Array.isArray(form.conditions) ? form.conditions : [];
    const primary = conditions.find((condition) => {
        const field = (condition?.field || '').trim().toLowerCase();
        return !condition?.negate && (field === 'host' || field === 'ai_vendor');
    }) || conditions[0] || null;
    const field = (primary?.field || '').trim().toLowerCase();
    const isAIToolRule = field === 'service_category' && primary?.value === 'ai_llm';
    const action = (form.action || '').trim().toLowerCase();
    const blockReason = (form.block_reason || '').trim();
    const generatedName = defaultSimpleRuleName({
        target_type: isAIToolRule && action === 'alert' ? 'governance_ai' : isAIToolRule ? 'ai' : field === 'ai_vendor' ? 'tool' : 'website',
        target_value: primary?.value || '',
        action: form.action || 'block',
        expectation: blockReason,
    });
    const currentName = (form.name || '').trim();
    const simpleName = !currentName || currentName === 'Policy rule' || currentName === generatedName ? '' : currentName;

    return {
        name: simpleName,
        target_type: isAIToolRule && action === 'alert' ? 'governance_ai' : isAIToolRule ? 'ai' : field === 'ai_vendor' ? 'tool' : 'website',
        target_value: primary?.value || '',
        expectation: isAIToolRule && action === 'alert' ? blockReason : '',
        priority: Number(form.priority) || defaultPriorityForAction(form.action),
        enabled: !!form.enabled,
        message: isAIToolRule && action === 'alert' ? '' : form.block_reason || '',
    };
}

function isSimpleCompatibleRule(rule) {
    const name = (rule?.name || '').trim().toLowerCase();
    if (name.startsWith('managed:') || name.startsWith('auto:')) {
        return false;
    }

    const conditions = Array.isArray(rule?.conditions) ? rule.conditions : [];
    if (conditions.length !== 1) {
        return false;
    }

    const condition = conditions[0];
    if (!condition || condition.negate) {
        return false;
    }

    const field = (condition.field || '').trim().toLowerCase();
    return field === 'host' || field === 'ai_vendor' || (field === 'service_category' && condition.value === 'ai_llm');
}

function buildPayload(form) {
    const conditions = (form.conditions || [])
        .map((c) => ({
            field: (c.field || '').trim(),
            operator: (c.operator || '').trim(),
            value: (c.value || '').trim(),
            negate: !!c.negate,
        }))
        .filter((c) => c.field && c.operator);

    return {
        name: (form.name || '').trim() || 'Policy rule',
        priority: Number(form.priority) || 10,
        enabled: !!form.enabled,
        action: form.action || 'alert',
        conditions,
        block_reason: (form.block_reason || '').trim() || null,
    };
}

function ConditionRow({ condition, onChange, onRemove, disabled }) {
    return (
        <div className="policy-condition-row">
            <AppSelect
                value={condition.field}
                disabled={disabled}
                onValueChange={(value) => onChange({ ...condition, field: value })}
                options={FIELD_OPTIONS}
            />
            <AppSelect
                value={condition.operator}
                disabled={disabled}
                onValueChange={(value) => onChange({ ...condition, operator: value })}
                options={OP_OPTIONS}
            />
            <input className="form-input" placeholder="Value" value={condition.value} disabled={disabled} onChange={(e) => onChange({ ...condition, value: e.target.value })} />
            <label style={{ display: 'flex', alignItems: 'center', gap: 6, color: 'var(--text-muted)', fontSize: 12 }}>
                <input type="checkbox" checked={!!condition.negate} disabled={disabled} onChange={(e) => onChange({ ...condition, negate: e.target.checked })} />
                Not
            </label>
            <button className="btn btn-sm btn-danger" disabled={disabled} onClick={onRemove}>Remove</button>
        </div>
    );
}

export default function Policies() {
    const { user } = useAuth();
    const isAdmin = user?.role === 'admin' || user?.role === 'owner';
    const toast = useToast();

    const [rules, setRules] = useState([]);
    const [loading, setLoading] = useState(true);
    const [showModal, setShowModal] = useState(false);
    const [editing, setEditing] = useState(null);
    const [editorMode, setEditorMode] = useState('simple');
    const [form, setForm] = useState(emptyForm());
    const [simpleForm, setSimpleForm] = useState(emptySimpleForm());
    const [saving, setSaving] = useState(false);
    const [testPrompt, setTestPrompt] = useState('');
    const [testResult, setTestResult] = useState(null);
    const [testLoading, setTestLoading] = useState(false);
    const [testError, setTestError] = useState('');
    const [enforcementOverride, setEnforcementOverride] = useState('');
    const [savingEnforcement, setSavingEnforcement] = useState(false);

    const enabledCount = useMemo(() => rules.filter((r) => r.enabled).length, [rules]);
    const baselineRules = useMemo(() => rules.filter((r) => (r.name || '').startsWith('Managed: AI ')), [rules]);

    useEffect(() => {
        loadPolicies();
    }, []);

    const loadPolicies = () => {
        setLoading(true);
        api.listPolicies()
            .then((d) => {
                setRules(d.rules || []);
                setEnforcementOverride(d.prompt_enforcement_override || '');
            })
            .catch(() => setRules([]))
            .finally(() => setLoading(false));
    };

    const saveEnforcementOverride = async () => {
        setSavingEnforcement(true);
        try {
            const result = await api.updatePolicyEnforcement(enforcementOverride);
            setEnforcementOverride(result?.prompt_enforcement_override || '');
            toast.success('Prompt enforcement override updated.');
        } catch (err) {
            toast.error(err.message || 'Failed to update prompt enforcement override');
        } finally {
            setSavingEnforcement(false);
        }
    };

    const openCreate = () => {
        setEditing(null);
        setEditorMode('simple');
        setForm(emptyForm((rules.length + 1) * 10));
        setSimpleForm(emptySimpleForm());
        setShowModal(true);
    };

    const openEdit = (rule) => {
        setEditing(rule);
        const nextForm = {
            name: rule.name || '',
            priority: rule.priority || 10,
            enabled: !!rule.enabled,
            action: rule.action || 'alert',
            block_reason: rule.block_reason || '',
            conditions: (rule.conditions && rule.conditions.length > 0)
                ? rule.conditions.map((c) => ({ field: c.field || 'host', operator: c.operator || 'contains', value: c.value || '', negate: !!c.negate }))
                : [emptyCondition()],
        };
        setForm(nextForm);
        setEditorMode(isSimpleCompatibleRule(rule) ? 'simple' : 'advanced');
        setSimpleForm(deriveSimpleForm(nextForm));
        setShowModal(true);
    };

    const switchEditorMode = (nextMode) => {
        if (nextMode === editorMode) return;

        if (nextMode === 'advanced') {
            setForm(buildEditorFormFromSimple(simpleForm));
        } else {
            setSimpleForm(deriveSimpleForm(form));
        }

        setEditorMode(nextMode);
    };

    const saveRule = async () => {
        let payload;
        if (editorMode === 'simple') {
            const targetValue = (simpleForm.target_value || '').trim();
            if (simpleForm.target_type !== 'governance_ai' && simpleForm.target_type !== 'ai' && !targetValue) {
                toast.warning(simpleForm.target_type === 'tool' ? 'Enter a tool name' : 'Enter a website');
                return;
            }
            if (simpleForm.target_type === 'governance_ai' && !(simpleForm.expectation || '').trim()) {
                toast.warning('Write the AI governance policy in plain language');
                return;
            }
            payload = buildPayload(buildEditorFormFromSimple(simpleForm));
        } else {
            payload = buildPayload(form);
        }

        if (payload.conditions.length === 0) {
            toast.warning('Add at least one condition');
            return;
        }

        setSaving(true);
        try {
            if (editing) {
                await api.updatePolicy(editing.id, payload);
            } else {
                await api.createPolicy(payload);
            }
            setShowModal(false);
            toast.success(editing ? 'Policy updated.' : 'Policy created.');
            loadPolicies();
        } catch (err) {
            toast.error(err.message || 'Failed to save policy');
        } finally {
            setSaving(false);
        }
    };

    const deleteRule = async (id) => {
        if (!confirm('Delete this policy rule?')) return;
        try {
            await api.deletePolicy(id);
            toast.success('Policy deleted.');
            loadPolicies();
        } catch (err) {
            toast.error(err.message || 'Failed to delete policy');
        }
    };

    const toggleEnabled = async (rule) => {
        try {
            await api.updatePolicy(rule.id, {
                name: rule.name,
                priority: rule.priority,
                enabled: !rule.enabled,
                action: rule.action,
                conditions: rule.conditions || [],
                block_reason: rule.block_reason || null,
            });
            toast.success('Rule state updated.');
            loadPolicies();
        } catch (err) {
            toast.error(err.message || 'Failed to update rule');
        }
    };

    const runPromptTest = async () => {
        const prompt = testPrompt.trim();
        if (!prompt) {
            toast.warning('Enter a prompt to test');
            return;
        }
        setTestLoading(true);
        setTestError('');
        setTestResult(null);
        try {
            const result = await api.promptPolicyTest({
                prompt_text: prompt,
                surface: 'browser_chromium',
                destination_url: 'https://chatgpt.com/',
                vendor: 'openai',
                service_category: 'ai_llm',
            });
            setTestResult(result?.result || result);
        } catch (err) {
            setTestError(err?.message || 'Real prompt evaluator is unavailable');
        } finally {
            setTestLoading(false);
        }
    };

    return (
        <>
            <div className="topbar">
                <div className="topbar-copy">
                    <div className="page-kicker">Corporate Policy</div>
                    <h1 className="topbar-title">Policies</h1>
                    <div className="page-subtitle">
                        Set AI governance expectations in plain language. Themisto handles technical matching in the background.
                    </div>
                </div>
                <div className="topbar-actions">
                    <div className="page-summary-card">
                        <span className="page-summary-label">Active coverage</span>
                        <strong>{enabledCount} enabled</strong>
                        <span>{rules.length} total policies in this workspace</span>
                    </div>
                    {isAdmin && (
                        <button className="btn btn-primary" onClick={openCreate}>
                            <Plus size={16} /> New Policy
                        </button>
                    )}
                </div>
            </div>

            <div className="page-content">
                <div className="policy-ops-grid">
                    <section className="chart-card policy-card">
                        <div className="policy-card-head">
                            <div>
                                <h2>Baseline DLP</h2>
                                <span>Managed AI DLP rules that protect credentials, personal data, source code, and custom matches.</span>
                            </div>
                            <ShieldCheck size={18} />
                        </div>
                        <div className="baseline-dlp-grid">
                            {['Credentials', 'PII', 'Source Code', 'DLP Match'].map((label) => {
                                const rule = baselineRules.find((r) => (r.name || '').toLowerCase().includes(label.toLowerCase().replace('dlp match', 'dlp match')));
                                return (
                                    <div className="baseline-dlp-item" key={label}>
                                        <span>{label}</span>
                                        <strong>{rule?.action || 'not configured'}</strong>
                                        <small>{rule?.enabled ? 'Enabled' : 'Not enabled'}</small>
                                    </div>
                                );
                            })}
                        </div>
                    </section>

                    <section className="chart-card policy-card">
                        <div className="policy-card-head">
                            <div>
                                <h2>Semantic Controls</h2>
                                <span>Semantic classification adds context; deterministic DLP still owns exact sensitive matches.</span>
                            </div>
                            <Bot size={18} />
                        </div>
                        <div className="semantic-control-list">
                            <label><input type="checkbox" checked readOnly /> Local classifier enabled when agent config enables prompt semantics</label>
                            <label><input type="checkbox" checked readOnly /> Gateway fallback used for ambiguous or low-confidence prompts</label>
                            <label><input type="checkbox" checked readOnly /> Ambiguous prompts alert by default</label>
                        </div>
                        <div className="policy-enforcement-control">
                            <label className="form-label">Emergency enforcement override</label>
                            <div className="policy-enforcement-row">
                                <select
                                    className="form-select"
                                    value={enforcementOverride}
                                    disabled={!isAdmin || savingEnforcement}
                                    onChange={(event) => setEnforcementOverride(event.target.value)}
                                >
                                    <option value="">Normal policy</option>
                                    <option value="monitor">Monitor only</option>
                                    <option value="alert">Alert only</option>
                                    <option value="enforce">Enforce</option>
                                </select>
                                <button className="btn btn-sm" disabled={!isAdmin || savingEnforcement} onClick={saveEnforcementOverride}>
                                    {savingEnforcement ? 'Saving...' : 'Apply'}
                                </button>
                            </div>
                            <span className="policy-helper-text">Use monitor only to recover a fleet if prompt protection is blocking because the local evaluator is down.</span>
                        </div>
                    </section>
                </div>

                <section className="chart-card policy-test-card">
                    <div className="policy-card-head">
                        <div>
                            <h2>Prompt Test</h2>
                            <span>Runs against the real local prompt evaluator path. If the agent or classifier is offline, this reports unavailable.</span>
                        </div>
                        <button className="btn btn-primary" onClick={runPromptTest} disabled={testLoading}>
                            <Play size={14} /> {testLoading ? 'Testing...' : 'Run Test'}
                        </button>
                    </div>
                    <textarea
                        className="form-input policy-textarea"
                        placeholder="Paste a test prompt, for example: summarize this public blog post without sensitive data."
                        value={testPrompt}
                        onChange={(e) => setTestPrompt(e.target.value)}
                    />
                    {testError && <div className="dlp-inline-error policy-test-error">{testError}</div>}
                    {testResult && (
                        <div className="policy-test-result">
                            <div><span>Final outcome</span><strong>{testResult.decision || testResult.outcome || '-'}</strong></div>
                            <div><span>Severity</span><strong>{testResult.severity || '-'}</strong></div>
                            <div><span>Detectors</span><strong>{(testResult.match_types || []).join(', ') || `${testResult.match_count || 0} match(es)`}</strong></div>
                            <div><span>Semantic context</span><strong>{testResult.semantic ? `${testResult.semantic.source || 'semantic'} / ${Math.round((testResult.semantic.confidence || 0) * 100)}%` : 'No semantic result'}</strong></div>
                            <div className="policy-test-wide"><span>Reason</span><strong>{testResult.reason || testResult.message || '-'}</strong></div>
                        </div>
                    )}
                </section>

                <div className="data-table-wrap">
                    {loading ? (
                        <div className="loading-wrap"><div className="spinner" /></div>
                    ) : rules.length === 0 ? (
                        <div className="empty-state">
                            <Shield size={48} className="empty-state-icon" />
                            <div className="empty-state-text">No policies defined</div>
                            <div className="empty-state-sub">Start with a smart AI governance policy, then add website or tool restrictions only when you need them.</div>
                        </div>
                    ) : (
                        <table className="data-table">
                            <thead>
                                <tr>
                                    <th>Priority</th>
                                    <th>Policy</th>
                                    <th>Action</th>
                                    <th>Conditions</th>
                                    <th>User Message</th>
                                    <th>Enabled</th>
                                    {isAdmin && <th>Actions</th>}
                                </tr>
                            </thead>
                            <tbody>
                                {rules.map((r) => (
                                    <tr key={r.id}>
                                        <td style={{ fontWeight: 600 }}>{r.priority}</td>
                                        <td style={{ color: 'var(--text-primary)' }}>{r.name || 'Policy rule'}</td>
                                        <td><span className="badge">{r.action || 'alert'}</span></td>
                                        <td style={{ color: 'var(--text-secondary)' }}>{(r.conditions || []).length} condition(s)</td>
                                        <td style={{ color: 'var(--text-secondary)', maxWidth: 320, wordBreak: 'break-word' }}>{r.block_reason || '-'}</td>
                                        <td>
                                            <button
                                                className={`toggle ${r.enabled ? 'on' : ''}`}
                                                disabled={!isAdmin}
                                                onClick={() => toggleEnabled(r)}
                                            />
                                        </td>
                                        {isAdmin && (
                                            <td>
                                                <div style={{ display: 'flex', gap: 6 }}>
                                                    <button className="btn btn-sm" onClick={() => openEdit(r)}>Edit</button>
                                                    <button className="btn btn-sm btn-danger" style={{ background: 'transparent', border: '1px solid transparent' }} onClick={() => deleteRule(r.id)}>Delete</button>
                                                </div>
                                            </td>
                                        )}
                                    </tr>
                                ))}
                            </tbody>
                        </table>
                    )}
                </div>

                {showModal && (
                    <div className="modal-overlay policy-modal-overlay" onClick={() => !saving && setShowModal(false)}>
                        <div className="modal policy-modal" onClick={(e) => e.stopPropagation()}>
                            <div className="policy-modal-header">
                                <div className="policy-modal-header-row">
                                    <h2 className="modal-title">{editing ? 'Edit Policy' : 'New Policy'}</h2>
                                    <div className="policy-modal-header-actions">
                                        <div className="policy-mode-switch">
                                            <button
                                                type="button"
                                                className={`policy-mode-button ${editorMode === 'simple' ? 'active' : ''}`}
                                                onClick={() => switchEditorMode('simple')}
                                            >
                                                Simple
                                            </button>
                                            <button
                                                type="button"
                                                className={`policy-mode-button ${editorMode === 'advanced' ? 'active' : ''}`}
                                                onClick={() => switchEditorMode('advanced')}
                                            >
                                                Advanced
                                            </button>
                                        </div>
                                        <button
                                            type="button"
                                            className="policy-modal-close"
                                            onClick={() => !saving && setShowModal(false)}
                                            disabled={saving}
                                            aria-label="Close policy dialog"
                                        >
                                            <X size={16} />
                                        </button>
                                    </div>
                                </div>
                            </div>

                            <div className="policy-modal-body">
                                {editorMode === 'simple' ? (
                                    <>
                                        <div className="policy-simple-intro">
                                            <div className="policy-simple-title">Tell Themisto what users should avoid</div>
                                            <div className="policy-helper-text">Use normal security and governance language. Advanced matching, AI-site detection, and prompt checks stay behind the scenes.</div>
                                        </div>

                                        <div className="policy-choice-grid">
                                            {[
                                                { value: 'governance_ai', title: 'Smart AI policy', body: 'Best for data leakage, secrets, source code, and unsafe AI use.', icon: <Bot size={18} /> },
                                                { value: 'ai', title: 'Pause all AI tools', body: 'Use when AI should be completely unavailable.', icon: <Shield size={18} /> },
                                                { value: 'website', title: 'Block a website', body: 'Use for a specific site like games.example.com.', icon: <Globe2 size={18} /> },
                                                { value: 'tool', title: 'Block a tool', body: 'Use for a specific AI vendor or app.', icon: <Monitor size={18} /> },
                                            ].map((option) => (
                                                <button
                                                    key={option.value}
                                                    type="button"
                                                    className={`policy-choice ${simpleForm.target_type === option.value ? 'active' : ''}`}
                                                    onClick={() => setSimpleForm((current) => ({ ...current, target_type: option.value, target_value: option.value === 'governance_ai' || option.value === 'ai' ? '' : current.target_value }))}
                                                >
                                                    <span className="policy-choice-icon">{option.icon}</span>
                                                    <span>
                                                        <strong>{option.title}</strong>
                                                        <small>{option.body}</small>
                                                    </span>
                                                </button>
                                            ))}
                                        </div>

                                        {simpleForm.target_type === 'governance_ai' ? (
                                            <div className="form-group">
                                                <label className="form-label">AI governance policy</label>
                                                <textarea
                                                    className="form-input policy-textarea"
                                                    placeholder="Example: Users should not paste credentials, source code, regulated data, or customer records into unsanctioned AI tools."
                                                    value={simpleForm.expectation}
                                                    onChange={(e) => setSimpleForm((current) => ({ ...current, expectation: e.target.value }))}
                                                />
                                                <div className="policy-helper-text">This keeps approved AI workflows usable while the local classifier blocks or flags risky prompt content.</div>
                                            </div>
                                        ) : simpleForm.target_type === 'ai' ? (
                                            <div className="policy-coverage-note">
                                                <strong>Coverage</strong>
                                                <span>Blocks known AI tools and newly detected AI prompt pages.</span>
                                            </div>
                                        ) : (
                                            <div className="form-group">
                                                <label className="form-label">{simpleForm.target_type === 'tool' ? 'Tool or vendor name' : 'Website'}</label>
                                                <input
                                                    className="form-input"
                                                    placeholder={simpleForm.target_type === 'tool' ? 'Example: Cursor or GitHub Copilot' : 'Example: coolmathgames.com'}
                                                    value={simpleForm.target_value}
                                                    onChange={(e) => setSimpleForm((current) => ({ ...current, target_value: e.target.value }))}
                                                />
                                            </div>
                                        )}

                                        <div className="policy-simple-footer">
                                            <label className="policy-checkbox-label">
                                                <input type="checkbox" checked={!!simpleForm.enabled} onChange={(e) => setSimpleForm((current) => ({ ...current, enabled: e.target.checked }))} />
                                                Policy enabled
                                            </label>
                                        </div>
                                    </>
                                ) : (
                                    <>
                                        <div className="policy-modal-grid">
                                            <div className="form-group">
                                                <label className="form-label">Name</label>
                                                <input className="form-input" value={form.name} onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))} />
                                            </div>
                                            <div className="form-group">
                                                <label className="form-label">Order</label>
                                                <input className="form-input" type="number" value={form.priority} onChange={(e) => setForm((f) => ({ ...f, priority: e.target.value }))} />
                                                <div className="policy-helper-text">Lower numbers run first.</div>
                                            </div>
                                            <div className="form-group">
                                                <label className="form-label">Action</label>
                                                <AppSelect
                                                    value={form.action}
                                                    onValueChange={(value) => setForm((f) => ({ ...f, action: value }))}
                                                    options={[
                                                        { value: 'allow', label: 'Allow' },
                                                        { value: 'alert', label: 'Alert' },
                                                        { value: 'block', label: 'Block' },
                                                    ]}
                                                />
                                            </div>
                                            <div className="form-group policy-toggle-group">
                                                <label className="policy-checkbox-label">
                                                    <input type="checkbox" checked={!!form.enabled} onChange={(e) => setForm((f) => ({ ...f, enabled: e.target.checked }))} />
                                                    Policy enabled
                                                </label>
                                            </div>
                                        </div>

                                        <div className="form-group">
                                            <label className="form-label">Message Shown To User</label>
                                            <input className="form-input" placeholder="Example: This request is blocked by your organization" value={form.block_reason} onChange={(e) => setForm((f) => ({ ...f, block_reason: e.target.value }))} />
                                        </div>

                                        <div className="policy-section">
                                            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: 10, marginBottom: 10, flexWrap: 'wrap' }}>
                                                <h3 style={{ margin: 0, fontSize: 14, color: 'var(--text-primary)' }}>Conditions</h3>
                                                <button className="btn btn-sm" onClick={() => setForm((f) => ({ ...f, conditions: [...f.conditions, emptyCondition()] }))}>Add Condition</button>
                                            </div>
                                            {(form.conditions || []).map((c, idx) => (
                                                <ConditionRow
                                                    key={`${idx}-${c.field}-${c.operator}`}
                                                    condition={c}
                                                    onChange={(next) => setForm((f) => ({ ...f, conditions: f.conditions.map((row, i) => i === idx ? next : row) }))}
                                                    onRemove={() => setForm((f) => ({ ...f, conditions: f.conditions.length > 1 ? f.conditions.filter((_, i) => i !== idx) : [emptyCondition()] }))}
                                                    disabled={saving}
                                                />
                                            ))}
                                        </div>

                                    </>
                                )}
                            </div>

                            <div className="modal-actions policy-modal-actions">
                                <button className="btn" onClick={() => setShowModal(false)} style={{ border: 'none', background: 'transparent' }} disabled={saving}>Cancel</button>
                                <button className="btn btn-primary" onClick={saveRule} disabled={saving}>{saving ? 'Saving...' : 'Save Policy'}</button>
                            </div>
                        </div>
                    </div>
                )}
            </div>
        </>
    );
}




