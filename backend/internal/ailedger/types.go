// Package aileger implements the vendor-neutral AI usage, spend and governance
// core. Vendor names and API semantics belong only in connector adapters.
package ailedger

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type OriginKind string
type SourceEvidenceLevel string
type ReconciliationStatus string
type EvidenceLevel string

const (
	OriginEndpoint  OriginKind = "endpoint"
	OriginConnector OriginKind = "connector"
	OriginImport    OriginKind = "import"

	SourceObserved      SourceEvidenceLevel = "observed"
	SourceAuthoritative SourceEvidenceLevel = "authoritative"
	SourceEstimated     SourceEvidenceLevel = "estimated"

	ReconciliationMatched       ReconciliationStatus = "matched"
	ReconciliationUnmatched     ReconciliationStatus = "unmatched"
	ReconciliationNotApplicable ReconciliationStatus = "not_applicable"

	EvidenceObserved      EvidenceLevel = "observed"
	EvidenceAuthoritative EvidenceLevel = "authoritative"
	EvidenceReconciled    EvidenceLevel = "reconciled"
	EvidenceEstimated     EvidenceLevel = "estimated"
)

type Provenance struct {
	OriginKind           OriginKind           `json:"origin_kind"`
	SourceEvidenceLevel  SourceEvidenceLevel  `json:"source_evidence_level"`
	ReconciliationStatus ReconciliationStatus `json:"reconciliation_status"`
	EvidenceLevel        EvidenceLevel        `json:"evidence_level"`
	SourceIdentifier     string               `json:"source_identifier"`
	SourceKey            string               `json:"source_key"`
	ExternalReference    string               `json:"external_reference,omitempty"`
	FreshnessAt          time.Time            `json:"freshness_at"`
	Scope                string               `json:"scope"`
	ReportingPeriodStart *time.Time           `json:"reporting_period_start,omitempty"`
	ReportingPeriodEnd   *time.Time           `json:"reporting_period_end,omitempty"`
	VendorKey            string               `json:"vendor_key"`
	ProductKey           string               `json:"product_key"`
}

func (p *Provenance) NormalizeAndValidate() error {
	p.SourceIdentifier = strings.TrimSpace(p.SourceIdentifier)
	p.SourceKey = strings.TrimSpace(p.SourceKey)
	p.ExternalReference = strings.TrimSpace(p.ExternalReference)
	p.Scope = strings.TrimSpace(p.Scope)
	p.VendorKey = normalizeKey(p.VendorKey)
	p.ProductKey = normalizeKey(p.ProductKey)
	p.EvidenceLevel = EffectiveEvidence(p.OriginKind, p.SourceEvidenceLevel, p.ReconciliationStatus)
	var errs []error
	switch p.OriginKind {
	case OriginEndpoint, OriginConnector, OriginImport:
	default:
		errs = append(errs, fmt.Errorf("invalid origin_kind %q", p.OriginKind))
	}
	switch p.SourceEvidenceLevel {
	case SourceObserved, SourceAuthoritative, SourceEstimated:
	default:
		errs = append(errs, fmt.Errorf("invalid source_evidence_level %q", p.SourceEvidenceLevel))
	}
	switch p.ReconciliationStatus {
	case ReconciliationMatched, ReconciliationUnmatched, ReconciliationNotApplicable:
	default:
		errs = append(errs, fmt.Errorf("invalid reconciliation_status %q", p.ReconciliationStatus))
	}
	if p.SourceIdentifier == "" || p.SourceKey == "" || p.Scope == "" || p.FreshnessAt.IsZero() {
		errs = append(errs, errors.New("source identifier/key, scope and freshness are required"))
	}
	if p.VendorKey == "" || p.ProductKey == "" {
		errs = append(errs, errors.New("vendor_key and product_key are required"))
	}
	if p.ReportingPeriodStart != nil && p.ReportingPeriodEnd != nil && !p.ReportingPeriodEnd.After(*p.ReportingPeriodStart) {
		errs = append(errs, errors.New("reporting period end must be after start"))
	}
	if p.OriginKind == OriginEndpoint {
		if p.SourceEvidenceLevel != SourceObserved || p.EvidenceLevel != EvidenceObserved {
			errs = append(errs, errors.New("endpoint facts must remain observed"))
		}
	}
	if p.SourceEvidenceLevel == SourceEstimated && p.EvidenceLevel != EvidenceEstimated {
		errs = append(errs, errors.New("estimates must remain estimated"))
	}
	return errors.Join(errs...)
}

// EffectiveEvidence is the only evidence transition function in the ledger.
// Identity mapping never upgrades endpoint facts, and estimates never upgrade.
func EffectiveEvidence(origin OriginKind, source SourceEvidenceLevel, reconciliation ReconciliationStatus) EvidenceLevel {
	if origin == OriginEndpoint || source == SourceObserved {
		return EvidenceObserved
	}
	if source == SourceEstimated {
		return EvidenceEstimated
	}
	if source == SourceAuthoritative && reconciliation == ReconciliationMatched {
		return EvidenceReconciled
	}
	return EvidenceAuthoritative
}

type Capability string

const (
	CapabilityProducts  Capability = "products"
	CapabilityDirectory Capability = "directory"
	CapabilityLicenses  Capability = "licenses"
	CapabilityUsage     Capability = "usage"
	CapabilityCosts     Capability = "costs"
)

type FieldSchema struct {
	Type        string   `json:"type"`
	Label       string   `json:"label"`
	Required    bool     `json:"required,omitempty"`
	Secret      bool     `json:"secret,omitempty"`
	Description string   `json:"description,omitempty"`
	Options     []string `json:"options,omitempty"`
}

type ConnectorDescriptor struct {
	AdapterKey       string                 `json:"adapter_key"`
	DisplayName      string                 `json:"display_name"`
	Capabilities     []Capability           `json:"capabilities"`
	CredentialSchema map[string]FieldSchema `json:"credential_schema"`
	ConfigSchema     map[string]FieldSchema `json:"config_schema"`
	AllowedHosts     []string               `json:"allowed_hosts"`
}

type Credentials map[string]interface{}
type ConnectorConfig map[string]interface{}
type Checkpoint map[string]interface{}

type ConnectorAdapter interface {
	Descriptor() ConnectorDescriptor
	Validate(credentials Credentials, config ConnectorConfig) error
	Sync(context.Context, Checkpoint, FactSink) (Checkpoint, error)
}

type FactSink interface {
	UpsertProduct(ProductFact) error
	UpsertIdentity(ExternalIdentityFact) error
	UpsertLicense(LicenseFact) error
	UpsertMetric(MetricFact) error
	UpsertCost(CostFact) error
}

type ProductFact struct {
	Provenance
	DisplayName        string `json:"display_name"`
	FunctionalCategory string `json:"functional_category"`
	Approved           bool   `json:"approved"`
	HasContract        bool   `json:"has_contract"`
	HasBillingSource   bool   `json:"has_billing_source"`
}

type ExternalIdentityFact struct {
	Provenance
	IdentitySourceKey string `json:"identity_source_key"`
	IdentityKind      string `json:"identity_kind"`
	ExternalID        string `json:"external_id"`
	NormalizedEmail   string `json:"normalized_email,omitempty"`
	ProjectKey        string `json:"project_key,omitempty"`
	DirectoryUserID   string `json:"directory_user_id,omitempty"`
}

type LicenseFact struct {
	Provenance
	LicenseKey         string  `json:"license_key"`
	ExternalIdentityID string  `json:"external_identity_id,omitempty"`
	DirectoryUserID    string  `json:"directory_user_id,omitempty"`
	AssignedExternalID string  `json:"assigned_external_id,omitempty"`
	Status             string  `json:"status"`
	Paid               bool    `json:"paid"`
	UnitAmount         *string `json:"unit_amount,omitempty"`
	Currency           string  `json:"currency,omitempty"`
	BillingInterval    string  `json:"billing_interval,omitempty"`
}

type MetricFact struct {
	Provenance
	MetricKey          string `json:"metric_key"`
	Value              string `json:"value"`
	Unit               string `json:"unit"`
	IsActivity         bool   `json:"is_activity"`
	ExternalIdentityID string `json:"external_identity_id,omitempty"`
	ProjectKey         string `json:"project_key,omitempty"`
	ModelIdentifier    string `json:"model_identifier,omitempty"`
}

type CostFact struct {
	Provenance
	CostKey            string `json:"cost_key"`
	Amount             string `json:"amount"`
	Currency           string `json:"currency"`
	CostKind           string `json:"cost_kind"`
	ExternalIdentityID string `json:"external_identity_id,omitempty"`
	ProjectKey         string `json:"project_key,omitempty"`
	TeamKey            string `json:"team_key,omitempty"`
}

type connectorContextKey string

const (
	materialContextKey connectorContextKey = "connector_material"
	clientContextKey   connectorContextKey = "connector_http_client"
)

type ConnectorMaterial struct {
	Credentials Credentials
	Config      ConnectorConfig
	ConnectorID string
	OrgID       string
}

func WithConnectorMaterial(ctx context.Context, material ConnectorMaterial, client *http.Client) context.Context {
	ctx = context.WithValue(ctx, materialContextKey, material)
	return context.WithValue(ctx, clientContextKey, client)
}

func ConnectorMaterialFromContext(ctx context.Context) (ConnectorMaterial, bool) {
	value, ok := ctx.Value(materialContextKey).(ConnectorMaterial)
	return value, ok
}

func HTTPClientFromContext(ctx context.Context) *http.Client {
	client, _ := ctx.Value(clientContextKey).(*http.Client)
	if client == nil {
		return http.DefaultClient
	}
	return client
}

func normalizeKey(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func NormalizeEmail(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
