package api

import (
	"crypto/subtle"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/themisto/backend/internal/controlplane"
	"github.com/themisto/backend/internal/metrics"
	"github.com/themisto/backend/internal/signing"
	"github.com/themisto/backend/internal/store"
)

type Server struct {
	store            *store.Store
	signer           *signing.Signer
	apiKey           string
	operatorOrgID    string
	operatorMode     string
	promptTestURL    string
	tokenTTL         time.Duration
	publicBackendURL string
	publicGatewayURL string
	controlPlane     *controlplane.Client
	logger           *slog.Logger
	mux              *http.ServeMux

	corsAllowedOrigins map[string]bool
	authRL             *rateLimiter
	operatorAuthRL     *rateLimiter
	enrollRL           *rateLimiter
	generalRL          *rateLimiter
}

func NewServer(
	s *store.Store,
	signer *signing.Signer,
	apiKey string,
	operatorOrgID string,
	operatorMode string,
	promptTestURL string,
	tokenTTL time.Duration,
	publicBackendURL string,
	publicGatewayURL string,
	controlPlane *controlplane.Client,
	logger *slog.Logger,
	corsAllowedOrigins []string,
) *Server {
	if tokenTTL <= 0 {
		tokenTTL = 24 * time.Hour
	}
	operatorMode = normalizeOperatorMode(operatorMode)

	corsMap := make(map[string]bool, len(corsAllowedOrigins))
	for _, o := range corsAllowedOrigins {
		corsMap[o] = true
	}

	srv := &Server{
		store:              s,
		signer:             signer,
		apiKey:             apiKey,
		operatorOrgID:      strings.TrimSpace(operatorOrgID),
		operatorMode:       operatorMode,
		promptTestURL:      strings.TrimSpace(promptTestURL),
		tokenTTL:           tokenTTL,
		publicBackendURL:   strings.TrimRight(publicBackendURL, "/"),
		publicGatewayURL:   strings.TrimRight(publicGatewayURL, "/"),
		controlPlane:       controlPlane,
		logger:             logger,
		mux:                http.NewServeMux(),
		corsAllowedOrigins: corsMap,
		authRL:             newRateLimiter(5, time.Minute),
		operatorAuthRL:     newRateLimiter(5, time.Minute),
		enrollRL:           newRateLimiter(10, time.Minute),
		generalRL:          newRateLimiter(60, time.Minute),
	}

	// Health & metrics
	srv.mux.HandleFunc("GET /healthz", srv.handleHealth)
	srv.mux.HandleFunc("GET /metrics", srv.withInternalAuth(func(w http.ResponseWriter, r *http.Request) {
		promhttp.Handler().ServeHTTP(w, r)
	}))

	// Organization management (API-key auth)
	srv.mux.HandleFunc("POST /api/v1/orgs", srv.withAuth(srv.handleCreateOrg))

	// Themisto operator console. Browser access exchanges the administrative
	// key for a short-lived HttpOnly session; headless API access remains supported.
	srv.mux.HandleFunc("POST /api/v1/operator/auth/login", withRateLimit(srv.operatorAuthRL, srv.handleOperatorLogin))
	srv.mux.HandleFunc("POST /api/v1/operator/auth/logout", srv.handleOperatorLogout)
	srv.mux.HandleFunc("GET /api/v1/operator/auth/me", srv.withOperatorAuth(srv.handleOperatorMe))
	srv.mux.HandleFunc("GET /api/v1/operator/orgs", srv.withOperatorAuth(srv.requireControlPlaneOperator(srv.handleOperatorListOrgs)))
	srv.mux.HandleFunc("GET /api/v1/operator/fleet", srv.withOperatorAuth(srv.handleOperatorFleet))
	srv.mux.HandleFunc("POST /api/v1/operator/orgs", srv.withOperatorAuth(srv.requireControlPlaneOperator(srv.handleOperatorCreateOrg)))
	srv.mux.HandleFunc("PUT /api/v1/operator/orgs/{orgID}/provisioning", srv.withOperatorAuth(srv.requireControlPlaneOperator(srv.handleOperatorUpdateProvisioning)))
	srv.mux.HandleFunc("POST /api/v1/operator/orgs/{orgID}/deployment-package", srv.withOperatorAuth(srv.requireControlPlaneOperator(srv.handleOperatorCreateDeploymentPackage)))
	srv.mux.HandleFunc("PUT /api/v1/operator/orgs/{orgID}/status", srv.withOperatorAuth(srv.requireControlPlaneOperator(srv.handleOperatorUpdateStatus)))
	srv.mux.HandleFunc("POST /api/v1/operator/orgs/{orgID}/revoke-certs", srv.withOperatorAuth(srv.requireControlPlaneOperator(srv.handleOperatorRevokeOrgCerts)))

	// Device/enrollment API (API-key auth)
	srv.mux.HandleFunc("POST /api/v1/devices", srv.withAuth(srv.handleRegisterDevice))
	srv.mux.HandleFunc("POST /api/v1/devices/{deviceID}/csr", withRateLimit(srv.enrollRL, srv.handleSubmitCSR))
	srv.mux.HandleFunc("POST /api/v1/devices/{deviceID}/revoke", srv.withAuth(srv.handleRevoke))
	srv.mux.HandleFunc("GET /internal/cert-status/{serial}", srv.withInternalAuth(srv.handleCertStatus))
	srv.mux.HandleFunc("GET /api/v1/enforcement/org-status/{orgID}", srv.withAuth(srv.handleEnforcementOrgStatus))
	srv.mux.HandleFunc("GET /api/v1/enforcement/device-status/{deviceID}", srv.withAuth(srv.handleEnforcementDeviceStatus))

	// Dashboard auth (rate-limited)
	srv.mux.HandleFunc("POST /api/v1/auth/login", withRateLimit(srv.authRL, srv.handleLogin))
	srv.mux.HandleFunc("POST /api/v1/auth/logout", srv.handleLogout)
	srv.mux.HandleFunc("GET /api/v1/auth/me", srv.withSession(srv.handleMe))
	srv.mux.HandleFunc("POST /api/v1/auth/terms/accept", srv.withSession(srv.handleAcceptTerms))

	// Dashboard APIs (session auth)
	srv.mux.HandleFunc("GET /api/v1/orgs/{orgID}/devices", srv.withSession(srv.handleListDevices))
	srv.mux.HandleFunc("POST /api/v1/devices/enrollment-package", srv.withRole("admin", srv.handleCreateEnrollmentPackage))
	srv.mux.HandleFunc("POST /api/v1/devices/enrollment-package/bulk", srv.withRole("admin", srv.handleBulkEnrollmentPackage))
	srv.mux.HandleFunc("POST /api/v1/devices/{deviceID}/enrollment-token", srv.withRole("admin", srv.handleReissueEnrollmentToken))
	srv.mux.HandleFunc("GET /enroll/{code}", srv.handleEnrollLink)
	srv.mux.HandleFunc("GET /api/v1/dashboard/stats", srv.withSession(srv.handleDashboardStats))
	srv.mux.HandleFunc("GET /api/v1/audit-log", srv.withSession(srv.handleListAudit))
	srv.mux.HandleFunc("GET /api/v1/telemetry/events", srv.withSession(srv.handleListTelemetry))
	srv.mux.HandleFunc("GET /api/v1/telemetry/timeseries", srv.withSession(srv.handleTelemetryTimeSeries))

	// AI usage + governance
	srv.mux.HandleFunc("GET /api/v1/ai-usage", srv.withSession(srv.handleAIUsage))
	srv.mux.HandleFunc("GET /api/v1/ai-governance/vendors", srv.withSession(srv.handleListAIVendorGovernance))
	srv.mux.HandleFunc("GET /api/v1/ai/governance/vendors", srv.withSession(srv.handleListAIVendorGovernance))
	srv.mux.HandleFunc("GET /api/v1/ai/intercept-domains", srv.withRole("admin", srv.handleListAIInterceptDomains))
	srv.mux.HandleFunc("PUT /api/v1/ai/intercept-domains", srv.withRole("admin", srv.handleReplaceAIInterceptDomains))
	srv.mux.HandleFunc("PUT /api/v1/ai-governance/vendors/{vendor}", srv.withRole("admin", srv.handleUpsertAIVendorGovernance))
	srv.mux.HandleFunc("POST /api/v1/ai-governance/vendors/{vendor}", srv.withRole("admin", srv.handleUpsertAIVendorGovernance))
	srv.mux.HandleFunc("PUT /api/v1/ai/governance/vendors/{vendor}", srv.withRole("admin", srv.handleUpsertAIVendorGovernance))
	srv.mux.HandleFunc("POST /api/v1/ai/governance/vendors/{vendor}", srv.withRole("admin", srv.handleUpsertAIVendorGovernance))
	srv.mux.HandleFunc("POST /api/v1/ai-governance/vendors/{vendor}/block", srv.withRole("admin", srv.handleBlockUnsanctionedVendor))
	srv.mux.HandleFunc("POST /api/v1/ai/governance/vendors/{vendor}/block", srv.withRole("admin", srv.handleBlockUnsanctionedVendor))
	srv.mux.HandleFunc("POST /api/v1/ai-governance/vendors/{vendor}/unblock", srv.withRole("admin", srv.handleUnblockVendor))
	srv.mux.HandleFunc("POST /api/v1/ai/governance/vendors/{vendor}/unblock", srv.withRole("admin", srv.handleUnblockVendor))

	// DLP
	srv.mux.HandleFunc("GET /api/v1/dlp/events", srv.withSession(srv.handleListDLPEvents))
	srv.mux.HandleFunc("GET /api/v1/dlp/events/{id}", srv.withSession(srv.handleGetDLPEvent))
	srv.mux.HandleFunc("PATCH /api/v1/dlp/events/{id}/review", srv.withRole("admin", srv.handleUpdateDLPEventReview))
	srv.mux.HandleFunc("GET /api/v1/dlp/events/{id}/body", srv.withRole("admin", srv.handleGetDLPEventBody))
	srv.mux.HandleFunc("GET /api/v1/dlp/summary", srv.withSession(srv.handleDLPSummary))
	srv.mux.HandleFunc("GET /api/v1/alerts", srv.withRole("admin", srv.handleListAlerts))
	srv.mux.HandleFunc("POST /api/v1/alerts/read", srv.withRole("admin", srv.handleMarkAlertsRead))

	// Policies (v2)
	srv.mux.HandleFunc("GET /api/v1/policies", srv.withSession(srv.handleListPolicies))
	srv.mux.HandleFunc("GET /api/v1/policies/enforcement", srv.withRole("admin", srv.handleGetPolicyEnforcement))
	srv.mux.HandleFunc("PUT /api/v1/policies/enforcement", srv.withRole("admin", srv.handleUpdatePolicyEnforcement))
	srv.mux.HandleFunc("POST /api/v1/policies", srv.withRole("admin", srv.handleCreatePolicy))
	srv.mux.HandleFunc("PUT /api/v1/policies/{id}", srv.withRole("admin", srv.handleUpdatePolicy))
	srv.mux.HandleFunc("DELETE /api/v1/policies/{id}", srv.withRole("admin", srv.handleDeletePolicy))
	srv.mux.HandleFunc("POST /api/v1/policies/test", srv.withRole("admin", srv.handleTestPolicy))
	srv.mux.HandleFunc("POST /api/v1/policies/prompt-test", srv.withRole("admin", srv.handlePromptPolicyTest))

	// Evidence exports
	srv.mux.HandleFunc("GET /api/v1/evidence/policies", srv.withRole("admin", srv.handleExportPolicySnapshot))
	srv.mux.HandleFunc("GET /api/v1/evidence/dlp.csv", srv.withRole("admin", srv.handleExportDLPEventsCSV))
	srv.mux.HandleFunc("GET /api/v1/evidence/audit.csv", srv.withRole("admin", srv.handleExportAuditCSV))

	// Settings (admin+ for settings, owner for user mgmt)
	srv.mux.HandleFunc("GET /api/v1/settings", srv.withSession(srv.handleGetSettings))
	srv.mux.HandleFunc("PUT /api/v1/settings", srv.withRole("admin", srv.handleUpdateSettings))
	srv.mux.HandleFunc("POST /api/v1/users", srv.withRole("owner", srv.handleCreateUser))
	srv.mux.HandleFunc("DELETE /api/v1/users/{id}", srv.withRole("owner", srv.handleDeleteUser))
	srv.mux.HandleFunc("PUT /api/v1/auth/password", srv.withSession(srv.handleChangePassword))

	return srv
}

func normalizeOperatorMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "control_plane":
		return "control_plane"
	default:
		return "customer_ops"
	}
}

func (s *Server) requireControlPlaneOperator(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if normalizeOperatorMode(s.operatorMode) != "control_plane" {
			writeError(w, http.StatusForbidden, "OPERATOR_MODE_RESTRICTED", "operator control-plane APIs are disabled in customer operations mode")
			return
		}
		next(w, r)
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Security headers
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
	w.Header().Set("Content-Security-Policy", "default-src 'self'")
	w.Header().Set("Referrer-Policy", "no-referrer")

	// CORS: only allow explicit origins in production. If no whitelist is
	// configured, allow any origin only during local development.
	origin := r.Header.Get("Origin")
	if origin != "" {
		allowAnyDevOrigin := len(s.corsAllowedOrigins) == 0 && strings.ToLower(strings.TrimSpace(os.Getenv("NODE_ENV"))) != "production"
		if allowAnyDevOrigin || s.corsAllowedOrigins[origin] {
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Themisto-Operator")
		}
	}
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// General rate limiting
	ip := extractClientIP(r)
	if !s.generalRL.allow(ip) {
		retry := s.generalRL.retryAfter(ip)
		w.Header().Set("Retry-After", strconv.Itoa(retry))
		writeError(w, http.StatusTooManyRequests, "RATE_LIMITED", "too many requests, try again later")
		return
	}

	// Metrics instrumentation
	start := time.Now()
	rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
	s.mux.ServeHTTP(rec, r)

	path := metricsPath(r)
	metrics.HTTPRequestsTotal.WithLabelValues(r.Method, path, strconv.Itoa(rec.status)).Inc()
	metrics.HTTPRequestDuration.WithLabelValues(r.Method, path).Observe(time.Since(start).Seconds())
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// metricsPath normalizes URL paths to avoid high-cardinality labels.
func metricsPath(r *http.Request) string {
	p := r.URL.Path
	switch {
	case strings.HasPrefix(p, "/api/v1/devices/") && strings.HasSuffix(p, "/csr"):
		return "/api/v1/devices/{id}/csr"
	case strings.HasPrefix(p, "/api/v1/devices/") && strings.HasSuffix(p, "/revoke"):
		return "/api/v1/devices/{id}/revoke"
	case strings.HasPrefix(p, "/api/v1/devices/") && strings.HasSuffix(p, "/enrollment-token"):
		return "/api/v1/devices/{id}/enrollment-token"
	case strings.HasPrefix(p, "/enroll/"):
		return "/enroll/{code}"
	case strings.HasPrefix(p, "/internal/cert-status/"):
		return "/internal/cert-status/{serial}"
	case strings.HasPrefix(p, "/api/v1/dlp/events/") && strings.HasSuffix(p, "/body"):
		return "/api/v1/dlp/events/{id}/body"
	case strings.HasPrefix(p, "/api/v1/dlp/events/"):
		return "/api/v1/dlp/events/{id}"
	case strings.HasPrefix(p, "/api/v1/policies/"):
		return "/api/v1/policies/{id}"
	case strings.HasPrefix(p, "/api/v1/users/"):
		return "/api/v1/users/{id}"
	default:
		return p
	}
}

func (s *Server) withInternalAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("X-Internal-Token")
		if token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(s.apiKey)) != 1 {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "invalid internal token")
			return
		}
		next(w, r)
	}
}

func (s *Server) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing or invalid Authorization header")
			return
		}
		token := strings.TrimPrefix(auth, "Bearer ")
		if subtle.ConstantTimeCompare([]byte(token), []byte(s.apiKey)) != 1 {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "invalid API key")
			return
		}
		next(w, r)
	}
}
