package api

import (
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

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"generated_at": time.Now().UTC(),
		"summary":      summary,
		"devices":      devices,
		"signals":      signals,
		"page":         page,
		"limit":        limit,
		"total":        total,
		"thresholds": map[string]int{
			"connected_seconds": 90,
			"offline_seconds":   300,
		},
	})
}
