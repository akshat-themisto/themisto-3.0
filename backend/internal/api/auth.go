package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/themisto/backend/internal/store"
)

type contextKey string

const userContextKey contextKey = "user"

func getUserFromContext(r *http.Request) *store.AdminUser {
	u, ok := r.Context().Value(userContextKey).(*store.AdminUser)
	if !ok {
		return nil
	}
	return u
}

func dashboardUserPayload(user *store.AdminUser, orgName string) map[string]interface{} {
	payload := map[string]interface{}{
		"id":                   user.ID,
		"email":                user.Email,
		"name":                 user.Name,
		"role":                 user.Role,
		"org_id":               user.OrgID,
		"must_change_password": user.MustChangePassword,
		"terms_accepted_at":    user.TermsAcceptedAt,
	}
	if orgName != "" {
		payload["org_name"] = orgName
	}
	return payload
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON body")
		return
	}

	if req.Email == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "email and password are required")
		return
	}

	user, err := s.store.GetUserByEmail(r.Context(), req.Email)
	if err != nil {
		s.logger.Error("get user by email", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	if user == nil || !s.store.CheckPassword(user, req.Password) {
		writeError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "invalid email or password")
		return
	}

	rawToken, tokenHash, err := store.GenerateSessionToken()
	if err != nil {
		s.logger.Error("generate session token", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}

	expiresAt := time.Now().Add(s.tokenTTL)
	if _, err := s.store.CreateSession(r.Context(), user.ID, tokenHash, expiresAt); err != nil {
		s.logger.Error("create session", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}

	setSessionCookie(w, r, rawToken, expiresAt)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"token":      rawToken,
		"expires_at": expiresAt,
		"user":       dashboardUserPayload(user, ""),
	})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	token := extractSessionToken(r)
	if token != "" {
		tokenHash := store.HashSessionToken(token)
		if err := s.store.DeleteSession(r.Context(), tokenHash); err != nil {
			s.logger.Error("delete session", "error", err)
		}
	}

	clearSessionCookie(w, r)

	writeJSON(w, http.StatusOK, map[string]string{"status": "logged_out"})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	user := getUserFromContext(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}

	org, err := s.store.GetOrganizationByID(r.Context(), user.OrgID)
	if err != nil {
		s.logger.Error("get org", "error", err)
	}

	orgName := ""
	if org != nil {
		orgName = org.Name
	}

	writeJSON(w, http.StatusOK, dashboardUserPayload(user, orgName))
}

func (s *Server) handleAcceptTerms(w http.ResponseWriter, r *http.Request) {
	user := getUserFromContext(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}

	if err := s.store.AcceptUserTerms(r.Context(), user.ID); err != nil {
		s.logger.Error("accept user terms", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}

	updated, err := s.store.GetUserByID(r.Context(), user.ID)
	if err != nil || updated == nil {
		s.logger.Error("reload user after accepting terms", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}

	org, err := s.store.GetOrganizationByID(r.Context(), updated.OrgID)
	if err != nil {
		s.logger.Error("get org", "error", err)
	}

	orgName := ""
	if org != nil {
		orgName = org.Name
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"user": dashboardUserPayload(updated, orgName),
	})
}

func (s *Server) withSession(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := extractSessionToken(r)
		if token == "" {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "session token required")
			return
		}

		tokenHash := store.HashSessionToken(token)
		session, err := s.store.GetSessionByToken(r.Context(), tokenHash)
		if err != nil {
			s.logger.Error("get session", "error", err)
			writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
			return
		}
		if session == nil {
			writeError(w, http.StatusUnauthorized, "SESSION_EXPIRED", "session expired or invalid")
			return
		}

		user, err := s.store.GetUserByID(r.Context(), session.UserID)
		if err != nil || user == nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "user not found")
			return
		}
		if user.MustChangePassword && !passwordChangeExempt(r) {
			writeError(w, http.StatusForbidden, "PASSWORD_CHANGE_REQUIRED", "password change required before accessing this resource")
			return
		}

		ctx := context.WithValue(r.Context(), userContextKey, user)
		next(w, r.WithContext(ctx))
	}
}

func (s *Server) withRole(minRole string, next http.HandlerFunc) http.HandlerFunc {
	return s.withSession(func(w http.ResponseWriter, r *http.Request) {
		user := getUserFromContext(r)
		if user == nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
			return
		}
		if !hasMinimumRole(user.Role, minRole) {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "insufficient permissions")
			return
		}
		next(w, r)
	})
}

func hasMinimumRole(userRole, minRole string) bool {
	roleLevel := map[string]int{
		"viewer": 1,
		"admin":  2,
		"owner":  3,
	}
	return roleLevel[userRole] >= roleLevel[minRole]
}

func extractSessionToken(r *http.Request) string {
	if cookie, err := r.Cookie("session"); err == nil && cookie.Value != "" {
		return cookie.Value
	}
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	return ""
}

func passwordChangeExempt(r *http.Request) bool {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/auth/me":
		return true
	case r.Method == http.MethodPut && r.URL.Path == "/api/v1/auth/password":
		return true
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/settings":
		return true
	default:
		return false
	}
}

func setSessionCookie(w http.ResponseWriter, r *http.Request, token string, expiresAt time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   isHTTPSRequest(r),
		MaxAge:   int(time.Until(expiresAt).Seconds()),
		Expires:  expiresAt,
	})
}

func clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   isHTTPSRequest(r),
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})
}

func isHTTPSRequest(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}
