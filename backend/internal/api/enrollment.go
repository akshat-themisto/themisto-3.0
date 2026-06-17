package api

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/x509"
	"io"
	"net/http"
	"strings"

	"github.com/themisto/backend/internal/metrics"
	"github.com/themisto/backend/internal/signing"
	"github.com/themisto/backend/internal/store"
	"github.com/themisto/backend/internal/token"
)

func (s *Server) handleSubmitCSR(w http.ResponseWriter, r *http.Request) {
	deviceID := strings.TrimSpace(r.PathValue("deviceID"))

	enrollToken := strings.TrimSpace(r.Header.Get("X-Enrollment-Token"))
	renewalToken := strings.TrimSpace(r.Header.Get("X-Renewal-Token"))

	if enrollToken == "" && renewalToken == "" {
		writeError(w, http.StatusUnauthorized, "TOKEN_INVALID", "X-Enrollment-Token or X-Renewal-Token header is required")
		return
	}

	isRenewal := renewalToken != ""

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

	org, err := s.store.GetOrganization(r.Context(), device.OrgID)
	if err != nil {
		s.logger.Error("get org", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	if org == nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL", "org not found")
		return
	}
	if err := s.ensureOrganizationAllowed(r.Context(), org.ID); err != nil {
		s.handleControlPlaneError(w, err)
		return
	}
	if err := s.ensureDeviceAllowed(r.Context(), device.ID); err != nil {
		s.handleControlPlaneError(w, err)
		return
	}

	csrPEM, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 16*1024))
	if err != nil {
		writeError(w, http.StatusBadRequest, "CSR_INVALID", "could not read CSR body")
		return
	}

	csrDER, err := signing.ValidateCSR(csrPEM, deviceID, org.Name)
	if err != nil {
		if csrErr, ok := err.(*signing.CSRValidationError); ok {
			writeError(w, http.StatusBadRequest, csrErr.Code, csrErr.Message)
			return
		}
		s.logger.Error("validate CSR", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}

	tx, err := s.store.DB.BeginTx(r.Context(), nil)
	if err != nil {
		s.logger.Error("begin tx", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	defer tx.Rollback()

	// Validate the appropriate token.
	if isRenewal {
		// Validate renewal token against device record.
		storedHash, err := s.store.GetRenewalTokenHash(r.Context(), deviceID)
		if err != nil {
			s.logger.Error("get renewal token hash", "error", err)
			writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
			return
		}
		if storedHash == "" {
			writeError(w, http.StatusUnauthorized, "TOKEN_INVALID", "no renewal token set for this device")
			return
		}
		if token.Hash(renewalToken) != storedHash {
			writeError(w, http.StatusUnauthorized, "TOKEN_INVALID", "invalid renewal token")
			return
		}
		// Supersede existing active certs.
		if err := s.store.SupersedeActiveCerts(r.Context(), tx, deviceID); err != nil {
			s.logger.Error("supersede certs", "error", err)
			writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
			return
		}
	} else {
		// Validate and consume enrollment token.
		tokenHash := token.Hash(enrollToken)
		et, err := s.store.ValidateAndConsumeToken(r.Context(), tx, tokenHash)
		if err != nil {
			s.logger.Error("validate token", "error", err)
			writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
			return
		}
		if et == nil {
			writeError(w, http.StatusUnauthorized, "TOKEN_INVALID", "enrollment token is invalid, used, or expired")
			return
		}
		if et.DeviceID != deviceID {
			writeError(w, http.StatusForbidden, "TOKEN_INVALID", "token does not match device")
			return
		}
		// A fresh enrollment token should be sufficient to recover a device
		// whose local certs were lost. Supersede any active certificate so
		// re-enrollment succeeds instead of forcing manual DB cleanup.
		if err := s.store.SupersedeActiveCerts(r.Context(), tx, deviceID); err != nil {
			s.logger.Error("supersede certs", "error", err)
			writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
			return
		}
	}

	result, err := s.signer.SignCSR(csrDER)
	if err != nil {
		s.logger.Error("sign CSR", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "signing failed")
		return
	}

	keyAlg := "ECDSA-P256"
	if csr, err := x509.ParseCertificateRequest(csrDER); err == nil {
		if ecKey, ok := csr.PublicKey.(*ecdsa.PublicKey); ok && ecKey.Curve == elliptic.P384() {
			keyAlg = "ECDSA-P384"
		}
	}

	cert := &store.Certificate{
		DeviceID:     deviceID,
		OrgID:        device.OrgID,
		Serial:       result.Serial,
		SubjectCN:    deviceID,
		SubjectO:     org.Name,
		IssuerCN:     s.signer.CACert.Subject.CommonName,
		KeyAlgorithm: keyAlg,
		NotBefore:    result.NotBefore,
		NotAfter:     result.NotAfter,
		Status:       "active",
		CSRPEM:       string(csrPEM),
		CertPEM:      result.CertPEM,
	}

	if err := s.store.CreateCertificate(r.Context(), tx, cert); err != nil {
		s.logger.Error("create certificate", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}

	if err := s.store.ActivateDevice(r.Context(), tx, deviceID); err != nil {
		s.logger.Error("activate device", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}

	// Generate and store a renewal token for future auto-renewal.
	rawRenewal, renewalHash, err := token.Generate()
	if err != nil {
		s.logger.Error("generate renewal token", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	if err := s.store.SetRenewalTokenHash(r.Context(), tx, deviceID, renewalHash); err != nil {
		s.logger.Error("set renewal token hash", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}

	orgID := device.OrgID
	action := "cert.issued"
	if isRenewal {
		action = "cert.renewed"
	}
	s.store.InsertAudit(r.Context(), tx, &store.AuditEntry{
		ActorType:    "system",
		ActorID:      "backend",
		OrgID:        &orgID,
		Action:       action,
		ResourceType: "certificate",
		ResourceID:   result.Serial,
		Details: map[string]interface{}{
			"device_id": deviceID,
			"serial":    result.Serial,
			"not_after": result.NotAfter,
			"renewal":   isRenewal,
		},
	})

	if err := tx.Commit(); err != nil {
		s.logger.Error("commit tx", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}

	if isRenewal {
		metrics.CertRenewalsTotal.WithLabelValues(device.OrgID).Inc()
	} else {
		metrics.EnrollmentCompletionsTotal.WithLabelValues(device.OrgID).Inc()
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"certificate":   result.CertPEM,
		"ca_chain":      result.ChainPEM,
		"not_before":    result.NotBefore,
		"not_after":     result.NotAfter,
		"serial":        result.Serial,
		"renewal_token": rawRenewal,
	})
}
