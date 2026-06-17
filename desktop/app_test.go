package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeriveIssues(t *testing.T) {
	t.Run("healthy snapshot produces no issues", func(t *testing.T) {
		snap := &DiagnosticsSnapshot{
			Status: AgentStatus{Running: true, GatewayConnected: true},
			Config: AgentConfig{Enrolled: true, EnrollmentHealth: "healthy"},
			Installed: InstalledState{
				BinaryExists: true, ConfigExists: true, ConfigValid: true,
				HasAgentID: true, HasGatewayURL: true, CertsExist: true,
				ServiceInstalled: true, Summary: "installed_ready",
			},
			ServiceState: "running",
			Browsers:     []BrowserInfo{{Name: "Chrome", Installed: true, Extension: true}},
			BrowserProtection: []BrowserProtectionStatus{{
				Name: "Chrome", Supported: true, Installed: true, DeploymentDetected: true,
				ProtectionStatus: "deployed", Confidence: "medium",
			}},
		}
		issues := deriveIssues(snap)
		if len(issues) != 0 {
			t.Fatalf("expected 0 issues, got %d: %+v", len(issues), issues)
		}
	})

	t.Run("not installed", func(t *testing.T) {
		snap := &DiagnosticsSnapshot{
			Installed:    InstalledState{Summary: "not_installed"},
			ServiceState: "not_found",
		}
		issues := deriveIssues(snap)
		found := false
		for _, i := range issues {
			if i.Title == "Agent not installed" && i.Severity == "error" {
				found = true
			}
		}
		if !found {
			t.Fatal("expected 'Agent not installed' error issue")
		}
	})

	t.Run("gateway disconnected", func(t *testing.T) {
		snap := &DiagnosticsSnapshot{
			Status: AgentStatus{Running: true, GatewayConnected: false, GatewayState: "offline", ProxyState: "running", ProxyListenerReady: true, RuntimePhase: "degraded"},
			Config: AgentConfig{Enrolled: true, EnrollmentHealth: "healthy"},
			Installed: InstalledState{
				BinaryExists: true, ConfigExists: true, ConfigValid: true,
				HasAgentID: true, HasGatewayURL: true, CertsExist: true,
				ServiceInstalled: true, Summary: "installed_ready",
			},
			ServiceState: "running",
		}
		issues := deriveIssues(snap)
		found := false
		for _, i := range issues {
			if i.Title == "Gateway unreachable" && i.Severity == "warning" {
				if i.ProtectionImpact != "degraded" {
					t.Fatalf("expected degraded protection impact, got %q", i.ProtectionImpact)
				}
				found = true
			}
		}
		if !found {
			t.Fatal("expected 'Gateway unreachable' warning issue")
		}
	})

	t.Run("gateway retrying after startup is boot state", func(t *testing.T) {
		snap := &DiagnosticsSnapshot{
			Status: AgentStatus{
				Running:            true,
				GatewayConnected:   false,
				GatewayState:       "retrying",
				ProxyState:         "running",
				ProxyListenerReady: true,
				RuntimePhase:       "retrying_gateway",
				UptimeSeconds:      75,
			},
			Config: AgentConfig{Enrolled: true, EnrollmentHealth: "healthy"},
			Installed: InstalledState{
				BinaryExists: true, ConfigExists: true, ConfigValid: true,
				HasAgentID: true, HasGatewayURL: true, CertsExist: true,
				ServiceInstalled: true, Summary: "installed_ready",
			},
			ServiceState: "running",
		}
		issues := deriveIssues(snap)
		found := false
		for _, i := range issues {
			if i.Title == "Gateway retrying after startup" && i.Severity == "info" {
				if i.ProtectionImpact != "starting" {
					t.Fatalf("expected starting protection impact, got %q", i.ProtectionImpact)
				}
				found = true
			}
			if i.Title == "Gateway unreachable" {
				t.Fatal("did not expect long-lived gateway warning during boot retry window")
			}
		}
		if !found {
			t.Fatal("expected 'Gateway retrying after startup' info issue")
		}
	})

	t.Run("proxy starting is reported separately from hard failure", func(t *testing.T) {
		snap := &DiagnosticsSnapshot{
			Status: AgentStatus{
				Running:            true,
				GatewayConnected:   false,
				GatewayState:       "starting",
				ProxyState:         "starting",
				ProxyListenerReady: false,
				RuntimePhase:       "starting",
				UptimeSeconds:      10,
			},
			Config: AgentConfig{Enrolled: true, EnrollmentHealth: "healthy"},
			Installed: InstalledState{
				BinaryExists: true, ConfigExists: true, ConfigValid: true,
				HasAgentID: true, HasGatewayURL: true, CertsExist: true,
				ServiceInstalled: true, Summary: "installed_ready",
			},
			ServiceState: "running",
		}
		issues := deriveIssues(snap)
		found := false
		for _, i := range issues {
			if i.Title == "Local protection stack starting" && i.Severity == "info" {
				if i.ProtectionImpact != "starting" {
					t.Fatalf("expected starting protection impact, got %q", i.ProtectionImpact)
				}
				found = true
			}
		}
		if !found {
			t.Fatal("expected 'Local protection stack starting' info issue")
		}
	})

	t.Run("cert expired", func(t *testing.T) {
		snap := &DiagnosticsSnapshot{
			Config: AgentConfig{EnrollmentHealth: "cert_expired", CertExpiry: "2025-01-01T00:00:00Z"},
			Installed: InstalledState{
				BinaryExists: true, ConfigExists: true, ConfigValid: true,
				HasAgentID: true, HasGatewayURL: true, Summary: "installed_ready",
			},
			ServiceState: "running",
		}
		issues := deriveIssues(snap)
		found := false
		for _, i := range issues {
			if i.Title == "Certificate expired" && i.Severity == "error" {
				found = true
			}
		}
		if !found {
			t.Fatal("expected 'Certificate expired' error issue")
		}
	})

	t.Run("extension missing", func(t *testing.T) {
		snap := &DiagnosticsSnapshot{
			Status: AgentStatus{Running: true, GatewayConnected: true},
			Config: AgentConfig{Enrolled: true, EnrollmentHealth: "healthy"},
			Installed: InstalledState{
				BinaryExists: true, ConfigExists: true, ConfigValid: true,
				HasAgentID: true, HasGatewayURL: true, CertsExist: true,
				ServiceInstalled: true, Summary: "installed_ready",
			},
			ServiceState: "running",
			Browsers:     []BrowserInfo{{Name: "Edge", Installed: true, Extension: false}},
			BrowserProtection: []BrowserProtectionStatus{{
				Name: "Edge", Supported: true, Installed: true, DeploymentDetected: false,
				ProtectionStatus: "not_deployed", Confidence: "high",
				Detail:      "Edge is installed, but the Themisto extension has not been deployed.",
				Remediation: "If you plan to use Edge with Themisto protection, contact IT to deploy the browser extension.",
			}},
		}
		issues := deriveIssues(snap)
		found := false
		for _, i := range issues {
			if i.Title == "Browser Edge coverage upgrade available" && i.Severity == "info" {
				if i.ProtectionImpact != "none" {
					t.Fatalf("expected maintenance protection impact, got %q", i.ProtectionImpact)
				}
				found = true
			}
		}
		if !found {
			t.Fatal("expected 'Browser Edge coverage upgrade available' maintenance issue")
		}
	})

	t.Run("service missing", func(t *testing.T) {
		snap := &DiagnosticsSnapshot{
			Installed: InstalledState{
				BinaryExists: true, ConfigExists: true, ConfigValid: true,
				HasAgentID: true, HasGatewayURL: true, CertsExist: true,
				Summary: "service_missing",
			},
			ServiceState: "not_found",
		}
		issues := deriveIssues(snap)
		found := false
		for _, i := range issues {
			if i.Title == "Agent service missing" && i.Severity == "error" {
				if i.ProtectionImpact != "error" {
					t.Fatalf("expected error protection impact, got %q", i.ProtectionImpact)
				}
				found = true
			}
		}
		if !found {
			t.Fatal("expected 'Agent service missing' error issue")
		}
	})

	t.Run("service missing while running becomes persistence warning", func(t *testing.T) {
		snap := &DiagnosticsSnapshot{
			Status: AgentStatus{Running: true, GatewayConnected: true},
			Config: AgentConfig{Enrolled: true, EnrollmentHealth: "healthy"},
			Installed: InstalledState{
				BinaryExists: true, ConfigExists: true, ConfigValid: true,
				HasAgentID: true, HasGatewayURL: true, CertsExist: true,
				Summary: "service_missing",
			},
			ServiceState: "not_found",
		}
		issues := deriveIssues(snap)
		found := false
		for _, i := range issues {
			if i.Title == "Startup persistence missing" && i.Severity == "warning" {
				if i.ProtectionImpact != "none" {
					t.Fatalf("expected no protection impact, got %q", i.ProtectionImpact)
				}
				found = true
			}
			if i.Title == "Agent service missing" && i.Severity == "error" {
				t.Fatal("did not expect fatal service error while agent is running")
			}
		}
		if !found {
			t.Fatal("expected 'Startup persistence missing' warning issue")
		}
	})

	t.Run("service stopped and not running", func(t *testing.T) {
		snap := &DiagnosticsSnapshot{
			Installed: InstalledState{
				BinaryExists: true, ConfigExists: true, ConfigValid: true,
				HasAgentID: true, HasGatewayURL: true, CertsExist: true,
				ServiceInstalled: true, Summary: "installed_ready",
			},
			ServiceState: "stopped",
		}
		issues := deriveIssues(snap)
		found := false
		for _, i := range issues {
			if i.Title == "Agent service stopped" && i.Severity == "error" {
				if i.ProtectionImpact != "error" {
					t.Fatalf("expected error protection impact, got %q", i.ProtectionImpact)
				}
				found = true
			}
		}
		if !found {
			t.Fatal("expected 'Agent service stopped' error issue")
		}
	})

	t.Run("service state unknown", func(t *testing.T) {
		snap := &DiagnosticsSnapshot{
			Installed: InstalledState{
				BinaryExists: true, ConfigExists: true, ConfigValid: true,
				HasAgentID: true, HasGatewayURL: true, CertsExist: true,
				ServiceInstalled: true, Summary: "installed_ready",
			},
			ServiceState: "unknown",
		}
		issues := deriveIssues(snap)
		found := false
		for _, i := range issues {
			if i.Title == "Agent service state unknown" && i.Severity == "warning" {
				if i.ProtectionImpact != "degraded" {
					t.Fatalf("expected degraded protection impact, got %q", i.ProtectionImpact)
				}
				found = true
			}
		}
		if !found {
			t.Fatal("expected 'Agent service state unknown' warning issue")
		}
	})
}

func TestFormatDiagnosticsReport(t *testing.T) {
	snap := DiagnosticsSnapshot{
		Timestamp:     "2026-03-20T14:30:00Z",
		AppVersion:    "0.1.0",
		SummaryStatus: "degraded",
		Status: AgentStatus{
			Running: true, AgentID: "test-001", GatewayConnected: false,
			Uptime: "1h0m0s", ProxyState: "running", Version: "0.1.0",
		},
		Config: AgentConfig{
			ListenAddr: "127.0.0.1:9090", GatewayURL: "https://gw.example.com",
			OrgName: "Test Org", AgentID: "test-001", Enrolled: true,
			EnrollmentHealth: "healthy",
		},
		System: SystemInfo{OS: "windows/amd64", Hostname: "TEST-PC", Version: "0.1.0", GoVersion: "go1.23.0"},
		Installed: InstalledState{
			BinaryExists: true, ConfigExists: true, ConfigValid: true,
			HasAgentID: true, HasGatewayURL: true, CertsExist: true,
			ServiceInstalled: true, Summary: "installed_ready",
		},
		Browsers: []BrowserInfo{{Name: "Chrome", Installed: true, Extension: true}},
		BrowserProtection: []BrowserProtectionStatus{{
			Name: "Chrome", Supported: true, Installed: true, DeploymentDetected: true,
			ProtectionStatus: "deployed", Confidence: "medium",
		}},
		PathChecks:    []PathCheck{{Path: `C:\test.exe`, Label: "Test", Exists: true}},
		ServiceState:  "running",
		AdvisoryCount: 1,
		Issues: []DiagnosticIssue{
			{Severity: "warning", ProtectionImpact: "degraded", Title: "Gateway unreachable", Detail: "Cannot reach gateway."},
		},
	}

	report := formatDiagnosticsReport(snap)

	required := []string{
		"=== Themisto Diagnostics Report ===",
		"Overall Status: degraded",
		"Maintenance Advisories: 1",
		"Hostname: TEST-PC",
		"Running: true",
		"Gateway URL: https://gw.example.com",
		"Summary: installed_ready",
		"Agent Service: running",
		"[OK]",
		"Chrome: Installed, Deployment: detected, Status: deployed (medium confidence)",
		"[WARNING] Gateway unreachable",
	}
	for _, s := range required {
		if !strings.Contains(report, s) {
			t.Errorf("report missing expected string: %q", s)
		}
	}
}

func TestDeriveExtensionGuidance(t *testing.T) {
	tests := []struct {
		name       string
		browser    string
		status     string
		confidence string
		lastError  string
		wantDetail string
		wantRemed  string
	}{
		{
			name: "deployed", browser: "Chrome", status: "deployed", confidence: "medium",
			wantDetail: "Chrome is installed and Themisto found the extension in local browser data.",
			wantRemed:  "No action needed.",
		},
		{
			name: "policy detected", browser: "Edge", status: "policy_detected", confidence: "medium",
			wantDetail: "Edge is installed and Themisto found rollout policy",
			wantRemed:  "Open or fully restart the browser",
		},
		{
			name: "not deployed", browser: "Edge", status: "not_deployed", confidence: "high",
			wantDetail: "Edge is installed, but the Themisto extension has not been deployed.",
			wantRemed:  "If you plan to use Edge with Themisto protection",
		},
		{
			name: "check failed with error", browser: "Brave", status: "check_failed", confidence: "low",
			lastError:  "access denied",
			wantDetail: "Themisto could not read browser deployment state for Brave.",
			wantRemed:  "Try refreshing.",
		},
		{
			name: "not installed", browser: "Firefox", status: "not_installed", confidence: "high",
			wantDetail: "This browser is not installed on this device.",
			wantRemed:  "No action is needed unless you plan to use it.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			detail, remediation := deriveExtensionGuidance(tt.browser, tt.status, tt.confidence, tt.lastError)
			if !strings.Contains(detail, tt.wantDetail) {
				t.Errorf("detail = %q, want substring %q", detail, tt.wantDetail)
			}
			if !strings.Contains(remediation, tt.wantRemed) {
				t.Errorf("remediation = %q, want substring %q", remediation, tt.wantRemed)
			}
		})
	}
}

func TestIsRegQueryNotFound(t *testing.T) {
	if !isRegQueryNotFound(&exec.ExitError{}, []byte("ERROR: The system was unable to find the specified registry key or value.")) {
		t.Fatal("expected missing registry key output to be treated as a normal negative")
	}

	if isRegQueryNotFound(&exec.ExitError{}, []byte("ERROR: Access is denied.")) {
		t.Fatal("expected genuine registry errors to remain failures")
	}
}

func TestDeriveIssuesExtensionStates(t *testing.T) {
	base := func() *DiagnosticsSnapshot {
		return &DiagnosticsSnapshot{
			Status: AgentStatus{Running: true, GatewayConnected: true},
			Config: AgentConfig{Enrolled: true, EnrollmentHealth: "healthy"},
			Installed: InstalledState{
				BinaryExists: true, ConfigExists: true, ConfigValid: true,
				HasAgentID: true, HasGatewayURL: true, CertsExist: true,
				ServiceInstalled: true, Summary: "installed_ready",
			},
			ServiceState: "running",
		}
	}

	t.Run("deployed produces no issue", func(t *testing.T) {
		snap := base()
		snap.BrowserProtection = []BrowserProtectionStatus{{
			Name: "Chrome", ProtectionStatus: "deployed", Confidence: "medium",
			Installed: true, DeploymentDetected: true,
		}}
		issues := deriveIssues(snap)
		for _, i := range issues {
			if strings.Contains(i.Title, "Chrome") {
				t.Fatalf("deployed browser should not produce an issue, got: %+v", i)
			}
		}
	})

	t.Run("not_deployed produces maintenance note", func(t *testing.T) {
		snap := base()
		snap.BrowserProtection = []BrowserProtectionStatus{{
			Name: "Edge", ProtectionStatus: "not_deployed", Confidence: "high",
			Installed: true, DeploymentDetected: false,
			Detail: "Edge is installed, but the Themisto extension has not been deployed.",
		}}
		issues := deriveIssues(snap)
		found := false
		for _, i := range issues {
			if i.Title == "Browser Edge coverage upgrade available" && i.Severity == "info" {
				if i.ProtectionImpact != "none" {
					t.Fatalf("expected maintenance protection impact, got %q", i.ProtectionImpact)
				}
				found = true
			}
		}
		if !found {
			t.Fatal("expected maintenance issue for not_deployed browser")
		}
	})

	t.Run("check_failed produces info", func(t *testing.T) {
		snap := base()
		snap.BrowserProtection = []BrowserProtectionStatus{{
			Name: "Brave", ProtectionStatus: "check_failed", Confidence: "low",
			Installed: true, LastError: "access denied",
		}}
		issues := deriveIssues(snap)
		found := false
		for _, i := range issues {
			if i.Title == "Browser Brave status unknown" && i.Severity == "info" {
				if i.ProtectionImpact != "none" {
					t.Fatalf("expected no protection impact, got %q", i.ProtectionImpact)
				}
				found = true
			}
		}
		if !found {
			t.Fatal("expected info issue for check_failed browser")
		}
	})

	t.Run("policy_detected produces no issue", func(t *testing.T) {
		snap := base()
		snap.BrowserProtection = []BrowserProtectionStatus{{
			Name: "Edge", ProtectionStatus: "policy_detected", Confidence: "medium",
			Installed: true, DeploymentDetected: true,
		}}
		issues := deriveIssues(snap)
		for _, i := range issues {
			if strings.Contains(i.Title, "Edge") {
				t.Fatalf("policy_detected browser should not produce an issue, got: %+v", i)
			}
		}
	})

	t.Run("not_installed produces no issue", func(t *testing.T) {
		snap := base()
		snap.BrowserProtection = []BrowserProtectionStatus{{
			Name: "Firefox", ProtectionStatus: "not_installed", Confidence: "high",
		}}
		issues := deriveIssues(snap)
		for _, i := range issues {
			if strings.Contains(i.Title, "Firefox") {
				t.Fatalf("not_installed browser should not produce an issue, got: %+v", i)
			}
		}
	})
}

func TestValidateStartupFiles(t *testing.T) {
	dir := t.TempDir()
	agentPath := filepath.Join(dir, "themisto-agent.exe")
	cfgPath := filepath.Join(dir, "agent.json")

	if err := os.WriteFile(agentPath, []byte("agent"), 0644); err != nil {
		t.Fatalf("write agent file: %v", err)
	}
	if err := os.WriteFile(cfgPath, []byte(`{"gateway_url":"https://localhost"}`), 0644); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	if err := validateStartupFiles(agentPath, cfgPath); err != nil {
		t.Fatalf("validateStartupFiles() unexpected error: %v", err)
	}

	if err := validateStartupFiles(filepath.Join(dir, "missing-agent.exe"), cfgPath); err == nil || !strings.Contains(err.Error(), "agent binary not found") {
		t.Fatalf("expected clear missing-agent error, got %v", err)
	}

	if err := validateStartupFiles(agentPath, filepath.Join(dir, "missing-agent.json")); err == nil || !strings.Contains(err.Error(), "config file not found") {
		t.Fatalf("expected clear missing-config error, got %v", err)
	}
}

func TestSummarizeDiagnosticsIssues(t *testing.T) {
	t.Run("starting issues produce starting status", func(t *testing.T) {
		status, advisories := summarizeDiagnosticsIssues([]DiagnosticIssue{
			{Title: "Gateway retrying after startup", Severity: "info", ProtectionImpact: "starting"},
		})
		if status != "starting" {
			t.Fatalf("status = %q, want starting", status)
		}
		if advisories != 0 {
			t.Fatalf("advisories = %d, want 0", advisories)
		}
	})

	t.Run("advisory only stays healthy", func(t *testing.T) {
		status, advisories := summarizeDiagnosticsIssues([]DiagnosticIssue{
			{Title: "Startup persistence missing", Severity: "warning", ProtectionImpact: "none"},
		})
		if status != "healthy" {
			t.Fatalf("status = %q, want healthy", status)
		}
		if advisories != 1 {
			t.Fatalf("advisories = %d, want 1", advisories)
		}
	})

	t.Run("degraded issue wins over advisories", func(t *testing.T) {
		status, advisories := summarizeDiagnosticsIssues([]DiagnosticIssue{
			{Title: "Gateway unreachable", Severity: "warning", ProtectionImpact: "degraded"},
			{Title: "Startup persistence missing", Severity: "warning", ProtectionImpact: "none"},
		})
		if status != "degraded" {
			t.Fatalf("status = %q, want degraded", status)
		}
		if advisories != 1 {
			t.Fatalf("advisories = %d, want 1", advisories)
		}
	})

	t.Run("error issue wins", func(t *testing.T) {
		status, advisories := summarizeDiagnosticsIssues([]DiagnosticIssue{
			{Title: "Agent service is not running", Severity: "error", ProtectionImpact: "error"},
			{Title: "Startup persistence missing", Severity: "warning", ProtectionImpact: "none"},
		})
		if status != "error" {
			t.Fatalf("status = %q, want error", status)
		}
		if advisories != 1 {
			t.Fatalf("advisories = %d, want 1", advisories)
		}
	})
}

func TestBuildAdminPolicyGuide(t *testing.T) {
	guide := buildAdminPolicyGuide()

	if len(guide.SupportedBrowsers) != len(supportedBrowsers) {
		t.Fatalf("expected %d supported browsers, got %d", len(supportedBrowsers), len(guide.SupportedBrowsers))
	}

	for _, name := range []string{"Chrome", "Edge", "Brave", "Firefox"} {
		found := false
		for _, browser := range guide.SupportedBrowsers {
			if browser == name {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("supported browsers missing %q", name)
		}
	}

	requiredStatuses := map[string]string{
		"deployed":        "does not prove every prompt is actively intercepted",
		"policy_detected": "found browser rollout policy",
		"not_deployed":    "did not find the expected local deployment signal",
		"check_failed":    "could not reliably read the local rollout signal",
		"not_installed":   "did not find this browser on the device",
		"unsupported":     "does not manage or verify extension rollout",
	}

	for status, wantMeaning := range requiredStatuses {
		found := false
		for _, def := range guide.BrowserStatusDefinitions {
			if def.Status == status {
				found = true
				if !strings.Contains(def.Meaning, wantMeaning) {
					t.Fatalf("status %q meaning = %q, want substring %q", status, def.Meaning, wantMeaning)
				}
			}
		}
		if !found {
			t.Fatalf("browser status definitions missing %q", status)
		}
	}

	localSignals := []string{
		`HKLM\SOFTWARE\Policies\Google\Chrome\ExtensionInstallForcelist`,
		`HKLM\SOFTWARE\Mozilla\Firefox\Extensions`,
	}
	for _, want := range localSignals {
		found := false
		for _, check := range guide.LocalChecks {
			if strings.Contains(check.Location, want) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("local checks missing %q", want)
		}
	}

	if len(guide.AuditVisibility) == 0 || len(guide.Limitations) == 0 || len(guide.EmployeeMessaging) == 0 {
		t.Fatal("expected audit visibility, limitations, and employee messaging sections to be populated")
	}

	foundStarting := false
	for _, def := range guide.HealthStatusDefinitions {
		if def.Status == "starting" {
			foundStarting = true
			if !strings.Contains(def.Meaning, "bringing the local protection stack online") {
				t.Fatalf("starting health definition meaning = %q", def.Meaning)
			}
		}
	}
	if !foundStarting {
		t.Fatal("health status definitions missing starting")
	}
}

func TestResolveEnrollmentLink(t *testing.T) {
	app := NewApp()

	t.Run("keeps full enrollment url", func(t *testing.T) {
		link, err := app.resolveEnrollmentLink("https://backend.example/enroll/abc123")
		if err != nil {
			t.Fatalf("resolveEnrollmentLink() unexpected error: %v", err)
		}
		if link != "https://backend.example/enroll/abc123" {
			t.Fatalf("resolveEnrollmentLink() = %q, want full url passthrough", link)
		}
	})

	t.Run("builds link from saved backend and short code", func(t *testing.T) {
		dir := t.TempDir()
		oldConfigPath := agentConfigPath
		agentConfigPath = filepath.Join(dir, "agent.json")
		t.Cleanup(func() { agentConfigPath = oldConfigPath })

		payload := map[string]string{
			"backend_url": "http://localhost:8443",
		}
		data, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal config: %v", err)
		}
		if err := os.WriteFile(agentConfigPath, data, 0600); err != nil {
			t.Fatalf("write config: %v", err)
		}

		link, err := app.resolveEnrollmentLink("abc123")
		if err != nil {
			t.Fatalf("resolveEnrollmentLink() unexpected error: %v", err)
		}
		if link != "http://localhost:8443/enroll/abc123" {
			t.Fatalf("resolveEnrollmentLink() = %q, want synthesized short-code url", link)
		}
	})

	t.Run("requires backend when only short code is provided", func(t *testing.T) {
		dir := t.TempDir()
		oldConfigPath := agentConfigPath
		agentConfigPath = filepath.Join(dir, "agent.json")
		t.Cleanup(func() { agentConfigPath = oldConfigPath })

		_, err := app.resolveEnrollmentLink("abc123")
		if err == nil {
			t.Fatal("expected error when backend url is unknown")
		}
		if !strings.Contains(err.Error(), "backend URL") {
			t.Fatalf("expected backend URL guidance, got %v", err)
		}
	})
}

func TestMergeEnrollmentConfig(t *testing.T) {
	existing := map[string]interface{}{
		"backend_url": "http://localhost:8443",
		"custom_key":  "keep-me",
	}
	imported := map[string]interface{}{
		"device_id":         "device-123",
		"org_name":          "Themisto Dev Org",
		"enrollment_token":  "token-123",
		"insecure_setting":  true,
	}

	merged := mergeEnrollmentConfig(existing, imported)

	if got := stringValue(merged["backend_url"]); got != "http://localhost:8443" {
		t.Fatalf("backend_url = %q, want existing value preserved", got)
	}
	if got := stringValue(merged["org_name"]); got != "Themisto Dev Org" {
		t.Fatalf("org_name = %q, want imported value", got)
	}
	if got := stringValue(merged["agent_id"]); got != "device-123" {
		t.Fatalf("agent_id = %q, want auto-filled from device_id", got)
	}
	if got, _ := merged["custom_key"].(string); got != "keep-me" {
		t.Fatalf("custom_key = %q, want existing custom value preserved", got)
	}
	if got, _ := merged["insecure_setting"].(bool); !got {
		t.Fatalf("insecure_setting = %v, want imported bool to be preserved", merged["insecure_setting"])
	}

	for _, entry := range certPathDefaults {
		if got := stringValue(merged[entry.key]); got != entry.path {
			t.Fatalf("%s = %q, want default path %q", entry.key, got, entry.path)
		}
	}
}
