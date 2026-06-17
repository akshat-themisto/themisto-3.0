package main

import (
	"fmt"
	"strings"
)

// AdminGuideItem is a titled explanatory item for the admin guide surface.
type AdminGuideItem struct {
	Title  string `json:"title"`
	Detail string `json:"detail"`
}

// AdminPolicyCheck describes one local detection signal the desktop app reads.
type AdminPolicyCheck struct {
	Browser  string `json:"browser"`
	Check    string `json:"check"`
	Location string `json:"location"`
	Detail   string `json:"detail"`
}

// AdminStatusDefinition explains how an admin should interpret a state.
type AdminStatusDefinition struct {
	Status      string `json:"status"`
	Label       string `json:"label"`
	Meaning     string `json:"meaning"`
	AdminAction string `json:"admin_action"`
}

// AdminPolicyGuide is the structured admin-facing explanation payload.
type AdminPolicyGuide struct {
	SupportedBrowsers        []string                `json:"supported_browsers"`
	DeploymentMethods        []AdminGuideItem        `json:"deployment_methods"`
	LocalChecks              []AdminPolicyCheck      `json:"local_checks"`
	BrowserStatusDefinitions []AdminStatusDefinition `json:"browser_status_definitions"`
	HealthStatusDefinitions  []AdminStatusDefinition `json:"health_status_definitions"`
	AuditVisibility          []AdminGuideItem        `json:"audit_visibility"`
	Limitations              []string                `json:"limitations"`
	Troubleshooting          []AdminGuideItem        `json:"troubleshooting"`
	EmployeeMessaging        []AdminGuideItem        `json:"employee_messaging"`
}

// GetAdminPolicyGuide returns backend-authored admin explanation content.
func (a *App) GetAdminPolicyGuide() AdminPolicyGuide {
	return buildAdminPolicyGuide()
}

func buildAdminPolicyGuide() AdminPolicyGuide {
	supported := make([]string, 0, len(supportedBrowsers))
	checks := make([]AdminPolicyCheck, 0, len(supportedBrowsers)*2)

	for _, browser := range supportedBrowsers {
		supported = append(supported, browser.name)

		if installPaths, ok := browserRegistry[browser.key]; ok {
			checks = append(checks, AdminPolicyCheck{
				Browser:  browser.name,
				Check:    "Browser detection",
				Location: strings.Join(installPaths, " or "),
				Detail:   fmt.Sprintf("Themisto checks these installation registry keys and then falls back to common executable locations to decide whether %s is present on the device.", browser.name),
			})
		}

		if browser.key == "firefox" {
			checks = append(checks, AdminPolicyCheck{
				Browser:  browser.name,
				Check:    "Extension deployment",
				Location: fmt.Sprintf("%s [%s]", firefoxExtKey, firefoxExtID),
				Detail:   "Themisto checks the Firefox enterprise extensions key for the Themisto extension registration.",
			})
			continue
		}

		if policyKey, ok := extensionPolicyKeys[browser.key]; ok {
			checks = append(checks, AdminPolicyCheck{
				Browser:  browser.name,
				Check:    "Extension deployment",
				Location: policyKey,
				Detail:   fmt.Sprintf("Themisto checks this force-install policy list for the Themisto extension ID (%s).", extensionID),
			})
		}
	}

	return AdminPolicyGuide{
		SupportedBrowsers: supported,
		DeploymentMethods: []AdminGuideItem{
			{
				Title:  "Chromium browsers",
				Detail: fmt.Sprintf("Chrome, Edge, and Brave are expected to use the Windows ExtensionInstallForcelist policy with the Themisto extension ID and update URL %s.", chromiumUpdateURL),
			},
			{
				Title:  "Firefox",
				Detail: fmt.Sprintf("Firefox rollout is expected to use the enterprise extensions registration key for %s. Production force-install still depends on signed XPI distribution outside this desktop app; the desktop app only checks the local registration signal.", firefoxExtID),
			},
		},
		LocalChecks: checks,
		BrowserStatusDefinitions: []AdminStatusDefinition{
			{
				Status:      "deployed",
				Label:       "Deployment detected",
				Meaning:     "Themisto found extension evidence in local browser data for this browser. This is stronger than policy detection alone, but it still does not prove every prompt is actively intercepted in the current session.",
				AdminAction: "If users still report missing protection, ask them to reopen the browser, then use diagnostics to confirm device health before treating it as a browser-runtime issue.",
			},
			{
				Status:      "policy_detected",
				Label:       "Policy detected",
				Meaning:     "Themisto found browser rollout policy for this browser, but it has not yet confirmed the extension inside the local browser profile data.",
				AdminAction: "Ask the employee to fully reopen the browser so managed extension policy can apply, then refresh diagnostics.",
			},
			{
				Status:      "not_deployed",
				Label:       "Not deployed",
				Meaning:     "The browser is installed, but Themisto did not find the expected local deployment signal for the extension on this device.",
				AdminAction: "Deploy the Themisto extension through GPO, MDM, or the expected browser policy for this browser, then ask the employee to reopen the browser.",
			},
			{
				Status:      "check_failed",
				Label:       "Check failed",
				Meaning:     "Themisto could not reliably read the local rollout signal. Treat this as unknown state rather than assuming protection is missing or present.",
				AdminAction: "Verify local registry or policy access, rerun diagnostics, and resolve the check failure before making rollout claims.",
			},
			{
				Status:      "not_installed",
				Label:       "Browser not installed",
				Meaning:     "Themisto did not find this browser on the device, so extension deployment is not expected locally.",
				AdminAction: "No action is needed unless this browser is expected in your environment or the user plans to use it.",
			},
			{
				Status:      "unsupported",
				Label:       "Unsupported browser",
				Meaning:     "Themisto detected a browser on this device, but the current desktop build does not manage or verify extension rollout for it.",
				AdminAction: "Use a supported browser like Chrome, Edge, Brave, or Firefox if Themisto browser protection is required.",
			},
		},
		HealthStatusDefinitions: []AdminStatusDefinition{
			{
				Status:      "healthy",
				Label:       "Healthy",
				Meaning:     "Diagnostics found no current issues with the local agent, enrollment, agent service, or browser rollout signals.",
				AdminAction: "Use browser rollout details to validate deployment expectations, but do not treat healthy local diagnostics as proof of browser runtime interception.",
			},
			{
				Status:      "starting",
				Label:       "Starting",
				Meaning:     "Themisto is still bringing the local protection stack online or retrying gateway connectivity after startup. This is usually a short-lived boot condition rather than a hard failure.",
				AdminAction: "Let the agent continue retrying during boot. If the device stays in this state for several minutes, refresh diagnostics and investigate gateway or local proxy startup issues.",
			},
			{
				Status:      "degraded",
				Label:       "Degraded",
				Meaning:     "Themisto found warning-level issues such as incomplete browser rollout or gateway connectivity concerns.",
				AdminAction: "Address warnings before expanding rollout, and ask employees to export diagnostics when symptoms continue.",
			},
			{
				Status:      "error",
				Label:       "Error",
				Meaning:     "Themisto found a critical local issue such as agent failure, missing service, missing certificates, or incomplete configuration.",
				AdminAction: "Repair core device health first; browser rollout explanations are secondary until the desktop agent is healthy again.",
			},
		},
		AuditVisibility: []AdminGuideItem{
			{
				Title:  "Local rollout visibility",
				Detail: "This desktop app can observe agent health, enrollment state, agent service status, file presence, and browser rollout signals derived from local policy and registry checks.",
			},
			{
				Title:  "Diagnostics export",
				Detail: "Admins and employees can copy or export diagnostics from the desktop app to share device-local evidence with IT support.",
			},
			{
				Title:  "Server-side audit history",
				Detail: "This Step 3 desktop surface explains audit visibility, but it does not fetch live backend audit entries. Use backend or dashboard audit views for server-side history.",
			},
		},
		Limitations: []string{
			"Runtime browser extension activity is not fully verified in this phase.",
			"Policy and registry detection show rollout signals, not guaranteed prompt interception.",
			"This desktop admin page is explanation-first and does not replace backend audit history.",
			"Firefox production deployment still depends on signed XPI distribution outside the desktop app.",
		},
		Troubleshooting: []AdminGuideItem{
			{
				Title:  "When deployment is missing",
				Detail: "Treat not_deployed as a rollout gap on that device. Confirm the correct browser policy is targeted, then ask the employee to reopen the browser after policy refresh.",
			},
			{
				Title:  "When policy is present but not yet loaded",
				Detail: "Treat policy_detected as a pending browser-side apply state. Ask the employee to fully close and reopen the browser, then refresh diagnostics before declaring rollout broken.",
			},
			{
				Title:  "When a check fails",
				Detail: "Treat check_failed as uncertainty, not absence. Verify local policy access and rerun diagnostics before escalating the issue as a deployment failure.",
			},
			{
				Title:  "When diagnostics are degraded or error",
				Detail: "Fix the agent, enrollment, certificate, or agent service issue first. Browser rollout interpretation is less trustworthy when core device health is broken.",
			},
			{
				Title:  "When rollout is mixed",
				Detail: "Use the per-browser table to separate browsers that are correctly deployed from those still missing policy. Mixed rollout usually indicates policy targeting or browser-specific deployment gaps.",
			},
		},
		EmployeeMessaging: []AdminGuideItem{
			{
				Title:  "What to tell employees when rollout is missing",
				Detail: "Tell them the browser is installed but local Themisto deployment was not detected yet, and that IT is updating policy. Ask them to keep the desktop app available and reopen the browser after the change.",
			},
			{
				Title:  "What to tell employees when state is unknown",
				Detail: "Ask them to refresh the Extensions page and export diagnostics. Explain that Themisto could not confirm the local rollout state yet and IT is checking the device signal.",
			},
			{
				Title:  "What to tell employees after policy changes",
				Detail: "Ask them to reopen the affected browser, then refresh Themisto. If the state still looks wrong, have them share the exported diagnostics bundle with IT.",
			},
		},
	}
}
