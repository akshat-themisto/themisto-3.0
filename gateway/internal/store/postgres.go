package store

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/lib/pq"
)

type Store struct {
	DB         *sql.DB
	bodyCipher *bodyCipher
}

func New(dsn string) (*Store, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(3)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	bodyCipher, err := newBodyCipherFromEnv()
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("init dlp body cipher: %w", err)
	}

	return &Store{DB: db, bodyCipher: bodyCipher}, nil
}

func (s *Store) Close() error {
	return s.DB.Close()
}

func (s *Store) Healthy() bool {
	return s.DB.Ping() == nil
}

type TelemetryEvent struct {
	Timestamp       time.Time
	DeviceID        string
	OrgID           string
	RequestMethod   string
	RequestHost     string
	RequestPath     string
	RequestPort     int
	ResponseStatus  int
	LatencyMs       int
	BytesSent       int64
	BytesReceived   int64
	PolicyDecision  string
	MatchedRuleID   *string
	PolicyVersion   *string
	AgentVersion    *string
	ProtocolVersion *string
	SourceApp       *string
	OS              *string
	ServiceCategory *string
	AIVendor        *string
	CaptureSurface  *string
}

func (s *Store) InsertTelemetryBatch(ctx context.Context, events []TelemetryEvent) error {
	if len(events) == 0 {
		return nil
	}

	var b strings.Builder
	b.WriteString(`INSERT INTO telemetry_events
		(timestamp, device_id, org_id, request_method, request_host, request_path, request_port,
		 response_status, latency_ms, bytes_sent, bytes_received,
		 policy_decision, matched_rule_id, policy_version,
		 agent_version, protocol_version, source_app, os,
		 service_category, ai_vendor, capture_surface)
		VALUES `)

	args := make([]interface{}, 0, len(events)*21)
	for i, e := range events {
		if i > 0 {
			b.WriteString(",")
		}
		base := i * 21
		fmt.Fprintf(&b, "($%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d)",
			base+1, base+2, base+3, base+4, base+5, base+6, base+7, base+8, base+9,
			base+10, base+11, base+12, base+13, base+14, base+15, base+16, base+17, base+18,
			base+19, base+20, base+21)
		args = append(args,
			e.Timestamp, e.DeviceID, e.OrgID, e.RequestMethod, e.RequestHost, e.RequestPath, e.RequestPort,
			e.ResponseStatus, e.LatencyMs, e.BytesSent, e.BytesReceived,
			e.PolicyDecision, e.MatchedRuleID, e.PolicyVersion,
			e.AgentVersion, e.ProtocolVersion, e.SourceApp, e.OS,
			e.ServiceCategory, e.AIVendor, e.CaptureSurface)
	}

	_, err := s.DB.ExecContext(ctx, b.String(), args...)
	return err
}

// PolicyCondition represents one rule condition from policy_rules.conditions JSON.
type PolicyCondition struct {
	Field    string `json:"field"`
	Operator string `json:"operator"`
	Value    string `json:"value"`
	Negate   bool   `json:"negate,omitempty"`
}

// PolicyRule represents a row from the policy_rules table.
type PolicyRule struct {
	ID          string
	OrgID       string
	Priority    int
	Name        string
	MatchHost   *string
	MatchPath   *string
	MatchMethod *string
	Decision    string
	Action      string
	Conditions  []PolicyCondition
	BlockReason *string
}

// GetPolicyRules returns all enabled rules for an org, ordered by priority.
func (s *Store) GetPolicyRules(ctx context.Context, orgID string) ([]PolicyRule, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id, org_id::text, priority,
		       COALESCE(NULLIF(name, ''), 'Policy rule ' || priority::text) AS name,
		       match_host, match_path, match_method,
		       decision,
		       COALESCE(NULLIF(action, ''), CASE decision WHEN 'allow' THEN 'allow' WHEN 'block' THEN 'block' ELSE 'alert' END) AS action,
		       conditions,
		       block_reason
		FROM policy_rules
		WHERE org_id = $1 AND enabled = true
		ORDER BY priority ASC`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []PolicyRule
	for rows.Next() {
		var r PolicyRule
		var rawConds []byte
		if err := rows.Scan(&r.ID, &r.OrgID, &r.Priority, &r.Name, &r.MatchHost, &r.MatchPath, &r.MatchMethod, &r.Decision, &r.Action, &rawConds, &r.BlockReason); err != nil {
			return nil, err
		}
		if err := decodePolicyConditions(rawConds, &r.Conditions); err != nil {
			return nil, err
		}
		rules = append(rules, r)
	}
	return rules, rows.Err()
}

// GetAllPolicyRules returns all enabled rules across all orgs, ordered by priority.
func (s *Store) GetAllPolicyRules(ctx context.Context) ([]PolicyRule, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id, org_id::text, priority,
		       COALESCE(NULLIF(name, ''), 'Policy rule ' || priority::text) AS name,
		       match_host, match_path, match_method,
		       decision,
		       COALESCE(NULLIF(action, ''), CASE decision WHEN 'allow' THEN 'allow' WHEN 'block' THEN 'block' ELSE 'alert' END) AS action,
		       conditions,
		       block_reason
		FROM policy_rules
		WHERE enabled = true
		ORDER BY priority ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []PolicyRule
	for rows.Next() {
		var r PolicyRule
		var rawConds []byte
		if err := rows.Scan(&r.ID, &r.OrgID, &r.Priority, &r.Name, &r.MatchHost, &r.MatchPath, &r.MatchMethod, &r.Decision, &r.Action, &rawConds, &r.BlockReason); err != nil {
			return nil, err
		}
		if err := decodePolicyConditions(rawConds, &r.Conditions); err != nil {
			return nil, err
		}
		rules = append(rules, r)
	}
	return rules, rows.Err()
}

func decodePolicyConditions(raw []byte, out *[]PolicyCondition) error {
	if len(raw) == 0 {
		*out = []PolicyCondition{}
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return err
	}
	if *out == nil {
		*out = []PolicyCondition{}
	}
	return nil
}

// ListEffectiveAIInterceptDomains returns enabled managed interception domains for an org.
// It falls back to baseline AI domains if no org-specific rows exist.
func (s *Store) ListEffectiveAIInterceptDomains(ctx context.Context, orgID string) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT domain
		FROM ai_intercept_domains
		WHERE org_id = $1::uuid AND enabled = true
		ORDER BY domain`, orgID)
	if err != nil {
		// During mixed-version rollouts this table may not exist yet.
		if pgErr, ok := err.(*pq.Error); ok && pgErr.Code == "42P01" {
			return defaultManagedAIInterceptDomains(), nil
		}
		return nil, err
	}
	defer rows.Close()

	domains := make([]string, 0)
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		if n := normalizeInterceptDomain(d); n != "" {
			domains = append(domains, n)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(domains) == 0 {
		return defaultManagedAIInterceptDomains(), nil
	}
	return normalizeAndExpandInterceptDomains(domains), nil
}

func defaultManagedAIInterceptDomains() []string {
	return []string{
		"api.anthropic.com",
		"api.githubcopilot.com",
		"api.openai.com",
		"api2.cursor.sh",
		"copilot-proxy.githubusercontent.com",
		"content-gemini.googleapis.com",
		"generativelanguage.googleapis.com",
	}
}

func normalizeAndExpandInterceptDomains(domains []string) []string {
	set := make(map[string]struct{}, len(domains)+16)
	for _, raw := range domains {
		if d := normalizeInterceptDomain(raw); d != "" {
			set[d] = struct{}{}
		}
	}
	for d := range set {
		for _, alias := range vendorAliasDomains(d) {
			if n := normalizeInterceptDomain(alias); n != "" {
				set[n] = struct{}{}
			}
		}
	}
	out := make([]string, 0, len(set))
	for d := range set {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

func vendorAliasDomains(domain string) []string {
	switch {
	case domain == "api.openai.com":
		return []string{"api.openai.com"}
	case domain == "openai.com", domain == "chat.openai.com", domain == "chatgpt.com":
		return []string{"api.openai.com"}
	case strings.HasSuffix(domain, "anthropic.com") || domain == "claude.ai":
		return []string{"api.anthropic.com"}
	case strings.HasSuffix(domain, "google.com") || strings.HasSuffix(domain, "googleapis.com") || strings.Contains(domain, "gemini"):
		return []string{"generativelanguage.googleapis.com", "content-gemini.googleapis.com"}
	case strings.HasSuffix(domain, "githubcopilot.com") || strings.HasSuffix(domain, "githubusercontent.com"):
		return []string{"api.githubcopilot.com", "copilot-proxy.githubusercontent.com"}
	case strings.Contains(domain, "cursor"):
		return []string{"api2.cursor.sh"}
	default:
		return nil
	}
}

func normalizeInterceptDomain(raw string) string {
	v := strings.TrimSpace(strings.ToLower(raw))
	if v == "" {
		return ""
	}
	v = strings.TrimPrefix(v, "*.")
	if strings.Contains(v, "://") {
		if parsed, err := url.Parse(v); err == nil {
			v = parsed.Host
		}
	}
	if strings.Contains(v, "/") {
		v = strings.SplitN(v, "/", 2)[0]
	}
	if host, _, err := net.SplitHostPort(v); err == nil {
		v = host
	}
	v = strings.TrimSuffix(v, ".")
	v = strings.TrimPrefix(v, ".")
	if v == "" || !strings.Contains(v, ".") {
		return ""
	}
	for _, r := range v {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '-' {
			continue
		}
		return ""
	}
	return v
}

// AuditEntry represents a row to insert into the audit_log table.
type AuditEntry struct {
	Timestamp    time.Time
	ActorType    string
	ActorID      string
	OrgID        *string
	Action       string
	ResourceType string
	ResourceID   string
	Details      map[string]interface{}
}

// InsertAuditBatch writes a batch of audit entries to the audit_log table.
func (s *Store) InsertAuditBatch(ctx context.Context, entries []AuditEntry) error {
	if len(entries) == 0 {
		return nil
	}

	var b strings.Builder
	b.WriteString(`INSERT INTO audit_log
		(timestamp, actor_type, actor_id, org_id, action, resource_type, resource_id, details)
		VALUES `)

	args := make([]interface{}, 0, len(entries)*8)
	for i, e := range entries {
		if i > 0 {
			b.WriteString(",")
		}
		base := i * 8
		fmt.Fprintf(&b, "($%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d)",
			base+1, base+2, base+3, base+4, base+5, base+6, base+7, base+8)
		detailsJSON, _ := json.Marshal(e.Details)
		args = append(args,
			e.Timestamp, e.ActorType, e.ActorID, e.OrgID,
			e.Action, e.ResourceType, e.ResourceID, string(detailsJSON))
	}

	_, err := s.DB.ExecContext(ctx, b.String(), args...)
	return err
}

// DLPEvent represents a DLP match row for the dlp_events table.
type DLPEvent struct {
	Timestamp            time.Time
	DeviceID             string
	OrgID                string
	RequestID            string
	RequestHost          string
	RequestPath          string
	RequestMethod        string
	SourceApp            string
	ServiceCategory      string
	AIVendor             string
	MatchTypes           []string
	MatchedPatterns      []string
	MatchedFields        []string
	MatchCount           int
	Severity             string
	ClassificationReason string
	ContentType          string
	FileCount            int
	ActionTaken          string
	PolicyRuleID         string
	ReasonCode           string
	ReasonDetail         string
	Protocol             string
	InterceptedHTTPS     bool
	InspectionQuality    string
	InspectionSkipReason string
	Direction            string
	RequestBody          string
	RequestBodyTruncated bool
}

// InsertDLPBatch writes a batch of DLP match events to the dlp_events table.
func (s *Store) InsertDLPBatch(ctx context.Context, events []DLPEvent) error {
	if len(events) == 0 {
		return nil
	}

	var b strings.Builder
	b.WriteString(`INSERT INTO dlp_events
		(timestamp, device_id, org_id, request_id, request_host, request_path, request_method,
		 source_app, service_category, ai_vendor, match_types, matched_patterns, matched_fields, match_count,
		 severity, classification_reason, content_type, file_count, action_taken,
		 policy_rule_id, reason_code, reason_detail,
		 protocol, intercepted_https, inspection_quality, inspection_skip_reason, direction,
		 request_body_encrypted, request_body_nonce, request_body_truncated)
		VALUES `)

	args := make([]interface{}, 0, len(events)*30)
	for i, e := range events {
		if i > 0 {
			b.WriteString(",")
		}
		base := i * 30
		fmt.Fprintf(&b, "($%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d)",
			base+1, base+2, base+3, base+4, base+5, base+6, base+7, base+8, base+9, base+10,
			base+11, base+12, base+13, base+14, base+15, base+16, base+17, base+18, base+19, base+20,
			base+21, base+22, base+23, base+24, base+25, base+26, base+27, base+28, base+29, base+30)

		action := e.ActionTaken
		if action == "" {
			action = "alert"
		}
		protocol := strings.TrimSpace(e.Protocol)
		if protocol == "" {
			protocol = "http"
		}
		inspectionQuality := strings.TrimSpace(e.InspectionQuality)
		if inspectionQuality == "" {
			inspectionQuality = "full"
		}
		direction := strings.TrimSpace(e.Direction)
		if direction == "" {
			direction = "outbound"
		}

		var encBody []byte
		var nonce []byte
		if strings.TrimSpace(e.RequestBody) != "" {
			var err error
			encBody, nonce, err = s.bodyCipher.Encrypt(e.RequestBody)
			if err != nil {
				return fmt.Errorf("encrypt dlp request body: %w", err)
			}
		}
		severity := strings.ToLower(strings.TrimSpace(e.Severity))
		switch severity {
		case "critical", "high", "medium", "low":
		default:
			severity = "low"
		}
		matchTypes := e.MatchTypes
		if matchTypes == nil {
			matchTypes = []string{}
		}
		matchedPatterns := e.MatchedPatterns
		if matchedPatterns == nil {
			matchedPatterns = []string{}
		}
		matchedFields := e.MatchedFields
		if matchedFields == nil {
			matchedFields = []string{}
		}

		args = append(args,
			e.Timestamp, e.DeviceID, e.OrgID, e.RequestID, e.RequestHost, e.RequestPath, e.RequestMethod,
			e.SourceApp, e.ServiceCategory, e.AIVendor,
			pq.Array(matchTypes), pq.Array(matchedPatterns), pq.Array(matchedFields),
			e.MatchCount, severity, strings.TrimSpace(e.ClassificationReason), strings.TrimSpace(e.ContentType), e.FileCount, action,
			e.PolicyRuleID, e.ReasonCode, e.ReasonDetail,
			protocol, e.InterceptedHTTPS, inspectionQuality, e.InspectionSkipReason, direction,
			encBody, nonce, e.RequestBodyTruncated)
	}

	_, err := s.DB.ExecContext(ctx, b.String(), args...)
	return err
}

type bodyCipher struct {
	aead cipher.AEAD
}

func newBodyCipherFromEnv() (*bodyCipher, error) {
	secret := strings.TrimSpace(os.Getenv("DLP_BODY_ENCRYPTION_KEY"))
	if secret == "" {
		secret = strings.TrimSpace(os.Getenv("INTERNAL_TOKEN"))
	}
	if secret == "" {
		secret = strings.TrimSpace(os.Getenv("ADMIN_API_KEY"))
	}
	if secret == "" {
		return nil, nil
	}

	sum := sha256.Sum256([]byte(secret))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return nil, fmt.Errorf("new AES cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("new GCM: %w", err)
	}
	return &bodyCipher{aead: aead}, nil
}

func (c *bodyCipher) Encrypt(plain string) ([]byte, []byte, error) {
	if plain == "" {
		return nil, nil, nil
	}

	if c == nil {
		// Do not persist prompt body unless encryption is configured.
		return nil, nil, nil
	}

	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, err
	}
	ciphertext := c.aead.Seal(nil, nonce, []byte(plain), nil)
	return ciphertext, nonce, nil
}
