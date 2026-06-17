package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/themisto/backend/internal/controlplane"
	"github.com/themisto/backend/internal/store"
	"github.com/themisto/backend/internal/token"
)

var deviceNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9\-]{0,127}$`)

type registerRequest struct {
	OrgID        string `json:"org_id"`
	DeviceName   string `json:"device_name"`
	OS           string `json:"os"`
	AgentVersion string `json:"agent_version"`
}

type registerResponse struct {
	DeviceID        string    `json:"device_id"`
	EnrollmentToken string    `json:"enrollment_token"`
	TokenExpiresAt  time.Time `json:"token_expires_at"`
}

type createEnrollmentPackageRequest struct {
	DeviceName    string `json:"device_name"`
	OS            string `json:"os"`
	AgentVersion  string `json:"agent_version"`
	TokenTTLHours int    `json:"token_ttl_hours,omitempty"` // 0 = use server default (24h), max 168 (7d)
}

type enrollmentPackageResponse struct {
	DeviceID        string                `json:"device_id"`
	DeviceName      string                `json:"device_name"`
	OS              string                `json:"os"`
	OrgName         string                `json:"org_name"`
	BackendURL      string                `json:"backend_url"`
	GatewayURL      string                `json:"gateway_url"`
	EnrollmentToken string                `json:"enrollment_token"`
	EnrollmentURL   string                `json:"enrollment_url,omitempty"`
	TokenExpiresAt  time.Time             `json:"token_expires_at"`
	ActivateMacOS   string                `json:"activate_macos_command"`
	ActivateWindows string                `json:"activate_windows_command"`
	AgentConfig     enrollmentAgentConfig `json:"agent_config"`
}

type reissueEnrollmentTokenResponse struct {
	DeviceID        string                `json:"device_id"`
	EnrollmentToken string                `json:"enrollment_token"`
	EnrollmentURL   string                `json:"enrollment_url,omitempty"`
	TokenExpiresAt  time.Time             `json:"token_expires_at"`
	ActivateMacOS   string                `json:"activate_macos_command"`
	ActivateWindows string                `json:"activate_windows_command"`
	AgentConfig     enrollmentAgentConfig `json:"agent_config"`
}

type enrollmentAgentConfig struct {
	AgentID                string `json:"agent_id"`
	GatewayURL             string `json:"gateway_url"`
	ListenAddr             string `json:"listen_addr"`
	DefaultDecision        string `json:"default_decision"`
	TelemetryFlushInterval string `json:"telemetry_flush_interval"`
	BackendURL             string `json:"backend_url"`
	DeviceID               string `json:"device_id"`
	EnrollmentToken        string `json:"enrollment_token"`
	OrgName                string `json:"org_name"`
}

func (s *Server) handleRegisterDevice(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON body")
		return
	}

	if req.OrgID == "" {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "org_id is required")
		return
	}

	_, device, rawToken, expiresAt, err := s.createDeviceAndEnrollmentToken(
		r.Context(),
		req.OrgID,
		req.DeviceName,
		req.OS,
		req.AgentVersion,
		"admin",
		"api-key",
	)
	if err != nil {
		s.handleDeviceCreationError(w, req.DeviceName, err)
		return
	}

	writeJSON(w, http.StatusCreated, registerResponse{
		DeviceID:        device.ID,
		EnrollmentToken: rawToken,
		TokenExpiresAt:  expiresAt,
	})
}

func (s *Server) resolveTokenTTL(requestedHours int) time.Duration {
	if requestedHours <= 0 {
		return s.tokenTTL
	}
	if requestedHours > 168 { // max 7 days
		requestedHours = 168
	}
	return time.Duration(requestedHours) * time.Hour
}

func (s *Server) handleCreateEnrollmentPackage(w http.ResponseWriter, r *http.Request) {
	user := getUserFromContext(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}

	var req createEnrollmentPackageRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON body")
		return
	}

	ttl := s.resolveTokenTTL(req.TokenTTLHours)
	org, device, rawToken, expiresAt, err := s.createDeviceAndEnrollmentTokenWithTTL(
		r.Context(),
		user.OrgID,
		req.DeviceName,
		req.OS,
		req.AgentVersion,
		"admin",
		user.ID,
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
		s.logger.Warn("failed to create enrollment short code", "error", err)
	}

	writeJSON(w, http.StatusCreated, enrollmentPackageResponse{
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
	})
}

func (s *Server) handleReissueEnrollmentToken(w http.ResponseWriter, r *http.Request) {
	user := getUserFromContext(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}

	deviceID := r.PathValue("deviceID")
	device, err := s.store.GetDevice(r.Context(), deviceID)
	if err != nil {
		s.logger.Error("get device", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	if device == nil || device.OrgID != user.OrgID {
		writeError(w, http.StatusNotFound, "DEVICE_NOT_FOUND", "device not found")
		return
	}

	org, err := s.store.GetOrganization(r.Context(), user.OrgID)
	if err != nil {
		s.logger.Error("get org", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	if org == nil {
		writeError(w, http.StatusNotFound, "ORG_NOT_FOUND", "organization not found")
		return
	}
	if err := s.ensureOrganizationAllowed(r.Context(), user.OrgID); err != nil {
		s.handleControlPlaneError(w, err)
		return
	}

	rawToken, expiresAt, err := s.issueEnrollmentToken(r.Context(), deviceID)
	if err != nil {
		s.logger.Error("create enrollment token", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}

	orgID := user.OrgID
	_ = s.store.InsertAudit(r.Context(), nil, &store.AuditEntry{
		ActorType:    "admin",
		ActorID:      user.ID,
		OrgID:        &orgID,
		Action:       "device.enrollment_token.issued",
		ResourceType: "device",
		ResourceID:   deviceID,
		Details: map[string]interface{}{
			"device_name":    device.DeviceName,
			"token_expires":  expiresAt,
			"token_reissued": true,
		},
	})

	backendURL, gatewayURL := s.urlsForOrg(org)
	agentConfig := buildAgentConfig(backendURL, gatewayURL, deviceID, org.Name, rawToken)

	enrollmentURL, err := s.storeEnrollmentShortCode(r, deviceID, org.ID, agentConfig, expiresAt)
	if err != nil {
		s.logger.Warn("failed to create enrollment short code", "error", err)
	}

	writeJSON(w, http.StatusCreated, reissueEnrollmentTokenResponse{
		DeviceID:        deviceID,
		EnrollmentToken: rawToken,
		EnrollmentURL:   enrollmentURL,
		TokenExpiresAt:  expiresAt,
		ActivateMacOS:   buildMacActivationCommand(backendURL, gatewayURL, deviceID, org.Name, rawToken),
		ActivateWindows: buildWindowsActivationCommand(backendURL, gatewayURL, deviceID, org.Name, rawToken),
		AgentConfig:     agentConfig,
	})
}

func (s *Server) handleListDevices(w http.ResponseWriter, r *http.Request) {
	user := getUserFromContext(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}
	// Always scope to the authenticated user's org — ignore orgID in URL path.
	orgID := user.OrgID
	status := r.URL.Query().Get("status")

	devices, err := s.store.ListDevices(r.Context(), orgID, status)
	if err != nil {
		s.logger.Error("list devices", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}

	type deviceEntry struct {
		DeviceID      string     `json:"device_id"`
		DeviceName    string     `json:"device_name"`
		OS            string     `json:"os"`
		Status        string     `json:"status"`
		CertSerial    *string    `json:"cert_serial,omitempty"`
		CertExpiresAt *time.Time `json:"cert_expires_at,omitempty"`
		EnrolledAt    *time.Time `json:"enrolled_at,omitempty"`
	}

	result := make([]deviceEntry, 0, len(devices))
	for _, d := range devices {
		result = append(result, deviceEntry{
			DeviceID:      d.ID,
			DeviceName:    d.DeviceName,
			OS:            d.OS,
			Status:        d.Status,
			CertSerial:    d.CertSerial,
			CertExpiresAt: d.CertExpiresAt,
			EnrolledAt:    d.EnrolledAt,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"devices": result,
		"total":   len(result),
	})
}

func (s *Server) createDeviceAndEnrollmentToken(
	ctx context.Context,
	orgID string,
	deviceName string,
	osType string,
	agentVersion string,
	actorType string,
	actorID string,
) (*store.Organization, *store.Device, string, time.Time, error) {
	return s.createDeviceAndEnrollmentTokenWithTTL(ctx, orgID, deviceName, osType, agentVersion, actorType, actorID, s.tokenTTL)
}

func (s *Server) createDeviceAndEnrollmentTokenWithTTL(
	ctx context.Context,
	orgID string,
	deviceName string,
	osType string,
	agentVersion string,
	actorType string,
	actorID string,
	ttl time.Duration,
) (*store.Organization, *store.Device, string, time.Time, error) {
	deviceName = strings.TrimSpace(deviceName)
	if !deviceNameRe.MatchString(deviceName) {
		return nil, nil, "", time.Time{}, fmt.Errorf("invalid device name")
	}
	switch osType {
	case "darwin", "windows", "linux":
	default:
		return nil, nil, "", time.Time{}, fmt.Errorf("invalid os type")
	}
	if agentVersion == "" {
		agentVersion = "1.0.0"
	}

	org, err := s.store.GetOrganization(ctx, orgID)
	if err != nil {
		return nil, nil, "", time.Time{}, err
	}
	if org == nil || org.Status != "active" {
		return nil, nil, "", time.Time{}, fmt.Errorf("organization not found or inactive")
	}
	if err := s.ensureOrganizationAllowed(ctx, orgID); err != nil {
		return nil, nil, "", time.Time{}, err
	}

	device, err := s.store.CreateDevice(ctx, orgID, deviceName, osType, agentVersion)
	if err != nil {
		return nil, nil, "", time.Time{}, err
	}

	rawToken, expiresAt, err := s.issueEnrollmentTokenWithTTL(ctx, device.ID, ttl)
	if err != nil {
		return nil, nil, "", time.Time{}, err
	}

	_ = s.store.InsertAudit(ctx, nil, &store.AuditEntry{
		ActorType:    actorType,
		ActorID:      actorID,
		OrgID:        &orgID,
		Action:       "device.registered",
		ResourceType: "device",
		ResourceID:   device.ID,
		Details: map[string]interface{}{
			"device_name":   device.DeviceName,
			"os":            device.OS,
			"token_expires": expiresAt,
		},
	})

	return org, device, rawToken, expiresAt, nil
}

func (s *Server) ensureOrganizationAllowed(ctx context.Context, orgID string) error {
	if s.controlPlane == nil {
		return nil
	}
	return s.controlPlane.EnforceOrganizationActive(ctx, orgID)
}

func (s *Server) ensureDeviceAllowed(ctx context.Context, deviceID string) error {
	if s.controlPlane == nil {
		return nil
	}
	return s.controlPlane.EnforceDeviceActive(ctx, deviceID)
}

func (s *Server) handleControlPlaneError(w http.ResponseWriter, err error) {
	switch {
	case err == nil:
		return
	case errors.Is(err, controlplane.ErrDenied):
		writeError(w, http.StatusForbidden, "ORG_OR_DEVICE_SUSPENDED", "organization or device is disabled by control plane")
	default:
		s.logger.Error("control plane enforcement failed", "error", err)
		writeError(w, http.StatusServiceUnavailable, "CONTROL_PLANE_UNAVAILABLE", "control plane enforcement is unavailable")
	}
}

func (s *Server) issueEnrollmentToken(ctx context.Context, deviceID string) (string, time.Time, error) {
	return s.issueEnrollmentTokenWithTTL(ctx, deviceID, s.tokenTTL)
}

func (s *Server) issueEnrollmentTokenWithTTL(ctx context.Context, deviceID string, ttl time.Duration) (string, time.Time, error) {
	rawToken, tokenHash, err := token.Generate()
	if err != nil {
		return "", time.Time{}, err
	}

	expiresAt := time.Now().Add(ttl)
	if _, err := s.store.CreateEnrollmentToken(ctx, deviceID, tokenHash, expiresAt); err != nil {
		return "", time.Time{}, err
	}
	return rawToken, expiresAt, nil
}

func (s *Server) handleDeviceCreationError(w http.ResponseWriter, deviceName string, err error) {
	switch {
	case err == nil:
		return
	case strings.Contains(err.Error(), "invalid device name"):
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "device_name must be 1-128 alphanumeric chars or hyphens")
	case strings.Contains(err.Error(), "invalid os type"):
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "os must be darwin, windows, or linux")
	case strings.Contains(err.Error(), "organization not found or inactive"):
		writeError(w, http.StatusNotFound, "ORG_NOT_FOUND", "organization not found or inactive")
	case isDuplicateError(err):
		writeError(w, http.StatusConflict, "DEVICE_EXISTS", fmt.Sprintf("device %q already exists in this org", deviceName))
	default:
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
	}
}

func buildMacActivationCommand(backendURL, gatewayURL, deviceID, orgName, token string) string {
	return fmt.Sprintf(
		`sudo /usr/local/bin/themisto-activate -backend-url %q -gateway-url %q -device-id %q -org-name %q -enrollment-token %q && sudo /usr/local/bin/themisto-service install`,
		backendURL, gatewayURL, deviceID, orgName, token,
	)
}

func buildWindowsActivationCommand(backendURL, gatewayURL, deviceID, orgName, token string) string {
	return fmt.Sprintf(
		`powershell -ExecutionPolicy Bypass -File "C:\Program Files\Themisto\themisto-activate.ps1" -BackendUrl %q -GatewayUrl %q -DeviceId %q -OrgName %q -EnrollmentToken %q`,
		backendURL, gatewayURL, deviceID, orgName, token,
	)
}

func buildAgentConfig(backendURL, gatewayURL, deviceID, orgName, token string) enrollmentAgentConfig {
	return enrollmentAgentConfig{
		AgentID:                deviceID,
		GatewayURL:             gatewayURL,
		ListenAddr:             "127.0.0.1:8080",
		DefaultDecision:        "bypass",
		TelemetryFlushInterval: "5s",
		BackendURL:             backendURL,
		DeviceID:               deviceID,
		EnrollmentToken:        token,
		OrgName:                orgName,
	}
}

func (s *Server) urlsForOrg(org *store.Organization) (string, string) {
	backendURL := s.publicBackendURL
	gatewayURL := s.publicGatewayURL
	if org != nil {
		if strings.TrimSpace(org.PublicBackendURL) != "" {
			backendURL = strings.TrimSpace(org.PublicBackendURL)
		}
		if strings.TrimSpace(org.PublicGatewayURL) != "" {
			gatewayURL = strings.TrimSpace(org.PublicGatewayURL)
		}
	}
	return backendURL, gatewayURL
}

func isDuplicateError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "duplicate key") || strings.Contains(msg, "unique constraint")
}
