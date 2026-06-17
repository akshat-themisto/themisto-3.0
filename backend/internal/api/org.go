package api

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"

	"github.com/themisto/backend/internal/store"
	"github.com/themisto/backend/internal/token"
)

var orgSlugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9\-]{1,62}[a-z0-9]$`)

type createOrgRequest struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

func (s *Server) handleCreateOrg(w http.ResponseWriter, r *http.Request) {
	var req createOrgRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON body")
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	req.Slug = strings.ToLower(strings.TrimSpace(req.Slug))

	if req.Name == "" || req.Slug == "" {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "name and slug are required")
		return
	}
	if !orgSlugRe.MatchString(req.Slug) {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "slug must be 3-64 lowercase alphanumeric chars or hyphens")
		return
	}

	rawAPIKey, apiKeyHash, err := token.Generate()
	if err != nil {
		s.logger.Error("generate api key", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}

	org, err := s.store.CreateOrganization(r.Context(), req.Name, req.Slug, apiKeyHash)
	if err != nil {
		if isDuplicateError(err) {
			writeError(w, http.StatusConflict, "ORG_EXISTS", "an organization with this slug already exists")
			return
		}
		s.logger.Error("create org", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}

	_ = s.store.InsertAudit(r.Context(), nil, &store.AuditEntry{
		ActorType:    "system",
		ActorID:      "api-key",
		OrgID:        &org.ID,
		Action:       "org.created",
		ResourceType: "organization",
		ResourceID:   org.ID,
		Details: map[string]interface{}{
			"name": org.Name,
			"slug": org.Slug,
		},
	})

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"org_id":  org.ID,
		"name":    org.Name,
		"slug":    org.Slug,
		"api_key": rawAPIKey,
	})
}
