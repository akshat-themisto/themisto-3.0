package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/themisto/backend/internal/store"
)

func (s *Server) handleListTelemetry(w http.ResponseWriter, r *http.Request) {
	user := getUserFromContext(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))

	filter := store.TelemetryFilter{
		OrgID:          user.OrgID,
		DeviceID:       r.URL.Query().Get("device_id"),
		RequestHost:    r.URL.Query().Get("host"),
		PolicyDecision: r.URL.Query().Get("decision"),
		Page:           page,
		Limit:          limit,
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

	events, total, err := s.store.ListTelemetryEvents(r.Context(), filter)
	if err != nil {
		s.logger.Error("list telemetry events", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}

	if events == nil {
		events = []store.TelemetryEvent{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"events": events,
		"total":  total,
		"page":   filter.Page,
		"limit":  filter.Limit,
	})
}

func (s *Server) handleTelemetryTimeSeries(w http.ResponseWriter, r *http.Request) {
	user := getUserFromContext(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}

	interval := r.URL.Query().Get("interval")
	if interval != "hour" && interval != "day" {
		interval = "hour"
	}

	to := time.Now()
	from := to.Add(-7 * 24 * time.Hour)

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

	points, err := s.store.GetTelemetryTimeSeries(r.Context(), user.OrgID, interval, from, to)
	if err != nil {
		s.logger.Error("get telemetry timeseries", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}

	if points == nil {
		points = []store.TimeSeriesPoint{}
	}

	// Also get top hosts
	topLimit, _ := strconv.Atoi(r.URL.Query().Get("top_limit"))
	if topLimit < 1 {
		topLimit = 10
	}

	topHosts, err := s.store.GetTopHosts(r.Context(), user.OrgID, topLimit, from, to)
	if err != nil {
		s.logger.Error("get top hosts", "error", err)
		topHosts = []store.TopHost{}
	}

	if topHosts == nil {
		topHosts = []store.TopHost{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"timeseries": points,
		"top_hosts":  topHosts,
		"interval":   interval,
		"from":       from,
		"to":         to,
	})
}
