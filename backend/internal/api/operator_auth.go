package api

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/themisto/backend/internal/store"
)

const (
	operatorSessionCookie = "themisto_operator_session"
	operatorSessionTTL    = 30 * time.Minute
	operatorConsoleHeader = "X-Themisto-Operator"
)

const operatorContextKey contextKey = "operator"

type operatorLoginRequest struct {
	AccessKey string `json:"access_key"`
}

func (s *Server) handleOperatorLogin(w http.ResponseWriter, r *http.Request) {
	var req operatorLoginRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON body")
		return
	}
	key := strings.TrimSpace(req.AccessKey)
	if key == "" || subtle.ConstantTimeCompare([]byte(key), []byte(s.apiKey)) != 1 {
		writeError(w, http.StatusUnauthorized, "INVALID_OPERATOR_CREDENTIALS", "invalid operator credentials")
		return
	}

	token, expiresAt, actorID, err := s.issueOperatorSession()
	if err != nil {
		s.logger.Error("issue operator session", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	setOperatorSessionCookie(w, r, token, expiresAt)
	s.auditOperatorAuth(r, actorID, "operator.auth.login")
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"authenticated": true,
		"expires_at":    expiresAt,
	})
}

func (s *Server) handleOperatorMe(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"authenticated": true,
		"actor_id":      getOperatorActorID(r),
		"org_id":        s.operatorOrgID,
	})
}

func (s *Server) handleOperatorLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(operatorSessionCookie); err == nil {
		if _, actorID, valid := s.verifyOperatorSession(cookie.Value); valid {
			s.auditOperatorAuth(r, actorID, "operator.auth.logout")
		}
	}
	clearOperatorSessionCookie(w, r)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]string{"status": "logged_out"})
}

func (s *Server) withOperatorAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if cookie, err := r.Cookie(operatorSessionCookie); err == nil {
			if _, actorID, valid := s.verifyOperatorSession(cookie.Value); valid {
				if isUnsafeMethod(r.Method) && r.Header.Get(operatorConsoleHeader) != "console" {
					writeError(w, http.StatusForbidden, "OPERATOR_REQUEST_REJECTED", "operator request header required")
					return
				}
				ctx := context.WithValue(r.Context(), operatorContextKey, actorID)
				next(w, r.WithContext(ctx))
				return
			}
		}

		// Preserve headless provisioning access without allowing browser-origin
		// requests to reintroduce the administrative key into JavaScript storage.
		if strings.TrimSpace(r.Header.Get("Origin")) == "" {
			auth := r.Header.Get("Authorization")
			if strings.HasPrefix(auth, "Bearer ") {
				token := strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
				if subtle.ConstantTimeCompare([]byte(token), []byte(s.apiKey)) == 1 {
					ctx := context.WithValue(r.Context(), operatorContextKey, "operator-api")
					next(w, r.WithContext(ctx))
					return
				}
			}
		}

		writeError(w, http.StatusUnauthorized, "OPERATOR_AUTH_REQUIRED", "operator authentication required")
	}
}

func (s *Server) issueOperatorSession() (string, time.Time, string, error) {
	nonceBytes := make([]byte, 24)
	if _, err := rand.Read(nonceBytes); err != nil {
		return "", time.Time{}, "", err
	}
	nonce := base64.RawURLEncoding.EncodeToString(nonceBytes)
	expiresAt := time.Now().UTC().Add(operatorSessionTTL)
	payload := fmt.Sprintf("v1.%d.%s", expiresAt.Unix(), nonce)
	signature := s.signOperatorPayload(payload)
	actorHash := sha256.Sum256(nonceBytes)
	actorID := "operator-" + base64.RawURLEncoding.EncodeToString(actorHash[:6])
	return payload + "." + signature, expiresAt, actorID, nil
}

func (s *Server) verifyOperatorSession(token string) (time.Time, string, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 4 || parts[0] != "v1" {
		return time.Time{}, "", false
	}
	payload := strings.Join(parts[:3], ".")
	expected := s.signOperatorPayload(payload)
	if subtle.ConstantTimeCompare([]byte(parts[3]), []byte(expected)) != 1 {
		return time.Time{}, "", false
	}
	expiresUnix, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return time.Time{}, "", false
	}
	expiresAt := time.Unix(expiresUnix, 0).UTC()
	now := time.Now().UTC()
	if !expiresAt.After(now) || expiresAt.After(now.Add(operatorSessionTTL+time.Minute)) {
		return time.Time{}, "", false
	}
	nonceBytes, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(nonceBytes) != 24 {
		return time.Time{}, "", false
	}
	actorHash := sha256.Sum256(nonceBytes)
	actorID := "operator-" + base64.RawURLEncoding.EncodeToString(actorHash[:6])
	return expiresAt, actorID, true
}

func (s *Server) signOperatorPayload(payload string) string {
	mac := hmac.New(sha256.New, []byte(s.apiKey))
	_, _ = mac.Write([]byte("themisto-operator-session\x00" + payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func getOperatorActorID(r *http.Request) string {
	actorID, _ := r.Context().Value(operatorContextKey).(string)
	if actorID == "" {
		return "operator"
	}
	return actorID
}

func setOperatorSessionCookie(w http.ResponseWriter, r *http.Request, token string, expiresAt time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     operatorSessionCookie,
		Value:    token,
		Path:     "/api/v1/operator",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   isHTTPSRequest(r),
		MaxAge:   int(time.Until(expiresAt).Seconds()),
		Expires:  expiresAt,
	})
}

func clearOperatorSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     operatorSessionCookie,
		Value:    "",
		Path:     "/api/v1/operator",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   isHTTPSRequest(r),
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})
}

func isUnsafeMethod(method string) bool {
	return method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions
}

func (s *Server) auditOperatorAuth(r *http.Request, actorID, action string) {
	if s.store == nil {
		return
	}
	ip := extractClientIP(r)
	if err := s.store.InsertAudit(r.Context(), nil, &store.AuditEntry{
		ActorType:    "operator",
		ActorID:      actorID,
		Action:       action,
		ResourceType: "operator_session",
		ResourceID:   actorID,
		Details:      map[string]interface{}{"user_agent": r.UserAgent()},
		IPAddress:    &ip,
	}); err != nil {
		s.logger.Warn("operator auth audit failed", "action", action, "error", err)
	}
}
