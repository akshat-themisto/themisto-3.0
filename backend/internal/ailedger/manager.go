package ailedger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type ConnectorRecord struct {
	ID                   string          `json:"id"`
	OrgID                string          `json:"org_id"`
	AdapterKey           string          `json:"adapter_key"`
	DisplayName          string          `json:"display_name"`
	EncryptedCredentials []byte          `json:"-"`
	Config               ConnectorConfig `json:"config"`
	Checkpoint           Checkpoint      `json:"checkpoint"`
	Status               string          `json:"status"`
	ConsecutiveFailures  int             `json:"consecutive_failures"`
	NextRetryAt          *time.Time      `json:"next_retry_at,omitempty"`
	LastAttemptAt        *time.Time      `json:"last_attempt_at,omitempty"`
	LastSuccessAt        *time.Time      `json:"last_success_at,omitempty"`
	FreshnessAt          *time.Time      `json:"freshness_at,omitempty"`
	LastError            string          `json:"last_error,omitempty"`
}

type ConnectorRepository interface {
	CreateConnector(context.Context, ConnectorRecord) (*ConnectorRecord, error)
	GetConnector(context.Context, string, string) (*ConnectorRecord, error)
	ListConnectors(context.Context, string) ([]ConnectorRecord, error)
	MarkConnectorSyncing(context.Context, string, string, time.Time) error
	MarkConnectorSuccess(context.Context, string, string, Checkpoint, time.Time) error
	MarkConnectorFailure(context.Context, string, string, int, time.Time, string) error
	FactSink(orgID, sourceIdentifier string) FactSink
	Reconcile(context.Context, string) error
	RefreshFindings(context.Context, string, time.Time) error
}

type Manager struct {
	registry *Registry
	repo     ConnectorRepository
	cipher   *CredentialCipher
}

func NewManager(registry *Registry, repo ConnectorRepository, cipher *CredentialCipher) *Manager {
	return &Manager{registry: registry, repo: repo, cipher: cipher}
}

func (m *Manager) ConnectorTypes() []ConnectorDescriptor {
	return m.registry.Descriptors()
}

func (m *Manager) CreateConnector(ctx context.Context, orgID, adapterKey, displayName string, credentials Credentials, config ConnectorConfig) (*ConnectorRecord, error) {
	adapter, ok := m.registry.Lookup(adapterKey)
	if !ok {
		return nil, fmt.Errorf("unknown connector adapter %q", adapterKey)
	}
	if err := adapter.Validate(credentials, config); err != nil {
		return nil, fmt.Errorf("connector validation: %w", err)
	}
	raw, err := json.Marshal(credentials)
	if err != nil {
		return nil, fmt.Errorf("marshal credentials: %w", err)
	}
	encrypted, err := m.cipher.Seal(raw, []byte(orgID))
	if err != nil {
		return nil, err
	}
	descriptor := adapter.Descriptor()
	if strings.TrimSpace(displayName) == "" {
		displayName = descriptor.DisplayName
	}
	return m.repo.CreateConnector(ctx, ConnectorRecord{
		OrgID: orgID, AdapterKey: descriptor.AdapterKey, DisplayName: displayName,
		EncryptedCredentials: encrypted, Config: config, Checkpoint: Checkpoint{},
		Status: "pending",
	})
}

func (m *Manager) SyncConnector(ctx context.Context, orgID, connectorID string) error {
	record, err := m.repo.GetConnector(ctx, orgID, connectorID)
	if err != nil {
		return err
	}
	if record == nil {
		return fmt.Errorf("connector not found")
	}
	adapter, ok := m.registry.Lookup(record.AdapterKey)
	if !ok {
		return fmt.Errorf("connector adapter %q is not registered", record.AdapterKey)
	}
	raw, err := m.cipher.Open(record.EncryptedCredentials, []byte(orgID))
	if err != nil {
		return fmt.Errorf("decrypt connector credentials: %w", err)
	}
	var credentials Credentials
	if err := json.Unmarshal(raw, &credentials); err != nil {
		return fmt.Errorf("decode connector credentials: %w", err)
	}
	if err := adapter.Validate(credentials, record.Config); err != nil {
		return fmt.Errorf("connector validation: %w", err)
	}
	now := time.Now().UTC()
	if err := m.repo.MarkConnectorSyncing(ctx, orgID, connectorID, now); err != nil {
		return err
	}
	client := newRestrictedHTTPClient(adapter.Descriptor().AllowedHosts)
	syncCtx := WithConnectorMaterial(ctx, ConnectorMaterial{
		Credentials: credentials, Config: record.Config, ConnectorID: connectorID, OrgID: orgID,
	}, client)
	sink := &connectorFactSink{delegate: m.repo.FactSink(orgID, connectorID), connectorID: connectorID, now: now}
	var checkpoint Checkpoint
	var syncErr error
	for attempt := 0; attempt < 3; attempt++ {
		checkpoint, syncErr = adapter.Sync(syncCtx, record.Checkpoint, sink)
		if syncErr == nil {
			break
		}
		if !retryableConnectorError(syncErr) || attempt == 2 {
			break
		}
		delay := time.Duration(1<<attempt) * time.Second
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			syncErr = ctx.Err()
			attempt = 3
		case <-timer.C:
		}
	}
	if syncErr != nil {
		failures := record.ConsecutiveFailures + 1
		nextRetry := now.Add(backoffForFailures(failures))
		_ = m.repo.MarkConnectorFailure(ctx, orgID, connectorID, failures, nextRetry, boundedError(syncErr))
		return syncErr
	}
	if checkpoint == nil {
		checkpoint = Checkpoint{}
	}
	if err := m.repo.MarkConnectorSuccess(ctx, orgID, connectorID, checkpoint, time.Now().UTC()); err != nil {
		return err
	}
	if err := m.repo.Reconcile(ctx, orgID); err != nil {
		return fmt.Errorf("reconcile connector facts: %w", err)
	}
	if err := m.repo.RefreshFindings(ctx, orgID, time.Now().UTC()); err != nil {
		return fmt.Errorf("refresh findings: %w", err)
	}
	return nil
}

func (m *Manager) ListConnectors(ctx context.Context, orgID string) ([]ConnectorRecord, error) {
	return m.repo.ListConnectors(ctx, orgID)
}

func (m *Manager) GetConnector(ctx context.Context, orgID, connectorID string) (*ConnectorRecord, error) {
	return m.repo.GetConnector(ctx, orgID, connectorID)
}

type connectorFactSink struct {
	delegate    FactSink
	connectorID string
	now         time.Time
}

// NewImportFactSink applies import provenance while reusing the same
// idempotent vendor-neutral persistence contract as connectors.
func NewImportFactSink(delegate FactSink, sourceIdentifier string, now time.Time) FactSink {
	return &importFactSink{delegate: delegate, sourceIdentifier: sourceIdentifier, now: now}
}

type importFactSink struct {
	delegate         FactSink
	sourceIdentifier string
	now              time.Time
}

func (s *importFactSink) normalize(provenance *Provenance) error {
	provenance.OriginKind = OriginImport
	if provenance.SourceEvidenceLevel == "" {
		provenance.SourceEvidenceLevel = SourceAuthoritative
	}
	if provenance.ReconciliationStatus == "" {
		provenance.ReconciliationStatus = ReconciliationUnmatched
	}
	provenance.SourceIdentifier = s.sourceIdentifier
	if provenance.FreshnessAt.IsZero() {
		provenance.FreshnessAt = s.now
	}
	return provenance.NormalizeAndValidate()
}
func (s *importFactSink) UpsertProduct(fact ProductFact) error {
	if err := s.normalize(&fact.Provenance); err != nil {
		return err
	}
	return s.delegate.UpsertProduct(fact)
}
func (s *importFactSink) UpsertIdentity(fact ExternalIdentityFact) error {
	if err := s.normalize(&fact.Provenance); err != nil {
		return err
	}
	return s.delegate.UpsertIdentity(fact)
}
func (s *importFactSink) UpsertLicense(fact LicenseFact) error {
	if err := s.normalize(&fact.Provenance); err != nil {
		return err
	}
	return s.delegate.UpsertLicense(fact)
}
func (s *importFactSink) UpsertMetric(fact MetricFact) error {
	if err := s.normalize(&fact.Provenance); err != nil {
		return err
	}
	return s.delegate.UpsertMetric(fact)
}
func (s *importFactSink) UpsertCost(fact CostFact) error {
	if err := s.normalize(&fact.Provenance); err != nil {
		return err
	}
	return s.delegate.UpsertCost(fact)
}

func (s *connectorFactSink) normalize(provenance *Provenance) error {
	provenance.OriginKind = OriginConnector
	if provenance.SourceEvidenceLevel == "" {
		provenance.SourceEvidenceLevel = SourceAuthoritative
	}
	if provenance.ReconciliationStatus == "" {
		provenance.ReconciliationStatus = ReconciliationUnmatched
	}
	provenance.SourceIdentifier = s.connectorID
	if provenance.FreshnessAt.IsZero() {
		provenance.FreshnessAt = s.now
	}
	return provenance.NormalizeAndValidate()
}

func (s *connectorFactSink) UpsertProduct(fact ProductFact) error {
	if err := s.normalize(&fact.Provenance); err != nil {
		return err
	}
	return s.delegate.UpsertProduct(fact)
}
func (s *connectorFactSink) UpsertIdentity(fact ExternalIdentityFact) error {
	if err := s.normalize(&fact.Provenance); err != nil {
		return err
	}
	return s.delegate.UpsertIdentity(fact)
}
func (s *connectorFactSink) UpsertLicense(fact LicenseFact) error {
	if err := s.normalize(&fact.Provenance); err != nil {
		return err
	}
	return s.delegate.UpsertLicense(fact)
}
func (s *connectorFactSink) UpsertMetric(fact MetricFact) error {
	if err := s.normalize(&fact.Provenance); err != nil {
		return err
	}
	return s.delegate.UpsertMetric(fact)
}
func (s *connectorFactSink) UpsertCost(fact CostFact) error {
	if err := s.normalize(&fact.Provenance); err != nil {
		return err
	}
	return s.delegate.UpsertCost(fact)
}

type restrictedTransport struct {
	allowed map[string]struct{}
	base    *http.Transport
}

func newRestrictedHTTPClient(hosts []string) *http.Client {
	allowed := make(map[string]struct{}, len(hosts))
	for _, host := range hosts {
		if host = strings.ToLower(strings.TrimSpace(host)); host != "" {
			allowed[host] = struct{}{}
		}
	}
	transport := &restrictedTransport{allowed: allowed, base: http.DefaultTransport.(*http.Transport).Clone()}
	client := &http.Client{Transport: transport, Timeout: 45 * time.Second}
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return fmt.Errorf("too many redirects")
		}
		return transport.validateURL(req.URL)
	}
	return client
}

func (t *restrictedTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if err := t.validateURL(request.URL); err != nil {
		return nil, err
	}
	return t.base.RoundTrip(request)
}

func (t *restrictedTransport) validateURL(target *url.URL) error {
	if target == nil || target.Scheme != "https" {
		return fmt.Errorf("connector outbound requests must use HTTPS")
	}
	host := strings.ToLower(target.Hostname())
	for allowed := range t.allowed {
		if strings.HasPrefix(allowed, "*.") {
			suffix := strings.TrimPrefix(allowed, "*")
			if strings.HasSuffix(host, suffix) {
				return nil
			}
		}
		if host == allowed {
			return nil
		}
	}
	return fmt.Errorf("connector outbound host %q is not allowed", host)
}

func retryableConnectorError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	var statusErr interface{ StatusCode() int }
	if errors.As(err, &statusErr) {
		code := statusErr.StatusCode()
		return code == http.StatusTooManyRequests || code >= 500
	}
	return false
}

func backoffForFailures(failures int) time.Duration {
	if failures < 1 {
		failures = 1
	}
	if failures > 8 {
		failures = 8
	}
	return time.Duration(1<<uint(failures-1)) * time.Minute
}

func boundedError(err error) string {
	value := err.Error()
	if len(value) > 1000 {
		value = value[:1000]
	}
	return value
}
