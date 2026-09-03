package handler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/themisto/gateway/internal/metrics"
	"github.com/themisto/gateway/internal/mtls"
	"github.com/themisto/gateway/internal/policy"
	"github.com/themisto/gateway/internal/telemetry"
	"github.com/themisto/gateway/internal/upstream"
)

type ProxyHandler struct {
	forwarder *upstream.Forwarder
	policy    *policy.Engine
	telem     *telemetry.Buffer
	verifier  *mtls.CertVerifier
	logger    *slog.Logger
}

func NewProxyHandler(
	fwd *upstream.Forwarder,
	pol *policy.Engine,
	telem *telemetry.Buffer,
	verifier *mtls.CertVerifier,
	logger *slog.Logger,
) *ProxyHandler {
	return &ProxyHandler{
		forwarder: fwd,
		policy:    pol,
		telem:     telem,
		verifier:  verifier,
		logger:    logger,
	}
}

func (h *ProxyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		http.Error(w, "client certificate required", http.StatusForbidden)
		return
	}

	clientCert := r.TLS.PeerCertificates[0]
	serial := clientCert.SerialNumber.Text(16)

	revoked, err := h.verifier.IsRevoked(serial)
	if err != nil {
		h.logger.Warn("revocation check error, applying fail mode", "serial", serial, "error", err)
		metrics.RevocationCheckErrors.Inc()
	}
	if revoked {
		http.Error(w, "certificate revoked", http.StatusForbidden)
		return
	}

	ctx := mtls.ContextWithClientInfo(r.Context(), clientCert)
	if verifiedDeviceID, verifiedOrgID, ok := h.verifier.LookupIdentity(serial); ok {
		ctx = mtls.ContextWithVerifiedIdentity(ctx, verifiedDeviceID, verifiedOrgID)
	}
	r = r.WithContext(ctx)

	deviceID := mtls.DeviceIDFromContext(ctx)
	orgID := mtls.OrgIDFromContext(ctx)

	// Route internal agent endpoints before proxy forwarding.
	if r.URL.Path == "/telemetry" && r.Method == http.MethodPost {
		h.handleTelemetry(w, r, deviceID, orgID)
		return
	}

	// If the agent relayed a request, rewrite URL from relay metadata headers.
	if err := rewriteRelayURL(r); err != nil {
		h.logger.Warn("invalid relay target", "error", err)
		http.Error(w, "invalid relay target", http.StatusBadRequest)
		return
	}

	targetHost := r.Host
	targetPath := r.URL.Path

	decision, ruleID := h.policy.Evaluate(orgID, targetHost, targetPath, r.Method)
	if decision == policy.Block {
		http.Error(w, "blocked by policy", http.StatusForbidden)
		elapsed := time.Since(start)
		metrics.RequestsTotal.WithLabelValues(orgID, string(decision), "403").Inc()
		metrics.RequestDuration.WithLabelValues(orgID).Observe(elapsed.Seconds())
		h.emitEvent(deviceID, orgID, r, 403, elapsed, 0, string(decision), ruleID)
		return
	}

	if r.Method == http.MethodConnect {
		status, bytesSent, err := h.forwarder.Tunnel(w, r)
		if err != nil {
			h.logger.Error("upstream tunnel failed", "host", targetHost, "error", err)
			if status == 0 {
				status = http.StatusBadGateway
			}
			if !headersSent(w) {
				http.Error(w, "upstream error", status)
			}
		}

		elapsed := time.Since(start)
		metrics.RequestsTotal.WithLabelValues(orgID, string(decision), fmt.Sprintf("%d", status)).Inc()
		metrics.RequestDuration.WithLabelValues(orgID).Observe(elapsed.Seconds())
		h.emitEvent(deviceID, orgID, r, status, elapsed, bytesSent, string(decision), ruleID)
		return
	}

	if r.URL.Scheme == "" {
		r.URL.Scheme = "https"
	}
	if r.URL.Host == "" {
		r.URL.Host = r.Host
	}

	status, bytesSent, err := h.forwarder.Forward(w, r)
	if err != nil {
		h.logger.Error("upstream forward failed", "host", targetHost, "error", err)
		if !headersSent(w) {
			http.Error(w, "upstream error", http.StatusBadGateway)
			status = http.StatusBadGateway
		}
	}

	elapsed := time.Since(start)
	metrics.RequestsTotal.WithLabelValues(orgID, string(decision), fmt.Sprintf("%d", status)).Inc()
	metrics.RequestDuration.WithLabelValues(orgID).Observe(elapsed.Seconds())
	h.emitEvent(deviceID, orgID, r, status, elapsed, bytesSent, string(decision), ruleID)
}

func rewriteRelayURL(r *http.Request) error {
	originalURL := strings.TrimSpace(r.Header.Get("X-Themisto-Original-URL"))
	if originalURL == "" {
		return nil
	}

	parsed, err := url.Parse(originalURL)
	if err != nil {
		return fmt.Errorf("parse X-Themisto-Original-URL: %w", err)
	}

	// Some clients send origin-form requests (path-only). In that case, the
	// original upstream host is carried separately.
	if parsed.Host == "" {
		originalHost := strings.TrimSpace(r.Header.Get("X-Themisto-Original-Host"))
		if originalHost == "" {
			return fmt.Errorf("missing host in relay metadata")
		}
		parsed.Host = originalHost
	}

	// For relayed non-CONNECT proxy requests, missing scheme means HTTP.
	if parsed.Scheme == "" {
		parsed.Scheme = "http"
	}
	if parsed.Path == "" {
		parsed.Path = "/"
	}

	r.URL = parsed
	r.Host = parsed.Host
	return nil
}

func (h *ProxyHandler) handleTelemetry(w http.ResponseWriter, r *http.Request, deviceID, orgID string) {
	var batch struct {
		Events []struct {
			Name    string `json:"name"`
			Payload struct {
				Data map[string]interface{} `json:"data"`
			} `json:"payload"`
			Timestamp time.Time `json:"ts"`
		} `json:"events"`
	}

	if err := json.NewDecoder(r.Body).Decode(&batch); err != nil {
		h.logger.Error("failed to decode telemetry batch", "error", err)
		http.Error(w, "invalid batch", http.StatusBadRequest)
		return
	}

	accepted := 0
	for _, e := range batch.Events {
		if isAgentStatusEvent(e.Name) {
			timestamp := e.Timestamp
			if timestamp.IsZero() {
				timestamp = time.Now()
			}
			h.telem.EmitAgentStatus(telemetry.AgentStatusEvent{
				Timestamp: timestamp,
				DeviceID:  deviceID,
				OrgID:     orgID,
				EventType: e.Name,
				Severity:  agentStatusSeverity(e.Name, e.Payload.Data),
				Data:      sanitizedAgentStatusData(e.Payload.Data),
			})
			accepted++
			continue
		}
		if e.Name == "ai.activity.v1" {
			activity, err := decodeAIActivity(e.Payload.Data, e.Timestamp, deviceID, orgID)
			if err != nil {
				h.logger.Warn("AI activity rejected", "error", err)
				continue
			}
			h.telem.EmitAIActivity(activity)
			accepted++
			continue
		}
		if e.Name != "proxy.request" && e.Name != "dlp.match" {
			continue
		}

		if e.Name == "dlp.match" {
			data := e.Payload.Data
			if data == nil {
				continue
			}
			if key, found := forbiddenCentralTelemetryField(data); found {
				h.logger.Warn("content-bearing DLP telemetry rejected", "field", key)
				continue
			}
			host, _ := asString(data["host"])
			method, _ := asString(data["method"])
			if host == "" || method == "" {
				continue
			}
			dlpEvt := telemetry.DLPEvent{
				Timestamp:     e.Timestamp,
				DeviceID:      deviceID,
				OrgID:         orgID,
				RequestHost:   host,
				RequestMethod: method,
				ActionTaken:   "alert",
			}
			if dlpEvt.Timestamp.IsZero() {
				dlpEvt.Timestamp = time.Now()
			}
			if v, ok := asString(data["request_id"]); ok {
				dlpEvt.RequestID = v
			}
			if v, ok := asString(data["source_app"]); ok {
				dlpEvt.SourceApp = v
			}
			if v, ok := asString(data["service_category"]); ok {
				dlpEvt.ServiceCategory = v
			}
			if v, ok := asString(data["ai_vendor"]); ok {
				dlpEvt.AIVendor = v
			}
			if v, ok := asInt(data["match_count"]); ok {
				dlpEvt.MatchCount = v
			}
			if v, ok := asInt(data["file_count"]); ok {
				dlpEvt.FileCount = v
			}
			if v, ok := asString(data["policy_rule_id"]); ok {
				dlpEvt.PolicyRuleID = v
			}
			if v, ok := asString(data["reason_code"]); ok {
				dlpEvt.ReasonCode = v
			}
			if v, ok := asString(data["semantic_source"]); ok {
				dlpEvt.SemanticSource = strings.TrimSpace(v)
			}
			if v, ok := asString(data["semantic_category"]); ok {
				dlpEvt.SemanticCategory = strings.TrimSpace(v)
			}
			if v, ok := asFloat64(data["semantic_confidence"]); ok {
				dlpEvt.SemanticConfidence = v
			}
			if v, ok := asBool(data["semantic_ambiguous"]); ok {
				dlpEvt.SemanticAmbiguous = v
			}
			if v, ok := asString(data["severity"]); ok {
				dlpEvt.Severity = strings.ToLower(strings.TrimSpace(v))
			}
			if v, ok := asString(data["classification_reason"]); ok {
				dlpEvt.ClassificationReason = strings.TrimSpace(v)
			}
			if v, ok := asString(data["content_type"]); ok {
				dlpEvt.ContentType = strings.TrimSpace(v)
			}
			if v, ok := asString(data["protocol"]); ok {
				dlpEvt.Protocol = strings.ToLower(strings.TrimSpace(v))
			}
			if v, ok := asBool(data["intercepted_https"]); ok {
				dlpEvt.InterceptedHTTPS = v
			}
			if v, ok := asString(data["inspection_quality"]); ok {
				dlpEvt.InspectionQuality = strings.ToLower(strings.TrimSpace(v))
			}
			if v, ok := asString(data["inspection_skip_reason"]); ok {
				dlpEvt.InspectionSkipReason = strings.TrimSpace(v)
			}
			if v, ok := asString(data["direction"]); ok {
				dlpEvt.Direction = strings.ToLower(strings.TrimSpace(v))
			}
			if v, ok := asString(data["action_taken"]); ok {
				switch strings.ToLower(v) {
				case "alert", "block", "redact":
					dlpEvt.ActionTaken = strings.ToLower(v)
				}
			}
			// match_types and matched_patterns are string slices in JSON.
			if v, ok := data["match_types"].([]interface{}); ok {
				for _, s := range v {
					if str, ok := s.(string); ok {
						dlpEvt.MatchTypes = append(dlpEvt.MatchTypes, str)
					}
				}
			}
			if strings.TrimSpace(dlpEvt.Protocol) == "" {
				dlpEvt.Protocol = "http"
			}
			if strings.TrimSpace(dlpEvt.InspectionQuality) == "" {
				dlpEvt.InspectionQuality = "full"
			}
			if strings.TrimSpace(dlpEvt.Direction) == "" {
				dlpEvt.Direction = "outbound"
			}
			switch dlpEvt.Severity {
			case "critical", "high", "medium", "low":
			default:
				dlpEvt.Severity = "low"
			}
			metrics.DLPProtocolScans.WithLabelValues(dlpEvt.Protocol, dlpEvt.InspectionQuality).Inc()
			h.telem.EmitDLP(dlpEvt)
			accepted++
			continue
		}

		data := e.Payload.Data
		if data == nil {
			continue
		}
		if key, found := forbiddenCentralTelemetryField(data); found {
			h.logger.Warn("content-bearing request telemetry rejected", "field", key)
			continue
		}

		method, ok := asString(data["method"])
		if !ok || method == "" {
			continue
		}
		host, ok := asString(data["host"])
		if !ok || host == "" {
			continue
		}
		decisionRaw, ok := asString(data["decision"])
		if !ok {
			continue
		}
		decision, ok := normalizePolicyDecision(decisionRaw)
		if !ok {
			continue
		}

		event := telemetry.Event{
			Timestamp:      e.Timestamp,
			DeviceID:       deviceID,
			OrgID:          orgID,
			RequestMethod:  method,
			RequestHost:    host,
			PolicyDecision: decision,
		}
		if event.Timestamp.IsZero() {
			event.Timestamp = time.Now()
		}

		// Map payload data back to Event fields
		if v, ok := asInt(data["status"]); ok {
			event.ResponseStatus = v
		}
		if v, ok := asInt(data["latency_ms"]); ok {
			event.LatencyMs = v
		}
		if v, ok := asInt(data["port"]); ok {
			event.RequestPort = v
		}
		if v, ok := asInt64(data["bytes_sent"]); ok {
			event.BytesSent = v
		}
		if v, ok := asInt64(data["bytes_received"]); ok {
			event.BytesReceived = v
		}
		if v, ok := asString(data["rule_id"]); ok && v != "" {
			event.MatchedRuleID = sanitizedRuleID(v)
		}
		if v, ok := asString(data["policy_version"]); ok && v != "" {
			pv := v
			event.PolicyVersion = &pv
		}
		if v, ok := asString(data["agent_version"]); ok && v != "" {
			av := v
			event.AgentVersion = &av
		}
		if v, ok := asString(data["protocol_version"]); ok && v != "" {
			pv := v
			event.ProtocolVersion = &pv
		}
		if v, ok := asString(data["source_app"]); ok && v != "" {
			sa := v
			event.SourceApp = &sa
		}
		if v, ok := asString(data["os"]); ok && v != "" {
			osVal := v
			event.OS = &osVal
		}
		if v, ok := asString(data["service_category"]); ok && v != "" {
			sc := v
			event.ServiceCategory = &sc
		}
		if v, ok := asString(data["ai_vendor"]); ok && v != "" {
			av := v
			event.AIVendor = &av
		}
		if v, ok := asString(data["capture_surface"]); ok && v != "" {
			cs := v
			event.CaptureSurface = &cs
		}

		h.telem.Emit(event)
		accepted++
	}
	if accepted == 0 {
		h.logger.Debug("telemetry batch had no request events", "events", len(batch.Events))
	}

	w.WriteHeader(http.StatusAccepted)
}

func isAgentStatusEvent(name string) bool {
	switch name {
	case "agent.started", "agent.stopped", "agent.heartbeat", "agent.integrity_error",
		"proxy.tamper_detected", "proxy.listener_unreachable", "proxy.remediated":
		return true
	default:
		return false
	}
}

func agentStatusSeverity(name string, data map[string]interface{}) string {
	switch name {
	case "proxy.tamper_detected", "proxy.listener_unreachable":
		return "critical"
	case "agent.integrity_error":
		return "warning"
	case "agent.heartbeat":
		if value, ok := asBool(data["gateway_connected"]); ok && !value {
			return "warning"
		}
		if value, ok := asString(data["proxy_integrity"]); ok && value != "ok" {
			return "warning"
		}
	}
	return "info"
}

func sanitizedAgentStatusData(data map[string]interface{}) map[string]interface{} {
	if data == nil {
		return map[string]interface{}{}
	}
	allowed := map[string]bool{
		"agent_version": true, "protocol_version": true, "policy_version": true,
		"uptime_seconds": true, "gateway_connected": true, "proxy_listener_alive": true,
		"proxy_integrity": true, "prompt_capture": true, "semantic_classifier": true,
		"browser_protection": true, "service_status": true, "auto_reregister": true,
		"hostname": true, "host": true, "port": true, "remediation": true,
		"policy_fresh": true, "classifier_required": true, "classifier_healthy": true,
		"prompt_enforcement_mode": true, "effective_prompt_enforcement_mode": true,
		"protection_state": true,
	}
	out := make(map[string]interface{}, len(allowed))
	for key, value := range data {
		if key == "surface_states" {
			if sanitized, ok := sanitizeSurfaceStates(value); ok {
				out[key] = sanitized
			}
			continue
		}
		if allowed[key] {
			out[key] = value
		}
	}
	return out
}

var forbiddenCentralTelemetryKeys = map[string]struct{}{
	"path": {}, "prompt": {}, "prompt_text": {}, "request_body": {}, "response_body": {},
	"file": {}, "files": {}, "file_path": {}, "local_path": {}, "matched_patterns": {},
	"matched_fields": {}, "matched_excerpts": {}, "mcp_payload": {}, "mcp_arguments": {},
	"credentials": {}, "api_key": {}, "access_token": {}, "command_arguments": {},
	"repository_contents": {}, "reason_detail": {}, "semantic_reason": {},
}

func forbiddenCentralTelemetryField(data map[string]interface{}) (string, bool) {
	var inspect func(map[string]interface{}) (string, bool)
	inspect = func(values map[string]interface{}) (string, bool) {
		for key, value := range values {
			normalized := strings.ToLower(strings.TrimSpace(key))
			if _, forbidden := forbiddenCentralTelemetryKeys[normalized]; forbidden {
				return key, true
			}
			switch nested := value.(type) {
			case map[string]interface{}:
				if key, found := inspect(nested); found {
					return key, true
				}
			case []interface{}:
				for _, item := range nested {
					if object, ok := item.(map[string]interface{}); ok {
						if key, found := inspect(object); found {
							return key, true
						}
					}
				}
			}
		}
		return "", false
	}
	return inspect(data)
}

func sanitizeSurfaceStates(raw interface{}) (map[string]string, bool) {
	var states map[string]string
	switch value := raw.(type) {
	case map[string]string:
		states = value
	case map[string]interface{}:
		states = make(map[string]string, len(value))
		for surface, state := range value {
			text, ok := state.(string)
			if !ok {
				continue
			}
			states[surface] = text
		}
	default:
		return nil, false
	}

	out := make(map[string]string, len(states))
	for surface, state := range states {
		if allowedSurfaceStateKey(surface) && allowedSurfaceStateValue(state) {
			out[surface] = state
		}
	}
	return out, len(out) > 0
}

func allowedSurfaceStateKey(surface string) bool {
	switch surface {
	case "browser_chromium", "browser_firefox", "browser_safari",
		"desktop", "claude_code", "cursor", "windsurf", "github_copilot":
		return true
	default:
		return false
	}
}

func allowedSurfaceStateValue(state string) bool {
	switch state {
	case "hard_block", "alert_only", "would_block", "monitor_only", "unprotected", "unknown":
		return true
	default:
		return false
	}
}

func (h *ProxyHandler) emitEvent(deviceID, orgID string, r *http.Request, status int, latency time.Duration, bytesSent int64, decision string, ruleID *string) {
	host := canonicalHost(r.Host)
	port := extractPort(r)
	event := telemetry.Event{
		Timestamp:      time.Now(),
		DeviceID:       deviceID,
		OrgID:          orgID,
		RequestMethod:  r.Method,
		RequestHost:    host,
		RequestPort:    port,
		ResponseStatus: status,
		LatencyMs:      int(latency.Milliseconds()),
		BytesSent:      bytesSent,
		BytesReceived:  r.ContentLength,
		PolicyDecision: decision,
		MatchedRuleID:  sanitizedRuleIDPtr(ruleID),
	}
	if v := strings.TrimSpace(r.Header.Get("X-Themisto-Process-Name")); v != "" {
		event.SourceApp = &v
	}
	if v := strings.TrimSpace(r.Header.Get("X-Themisto-Service-Category")); v != "" {
		event.ServiceCategory = &v
	}
	if v := strings.TrimSpace(r.Header.Get("X-Themisto-AI-Vendor")); v != "" {
		event.AIVendor = &v
	}
	h.telem.Emit(event)
}

func extractPort(r *http.Request) int {
	host := r.Host
	if host == "" && r.URL != nil {
		host = r.URL.Host
	}
	_, portStr, _ := strings.Cut(host, ":")
	if portStr != "" {
		if p, err := strconv.Atoi(portStr); err == nil {
			return p
		}
	}
	if r.Method == http.MethodConnect {
		return 443
	}
	if r.URL == nil {
		return 80
	}
	if r.URL.Scheme == "https" {
		return 443
	}
	return 80
}

func canonicalHost(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return raw
	}
	if host, _, err := net.SplitHostPort(raw); err == nil {
		return host
	}
	if strings.Count(raw, ":") > 1 {
		return strings.Trim(raw, "[]")
	}
	return raw
}

func extractURLPort(u *url.URL) int {
	if u == nil {
		return 80
	}
	_, portStr, _ := strings.Cut(u.Host, ":")
	if portStr != "" {
		if p, err := strconv.Atoi(portStr); err == nil {
			return p
		}
	}
	if u.Scheme == "https" {
		return 443
	}
	return 80
}

func headersSent(w http.ResponseWriter) bool {
	_, ok := w.(interface{ Written() bool })
	return ok
}

func normalizePolicyDecision(raw string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "allow", "forward", "bypass":
		return "allow", true
	case "block", "deny":
		return "block", true
	case "log_only", "log-only", "logonly", "alert":
		return "log_only", true
	default:
		return "", false
	}
}

func asString(v interface{}) (string, bool) {
	s, ok := v.(string)
	return s, ok
}

func asBool(v interface{}) (bool, bool) {
	b, ok := v.(bool)
	return b, ok
}

func asInt(v interface{}) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case float32:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	case int32:
		return int(n), true
	default:
		return 0, false
	}
}

func asInt64(v interface{}) (int64, bool) {
	switch n := v.(type) {
	case float64:
		return int64(n), true
	case float32:
		return int64(n), true
	case int:
		return int64(n), true
	case int64:
		return n, true
	case int32:
		return int64(n), true
	default:
		return 0, false
	}
}

func asFloat64(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case int32:
		return float64(n), true
	default:
		return 0, false
	}
}

func sanitizedRuleIDPtr(ruleID *string) *string {
	if ruleID == nil {
		return nil
	}
	return sanitizedRuleID(*ruleID)
}

func sanitizedRuleID(raw string) *string {
	raw = strings.TrimSpace(raw)
	if raw == "" || !isUUID(raw) {
		return nil
	}
	v := raw
	return &v
}

func isUUID(raw string) bool {
	parts := strings.Split(raw, "-")
	if len(parts) != 5 {
		return false
	}
	expected := []int{8, 4, 4, 4, 12}
	for i, part := range parts {
		if len(part) != expected[i] {
			return false
		}
		for _, c := range part {
			if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
				return false
			}
		}
	}
	return true
}
