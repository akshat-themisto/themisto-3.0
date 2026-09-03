package ailedger

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
)

type SQLStore struct {
	db *sql.DB
}

func NewSQLStore(db *sql.DB) *SQLStore {
	return &SQLStore{db: db}
}

func (s *SQLStore) CreateConnector(ctx context.Context, record ConnectorRecord) (*ConnectorRecord, error) {
	config, err := json.Marshal(record.Config)
	if err != nil {
		return nil, err
	}
	checkpoint, err := json.Marshal(record.Checkpoint)
	if err != nil {
		return nil, err
	}
	row := s.db.QueryRowContext(ctx, `
		INSERT INTO ai_ledger_connectors (
			org_id, adapter_key, display_name, encrypted_credentials, config, checkpoint, status
		) VALUES ($1::uuid, $2, $3, $4, $5::jsonb, $6::jsonb, $7)
		RETURNING id::text, created_at`,
		record.OrgID, normalizeKey(record.AdapterKey), strings.TrimSpace(record.DisplayName),
		record.EncryptedCredentials, string(config), string(checkpoint), record.Status,
	)
	var createdAt time.Time
	if err := row.Scan(&record.ID, &createdAt); err != nil {
		return nil, err
	}
	return &record, nil
}

func (s *SQLStore) GetConnector(ctx context.Context, orgID, connectorID string) (*ConnectorRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id::text, org_id::text, adapter_key, display_name, encrypted_credentials,
		       config, checkpoint, status, consecutive_failures, next_retry_at,
		       last_attempt_at, last_success_at, freshness_at, COALESCE(last_error, '')
		FROM ai_ledger_connectors
		WHERE org_id = $1::uuid AND id = $2::uuid`, orgID, connectorID)
	record, err := scanConnector(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return record, err
}

func (s *SQLStore) ListConnectors(ctx context.Context, orgID string) ([]ConnectorRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id::text, org_id::text, adapter_key, display_name, encrypted_credentials,
		       config, checkpoint, status, consecutive_failures, next_retry_at,
		       last_attempt_at, last_success_at, freshness_at, COALESCE(last_error, '')
		FROM ai_ledger_connectors
		WHERE org_id = $1::uuid
		ORDER BY created_at DESC`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ConnectorRecord, 0)
	for rows.Next() {
		record, err := scanConnector(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *record)
	}
	return out, rows.Err()
}

type rowScanner interface {
	Scan(...interface{}) error
}

func scanConnector(row rowScanner) (*ConnectorRecord, error) {
	var record ConnectorRecord
	var configRaw, checkpointRaw []byte
	if err := row.Scan(
		&record.ID, &record.OrgID, &record.AdapterKey, &record.DisplayName,
		&record.EncryptedCredentials, &configRaw, &checkpointRaw, &record.Status,
		&record.ConsecutiveFailures, &record.NextRetryAt, &record.LastAttemptAt,
		&record.LastSuccessAt, &record.FreshnessAt, &record.LastError,
	); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(configRaw, &record.Config); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(checkpointRaw, &record.Checkpoint); err != nil {
		return nil, err
	}
	return &record, nil
}

func (s *SQLStore) MarkConnectorSyncing(ctx context.Context, orgID, connectorID string, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE ai_ledger_connectors
		SET status = 'syncing', last_attempt_at = $3, last_error = NULL, updated_at = now()
		WHERE org_id = $1::uuid AND id = $2::uuid`, orgID, connectorID, at)
	return err
}

func (s *SQLStore) MarkConnectorSuccess(ctx context.Context, orgID, connectorID string, checkpoint Checkpoint, at time.Time) error {
	raw, err := json.Marshal(checkpoint)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		UPDATE ai_ledger_connectors
		SET status = 'healthy', checkpoint = $3::jsonb, consecutive_failures = 0,
		    next_retry_at = NULL, last_success_at = $4, freshness_at = $4,
		    last_error = NULL, updated_at = now()
		WHERE org_id = $1::uuid AND id = $2::uuid`, orgID, connectorID, string(raw), at)
	return err
}

func (s *SQLStore) MarkConnectorFailure(ctx context.Context, orgID, connectorID string, failures int, nextRetry time.Time, message string) error {
	status := "degraded"
	if failures >= 3 {
		status = "error"
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE ai_ledger_connectors
		SET status = $3, consecutive_failures = $4, next_retry_at = $5,
		    last_error = $6, updated_at = now()
		WHERE org_id = $1::uuid AND id = $2::uuid`,
		orgID, connectorID, status, failures, nextRetry, message)
	return err
}

func (s *SQLStore) FactSink(orgID, sourceIdentifier string) FactSink {
	return &sqlFactSink{db: s.db, orgID: orgID, sourceIdentifier: sourceIdentifier}
}

type sqlFactSink struct {
	db               *sql.DB
	orgID            string
	sourceIdentifier string
}

func (s *sqlFactSink) UpsertProduct(fact ProductFact) error {
	p := fact.Provenance
	if err := p.NormalizeAndValidate(); err != nil {
		return err
	}
	_, err := s.db.ExecContext(context.Background(), `
		INSERT INTO ai_ledger_products (
			org_id, vendor_key, product_key, display_name, functional_category,
			approved, has_contract, has_billing_source, origin_kind,
			source_evidence_level, reconciliation_status, evidence_level,
			source_identifier, source_key, external_reference, freshness_at,
			scope, reporting_period_start, reporting_period_end
		) VALUES (
			$1::uuid,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,
			NULLIF($15,''),$16,$17,$18,$19
		)
		ON CONFLICT (org_id, vendor_key, product_key, source_identifier, source_key)
		DO UPDATE SET
			display_name = EXCLUDED.display_name,
			functional_category = EXCLUDED.functional_category,
			approved = ai_ledger_products.approved OR EXCLUDED.approved,
			has_contract = ai_ledger_products.has_contract OR EXCLUDED.has_contract,
			has_billing_source = ai_ledger_products.has_billing_source OR EXCLUDED.has_billing_source,
			source_evidence_level = EXCLUDED.source_evidence_level,
			reconciliation_status = EXCLUDED.reconciliation_status,
			evidence_level = EXCLUDED.evidence_level,
			external_reference = EXCLUDED.external_reference,
			freshness_at = EXCLUDED.freshness_at,
			scope = EXCLUDED.scope,
			reporting_period_start = EXCLUDED.reporting_period_start,
			reporting_period_end = EXCLUDED.reporting_period_end,
			updated_at = now()`,
		s.orgID, p.VendorKey, p.ProductKey, strings.TrimSpace(fact.DisplayName),
		defaultString(normalizeKey(fact.FunctionalCategory), "unknown"), fact.Approved,
		fact.HasContract, fact.HasBillingSource, p.OriginKind, p.SourceEvidenceLevel,
		p.ReconciliationStatus, p.EvidenceLevel, s.sourceIdentifier, p.SourceKey,
		p.ExternalReference, p.FreshnessAt, p.Scope, p.ReportingPeriodStart, p.ReportingPeriodEnd,
	)
	return err
}

func (s *sqlFactSink) UpsertIdentity(fact ExternalIdentityFact) error {
	p := fact.Provenance
	if err := p.NormalizeAndValidate(); err != nil {
		return err
	}
	fact.IdentitySourceKey = normalizeKey(fact.IdentitySourceKey)
	fact.IdentityKind = normalizeKey(fact.IdentityKind)
	fact.ExternalID = strings.TrimSpace(fact.ExternalID)
	fact.NormalizedEmail = NormalizeEmail(fact.NormalizedEmail)
	if fact.IdentitySourceKey == "" || fact.ExternalID == "" {
		return fmt.Errorf("identity source key and external id are required")
	}
	if fact.IdentityKind == "directory_user" {
		status := "active"
		if fact.ProjectKey == "suspended" || fact.ProjectKey == "deleted" {
			status = fact.ProjectKey
		}
		_, err := s.db.ExecContext(context.Background(), `
			INSERT INTO ai_ledger_directory_users (
				org_id, identity_source_key, external_user_id, normalized_email,
				status, origin_kind, source_evidence_level, reconciliation_status,
				evidence_level, source_identifier, source_key, external_reference,
				freshness_at, scope, reporting_period_start, reporting_period_end
			) VALUES (
				$1::uuid,$2,$3,NULLIF($4,''),$5,$6,$7,'not_applicable',$8,
				$9,$10,NULLIF($11,''),$12,$13,$14,$15
			)
			ON CONFLICT (org_id, identity_source_key, external_user_id)
			DO UPDATE SET normalized_email = EXCLUDED.normalized_email,
				status = EXCLUDED.status, source_evidence_level = EXCLUDED.source_evidence_level,
				evidence_level = EXCLUDED.evidence_level, source_identifier = EXCLUDED.source_identifier,
				source_key = EXCLUDED.source_key, external_reference = EXCLUDED.external_reference,
				freshness_at = EXCLUDED.freshness_at, updated_at = now()`,
			s.orgID, fact.IdentitySourceKey, fact.ExternalID, fact.NormalizedEmail, status,
			p.OriginKind, p.SourceEvidenceLevel, p.EvidenceLevel, s.sourceIdentifier,
			p.SourceKey, p.ExternalReference, p.FreshnessAt, p.Scope,
			p.ReportingPeriodStart, p.ReportingPeriodEnd,
		)
		if err != nil {
			return err
		}
	}
	_, err := s.db.ExecContext(context.Background(), `
		INSERT INTO ai_ledger_external_identities (
			org_id, identity_source_key, identity_kind, external_id, normalized_email,
			directory_user_id, project_key, reconciliation_status, origin_kind,
			source_evidence_level, evidence_level, source_identifier, source_key,
			external_reference, freshness_at, scope, reporting_period_start,
			reporting_period_end
		) VALUES (
			$1::uuid,$2,$3,$4,NULLIF($5,''),
			CASE WHEN $3 = 'directory_user' THEN (
				SELECT id FROM ai_ledger_directory_users
				WHERE org_id = $1::uuid AND identity_source_key = $2 AND external_user_id = $4
			) ELSE NULL END,
			NULLIF($6,''),$7,$8,$9,$10,$11,$12,NULLIF($13,''),$14,$15,$16,$17
		)
		ON CONFLICT (org_id, identity_source_key, identity_kind, external_id)
		DO UPDATE SET normalized_email = EXCLUDED.normalized_email,
			project_key = EXCLUDED.project_key,
			reconciliation_status = EXCLUDED.reconciliation_status,
			source_evidence_level = EXCLUDED.source_evidence_level,
			evidence_level = EXCLUDED.evidence_level,
			source_identifier = EXCLUDED.source_identifier,
			source_key = EXCLUDED.source_key,
			external_reference = EXCLUDED.external_reference,
			freshness_at = EXCLUDED.freshness_at,
			updated_at = now()`,
		s.orgID, fact.IdentitySourceKey, fact.IdentityKind, fact.ExternalID,
		fact.NormalizedEmail, fact.ProjectKey, p.ReconciliationStatus, p.OriginKind,
		p.SourceEvidenceLevel, p.EvidenceLevel, s.sourceIdentifier, p.SourceKey,
		p.ExternalReference, p.FreshnessAt, p.Scope, p.ReportingPeriodStart, p.ReportingPeriodEnd,
	)
	return err
}

func (s *sqlFactSink) UpsertLicense(fact LicenseFact) error {
	p := fact.Provenance
	if err := p.NormalizeAndValidate(); err != nil {
		return err
	}
	if strings.TrimSpace(fact.LicenseKey) == "" {
		return fmt.Errorf("license_key is required")
	}
	status := defaultString(normalizeKey(fact.Status), "assigned")
	_, err := s.db.ExecContext(context.Background(), `
		INSERT INTO ai_ledger_licenses (
			org_id, vendor_key, product_key, license_key, external_identity_id,
			directory_user_id, assigned_external_id, license_status, paid,
			unit_amount, currency, billing_interval, origin_kind,
			source_evidence_level, reconciliation_status, evidence_level,
			source_identifier, source_key, external_reference, freshness_at,
			scope, reporting_period_start, reporting_period_end
		) VALUES (
			$1::uuid,$2,$3,$4,
			(SELECT id FROM ai_ledger_external_identities WHERE org_id=$1::uuid AND external_id=$5 ORDER BY updated_at DESC LIMIT 1),
			NULL,NULLIF($5,''),$6,$7,NULLIF($8,'')::numeric,NULLIF(upper($9),''),
			NULLIF($10,''),$11,$12,$13,$14,$15,$16,NULLIF($17,''),$18,$19,$20,$21
		)
		ON CONFLICT (org_id, source_identifier, source_key)
		DO UPDATE SET license_status=EXCLUDED.license_status, paid=EXCLUDED.paid,
			unit_amount=EXCLUDED.unit_amount, currency=EXCLUDED.currency,
			billing_interval=EXCLUDED.billing_interval,
			external_identity_id=EXCLUDED.external_identity_id,
			assigned_external_id=EXCLUDED.assigned_external_id,
			source_evidence_level=EXCLUDED.source_evidence_level,
			reconciliation_status=EXCLUDED.reconciliation_status,
			evidence_level=EXCLUDED.evidence_level, freshness_at=EXCLUDED.freshness_at,
			reporting_period_start=EXCLUDED.reporting_period_start,
			reporting_period_end=EXCLUDED.reporting_period_end, updated_at=now()`,
		s.orgID, p.VendorKey, p.ProductKey, fact.LicenseKey, fact.ExternalIdentityID,
		status, fact.Paid, valueOrEmpty(fact.UnitAmount), fact.Currency,
		fact.BillingInterval, p.OriginKind, p.SourceEvidenceLevel,
		p.ReconciliationStatus, p.EvidenceLevel, s.sourceIdentifier, p.SourceKey,
		p.ExternalReference, p.FreshnessAt, p.Scope, p.ReportingPeriodStart,
		p.ReportingPeriodEnd,
	)
	return err
}

func (s *sqlFactSink) UpsertMetric(fact MetricFact) error {
	p := fact.Provenance
	if err := p.NormalizeAndValidate(); err != nil {
		return err
	}
	if p.ReportingPeriodStart == nil || p.ReportingPeriodEnd == nil {
		return fmt.Errorf("metric reporting period is required")
	}
	_, err := s.db.ExecContext(context.Background(), `
		INSERT INTO ai_ledger_metrics (
			org_id,vendor_key,product_key,metric_key,metric_value,unit,is_activity,
			external_identity_id,project_key,model_identifier,origin_kind,
			source_evidence_level,reconciliation_status,evidence_level,
			source_identifier,source_key,external_reference,freshness_at,scope,
			reporting_period_start,reporting_period_end
		) VALUES (
			$1::uuid,$2,$3,$4,$5::numeric,$6,$7,
			(SELECT id FROM ai_ledger_external_identities WHERE org_id=$1::uuid AND external_id=$8 ORDER BY updated_at DESC LIMIT 1),
			NULLIF($9,''),NULLIF($10,''),$11,$12,$13,$14,$15,$16,
			NULLIF($17,''),$18,$19,$20,$21
		)
		ON CONFLICT (org_id,source_identifier,source_key)
		DO UPDATE SET metric_value=EXCLUDED.metric_value,unit=EXCLUDED.unit,
			is_activity=EXCLUDED.is_activity,external_identity_id=EXCLUDED.external_identity_id,
			project_key=EXCLUDED.project_key,model_identifier=EXCLUDED.model_identifier,
			source_evidence_level=EXCLUDED.source_evidence_level,
			reconciliation_status=EXCLUDED.reconciliation_status,
			evidence_level=EXCLUDED.evidence_level,freshness_at=EXCLUDED.freshness_at,
			reporting_period_start=EXCLUDED.reporting_period_start,
			reporting_period_end=EXCLUDED.reporting_period_end,updated_at=now()`,
		s.orgID, p.VendorKey, p.ProductKey, fact.MetricKey, fact.Value, fact.Unit, fact.IsActivity,
		fact.ExternalIdentityID, fact.ProjectKey, fact.ModelIdentifier, p.OriginKind,
		p.SourceEvidenceLevel, p.ReconciliationStatus, p.EvidenceLevel, s.sourceIdentifier,
		p.SourceKey, p.ExternalReference, p.FreshnessAt, p.Scope, p.ReportingPeriodStart, p.ReportingPeriodEnd,
	)
	return err
}

func (s *sqlFactSink) UpsertCost(fact CostFact) error {
	p := fact.Provenance
	if err := p.NormalizeAndValidate(); err != nil {
		return err
	}
	if p.ReportingPeriodStart == nil || p.ReportingPeriodEnd == nil {
		return fmt.Errorf("cost reporting period is required")
	}
	_, err := s.db.ExecContext(context.Background(), `
		INSERT INTO ai_ledger_costs (
			org_id,vendor_key,product_key,cost_key,amount,currency,cost_kind,
			external_identity_id,project_key,team_key,origin_kind,
			source_evidence_level,reconciliation_status,evidence_level,
			source_identifier,source_key,external_reference,freshness_at,scope,
			reporting_period_start,reporting_period_end
		) VALUES (
			$1::uuid,$2,$3,$4,$5::numeric,upper($6),$7,
			(SELECT id FROM ai_ledger_external_identities WHERE org_id=$1::uuid AND external_id=$8 ORDER BY updated_at DESC LIMIT 1),
			NULLIF($9,''),NULLIF($10,''),$11,$12,$13,$14,$15,$16,
			NULLIF($17,''),$18,$19,$20,$21
		)
		ON CONFLICT (org_id,source_identifier,source_key)
		DO UPDATE SET amount=EXCLUDED.amount,currency=EXCLUDED.currency,
			cost_kind=EXCLUDED.cost_kind,external_identity_id=EXCLUDED.external_identity_id,
			project_key=EXCLUDED.project_key,team_key=EXCLUDED.team_key,
			source_evidence_level=EXCLUDED.source_evidence_level,
			reconciliation_status=EXCLUDED.reconciliation_status,
			evidence_level=EXCLUDED.evidence_level,freshness_at=EXCLUDED.freshness_at,
			reporting_period_start=EXCLUDED.reporting_period_start,
			reporting_period_end=EXCLUDED.reporting_period_end,updated_at=now()`,
		s.orgID, p.VendorKey, p.ProductKey, fact.CostKey, fact.Amount, fact.Currency,
		defaultString(normalizeKey(fact.CostKind), "charge"), fact.ExternalIdentityID,
		fact.ProjectKey, fact.TeamKey, p.OriginKind, p.SourceEvidenceLevel,
		p.ReconciliationStatus, p.EvidenceLevel, s.sourceIdentifier, p.SourceKey,
		p.ExternalReference, p.FreshnessAt, p.Scope, p.ReportingPeriodStart, p.ReportingPeriodEnd,
	)
	return err
}

func (s *SQLStore) Reconcile(ctx context.Context, orgID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	queries := []string{
		`UPDATE ai_ledger_external_identities
		 SET reconciliation_status='matched',
		     evidence_level=CASE WHEN source_evidence_level='estimated' THEN 'estimated' ELSE 'reconciled' END,
		     updated_at=now()
		 WHERE org_id=$1::uuid AND identity_kind='project' AND project_key IS NOT NULL`,
		`UPDATE ai_ledger_external_identities e
		 SET directory_user_id = u.id, reconciliation_status = 'matched',
		     evidence_level = CASE WHEN e.source_evidence_level='estimated' THEN 'estimated' ELSE 'reconciled' END,
		     updated_at = now()
		 FROM ai_ledger_directory_users u
		 WHERE e.org_id=$1::uuid AND u.org_id=e.org_id
		   AND e.identity_source_key=u.identity_source_key
		   AND e.external_id=u.external_user_id`,
		`WITH unique_email AS (
			SELECT normalized_email, min(id::text)::uuid AS user_id
			FROM ai_ledger_directory_users
			WHERE org_id=$1::uuid AND normalized_email IS NOT NULL
			GROUP BY normalized_email HAVING count(*)=1
		 )
		 UPDATE ai_ledger_external_identities e
		 SET directory_user_id=u.user_id,reconciliation_status='matched',
		     evidence_level=CASE WHEN e.source_evidence_level='estimated' THEN 'estimated' ELSE 'reconciled' END,
		     updated_at=now()
		 FROM unique_email u
		 WHERE e.org_id=$1::uuid AND e.directory_user_id IS NULL
		   AND e.normalized_email=u.normalized_email`,
		`UPDATE ai_ledger_external_identities
		 SET reconciliation_status='unmatched',
		     evidence_level=CASE WHEN source_evidence_level='estimated' THEN 'estimated' ELSE 'authoritative' END,
		     updated_at=now()
		 WHERE org_id=$1::uuid AND directory_user_id IS NULL AND identity_kind <> 'project'`,
		`UPDATE ai_ledger_licenses l
		 SET directory_user_id=e.directory_user_id,
		     reconciliation_status=CASE WHEN e.directory_user_id IS NULL THEN 'unmatched' ELSE 'matched' END,
		     evidence_level=CASE
		       WHEN l.source_evidence_level='estimated' THEN 'estimated'
		       WHEN e.directory_user_id IS NOT NULL THEN 'reconciled'
		       ELSE 'authoritative' END,
		     updated_at=now()
		 FROM ai_ledger_external_identities e
		 WHERE l.org_id=$1::uuid AND e.org_id=l.org_id AND l.external_identity_id=e.id`,
		`UPDATE ai_ledger_metrics m
		 SET reconciliation_status=CASE
		       WHEN e.directory_user_id IS NOT NULL OR m.project_key IS NOT NULL THEN 'matched'
		       ELSE 'unmatched' END,
		     evidence_level=CASE
		       WHEN m.source_evidence_level='estimated' THEN 'estimated'
		       WHEN e.directory_user_id IS NOT NULL OR m.project_key IS NOT NULL THEN 'reconciled'
		       ELSE 'authoritative' END,
		     updated_at=now()
		 FROM ai_ledger_external_identities e
		 WHERE m.org_id=$1::uuid AND e.org_id=m.org_id AND m.external_identity_id=e.id`,
		`UPDATE ai_ledger_metrics
		 SET reconciliation_status='matched',
		     evidence_level=CASE WHEN source_evidence_level='estimated' THEN 'estimated' ELSE 'reconciled' END,
		     updated_at=now()
		 WHERE org_id=$1::uuid AND external_identity_id IS NULL AND project_key IS NOT NULL`,
		`UPDATE ai_ledger_costs c
		 SET reconciliation_status=CASE
		       WHEN e.directory_user_id IS NOT NULL OR c.project_key IS NOT NULL OR c.team_key IS NOT NULL THEN 'matched'
		       ELSE 'unmatched' END,
		     evidence_level=CASE
		       WHEN c.source_evidence_level='estimated' THEN 'estimated'
		       WHEN e.directory_user_id IS NOT NULL OR c.project_key IS NOT NULL OR c.team_key IS NOT NULL THEN 'reconciled'
		       ELSE 'authoritative' END,
		     updated_at=now()
		 FROM ai_ledger_external_identities e
		 WHERE c.org_id=$1::uuid AND e.org_id=c.org_id AND c.external_identity_id=e.id`,
		`UPDATE ai_ledger_costs
		 SET reconciliation_status='matched',
		     evidence_level=CASE WHEN source_evidence_level='estimated' THEN 'estimated' ELSE 'reconciled' END,
		     updated_at=now()
		 WHERE org_id=$1::uuid AND external_identity_id IS NULL
		   AND (project_key IS NOT NULL OR team_key IS NOT NULL)`,
	}
	for _, query := range queries {
		if _, err := tx.ExecContext(ctx, query, orgID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *SQLStore) RefreshFindings(ctx context.Context, orgID string, now time.Time) error {
	snapshot, err := s.findingSnapshot(ctx, orgID)
	if err != nil {
		return err
	}
	findings := EvaluateFindings(snapshot, now)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	keys := make([]string, 0, len(findings))
	for _, finding := range findings {
		evidence, _ := json.Marshal(finding.EvidenceSourceKeys)
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO ai_ledger_findings (
				org_id,finding_key,finding_type,status,severity,title,summary,
				recommendation,evidence,vendor_key,product_key,directory_user_id,
				currency,amount,first_observed_at,last_observed_at
			) VALUES (
				$1::uuid,$2,$3,'open',$4,$5,$6,$7,$8::jsonb,NULLIF($9,''),
				NULLIF($10,''),NULLIF($11,'')::uuid,NULLIF($12,''),NULLIF($13::numeric,0::numeric),$14,$14
			)
			ON CONFLICT (org_id,finding_key)
			DO UPDATE SET finding_type=EXCLUDED.finding_type,severity=EXCLUDED.severity,
				title=EXCLUDED.title,summary=EXCLUDED.summary,
				recommendation=EXCLUDED.recommendation,evidence=EXCLUDED.evidence,
				vendor_key=EXCLUDED.vendor_key,product_key=EXCLUDED.product_key,
				directory_user_id=EXCLUDED.directory_user_id,currency=EXCLUDED.currency,
				amount=EXCLUDED.amount,last_observed_at=EXCLUDED.last_observed_at,
				status=CASE WHEN ai_ledger_findings.status='resolved' THEN 'open' ELSE ai_ledger_findings.status END,
				updated_at=now()`,
			orgID, finding.Key, finding.Type, finding.Severity, finding.Title, finding.Summary,
			finding.Recommendation, string(evidence), finding.VendorKey, finding.ProductKey,
			finding.DirectoryUserID, finding.Currency, finding.Amount, now,
		); err != nil {
			return err
		}
		keys = append(keys, finding.Key)
	}
	if len(keys) == 0 {
		if _, err := tx.ExecContext(ctx, `UPDATE ai_ledger_findings SET status='resolved',updated_at=now() WHERE org_id=$1::uuid AND status='open'`, orgID); err != nil {
			return err
		}
	} else if _, err := tx.ExecContext(ctx, `
		UPDATE ai_ledger_findings SET status='resolved',updated_at=now()
		WHERE org_id=$1::uuid AND status='open' AND NOT (finding_key = ANY($2))`,
		orgID, pq.Array(keys)); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLStore) findingSnapshot(ctx context.Context, orgID string) (FindingSnapshot, error) {
	var snapshot FindingSnapshot
	rows, err := s.db.QueryContext(ctx, `
		SELECT vendor_key,product_key,max(display_name),
		       COALESCE(NULLIF(max(functional_category),'unknown'),'unknown'),
		       bool_or(approved),bool_or(has_contract),bool_or(has_billing_source)
		FROM ai_ledger_products WHERE org_id=$1::uuid
		GROUP BY vendor_key,product_key`, orgID)
	if err != nil {
		return snapshot, err
	}
	for rows.Next() {
		var item FindingProduct
		if err := rows.Scan(&item.VendorKey, &item.ProductKey, &item.DisplayName,
			&item.FunctionalCategory, &item.Approved, &item.HasContract, &item.HasBillingSource); err != nil {
			rows.Close()
			return snapshot, err
		}
		snapshot.Products = append(snapshot.Products, item)
	}
	rows.Close()

	userRows, err := s.db.QueryContext(ctx, `
		SELECT id::text,identity_source_key,external_user_id,COALESCE(normalized_email,''),status
		FROM ai_ledger_directory_users WHERE org_id=$1::uuid`, orgID)
	if err != nil {
		return snapshot, err
	}
	for userRows.Next() {
		var item DirectoryUser
		if err := userRows.Scan(&item.ID, &item.IdentitySourceKey, &item.ExternalUserID, &item.NormalizedEmail, &item.Status); err != nil {
			userRows.Close()
			return snapshot, err
		}
		snapshot.Users = append(snapshot.Users, item)
	}
	userRows.Close()

	seatRows, err := s.db.QueryContext(ctx, `
		SELECT license_key,vendor_key,product_key,COALESCE(directory_user_id::text,''),
		       paid,license_status,source_key
		FROM ai_ledger_licenses WHERE org_id=$1::uuid`, orgID)
	if err != nil {
		return snapshot, err
	}
	for seatRows.Next() {
		var item FindingSeat
		if err := seatRows.Scan(&item.LicenseKey, &item.VendorKey, &item.ProductKey,
			&item.DirectoryUserID, &item.Paid, &item.Status, &item.SourceKey); err != nil {
			seatRows.Close()
			return snapshot, err
		}
		snapshot.Seats = append(snapshot.Seats, item)
	}
	seatRows.Close()

	activityRows, err := s.db.QueryContext(ctx, `
		SELECT a.vendor_key,a.product_key,COALESCE(assignment.directory_user_id::text,''),
		       COALESCE(a.project_identifier,''),a.observed_at,a.source_key
		FROM ai_ledger_activity_events a
		LEFT JOIN LATERAL (
			SELECT min(x.directory_user_id::text)::uuid AS directory_user_id
			FROM ai_ledger_device_user_assignments x
			WHERE x.org_id=$1::uuid AND x.device_id::text=a.device_id
			  AND x.assigned_from<=a.observed_at
			  AND (x.assigned_until IS NULL OR x.assigned_until>a.observed_at)
			HAVING count(DISTINCT x.directory_user_id)=1
		) assignment ON true
		WHERE a.org_id=$1::text`, orgID)
	if err != nil {
		return snapshot, err
	}
	for activityRows.Next() {
		var item FindingActivity
		if err := activityRows.Scan(&item.VendorKey, &item.ProductKey, &item.DirectoryUserID,
			&item.ProjectKey, &item.ObservedAt, &item.SourceKey); err != nil {
			activityRows.Close()
			return snapshot, err
		}
		item.OriginKind = OriginEndpoint
		item.IsActivity = true
		snapshot.Activities = append(snapshot.Activities, item)
	}
	activityRows.Close()

	metricRows, err := s.db.QueryContext(ctx, `
		SELECT m.vendor_key,m.product_key,COALESCE(e.directory_user_id::text,''),
		       COALESCE(m.project_key,''),m.reporting_period_end,m.source_key
		FROM ai_ledger_metrics m
		LEFT JOIN ai_ledger_external_identities e ON e.id=m.external_identity_id
		WHERE m.org_id=$1::uuid AND m.is_activity=true`, orgID)
	if err != nil {
		return snapshot, err
	}
	for metricRows.Next() {
		var item FindingActivity
		if err := metricRows.Scan(&item.VendorKey, &item.ProductKey, &item.DirectoryUserID,
			&item.ProjectKey, &item.ObservedAt, &item.SourceKey); err != nil {
			metricRows.Close()
			return snapshot, err
		}
		item.OriginKind = OriginConnector
		item.IsActivity = true
		snapshot.Activities = append(snapshot.Activities, item)
	}
	metricRows.Close()

	costRows, err := s.db.QueryContext(ctx, `
		SELECT c.vendor_key,c.product_key,COALESCE(e.directory_user_id::text,''),
		       COALESCE(c.project_key,''),COALESCE(c.team_key,''),c.amount::float8,
		       c.currency,c.reporting_period_start,c.source_evidence_level,c.source_key
		FROM ai_ledger_costs c
		LEFT JOIN ai_ledger_external_identities e ON e.id=c.external_identity_id
		WHERE c.org_id=$1::uuid`, orgID)
	if err != nil {
		return snapshot, err
	}
	for costRows.Next() {
		var item FindingCost
		if err := costRows.Scan(&item.VendorKey, &item.ProductKey, &item.DirectoryUserID,
			&item.ProjectKey, &item.TeamKey, &item.Amount, &item.Currency, &item.Day,
			&item.SourceEvidence, &item.SourceKey); err != nil {
			costRows.Close()
			return snapshot, err
		}
		snapshot.Costs = append(snapshot.Costs, item)
	}
	return snapshot, costRows.Close()
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
