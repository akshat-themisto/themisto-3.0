package api

import (
	"encoding/json"
	"net/http"
)

type bulkEnrollRequest struct {
	Devices       []bulkDeviceEntry `json:"devices"`
	TokenTTLHours int               `json:"token_ttl_hours,omitempty"`
}

type bulkDeviceEntry struct {
	DeviceName   string `json:"device_name"`
	OS           string `json:"os"`
	AgentVersion string `json:"agent_version,omitempty"`
}

func (s *Server) handleBulkEnrollmentPackage(w http.ResponseWriter, r *http.Request) {
	user := getUserFromContext(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}

	var req bulkEnrollRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON body")
		return
	}

	if len(req.Devices) == 0 {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "devices list is empty")
		return
	}
	if len(req.Devices) > 50 {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "maximum 50 devices per bulk request")
		return
	}

	ttl := s.resolveTokenTTL(req.TokenTTLHours)

	packages := make([]enrollmentPackageResponse, 0, len(req.Devices))
	for _, d := range req.Devices {
		org, device, rawToken, expiresAt, err := s.createDeviceAndEnrollmentTokenWithTTL(
			r.Context(),
			user.OrgID,
			d.DeviceName,
			d.OS,
			d.AgentVersion,
			"admin",
			user.ID,
			ttl,
		)
		if err != nil {
			s.logger.Warn("bulk enroll: failed to create device", "device_name", d.DeviceName, "error", err)
			continue
		}

		backendURL, gatewayURL := s.urlsForOrg(org)
		agentConfig := buildAgentConfig(backendURL, gatewayURL, device.ID, org.Name, rawToken)

		enrollmentURL, err := s.storeEnrollmentShortCode(r, device.ID, org.ID, agentConfig, expiresAt)
		if err != nil {
			s.logger.Warn("bulk enroll: failed to create short code", "error", err)
		}

		packages = append(packages, enrollmentPackageResponse{
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

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"packages": packages,
		"total":    len(packages),
	})
}
