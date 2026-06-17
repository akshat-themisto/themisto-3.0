package api

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"time"
)

const shortCodeAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789"

func generateShortCode(length int) (string, error) {
	b := make([]byte, length)
	for i := range b {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(shortCodeAlphabet))))
		if err != nil {
			return "", err
		}
		b[i] = shortCodeAlphabet[n.Int64()]
	}
	return string(b), nil
}

// storeEnrollmentShortCode generates a short code, persists it, and returns the full enrollment URL.
func (s *Server) storeEnrollmentShortCode(r *http.Request, deviceID, orgID string, agentConfig enrollmentAgentConfig, expiresAt time.Time) (string, error) {
	code, err := generateShortCode(8)
	if err != nil {
		return "", err
	}

	configJSON, err := json.Marshal(agentConfig)
	if err != nil {
		return "", err
	}

	if err := s.store.CreateEnrollmentShortCode(r.Context(), code, deviceID, orgID, configJSON, expiresAt); err != nil {
		return "", err
	}

	return fmt.Sprintf("%s/enroll/%s", s.publicBackendURL, code), nil
}

func (s *Server) handleEnrollLink(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	if code == "" {
		s.renderEnrollExpired(w)
		return
	}

	sc, err := s.store.GetEnrollmentShortCode(r.Context(), code)
	if err != nil {
		s.logger.Error("get enrollment short code", "error", err)
		s.renderEnrollExpired(w)
		return
	}
	if sc == nil {
		s.renderEnrollExpired(w)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="agent.json"`)
	w.WriteHeader(http.StatusOK)
	w.Write(sc.ConfigJSON)
}

func (s *Server) renderEnrollExpired(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusGone)
	fmt.Fprint(w, `<!DOCTYPE html>
<html><head><title>Enrollment Link Expired</title>
<style>body{font-family:system-ui,sans-serif;display:flex;align-items:center;justify-content:center;height:100vh;margin:0;background:#0a0a0a;color:#e5e5e5}
.card{text-align:center;padding:48px;border:1px solid #333;border-radius:12px;max-width:420px}
h1{font-size:20px;margin-bottom:12px}p{color:#999;font-size:14px}</style></head>
<body><div class="card"><h1>Enrollment Link Expired</h1>
<p>This enrollment link is no longer valid. Please ask your administrator for a new one.</p></div></body></html>`)
}
