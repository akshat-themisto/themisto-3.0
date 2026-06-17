package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/themisto/backend/internal/store"
)

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	user := getUserFromContext(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}

	org, err := s.store.GetOrganizationByID(r.Context(), user.OrgID)
	if err != nil {
		s.logger.Error("get org", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}

	users, err := s.store.ListUsers(r.Context(), user.OrgID)
	if err != nil {
		s.logger.Error("list users", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}

	if users == nil {
		users = []store.AdminUser{}
	}

	// Strip password hashes from response
	type safeUser struct {
		ID    string `json:"id"`
		Email string `json:"email"`
		Name  string `json:"name"`
		Role  string `json:"role"`
	}
	safeUsers := make([]safeUser, len(users))
	for i, u := range users {
		safeUsers[i] = safeUser{ID: u.ID, Email: u.Email, Name: u.Name, Role: u.Role}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"organization": map[string]interface{}{
			"id":     org.ID,
			"name":   org.Name,
			"slug":   org.Slug,
			"status": org.Status,
		},
		"users": safeUsers,
	})
}

type updateSettingsRequest struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

func (s *Server) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	user := getUserFromContext(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}

	var req updateSettingsRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON body")
		return
	}

	if req.Name == "" || req.Slug == "" {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "name and slug are required")
		return
	}

	if err := s.store.UpdateOrganization(r.Context(), user.OrgID, req.Name, req.Slug); err != nil {
		s.logger.Error("update org", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

type createUserRequest struct {
	Email    string `json:"email"`
	Name     string `json:"name"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	user := getUserFromContext(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}

	var req createUserRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON body")
		return
	}

	if req.Email == "" || req.Name == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "email, name, and password are required")
		return
	}

	validRoles := map[string]bool{"viewer": true, "admin": true, "owner": true}
	if !validRoles[req.Role] {
		req.Role = "viewer"
	}

	newUser, err := s.store.CreateUser(r.Context(), user.OrgID, req.Email, req.Name, req.Password, req.Role)
	if err != nil {
		s.logger.Error("create user", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"id":    newUser.ID,
		"email": newUser.Email,
		"name":  newUser.Name,
		"role":  newUser.Role,
	})
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	user := getUserFromContext(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}

	targetID := r.PathValue("id")

	if targetID == user.ID {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "cannot delete yourself")
		return
	}

	// Verify target user belongs to the same org.
	target, err := s.store.GetUserByID(r.Context(), targetID)
	if err != nil {
		s.logger.Error("get target user", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	if target == nil || target.OrgID != user.OrgID {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "user not found")
		return
	}

	if err := s.store.DeleteUser(r.Context(), targetID); err != nil {
		s.logger.Error("delete user", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	user := getUserFromContext(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}

	var req changePasswordRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON body")
		return
	}

	if req.CurrentPassword == "" || req.NewPassword == "" {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "current_password and new_password are required")
		return
	}

	if len(req.NewPassword) < 8 {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "new password must be at least 8 characters")
		return
	}

	if !s.store.CheckPassword(user, req.CurrentPassword) {
		writeError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "current password is incorrect")
		return
	}

	rawToken, tokenHash, err := store.GenerateSessionToken()
	if err != nil {
		s.logger.Error("generate replacement session token", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}

	expiresAt := time.Now().Add(s.tokenTTL)
	if err := s.store.RotateUserPasswordAndSessions(r.Context(), user.ID, req.NewPassword, tokenHash, expiresAt); err != nil {
		s.logger.Error("rotate password and sessions", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}

	setSessionCookie(w, r, rawToken, expiresAt)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":     "password_updated",
		"token":      rawToken,
		"expires_at": expiresAt,
	})
}
