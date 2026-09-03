package ailedger

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

func (s *SQLStore) Summary(ctx context.Context, orgID string) (map[string]interface{}, error) {
	summary := map[string]interface{}{}
	var endpointEvents, endpointProducts, connectorActivities, paidSeats, openFindings int64
	if err := s.db.QueryRowContext(ctx, `
		SELECT
		  COALESCE((SELECT sum(activity_count) FROM ai_ledger_activity_events WHERE org_id=$1::text),0),
		  COALESCE((SELECT count(DISTINCT vendor_key || '/' || product_key) FROM ai_ledger_activity_events WHERE org_id=$1::text),0),
		  COALESCE((SELECT count(*) FROM ai_ledger_metrics WHERE org_id=$1::uuid AND is_activity=true),0),
		  COALESCE((SELECT count(*) FROM ai_ledger_licenses WHERE org_id=$1::uuid AND paid=true AND license_status='assigned'),0),
		  COALESCE((SELECT count(*) FROM ai_ledger_findings WHERE org_id=$1::uuid AND status='open'),0)`,
		orgID).Scan(&endpointEvents, &endpointProducts, &connectorActivities, &paidSeats, &openFindings); err != nil {
		return nil, err
	}
	summary["endpoint_activity_count"] = endpointEvents
	summary["endpoint_product_count"] = endpointProducts
	summary["connector_activity_record_count"] = connectorActivities
	summary["paid_seat_count"] = paidSeats
	summary["open_finding_count"] = openFindings
	rows, err := s.db.QueryContext(ctx, `
		SELECT currency, sum(amount)::text, min(reporting_period_start), max(reporting_period_end),
		       max(freshness_at)
		FROM ai_ledger_costs WHERE org_id=$1::uuid GROUP BY currency ORDER BY currency`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	spend := make([]map[string]interface{}, 0)
	for rows.Next() {
		var currency, amount string
		var start, end, freshness time.Time
		if err := rows.Scan(&currency, &amount, &start, &end, &freshness); err != nil {
			return nil, err
		}
		spend = append(spend, map[string]interface{}{
			"currency": currency, "authoritative_total": amount,
			"period_start": start, "period_end": end, "freshness_at": freshness,
		})
	}
	summary["spend_by_currency"] = spend
	return summary, rows.Err()
}

func (s *SQLStore) Products(ctx context.Context, orgID string) ([]map[string]interface{}, error) {
	rows, err := s.db.QueryContext(ctx, `
		WITH catalog AS (
		  SELECT vendor_key,product_key,max(display_name) AS display_name,
		         COALESCE(NULLIF(max(functional_category),'unknown'),'unknown') AS category,
		         bool_or(approved) AS approved,bool_or(has_contract) AS has_contract,
		         bool_or(has_billing_source) AS has_billing_source,
		         max(freshness_at) AS freshness
		  FROM ai_ledger_products WHERE org_id=$1::uuid GROUP BY vendor_key,product_key
		), observed AS (
		  SELECT vendor_key,product_key,sum(activity_count) AS endpoint_count,
		         count(DISTINCT device_id) AS device_count,max(observed_at) AS last_endpoint_at
		  FROM ai_ledger_activity_events WHERE org_id=$1::text GROUP BY vendor_key,product_key
		), connector_use AS (
		  SELECT vendor_key,product_key,count(*) AS connector_activity_records,
		         max(reporting_period_end) AS last_connector_at,
		         CASE WHEN bool_and(evidence_level='reconciled') THEN 'reconciled'
		              WHEN bool_or(evidence_level='estimated') THEN 'estimated'
		              ELSE 'authoritative' END AS effective_evidence
		  FROM ai_ledger_metrics WHERE org_id=$1::uuid AND is_activity=true
		  GROUP BY vendor_key,product_key
		), seats AS (
		  SELECT vendor_key,product_key,count(*) FILTER (WHERE paid AND license_status='assigned') AS paid_seats
		  FROM ai_ledger_licenses WHERE org_id=$1::uuid GROUP BY vendor_key,product_key
		), costs AS (
		  SELECT vendor_key,product_key,
		         jsonb_object_agg(currency,total ORDER BY currency) AS totals
		  FROM (
		    SELECT vendor_key,product_key,currency,sum(amount)::text AS total
		    FROM ai_ledger_costs WHERE org_id=$1::uuid
		    GROUP BY vendor_key,product_key,currency
		  ) x GROUP BY vendor_key,product_key
		), keys AS (
		  SELECT vendor_key,product_key FROM catalog UNION
		  SELECT vendor_key,product_key FROM observed UNION
		  SELECT vendor_key,product_key FROM connector_use UNION
		  SELECT vendor_key,product_key FROM seats UNION
		  SELECT vendor_key,product_key FROM costs
		)
		SELECT k.vendor_key,k.product_key,COALESCE(c.display_name,k.product_key),
		       COALESCE(c.category,'unknown'),COALESCE(c.approved,false),
		       COALESCE(c.has_contract,false),COALESCE(c.has_billing_source,false),
		       COALESCE(o.endpoint_count,0),COALESCE(o.device_count,0),o.last_endpoint_at,
		       COALESCE(u.connector_activity_records,0),u.last_connector_at,COALESCE(u.effective_evidence,'authoritative'),
		       COALESCE(st.paid_seats,0),COALESCE(co.totals,'{}'::jsonb),
		       c.freshness
		FROM keys k
		LEFT JOIN catalog c USING(vendor_key,product_key)
		LEFT JOIN observed o USING(vendor_key,product_key)
		LEFT JOIN connector_use u USING(vendor_key,product_key)
		LEFT JOIN seats st USING(vendor_key,product_key)
		LEFT JOIN costs co USING(vendor_key,product_key)
		ORDER BY COALESCE(o.endpoint_count,0) DESC,k.vendor_key,k.product_key`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]map[string]interface{}, 0)
	for rows.Next() {
		var vendor, product, display, category, connectorEvidence string
		var approved, contract, billing bool
		var endpointCount, deviceCount, connectorCount, seats int64
		var endpointAt, connectorAt, freshness *time.Time
		var costsRaw []byte
		if err := rows.Scan(&vendor, &product, &display, &category, &approved, &contract, &billing,
			&endpointCount, &deviceCount, &endpointAt, &connectorCount, &connectorAt, &connectorEvidence, &seats,
			&costsRaw, &freshness); err != nil {
			return nil, err
		}
		var costTotals map[string]string
		_ = json.Unmarshal(costsRaw, &costTotals)
		out = append(out, map[string]interface{}{
			"vendor_key": vendor, "product_key": product, "display_name": display,
			"functional_category": category, "approved": approved, "has_contract": contract,
			"has_billing_source": billing, "endpoint_activity_count": endpointCount,
			"endpoint_device_count": deviceCount, "last_endpoint_activity_at": endpointAt,
			"connector_activity_record_count": connectorCount, "last_connector_activity_at": connectorAt,
			"paid_seat_count": seats, "cost_totals": costTotals, "freshness_at": freshness,
			"endpoint_evidence_level":         "observed",
			"connector_source_evidence_level": "authoritative",
			"connector_evidence_level":        connectorEvidence,
			"connector_reconciliation_status": map[bool]string{true: "matched", false: "unmatched"}[connectorEvidence == "reconciled"],
		})
	}
	return out, rows.Err()
}

func (s *SQLStore) People(ctx context.Context, orgID string) ([]map[string]interface{}, error) {
	rows, err := s.db.QueryContext(ctx, `
		WITH endpoint AS (
		  SELECT a.resolved_directory_user_id AS directory_user_id,count(*) AS events,count(DISTINCT a.vendor_key || '/' || a.product_key) AS products,
		         max(a.observed_at) AS last_at
		  FROM (
		    SELECT e.vendor_key,e.product_key,e.observed_at,
		      assignment.directory_user_id AS resolved_directory_user_id
		    FROM ai_ledger_activity_events e
		    LEFT JOIN LATERAL (
		      SELECT min(x.directory_user_id::text)::uuid AS directory_user_id
		      FROM ai_ledger_device_user_assignments x
		      WHERE x.org_id=$1::uuid AND x.device_id::text=e.device_id
		        AND x.assigned_from<=e.observed_at
		        AND (x.assigned_until IS NULL OR x.assigned_until>e.observed_at)
		      HAVING count(DISTINCT x.directory_user_id)=1
		    ) assignment ON true
		    WHERE e.org_id=$1::text
		  ) a
		  WHERE a.resolved_directory_user_id IS NOT NULL
		  GROUP BY a.resolved_directory_user_id
		), connector_use AS (
		  SELECT i.directory_user_id,count(*) AS records,max(m.reporting_period_end) AS last_at,
		         CASE WHEN bool_and(m.evidence_level='reconciled') THEN 'reconciled'
		              WHEN bool_or(m.evidence_level='estimated') THEN 'estimated'
		              ELSE 'authoritative' END AS effective_evidence
		  FROM ai_ledger_metrics m
		  JOIN ai_ledger_external_identities i ON i.id=m.external_identity_id
		  WHERE m.org_id=$1::uuid AND m.is_activity=true AND i.directory_user_id IS NOT NULL
		  GROUP BY i.directory_user_id
		), seats AS (
		  SELECT directory_user_id,count(*) FILTER(WHERE paid AND license_status='assigned') AS paid
		  FROM ai_ledger_licenses WHERE org_id=$1::uuid AND directory_user_id IS NOT NULL
		  GROUP BY directory_user_id
		)
		SELECT u.id::text,u.normalized_email,COALESCE(u.display_name,''),u.status,
		       COALESCE(e.events,0),COALESCE(e.products,0),e.last_at,
		       COALESCE(c.records,0),c.last_at,COALESCE(c.effective_evidence,'authoritative'),COALESCE(st.paid,0),
		       u.identity_source_key,u.external_user_id,u.freshness_at,u.evidence_level
		FROM ai_ledger_directory_users u
		LEFT JOIN endpoint e ON e.directory_user_id=u.id
		LEFT JOIN connector_use c ON c.directory_user_id=u.id
		LEFT JOIN seats st ON st.directory_user_id=u.id
		WHERE u.org_id=$1::uuid
		ORDER BY COALESCE(e.events,0) DESC,u.normalized_email`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id string
		var email *string
		var name, status, source, external, evidence, connectorEvidence string
		var endpointEvents, endpointProducts, connectorRecords, paidSeats int64
		var endpointAt, connectorAt *time.Time
		var freshness time.Time
		if err := rows.Scan(&id, &email, &name, &status, &endpointEvents, &endpointProducts, &endpointAt,
			&connectorRecords, &connectorAt, &connectorEvidence, &paidSeats, &source, &external, &freshness, &evidence); err != nil {
			return nil, err
		}
		out = append(out, map[string]interface{}{
			"id": id, "normalized_email": email, "display_name": name, "status": status,
			"endpoint_activity_count": endpointEvents, "endpoint_product_count": endpointProducts,
			"last_endpoint_activity_at": endpointAt, "connector_activity_record_count": connectorRecords,
			"last_connector_activity_at": connectorAt, "paid_seat_count": paidSeats,
			"connector_source_evidence_level": "authoritative", "connector_evidence_level": connectorEvidence,
			"connector_reconciliation_status": map[bool]string{true: "matched", false: "unmatched"}[connectorEvidence == "reconciled"],
			"identity_source_key":             source, "external_user_id": external,
			"freshness_at": freshness, "evidence_level": evidence,
		})
	}
	return out, rows.Err()
}

func (s *SQLStore) Findings(ctx context.Context, orgID, status string) ([]map[string]interface{}, error) {
	query := `SELECT id::text,finding_key,finding_type,status,severity,title,summary,recommendation,
		evidence,vendor_key,product_key,directory_user_id::text,currency,amount::text,
		first_observed_at,last_observed_at
		FROM ai_ledger_findings WHERE org_id=$1::uuid`
	args := []interface{}{orgID}
	if status != "" {
		query += ` AND status=$2`
		args = append(args, status)
	}
	query += ` ORDER BY CASE severity WHEN 'important' THEN 1 WHEN 'review' THEN 2 ELSE 3 END,last_observed_at DESC`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id, key, kind, state, severity, title, summary, recommendation string
		var evidence []byte
		var vendor, product, user, currency, amount *string
		var first, last time.Time
		if err := rows.Scan(&id, &key, &kind, &state, &severity, &title, &summary, &recommendation,
			&evidence, &vendor, &product, &user, &currency, &amount, &first, &last); err != nil {
			return nil, err
		}
		var evidenceKeys []string
		_ = json.Unmarshal(evidence, &evidenceKeys)
		out = append(out, map[string]interface{}{
			"id": id, "finding_key": key, "finding_type": kind, "status": state, "severity": severity,
			"title": title, "summary": summary, "recommendation": recommendation,
			"evidence_source_keys": evidenceKeys, "vendor_key": vendor, "product_key": product,
			"directory_user_id": user, "currency": currency, "amount": amount,
			"first_observed_at": first, "last_observed_at": last,
		})
	}
	return out, rows.Err()
}

func (s *SQLStore) Sources(ctx context.Context, orgID string) ([]map[string]interface{}, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT source_kind,source_identifier,status,freshness_at,scope,record_count
		FROM (
		  SELECT 'connector' AS source_kind,id::text AS source_identifier,status,
		         freshness_at,'organization' AS scope,
		         (SELECT count(*) FROM ai_ledger_metrics m WHERE m.org_id=c.org_id AND m.source_identifier=c.id::text)
		         +(SELECT count(*) FROM ai_ledger_costs x WHERE x.org_id=c.org_id AND x.source_identifier=c.id::text)
		         +(SELECT count(*) FROM ai_ledger_licenses l WHERE l.org_id=c.org_id AND l.source_identifier=c.id::text) AS record_count
		  FROM ai_ledger_connectors c WHERE org_id=$1::uuid
		  UNION ALL
		  SELECT 'endpoint',device_id,'observed',max(freshness_at),'device',count(*)
		  FROM ai_ledger_activity_events WHERE org_id=$1::text GROUP BY device_id
		  UNION ALL
		  SELECT 'import',source_identifier,status,COALESCE(imported_at,created_at),'organization',record_count
		  FROM ai_ledger_imports WHERE org_id=$1::uuid
		) sources ORDER BY source_kind,source_identifier`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]map[string]interface{}, 0)
	for rows.Next() {
		var kind, id, status, scope string
		var freshness *time.Time
		var count int64
		if err := rows.Scan(&kind, &id, &status, &freshness, &scope, &count); err != nil {
			return nil, err
		}
		evidence := "authoritative"
		reconciliation := "unmatched"
		if kind == "endpoint" {
			evidence = "observed"
			reconciliation = "not_applicable"
		}
		out = append(out, map[string]interface{}{
			"origin_kind": kind, "source_identifier": id, "status": status,
			"freshness_at": freshness, "scope": scope, "record_count": count,
			"source_evidence_level": evidence, "evidence_level": evidence,
			"reconciliation_status": reconciliation,
		})
	}
	return out, rows.Err()
}

func (s *SQLStore) DirectoryUsers(ctx context.Context, orgID string) ([]map[string]interface{}, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id::text,identity_source_key,external_user_id,normalized_email,
		       COALESCE(display_name,''),status,origin_kind,source_evidence_level,
		       reconciliation_status,evidence_level,source_identifier,source_key,
		       freshness_at,scope
		FROM ai_ledger_directory_users WHERE org_id=$1::uuid
		ORDER BY normalized_email NULLS LAST,external_user_id`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id, source, external, name, status, origin, sourceEvidence, reconciliation, evidence, sourceID, sourceKey, scope string
		var email *string
		var freshness time.Time
		if err := rows.Scan(&id, &source, &external, &email, &name, &status, &origin, &sourceEvidence,
			&reconciliation, &evidence, &sourceID, &sourceKey, &freshness, &scope); err != nil {
			return nil, err
		}
		out = append(out, map[string]interface{}{
			"id": id, "identity_source_key": source, "external_user_id": external,
			"normalized_email": email, "display_name": name, "status": status,
			"origin_kind": origin, "source_evidence_level": sourceEvidence,
			"reconciliation_status": reconciliation, "evidence_level": evidence,
			"source_identifier": sourceID, "source_key": sourceKey,
			"freshness_at": freshness, "scope": scope,
		})
	}
	return out, rows.Err()
}

func (s *SQLStore) AssignDeviceUser(ctx context.Context, orgID, deviceID, userID, source, actorID string, from time.Time, until *time.Time) error {
	if source != "mdm" && source != "admin" {
		return fmt.Errorf("assignment_source must be mdm or admin")
	}
	if from.IsZero() {
		from = time.Now().UTC()
	}
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO ai_ledger_device_user_assignments(
			org_id,device_id,directory_user_id,assigned_from,assigned_until,
			assignment_source,assigned_by
		)
		SELECT $1::uuid,d.id,u.id,$4,$5,$6,NULLIF($7,'')::uuid
		FROM devices d,ai_ledger_directory_users u
		WHERE d.id=$2::uuid AND d.org_id=$1::uuid AND u.id=$3::uuid AND u.org_id=$1::uuid`,
		orgID, deviceID, userID, from, until, source, actorID)
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count != 1 {
		return fmt.Errorf("device or directory user not found in organization")
	}
	return nil
}

// DeleteEndpointActivityBefore applies an explicit organization-scoped
// retention cutoff. Commercial source facts are intentionally unaffected.
func (s *SQLStore) DeleteEndpointActivityBefore(ctx context.Context, orgID string, cutoff time.Time) (int64, error) {
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM ai_ledger_activity_events
		WHERE org_id=$1::text AND observed_at < $2`, orgID, cutoff.UTC())
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *SQLStore) RecordImport(ctx context.Context, orgID, format, version, hash, source string, count int, status string, validationErrors []string) (string, error) {
	raw, _ := json.Marshal(validationErrors)
	var id string
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO ai_ledger_imports(
			org_id,import_format,schema_version,content_hash,source_identifier,
			record_count,status,validation_errors,imported_at
		) VALUES($1::uuid,$2,$3,$4,$5,$6,$7,$8::jsonb,CASE WHEN $7='committed' THEN now() ELSE NULL END)
		ON CONFLICT(org_id,content_hash) DO UPDATE SET
			status=EXCLUDED.status,record_count=EXCLUDED.record_count,
			validation_errors=EXCLUDED.validation_errors,
			imported_at=CASE WHEN EXCLUDED.status='committed' THEN now() ELSE ai_ledger_imports.imported_at END
		RETURNING id::text`,
		orgID, format, version, hash, source, count, status, string(raw)).Scan(&id)
	return id, err
}
