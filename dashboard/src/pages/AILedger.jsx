import { useEffect, useMemo, useState } from 'react';
import { Activity, Bot, CircleDollarSign, Database, FileCheck2, Link2, RefreshCw, Search, WalletCards } from 'lucide-react';
import { api } from '../api/client';
import { useToast } from '../context/ToastContext';

const TABS = [
    ['overview', 'Overview'],
    ['products', 'Products'],
    ['people', 'People'],
    ['findings', 'Findings'],
    ['sources', 'Data sources'],
];

function formatDate(value) {
    if (!value) return 'Not reported';
    const date = new Date(value);
    return Number.isNaN(date.getTime()) ? 'Not reported' : date.toLocaleString();
}

function formatSpend(rows = []) {
    if (!rows.length) return 'No billing source';
    return rows.map((row) => {
        const numeric = Number(row.authoritative_total);
        return new Intl.NumberFormat(undefined, {
            style: 'currency', currency: row.currency, maximumFractionDigits: 2,
        }).format(Number.isFinite(numeric) ? numeric : 0);
    }).join(' · ');
}

function EvidenceBadge({ level = 'unknown' }) {
    const colors = {
        observed: ['rgba(34,197,94,.14)', '#86efac'],
        authoritative: ['rgba(59,130,246,.14)', '#93c5fd'],
        reconciled: ['rgba(168,85,247,.14)', '#d8b4fe'],
        estimated: ['rgba(245,158,11,.14)', '#fcd34d'],
    };
    const [background, color] = colors[level] || ['rgba(115,115,115,.14)', '#a3a3a3'];
    return <span className="badge" style={{ background, color }}>{level}</span>;
}

function LedgerTable({ columns, rows, empty = 'No records yet' }) {
    if (!rows?.length) {
        return <div className="empty-state"><Database size={34} className="empty-state-icon" /><div className="empty-state-text">{empty}</div></div>;
    }
    return (
        <div style={{ overflowX: 'auto' }}>
            <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
                <thead><tr style={{ borderBottom: '1px solid #262626' }}>{columns.map((column) => <th key={column.key} style={{ textAlign: column.align || 'left', padding: '10px 12px', color: 'var(--text-muted)', fontWeight: 500 }}>{column.label}</th>)}</tr></thead>
                <tbody>{rows.map((row, index) => (
                    <tr key={row.id || row.finding_key || [row.vendor_key, row.product_key, index].join('-')} style={{ borderBottom: '1px solid #1f1f1f' }}>
                        {columns.map((column) => <td key={column.key} style={{ padding: '11px 12px', textAlign: column.align || 'left', verticalAlign: 'top' }}>{column.render ? column.render(row) : row[column.key]}</td>)}
                    </tr>
                ))}</tbody>
            </table>
        </div>
    );
}

function DynamicConnectorForm({ descriptors, onCreated }) {
    const toast = useToast();
    const [adapterKey, setAdapterKey] = useState(descriptors[0]?.adapter_key || '');
    const [displayName, setDisplayName] = useState('');
    const [values, setValues] = useState({});
    const [busy, setBusy] = useState(false);
    const descriptor = descriptors.find((item) => item.adapter_key === adapterKey);

    useEffect(() => {
        if (!adapterKey && descriptors[0]) setAdapterKey(descriptors[0].adapter_key);
    }, [adapterKey, descriptors]);

    if (!descriptors.length) return <div className="empty-state-sub">No connector adapters are registered.</div>;
    const setValue = (section, key, value) => setValues((current) => ({ ...current, [section + ':' + key]: value }));
    const renderFields = (section, schema = {}) => Object.entries(schema).map(([key, field]) => (
        <div className="form-group" key={section + ':' + key}>
            <label className="form-label">{field.label || key}{field.required ? ' *' : ''}</label>
            <input
                className="form-input"
                type={field.secret ? 'password' : field.type === 'number' ? 'number' : 'text'}
                value={values[section + ':' + key] || ''}
                placeholder={field.description || ''}
                onChange={(event) => setValue(section, key, event.target.value)}
            />
        </div>
    ));
    const submit = async (event) => {
        event.preventDefault();
        const credentials = {};
        const config = {};
        Object.keys(descriptor?.credential_schema || {}).forEach((key) => { credentials[key] = values['credentials:' + key] || ''; });
        Object.keys(descriptor?.config_schema || {}).forEach((key) => {
            const value = values['config:' + key] || '';
            if (value !== '') config[key] = value;
        });
        setBusy(true);
        try {
            await api.aiLedgerCreateConnector({ adapter_key: adapterKey, display_name: displayName, credentials, config });
            toast.success('Data source created. Sync it when credentials are ready.');
            setDisplayName('');
            setValues({});
            onCreated();
        } catch (error) {
            toast.error(error.message || 'Could not create data source');
        } finally {
            setBusy(false);
        }
    };
    return (
        <form onSubmit={submit}>
            <div className="form-group"><label className="form-label">Connector type</label><select className="form-select" value={adapterKey} onChange={(event) => { setAdapterKey(event.target.value); setValues({}); }}>{descriptors.map((item) => <option key={item.adapter_key} value={item.adapter_key}>{item.display_name}</option>)}</select></div>
            <div className="form-group"><label className="form-label">Source name</label><input className="form-input" value={displayName} onChange={(event) => setDisplayName(event.target.value)} placeholder={descriptor?.display_name} /></div>
            {renderFields('credentials', descriptor?.credential_schema)}
            {renderFields('config', descriptor?.config_schema)}
            <div style={{ color: 'var(--text-muted)', fontSize: 12, margin: '10px 0' }}>Capabilities: {(descriptor?.capabilities || []).join(', ') || 'None reported'}</div>
            <button className="btn btn-primary" type="submit" disabled={busy}>{busy ? 'Creating…' : 'Create source'}</button>
        </form>
    );
}

export default function AILedger() {
    const toast = useToast();
    const [activeTab, setActiveTab] = useState('overview');
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState('');
    const [summary, setSummary] = useState({});
    const [products, setProducts] = useState([]);
    const [people, setPeople] = useState([]);
    const [findings, setFindings] = useState([]);
    const [sources, setSources] = useState([]);
    const [connectorTypes, setConnectorTypes] = useState([]);
    const [connectors, setConnectors] = useState([]);
    const [search, setSearch] = useState('');
    const [syncing, setSyncing] = useState('');
    const [csvData, setCSVData] = useState('');
    const [csvResult, setCSVResult] = useState(null);

    const load = async () => {
        setLoading(true);
        setError('');
        try {
            const responses = await Promise.all([
                api.aiLedgerSummary(), api.aiLedgerProducts(), api.aiLedgerPeople(),
                api.aiLedgerFindings({ status: 'open' }), api.aiLedgerSources(), api.aiLedgerConnectorTypes(),
            ]);
            setSummary(responses[0].data || {});
            setProducts(responses[1].data || []);
            setPeople(responses[2].data || []);
            setFindings(responses[3].data || []);
            setSources(responses[4].data || []);
            setConnectorTypes(responses[5].data || []);
            const connectorSources = (responses[4].data || []).filter((source) => source.origin_kind === 'connector');
            const statuses = await Promise.all(connectorSources.map((source) => api.aiLedgerConnectorStatus(source.source_identifier).catch(() => null)));
            setConnectors(statuses.filter(Boolean));
        } catch (loadError) {
            setError(loadError.message || 'Could not load AI Ledger');
        } finally {
            setLoading(false);
        }
    };
    useEffect(() => { load(); }, []);

    const filteredProducts = useMemo(() => {
        const query = search.trim().toLowerCase();
        if (!query) return products;
        return products.filter((product) => [product.display_name, product.vendor_key, product.product_key, product.functional_category].some((value) => String(value || '').toLowerCase().includes(query)));
    }, [products, search]);

    const syncConnector = async (id) => {
        setSyncing(id);
        try {
            await api.aiLedgerSyncConnector(id);
            toast.success('Source sync completed and findings were refreshed.');
            await load();
        } catch (syncError) {
            toast.error(syncError.message || 'Source sync failed');
        } finally {
            setSyncing('');
        }
    };
    const validateCSV = async () => {
        try {
            const result = await api.aiLedgerCSVValidate(csvData);
            setCSVResult(result);
            if (result.valid) toast.success('CSV is valid (' + result.record_count + ' records).');
        } catch (csvError) {
            toast.error(csvError.message || 'CSV validation failed');
        }
    };
    const commitCSV = async () => {
        try {
            const result = await api.aiLedgerCSVCommit(csvData);
            toast.success('Imported ' + result.record_count + ' records.');
            setCSVData('');
            setCSVResult(null);
            await load();
        } catch (csvError) {
            toast.error(csvError.message || 'CSV import failed');
        }
    };

    if (loading) return <div className="loading-wrap"><div className="spinner" /></div>;

    const productColumns = [
        { key: 'product', label: 'Product', render: (row) => <div><strong style={{ color: 'var(--text-primary)' }}>{row.display_name}</strong><div style={{ color: 'var(--text-muted)', fontSize: 12 }}>{row.vendor_key} / {row.product_key}</div></div> },
        { key: 'approved', label: 'Governance', render: (row) => <span className="badge" style={{ color: row.approved ? '#86efac' : '#fcd34d' }}>{row.approved ? 'Approved' : 'Review'}</span> },
        { key: 'category', label: 'Category', render: (row) => row.functional_category || 'unknown' },
        { key: 'endpoint', label: 'Endpoint activity', align: 'right', render: (row) => <div><strong>{Number(row.endpoint_activity_count || 0).toLocaleString()}</strong><div style={{ marginTop: 5 }}><EvidenceBadge level="observed" /></div></div> },
        { key: 'connector', label: 'Connector activity', align: 'right', render: (row) => <div><strong>{Number(row.connector_activity_record_count || 0).toLocaleString()}</strong><div style={{ marginTop: 5 }}><EvidenceBadge level={row.connector_evidence_level || 'authoritative'} /></div><div style={{ color: 'var(--text-muted)', fontSize: 11, marginTop: 4 }}>source: {row.connector_source_evidence_level || 'authoritative'} · {row.connector_reconciliation_status || 'unmatched'}</div></div> },
        { key: 'seats', label: 'Paid seats', align: 'right', render: (row) => Number(row.paid_seat_count || 0).toLocaleString() },
        { key: 'cost', label: 'Source totals', align: 'right', render: (row) => Object.entries(row.cost_totals || {}).map(([currency, total]) => <div key={currency}>{currency} {total}</div>) },
        { key: 'freshness', label: 'Freshness', render: (row) => formatDate(row.freshness_at || row.last_endpoint_activity_at || row.last_connector_activity_at) },
    ];

    return (
        <div className="page-content">
            <div className="page-kicker">AI operations</div>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', gap: 16, flexWrap: 'wrap' }}>
                <div><h1 style={{ marginBottom: 6 }}>AI Ledger</h1><p className="page-subtitle">Reduce cost, improve AI adoption, and govern the products employees actually use. Every number links back to endpoint, connector, or import evidence.</p></div>
                <a className="btn" href={api.aiLedgerExportURL()}><FileCheck2 size={15} /> Export evidence</a>
            </div>
            <div className="page-summary-card" style={{ marginTop: 18 }}><div className="page-summary-label">Operating mode</div><strong>Insights only</strong><span>No license, procurement, vendor, or enforcement changes are made automatically.</span></div>
            <div style={{ display: 'flex', gap: 8, margin: '20px 0', flexWrap: 'wrap' }}>{TABS.map(([key, label]) => <button key={key} className={'btn' + (activeTab === key ? ' btn-primary' : '')} onClick={() => setActiveTab(key)}>{label}</button>)}</div>
            {error && <div className="chart-card" style={{ color: '#fca5a5' }}>{error}</div>}

            {activeTab === 'overview' && <>
                <div className="stats-grid">{[
                    ['Observed AI products', summary.endpoint_product_count || 0, 'Endpoint-confirmed inventory', <Bot size={16} />],
                    ['Authoritative spend', formatSpend(summary.spend_by_currency), 'Currencies remain separate', <CircleDollarSign size={16} />],
                    ['Paid seats', summary.paid_seat_count || 0, 'Assigned paid licenses', <WalletCards size={16} />],
                    ['Open recommendations', summary.open_finding_count || 0, 'Evidence-backed review queue', <Activity size={16} />],
                ].map(([label, value, sub, icon]) => <div className="stat-card" key={label}><div className="stat-card-header"><span className="stat-card-label">{label}</span><span className="stat-card-icon">{icon}</span></div><div className="stat-card-value" style={{ fontSize: typeof value === 'string' && value.length > 20 ? 20 : undefined }}>{value}</div><div className="stat-card-sub">{sub}</div></div>)}</div>
                <div className="chart-card" style={{ marginTop: 16 }}><div className="chart-card-title">What the evidence says</div><LedgerTable columns={productColumns.slice(0, 6)} rows={products.slice(0, 8)} empty="Endpoint observations will appear here after enrollment and telemetry delivery." /></div>
            </>}

            {activeTab === 'products' && <div className="chart-card"><div style={{ display: 'flex', justifyContent: 'space-between', gap: 12, marginBottom: 14, flexWrap: 'wrap' }}><div className="chart-card-title">Product inventory</div><div style={{ position: 'relative', minWidth: 260 }}><Search size={15} style={{ position: 'absolute', left: 10, top: 10, color: 'var(--text-muted)' }} /><input className="form-input" style={{ paddingLeft: 32 }} value={search} onChange={(event) => setSearch(event.target.value)} placeholder="Search products or categories" /></div></div><LedgerTable columns={productColumns} rows={filteredProducts} empty="No AI products have been observed or reported." /></div>}

            {activeTab === 'people' && <div className="chart-card"><div className="chart-card-title">People reconciliation</div><p className="page-subtitle">Endpoint activity is attributed only through explicit device assignments. Connector activity remains separate.</p><LedgerTable rows={people} empty="Connect a directory source before reconciling people." columns={[
                { key: 'person', label: 'Directory user', render: (row) => <div><strong>{row.display_name || row.normalized_email || row.external_user_id}</strong><div style={{ color: 'var(--text-muted)', fontSize: 12 }}>{row.normalized_email || 'No normalized email'}</div></div> },
                { key: 'status', label: 'Status' },
                { key: 'endpoint_activity_count', label: 'Endpoint activity', align: 'right', render: (row) => <div>{Number(row.endpoint_activity_count || 0).toLocaleString()} <EvidenceBadge level="observed" /></div> },
                { key: 'connector_activity_record_count', label: 'Connector activity', align: 'right', render: (row) => <div>{Number(row.connector_activity_record_count || 0).toLocaleString()} <EvidenceBadge level={row.connector_evidence_level || 'authoritative'} /><div style={{ color: 'var(--text-muted)', fontSize: 11, marginTop: 4 }}>source: {row.connector_source_evidence_level || 'authoritative'} · {row.connector_reconciliation_status || 'unmatched'}</div></div> },
                { key: 'paid_seat_count', label: 'Paid seats', align: 'right' },
                { key: 'freshness_at', label: 'Directory freshness', render: (row) => formatDate(row.freshness_at) },
            ]} /></div>}

            {activeTab === 'findings' && <div className="chart-card"><div className="chart-card-title">Evidence-backed recommendations</div><LedgerTable rows={findings} empty="No open recommendations." columns={[
                { key: 'finding', label: 'Finding', render: (row) => <div><strong>{row.title}</strong><div style={{ color: 'var(--text-muted)', marginTop: 4 }}>{row.summary}</div></div> },
                { key: 'type', label: 'Type', render: (row) => String(row.finding_type || '').replaceAll('_', ' ') },
                { key: 'recommendation', label: 'Recommended review', render: (row) => row.recommendation },
                { key: 'evidence', label: 'Evidence', render: (row) => <div><span>{row.evidence_source_keys?.length || 0} source key(s)</span><div style={{ marginTop: 5 }}><EvidenceBadge level={row.finding_type === 'shadow_ai' ? 'observed' : 'authoritative'} /></div></div> },
                { key: 'last_observed_at', label: 'As of', render: (row) => formatDate(row.last_observed_at) },
            ]} /></div>}

            {activeTab === 'sources' && <div style={{ display: 'grid', gridTemplateColumns: 'minmax(0, 1.5fr) minmax(300px, .8fr)', gap: 16, alignItems: 'start' }}><div>
                <div className="chart-card"><div className="chart-card-title">Source health and freshness</div><LedgerTable rows={sources} empty="No endpoint, connector, or import source has reported yet." columns={[
                    { key: 'source', label: 'Source', render: (row) => <div><strong>{row.source_identifier}</strong><div style={{ color: 'var(--text-muted)', fontSize: 12 }}>{row.origin_kind} · {row.scope}</div></div> },
                    { key: 'status', label: 'Health' },
                    { key: 'records', label: 'Records', align: 'right', render: (row) => Number(row.record_count || 0).toLocaleString() },
                    { key: 'evidence', label: 'Evidence', render: (row) => <div><EvidenceBadge level={row.evidence_level} /><div style={{ color: 'var(--text-muted)', fontSize: 11, marginTop: 4 }}>source: {row.source_evidence_level} · {row.reconciliation_status}</div></div> },
                    { key: 'freshness_at', label: 'Freshness', render: (row) => formatDate(row.freshness_at) },
                ]} /></div>
                <div className="chart-card" style={{ marginTop: 16 }}><div className="chart-card-title">Connector sync</div><LedgerTable rows={connectors} empty="Create a connector to begin importing authoritative data." columns={[
                    { key: 'name', label: 'Connector', render: (row) => <div><strong>{row.display_name}</strong><div style={{ color: 'var(--text-muted)', fontSize: 12 }}>{row.adapter_key}</div></div> },
                    { key: 'status', label: 'Status' },
                    { key: 'freshness_at', label: 'Freshness', render: (row) => formatDate(row.freshness_at) },
                    { key: 'action', label: '', align: 'right', render: (row) => <button className="btn btn-sm" disabled={syncing === row.id} onClick={() => syncConnector(row.id)}><RefreshCw size={14} /> {syncing === row.id ? 'Syncing…' : 'Sync'}</button> },
                ]} /></div>
                <div className="chart-card" style={{ marginTop: 16 }}><div className="chart-card-title">Versioned CSV fallback</div><textarea className="form-input" style={{ minHeight: 140, resize: 'vertical', fontFamily: 'monospace' }} value={csvData} onChange={(event) => { setCSVData(event.target.value); setCSVResult(null); }} placeholder="Paste themisto_ai_ledger_csv_v1 data" />{csvResult && <div style={{ color: csvResult.valid ? '#86efac' : '#fca5a5', margin: '10px 0' }}>{csvResult.valid ? csvResult.record_count + ' records validated' : (csvResult.errors || []).join(', ')}</div>}<div style={{ display: 'flex', gap: 8, marginTop: 10 }}><button className="btn" onClick={validateCSV} disabled={!csvData}>Validate</button><button className="btn btn-primary" onClick={commitCSV} disabled={!csvResult?.valid}>Commit import</button></div></div>
            </div><div className="chart-card"><div className="chart-card-title"><Link2 size={16} /> Add data source</div><DynamicConnectorForm descriptors={connectorTypes} onCreated={load} /></div></div>}
        </div>
    );
}
