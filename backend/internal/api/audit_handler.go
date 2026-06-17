package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/themisto/backend/internal/store"
)

func (s *Server) handleListAudit(w http.ResponseWriter, r *http.Request) {
	user := getUserFromContext(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))

	filter := store.AuditLogFilter{
		OrgID:     user.OrgID,
		Action:    r.URL.Query().Get("action"),
		ActorType: r.URL.Query().Get("actor_type"),
		Page:      page,
		Limit:     limit,
	}

	if from := r.URL.Query().Get("from"); from != "" {
		if t, err := time.Parse(time.RFC3339, from); err == nil {
			filter.DateFrom = &t
		}
	}
	if to := r.URL.Query().Get("to"); to != "" {
		if t, err := time.Parse(time.RFC3339, to); err == nil {
			filter.DateTo = &t
		}
	}

	entries, total, err := s.store.ListAuditLog(r.Context(), filter)
	if err != nil {
		s.logger.Error("list audit log", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}

	if entries == nil {
		entries = []store.AuditLogEntry{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"entries": entries,
		"total":   total,
		"page":    filter.Page,
		"limit":   filter.Limit,
	})
}
