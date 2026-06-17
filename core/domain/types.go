// Package domain holds shared types for the agent core. No OS-specific types or imports.
package domain

import (
	"io"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Configuration
// ---------------------------------------------------------------------------

// AgentConfig holds validated agent configuration.
type AgentConfig struct {
	AgentID                 string
	GatewayURL              string
	ListenAddr              string
	PromptCaptureListenAddr string

	HTTPSInterceptEnabled     bool
	HTTPSInterceptDomains     []string
	HTTPSInterceptFailMode    string
	HTTPSInterceptProtocols   []string
	HTTPSInterceptCaptureMode string

	PolicySyncInterval     time.Duration
	TelemetryFlushInterval time.Duration
	MaxConcurrentConns     int
	ShutdownTimeout        time.Duration

	DefaultDecision Decision
	BlockPageBody   string
	LogURLPaths     bool
	AutoReregister  bool

	// Bootstrap certificate paths (PEM files from enrollment)
	CertPath string // Path to device certificate PEM file.
	KeyPath  string // Path to device private key PEM file.
	CAPath   string // Path to CA chain PEM file.

	// Retry / backoff
	InitialBackoff          time.Duration
	MaxBackoff              time.Duration
	BackoffMultiplier       float64
	JitterFraction          float64
	MaxRetries              int
	CircuitBreakerThreshold int
	CircuitBreakerCooldown  time.Duration

	HealthCheckInterval    time.Duration
	IntegrityCheckInterval time.Duration

	PromptSemanticsEnabled            bool
	PromptSemanticsPolicy             string
	PromptSemanticsLocalURL           string
	PromptSemanticsGatewayEnabled     bool
	PromptSemanticsLocalTimeout       time.Duration
	PromptSemanticsGatewayTimeout     time.Duration
	PromptSemanticsBlockThreshold     float64
	PromptSemanticsAlertThreshold     float64
	PromptSemanticsAmbiguousThreshold float64
}

// TLSConfig holds raw TLS material for mTLS connections to the gateway.
type TLSConfig struct {
	CertDER    []byte // DER-encoded client certificate.
	KeyDER     []byte // DER-encoded private key.
	CADER      []byte // DER-encoded CA certificate (single cert, legacy).
	CAChainPEM []byte // PEM-encoded CA chain (may contain multiple certs). Takes priority over CADER.
}

// ---------------------------------------------------------------------------
// Policy
// ---------------------------------------------------------------------------

// PolicyPayload holds policy data received from the gateway.
type PolicyPayload struct {
	Version      string             `json:"version"`
	Rules        []PolicyRule       `json:"rules"`
	Interception PolicyInterception `json:"interception"`
}

// PolicyInterception holds managed HTTPS interception settings synced from gateway.
type PolicyInterception struct {
	Enabled     bool     `json:"enabled"`
	Domains     []string `json:"domains"`
	Protocols   []string `json:"protocols"`
	FailMode    string   `json:"fail_mode"`
	CaptureMode string   `json:"capture_mode"`
	Scope       string   `json:"scope"`
}

// PolicyRule is a single routing rule within a policy.
type PolicyRule struct {
	ID          string          `json:"id"`
	Priority    int             `json:"priority"`
	Decision    Decision        `json:"decision"`
	Conditions  []RuleCondition `json:"conditions"`
	BlockReason string          `json:"block_reason,omitempty"`
}

// RuleCondition describes a match predicate evaluated against RequestContext.
type RuleCondition struct {
	Field    string `json:"field"`    // host, path, method, process_name, process_path, process_signed, process_bundle, process_signer
	Operator string `json:"operator"` // eq, contains, prefix, suffix, regex, glob
	Value    string `json:"value"`
	Negate   bool   `json:"negate,omitempty"`
}

// ---------------------------------------------------------------------------
// Request / Response
// ---------------------------------------------------------------------------

// RequestContext holds request metadata used by Routing and Policy.
type RequestContext struct {
	RequestID            string
	Host                 string
	Port                 uint16
	Path                 string
	Method               string
	Scheme               string
	Protocol             string // http, websocket, grpc
	Direction            string // outbound, inbound
	InterceptedHTTPS     bool
	InspectionQuality    string // full, partial, skipped
	InspectionSkipReason string
	Process              ProcessInfo
	ServiceCategory      string // e.g. "ai_llm", "ai_code", "ai_image"; empty if not an AI service
	AIVendor             string // e.g. "openai", "anthropic"; empty if not an AI service
	CaptureSurface       string // capture origin: browser_chromium/browser_firefox/browser_safari/desktop/claude_code/cursor/windsurf/github_copilot
	DLP                  DLPInfo
}

const (
	HTTPSInterceptFailOpen   = "fail_open"
	HTTPSInterceptFailClosed = "fail_closed"
)

const (
	InterceptProtocolHTTP      = "http"
	InterceptProtocolWebSocket = "websocket"
	InterceptProtocolGRPC      = "grpc"
)

const (
	InterceptCaptureModeEncryptedFullBody = "encrypted_full_body"
)

const (
	InterceptScopeHybridAllowlist = "hybrid_allowlist"
	InterceptScopeAPIOnly         = "api_only"
)

const (
	InspectionQualityFull    = "full"
	InspectionQualityPartial = "partial"
	InspectionQualitySkipped = "skipped"
)

const (
	TrafficDirectionOutbound = "outbound"
	TrafficDirectionInbound  = "inbound"
)

// Decision is the routing decision for a request.
type Decision int

const (
	DecisionForward Decision = iota
	DecisionBypass
	DecisionBlock
	DecisionAlert // forward the request but emit a DLP alert event
)

// String returns a human-readable label.
func (d Decision) String() string {
	switch d {
	case DecisionForward:
		return "forward"
	case DecisionBypass:
		return "bypass"
	case DecisionBlock:
		return "block"
	case DecisionAlert:
		return "alert"
	default:
		return "unknown"
	}
}

// MarshalJSON encodes Decision as its string label.
func (d Decision) MarshalJSON() ([]byte, error) {
	return []byte(`"` + d.String() + `"`), nil
}

// UnmarshalJSON decodes a string label ("forward", "bypass", "block") into Decision.
func (d *Decision) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	switch s {
	case "forward", "allow":
		*d = DecisionForward
	case "bypass", "log_only":
		*d = DecisionBypass
	case "block":
		*d = DecisionBlock
	case "alert":
		*d = DecisionAlert
	default:
		*d = DecisionForward
	}
	return nil
}

// ProxyRequest represents a request to be relayed to the gateway.
type ProxyRequest struct {
	Method  string
	URL     string
	Host    string
	Headers map[string][]string
	Body    io.Reader
}

// ProxyResponse represents the response from the gateway relay.
type ProxyResponse struct {
	StatusCode int
	Headers    map[string][]string
	Body       io.ReadCloser
}

// HTTPRequest is a generic HTTP request for GatewayClient.Do (control plane).
type HTTPRequest struct {
	Method  string
	URL     string
	Headers map[string][]string
	Body    []byte
}

// HTTPResponse is a generic HTTP response from GatewayClient.Do.
type HTTPResponse struct {
	StatusCode int
	Headers    map[string][]string
	Body       []byte
}

// ---------------------------------------------------------------------------
// Telemetry
// ---------------------------------------------------------------------------

// EventPayload holds structured data for telemetry events.
type EventPayload struct {
	AgentID   string                 `json:"agent_id"`
	Timestamp time.Time              `json:"timestamp"`
	Data      map[string]interface{} `json:"data"`
}

// ---------------------------------------------------------------------------
// OS adapter domain types
// ---------------------------------------------------------------------------

// ProcessInfo holds metadata about the OS process that originated a network
// connection. Populated by the OS adapter's ProcessResolver and attached to
// RequestContext by the core before routing and policy evaluation.
//
// Fields that cannot be determined on the current platform must be left at
// their zero value. The core treats zero-valued fields as "unknown" and never
// makes security decisions based on a field the adapter could not populate.
type ProcessInfo struct {
	PID       int    // OS process identifier.
	Path      string // Absolute path to the executable binary.
	Name      string // Short process name (basename of Path or OS-reported name).
	User      string // OS user account that owns the process.
	BundleID  string // Application bundle identifier (macOS); empty on other platforms.
	Signed    bool   // True if the executable's code signature was verified.
	SignerID  string // Code-signing identity or certificate subject; empty if unsigned.
	ParentPID int    // Parent process identifier; 0 if unknown.
}

// ProxyState describes the current OS-level HTTP/HTTPS proxy configuration
// as observed by the adapter.
type ProxyState struct {
	Host         string // Proxy host address (e.g., "127.0.0.1"); empty if inactive.
	Port         uint16 // Proxy port number; 0 if inactive.
	Active       bool   // True if a proxy is currently configured in the OS.
	OwnedByAgent bool   // True if the current proxy was set by this agent instance.
}

// ServiceStatus describes the state of the agent's OS-level background
// service (launchd plist, Windows Service, systemd unit, etc.).
type ServiceStatus int

const (
	ServiceUnknown      ServiceStatus = iota // Status cannot be determined.
	ServiceNotInstalled                      // Service is not registered with the OS.
	ServiceStopped                           // Registered but not running.
	ServiceRunning                           // Registered and running normally.
	ServiceDegraded                          // Running but reporting errors or partial failure.
)

// ---------------------------------------------------------------------------
// DLP (Data Loss Prevention)
// ---------------------------------------------------------------------------

// DLPMatchType identifies the category of sensitive data detected.
type DLPMatchType string

const (
	DLPMatchPII         DLPMatchType = "pii"
	DLPMatchCredentials DLPMatchType = "credentials"
	DLPMatchSourceCode  DLPMatchType = "source_code"
	DLPMatchKeyword     DLPMatchType = "keyword"
)

// DLPMatch represents a single pattern match found in request body.
type DLPMatch struct {
	Type    DLPMatchType `json:"type"`
	Pattern string       `json:"pattern"` // human-readable pattern name, e.g. "ssn", "api_key_openai"
	Excerpt string       `json:"excerpt"` // short redacted excerpt for audit (max 64 chars)
}

// DLPInfo holds the result of scanning a request body for sensitive content.
type DLPInfo struct {
	Scanned              bool       // true if the body was scanned
	ContainsPII          bool       // true if PII was detected
	ContainsCredentials  bool       // true if credentials were detected
	ContainsSourceCode   bool       // true if source code was detected
	Matches              []DLPMatch // individual matches
	MatchedFields        []string   // logical payload fields that matched (e.g. prompt/content/file:foo.py)
	Severity             string     // low, medium, high, critical
	ClassificationReason string     // deterministic summary for UI/analytics
	ContentType          string     // normalized request content type inspected
	FileCount            int        // number of multipart files inspected
	BodySample           string     // captured body content used for DLP event logging (may be truncated)
	BodyTruncated        bool       // true when BodySample is truncated due to size limits
}

// ---------------------------------------------------------------------------
// Pre-send prompt capture
// ---------------------------------------------------------------------------

type CaptureSurface string

const (
	CaptureSurfaceBrowserChromium CaptureSurface = "browser_chromium"
	CaptureSurfaceBrowserFirefox  CaptureSurface = "browser_firefox"
	CaptureSurfaceBrowserSafari   CaptureSurface = "browser_safari"
	CaptureSurfaceDesktop         CaptureSurface = "desktop"
	CaptureSurfaceClaudeCode      CaptureSurface = "claude_code"
	CaptureSurfaceCursor          CaptureSurface = "cursor"
	CaptureSurfaceWindsurf        CaptureSurface = "windsurf"
	CaptureSurfaceGitHubCopilot   CaptureSurface = "github_copilot"
)

func (s CaptureSurface) Valid() bool {
	switch s {
	case CaptureSurfaceBrowserChromium, CaptureSurfaceBrowserFirefox, CaptureSurfaceBrowserSafari, CaptureSurfaceDesktop, CaptureSurfaceClaudeCode, CaptureSurfaceCursor, CaptureSurfaceWindsurf, CaptureSurfaceGitHubCopilot:
		return true
	default:
		return false
	}
}

type CaptureOutcome string

const (
	CaptureOutcomeBlocked          CaptureOutcome = "blocked"
	CaptureOutcomeAllowed          CaptureOutcome = "allowed"
	CaptureOutcomeWouldBlock       CaptureOutcome = "would_block"
	CaptureOutcomeDegradedFailOpen CaptureOutcome = "degraded_fail_open"
)

func (o CaptureOutcome) Valid() bool {
	switch o {
	case CaptureOutcomeBlocked, CaptureOutcomeAllowed, CaptureOutcomeWouldBlock, CaptureOutcomeDegradedFailOpen:
		return true
	default:
		return false
	}
}

// PromptEvaluationRequest is sent by endpoint adapters before prompt submit.
type PromptEvaluationRequest struct {
	PromptText      string            `json:"prompt_text"`
	Surface         CaptureSurface    `json:"surface"`
	AppName         string            `json:"app_name,omitempty"`
	AppID           string            `json:"app_id,omitempty"`
	AppPath         string            `json:"app_path,omitempty"`
	DestinationURL  string            `json:"destination_url,omitempty"`
	DestinationHost string            `json:"destination_host,omitempty"`
	DestinationPath string            `json:"destination_path,omitempty"`
	Vendor          string            `json:"vendor,omitempty"`
	ServiceCategory string            `json:"service_category,omitempty"`
	Protocol        string            `json:"protocol,omitempty"`
	RequestID       string            `json:"request_id,omitempty"`
	AdapterVersion  string            `json:"adapter_version,omitempty"`
	Metadata        map[string]string `json:"metadata,omitempty"`
}

// PromptEvaluationResponse is returned to endpoint adapters for enforcement UX.
type PromptEvaluationResponse struct {
	EvaluationID  string                `json:"evaluation_id"`
	Decision      Decision              `json:"decision"`
	PolicyRuleID  string                `json:"policy_rule_id,omitempty"`
	ReasonCode    string                `json:"reason_code,omitempty"`
	Reason        string                `json:"reason,omitempty"`
	Message       string                `json:"message"`
	Outcome       CaptureOutcome        `json:"outcome"`
	MatchCount    int                   `json:"match_count"`
	MatchTypes    []string              `json:"match_types,omitempty"`
	Severity      string                `json:"severity,omitempty"`
	Degraded      bool                  `json:"degraded"`
	DegradedCause string                `json:"degraded_cause,omitempty"`
	Semantic      *PromptSemanticResult `json:"semantic,omitempty"`
}

// PromptOutcomeRequest is sent by endpoint adapters after enforcement action.
type PromptOutcomeRequest struct {
	EvaluationID     string         `json:"evaluation_id,omitempty"`
	Surface          CaptureSurface `json:"surface"`
	Outcome          CaptureOutcome `json:"outcome"`
	DestinationHost  string         `json:"destination_host,omitempty"`
	DestinationPath  string         `json:"destination_path,omitempty"`
	Vendor           string         `json:"vendor,omitempty"`
	ServiceCategory  string         `json:"service_category,omitempty"`
	AppName          string         `json:"app_name,omitempty"`
	AdapterVersion   string         `json:"adapter_version,omitempty"`
	UserMessageShown bool           `json:"user_message_shown"`
	Error            string         `json:"error,omitempty"`
}

type PromptSemanticRequest struct {
	PromptText      string            `json:"prompt_text"`
	Policy          string            `json:"policy"`
	Surface         CaptureSurface    `json:"surface"`
	AppName         string            `json:"app_name,omitempty"`
	DestinationHost string            `json:"destination_host,omitempty"`
	DestinationPath string            `json:"destination_path,omitempty"`
	Vendor          string            `json:"vendor,omitempty"`
	ServiceCategory string            `json:"service_category,omitempty"`
	DLP             DLPInfo           `json:"dlp,omitempty"`
	Metadata        map[string]string `json:"metadata,omitempty"`
}

type PromptSemanticResult struct {
	Decision   Decision `json:"decision"`
	Confidence float64  `json:"confidence"`
	Reason     string   `json:"reason,omitempty"`
	Category   string   `json:"category,omitempty"`
	Source     string   `json:"source,omitempty"`
	Ambiguous  bool     `json:"ambiguous"`
	LatencyMs  int64    `json:"latency_ms,omitempty"`
}
