package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/themisto/backend/internal/store"
)

func (s *Server) handleOperatorFleet(w http.ResponseWriter, r *http.Request) {
	if s.operatorOrgID == "" {
		writeError(w, http.StatusServiceUnavailable, "OPERATOR_ORG_NOT_CONFIGURED", "operator organization is not configured")
		return
	}
	page := 1
	if raw := r.URL.Query().Get("page"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			page = parsed
		}
	}
	limit := 25
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			limit = parsed
		}
	}
	if limit < 1 || limit > 100 {
		limit = 25
	}
	signalLimit := 100
	if raw := r.URL.Query().Get("signal_limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			signalLimit = parsed
		}
	}
	selectedDeviceID := strings.TrimSpace(r.URL.Query().Get("selected_device_id"))
	filter := store.FleetFilter{
		OrgID:  s.operatorOrgID,
		Page:   page,
		Limit:  limit,
		Status: strings.TrimSpace(r.URL.Query().Get("status")),
		Query:  strings.TrimSpace(r.URL.Query().Get("q")),
	}

	devices, summary, total, err := s.store.ListFleet(r.Context(), filter, time.Now())
	if err != nil {
		s.logger.Error("operator list fleet", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	signals, err := s.store.ListFleetSignals(r.Context(), s.operatorOrgID, signalLimit)
	if err != nil {
		s.logger.Error("operator list fleet signals", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	deviceSignals := []store.FleetSignal{}
	if selectedDeviceID != "" {
		deviceSignals, err = s.store.ListFleetDeviceSignals(r.Context(), s.operatorOrgID, selectedDeviceID, 200)
		if err != nil {
			s.logger.Error("operator list fleet device signals", "error", err)
			writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"generated_at":                time.Now().UTC(),
		"summary":                     summary,
		"devices":                     devices,
		"signals":                     signals,
		"selected_device_id":          selectedDeviceID,
		"selected_device_signals":     deviceSignals,
		"prompt_enforcement_override": s.policyEnforcementOverride(r.Context(), s.operatorOrgID),
		"page":                        page,
		"limit":                       limit,
		"total":                       total,
		"thresholds": map[string]int{
			"connected_seconds": 90,
			"offline_seconds":   300,
		},
	})
}

func (s *Server) handleOperatorFleetEmergencyMode(w http.ResponseWriter, r *http.Request) {
	if s.operatorOrgID == "" {
		writeError(w, http.StatusServiceUnavailable, "OPERATOR_ORG_NOT_CONFIGURED", "operator organization is not configured")
		return
	}

	var req policyEnforcementRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON body")
		return
	}
	mode := normalizePolicyEnforcementOverride(req.PromptEnforcementOverride)
	if mode == "" && strings.TrimSpace(req.PromptEnforcementOverride) == "" {
		mode = normalizePolicyEnforcementOverride(req.Mode)
	}
	if !validPolicyEnforcementOverride(mode) {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "prompt_enforcement_override must be empty, monitor, alert, or enforce")
		return
	}
	if err := s.store.UpdatePromptEnforcementOverride(r.Context(), s.operatorOrgID, mode); err != nil {
		s.logger.Error("operator update fleet emergency mode", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"prompt_enforcement_override": mode,
	})
}
