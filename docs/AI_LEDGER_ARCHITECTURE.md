# Themisto AI Ledger architecture

Themisto AI Ledger is an endpoint-grounded AI usage, spend, productivity, and governance product. It is not a generic SaaS inventory, procurement, CRM, HR, or contract-lifecycle product. The MVP produces evidence-backed insights only; it never removes a license, changes a vendor, alters procurement, or changes enforcement automatically.

## Trust and evidence model

`ai.activity.v1` is the canonical observation of employee AI use. It is emitted only by endpoints and always remains `origin_kind=endpoint`, `source_evidence_level=observed`, and effective `evidence_level=observed`. Identity assignment can associate an event with an explicitly mapped directory user, but cannot upgrade its evidence.

Connectors and imports provide authoritative commercial and administrative facts. An unmatched authoritative fact remains `authoritative`; a deterministic external-ID, project, account, or exact unambiguous normalized-email match makes it `reconciled`. Estimates always remain `estimated`.

Connector metrics and endpoint activity are stored and presented separately. Cost-only rows are never activity. Connector activity can suppress an inactive-seat recommendation, but without endpoint evidence it also creates a connector-coverage-gap finding. Shadow AI and primary seat utilization use endpoint observations exclusively. Provider organization, tenant, and project totals remain at that scope; the ledger does not invent per-user attribution.

Every persisted fact retains origin, source evidence, reconciliation status, effective evidence, stable source identifier and key, optional external reference, freshness, scope, reporting period, vendor key, and product key. Decimal source amounts and currencies remain separate so mixed currencies, credits, and negative adjustments are not silently converted or discarded.

## Connector boundary

The compiled registry accepts implementations of:

```go
type ConnectorAdapter interface {
    Descriptor() ConnectorDescriptor
    Validate(credentials Credentials, config ConnectorConfig) error
    Sync(context.Context, Checkpoint, FactSink) (Checkpoint, error)
}
```

`ConnectorDescriptor` supplies its key, label, capabilities, credential/config schemas, and allowed outbound hosts. `FactSink` exposes only product, identity, license, metric, and cost upserts. The manager performs registry lookup, organization-scoped encrypted credential loading, descriptor validation, HTTPS host enforcement, checkpoints, bounded retries/backoff, health/freshness, idempotent persistence, reconciliation, and findings.

Pagination tokens, API paths, credentials, billing interpretation, SKU handling, raw metric mapping, and identity extraction stay inside adapter packages. The `acme_ai` contract test proves that adding an adapter requires registry registration only. Current built-ins expose OpenAI, Anthropic, Google Workspace, Google Cloud Billing, normalized CSV v1, and the fake adapter. Provider adapters also accept normalized facts for deterministic offline and correction workflows.

## Identity and findings

Directory users have an explicit `identity_source_key` and stable external user ID. Connector identities match by external ID first, then exact normalized email only when unambiguous. Renames preserve the external-ID match. Suspended/deleted users, shared devices, multiple accounts, and project-only facts remain explicit. A device is associated with a directory user only by an MDM or administrator assignment with an effective interval; local operating-system usernames are not identities.

The deterministic findings engine implements inactive paid seat (30 days), orphaned seat, endpoint-only shadow AI, endpoint-only unpriced AI, overlapping active paid seats in the same non-unknown functional category, spend spike (seven-day average over 150% of the previous 28 days and at least $50/day higher), unattributed authoritative spend, and connector activity coverage gap.

## API and dashboard

Administrator/owner-only routes under `/api/v1/ai-ledger` expose summary, products, people, findings, sources, connector descriptors, connector creation/status/sync, directory users, explicit device-user assignments, CSV validation/commit, and privacy-safe export. The dashboard renders descriptor-driven connector forms and contains no provider-name branching. It separates endpoint and connector activity, evidence, freshness, scope, reconciliation, and currency totals, and uses insights-only language.
