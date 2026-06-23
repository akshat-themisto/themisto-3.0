package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

const (
	fleetConnectedWindow = 90 * time.Second
	fleetDegradedWindow  = 5 * time.Minute
)

type FleetDevice struct {
	DeviceID             string                 `json:"device_id"`
	DeviceName           string                 `json:"device_name"`
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
	AgentVersion       string `json:"agent_version"`
	GatewayConnected   bool   `json:"gateway_connected"`
	ProxyListenerAlive bool   `json:"proxy_listener_alive"`
	ProxyIntegrity     string `json:"proxy_integrity"`
	PromptCapture      string `json:"prompt_capture"`
	SemanticClassifier string `json:"semantic_classifier"`
	ServiceStatus      string `json:"service_status"`
	PolicyVersion      string `json:"policy_version"`
	UptimeSeconds      int64  `json:"uptime_seconds"`
}

func (s *Store) ListOperatorFleet(ctx context.Context, now time.Time) ([]FleetDevice, FleetSummary, error) {
	return s.listFleet(ctx, "", now)
}

func (s *Store) ListFleet(ctx context.Context, orgID string, now time.Time) ([]FleetDevice, FleetSummary, error) {
	return s.listFleet(ctx, orgID, now)
}

func (s *Store) listFleet(ctx context.Context, orgID string, now time.Time) ([]FleetDevice, FleetSummary, error) {
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
		ORDER BY o.name, d.device_name`, orgID)
	if err != nil {
		return nil, FleetSummary{}, err
	}
	defer rows.Close()

	devices := []FleetDevice{}
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
			return nil, FleetSummary{}, err
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
		device.ServiceStatus = heartbeat.ServiceStatus
		device.PolicyVersion = heartbeat.PolicyVersion
		device.UptimeSeconds = heartbeat.UptimeSeconds
		device.Connectivity, device.HealthReason = deriveFleetConnectivity(device, now)
		updateFleetSummary(&summary, device)
		devices = append(devices, device)
	}
	return devices, summary, rows.Err()
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
			serviceUnhealthy(device.ServiceStatus) {
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
