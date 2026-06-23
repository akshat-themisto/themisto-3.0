package api

import (
	"net/http"
	"strconv"
	"time"
)

func (s *Server) handleOperatorFleet(w http.ResponseWriter, r *http.Request) {
	if s.operatorOrgID == "" {
		writeError(w, http.StatusServiceUnavailable, "OPERATOR_ORG_NOT_CONFIGURED", "operator organization is not configured")
		return
	}
	limit := 100
	if raw := r.URL.Query().Get("signal_limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			limit = parsed
		}
	}

	devices, summary, err := s.store.ListFleet(r.Context(), s.operatorOrgID, time.Now())
	if err != nil {
		s.logger.Error("operator list fleet", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	signals, err := s.store.ListFleetSignals(r.Context(), s.operatorOrgID, limit)
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
		"thresholds": map[string]int{
			"connected_seconds": 90,
			"offline_seconds":   300,
		},
	})
}
