package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
)

// DLPEvent represents a DLP match event stored in the dlp_events table.
type DLPEvent struct {
	ID                   int64     `json:"id"`
	Timestamp            time.Time `json:"timestamp"`
	DeviceID             string    `json:"device_id"`
	DeviceName           string    `json:"device_name"`
	OrgID                string    `json:"org_id"`
	RequestID            *string   `json:"request_id,omitempty"`
	RequestHost          string    `json:"request_host"`
	RequestPath          *string   `json:"request_path,omitempty"`
	RequestMethod        string    `json:"request_method"`
	SourceApp            *string   `json:"source_app,omitempty"`
	ServiceCategory      *string   `json:"service_category,omitempty"`
	AIVendor             *string   `json:"ai_vendor,omitempty"`
	MatchTypes           []string  `json:"match_types"`
	MatchedPatterns      []string  `json:"matched_patterns"`
	MatchedFields        []string  `json:"matched_fields"`
	MatchCount           int       `json:"match_count"`
	Severity             string    `json:"severity"`
	ClassificationReason *string   `json:"classification_reason,omitempty"`
	ContentType          *string   `json:"content_type,omitempty"`
	FileCount            int       `json:"file_count"`
	ActionTaken          string    `json:"action_taken"`
	PolicyRuleID         *string   `json:"policy_rule_id,omitempty"`
	RuleID               *string   `json:"rule_id,omitempty"`
	ReasonCode           *string   `json:"reason_code,omitempty"`
	ReasonDetail         *string   `json:"reason_detail,omitempty"`
	SemanticSource       *string   `json:"semantic_source,omitempty"`
	SemanticCategory     *string   `json:"semantic_category,omitempty"`
	SemanticConfidence   *float64  `json:"semantic_confidence,omitempty"`
	SemanticAmbiguous    *bool     `json:"semantic_ambiguous,omitempty"`
	SemanticReason       *string   `json:"semantic_reason,omitempty"`
	Protocol             string    `json:"protocol"`
	InterceptedHTTPS     bool      `json:"intercepted_https"`
	InspectionQuality    string    `json:"inspection_quality"`
	InspectionSkipReason *string   `json:"inspection_skip_reason,omitempty"`
	Direction            string    `json:"direction"`
	HasRequestBody       bool      `json:"has_request_body"`
	RequestBodyTruncated bool      `json:"request_body_truncated"`
}

// DLPEventBody represents decrypted request body for a specific DLP event.
type DLPEventBody struct {
	ID                   int64     `json:"id"`
	Timestamp            time.Time `json:"timestamp"`
	DeviceID             string    `json:"device_id"`
	DeviceName           string    `json:"device_name"`
	RequestHost          string    `json:"request_host"`
	RequestPath          *string   `json:"request_path,omitempty"`
	RequestMethod        string    `json:"request_method"`
	SourceApp            *string   `json:"source_app,omitempty"`
	Protocol             string    `json:"protocol"`
	InterceptedHTTPS     bool      `json:"intercepted_https"`
	InspectionQuality    string    `json:"inspection_quality"`
	InspectionSkipReason *string   `json:"inspection_skip_reason,omitempty"`
	Direction            string    `json:"direction"`
	Body                 string    `json:"body"`
	Truncated            bool      `json:"truncated"`
}

// DLPFilter holds filtering options for listing DLP events.
type DLPFilter struct {
	OrgID       string
	DeviceID    string
	RequestHost string
	AIVendor    string
	MatchType   string
	Protocol    string
	DateFrom    *time.Time
	DateTo      *time.Time
	Page        int
	Limit       int
}

// ListDLPEvents returns a paginated list of DLP events for the given org.
func (s *Store) ListDLPEvents(ctx context.Context, filter DLPFilter) ([]DLPEvent, int, error) {
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.Limit < 1 || filter.Limit > 100 {
		filter.Limit = 25
	}

	where := "WHERE 1=1"
	args := []interface{}{}
	argN := 1

	if filter.OrgID != "" {
		where += fmt.Sprintf(" AND e.org_id = $%d", argN)
		args = append(args, filter.OrgID)
		argN++
	}
	if filter.DeviceID != "" {
		where += fmt.Sprintf(" AND e.device_id = $%d", argN)
		args = append(args, filter.DeviceID)
		argN++
	}
	if filter.RequestHost != "" {
		where += fmt.Sprintf(" AND e.request_host ILIKE $%d", argN)
		args = append(args, "%"+filter.RequestHost+"%")
		argN++
	}
	if filter.AIVendor != "" {
		where += fmt.Sprintf(" AND e.ai_vendor = $%d", argN)
		args = append(args, filter.AIVendor)
		argN++
	}
	if filter.MatchType != "" {
		where += fmt.Sprintf(" AND $%d = ANY(e.match_types)", argN)
		args = append(args, filter.MatchType)
		argN++
	}
	if filter.Protocol != "" {
		where += fmt.Sprintf(" AND e.protocol = $%d", argN)
		args = append(args, filter.Protocol)
		argN++
	}
	if filter.DateFrom != nil {
		where += fmt.Sprintf(" AND e.timestamp >= $%d", argN)
		args = append(args, *filter.DateFrom)
		argN++
	}
	if filter.DateTo != nil {
		where += fmt.Sprintf(" AND e.timestamp <= $%d", argN)
		args = append(args, *filter.DateTo)
		argN++
	}

	var total int
	if err := s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM dlp_events e "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	offset := (filter.Page - 1) * filter.Limit
	query := fmt.Sprintf(
		`SELECT e.id, e.timestamp, e.device_id, COALESCE(d.device_name, ''), e.org_id,
		        e.request_id, e.request_host, e.request_path, e.request_method, e.source_app,
		        e.service_category, e.ai_vendor, e.match_types, e.matched_patterns, e.matched_fields,
		        e.match_count, e.severity, e.classification_reason, e.content_type, e.file_count, e.action_taken,
		        e.policy_rule_id, e.reason_code, e.reason_detail,
		        e.semantic_source, e.semantic_category, e.semantic_confidence, e.semantic_ambiguous, e.semantic_reason,
		        e.protocol, e.intercepted_https, e.inspection_quality, e.inspection_skip_reason, e.direction,
		        (e.request_body_encrypted IS NOT NULL), e.request_body_truncated
		 FROM dlp_events e
		 LEFT JOIN devices d ON d.id::text = e.device_id AND d.org_id::text = e.org_id
		 %s ORDER BY e.timestamp DESC LIMIT $%d OFFSET $%d`,
		where, argN, argN+1,
	)
	args = append(args, filter.Limit, offset)

	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var events []DLPEvent
	for rows.Next() {
		var e DLPEvent
		if err := rows.Scan(
			&e.ID, &e.Timestamp, &e.DeviceID, &e.DeviceName, &e.OrgID, &e.RequestID,
			&e.RequestHost, &e.RequestPath, &e.RequestMethod,
			&e.SourceApp, &e.ServiceCategory, &e.AIVendor,
			pq.Array(&e.MatchTypes), pq.Array(&e.MatchedPatterns), pq.Array(&e.MatchedFields),
			&e.MatchCount, &e.Severity, &e.ClassificationReason, &e.ContentType, &e.FileCount, &e.ActionTaken,
			&e.PolicyRuleID, &e.ReasonCode, &e.ReasonDetail,
			&e.SemanticSource, &e.SemanticCategory, &e.SemanticConfidence, &e.SemanticAmbiguous, &e.SemanticReason,
			&e.Protocol, &e.InterceptedHTTPS, &e.InspectionQuality, &e.InspectionSkipReason, &e.Direction,
			&e.HasRequestBody, &e.RequestBodyTruncated,
		); err != nil {
			return nil, 0, err
		}
		e.RuleID = e.PolicyRuleID
		if e.MatchTypes == nil {
			e.MatchTypes = []string{}
		}
		if e.MatchedPatterns == nil {
			e.MatchedPatterns = []string{}
		}
		if e.MatchedFields == nil {
			e.MatchedFields = []string{}
		}
		if strings.TrimSpace(e.Severity) == "" {
			e.Severity = "low"
		}
		events = append(events, e)
	}
	return events, total, rows.Err()
}

// GetDLPEvent returns one DLP event by ID scoped to an org.
func (s *Store) GetDLPEvent(ctx context.Context, orgID string, eventID int64) (*DLPEvent, error) {
	row := s.DB.QueryRowContext(ctx, `
		SELECT e.id, e.timestamp, e.device_id, COALESCE(d.device_name, ''), e.org_id,
		       e.request_id, e.request_host, e.request_path, e.request_method, e.source_app,
		       e.service_category, e.ai_vendor, e.match_types, e.matched_patterns, e.matched_fields,
		       e.match_count, e.severity, e.classification_reason, e.content_type, e.file_count, e.action_taken,
		       e.policy_rule_id, e.reason_code, e.reason_detail,
		       e.semantic_source, e.semantic_category, e.semantic_confidence, e.semantic_ambiguous, e.semantic_reason,
		       e.protocol, e.intercepted_https, e.inspection_quality, e.inspection_skip_reason, e.direction,
		       (e.request_body_encrypted IS NOT NULL), e.request_body_truncated
		FROM dlp_events e
		LEFT JOIN devices d ON d.id::text = e.device_id AND d.org_id::text = e.org_id
		WHERE e.id = $1 AND e.org_id = $2
		LIMIT 1`, eventID, orgID)

	var e DLPEvent
	if err := row.Scan(
		&e.ID, &e.Timestamp, &e.DeviceID, &e.DeviceName, &e.OrgID, &e.RequestID,
		&e.RequestHost, &e.RequestPath, &e.RequestMethod,
		&e.SourceApp, &e.ServiceCategory, &e.AIVendor,
		pq.Array(&e.MatchTypes), pq.Array(&e.MatchedPatterns), pq.Array(&e.MatchedFields),
		&e.MatchCount, &e.Severity, &e.ClassificationReason, &e.ContentType, &e.FileCount, &e.ActionTaken,
		&e.PolicyRuleID, &e.ReasonCode, &e.ReasonDetail,
		&e.SemanticSource, &e.SemanticCategory, &e.SemanticConfidence, &e.SemanticAmbiguous, &e.SemanticReason,
		&e.Protocol, &e.InterceptedHTTPS, &e.InspectionQuality, &e.InspectionSkipReason, &e.Direction,
		&e.HasRequestBody, &e.RequestBodyTruncated,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	e.RuleID = e.PolicyRuleID
	if e.MatchTypes == nil {
		e.MatchTypes = []string{}
	}
	if e.MatchedPatterns == nil {
		e.MatchedPatterns = []string{}
	}
	if e.MatchedFields == nil {
		e.MatchedFields = []string{}
	}
	if strings.TrimSpace(e.Severity) == "" {
		e.Severity = "low"
	}
	return &e, nil
}

// GetDLPEventBody returns decrypted request body for a DLP event in the org.
func (s *Store) GetDLPEventBody(ctx context.Context, orgID string, eventID int64) (*DLPEventBody, error) {
	row := s.DB.QueryRowContext(ctx, `
		SELECT e.id, e.timestamp, e.device_id, COALESCE(d.device_name, ''), e.request_host,
		       e.request_path, e.request_method, e.source_app, e.protocol, e.intercepted_https,
		       e.inspection_quality, e.inspection_skip_reason, e.direction,
		       e.request_body_encrypted, e.request_body_nonce, e.request_body_truncated
		FROM dlp_events e
		LEFT JOIN devices d ON d.id::text = e.device_id AND d.org_id::text = e.org_id
		WHERE e.id = $1 AND e.org_id = $2`,
		eventID, orgID,
	)

	var out DLPEventBody
	var encBody []byte
	var nonce []byte
	if err := row.Scan(
		&out.ID,
		&out.Timestamp,
		&out.DeviceID,
		&out.DeviceName,
		&out.RequestHost,
		&out.RequestPath,
		&out.RequestMethod,
		&out.SourceApp,
		&out.Protocol,
		&out.InterceptedHTTPS,
		&out.InspectionQuality,
		&out.InspectionSkipReason,
		&out.Direction,
		&encBody,
		&nonce,
		&out.Truncated,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	if len(encBody) > 0 {
		body, err := s.bodyCipher.Decrypt(encBody, nonce)
		if err != nil {
			return nil, fmt.Errorf("decrypt dlp request body: %w", err)
		}
		out.Body = body
	}
	return &out, nil
}

// ScrubExpiredDLPBodies removes encrypted body material from old DLP events.
// Event metadata remains for audit and analytics.
func (s *Store) ScrubExpiredDLPBodies(ctx context.Context, retentionDays int) (int64, error) {
	if retentionDays <= 0 {
		retentionDays = 30
	}
	res, err := s.DB.ExecContext(ctx, `
		UPDATE dlp_events
		SET request_body_encrypted = NULL,
		    request_body_nonce = NULL
		WHERE request_body_encrypted IS NOT NULL
		  AND timestamp < now() - ($1::text || ' days')::interval`, retentionDays)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// DLPSummary holds aggregate DLP statistics.
type DLPSummary struct {
	TotalEvents      int             `json:"total_events"`
	PIIEvents        int             `json:"pii_events"`
	CredentialEvents int             `json:"credential_events"`
	CodeEvents       int             `json:"code_events"`
	KeywordEvents    int             `json:"keyword_events"`
	TopAIVendors     []DLPVendorStat `json:"top_ai_vendors"`
	TopDevices       []DLPDeviceStat `json:"top_devices"`
}

// DLPVendorStat holds DLP event counts per AI vendor.
type DLPVendorStat struct {
	AIVendor   string `json:"ai_vendor"`
	EventCount int    `json:"event_count"`
}

// DLPDeviceStat holds DLP event counts per device.
type DLPDeviceStat struct {
	DeviceID   string `json:"device_id"`
	EventCount int    `json:"event_count"`
	SourceApp  string `json:"source_app,omitempty"`
}

func (s *Store) GetDLPSummary(ctx context.Context, orgID string, from, to time.Time) (*DLPSummary, error) {
	summary := &DLPSummary{}

	err := s.DB.QueryRowContext(ctx, `
		SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE 'pii' = ANY(match_types)),
			COUNT(*) FILTER (WHERE 'credentials' = ANY(match_types)),
			COUNT(*) FILTER (WHERE 'source_code' = ANY(match_types)),
			COUNT(*) FILTER (WHERE 'keyword' = ANY(match_types))
		FROM dlp_events
		WHERE org_id = $1 AND timestamp >= $2 AND timestamp <= $3`,
		orgID, from, to,
	).Scan(&summary.TotalEvents, &summary.PIIEvents, &summary.CredentialEvents,
		&summary.CodeEvents, &summary.KeywordEvents)
	if err != nil {
		return nil, fmt.Errorf("dlp summary: %w", err)
	}

	rows, err := s.DB.QueryContext(ctx, `
		SELECT COALESCE(ai_vendor, 'unknown'), COUNT(*) as cnt
		FROM dlp_events
		WHERE org_id = $1 AND timestamp >= $2 AND timestamp <= $3
		  AND ai_vendor IS NOT NULL
		GROUP BY ai_vendor ORDER BY cnt DESC LIMIT 10`,
		orgID, from, to,
	)
	if err != nil {
		return nil, fmt.Errorf("dlp top vendors: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var vs DLPVendorStat
		if err := rows.Scan(&vs.AIVendor, &vs.EventCount); err != nil {
			return nil, err
		}
		summary.TopAIVendors = append(summary.TopAIVendors, vs)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	devRows, err := s.DB.QueryContext(ctx, `
		SELECT d.device_id, COUNT(*) as cnt,
		       COALESCE((SELECT source_app FROM dlp_events d2 WHERE d2.device_id = d.device_id AND d2.org_id = $1 AND source_app IS NOT NULL LIMIT 1), '')
		FROM dlp_events d
		WHERE d.org_id = $1 AND d.timestamp >= $2 AND d.timestamp <= $3
		GROUP BY d.device_id ORDER BY cnt DESC LIMIT 10`,
		orgID, from, to,
	)
	if err != nil {
		return nil, fmt.Errorf("dlp top devices: %w", err)
	}
	defer devRows.Close()
	for devRows.Next() {
		var ds DLPDeviceStat
		if err := devRows.Scan(&ds.DeviceID, &ds.EventCount, &ds.SourceApp); err != nil {
			return nil, err
		}
		summary.TopDevices = append(summary.TopDevices, ds)
	}

	return summary, devRows.Err()
}

// GetDLPTimeSeries returns DLP event counts grouped by time interval.
func (s *Store) GetDLPTimeSeries(ctx context.Context, orgID string, interval string, from, to time.Time) ([]TimeSeriesPoint, error) {
	truncInterval := "hour"
	if interval == "day" {
		truncInterval = "day"
	}

	query := fmt.Sprintf(`
		SELECT date_trunc('%s', timestamp) as bucket,
		       COUNT(*) as total,
		       COUNT(*) FILTER (WHERE 'pii' = ANY(match_types)) as pii_count,
		       COUNT(*) FILTER (WHERE 'credentials' = ANY(match_types)) as cred_count,
		       0 as log_only
		FROM dlp_events
		WHERE org_id = $1 AND timestamp >= $2 AND timestamp <= $3
		GROUP BY bucket
		ORDER BY bucket`, truncInterval)

	rows, err := s.DB.QueryContext(ctx, query, orgID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var points []TimeSeriesPoint
	for rows.Next() {
		var p TimeSeriesPoint
		if err := rows.Scan(&p.Bucket, &p.Total, &p.Allowed, &p.Blocked, &p.LogOnly); err != nil {
			return nil, err
		}
		points = append(points, p)
	}
	return points, rows.Err()
}

// ensure sql and pq imports are used.
var _ = sql.ErrNoRows
var _ = pq.Array
