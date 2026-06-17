package handler

import (
	"io"
	"log/slog"
	"net/http"
)

// TelemetryHandler accepts telemetry POSTs from agents.
// For now it logs and returns 202 Accepted — the gateway already records
// its own telemetry per-request via the proxy handler.
type TelemetryHandler struct {
	logger *slog.Logger
}

func NewTelemetryHandler(logger *slog.Logger) *TelemetryHandler {
	return &TelemetryHandler{logger: logger}
}

func (h *TelemetryHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
		return
	}
	h.logger.Info("telemetry received from agent", "bytes", len(body))
	w.WriteHeader(http.StatusAccepted)
}
