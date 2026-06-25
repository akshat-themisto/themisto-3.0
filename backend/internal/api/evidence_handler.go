package api

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/lib/pq"
	"github.com/themisto/backend/internal/store"
)

func (s *Server) handleExportPolicySnapshot(w http.ResponseWriter, r *http.Request) {
	user := getUserFromContext(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}

	rules, err := s.store.ListPolicyRulesV2(r.Context(), user.OrgID)
	if err != nil {
		s.logger.Error("export policy snapshot", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=policy-snapshot-%s.json", time.Now().Format("20060102-150405")))
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"generated_at": time.Now().UTC(),
		"org_id":       user.OrgID,
		"rules":        rules,
	})
}

func (s *Server) handleExportDLPEventsCSV(w http.ResponseWriter, r *http.Request) {
	user := getUserFromContext(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}

	rows, err := s.store.DB.QueryContext(r.Context(), `
		SELECT id, timestamp, device_id, request_host, request_method,
		       COALESCE(source_app, ''),
		       COALESCE(ai_vendor, ''),
		       COALESCE(reason_code, ''),
		       COALESCE(action_taken, ''),
		       COALESCE(match_count, 0),
		       COALESCE(policy_rule_id, ''),
		       COALESCE(protocol, 'http'),
		       COALESCE(intercepted_https, false),
		       COALESCE(inspection_quality, 'full'),
		       COALESCE(inspection_skip_reason, ''),
		       COALESCE(direction, 'outbound'),
		       COALESCE(request_body_truncated, false),
		       COALESCE(semantic_source, ''),
		       COALESCE(semantic_category, ''),
		       COALESCE(semantic_confidence, 0),
		       COALESCE(semantic_ambiguous, false),
		       COALESCE(semantic_reason, ''),
		       COALESCE(review_status, 'unreviewed'),
		       COALESCE(review_note, ''),
		       COALESCE(reviewed_by, ''),
		       reviewed_at,
		       match_types,
		       matched_patterns
		FROM dlp_events
		WHERE org_id = $1
		ORDER BY timestamp DESC
		LIMIT 20000`, user.OrgID)
	if err != nil {
		s.logger.Error("export dlp csv", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	defer rows.Close()

	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=dlp-events-%s.csv", time.Now().Format("20060102-150405")))
	cw := csv.NewWriter(w)
	defer cw.Flush()

	_ = cw.Write([]string{"id", "timestamp", "device_id", "request_host", "request_method", "source_app", "ai_vendor", "reason_code", "action_taken", "match_count", "policy_rule_id", "protocol", "intercepted_https", "inspection_quality", "inspection_skip_reason", "direction", "request_body_truncated", "semantic_source", "semantic_category", "semantic_confidence", "semantic_ambiguous", "semantic_reason", "review_status", "review_note", "reviewed_by", "reviewed_at", "match_types", "matched_patterns"})

	for rows.Next() {
		var (
			id                 int64
			ts                 time.Time
			deviceID           string
			host               string
			method             string
			sourceApp          string
			aiVendor           string
			reasonCode         string
			action             string
			matchCount         int
			policyRuleID       string
			protocol           string
			intercepted        bool
			quality            string
			skipReason         string
			direction          string
			bodyTruncated      bool
			semanticSource     string
			semanticCategory   string
			semanticConfidence float64
			semanticAmbiguous  bool
			semanticReason     string
			reviewStatus       string
			reviewNote         string
			reviewedBy         string
			reviewedAt         *time.Time
			matchTypes         []string
			patterns           []string
		)
		if err := rows.Scan(&id, &ts, &deviceID, &host, &method, &sourceApp, &aiVendor, &reasonCode, &action, &matchCount, &policyRuleID, &protocol, &intercepted, &quality, &skipReason, &direction, &bodyTruncated, &semanticSource, &semanticCategory, &semanticConfidence, &semanticAmbiguous, &semanticReason, &reviewStatus, &reviewNote, &reviewedBy, &reviewedAt, pq.Array(&matchTypes), pq.Array(&patterns)); err != nil {
			continue
		}
		reviewedAtValue := ""
		if reviewedAt != nil {
			reviewedAtValue = reviewedAt.UTC().Format(time.RFC3339)
		}
		_ = cw.Write([]string{
			strconv.FormatInt(id, 10),
			ts.UTC().Format(time.RFC3339),
			deviceID,
			host,
			method,
			sourceApp,
			aiVendor,
			reasonCode,
			action,
			strconv.Itoa(matchCount),
			policyRuleID,
			protocol,
			strconv.FormatBool(intercepted),
			quality,
			skipReason,
			direction,
			strconv.FormatBool(bodyTruncated),
			semanticSource,
			semanticCategory,
			strconv.FormatFloat(semanticConfidence, 'f', 3, 64),
			strconv.FormatBool(semanticAmbiguous),
			semanticReason,
			reviewStatus,
			reviewNote,
			reviewedBy,
			reviewedAtValue,
			strings.Join(matchTypes, ";"),
			strings.Join(patterns, ";"),
		})
	}
}

func (s *Server) handleExportAuditCSV(w http.ResponseWriter, r *http.Request) {
	user := getUserFromContext(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}

	entries, _, err := s.store.ListAuditLog(r.Context(), store.AuditLogFilter{OrgID: user.OrgID, Page: 1, Limit: 10000})
	if err != nil {
		s.logger.Error("export audit csv", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}

	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=audit-log-%s.csv", time.Now().Format("20060102-150405")))
	cw := csv.NewWriter(w)
	defer cw.Flush()
	_ = cw.Write([]string{"id", "timestamp", "actor_type", "actor_id", "action", "resource_type", "resource_id", "ip_address"})

	for _, e := range entries {
		ip := ""
		if e.IPAddress != nil {
			ip = *e.IPAddress
		}
		_ = cw.Write([]string{
			strconv.FormatInt(e.ID, 10),
			e.Timestamp.UTC().Format(time.RFC3339),
			e.ActorType,
			e.ActorID,
			e.Action,
			e.ResourceType,
			e.ResourceID,
			ip,
		})
	}
}
