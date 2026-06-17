package api

import "net/http"

func (s *Server) handleCertStatus(w http.ResponseWriter, r *http.Request) {
	serial := r.PathValue("serial")

	cs, err := s.store.GetCertStatus(r.Context(), serial)
	if err != nil {
		s.logger.Error("get cert status", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	if cs == nil {
		writeError(w, http.StatusNotFound, "CERT_NOT_FOUND", "certificate not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"serial":    cs.Serial,
		"status":    cs.Status,
		"device_id": cs.DeviceID,
		"org_id":    cs.OrgID,
	})
}
