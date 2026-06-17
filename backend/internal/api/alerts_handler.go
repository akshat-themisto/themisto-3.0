package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/themisto/backend/internal/store"
)

type markAlertsReadRequest struct {
	EventIDs []int64 `json:"event_ids"`
}

func (s *Server) handleListAlerts(w http.ResponseWriter, r *http.Request) {
	user := getUserFromContext(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))

	items, total, unread, err := s.store.ListAlerts(r.Context(), store.AlertFilter{
		OrgID:    user.OrgID,
		UserID:   user.ID,
		Severity: r.URL.Query().Get("severity"),
		Action:   r.URL.Query().Get("action"),
		Page:     page,
		Limit:    limit,
	})
	if err != nil {
		s.logger.Error("list alerts", "org_id", user.OrgID, "user_id", user.ID, "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	if items == nil {
		items = []store.AlertFeedItem{}
	}

	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 25
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"alerts":       items,
		"total":        total,
		"unread":       unread,
		"page":         page,
		"limit":        limit,
		"poll_seconds": 15,
	})
}

func (s *Server) handleMarkAlertsRead(w http.ResponseWriter, r *http.Request) {
	user := getUserFromContext(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}

	var req markAlertsReadRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON body")
		return
	}
	if len(req.EventIDs) == 0 {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "event_ids are required")
		return
	}

	if err := s.store.MarkAlertsRead(r.Context(), user.OrgID, user.ID, req.EventIDs); err != nil {
		s.logger.Error("mark alerts read", "org_id", user.OrgID, "user_id", user.ID, "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}

	orgID := user.OrgID
	_ = s.store.InsertAudit(r.Context(), nil, &store.AuditEntry{
		ActorType:    "admin_user",
		ActorID:      user.ID,
		OrgID:        &orgID,
		Action:       "alerts.mark_read",
		ResourceType: "dlp_alert",
		ResourceID:   user.ID,
		Details: map[string]interface{}{
			"event_ids": req.EventIDs,
			"count":     len(req.EventIDs),
		},
		IPAddress: clientIPFromRequest(r),
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{"status": "ok"})
}
