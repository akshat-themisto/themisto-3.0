package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"math"
	"strings"
	"time"
)

const (
	fleetConnectedWindow = 90 * time.Second
	fleetDegradedWindow  = 5 * time.Minute
)

type FleetDevice struct {
	DeviceID             string                 `json:"device_id"`
	DeviceName           string                 `json:"device_name"`
	Hostname             string                 `json:"hostname,omitempty"`
	AgentUser            string                 `json:"agent_user,omitempty"`
	OrganizationID       string                 `json:"organization_id"`
	OrganizationName     string                 `json:"organization_name"`
	OrganizationStatus   string                 `json:"organization_status"`
	OS                   string                 `json:"os"`
	AgentVersion         string                 `json:"agent_version"`
	EnrollmentStatus     string                 `json:"enrollment_status"`
	Connectivity         string                 `json:"connectivity"`
	HealthReason         string                 `json:"health_reason"`
	LastSeenAt           *time.Time             `json:"last_seen_at,omitempty"`
	LastHeartbeatAt      *time.Time             `json:"last_heartbeat_at,omitempty"`
	LastTamperAt         *time.Time             `json:"last_tamper_at,omitempty"`
	LastHealthyAt        *time.Time             `json:"last_healthy_at,omitempty"`
	LastStoppedAt        *time.Time             `json:"last_stopped_at,omitempty"`
	CertificateStatus    string                 `json:"certificate_status"`
	CertificateExpiresAt *time.Time             `json:"certificate_expires_at,omitempty"`
	GatewayConnected     bool                   `json:"gateway_connected"`
	ProxyListenerAlive   bool                   `json:"proxy_listener_alive"`
	ProxyIntegrity       string                 `json:"proxy_integrity"`
	PromptCapture        string                 `json:"prompt_capture"`
	SemanticClassifier   string                 `json:"semantic_classifier"`
	BrowserProtection    string                 `json:"browser_protection,omitempty"`
	PolicyFresh          bool                   `json:"policy_fresh"`
	ClassifierRequired   bool                   `json:"classifier_required"`
	ClassifierHealthy    bool                   `json:"classifier_healthy"`
	PromptEnforcement    string                 `json:"prompt_enforcement_mode,omitempty"`
	EffectiveEnforcement string                 `json:"effective_prompt_enforcement_mode,omitempty"`
	ProtectionState      string                 `json:"protection_state,omitempty"`
	SurfaceStates        map[string]string      `json:"surface_states,omitempty"`
	ServiceStatus        string                 `json:"service_status"`
	PolicyVersion        string                 `json:"policy_version"`
	UptimeSeconds        int64                  `json:"uptime_seconds"`
	LatestEventType      string                 `json:"latest_event_type"`
	LatestEventSeverity  string                 `json:"latest_event_severity"`
	LatestEventData      map[string]interface{} `json:"latest_event_data,omitempty"`
}

type FleetSummary struct {
	Total            int `json:"total"`
	Connected        int `json:"connected"`
	Degraded         int `json:"degraded"`
	Offline          int `json:"offline"`
	NeverSeen        int `json:"never_seen"`
	SuspectedTamper  int `json:"suspected_tamper"`
	ManagedInactive  int `json:"managed_inactive"`
	ClassifierIssues int `json:"classifier_issues"`
	Protected        int `json:"protected"`
	Unprotected      int `json:"unprotected"`
	MonitorOnly      int `json:"monitor_only"`
}

type FleetFilter struct {
	OrgID  string
	Status string
	Query  string
	Page   int
	Limit  int
}

type FleetSignal struct {
	ID               int64                  `json:"id"`
	Timestamp        time.Time              `json:"timestamp"`
	DeviceID         string                 `json:"device_id"`
	DeviceName       string                 `json:"device_name"`
	OrganizationID   string                 `json:"organization_id"`
	OrganizationName string                 `json:"organization_name"`
	EventType        string                 `json:"event_type"`
	Severity         string                 `json:"severity"`
	Data             map[string]interface{} `json:"data"`
}

type fleetHeartbeatData struct {
	AgentVersion       string            `json:"agent_version"`
	GatewayConnected   bool              `json:"gateway_connected"`
	ProxyListenerAlive bool              `json:"proxy_listener_alive"`
	ProxyIntegrity     string            `json:"proxy_integrity"`
	PromptCapture      string            `json:"prompt_capture"`
	SemanticClassifier string            `json:"semantic_classifier"`
	BrowserProtection  string            `json:"browser_protection"`
	PolicyFresh        bool              `json:"policy_fresh"`
	ClassifierRequired bool              `json:"classifier_required"`
	ClassifierHealthy  bool              `json:"classifier_healthy"`
	PromptEnforcement  string            `json:"prompt_enforcement_mode"`
	EffectiveEnforce   string            `json:"effective_prompt_enforcement_mode"`
	ProtectionState    string            `json:"protection_state"`
	ServiceStatus      string            `json:"service_status"`
	PolicyVersion      string            `json:"policy_version"`
	UptimeSeconds      int64             `json:"uptime_seconds"`
	Hostname           string            `json:"hostname"`
	AgentUser          string            `json:"agent_user"`
	SurfaceStates      map[string]string `json:"surface_states"`
}

func (s *Store) ListOperatorFleet(ctx context.Context, now time.Time) ([]FleetDevice, FleetSummary, error) {
	devices, summary, _, err := s.listFleet(ctx, FleetFilter{}, now)
	return devices, summary, err
}

func (s *Store) ListFleet(ctx context.Context, filter FleetFilter, now time.Time) ([]FleetDevice, FleetSummary, int, error) {
	return s.listFleet(ctx, filter, now)
}

func (s *Store) listFleet(ctx context.Context, filter FleetFilter, now time.Time) ([]FleetDevice, FleetSummary, int, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT
			d.id::text, d.device_name, o.id::text, o.name, o.status,
			d.os, COALESCE(d.agent_version, ''), d.status,
			c.status, c.not_after,
			status_rollup.last_event_at, status_rollup.last_heartbeat_at,
			status_rollup.last_tamper_at, status_rollup.last_healthy_at,
			status_rollup.last_stopped_at, traffic.last_traffic_at,
			COALESCE(latest.event_type, ''), COALESCE(latest.severity, ''),
			COALESCE(latest.data, '{}'::jsonb), COALESCE(heartbeat.data, '{}'::jsonb)
		FROM devices d
		JOIN organizations o ON o.id = d.org_id
		LEFT JOIN certificates c ON c.device_id = d.id AND c.status = 'active'
		LEFT JOIN LATERAL (
			SELECT
				MAX(timestamp) AS last_event_at,
				MAX(timestamp) FILTER (WHERE event_type = 'agent.heartbeat') AS last_heartbeat_at,
				MAX(timestamp) FILTER (WHERE event_type = 'agent.stopped') AS last_stopped_at,
				MAX(timestamp) FILTER (WHERE severity = 'critical') AS last_tamper_at,
				MAX(timestamp) FILTER (
					WHERE event_type = 'agent.heartbeat'
					  AND data->>'proxy_integrity' = 'ok'
					  AND data->>'proxy_listener_alive' = 'true'
				) AS last_healthy_at
			FROM agent_status_events
			WHERE device_id = d.id::text
		) status_rollup ON true
		LEFT JOIN LATERAL (
			SELECT event_type, severity, data
			FROM agent_status_events
			WHERE device_id = d.id::text
			ORDER BY timestamp DESC
			LIMIT 1
		) latest ON true
		LEFT JOIN LATERAL (
			SELECT data
			FROM agent_status_events
			WHERE device_id = d.id::text AND event_type = 'agent.heartbeat'
			ORDER BY timestamp DESC
			LIMIT 1
		) heartbeat ON true
		LEFT JOIN LATERAL (
			SELECT MAX(timestamp) AS last_traffic_at
			FROM telemetry_events
			WHERE device_id = d.id::text
		) traffic ON true
		WHERE ($1 = '' OR d.org_id::text = $1)
		ORDER BY o.name, d.device_name`, strings.TrimSpace(filter.OrgID))
	if err != nil {
		return nil, FleetSummary{}, 0, err
	}
	defer rows.Close()

	allDevices := []FleetDevice{}
	var summary FleetSummary
	for rows.Next() {
		var (
			device                   FleetDevice
			lastEvent, lastHeartbeat sql.NullTime
			lastTamper, lastHealthy  sql.NullTime
			lastStopped, lastTraffic sql.NullTime
			certStatus               sql.NullString
			certExpiry               sql.NullTime
			latestRaw, heartbeatRaw  []byte
		)
		if err := rows.Scan(
			&device.DeviceID, &device.DeviceName, &device.OrganizationID,
			&device.OrganizationName, &device.OrganizationStatus, &device.OS,
			&device.AgentVersion, &device.EnrollmentStatus,
			&certStatus, &certExpiry, &lastEvent, &lastHeartbeat, &lastTamper,
			&lastHealthy, &lastStopped, &lastTraffic, &device.LatestEventType,
			&device.LatestEventSeverity, &latestRaw, &heartbeatRaw,
		); err != nil {
			return nil, FleetSummary{}, 0, err
		}
		device.CertificateStatus = certStatus.String
		device.CertificateExpiresAt = nullTimePtr(certExpiry)
		device.LastHeartbeatAt = nullTimePtr(lastHeartbeat)
		device.LastTamperAt = nullTimePtr(lastTamper)
		device.LastHealthyAt = nullTimePtr(lastHealthy)
		device.LastStoppedAt = nullTimePtr(lastStopped)
		device.LastSeenAt = latestTime(nullTimePtr(lastEvent), nullTimePtr(lastTraffic))
		_ = json.Unmarshal(latestRaw, &device.LatestEventData)

		var heartbeat fleetHeartbeatData
		_ = json.Unmarshal(heartbeatRaw, &heartbeat)
		if heartbeat.AgentVersion != "" {
			device.AgentVersion = heartbeat.AgentVersion
		}
		device.GatewayConnected = heartbeat.GatewayConnected
		device.ProxyListenerAlive = heartbeat.ProxyListenerAlive
		device.ProxyIntegrity = heartbeat.ProxyIntegrity
		device.PromptCapture = heartbeat.PromptCapture
		device.SemanticClassifier = heartbeat.SemanticClassifier
		device.BrowserProtection = heartbeat.BrowserProtection
		device.PolicyFresh = heartbeat.PolicyFresh
		device.ClassifierRequired = heartbeat.ClassifierRequired
		device.ClassifierHealthy = heartbeat.ClassifierHealthy
		device.PromptEnforcement = heartbeat.PromptEnforcement
		device.EffectiveEnforcement = heartbeat.EffectiveEnforce
		device.ProtectionState = heartbeat.ProtectionState
		device.SurfaceStates = heartbeat.SurfaceStates
		device.ServiceStatus = heartbeat.ServiceStatus
		device.PolicyVersion = heartbeat.PolicyVersion
		device.UptimeSeconds = heartbeat.UptimeSeconds
		device.Hostname = heartbeat.Hostname
		device.AgentUser = heartbeat.AgentUser
		device.Connectivity, device.HealthReason = deriveFleetConnectivity(device, now)
		updateFleetSummary(&summary, device)
		allDevices = append(allDevices, device)
	}
	if err := rows.Err(); err != nil {
		return nil, FleetSummary{}, 0, err
	}

	filtered := filterFleetDevices(allDevices, filter)
	total := len(filtered)
	filtered = paginateFleetDevices(filtered, filter.Page, filter.Limit)
	return filtered, summary, total, nil
}

func (s *Store) ListOperatorFleetSignals(ctx context.Context, limit int) ([]FleetSignal, error) {
	return s.listFleetSignals(ctx, "", limit)
}

func (s *Store) ListFleetSignals(ctx context.Context, orgID string, limit int) ([]FleetSignal, error) {
	return s.listFleetSignals(ctx, orgID, limit)
}

func (s *Store) listFleetSignals(ctx context.Context, orgID string, limit int) ([]FleetSignal, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.DB.QueryContext(ctx, `
		SELECT e.id, e.timestamp, e.device_id, COALESCE(d.device_name, e.device_id),
		       e.org_id, COALESCE(o.name, e.org_id), e.event_type, e.severity, e.data
		FROM agent_status_events e
		LEFT JOIN devices d ON d.id::text = e.device_id
		LEFT JOIN organizations o ON o.id::text = e.org_id
		WHERE ($1 = '' OR e.org_id = $1)
		  AND (e.severity != 'info'
		   OR e.event_type IN ('agent.stopped', 'proxy.remediated'))
		ORDER BY e.timestamp DESC
		LIMIT $2`, orgID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	signals := []FleetSignal{}
	for rows.Next() {
		var signal FleetSignal
		var raw []byte
		if err := rows.Scan(&signal.ID, &signal.Timestamp, &signal.DeviceID,
			&signal.DeviceName, &signal.OrganizationID, &signal.OrganizationName,
			&signal.EventType, &signal.Severity, &raw); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(raw, &signal.Data)
		signals = append(signals, signal)
	}
	return signals, rows.Err()
}

func deriveFleetConnectivity(device FleetDevice, now time.Time) (string, string) {
	if device.OrganizationStatus != "active" || device.EnrollmentStatus != "active" {
		return "managed_inactive", "Organization or device is not active"
	}
	if device.LastTamperAt != nil && (device.LastHealthyAt == nil || device.LastTamperAt.After(*device.LastHealthyAt)) {
		return "suspected_tamper", "Integrity incident has not been followed by a healthy heartbeat"
	}
	if device.LastStoppedAt != nil && (device.LastHeartbeatAt == nil || device.LastStoppedAt.After(*device.LastHeartbeatAt)) {
		return "offline", "Agent reported a clean shutdown"
	}
	if device.LastSeenAt == nil {
		return "never_seen", "No operational heartbeat or traffic has been received"
	}
	age := now.Sub(*device.LastSeenAt)
	if age <= fleetConnectedWindow {
		if !device.GatewayConnected || !device.ProxyListenerAlive || device.ProxyIntegrity != "ok" ||
			device.PromptCapture != "healthy" || classifierUnhealthy(device.SemanticClassifier) ||
			serviceUnhealthy(device.ServiceStatus) || protectionDegraded(device.ProtectionState) {
			return "degraded", "Recent heartbeat reports a component problem"
		}
		return "connected", "Operational heartbeat is current"
	}
	if age <= fleetDegradedWindow {
		return "degraded", "Heartbeat is delayed"
	}
	return "offline", "No signal received within the offline threshold"
}

func classifierUnhealthy(status string) bool {
	return status != "" && status != "healthy" && status != "disabled"
}

func serviceUnhealthy(status string) bool {
	return status != "" && status != "running"
}

func protectionDegraded(state string) bool {
	switch state {
	case "", "protected":
		return false
	case "monitor_only", "unprotected", "degraded", "tampered":
		return true
	default:
		return true
	}
}

func updateFleetSummary(summary *FleetSummary, device FleetDevice) {
	summary.Total++
	switch device.Connectivity {
	case "connected":
		summary.Connected++
	case "degraded":
		summary.Degraded++
	case "offline":
		summary.Offline++
	case "never_seen":
		summary.NeverSeen++
	case "suspected_tamper":
		summary.SuspectedTamper++
	case "managed_inactive":
		summary.ManagedInactive++
	}
	if classifierUnhealthy(device.SemanticClassifier) {
		summary.ClassifierIssues++
	}
	switch device.ProtectionState {
	case "protected":
		summary.Protected++
	case "monitor_only":
		summary.MonitorOnly++
	case "unprotected", "degraded", "tampered":
		summary.Unprotected++
	}
}

func filterFleetDevices(devices []FleetDevice, filter FleetFilter) []FleetDevice {
	status := strings.TrimSpace(strings.ToLower(filter.Status))
	query := strings.TrimSpace(strings.ToLower(filter.Query))
	if status == "" && query == "" {
		return devices
	}
	out := make([]FleetDevice, 0, len(devices))
	for _, device := range devices {
		if status != "" && strings.ToLower(device.Connectivity) != status {
			continue
		}
		if query != "" && !fleetDeviceMatchesQuery(device, query) {
			continue
		}
		out = append(out, device)
	}
	return out
}

func fleetDeviceMatchesQuery(device FleetDevice, query string) bool {
	values := []string{
		device.DeviceName,
		device.DeviceID,
		device.Hostname,
		device.AgentUser,
		device.OS,
		device.AgentVersion,
		device.PolicyVersion,
		device.OrganizationName,
	}
	for _, value := range values {
		if strings.Contains(strings.ToLower(value), query) {
			return true
		}
	}
	return false
}

func paginateFleetDevices(devices []FleetDevice, page, limit int) []FleetDevice {
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	if page <= 0 {
		page = 1
	}
	if len(devices) == 0 {
		return []FleetDevice{}
	}
	start := (page - 1) * limit
	if start >= len(devices) {
		lastPage := int(math.Ceil(float64(len(devices)) / float64(limit)))
		if lastPage < 1 {
			lastPage = 1
		}
		start = (lastPage - 1) * limit
	}
	end := start + limit
	if end > len(devices) {
		end = len(devices)
	}
	return devices[start:end]
}

func nullTimePtr(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	result := value.Time
	return &result
}

func latestTime(values ...*time.Time) *time.Time {
	var latest *time.Time
	for _, value := range values {
		if value != nil && (latest == nil || value.After(*latest)) {
			copyValue := *value
			latest = &copyValue
		}
	}
	return latest
}
