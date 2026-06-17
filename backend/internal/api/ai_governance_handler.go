package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/themisto/backend/internal/store"
)

type upsertAIVendorGovernanceRequest struct {
	Sanctioned bool    `json:"sanctioned"`
	RiskTier   string  `json:"risk_tier"`
	Notes      *string `json:"notes,omitempty"`
}

func (s *Server) handleListAIVendorGovernance(w http.ResponseWriter, r *http.Request) {
	user := getUserFromContext(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}

	rows, err := s.store.ListAIVendorGovernance(r.Context(), user.OrgID)
	if err != nil {
		s.logger.Error("list ai governance", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	if rows == nil {
		rows = []store.AIVendorGovernance{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"vendors": rows,
		"total":   len(rows),
	})
}

func (s *Server) handleUpsertAIVendorGovernance(w http.ResponseWriter, r *http.Request) {
	user := getUserFromContext(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}

	vendor := strings.ToLower(strings.TrimSpace(r.PathValue("vendor")))
	if vendor == "" {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "vendor is required")
		return
	}

	var req upsertAIVendorGovernanceRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON body")
		return
	}

	row, err := s.store.UpsertAIVendorGovernance(r.Context(), user.OrgID, store.AIVendorGovernance{
		AIVendor:   vendor,
		Sanctioned: req.Sanctioned,
		RiskTier:   req.RiskTier,
		Notes:      req.Notes,
		UpdatedBy:  &user.Email,
	})
	if err != nil {
		s.logger.Error("upsert ai governance", "vendor", vendor, "error", err)
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}
	if row.Sanctioned {
		if err := s.store.DisableUnsanctionedBlockRule(r.Context(), user.OrgID, vendor); err != nil {
			s.logger.Error("disable unsanctioned block rule", "vendor", vendor, "error", err)
			writeError(w, http.StatusInternalServerError, "INTERNAL", "failed to disable vendor block rule")
			return
		}
	}

	orgID := user.OrgID
	_ = s.store.InsertAudit(r.Context(), nil, &store.AuditEntry{
		ActorType:    "admin_user",
		ActorID:      user.ID,
		OrgID:        &orgID,
		Action:       "ai.governance.updated",
		ResourceType: "ai_vendor",
		ResourceID:   vendor,
		Details: map[string]interface{}{
			"sanctioned": row.Sanctioned,
			"risk_tier":  row.RiskTier,
			"notes":      row.Notes,
		},
		IPAddress: clientIPFromRequest(r),
	})

	writeJSON(w, http.StatusOK, row)
}

func (s *Server) handleBlockUnsanctionedVendor(w http.ResponseWriter, r *http.Request) {
	user := getUserFromContext(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}

	vendor := strings.ToLower(strings.TrimSpace(r.PathValue("vendor")))
	if vendor == "" {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "vendor is required")
		return
	}

	_, err := s.store.UpsertAIVendorGovernance(r.Context(), user.OrgID, store.AIVendorGovernance{
		AIVendor:   vendor,
		Sanctioned: false,
		RiskTier:   "high",
		UpdatedBy:  &user.Email,
	})
	if err != nil {
		s.logger.Error("set unsanctioned vendor", "vendor", vendor, "error", err)
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}
	if err := s.store.EnsureUnsanctionedBlockRule(r.Context(), user.OrgID, vendor, user.Email); err != nil {
		s.logger.Error("ensure unsanctioned block rule", "vendor", vendor, "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "failed to enforce unsanctioned block")
		return
	}

	orgID := user.OrgID
	_ = s.store.InsertAudit(r.Context(), nil, &store.AuditEntry{
		ActorType:    "admin_user",
		ActorID:      user.ID,
		OrgID:        &orgID,
		Action:       "ai.governance.block_unsanctioned",
		ResourceType: "ai_vendor",
		ResourceID:   vendor,
		Details: map[string]interface{}{
			"enforcement": "block",
		},
		IPAddress: clientIPFromRequest(r),
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":    "ok",
		"ai_vendor": vendor,
		"enforced":  "block",
	})
}

func (s *Server) handleUnblockVendor(w http.ResponseWriter, r *http.Request) {
	user := getUserFromContext(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}

	vendor := strings.ToLower(strings.TrimSpace(r.PathValue("vendor")))
	if vendor == "" {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "vendor is required")
		return
	}

	existing, err := s.store.GetAIVendorGovernance(r.Context(), user.OrgID, vendor)
	if err != nil {
		s.logger.Error("get ai governance", "vendor", vendor, "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "failed to read governance state")
		return
	}

	if err := s.store.DisableUnsanctionedBlockRule(r.Context(), user.OrgID, vendor); err != nil {
		s.logger.Error("disable unsanctioned block rule", "vendor", vendor, "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "failed to disable vendor block rule")
		return
	}

	orgID := user.OrgID
	_ = s.store.InsertAudit(r.Context(), nil, &store.AuditEntry{
		ActorType:    "admin_user",
		ActorID:      user.ID,
		OrgID:        &orgID,
		Action:       "ai.governance.unblocked",
		ResourceType: "ai_vendor",
		ResourceID:   vendor,
		Details: map[string]interface{}{
			"sanctioned": existing != nil && existing.Sanctioned,
			"enforced":   "allow",
		},
		IPAddress: clientIPFromRequest(r),
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":    "ok",
		"ai_vendor": vendor,
		"enforced":  "allow",
		"vendor":    existing,
	})
}
