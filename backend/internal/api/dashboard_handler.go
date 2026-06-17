package api

import "net/http"

func (s *Server) handleDashboardStats(w http.ResponseWriter, r *http.Request) {
	user := getUserFromContext(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}

	stats, err := s.store.GetDashboardStats(r.Context(), user.OrgID)
	if err != nil {
		s.logger.Error("get dashboard stats", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}

	writeJSON(w, http.StatusOK, stats)
}
