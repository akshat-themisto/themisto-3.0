package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// ── Dashboard Stats ─────────────────────────────────────────────────────────

type DashboardStats struct {
	TotalDevices    int `json:"total_devices"`
	ActiveDevices   int `json:"active_devices"`
	PendingDevices  int `json:"pending_devices"`
	ActiveCerts     int `json:"active_certs"`
	ExpiringCerts   int `json:"expiring_certs"`
	RevokedCerts    int `json:"revoked_certs"`
	TotalRequests   int `json:"total_requests"`
	BlockedRequests int `json:"blocked_requests"`
	RequestsToday   int `json:"requests_today"`
}

func (s *Store) GetDashboardStats(ctx context.Context, orgID string) (*DashboardStats, error) {
	stats := &DashboardStats{}

	err := s.DB.QueryRowContext(ctx,
		`SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE status = 'active'),
			COUNT(*) FILTER (WHERE status = 'pending')
		 FROM devices WHERE org_id = $1`, orgID,
	).Scan(&stats.TotalDevices, &stats.ActiveDevices, &stats.PendingDevices)
	if err != nil {
		return nil, fmt.Errorf("device stats: %w", err)
	}

	err = s.DB.QueryRowContext(ctx,
		`SELECT
			COUNT(*) FILTER (WHERE status = 'active'),
			COUNT(*) FILTER (WHERE status = 'active' AND not_after < now() + interval '30 days'),
			COUNT(*) FILTER (WHERE status = 'revoked')
		 FROM certificates WHERE org_id = $1`, orgID,
	).Scan(&stats.ActiveCerts, &stats.ExpiringCerts, &stats.RevokedCerts)
	if err != nil {
		return nil, fmt.Errorf("cert stats: %w", err)
	}

	err = s.DB.QueryRowContext(ctx,
		`SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE policy_decision = 'block'),
			COUNT(*) FILTER (WHERE timestamp >= CURRENT_DATE)
		 FROM telemetry_events WHERE org_id = $1`, orgID,
	).Scan(&stats.TotalRequests, &stats.BlockedRequests, &stats.RequestsToday)
	if err != nil {
		return nil, fmt.Errorf("telemetry stats: %w", err)
	}

	return stats, nil
}

// ── Audit Log ───────────────────────────────────────────────────────────────

type AuditLogEntry struct {
	ID           int64                  `json:"id"`
	Timestamp    time.Time              `json:"timestamp"`
	ActorType    string                 `json:"actor_type"`
	ActorID      string                 `json:"actor_id"`
	OrgID        *string                `json:"org_id,omitempty"`
	Action       string                 `json:"action"`
	ResourceType string                 `json:"resource_type"`
	ResourceID   string                 `json:"resource_id"`
	Details      map[string]interface{} `json:"details,omitempty"`
	IPAddress    *string                `json:"ip_address,omitempty"`
}

type AuditLogFilter struct {
	OrgID     string
	Action    string
	ActorType string
	DateFrom  *time.Time
	DateTo    *time.Time
	Page      int
	Limit     int
}

func (s *Store) ListAuditLog(ctx context.Context, filter AuditLogFilter) ([]AuditLogEntry, int, error) {
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
		where += fmt.Sprintf(" AND org_id = $%d", argN)
		args = append(args, filter.OrgID)
		argN++
	}
	if filter.Action != "" {
		where += fmt.Sprintf(" AND action = $%d", argN)
		args = append(args, filter.Action)
		argN++
	}
	if filter.ActorType != "" {
		where += fmt.Sprintf(" AND actor_type = $%d", argN)
		args = append(args, filter.ActorType)
		argN++
	}
	if filter.DateFrom != nil {
		where += fmt.Sprintf(" AND timestamp >= $%d", argN)
		args = append(args, *filter.DateFrom)
		argN++
	}
	if filter.DateTo != nil {
		where += fmt.Sprintf(" AND timestamp <= $%d", argN)
		args = append(args, *filter.DateTo)
		argN++
	}

	var total int
	countQuery := "SELECT COUNT(*) FROM audit_log " + where
	if err := s.DB.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	offset := (filter.Page - 1) * filter.Limit
	query := fmt.Sprintf(
		"SELECT id, timestamp, actor_type, actor_id, org_id, action, resource_type, resource_id, details, ip_address FROM audit_log %s ORDER BY timestamp DESC LIMIT $%d OFFSET $%d",
		where, argN, argN+1,
	)
	args = append(args, filter.Limit, offset)

	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var entries []AuditLogEntry
	for rows.Next() {
		var e AuditLogEntry
		var detailsJSON sql.NullString
		if err := rows.Scan(&e.ID, &e.Timestamp, &e.ActorType, &e.ActorID, &e.OrgID,
			&e.Action, &e.ResourceType, &e.ResourceID, &detailsJSON, &e.IPAddress); err != nil {
			return nil, 0, err
		}
		if detailsJSON.Valid && detailsJSON.String != "" {
			json.Unmarshal([]byte(detailsJSON.String), &e.Details)
		}
		entries = append(entries, e)
	}
	return entries, total, rows.Err()
}

// ── Telemetry ───────────────────────────────────────────────────────────────

type TelemetryEvent struct {
	ID             int64     `json:"id"`
	Timestamp      time.Time `json:"timestamp"`
	DeviceID       string    `json:"device_id"`
	OrgID          string    `json:"org_id"`
	RequestMethod  string    `json:"request_method"`
	RequestHost    string    `json:"request_host"`
	RequestPath    *string   `json:"request_path,omitempty"`
	RequestPort    *int      `json:"request_port,omitempty"`
	ResponseStatus *int      `json:"response_status,omitempty"`
	LatencyMs      int       `json:"latency_ms"`
	BytesSent      int64     `json:"bytes_sent"`
	BytesReceived  int64     `json:"bytes_received"`
	PolicyDecision string    `json:"policy_decision"`
	MatchedRuleID  *string   `json:"matched_rule_id,omitempty"`
	PolicyVersion  *string   `json:"policy_version,omitempty"`
	AgentVersion   *string   `json:"agent_version,omitempty"`
	SourceApp      *string   `json:"source_app,omitempty"`
	OS             *string   `json:"os,omitempty"`
}

type TelemetryFilter struct {
	OrgID          string
	DeviceID       string
	RequestHost    string
	PolicyDecision string
	DateFrom       *time.Time
	DateTo         *time.Time
	Page           int
	Limit          int
}

func (s *Store) ListTelemetryEvents(ctx context.Context, filter TelemetryFilter) ([]TelemetryEvent, int, error) {
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
		where += fmt.Sprintf(" AND org_id = $%d", argN)
		args = append(args, filter.OrgID)
		argN++
	}
	if filter.DeviceID != "" {
		where += fmt.Sprintf(" AND device_id = $%d", argN)
		args = append(args, filter.DeviceID)
		argN++
	}
	if filter.RequestHost != "" {
		where += fmt.Sprintf(" AND request_host ILIKE $%d", argN)
		args = append(args, "%"+filter.RequestHost+"%")
		argN++
	}
	if filter.PolicyDecision != "" {
		where += fmt.Sprintf(" AND policy_decision = $%d", argN)
		args = append(args, filter.PolicyDecision)
		argN++
	}
	if filter.DateFrom != nil {
		where += fmt.Sprintf(" AND timestamp >= $%d", argN)
		args = append(args, *filter.DateFrom)
		argN++
	}
	if filter.DateTo != nil {
		where += fmt.Sprintf(" AND timestamp <= $%d", argN)
		args = append(args, *filter.DateTo)
		argN++
	}

	var total int
	if err := s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM telemetry_events "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	offset := (filter.Page - 1) * filter.Limit
	query := fmt.Sprintf(
		`SELECT id, timestamp, device_id, org_id, request_method, request_host, request_path,
		        request_port, response_status, latency_ms, bytes_sent, bytes_received,
		        policy_decision, matched_rule_id, policy_version, agent_version, source_app, os
		 FROM telemetry_events %s ORDER BY timestamp DESC LIMIT $%d OFFSET $%d`,
		where, argN, argN+1,
	)
	args = append(args, filter.Limit, offset)

	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var events []TelemetryEvent
	for rows.Next() {
		var e TelemetryEvent
		if err := rows.Scan(&e.ID, &e.Timestamp, &e.DeviceID, &e.OrgID, &e.RequestMethod,
			&e.RequestHost, &e.RequestPath, &e.RequestPort, &e.ResponseStatus, &e.LatencyMs,
			&e.BytesSent, &e.BytesReceived, &e.PolicyDecision, &e.MatchedRuleID,
			&e.PolicyVersion, &e.AgentVersion, &e.SourceApp, &e.OS); err != nil {
			return nil, 0, err
		}
		events = append(events, e)
	}
	return events, total, rows.Err()
}

type TimeSeriesPoint struct {
	Bucket  time.Time `json:"bucket"`
	Total   int       `json:"total"`
	Allowed int       `json:"allowed"`
	Blocked int       `json:"blocked"`
	LogOnly int       `json:"log_only"`
}

func (s *Store) GetTelemetryTimeSeries(ctx context.Context, orgID string, interval string, from, to time.Time) ([]TimeSeriesPoint, error) {
	truncInterval := "hour"
	if interval == "day" {
		truncInterval = "day"
	}

	query := fmt.Sprintf(`
		SELECT date_trunc('%s', timestamp) as bucket,
		       COUNT(*) as total,
		       COUNT(*) FILTER (WHERE policy_decision = 'allow') as allowed,
		       COUNT(*) FILTER (WHERE policy_decision = 'block') as blocked,
		       COUNT(*) FILTER (WHERE policy_decision = 'log_only') as log_only
		FROM telemetry_events
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

// ── Top Hosts ───────────────────────────────────────────────────────────────

type TopHost struct {
	Host         string `json:"host"`
	RequestCount int    `json:"request_count"`
	BlockedCount int    `json:"blocked_count"`
	AvgLatencyMs int    `json:"avg_latency_ms"`
}

func (s *Store) GetTopHosts(ctx context.Context, orgID string, limit int, from, to time.Time) ([]TopHost, error) {
	if limit < 1 || limit > 50 {
		limit = 10
	}
	rows, err := s.DB.QueryContext(ctx,
		`SELECT request_host,
		        COUNT(*) as request_count,
		        COUNT(*) FILTER (WHERE policy_decision = 'block') as blocked_count,
		        COALESCE(AVG(latency_ms)::int, 0) as avg_latency_ms
		 FROM telemetry_events
		 WHERE org_id = $1 AND timestamp >= $2 AND timestamp <= $3
		 GROUP BY request_host
		 ORDER BY request_count DESC
		 LIMIT $4`, orgID, from, to, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var hosts []TopHost
	for rows.Next() {
		var h TopHost
		if err := rows.Scan(&h.Host, &h.RequestCount, &h.BlockedCount, &h.AvgLatencyMs); err != nil {
			return nil, err
		}
		hosts = append(hosts, h)
	}
	return hosts, rows.Err()
}

// ── Policy Rules ────────────────────────────────────────────────────────────

type PolicyRule struct {
	ID          string    `json:"id"`
	OrgID       string    `json:"org_id"`
	Priority    int       `json:"priority"`
	MatchHost   *string   `json:"match_host,omitempty"`
	MatchPath   *string   `json:"match_path,omitempty"`
	MatchMethod *string   `json:"match_method,omitempty"`
	Decision    string    `json:"decision"`
	Enabled     bool      `json:"enabled"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (s *Store) ListPolicyRules(ctx context.Context, orgID string) ([]PolicyRule, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, org_id, priority, match_host, match_path, match_method, decision, enabled, created_at, updated_at
		 FROM policy_rules WHERE org_id = $1 ORDER BY priority`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []PolicyRule
	for rows.Next() {
		var r PolicyRule
		if err := rows.Scan(&r.ID, &r.OrgID, &r.Priority, &r.MatchHost, &r.MatchPath,
			&r.MatchMethod, &r.Decision, &r.Enabled, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		rules = append(rules, r)
	}
	return rules, rows.Err()
}

func (s *Store) CreatePolicyRule(ctx context.Context, orgID string, priority int, matchHost, matchPath, matchMethod *string, decision string) (*PolicyRule, error) {
	r := &PolicyRule{}
	err := s.DB.QueryRowContext(ctx,
		`INSERT INTO policy_rules (org_id, priority, match_host, match_path, match_method, decision)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id, org_id, priority, match_host, match_path, match_method, decision, enabled, created_at, updated_at`,
		orgID, priority, matchHost, matchPath, matchMethod, decision,
	).Scan(&r.ID, &r.OrgID, &r.Priority, &r.MatchHost, &r.MatchPath, &r.MatchMethod,
		&r.Decision, &r.Enabled, &r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return r, nil
}

func (s *Store) UpdatePolicyRule(ctx context.Context, id string, priority int, matchHost, matchPath, matchMethod *string, decision string, enabled bool) (*PolicyRule, error) {
	r := &PolicyRule{}
	err := s.DB.QueryRowContext(ctx,
		`UPDATE policy_rules
		 SET priority = $1, match_host = $2, match_path = $3, match_method = $4, decision = $5, enabled = $6, updated_at = now()
		 WHERE id = $7
		 RETURNING id, org_id, priority, match_host, match_path, match_method, decision, enabled, created_at, updated_at`,
		priority, matchHost, matchPath, matchMethod, decision, enabled, id,
	).Scan(&r.ID, &r.OrgID, &r.Priority, &r.MatchHost, &r.MatchPath, &r.MatchMethod,
		&r.Decision, &r.Enabled, &r.CreatedAt, &r.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return r, nil
}

func (s *Store) DeletePolicyRule(ctx context.Context, id string) error {
	result, err := s.DB.ExecContext(ctx, `DELETE FROM policy_rules WHERE id = $1`, id)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ── Organization Settings ───────────────────────────────────────────────────

func (s *Store) GetOrganizationByID(ctx context.Context, id string) (*Organization, error) {
	o := &Organization{}
	err := s.DB.QueryRowContext(ctx,
		`SELECT id, name, slug, api_key_hash, status,
		        COALESCE(public_backend_url, ''), COALESCE(public_gateway_url, ''),
		        COALESCE(status_reason, ''), status_updated_at,
		        COALESCE(status_updated_by, ''), created_at, updated_at
		 FROM organizations WHERE id = $1`, id,
	).Scan(
		&o.ID, &o.Name, &o.Slug, &o.APIKeyHash, &o.Status,
		&o.PublicBackendURL, &o.PublicGatewayURL, &o.StatusReason,
		&o.StatusUpdatedAt, &o.StatusUpdatedBy, &o.CreatedAt, &o.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return o, nil
}

func (s *Store) UpdateOrganization(ctx context.Context, id, name, slug string) error {
	_, err := s.DB.ExecContext(ctx,
		`UPDATE organizations SET name = $1, slug = $2, updated_at = now() WHERE id = $3`,
		name, slug, id)
	return err
}

// ── AI Usage ─────────────────────────────────────────────────────────────────

// AIVendorStat holds per-vendor request counts.
type AIVendorStat struct {
	Vendor         string `json:"vendor"`
	Name           string `json:"name"`
	Category       string `json:"category"`
	RequestCount   int    `json:"request_count"`
	BlockedCount   int    `json:"blocked_count"`
	DeviceCount    int    `json:"device_count"`
	BytesSentTotal int64  `json:"bytes_sent_total"`
}

// AIUsageSummary is the top-level AI usage response.
type AIUsageSummary struct {
	TotalAIRequests   int            `json:"total_ai_requests"`
	UniqueAIVendors   int            `json:"unique_ai_vendors"`
	UniqueAIDevices   int            `json:"unique_ai_devices"`
	UnsanctionedCount int            `json:"unsanctioned_count"`
	VendorBreakdown   []AIVendorStat `json:"vendor_breakdown"`
	CategoryBreakdown []CategoryStat `json:"category_breakdown"`
}

// CategoryStat holds per-category counts.
type CategoryStat struct {
	Category     string `json:"category"`
	RequestCount int    `json:"request_count"`
	DeviceCount  int    `json:"device_count"`
}

func (s *Store) GetAIUsageSummary(ctx context.Context, orgID string, from, to time.Time) (*AIUsageSummary, error) {
	summary := &AIUsageSummary{}

	// Overall AI stats.
	err := s.DB.QueryRowContext(ctx, `
		SELECT
			COUNT(*),
			COUNT(DISTINCT ai_vendor) FILTER (WHERE ai_vendor IS NOT NULL),
			COUNT(DISTINCT device_id) FILTER (WHERE ai_vendor IS NOT NULL)
		FROM telemetry_events
		WHERE org_id = $1 AND timestamp >= $2 AND timestamp <= $3
		  AND ai_vendor IS NOT NULL`,
		orgID, from, to,
	).Scan(&summary.TotalAIRequests, &summary.UniqueAIVendors, &summary.UniqueAIDevices)
	if err != nil {
		return nil, fmt.Errorf("ai usage summary: %w", err)
	}

	// Vendor breakdown.
	rows, err := s.DB.QueryContext(ctx, `
		SELECT
			ai_vendor,
			service_category,
			COUNT(*) as request_count,
			COUNT(*) FILTER (WHERE policy_decision = 'block') as blocked_count,
			COUNT(DISTINCT device_id) as device_count,
			COALESCE(SUM(bytes_sent), 0) as bytes_sent_total
		FROM telemetry_events
		WHERE org_id = $1 AND timestamp >= $2 AND timestamp <= $3
		  AND ai_vendor IS NOT NULL
		GROUP BY ai_vendor, service_category
		ORDER BY request_count DESC`,
		orgID, from, to,
	)
	if err != nil {
		return nil, fmt.Errorf("ai vendor breakdown: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var stat AIVendorStat
		if err := rows.Scan(&stat.Vendor, &stat.Category, &stat.RequestCount,
			&stat.BlockedCount, &stat.DeviceCount, &stat.BytesSentTotal); err != nil {
			return nil, err
		}
		summary.VendorBreakdown = append(summary.VendorBreakdown, stat)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Category breakdown.
	catRows, err := s.DB.QueryContext(ctx, `
		SELECT
			service_category,
			COUNT(*) as request_count,
			COUNT(DISTINCT device_id) as device_count
		FROM telemetry_events
		WHERE org_id = $1 AND timestamp >= $2 AND timestamp <= $3
		  AND service_category IS NOT NULL
		GROUP BY service_category
		ORDER BY request_count DESC`,
		orgID, from, to,
	)
	if err != nil {
		return nil, fmt.Errorf("ai category breakdown: %w", err)
	}
	defer catRows.Close()
	for catRows.Next() {
		var stat CategoryStat
		if err := catRows.Scan(&stat.Category, &stat.RequestCount, &stat.DeviceCount); err != nil {
			return nil, err
		}
		summary.CategoryBreakdown = append(summary.CategoryBreakdown, stat)
	}

	return summary, catRows.Err()
}

// AIDeviceStat holds AI request counts per device.
type AIDeviceStat struct {
	DeviceID     string `json:"device_id"`
	RequestCount int    `json:"request_count"`
	VendorCount  int    `json:"vendor_count"`
}

// GetTopAIDevices returns the devices with the most AI service requests.
func (s *Store) GetTopAIDevices(ctx context.Context, orgID string, limit int, from, to time.Time) ([]AIDeviceStat, error) {
	if limit < 1 || limit > 50 {
		limit = 10
	}
	rows, err := s.DB.QueryContext(ctx, `
		SELECT device_id, COUNT(*) as request_count, COUNT(DISTINCT ai_vendor) as vendor_count
		FROM telemetry_events
		WHERE org_id = $1 AND timestamp >= $2 AND timestamp <= $3
		  AND ai_vendor IS NOT NULL
		GROUP BY device_id
		ORDER BY request_count DESC
		LIMIT $4`,
		orgID, from, to, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var stats []AIDeviceStat
	for rows.Next() {
		var st AIDeviceStat
		if err := rows.Scan(&st.DeviceID, &st.RequestCount, &st.VendorCount); err != nil {
			return nil, err
		}
		stats = append(stats, st)
	}
	return stats, rows.Err()
}
