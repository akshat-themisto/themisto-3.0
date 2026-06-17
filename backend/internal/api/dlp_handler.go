package api

import (
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/themisto/backend/internal/store"
)

func (s *Server) handleListDLPEvents(w http.ResponseWriter, r *http.Request) {
	user := getUserFromContext(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit < 1 || limit > 100 {
		limit = 25
	}

	filter := store.DLPFilter{
		OrgID:       user.OrgID,
		DeviceID:    r.URL.Query().Get("device_id"),
		RequestHost: r.URL.Query().Get("host"),
		AIVendor:    r.URL.Query().Get("ai_vendor"),
		MatchType:   r.URL.Query().Get("match_type"),
		Protocol:    r.URL.Query().Get("protocol"),
		Page:        page,
		Limit:       limit,
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

	events, total, err := s.store.ListDLPEvents(r.Context(), filter)
	if err != nil {
		s.logger.Error("list DLP events", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}

	if events == nil {
		events = []store.DLPEvent{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"events": events,
		"total":  total,
		"page":   page,
		"limit":  limit,
	})
}

func (s *Server) handleGetDLPEventBody(w http.ResponseWriter, r *http.Request) {
	user := getUserFromContext(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}

	idStr := r.PathValue("id")
	eventID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || eventID <= 0 {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid DLP event id")
		return
	}

	body, err := s.store.GetDLPEventBody(r.Context(), user.OrgID, eventID)
	if err != nil {
		s.logger.Error("get DLP event body", "event_id", eventID, "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	if body == nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "DLP event not found")
		return
	}

	orgID := user.OrgID
	_ = s.store.InsertAudit(r.Context(), nil, &store.AuditEntry{
		ActorType:    "admin_user",
		ActorID:      user.ID,
		OrgID:        &orgID,
		Action:       "dlp.body.viewed",
		ResourceType: "dlp_event",
		ResourceID:   strconv.FormatInt(eventID, 10),
		Details: map[string]interface{}{
			"viewer_email":      user.Email,
			"request_host":      body.RequestHost,
			"request_method":    body.RequestMethod,
			"request_path":      body.RequestPath,
			"request_truncated": body.Truncated,
		},
		IPAddress: clientIPFromRequest(r),
	})

	writeJSON(w, http.StatusOK, body)
}

func (s *Server) handleGetDLPEvent(w http.ResponseWriter, r *http.Request) {
	user := getUserFromContext(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}

	idStr := r.PathValue("id")
	eventID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || eventID <= 0 {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid DLP event id")
		return
	}

	event, err := s.store.GetDLPEvent(r.Context(), user.OrgID, eventID)
	if err != nil {
		s.logger.Error("get DLP event", "event_id", eventID, "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	if event == nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "DLP event not found")
		return
	}

	writeJSON(w, http.StatusOK, event)
}

func (s *Server) handleDLPSummary(w http.ResponseWriter, r *http.Request) {
	user := getUserFromContext(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}

	to := time.Now()
	from := to.Add(-30 * 24 * time.Hour)

	if f := r.URL.Query().Get("from"); f != "" {
		if t, err := time.Parse(time.RFC3339, f); err == nil {
			from = t
		}
	}
	if t := r.URL.Query().Get("to"); t != "" {
		if parsed, err := time.Parse(time.RFC3339, t); err == nil {
			to = parsed
		}
	}

	summary, err := s.store.GetDLPSummary(r.Context(), user.OrgID, from, to)
	if err != nil {
		s.logger.Error("get DLP summary", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}

	writeJSON(w, http.StatusOK, summary)
}

func (s *Server) handleAIUsage(w http.ResponseWriter, r *http.Request) {
	user := getUserFromContext(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}

	to := time.Now()
	from := to.Add(-30 * 24 * time.Hour)

	if f := r.URL.Query().Get("from"); f != "" {
		if t, err := time.Parse(time.RFC3339, f); err == nil {
			from = t
		}
	}
	if t := r.URL.Query().Get("to"); t != "" {
		if parsed, err := time.Parse(time.RFC3339, t); err == nil {
			to = parsed
		}
	}

	summary, err := s.store.GetAIUsageSummaryV2(r.Context(), user.OrgID, from, to)
	if err != nil {
		s.logger.Error("get AI usage summary", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}

	topDevices, err := s.store.GetTopAIDevices(r.Context(), user.OrgID, 10, from, to)
	if err != nil {
		s.logger.Error("get top AI devices", "error", err)
		topDevices = []store.AIDeviceStat{}
	}
	if topDevices == nil {
		topDevices = []store.AIDeviceStat{}
	}

	topViolating, err := s.store.GetTopAIViolatingDevices(r.Context(), user.OrgID, 10, from, to)
	if err != nil {
		s.logger.Error("get top violating devices", "error", err)
		topViolating = []store.AIViolatingDeviceStat{}
	}
	if topViolating == nil {
		topViolating = []store.AIViolatingDeviceStat{}
	}

	governance, err := s.store.ListAIVendorGovernance(r.Context(), user.OrgID)
	if err != nil {
		s.logger.Error("list ai governance", "error", err)
		governance = []store.AIVendorGovernance{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"summary":               summary,
		"top_devices":           topDevices,
		"top_violating_devices": topViolating,
		"governance":            governance,
		"from":                  from,
		"to":                    to,
	})
}

func clientIPFromRequest(r *http.Request) *string {
	xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For"))
	if xff != "" {
		parts := strings.Split(xff, ",")
		if len(parts) > 0 {
			ip := strings.TrimSpace(parts[0])
			if ip != "" {
				return &ip
			}
		}
	}

	remote := strings.TrimSpace(r.RemoteAddr)
	if remote == "" {
		return nil
	}
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		return &remote
	}
	if host == "" {
		return nil
	}
	return &host
}
