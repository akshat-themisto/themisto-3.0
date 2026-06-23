package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/themisto/backend/internal/store"
	"github.com/themisto/backend/internal/token"
)

type operatorCreateOrgRequest struct {
	Name             string `json:"name"`
	Slug             string `json:"slug"`
	OwnerEmail       string `json:"owner_email"`
	OwnerName        string `json:"owner_name"`
	OwnerPassword    string `json:"owner_password"`
	PublicBackendURL string `json:"public_backend_url"`
	PublicGatewayURL string `json:"public_gateway_url"`
}

type operatorProvisioningRequest struct {
	PublicBackendURL string `json:"public_backend_url"`
	PublicGatewayURL string `json:"public_gateway_url"`
}

type operatorStatusRequest struct {
	Status      string `json:"status"`
	Reason      string `json:"reason"`
	RevokeCerts bool   `json:"revoke_certs"`
}

type operatorDeploymentPackageRequest struct {
	DeviceName       string `json:"device_name"`
	OS               string `json:"os"`
	AgentVersion     string `json:"agent_version"`
	TokenTTLHours    int    `json:"token_ttl_hours,omitempty"`
	PublicBackendURL string `json:"public_backend_url"`
	PublicGatewayURL string `json:"public_gateway_url"`
}

type operatorPreflightCheck struct {
	Key     string `json:"key"`
	Label   string `json:"label"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

type operatorDeploymentPackageResponse struct {
	Organization       *store.OperatorOrganization `json:"organization"`
	Package            enrollmentPackageResponse   `json:"package"`
	AgentConfigJSON    string                      `json:"agent_config_json"`
	WindowsInstallPS1  string                      `json:"windows_install_ps1"`
	MacInstallScript   string                      `json:"mac_install_script"`
	ChromiumPolicyJSON string                      `json:"chromium_policy_json"`
	MDMNotes           []string                    `json:"mdm_notes"`
	Preflight          []operatorPreflightCheck    `json:"preflight"`
}

func (s *Server) handleOperatorListOrgs(w http.ResponseWriter, r *http.Request) {
	orgs, err := s.store.ListOperatorOrganizations(r.Context())
	if err != nil {
		s.logger.Error("operator list orgs", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"organizations": orgs})
}

func (s *Server) handleOperatorCreateOrg(w http.ResponseWriter, r *http.Request) {
	var req operatorCreateOrgRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON body")
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	req.Slug = strings.ToLower(strings.TrimSpace(req.Slug))
	req.OwnerEmail = strings.ToLower(strings.TrimSpace(req.OwnerEmail))
	req.OwnerName = strings.TrimSpace(req.OwnerName)
	req.PublicBackendURL = strings.TrimSpace(req.PublicBackendURL)
	req.PublicGatewayURL = strings.TrimSpace(req.PublicGatewayURL)

	if req.Name == "" || req.Slug == "" {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "name and slug are required")
		return
	}
	if !orgSlugRe.MatchString(req.Slug) {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "slug must be 3-64 lowercase alphanumeric chars or hyphens")
		return
	}
	if req.OwnerEmail == "" || req.OwnerPassword == "" {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "owner_email and owner_password are required")
		return
	}
	if err := validatePublicURL(req.PublicBackendURL, false); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_BACKEND_URL", err.Error())
		return
	}
	if err := validatePublicURL(req.PublicGatewayURL, false); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_GATEWAY_URL", err.Error())
		return
	}

	rawAPIKey, apiKeyHash, err := token.Generate()
	if err != nil {
		s.logger.Error("operator generate org api key", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}

	org, err := s.store.CreateOrganization(r.Context(), req.Name, req.Slug, apiKeyHash)
	if err != nil {
		if isDuplicateError(err) {
			writeError(w, http.StatusConflict, "ORG_EXISTS", "an organization with this slug already exists")
			return
		}
		s.logger.Error("operator create org", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	if req.PublicBackendURL != "" || req.PublicGatewayURL != "" {
		if err := s.store.UpdateOrganizationProvisioning(r.Context(), org.ID, req.PublicBackendURL, req.PublicGatewayURL); err != nil {
			s.logger.Error("operator update provisioning", "error", err)
			writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
			return
		}
	}

	ownerName := req.OwnerName
	if ownerName == "" {
		ownerName = "Owner"
	}
	owner, err := s.store.CreateUser(r.Context(), org.ID, req.OwnerEmail, ownerName, req.OwnerPassword, "owner")
	if err != nil {
		s.logger.Error("operator create owner", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "organization created, but owner user could not be created")
		return
	}

	_ = s.store.InsertAudit(r.Context(), nil, &store.AuditEntry{
		ActorType:    "system",
		ActorID:      getOperatorActorID(r),
		OrgID:        &org.ID,
		Action:       "operator.org.registered",
		ResourceType: "organization",
		ResourceID:   org.ID,
		Details: map[string]interface{}{
			"name":               req.Name,
			"slug":               req.Slug,
			"owner_email":        req.OwnerEmail,
			"public_backend_url": req.PublicBackendURL,
			"public_gateway_url": req.PublicGatewayURL,
		},
	})

	orgView, _ := s.store.GetOperatorOrganization(r.Context(), org.ID)
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"organization": orgView,
		"owner": map[string]interface{}{
			"id":    owner.ID,
			"email": owner.Email,
			"name":  owner.Name,
			"role":  owner.Role,
		},
		"api_key": rawAPIKey,
	})
}

func (s *Server) handleOperatorUpdateProvisioning(w http.ResponseWriter, r *http.Request) {
	orgID := strings.TrimSpace(r.PathValue("orgID"))
	var req operatorProvisioningRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON body")
		return
	}
	req.PublicBackendURL = strings.TrimSpace(req.PublicBackendURL)
	req.PublicGatewayURL = strings.TrimSpace(req.PublicGatewayURL)
	if err := validatePublicURL(req.PublicBackendURL, false); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_BACKEND_URL", err.Error())
		return
	}
	if err := validatePublicURL(req.PublicGatewayURL, false); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_GATEWAY_URL", err.Error())
		return
	}

	if err := s.store.UpdateOrganizationProvisioning(r.Context(), orgID, req.PublicBackendURL, req.PublicGatewayURL); err != nil {
		s.logger.Error("operator update org provisioning", "org_id", orgID, "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	_ = s.store.InsertAudit(r.Context(), nil, &store.AuditEntry{
		ActorType:    "system",
		ActorID:      getOperatorActorID(r),
		OrgID:        &orgID,
		Action:       "operator.org.provisioning.updated",
		ResourceType: "organization",
		ResourceID:   orgID,
		Details: map[string]interface{}{
			"public_backend_url": req.PublicBackendURL,
			"public_gateway_url": req.PublicGatewayURL,
		},
	})
	org, _ := s.store.GetOperatorOrganization(r.Context(), orgID)
	writeJSON(w, http.StatusOK, map[string]interface{}{"organization": org})
}

func (s *Server) handleOperatorCreateDeploymentPackage(w http.ResponseWriter, r *http.Request) {
	orgID := strings.TrimSpace(r.PathValue("orgID"))
	var req operatorDeploymentPackageRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON body")
		return
	}
	req.DeviceName = strings.TrimSpace(req.DeviceName)
	req.OS = strings.ToLower(strings.TrimSpace(req.OS))
	req.AgentVersion = strings.TrimSpace(req.AgentVersion)
	req.PublicBackendURL = strings.TrimRight(strings.TrimSpace(req.PublicBackendURL), "/")
	req.PublicGatewayURL = strings.TrimRight(strings.TrimSpace(req.PublicGatewayURL), "/")
	if req.DeviceName == "" {
		req.DeviceName = fmt.Sprintf("deployment-%s", time.Now().UTC().Format("20060102150405"))
	}
	if req.OS == "" {
		req.OS = "windows"
	}
	if req.AgentVersion == "" {
		req.AgentVersion = "1.0.0"
	}
	if err := validatePublicURL(req.PublicBackendURL, true); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_BACKEND_URL", err.Error())
		return
	}
	if err := validatePublicURL(req.PublicGatewayURL, true); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_GATEWAY_URL", err.Error())
		return
	}

	if err := s.store.UpdateOrganizationProvisioning(r.Context(), orgID, req.PublicBackendURL, req.PublicGatewayURL); err != nil {
		s.logger.Error("operator deployment save provisioning", "org_id", orgID, "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}

	ttl := s.resolveTokenTTL(req.TokenTTLHours)
	org, device, rawToken, expiresAt, err := s.createDeviceAndEnrollmentTokenWithTTL(
		r.Context(),
		orgID,
		req.DeviceName,
		req.OS,
		req.AgentVersion,
		"system",
		getOperatorActorID(r),
		ttl,
	)
	if err != nil {
		s.handleDeviceCreationError(w, req.DeviceName, err)
		return
	}

	backendURL, gatewayURL := s.urlsForOrg(org)
	agentConfig := buildAgentConfig(backendURL, gatewayURL, device.ID, org.Name, rawToken)
	enrollmentURL, err := s.storeEnrollmentShortCode(r, device.ID, org.ID, agentConfig, expiresAt)
	if err != nil {
		s.logger.Warn("operator deployment short code failed", "org_id", orgID, "error", err)
	}
	pkg := enrollmentPackageResponse{
		DeviceID:        device.ID,
		DeviceName:      device.DeviceName,
		OS:              device.OS,
		OrgName:         org.Name,
		BackendURL:      backendURL,
		GatewayURL:      gatewayURL,
		EnrollmentToken: rawToken,
		EnrollmentURL:   enrollmentURL,
		TokenExpiresAt:  expiresAt,
		ActivateMacOS:   buildMacActivationCommand(backendURL, gatewayURL, device.ID, org.Name, rawToken),
		ActivateWindows: buildWindowsActivationCommand(backendURL, gatewayURL, device.ID, org.Name, rawToken),
		AgentConfig:     agentConfig,
	}

	agentConfigJSON, err := prettyJSON(agentConfig)
	if err != nil {
		s.logger.Error("operator deployment marshal agent config", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	chromiumPolicy, _ := prettyJSON(map[string]interface{}{
		"ExtensionInstallForcelist": []string{
			"<themisto-extension-id>;https://clients2.google.com/service/update2/crx",
		},
		"ExtensionSettings": map[string]interface{}{
			"<themisto-extension-id>": map[string]interface{}{
				"installation_mode": "force_installed",
				"update_url":        "https://clients2.google.com/service/update2/crx",
			},
		},
	})

	_ = s.store.InsertAudit(r.Context(), nil, &store.AuditEntry{
		ActorType:    "system",
		ActorID:      getOperatorActorID(r),
		OrgID:        &orgID,
		Action:       "operator.deployment_package.created",
		ResourceType: "device",
		ResourceID:   device.ID,
		Details: map[string]interface{}{
			"device_name":        device.DeviceName,
			"os":                 device.OS,
			"public_backend_url": backendURL,
			"public_gateway_url": gatewayURL,
			"token_expires":      expiresAt,
		},
	})

	orgView, _ := s.store.GetOperatorOrganization(r.Context(), orgID)
	writeJSON(w, http.StatusCreated, operatorDeploymentPackageResponse{
		Organization:       orgView,
		Package:            pkg,
		AgentConfigJSON:    agentConfigJSON,
		WindowsInstallPS1:  buildWindowsDeploymentScript(agentConfigJSON),
		MacInstallScript:   buildMacDeploymentScript(agentConfigJSON),
		ChromiumPolicyJSON: chromiumPolicy,
		MDMNotes: []string{
			"Windows: deploy the signed Themisto installer, then place agent.json in C:\\ProgramData\\Themisto before first launch or embed this JSON during installer build.",
			"Chromium browsers: replace <themisto-extension-id> with the published extension ID before using force-install policy in Intune, Google Admin, or GPO.",
			"macOS: deploy the signed PKG with the same agent.json payload via Jamf or another MDM.",
			"This package is for one enrollment device/token. For fleets, generate one package per target device or use the bulk enrollment API.",
		},
		Preflight: buildOperatorPreflight(r.Context(), req.PublicBackendURL, req.PublicGatewayURL),
	})
}

func (s *Server) handleOperatorUpdateStatus(w http.ResponseWriter, r *http.Request) {
	orgID := strings.TrimSpace(r.PathValue("orgID"))
	var req operatorStatusRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON body")
		return
	}
	req.Status = strings.ToLower(strings.TrimSpace(req.Status))
	req.Reason = strings.TrimSpace(req.Reason)
	if req.Status != "active" && req.Status != "suspended" && req.Status != "offboarded" {
		writeError(w, http.StatusBadRequest, "INVALID_STATUS", "status must be active, suspended, or offboarded")
		return
	}
	if req.Status != "active" && req.Reason == "" {
		writeError(w, http.StatusBadRequest, "INVALID_REASON", "reason is required when suspending or offboarding an org")
		return
	}

	tx, err := s.store.DB.BeginTx(r.Context(), nil)
	if err != nil {
		s.logger.Error("operator begin status tx", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	defer tx.Rollback()

	if err := s.store.UpdateOrganizationStatus(r.Context(), tx, orgID, req.Status, req.Reason, getOperatorActorID(r)); err != nil {
		s.logger.Error("operator update org status", "org_id", orgID, "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}

	var revokedCerts int64
	var expiredTokens int64
	var expiredShortCodes int64
	var deviceRows int64

	if req.Status == "suspended" {
		expiredTokens, expiredShortCodes, err = s.store.ExpireEnrollmentForOrg(r.Context(), tx, orgID)
		if err != nil {
			s.logger.Error("operator expire suspended enrollment", "org_id", orgID, "error", err)
			writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
			return
		}
	}
	if req.Status == "offboarded" {
		revokedCerts, err = s.store.RevokeActiveCertificatesForOrg(r.Context(), tx, orgID, "org_offboarded")
		if err != nil {
			s.logger.Error("operator revoke org certs", "org_id", orgID, "error", err)
			writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
			return
		}
		deviceRows, err = s.store.SetOrgDevicesStatus(r.Context(), tx, orgID, "decommissioned")
		if err != nil {
			s.logger.Error("operator decommission org devices", "org_id", orgID, "error", err)
			writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
			return
		}
		expiredTokens, expiredShortCodes, err = s.store.ExpireEnrollmentForOrg(r.Context(), tx, orgID)
		if err != nil {
			s.logger.Error("operator expire org enrollment", "org_id", orgID, "error", err)
			writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
			return
		}
	}
	if req.RevokeCerts && req.Status != "offboarded" {
		revokedCerts, err = s.store.RevokeActiveCertificatesForOrg(r.Context(), tx, orgID, "admin_action")
		if err != nil {
			s.logger.Error("operator revoke org certs", "org_id", orgID, "error", err)
			writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
			return
		}
	}

	_ = s.store.InsertAudit(r.Context(), tx, &store.AuditEntry{
		ActorType:    "system",
		ActorID:      getOperatorActorID(r),
		OrgID:        &orgID,
		Action:       "operator.org.status.updated",
		ResourceType: "organization",
		ResourceID:   orgID,
		Details: map[string]interface{}{
			"status":              req.Status,
			"reason":              req.Reason,
			"revoked_certs":       revokedCerts,
			"expired_tokens":      expiredTokens,
			"expired_short_codes": expiredShortCodes,
			"devices_updated":     deviceRows,
		},
	})

	if err := tx.Commit(); err != nil {
		s.logger.Error("operator commit status tx", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	org, _ := s.store.GetOperatorOrganization(r.Context(), orgID)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"organization":        org,
		"revoked_certs":       revokedCerts,
		"expired_tokens":      expiredTokens,
		"expired_short_codes": expiredShortCodes,
		"devices_updated":     deviceRows,
	})
}

func (s *Server) handleOperatorRevokeOrgCerts(w http.ResponseWriter, r *http.Request) {
	orgID := strings.TrimSpace(r.PathValue("orgID"))
	tx, err := s.store.DB.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	defer tx.Rollback()
	count, err := s.store.RevokeActiveCertificatesForOrg(r.Context(), tx, orgID, "admin_action")
	if err != nil {
		s.logger.Error("operator revoke org certs", "org_id", orgID, "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	_ = s.store.InsertAudit(r.Context(), tx, &store.AuditEntry{
		ActorType:    "system",
		ActorID:      getOperatorActorID(r),
		OrgID:        &orgID,
		Action:       "operator.org.certs.revoked",
		ResourceType: "organization",
		ResourceID:   orgID,
		Details:      map[string]interface{}{"revoked_certs": count},
	})
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"revoked_certs": count})
}

func (s *Server) handleEnforcementOrgStatus(w http.ResponseWriter, r *http.Request) {
	orgID := strings.TrimSpace(r.PathValue("orgID"))
	org, err := s.store.GetOrganization(r.Context(), orgID)
	if err != nil {
		s.logger.Error("enforcement get org status", "org_id", orgID, "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	if org == nil {
		writeError(w, http.StatusNotFound, "ORG_NOT_FOUND", "organization not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":             org.Status,
		"reason":             org.StatusReason,
		"public_backend_url": org.PublicBackendURL,
		"public_gateway_url": org.PublicGatewayURL,
	})
}

func (s *Server) handleEnforcementDeviceStatus(w http.ResponseWriter, r *http.Request) {
	deviceID := strings.TrimSpace(r.PathValue("deviceID"))
	device, err := s.store.GetDevice(r.Context(), deviceID)
	if err != nil {
		s.logger.Error("enforcement get device", "device_id", deviceID, "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	if device == nil {
		writeError(w, http.StatusNotFound, "DEVICE_NOT_FOUND", "device not found")
		return
	}
	org, err := s.store.GetOrganization(r.Context(), device.OrgID)
	if err != nil {
		s.logger.Error("enforcement get org for device", "device_id", deviceID, "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	orgStatus := "unknown"
	reason := ""
	if org != nil {
		orgStatus = org.Status
		reason = org.StatusReason
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":              device.Status,
		"status_reason":       reason,
		"organization_status": orgStatus,
	})
}

func validatePublicURL(raw string, required bool) error {
	if raw == "" {
		if required {
			return &url.Error{Op: "validate", URL: raw, Err: errURLRequired{}}
		}
		return nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return &url.Error{Op: "validate", URL: raw, Err: errURLAbsolute{}}
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return &url.Error{Op: "validate", URL: raw, Err: errURLHTTPOnly{}}
	}
	return nil
}

type errURLRequired struct{}

func (errURLRequired) Error() string { return "url is required" }

type errURLAbsolute struct{}

func (errURLAbsolute) Error() string {
	return "url must be absolute, for example https://gateway.company.com"
}

type errURLHTTPOnly struct{}

func (errURLHTTPOnly) Error() string { return "url must start with https:// or http://" }

func prettyJSON(v interface{}) (string, error) {
	body, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func buildWindowsDeploymentScript(agentConfigJSON string) string {
	escaped := strings.ReplaceAll(agentConfigJSON, "@'", "@''")
	return fmt.Sprintf(`$ErrorActionPreference = "Stop"
$themistoData = "C:\ProgramData\Themisto"
New-Item -ItemType Directory -Force -Path $themistoData | Out-Null
@'
%s
'@ | Set-Content -Encoding UTF8 -Path (Join-Path $themistoData "agent.json")
Start-Process -FilePath ".\ThemistoInstaller.exe" -Wait -Verb RunAs
`, escaped)
}

func buildMacDeploymentScript(agentConfigJSON string) string {
	escaped := strings.ReplaceAll(agentConfigJSON, "'", "'\"'\"'")
	return fmt.Sprintf(`#!/bin/sh
set -eu
sudo mkdir -p /Library/Application\ Support/Themisto
printf '%%s
' '%s' | sudo tee /Library/Application\ Support/Themisto/agent.json >/dev/null
sudo installer -pkg ./Themisto.pkg -target /
`, escaped)
}

func buildOperatorPreflight(ctx context.Context, backendURL, gatewayURL string) []operatorPreflightCheck {
	checks := []operatorPreflightCheck{
		{Key: "backend_url", Label: "Backend URL configured", Status: "ok", Message: backendURL},
		{Key: "gateway_url", Label: "Gateway URL configured", Status: "ok", Message: gatewayURL},
	}
	checks = append(checks, probeHealth(ctx, "backend_health", "Backend /healthz", backendURL))
	checks = append(checks, probeHealth(ctx, "gateway_health", "Gateway /healthz", gatewayURL))
	checks = append(checks, operatorPreflightCheck{
		Key:     "enrollment_package",
		Label:   "Enrollment package",
		Status:  "ok",
		Message: "Device record and short-lived enrollment token were created.",
	})
	return checks
}

func probeHealth(parent context.Context, key, label, baseURL string) operatorPreflightCheck {
	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()

	u := strings.TrimRight(baseURL, "/") + "/healthz"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return operatorPreflightCheck{Key: key, Label: label, Status: "fail", Message: err.Error()}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return operatorPreflightCheck{Key: key, Label: label, Status: "warn", Message: err.Error()}
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return operatorPreflightCheck{Key: key, Label: label, Status: "ok", Message: fmt.Sprintf("%s returned %d", u, resp.StatusCode)}
	}
	return operatorPreflightCheck{Key: key, Label: label, Status: "warn", Message: fmt.Sprintf("%s returned %d", u, resp.StatusCode)}
}
