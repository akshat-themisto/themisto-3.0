package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/options"
	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	statusAPIBase = "http://127.0.0.1:17176"
)

// App struct holds the desktop app state.
type App struct {
	ctx        context.Context
	auth       *AuthManager
	client     *http.Client
	appVersion string
}

// NewApp creates a new App instance.
func NewApp() *App {
	transport := &http.Transport{
		Proxy: nil,
		DialContext: (&net.Dialer{
			Timeout:   2 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
	}
	return &App{
		auth: NewAuthManager(),
		client: &http.Client{
			Timeout:   5 * time.Second,
			Transport: transport,
		},
		appVersion: "0.1.0",
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	ensureLaunchOnLoginDefault()
	go a.ensureCurrentUserProxyRegistration()
	initTray(ctx)
}

func (a *App) domReady(ctx context.Context) {
	ensureWindowIcons()
}

func (a *App) shutdown(ctx context.Context) {
	destroyTray()
}

// onSecondInstance is called by Wails SingleInstanceLock when a second
// copy of the app is launched. It reveals and focuses the existing window
// instead of allowing a duplicate.
func (a *App) onSecondInstance(data options.SecondInstanceData) {
	wailsRuntime.WindowUnminimise(a.ctx)
	wailsRuntime.WindowShow(a.ctx)
}

// --- Status API calls (no admin gate) ---

type AgentStatus struct {
	Running            bool   `json:"running"`
	AgentID            string `json:"agent_id"`
	GatewayConnected   bool   `json:"gateway_connected"`
	GatewayState       string `json:"gateway_state"`
	Uptime             string `json:"uptime"`
	UptimeSeconds      int64  `json:"uptime_seconds"`
	ProxyState         string `json:"proxy_state"`
	ProxyListenerReady bool   `json:"proxy_listener_ready"`
	RuntimePhase       string `json:"runtime_phase"`
	Version            string `json:"version"`
	BufferSize         int    `json:"buffer_size"`
	Error              string `json:"error,omitempty"`
}

// GetAgentStatus calls the agent status API.
func (a *App) GetAgentStatus() AgentStatus {
	resp, err := a.client.Get(statusAPIBase + "/v1/status")
	if err != nil {
		return AgentStatus{Running: false, Error: "Agent not reachable: " + err.Error()}
	}
	defer resp.Body.Close()

	var status AgentStatus
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err := json.Unmarshal(body, &status); err != nil {
		return AgentStatus{Running: false, Error: "Invalid response"}
	}
	return status
}

type AgentConfig struct {
	ListenAddr       string   `json:"listen_addr"`
	GatewayURL       string   `json:"gateway_url"`
	Enrolled         bool     `json:"enrolled"`
	OrgName          string   `json:"org_name"`
	AgentID          string   `json:"agent_id"`
	EnrollmentHealth string   `json:"enrollment_health"`
	CertExpiry       string   `json:"cert_expiry,omitempty"`
	ConfigIssues     []string `json:"config_issues,omitempty"`
	Error            string   `json:"error,omitempty"`
}

// GetConfig calls the agent config API.
func (a *App) GetConfig() AgentConfig {
	resp, err := a.client.Get(statusAPIBase + "/v1/config")
	if err != nil {
		return AgentConfig{Error: "Agent not reachable"}
	}
	defer resp.Body.Close()

	var cfg AgentConfig
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err := json.Unmarshal(body, &cfg); err != nil {
		return AgentConfig{Error: "Invalid response"}
	}
	return cfg
}

// IsEnrolled checks if the agent has been enrolled.
func (a *App) IsEnrolled() bool {
	cfg := a.GetConfig()
	return cfg.Enrolled
}

type SystemInfo struct {
	OS        string `json:"os"`
	Hostname  string `json:"hostname"`
	Version   string `json:"version"`
	GoVersion string `json:"go_version"`
}

// GetSystemInfo returns basic system information.
func (a *App) GetSystemInfo() SystemInfo {
	hostname, _ := os.Hostname()
	return SystemInfo{
		OS:        runtime.GOOS + "/" + runtime.GOARCH,
		Hostname:  hostname,
		Version:   a.appVersion,
		GoVersion: runtime.Version(),
	}
}

// --- Enrollment (no admin gate — one-time setup) ---

type EnrollResult struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

type EnrollmentDefaults struct {
	Token      string `json:"token"`
	OrgName    string `json:"org_name"`
	DeviceID   string `json:"device_id"`
	BackendURL string `json:"backend_url"`
	GatewayURL string `json:"gateway_url"`
}

type EnrollmentImportResult struct {
	Success  bool               `json:"success"`
	Message  string             `json:"message"`
	Source   string             `json:"source,omitempty"`
	Defaults EnrollmentDefaults `json:"defaults,omitempty"`
}

func (a *App) GetEnrollmentDefaults() EnrollmentDefaults {
	cfg, err := loadEnrollmentConfigMap(agentConfigPath)
	if err != nil {
		return EnrollmentDefaults{}
	}
	return enrollmentDefaultsFromMap(cfg)
}

func (a *App) UseEnrollmentLink(raw string) EnrollmentImportResult {
	link, err := a.resolveEnrollmentLink(raw)
	if err != nil {
		return EnrollmentImportResult{Message: err.Error()}
	}

	resp, err := a.client.Get(link)
	if err != nil {
		return EnrollmentImportResult{Message: "Could not download enrollment config: " + err.Error()}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return EnrollmentImportResult{Message: "Could not read enrollment config: " + err.Error()}
	}
	if resp.StatusCode >= 300 {
		return EnrollmentImportResult{
			Message: fmt.Sprintf("Enrollment link returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body))),
			Source:  link,
		}
	}

	return saveEnrollmentConfigPayload(body, link)
}

func (a *App) ImportEnrollmentConfigFile() EnrollmentImportResult {
	if a.ctx == nil {
		return EnrollmentImportResult{Message: "Desktop context not ready yet. Please try again."}
	}

	path, err := wailsRuntime.OpenFileDialog(a.ctx, wailsRuntime.OpenDialogOptions{
		Title: "Select Themisto enrollment config",
		Filters: []wailsRuntime.FileFilter{
			{DisplayName: "JSON files", Pattern: "*.json"},
		},
		DefaultDirectory: enrollmentDialogDefaultDir(),
	})
	if err != nil {
		return EnrollmentImportResult{Message: "Could not open file picker: " + err.Error()}
	}
	if strings.TrimSpace(path) == "" {
		return EnrollmentImportResult{Message: "No config file selected."}
	}

	body, err := os.ReadFile(path)
	if err != nil {
		return EnrollmentImportResult{Message: "Could not read selected file: " + err.Error()}
	}

	return saveEnrollmentConfigPayload(body, path)
}

func (a *App) resolveEnrollmentLink(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("paste the enrollment link or short code first")
	}

	if u, err := url.Parse(raw); err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" {
		return raw, nil
	}

	defaults := a.GetEnrollmentDefaults()
	if strings.TrimSpace(defaults.BackendURL) == "" {
		return "", fmt.Errorf("paste the full enrollment URL, or import agent.json first so Themisto knows the backend URL")
	}

	code := strings.TrimPrefix(strings.TrimPrefix(raw, "/"), "enroll/")
	code = strings.TrimPrefix(code, "/")
	if code == "" {
		return "", fmt.Errorf("the enrollment short code is empty")
	}

	return strings.TrimRight(defaults.BackendURL, "/") + "/enroll/" + code, nil
}

func saveEnrollmentConfigPayload(body []byte, source string) EnrollmentImportResult {
	cfg, err := parseEnrollmentConfigPayload(body)
	if err != nil {
		return EnrollmentImportResult{Message: err.Error(), Source: source}
	}

	defaults, missing, err := persistEnrollmentConfig(cfg)
	if err != nil {
		return EnrollmentImportResult{Message: "Could not save enrollment config: " + err.Error(), Source: source}
	}

	message := "Enrollment config loaded. Review and continue."
	if len(missing) > 0 {
		message = "Config imported, but it is still missing: " + strings.Join(missing, ", ") + ". You can fill those in below."
	}

	return EnrollmentImportResult{
		Success:  true,
		Message:  message,
		Source:   source,
		Defaults: defaults,
	}
}

func parseEnrollmentConfigPayload(body []byte) (map[string]interface{}, error) {
	var cfg map[string]interface{}
	if err := json.Unmarshal(body, &cfg); err != nil {
		return nil, fmt.Errorf("that file or link did not contain a valid agent.json payload")
	}
	if cfg == nil {
		return nil, fmt.Errorf("the enrollment config was empty")
	}
	return cfg, nil
}

func persistEnrollmentConfig(imported map[string]interface{}) (EnrollmentDefaults, []string, error) {
	existing, _ := loadEnrollmentConfigMap(agentConfigPath)
	merged := mergeEnrollmentConfig(existing, imported)

	if err := os.MkdirAll(filepath.Dir(agentConfigPath), 0755); err != nil {
		return EnrollmentDefaults{}, nil, err
	}
	if err := writeEnrollmentConfigMap(agentConfigPath, merged); err != nil {
		return EnrollmentDefaults{}, nil, err
	}

	defaults := enrollmentDefaultsFromMap(merged)
	return defaults, missingEnrollmentFields(defaults), nil
}

func mergeEnrollmentConfig(existing, imported map[string]interface{}) map[string]interface{} {
	merged := map[string]interface{}{}
	for k, v := range existing {
		merged[k] = v
	}
	for k, v := range imported {
		merged[k] = v
	}
	for _, entry := range certPathDefaults {
		if strings.TrimSpace(stringValue(merged[entry.key])) == "" {
			merged[entry.key] = entry.path
		}
	}
	if strings.TrimSpace(stringValue(merged["agent_id"])) == "" && strings.TrimSpace(stringValue(merged["device_id"])) != "" {
		merged["agent_id"] = stringValue(merged["device_id"])
	}
	return merged
}

func loadEnrollmentConfigMap(path string) (map[string]interface{}, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	if cfg == nil {
		cfg = map[string]interface{}{}
	}
	return cfg, nil
}

func writeEnrollmentConfigMap(path string, cfg map[string]interface{}) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0600)
}

func enrollmentDefaultsFromMap(cfg map[string]interface{}) EnrollmentDefaults {
	return EnrollmentDefaults{
		Token:      stringValue(cfg["enrollment_token"]),
		OrgName:    stringValue(cfg["org_name"]),
		DeviceID:   stringValue(cfg["device_id"]),
		BackendURL: stringValue(cfg["backend_url"]),
		GatewayURL: stringValue(cfg["gateway_url"]),
	}
}

func missingEnrollmentFields(defaults EnrollmentDefaults) []string {
	var missing []string
	if defaults.Token == "" {
		missing = append(missing, "enrollment token")
	}
	if defaults.OrgName == "" {
		missing = append(missing, "organization name")
	}
	if defaults.DeviceID == "" {
		missing = append(missing, "device ID")
	}
	if defaults.BackendURL == "" {
		missing = append(missing, "backend URL")
	}
	return missing
}

func enrollmentDialogDefaultDir() string {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return ""
	}
	downloads := filepath.Join(home, "Downloads")
	if stat, err := os.Stat(downloads); err == nil && stat.IsDir() {
		return downloads
	}
	return home
}

func stringValue(v interface{}) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

// EnrollDevice calls the agent's enrollment endpoint.
func (a *App) EnrollDevice(token, backendURL, gatewayURL, orgName, deviceID string) EnrollResult {
	token = strings.TrimSpace(token)
	backendURL = strings.TrimSpace(backendURL)
	gatewayURL = strings.TrimSpace(gatewayURL)
	orgName = strings.TrimSpace(orgName)
	deviceID = strings.TrimSpace(deviceID)

	defaults := a.GetEnrollmentDefaults()
	if token == "" {
		token = defaults.Token
	}
	if backendURL == "" {
		backendURL = defaults.BackendURL
	}
	if gatewayURL == "" {
		gatewayURL = defaults.GatewayURL
	}
	if orgName == "" {
		orgName = defaults.OrgName
	}
	if deviceID == "" {
		deviceID = defaults.DeviceID
	}

	if already := a.currentHealthyEnrollment(); already != nil {
		return *already
	}

	if !a.isStatusAPIReachable() && runtime.GOOS == "darwin" {
		if err := directEnrollAndStartService(token, backendURL, gatewayURL, orgName, deviceID); err != nil {
			return EnrollResult{Message: err.Error()}
		}
		if already := a.currentHealthyEnrollment(); already != nil {
			return *already
		}
		if a.waitForStatusAPI(15 * time.Second) {
			return EnrollResult{
				Success: true,
				Message: "Device enrolled and local Themisto service started.",
			}
		}
		return EnrollResult{
			Message: "enrollment finished, but the local Themisto service still did not expose the status API on 127.0.0.1:17176",
		}
	}

	if err := a.ensureLocalEnrollmentAgent(); err != nil {
		return EnrollResult{Message: err.Error()}
	}
	if already := a.currentHealthyEnrollment(); already != nil {
		return *already
	}

	payload := map[string]string{
		"token":       token,
		"backend_url": backendURL,
		"gateway_url": gatewayURL,
		"org_name":    orgName,
		"device_id":   deviceID,
	}
	data, _ := json.Marshal(payload)

	req, err := http.NewRequest(http.MethodPost, statusAPIBase+"/v1/enroll", strings.NewReader(string(data)))
	if err != nil {
		return EnrollResult{Message: "Could not build request: " + err.Error()}
	}
	req.Header.Set("Content-Type", "application/json")

	// Read the local auth token written by the agent status API.
	if authToken := readLocalAuthToken(); authToken != "" {
		req.Header.Set("Authorization", "Bearer "+authToken)
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return EnrollResult{Message: "Could not reach agent: " + err.Error()}
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var result EnrollResult
	if err := json.Unmarshal(body, &result); err != nil {
		return EnrollResult{Message: "Invalid response from agent"}
	}
	return result
}

func (a *App) currentHealthyEnrollment() *EnrollResult {
	if !a.isStatusAPIReachable() {
		return nil
	}
	cfg := a.GetConfig()
	if cfg.Error != "" {
		return nil
	}
	if cfg.Enrolled && (cfg.EnrollmentHealth == "" || cfg.EnrollmentHealth == "healthy") {
		return &EnrollResult{
			Success: true,
			Message: "Device is already enrolled and the local Themisto service is healthy.",
		}
	}
	return nil
}

// readLocalAuthToken reads the shared-secret auth token written by the agent's
// status API server. Returns empty string on failure (best-effort).
func readLocalAuthToken() string {
	data, err := os.ReadFile(statusTokenPath)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func (a *App) ensureLocalEnrollmentAgent() error {
	if a.isStatusAPIReachable() {
		return nil
	}

	state, detail := queryServiceState()

	switch runtime.GOOS {
	case "darwin", "windows":
		switch state {
		case "not_found":
			if runtime.GOOS == "darwin" {
				if err := repairAgentService(); err != nil {
					return fmt.Errorf("local Themisto service is not installed yet: %w", err)
				}
			} else {
				return fmt.Errorf("local Themisto service is not installed yet. Repair or reinstall Themisto, then try enrollment again")
			}
		case "stopped", "unknown":
			if err := startService(); err != nil {
				return fmt.Errorf("could not start the local Themisto service: %w", err)
			}
		}

		if a.waitForStatusAPI(20 * time.Second) {
			return nil
		}
		state, detail = queryServiceState()
		if state == "running" && strings.TrimSpace(detail) == "" {
			if runtime.GOOS == "darwin" {
				detail = "Themisto launchd service is running, but the local status API on 127.0.0.1:17176 did not respond."
			} else {
				detail = "Themisto service is running, but the local status API on 127.0.0.1:17176 did not respond."
			}
		}
		if strings.TrimSpace(detail) != "" {
			return fmt.Errorf("local Themisto agent is still unreachable on 127.0.0.1:17176 (%s)", detail)
		}
		return fmt.Errorf("local Themisto agent is still unreachable on 127.0.0.1:17176")
	default:
		return nil
	}
}

func (a *App) isStatusAPIReachable() bool {
	resp, err := a.client.Get(statusAPIBase + "/v1/status")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode < 500
}

func (a *App) waitForStatusAPI(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if a.isStatusAPIReachable() {
			return true
		}
		time.Sleep(300 * time.Millisecond)
	}
	return false
}

// --- Installed state diagnostics ---

type InstalledState struct {
	BinaryExists     bool   `json:"binary_exists"`
	ConfigExists     bool   `json:"config_exists"`
	ConfigReadable   bool   `json:"config_readable"`
	ConfigValid      bool   `json:"config_valid"`
	HasAgentID       bool   `json:"has_agent_id"`
	HasGatewayURL    bool   `json:"has_gateway_url"`
	CertsExist       bool   `json:"certs_exist"`
	ServiceInstalled bool   `json:"service_installed"`
	Summary          string `json:"summary"`
}

// CheckInstalledState reads the filesystem and Windows Service state directly
// to diagnose why the agent may not be running. Does not depend on the status
// API being reachable.
func (a *App) CheckInstalledState() InstalledState {
	s := InstalledState{}
	configReadable := false

	_, err := os.Stat(agentBinaryPath)
	s.BinaryExists = err == nil

	if _, err := os.Stat(agentConfigPath); err == nil {
		s.ConfigExists = true
	}

	data, err := os.ReadFile(agentConfigPath)
	if err == nil {
		configReadable = true
	}
	s.ConfigReadable = configReadable

	if configReadable {
		var cfg map[string]interface{}
		if json.Unmarshal(data, &cfg) == nil {
			s.ConfigValid = true
			if v, _ := cfg["agent_id"].(string); v != "" {
				s.HasAgentID = true
			}
			if v, _ := cfg["gateway_url"].(string); v != "" {
				s.HasGatewayURL = true
			}
			cp, _ := cfg["cert_path"].(string)
			kp, _ := cfg["key_path"].(string)
			ca, _ := cfg["ca_path"].(string)
			if cp != "" && kp != "" && ca != "" {
				_, e1 := os.Stat(cp)
				_, e2 := os.Stat(kp)
				_, e3 := os.Stat(ca)
				s.CertsExist = e1 == nil && e2 == nil && e3 == nil
			}
		}
	}

	serviceStateSupported := runtime.GOOS == "windows" || runtime.GOOS == "darwin"
	if serviceStateSupported {
		s.ServiceInstalled = isServiceInstalled()
	}

	switch {
	case !s.BinaryExists:
		s.Summary = "not_installed"
	case !s.ConfigExists:
		s.Summary = "installed_not_configured"
	case configReadable && !s.ConfigValid:
		s.Summary = "installed_not_configured"
	case configReadable && (!s.HasAgentID || !s.HasGatewayURL):
		s.Summary = "installed_not_configured"
	case configReadable && !s.CertsExist:
		s.Summary = "installed_not_enrolled"
	case serviceStateSupported && !s.ServiceInstalled:
		s.Summary = "service_missing"
	default:
		s.Summary = "installed_ready"
	}
	return s
}

// --- Browser extensions ---

type BrowserInfo struct {
	Name      string `json:"name"`
	Installed bool   `json:"installed"`
	Extension bool   `json:"extension_installed"`
}

// DetectBrowsers checks for installed browsers.
func (a *App) DetectBrowsers() []BrowserInfo {
	return detectBrowsers()
}

// GetExtensionStatus checks extension install status per browser.
func (a *App) GetExtensionStatus() []BrowserInfo {
	return detectBrowsers()
}

// GetBrowserProtectionStatus returns the authoritative per-browser protection status.
func (a *App) GetBrowserProtectionStatus() []BrowserProtectionStatus {
	return detectBrowserProtection()
}

// --- Diagnostics snapshot ---

// PathCheck records whether a canonical filesystem path exists.
type PathCheck struct {
	Path   string `json:"path"`
	Label  string `json:"label"`
	Exists bool   `json:"exists"`
}

// DiagnosticIssue is a single derived problem with actionable remediation.
type DiagnosticIssue struct {
	Area             string `json:"area"`
	Title            string `json:"title"`
	Detail           string `json:"detail"`
	Remediation      string `json:"remediation"`
	Severity         string `json:"severity"`          // "error", "warning", "info"
	ProtectionImpact string `json:"protection_impact"` // "error", "degraded", "starting", "none"
}

// DiagnosticsSnapshot aggregates all diagnostic data into one struct.
type DiagnosticsSnapshot struct {
	Timestamp         string                         `json:"timestamp"`
	AppVersion        string                         `json:"app_version"`
	SummaryStatus     string                         `json:"summary_status"` // "healthy", "starting", "degraded", "error"
	Status            AgentStatus                    `json:"status"`
	Config            AgentConfig                    `json:"config"`
	Installed         InstalledState                 `json:"installed"`
	System            SystemInfo                     `json:"system"`
	Browsers          []BrowserInfo                  `json:"browsers"`
	BrowserProtection []BrowserProtectionStatus      `json:"browser_protection"`
	ClaudeCode        ClaudeCodeIntegrationStatus    `json:"claude_code"`
	Cursor            CursorIntegrationStatus        `json:"cursor"`
	GitHubCopilot     GitHubCopilotIntegrationStatus `json:"github_copilot"`
	Windsurf          WindsurfIntegrationStatus      `json:"windsurf"`
	PathChecks        []PathCheck                    `json:"path_checks"`
	ServiceState      string                         `json:"service_state"`
	ServiceDetail     string                         `json:"service_detail"`
	Issues            []DiagnosticIssue              `json:"issues"`
	AdvisoryCount     int                            `json:"advisory_count"`
	CollectionErrors  []string                       `json:"collection_errors,omitempty"`
}

// ExportResult is the result of exporting a diagnostics bundle.
type ExportResult struct {
	Success bool   `json:"success"`
	Path    string `json:"path,omitempty"`
	Error   string `json:"error,omitempty"`
}

// GetDiagnosticsSnapshot collects all diagnostic data into a single snapshot.
func (a *App) GetDiagnosticsSnapshot() DiagnosticsSnapshot {
	snap := DiagnosticsSnapshot{
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
		AppVersion: a.appVersion,
	}

	snap.Status = a.GetAgentStatus()
	if snap.Status.Error != "" {
		snap.CollectionErrors = append(snap.CollectionErrors, "status: "+snap.Status.Error)
	}

	snap.Config = a.GetConfig()
	if snap.Config.Error != "" {
		snap.CollectionErrors = append(snap.CollectionErrors, "config: "+snap.Config.Error)
	}

	snap.Installed = a.CheckInstalledState()
	snap.System = a.GetSystemInfo()
	snap.BrowserProtection = a.GetBrowserProtectionStatus()
	// Derive legacy Browsers from BrowserProtection
	for _, bp := range snap.BrowserProtection {
		snap.Browsers = append(snap.Browsers, BrowserInfo{
			Name: bp.Name, Installed: bp.Installed, Extension: bp.DeploymentDetected,
		})
	}
	snap.ClaudeCode = cachedClaudeCodeIntegrationForDiagnostics()
	snap.Cursor = cachedCursorIntegrationForDiagnostics()
	snap.GitHubCopilot = cachedGitHubCopilotIntegrationForDiagnostics()
	snap.Windsurf = cachedWindsurfIntegrationForDiagnostics()
	snap.PathChecks = checkCanonicalPaths()

	state, detail := queryServiceState()
	snap.ServiceState = state
	snap.ServiceDetail = detail

	snap.Issues = deriveIssues(&snap)
	snap.SummaryStatus, snap.AdvisoryCount = summarizeDiagnosticsIssues(snap.Issues)

	return snap
}

// deriveIssues is the single source of truth for employee-facing diagnostics.
func deriveIssues(snap *DiagnosticsSnapshot) []DiagnosticIssue {
	var issues []DiagnosticIssue

	add := func(area, title, detail, remediation, severity, protectionImpact string) {
		issues = append(issues, DiagnosticIssue{
			Area: area, Title: title, Detail: detail,
			Remediation: remediation, Severity: severity, ProtectionImpact: protectionImpact,
		})
	}

	// Installation state
	if !snap.Installed.BinaryExists {
		add("agent", "Agent not installed",
			"The agent binary was not found at the expected location.",
			"Contact IT to install the Themisto agent.", "error", "error")
	}

	if snap.Installed.BinaryExists && (!snap.Installed.ConfigExists || (snap.Installed.ConfigReadable && !snap.Installed.ConfigValid)) {
		add("config", "Configuration incomplete",
			"agent.json is missing or invalid.",
			"Contact IT to complete agent setup.", "error", "error")
	}

	if snap.Installed.BinaryExists && snap.Installed.ConfigReadable && snap.Installed.ConfigValid &&
		(!snap.Installed.HasAgentID || !snap.Installed.HasGatewayURL) {
		add("config", "Configuration incomplete",
			"agent.json is missing required fields (agent_id or gateway_url).",
			"Contact IT to complete agent setup.", "error", "error")
	}

	// Enrollment state
	if snap.Installed.BinaryExists && snap.Installed.ConfigValid && !snap.Config.Enrolled && snap.Config.Error == "" {
		add("enrollment", "Device not enrolled",
			"Enrollment has not been completed.",
			"Complete enrollment to connect to your organization.", "warning", "degraded")
	}

	// Installed but not enrolled (filesystem check) — covers case when config API is unreachable
	if snap.Installed.Summary == "installed_not_enrolled" && snap.Config.Error != "" {
		add("cert", "Certificate files missing",
			"One or more certificate files are not on disk.",
			"Contact IT to re-enroll this device.", "error", "error")
	}

	// Certificate issues (from enrollment_health)
	switch snap.Config.EnrollmentHealth {
	case "cert_missing":
		add("cert", "Certificate files missing",
			"One or more certificate files are not on disk.",
			"Contact IT to re-enroll this device.", "error", "error")
	case "cert_invalid":
		add("cert", "Certificate invalid",
			"Certificate files exist but could not be validated.",
			"Contact IT to re-enroll this device.", "error", "error")
	case "cert_expired":
		detail := "The device certificate has expired."
		if snap.Config.CertExpiry != "" {
			detail = fmt.Sprintf("The device certificate has expired (%s).", snap.Config.CertExpiry)
		}
		add("cert", "Certificate expired", detail,
			"Contact IT to renew enrollment.", "error", "error")
	}

	// Service state. Suppress Windows-service diagnostics on non-Windows
	// snapshots produced by the non-Windows service stub.
	nonWindowsStubDetail := strings.Contains(strings.ToLower(snap.ServiceDetail), "not a windows system")
	if snap.Installed.BinaryExists && (runtime.GOOS == "windows" || !nonWindowsStubDetail) {
		switch snap.ServiceState {
		case "not_found":
			if snap.Status.Running {
				add("service", "Startup persistence missing",
					"The agent is running now, but the background service is not installed. It may not come back after reboot.",
					"Use Repair agent service to reinstall the background service, or contact IT.", "warning", "none")
			} else {
				add("service", "Agent service missing",
					"The Themisto background service is not installed.",
					"Use Repair agent service or re-run the installer.", "error", "error")
			}
		case "stopped":
			if snap.Status.Running {
				add("service", "Agent running outside service",
					"The agent is running but not through the background service. This is unusual.",
					"This may indicate a manual start. Contact IT if unexpected.", "info", "none")
			} else {
				add("service", "Agent service stopped",
					"The Themisto background service is installed but stopped.",
					"Start the agent service from Admin controls, or contact IT.", "error", "error")
			}
		case "unknown":
			add("service", "Agent service state unknown",
				"Themisto could not query the background service state. This may indicate a system problem.",
				"Contact IT.", "warning", "degraded")
			// "running", "start_pending", "stop_pending" — no issue
		}
	}

	// Agent running state
	if !snap.Status.Running && snap.Installed.Summary == "installed_ready" {
		add("agent", "Agent service is not running",
			"The Themisto agent process is not responding on this device.",
			"Try restarting the service, or contact IT if the problem persists.", "error", "error")
	}

	// Local protection stack / proxy listener state.
	if snap.Status.Running {
		switch snap.Status.ProxyState {
		case "starting":
			add("proxy", "Local protection stack starting",
				"Themisto is still bringing up the local proxy listener after startup.",
				"The agent will keep retrying during startup. If this does not settle shortly, refresh diagnostics or contact IT.", "info", "starting")
		case "degraded":
			add("proxy", "Local protection stack unavailable",
				"Themisto could not initialize the local proxy listener after startup retries.",
				"Restart the agent or contact IT to repair the local protection stack.", "error", "error")
		}
	}

	// Gateway connectivity (only relevant when agent is running)
	if snap.Status.Running && !snap.Status.GatewayConnected {
		switch snap.Status.GatewayState {
		case "retrying", "starting":
			add("gateway", "Gateway retrying after startup",
				"The local protection stack is up, and Themisto is still retrying the secure gateway while the system network stack finishes coming online.",
				"The agent will keep retrying in the background. If this continues for several minutes, check connectivity or contact IT.", "info", "starting")
		default:
			add("gateway", "Gateway unreachable",
				"The agent is running but cannot reach the gateway.",
				"Check network connectivity or contact IT.", "warning", "degraded")
		}
	}

	// Extension coverage (from richer browser protection model)
	for _, bp := range snap.BrowserProtection {
		switch bp.ProtectionStatus {
		case "not_deployed":
			add("extension", fmt.Sprintf("Browser %s coverage upgrade available", bp.Name),
				bp.Detail, bp.Remediation, "info", "none")
		case "check_failed":
			add("extension", fmt.Sprintf("Browser %s status unknown", bp.Name),
				bp.Detail, bp.Remediation, "info", "none")
		}
		// "deployed" → no issue (deployment detected, medium confidence)
		// "not_installed" → no issue
	}

	// Claude Code integration (optional — protection_impact is always "none")
	switch snap.ClaudeCode.IntegrationState {
	case "missing_hook":
		if snap.ClaudeCode.CLIInstalled {
			add("claude_code", "Claude Code hook not configured",
				snap.ClaudeCode.Detail, snap.ClaudeCode.Remediation, "info", "none")
		}
	case "missing_script":
		add("claude_code", "Claude Code hook script missing",
			snap.ClaudeCode.Detail, snap.ClaudeCode.Remediation, "warning", "none")
	case "partial":
		add("claude_code", "Claude Code integration incomplete",
			snap.ClaudeCode.Detail, snap.ClaudeCode.Remediation, "warning", "none")
	case "hook_present_but_disabled":
		add("claude_code", "Claude Code hook disabled by settings",
			snap.ClaudeCode.Detail, snap.ClaudeCode.Remediation, "warning", "none")
	case "check_failed":
		add("claude_code", "Claude Code integration check failed",
			snap.ClaudeCode.Detail, snap.ClaudeCode.Remediation, "info", "none")
	}

	switch snap.Cursor.IntegrationState {
	case "missing_hook":
		if snap.Cursor.Installed {
			add("cursor", "Cursor hook not configured",
				snap.Cursor.Detail, snap.Cursor.Remediation, "info", "none")
		}
	case "missing_script":
		add("cursor", "Cursor hook script missing",
			snap.Cursor.Detail, snap.Cursor.Remediation, "warning", "none")
	case "partial":
		add("cursor", "Cursor integration incomplete",
			snap.Cursor.Detail, snap.Cursor.Remediation, "warning", "none")
	case "check_failed":
		add("cursor", "Cursor integration check failed",
			snap.Cursor.Detail, snap.Cursor.Remediation, "info", "none")
	}

	switch snap.GitHubCopilot.IntegrationState {
	case "missing_hook":
		if snap.GitHubCopilot.Installed {
			add("github_copilot", "GitHub Copilot hooks not configured",
				snap.GitHubCopilot.Detail, snap.GitHubCopilot.Remediation, "info", "none")
		}
	case "missing_script":
		add("github_copilot", "GitHub Copilot hook assets missing",
			snap.GitHubCopilot.Detail, snap.GitHubCopilot.Remediation, "warning", "none")
	case "partial":
		add("github_copilot", "GitHub Copilot integration incomplete",
			snap.GitHubCopilot.Detail, snap.GitHubCopilot.Remediation, "warning", "none")
	case "check_failed":
		add("github_copilot", "GitHub Copilot integration check failed",
			snap.GitHubCopilot.Detail, snap.GitHubCopilot.Remediation, "info", "none")
	}

	switch snap.Windsurf.IntegrationState {
	case "configured":
		add("windsurf", "Windsurf monitoring configured",
			"The Windsurf pre_user_prompt hook is configured on disk for prompt monitoring, but Themisto has not been able to confirm reliable runtime blocking on the tested Windsurf builds.",
			"Treat Windsurf as audit-only for now. Themisto will still evaluate prompts and record what would have been blocked, but prompts may still reach the model.",
			"info", "none")
	case "missing_hook":
		if snap.Windsurf.Installed {
			add("windsurf", "Windsurf monitoring not configured",
				snap.Windsurf.Detail, snap.Windsurf.Remediation, "info", "none")
		}
	case "missing_script":
		add("windsurf", "Windsurf monitoring assets missing",
			snap.Windsurf.Detail, snap.Windsurf.Remediation, "warning", "none")
	case "partial":
		add("windsurf", "Windsurf monitoring incomplete",
			snap.Windsurf.Detail, snap.Windsurf.Remediation, "warning", "none")
	case "check_failed":
		add("windsurf", "Windsurf integration check failed",
			snap.Windsurf.Detail, snap.Windsurf.Remediation, "info", "none")
	}

	return issues
}

func summarizeDiagnosticsIssues(issues []DiagnosticIssue) (summaryStatus string, advisoryCount int) {
	hasErrorImpact := false
	hasDegradedImpact := false
	hasStartingImpact := false

	for _, issue := range issues {
		switch issue.ProtectionImpact {
		case "error":
			hasErrorImpact = true
		case "degraded":
			hasDegradedImpact = true
		case "starting":
			hasStartingImpact = true
		default:
			advisoryCount++
		}
	}

	switch {
	case hasErrorImpact:
		return "error", advisoryCount
	case hasDegradedImpact:
		return "degraded", advisoryCount
	case hasStartingImpact:
		return "starting", advisoryCount
	default:
		return "healthy", advisoryCount
	}
}

// checkCanonicalPaths checks existence of canonical installation paths.
func checkCanonicalPaths() []PathCheck {
	checks := []PathCheck{
		{Path: agentBinaryPath, Label: "Agent binary"},
		{Path: desktopBinaryPath, Label: "Desktop app binary"},
		{Path: agentConfigPath, Label: "Agent config"},
		{Path: statusTokenPath, Label: "Status API token"},
	}

	// Add cert paths from config, or use canonical defaults.
	// Use a slice (not a map) for deterministic ordering in reports.
	data, err := os.ReadFile(agentConfigPath)
	var cfgMap map[string]interface{}
	if err == nil {
		_ = json.Unmarshal(data, &cfgMap)
	}
	for _, ce := range certPathDefaults {
		path := ce.path
		if cfgMap != nil {
			if v, _ := cfgMap[ce.key].(string); v != "" {
				path = v
			}
		}
		checks = append(checks, PathCheck{Path: path, Label: ce.label})
	}

	// Claude Code hook script.
	checks = append(checks, PathCheck{Path: claudeCodeHookScriptPath(), Label: "Claude Code hook script"})
	checks = append(checks, PathCheck{Path: cursorUserHooksPath(), Label: "Cursor user hooks.json"})
	checks = append(checks, PathCheck{Path: cursorHookScriptPath(), Label: "Cursor hook script"})
	checks = append(checks, PathCheck{Path: gitHubCopilotVSCodeSettingsPath(), Label: "VS Code user settings"})
	checks = append(checks, PathCheck{Path: gitHubCopilotHookConfigPath(), Label: "GitHub Copilot hooks config"})
	checks = append(checks, PathCheck{Path: gitHubCopilotHookScriptPath(), Label: "GitHub Copilot hook script"})
	checks = append(checks, PathCheck{Path: windsurfUserHooksPath(), Label: "Windsurf user hooks.json"})
	checks = append(checks, PathCheck{Path: windsurfHookLauncherPath(), Label: "Windsurf hook launcher"})
	checks = append(checks, PathCheck{Path: windsurfHookScriptPath(), Label: "Windsurf legacy hook script"})

	for i := range checks {
		_, err := os.Stat(checks[i].Path)
		checks[i].Exists = err == nil
	}
	return checks
}

// BuildDiagnosticsReport collects a snapshot and returns a plain-text report.
func (a *App) BuildDiagnosticsReport() string {
	snap := a.GetDiagnosticsSnapshot()
	return formatDiagnosticsReport(snap)
}

// formatDiagnosticsReport produces a human-readable sectioned report.
func formatDiagnosticsReport(snap DiagnosticsSnapshot) string {
	var b strings.Builder

	b.WriteString("=== Themisto Diagnostics Report ===\n")
	fmt.Fprintf(&b, "Generated: %s\n", snap.Timestamp)
	fmt.Fprintf(&b, "App Version: %s\n", snap.AppVersion)
	fmt.Fprintf(&b, "Overall Status: %s\n", snap.SummaryStatus)
	fmt.Fprintf(&b, "Maintenance Advisories: %d\n", snap.AdvisoryCount)

	b.WriteString("\n--- System ---\n")
	fmt.Fprintf(&b, "Hostname: %s\n", snap.System.Hostname)
	fmt.Fprintf(&b, "OS: %s\n", snap.System.OS)
	fmt.Fprintf(&b, "Go Runtime: %s\n", snap.System.GoVersion)

	b.WriteString("\n--- Agent Status ---\n")
	fmt.Fprintf(&b, "Running: %v\n", snap.Status.Running)
	fmt.Fprintf(&b, "Agent ID: %s\n", snap.Status.AgentID)
	fmt.Fprintf(&b, "Version: %s\n", snap.Status.Version)
	fmt.Fprintf(&b, "Gateway Connected: %v\n", snap.Status.GatewayConnected)
	fmt.Fprintf(&b, "Gateway State: %s\n", snap.Status.GatewayState)
	fmt.Fprintf(&b, "Proxy State: %s\n", snap.Status.ProxyState)
	fmt.Fprintf(&b, "Proxy Listener Ready: %v\n", snap.Status.ProxyListenerReady)
	fmt.Fprintf(&b, "Runtime Phase: %s\n", snap.Status.RuntimePhase)
	fmt.Fprintf(&b, "Uptime: %s\n", snap.Status.Uptime)
	fmt.Fprintf(&b, "Buffer Size: %d\n", snap.Status.BufferSize)
	if snap.Status.Error != "" {
		fmt.Fprintf(&b, "Error: %s\n", snap.Status.Error)
	}

	b.WriteString("\n--- Agent Configuration ---\n")
	fmt.Fprintf(&b, "Listen Address: %s\n", snap.Config.ListenAddr)
	fmt.Fprintf(&b, "Gateway URL: %s\n", snap.Config.GatewayURL)
	fmt.Fprintf(&b, "Organization: %s\n", snap.Config.OrgName)
	fmt.Fprintf(&b, "Agent ID: %s\n", snap.Config.AgentID)
	fmt.Fprintf(&b, "Enrolled: %v\n", snap.Config.Enrolled)
	fmt.Fprintf(&b, "Enrollment Health: %s\n", snap.Config.EnrollmentHealth)
	if snap.Config.CertExpiry != "" {
		fmt.Fprintf(&b, "Certificate Expiry: %s\n", snap.Config.CertExpiry)
	}
	if snap.Config.Error != "" {
		fmt.Fprintf(&b, "Error: %s\n", snap.Config.Error)
	}

	b.WriteString("\n--- Installation State ---\n")
	fmt.Fprintf(&b, "Summary: %s\n", snap.Installed.Summary)
	fmt.Fprintf(&b, "Agent Service: %s\n", snap.ServiceState)

	b.WriteString("\n--- File Paths ---\n")
	for _, pc := range snap.PathChecks {
		tag := "[OK]     "
		if !pc.Exists {
			tag = "[MISSING]"
		}
		fmt.Fprintf(&b, "%s %s\n", tag, pc.Path)
	}

	b.WriteString("\n--- Browser Protection ---\n")
	if len(snap.BrowserProtection) == 0 {
		b.WriteString("(none detected)\n")
	}
	for _, bp := range snap.BrowserProtection {
		if !bp.Installed {
			fmt.Fprintf(&b, "%s: Not installed\n", bp.Name)
			continue
		}
		deployment := "not detected"
		if bp.DeploymentDetected {
			deployment = "detected"
		}
		fmt.Fprintf(&b, "%s: Installed, Deployment: %s, Status: %s (%s confidence)\n",
			bp.Name, deployment, bp.ProtectionStatus, bp.Confidence)
		if bp.LastError != "" {
			fmt.Fprintf(&b, "  Error: %s\n", bp.LastError)
		}
	}

	b.WriteString("\n--- Claude Code Integration ---\n")
	if snap.ClaudeCode.Target != "" {
		fmt.Fprintf(&b, "Target: %s\n", snap.ClaudeCode.Target)
	}
	fmt.Fprintf(&b, "CLI Installed: %v\n", snap.ClaudeCode.CLIInstalled)
	if snap.ClaudeCode.CLIPath != "" {
		fmt.Fprintf(&b, "CLI Path: %s\n", snap.ClaudeCode.CLIPath)
	}
	fmt.Fprintf(&b, "Hook Configured: %v\n", snap.ClaudeCode.HookConfigured)
	fmt.Fprintf(&b, "Hook Script Exists: %v\n", snap.ClaudeCode.HookScriptExists)
	if snap.ClaudeCode.HookSource != "" {
		fmt.Fprintf(&b, "Hook Source: %s\n", snap.ClaudeCode.HookSource)
	}
	fmt.Fprintf(&b, "Integration State: %s\n", snap.ClaudeCode.IntegrationState)
	if snap.ClaudeCode.HooksDisabled {
		b.WriteString("Hooks Disabled: true (disableAllHooks is set)\n")
	}
	if snap.ClaudeCode.ManagedOverride {
		b.WriteString("Managed Override: true (allowManagedHooksOnly is set)\n")
	}
	if snap.ClaudeCode.Detail != "" {
		fmt.Fprintf(&b, "Detail: %s\n", snap.ClaudeCode.Detail)
	}
	if snap.ClaudeCode.LastError != "" {
		fmt.Fprintf(&b, "Error: %s\n", snap.ClaudeCode.LastError)
	}

	b.WriteString("\n--- Cursor Integration ---\n")
	fmt.Fprintf(&b, "Installed: %v\n", snap.Cursor.Installed)
	if snap.Cursor.CLIPath != "" {
		fmt.Fprintf(&b, "CLI Path: %s\n", snap.Cursor.CLIPath)
	}
	if snap.Cursor.HooksConfigPath != "" {
		fmt.Fprintf(&b, "Hooks Config Path: %s\n", snap.Cursor.HooksConfigPath)
	}
	if snap.Cursor.EnterpriseHooksPath != "" {
		fmt.Fprintf(&b, "Enterprise Hooks Path: %s\n", snap.Cursor.EnterpriseHooksPath)
	}
	fmt.Fprintf(&b, "Hook Configured: %v\n", snap.Cursor.HookConfigured)
	fmt.Fprintf(&b, "Hook Script Exists: %v\n", snap.Cursor.HookScriptExists)
	if snap.Cursor.HookSource != "" {
		fmt.Fprintf(&b, "Hook Source: %s\n", snap.Cursor.HookSource)
	}
	if snap.Cursor.EnterpriseManaged {
		b.WriteString("Enterprise Managed: true\n")
	}
	fmt.Fprintf(&b, "Integration State: %s\n", snap.Cursor.IntegrationState)
	if snap.Cursor.Detail != "" {
		fmt.Fprintf(&b, "Detail: %s\n", snap.Cursor.Detail)
	}
	if snap.Cursor.LastError != "" {
		fmt.Fprintf(&b, "Error: %s\n", snap.Cursor.LastError)
	}

	b.WriteString("\n--- GitHub Copilot Integration ---\n")
	fmt.Fprintf(&b, "VS Code Detected: %v\n", snap.GitHubCopilot.Installed)
	if snap.GitHubCopilot.CLIPath != "" {
		fmt.Fprintf(&b, "VS Code Path: %s\n", snap.GitHubCopilot.CLIPath)
	}
	if snap.GitHubCopilot.SettingsPath != "" {
		fmt.Fprintf(&b, "Settings Path: %s\n", snap.GitHubCopilot.SettingsPath)
	}
	if snap.GitHubCopilot.HookDirectoryPath != "" {
		fmt.Fprintf(&b, "Hook Directory: %s\n", snap.GitHubCopilot.HookDirectoryPath)
	}
	fmt.Fprintf(&b, "Hook Configured: %v\n", snap.GitHubCopilot.HookConfigured)
	fmt.Fprintf(&b, "Hook Assets Present: %v\n", snap.GitHubCopilot.HookScriptExists)
	fmt.Fprintf(&b, "Integration State: %s\n", snap.GitHubCopilot.IntegrationState)
	if snap.GitHubCopilot.Detail != "" {
		fmt.Fprintf(&b, "Detail: %s\n", snap.GitHubCopilot.Detail)
	}
	if snap.GitHubCopilot.LastError != "" {
		fmt.Fprintf(&b, "Error: %s\n", snap.GitHubCopilot.LastError)
	}

	b.WriteString("\n--- Windsurf Integration ---\n")
	fmt.Fprintf(&b, "Installed: %v\n", snap.Windsurf.Installed)
	if snap.Windsurf.CLIPath != "" {
		fmt.Fprintf(&b, "CLI Path: %s\n", snap.Windsurf.CLIPath)
	}
	if snap.Windsurf.HooksConfigPath != "" {
		fmt.Fprintf(&b, "Hooks Config Path: %s\n", snap.Windsurf.HooksConfigPath)
	}
	if snap.Windsurf.SystemHooksPath != "" {
		fmt.Fprintf(&b, "System Hooks Path: %s\n", snap.Windsurf.SystemHooksPath)
	}
	fmt.Fprintf(&b, "Hook Configured: %v\n", snap.Windsurf.HookConfigured)
	fmt.Fprintf(&b, "Hook Assets Present: %v\n", snap.Windsurf.HookScriptExists)
	if snap.Windsurf.HookSource != "" {
		fmt.Fprintf(&b, "Hook Source: %s\n", snap.Windsurf.HookSource)
	}
	if snap.Windsurf.SystemManaged {
		b.WriteString("System Managed: true\n")
	}
	fmt.Fprintf(&b, "Integration State: %s\n", snap.Windsurf.IntegrationState)
	if snap.Windsurf.Detail != "" {
		fmt.Fprintf(&b, "Detail: %s\n", snap.Windsurf.Detail)
	}
	if snap.Windsurf.LastError != "" {
		fmt.Fprintf(&b, "Error: %s\n", snap.Windsurf.LastError)
	}

	b.WriteString("\n--- Issues ---\n")
	if len(snap.Issues) == 0 {
		b.WriteString("(none)\n")
	}
	for _, issue := range snap.Issues {
		sev := strings.ToUpper(issue.Severity)
		fmt.Fprintf(&b, "[%s] %s — %s\n", sev, issue.Title, issue.Detail)
	}

	b.WriteString("\n--- Config Issues ---\n")
	if len(snap.Config.ConfigIssues) == 0 {
		b.WriteString("(none)\n")
	}
	for _, ci := range snap.Config.ConfigIssues {
		fmt.Fprintf(&b, "- %s\n", ci)
	}

	b.WriteString("\n--- Collection Errors ---\n")
	if len(snap.CollectionErrors) == 0 {
		b.WriteString("(none)\n")
	}
	for _, ce := range snap.CollectionErrors {
		fmt.Fprintf(&b, "- %s\n", ce)
	}

	return b.String()
}

// ExportDiagnosticsBundle writes a multi-file diagnostics bundle to %TEMP%.
func (a *App) ExportDiagnosticsBundle() ExportResult {
	snap := a.GetDiagnosticsSnapshot()

	ts := time.Now().Format("20060102-150405")
	dir := filepath.Join(os.TempDir(), "Themisto", "diagnostics", "themisto-diagnostics-"+ts)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return ExportResult{Error: "Could not create output directory: " + err.Error()}
	}

	// report.txt
	report := formatDiagnosticsReport(snap)
	if err := os.WriteFile(filepath.Join(dir, "report.txt"), []byte(report), 0644); err != nil {
		return ExportResult{Error: "Could not write report.txt: " + err.Error()}
	}

	// snapshot.json
	snapJSON, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return ExportResult{Error: "Could not marshal snapshot: " + err.Error()}
	}
	if err := os.WriteFile(filepath.Join(dir, "snapshot.json"), snapJSON, 0644); err != nil {
		return ExportResult{Error: "Could not write snapshot.json: " + err.Error()}
	}

	// service.txt
	if err := os.WriteFile(filepath.Join(dir, "service.txt"), []byte(snap.ServiceDetail), 0644); err != nil {
		return ExportResult{Error: "Could not write service.txt: " + err.Error()}
	}

	// agent-config.redacted.json
	redacted := redactAgentConfig()
	if redacted != nil {
		redactedJSON, _ := json.MarshalIndent(redacted, "", "  ")
		_ = os.WriteFile(filepath.Join(dir, "agent-config.redacted.json"), redactedJSON, 0644)
	}

	return ExportResult{Success: true, Path: dir}
}

// redactAgentConfig reads the installed agent.json and redacts sensitive values.
func redactAgentConfig() map[string]interface{} {
	data, err := os.ReadFile(agentConfigPath)
	if err != nil {
		return nil
	}
	var cfg map[string]interface{}
	if json.Unmarshal(data, &cfg) != nil {
		return nil
	}
	sensitiveKeys := []string{"enrollment_token", "auth_token", "api_key", "secret"}
	for _, key := range sensitiveKeys {
		if _, ok := cfg[key]; ok {
			cfg[key] = "[REDACTED]"
		}
	}
	return cfg
}

// --- Claude Code integration (no admin gate — per-user config) ---

// GetClaudeCodeIntegrationStatus returns the current Claude Code hook integration state.
func (a *App) GetClaudeCodeIntegrationStatus() ClaudeCodeIntegrationStatus {
	return detectClaudeCodeIntegration()
}

// GetClaudeCodeIntegrationOverview returns Claude Code integration status across supported environments.
func (a *App) GetClaudeCodeIntegrationOverview() ClaudeCodeIntegrationOverview {
	return detectClaudeCodeIntegrationOverview()
}

// InstallClaudeCodeHook installs the Themisto hook into Claude Code user settings.
func (a *App) InstallClaudeCodeHook() ActionResult {
	target := defaultClaudeCodeTarget()
	if err := installClaudeCodeHookForTarget(target); err != nil {
		return ActionResult{Success: false, Message: err.Error()}
	}
	return ActionResult{
		Success: true,
		Message: fmt.Sprintf("Claude Code hook files were written for %s. Restart Claude Code or review the change from /hooks before expecting the current session to enforce it.", target),
	}
}

// RepairClaudeCodeHook removes and reinstalls the Themisto hook.
func (a *App) RepairClaudeCodeHook() ActionResult {
	target := defaultClaudeCodeTarget()
	if err := repairClaudeCodeHookForTarget(target); err != nil {
		return ActionResult{Success: false, Message: err.Error()}
	}
	return ActionResult{
		Success: true,
		Message: fmt.Sprintf("Claude Code hook files were repaired for %s. Restart Claude Code or review the change from /hooks before expecting the current session to enforce it.", target),
	}
}

// RemoveClaudeCodeHook removes the Themisto hook from Claude Code user settings.
func (a *App) RemoveClaudeCodeHook() ActionResult {
	target := defaultClaudeCodeTarget()
	if err := removeClaudeCodeHookForTarget(target); err != nil {
		return ActionResult{Success: false, Message: err.Error()}
	}
	return ActionResult{
		Success: true,
		Message: fmt.Sprintf("Claude Code hook files were removed for %s. Restart Claude Code or review the change from /hooks if the current session still shows the old hook.", target),
	}
}

// InstallClaudeCodeHookForTarget installs the Themisto hook into the requested Claude Code environment.
func (a *App) InstallClaudeCodeHookForTarget(target string) ActionResult {
	if err := installClaudeCodeHookForTarget(target); err != nil {
		return ActionResult{Success: false, Message: err.Error()}
	}
	return ActionResult{
		Success: true,
		Message: fmt.Sprintf("Claude Code hook files were written for %s. Restart Claude Code or review the change from /hooks before expecting the current session to enforce it.", target),
	}
}

// RepairClaudeCodeHookForTarget repairs the Themisto hook in the requested Claude Code environment.
func (a *App) RepairClaudeCodeHookForTarget(target string) ActionResult {
	if err := repairClaudeCodeHookForTarget(target); err != nil {
		return ActionResult{Success: false, Message: err.Error()}
	}
	return ActionResult{
		Success: true,
		Message: fmt.Sprintf("Claude Code hook files were repaired for %s. Restart Claude Code or review the change from /hooks before expecting the current session to enforce it.", target),
	}
}

// RemoveClaudeCodeHookForTarget removes the Themisto hook from the requested Claude Code environment.
func (a *App) RemoveClaudeCodeHookForTarget(target string) ActionResult {
	if err := removeClaudeCodeHookForTarget(target); err != nil {
		return ActionResult{Success: false, Message: err.Error()}
	}
	return ActionResult{
		Success: true,
		Message: fmt.Sprintf("Claude Code hook files were removed for %s. Restart Claude Code or review the change from /hooks if the current session still shows the old hook.", target),
	}
}

// --- Cursor integration (no admin gate - per-user config) ---

// GetCursorIntegrationStatus returns the current Cursor hook integration state.
func (a *App) GetCursorIntegrationStatus() CursorIntegrationStatus {
	return detectCursorIntegration()
}

// InstallCursorHook installs the Themisto hook into Cursor user hooks.json.
func (a *App) InstallCursorHook() ActionResult {
	if err := installCursorHook(); err != nil {
		return ActionResult{Success: false, Message: err.Error()}
	}
	return ActionResult{
		Success: true,
		Message: "Cursor hook files were written. Restart Cursor before expecting the current session to enforce prompt governance.",
	}
}

// RepairCursorHook removes and reinstalls the Cursor hook.
func (a *App) RepairCursorHook() ActionResult {
	if err := repairCursorHook(); err != nil {
		return ActionResult{Success: false, Message: err.Error()}
	}
	return ActionResult{
		Success: true,
		Message: "Cursor hook files were repaired. Restart Cursor before expecting the current session to enforce prompt governance.",
	}
}

// RemoveCursorHook removes the Themisto hook from Cursor user hooks.json.
func (a *App) RemoveCursorHook() ActionResult {
	if err := removeCursorHook(); err != nil {
		return ActionResult{Success: false, Message: err.Error()}
	}
	return ActionResult{
		Success: true,
		Message: "Cursor hook files were removed. Restart Cursor if the current session still shows the old hook behavior.",
	}
}

// --- Cursor data cleanup ---

// GetCursorCleanupPreview returns a lightweight summary of discoverable Cursor databases and transcripts.
func (a *App) GetCursorCleanupPreview() CursorCleanupPreview {
	return cursorCleanupPreview()
}

// ScanCursorData performs a read-only scan of Cursor's local storage for sensitive data.
func (a *App) ScanCursorData() CursorCleanupResult {
	return scanCursorData()
}

// CleanCursorData redacts sensitive data in Cursor's local storage in-place.
func (a *App) CleanCursorData() CursorCleanupResult {
	return cleanCursorData()
}

// --- GitHub Copilot integration (per-user VS Code settings) ---

// GetGitHubCopilotIntegrationStatus returns the current GitHub Copilot hook integration state.
func (a *App) GetGitHubCopilotIntegrationStatus() GitHubCopilotIntegrationStatus {
	return detectGitHubCopilotIntegration()
}

// InstallGitHubCopilotHook installs the Themisto GitHub Copilot hooks into VS Code user settings.
func (a *App) InstallGitHubCopilotHook() ActionResult {
	if err := installGitHubCopilotHook(); err != nil {
		return ActionResult{Success: false, Message: err.Error()}
	}
	return ActionResult{
		Success: true,
		Message: "GitHub Copilot hooks were written. Restart VS Code to pick up direct hook changes. Prompt submission is audit-visible, and validated tool control currently applies to Copilot-issued run_in_terminal calls tied to a prompt Themisto flagged in the same session.",
	}
}

// RepairGitHubCopilotHook removes and reinstalls the Themisto GitHub Copilot hooks.
func (a *App) RepairGitHubCopilotHook() ActionResult {
	if err := repairGitHubCopilotHook(); err != nil {
		return ActionResult{Success: false, Message: err.Error()}
	}
	return ActionResult{
		Success: true,
		Message: "GitHub Copilot hooks were repaired. Restart VS Code to reload the managed hook directory. Prompt submission remains audit-only; validated tool control currently applies to Copilot-issued run_in_terminal calls tied to a prompt Themisto flagged in the same session.",
	}
}

// RemoveGitHubCopilotHook removes the Themisto GitHub Copilot hook directory from VS Code user settings.
func (a *App) RemoveGitHubCopilotHook() ActionResult {
	if err := removeGitHubCopilotHook(); err != nil {
		return ActionResult{Success: false, Message: err.Error()}
	}
	return ActionResult{
		Success: true,
		Message: "GitHub Copilot hooks were removed. Restart VS Code if the current session still shows the old hook behavior.",
	}
}

// --- Windsurf integration (no admin gate - per-user config) ---

// GetWindsurfIntegrationStatus returns the current Windsurf hook integration state.
func (a *App) GetWindsurfIntegrationStatus() WindsurfIntegrationStatus {
	return detectWindsurfIntegration()
}

// InstallWindsurfHook installs the Themisto hook into Windsurf user hooks.json.
func (a *App) InstallWindsurfHook() ActionResult {
	if err := installWindsurfHook(); err != nil {
		return ActionResult{Success: false, Message: err.Error()}
	}
	return ActionResult{
		Success: true,
		Message: "Windsurf monitoring files were written. Restart Windsurf to pick up the changes. Themisto will evaluate prompts and record what would have been blocked, but runtime blocking is not currently reliable on tested Windsurf builds.",
	}
}

// RepairWindsurfHook removes and reinstalls the Themisto Windsurf hook.
func (a *App) RepairWindsurfHook() ActionResult {
	if err := repairWindsurfHook(); err != nil {
		return ActionResult{Success: false, Message: err.Error()}
	}
	return ActionResult{
		Success: true,
		Message: "Windsurf monitoring files were repaired. Restart Windsurf to pick up the changes. Themisto will continue monitoring prompts, but runtime blocking remains unverified on tested Windsurf builds.",
	}
}

// RemoveWindsurfHook removes the Themisto hook from Windsurf user hooks.json.
func (a *App) RemoveWindsurfHook() ActionResult {
	if err := removeWindsurfHook(); err != nil {
		return ActionResult{Success: false, Message: err.Error()}
	}
	return ActionResult{
		Success: true,
		Message: "Windsurf monitoring files were removed. Restart Windsurf if the current session still shows the old hook behavior.",
	}
}

// --- Admin-gated actions ---

// ValidateAdmin authenticates against the backend.
func (a *App) ValidateAdmin(email, password string) (string, error) {
	return a.auth.ValidateAdmin(email, password)
}

// RestartAgent stops and starts the agent Windows Service. Admin-gated.
func (a *App) RestartAgent(adminToken string) error {
	if !a.auth.IsAuthorized(adminToken) {
		return fmt.Errorf("unauthorized: admin authentication required")
	}
	_ = stopService()
	time.Sleep(1 * time.Second)
	return startService()
}

// StartService starts the agent Windows Service. Admin-gated.
func (a *App) StartService(adminToken string) error {
	if !a.auth.IsAuthorized(adminToken) {
		return fmt.Errorf("unauthorized: admin authentication required")
	}
	return startService()
}

// StopService stops the agent Windows Service. Admin-gated.
func (a *App) StopService(adminToken string) error {
	if !a.auth.IsAuthorized(adminToken) {
		return fmt.Errorf("unauthorized: admin authentication required")
	}
	return stopService()
}

// RepairAgentService removes and reinstalls the agent Windows Service. Admin-gated.
func (a *App) RepairAgentService(adminToken string) error {
	if !a.auth.IsAuthorized(adminToken) {
		return fmt.Errorf("unauthorized: admin authentication required")
	}
	return repairAgentService()
}

// InstallExtension deploys browser extension via registry. Admin-gated.
func (a *App) InstallExtension(browser, adminToken string) error {
	if !a.auth.IsAuthorized(adminToken) {
		return fmt.Errorf("unauthorized: admin authentication required")
	}
	return installExtension(browser)
}

// UninstallExtension removes browser extension registry keys. Admin-gated.
func (a *App) UninstallExtension(browser, adminToken string) error {
	if !a.auth.IsAuthorized(adminToken) {
		return fmt.Errorf("unauthorized: admin authentication required")
	}
	return uninstallExtension(browser)
}

// UpdateSetting modifies a single config value. Admin-gated.
func (a *App) UpdateSetting(key, value, adminToken string) error {
	if !a.auth.IsAuthorized(adminToken) {
		return fmt.Errorf("unauthorized: admin authentication required")
	}
	return updateConfigSetting(key, value)
}

// validateStartupFiles checks that the agent binary and config exist on disk.
func validateStartupFiles(agentPath, cfgPath string) error {
	if _, err := os.Stat(agentPath); err != nil {
		return fmt.Errorf("repair agent service: agent binary not found at %s", agentPath)
	}
	if _, err := os.Stat(cfgPath); err != nil {
		return fmt.Errorf("repair agent service: config file not found at %s", cfgPath)
	}
	return nil
}

// settingTypes defines the allowlist of modifiable config keys and their types.
var settingTypes = map[string]string{
	"gateway_url":          "string",
	"listen_addr":          "string",
	"log_level":            "string",
	"auto_reregister":      "bool",
	"log_url_paths":        "bool",
	"max_concurrent_conns": "int",
}

func updateConfigSetting(key, value string) error {
	expectedType, ok := settingTypes[key]
	if !ok {
		return fmt.Errorf("unknown or non-modifiable setting: %q", key)
	}

	// Parse value according to the expected type.
	var typed interface{}
	switch expectedType {
	case "string":
		typed = value
	case "bool":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("setting %q expects a boolean value: %w", key, err)
		}
		typed = b
	case "int":
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("setting %q expects an integer value: %w", key, err)
		}
		typed = n
	default:
		typed = value
	}

	data, err := os.ReadFile(agentConfigPath)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}
	cfg[key] = typed
	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	out = append(out, '\n')
	return os.WriteFile(agentConfigPath, out, 0600)
}
