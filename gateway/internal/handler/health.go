package handler

import (
	"encoding/json"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/themisto/gateway/internal/store"
)

type HealthHandler struct {
	db        *store.Store
	startTime time.Time
	connCount *atomic.Int64
}

func NewHealthHandler(db *store.Store, connCount *atomic.Int64) *HealthHandler {
	return &HealthHandler{
		db:        db,
		startTime: time.Now(),
		connCount: connCount,
	}
}

func (h *HealthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	dbOK := h.db.Healthy()
	status := http.StatusOK
	state := "ok"
	if !dbOK {
		status = http.StatusServiceUnavailable
		state = "error"
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":             state,
		"uptime_seconds":     int(time.Since(h.startTime).Seconds()),
		"db_connected":       dbOK,
		"active_connections": h.connCount.Load(),
	})
}
