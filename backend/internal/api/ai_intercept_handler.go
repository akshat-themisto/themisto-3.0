package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/themisto/backend/internal/store"
)

type replaceAIInterceptDomainsRequest struct {
	Domains []string `json:"domains"`
}

func (s *Server) handleListAIInterceptDomains(w http.ResponseWriter, r *http.Request) {
	user := getUserFromContext(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}

	if err := s.store.EnsureDefaultAIInterceptDomains(r.Context(), user.OrgID); err != nil {
		s.logger.Warn("ensure default ai intercept domains", "org_id", user.OrgID, "error", err)
	}

	effective, err := s.store.ListEffectiveAIInterceptDomains(r.Context(), user.OrgID)
	if err != nil {
		s.logger.Error("list effective ai intercept domains", "org_id", user.OrgID, "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	entries, err := s.store.ListAIInterceptDomains(r.Context(), user.OrgID)
	if err != nil {
		s.logger.Error("list ai intercept domain entries", "org_id", user.OrgID, "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	if effective == nil {
		effective = []string{}
	}
	if entries == nil {
		entries = []store.AIInterceptDomain{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"domains": effective,
		"entries": entries,
		"total":   len(effective),
	})
}

func (s *Server) handleReplaceAIInterceptDomains(w http.ResponseWriter, r *http.Request) {
	user := getUserFromContext(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}

	var req replaceAIInterceptDomainsRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON body")
		return
	}
	for i := range req.Domains {
		req.Domains[i] = strings.TrimSpace(req.Domains[i])
	}

	domains, err := s.store.ReplaceManagedAIInterceptDomains(r.Context(), user.OrgID, req.Domains)
	if err != nil {
		s.logger.Error("replace ai intercept domains", "org_id", user.OrgID, "error", err)
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}

	orgID := user.OrgID
	_ = s.store.InsertAudit(r.Context(), nil, &store.AuditEntry{
		ActorType:    "admin_user",
		ActorID:      user.ID,
		OrgID:        &orgID,
		Action:       "ai.intercept_domains.updated",
		ResourceType: "ai_intercept_domains",
		ResourceID:   user.OrgID,
		Details: map[string]interface{}{
			"updated_count": len(domains),
			"domains":       domains,
		},
		IPAddress: clientIPFromRequest(r),
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"domains": domains,
		"total":   len(domains),
	})
}
