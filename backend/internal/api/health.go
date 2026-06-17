package api

import "net/http"

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if !s.store.Healthy() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
			"status":       "error",
			"db_connected": false,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":       "ok",
		"db_connected": true,
	})
}
