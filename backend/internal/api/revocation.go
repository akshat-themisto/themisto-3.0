package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/themisto/backend/internal/store"
)

type revokeRequest struct {
	Reason string `json:"reason"`
	Serial string `json:"serial"`
}

func (s *Server) handleRevoke(w http.ResponseWriter, r *http.Request) {
	deviceID := r.PathValue("deviceID")

	var req revokeRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON body")
		return
	}

	validReasons := map[string]bool{
		"device_decommissioned": true,
		"key_compromise":        true,
		"org_offboarded":        true,
		"policy_violation":      true,
		"superseded":            true,
		"admin_action":          true,
	}
	if !validReasons[req.Reason] {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid revocation reason")
		return
	}

	device, err := s.store.GetDevice(r.Context(), deviceID)
	if err != nil {
		s.logger.Error("get device", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	if device == nil {
		writeError(w, http.StatusNotFound, "DEVICE_NOT_FOUND", "device not found")
		return
	}

	serial := req.Serial
	if serial == "" {
		cert, err := s.store.GetActiveCertForDevice(r.Context(), deviceID)
		if err != nil {
			s.logger.Error("get active cert", "error", err)
			writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
			return
		}
		if cert == nil {
			writeError(w, http.StatusNotFound, "CERT_NOT_FOUND", "no active certificate for this device")
			return
		}
		serial = cert.Serial
	}

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
	if cs.DeviceID != deviceID {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "certificate does not belong to this device")
		return
	}
	if cs.Status != "active" {
		writeError(w, http.StatusConflict, "CERT_NOT_ACTIVE", "certificate is not active")
		return
	}

	tx, err := s.store.DB.BeginTx(r.Context(), nil)
	if err != nil {
		s.logger.Error("begin tx", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	defer tx.Rollback()

	if err := s.store.RevokeCertificate(r.Context(), tx, serial, req.Reason); err != nil {
		s.logger.Error("revoke cert", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}

	orgID := device.OrgID
	s.store.InsertAudit(r.Context(), tx, &store.AuditEntry{
		ActorType:    "admin",
		ActorID:      "api-key",
		OrgID:        &orgID,
		Action:       "cert.revoked",
		ResourceType: "certificate",
		ResourceID:   serial,
		Details: map[string]interface{}{
			"device_id": deviceID,
			"serial":    serial,
			"reason":    req.Reason,
		},
	})

	if err := tx.Commit(); err != nil {
		s.logger.Error("commit tx", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}

	now := time.Now()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"device_id":  deviceID,
		"serial":     serial,
		"status":     "revoked",
		"revoked_at": now,
	})
}
