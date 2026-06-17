package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
)

// AlertFeedItem represents one dashboard bell notification sourced from DLP events.
type AlertFeedItem struct {
	EventID              int64      `json:"event_id"`
	Timestamp            time.Time  `json:"timestamp"`
	DeviceID             string     `json:"device_id"`
	RequestHost          string     `json:"request_host"`
	RequestPath          *string    `json:"request_path,omitempty"`
	RequestMethod        string     `json:"request_method"`
	SourceApp            *string    `json:"source_app,omitempty"`
	AIVendor             *string    `json:"ai_vendor,omitempty"`
	ServiceCategory      *string    `json:"service_category,omitempty"`
	ActionTaken          string     `json:"action_taken"`
	Severity             string     `json:"severity"`
	ClassificationReason *string    `json:"classification_reason,omitempty"`
	ReasonCode           *string    `json:"reason_code,omitempty"`
	ReasonDetail         *string    `json:"reason_detail,omitempty"`
	MatchTypes           []string   `json:"match_types"`
	MatchedPatterns      []string   `json:"matched_patterns"`
	MatchedFields        []string   `json:"matched_fields"`
	ContentType          *string    `json:"content_type,omitempty"`
	FileCount            int        `json:"file_count"`
	HasRequestBody       bool       `json:"has_request_body"`
	Unread               bool       `json:"unread"`
	ReadAt               *time.Time `json:"read_at,omitempty"`
}

// AlertFilter configures alert feed retrieval.
type AlertFilter struct {
	OrgID    string
	UserID   string
	Severity string
	Action   string
	Page     int
	Limit    int
}

// ListAlerts returns alert feed items plus total/unread counts.
func (s *Store) ListAlerts(ctx context.Context, filter AlertFilter) ([]AlertFeedItem, int, int, error) {
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.Limit < 1 || filter.Limit > 100 {
		filter.Limit = 25
	}

	whereTotal := "WHERE e.org_id = $1"
	argsTotal := []interface{}{filter.OrgID}
	argNTotal := 2
	if sev := strings.ToLower(strings.TrimSpace(filter.Severity)); sev != "" {
		whereTotal += fmt.Sprintf(" AND e.severity = $%d", argNTotal)
		argsTotal = append(argsTotal, sev)
		argNTotal++
	}
	if action := strings.ToLower(strings.TrimSpace(filter.Action)); action != "" {
		whereTotal += fmt.Sprintf(" AND e.action_taken = $%d", argNTotal)
		argsTotal = append(argsTotal, action)
		argNTotal++
	}

	var total int
	if err := s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM dlp_events e "+whereTotal, argsTotal...).Scan(&total); err != nil {
		return nil, 0, 0, err
	}

	whereFeed := "WHERE e.org_id = $1"
	argsFeed := []interface{}{filter.OrgID, filter.UserID}
	argNFeed := 3
	if sev := strings.ToLower(strings.TrimSpace(filter.Severity)); sev != "" {
		whereFeed += fmt.Sprintf(" AND e.severity = $%d", argNFeed)
		argsFeed = append(argsFeed, sev)
		argNFeed++
	}
	if action := strings.ToLower(strings.TrimSpace(filter.Action)); action != "" {
		whereFeed += fmt.Sprintf(" AND e.action_taken = $%d", argNFeed)
		argsFeed = append(argsFeed, action)
		argNFeed++
	}

	var unread int
	if err := s.DB.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM dlp_events e
		LEFT JOIN admin_user_alert_reads r
		ON r.dlp_event_id = e.id AND r.user_id = $2::uuid
		`+whereFeed+` AND r.user_id IS NULL`, argsFeed...).Scan(&unread); err != nil {
		return nil, 0, 0, err
	}

	offset := (filter.Page - 1) * filter.Limit
	query := fmt.Sprintf(`
		SELECT e.id, e.timestamp, e.device_id, e.request_host, e.request_path, e.request_method,
		       e.source_app, e.ai_vendor, e.service_category, e.action_taken, e.severity,
		       e.classification_reason, e.reason_code, e.reason_detail,
		       e.match_types, e.matched_patterns, e.matched_fields,
		       e.content_type, e.file_count,
		       (e.request_body_encrypted IS NOT NULL) AS has_request_body,
		       (r.user_id IS NULL) AS unread,
		       r.read_at
		FROM dlp_events e
		LEFT JOIN admin_user_alert_reads r
		  ON r.dlp_event_id = e.id AND r.user_id = $2::uuid
		%s
		ORDER BY e.timestamp DESC
		LIMIT $%d OFFSET $%d`, whereFeed, argNFeed, argNFeed+1)

	argsFeed = append(argsFeed, filter.Limit, offset)
	rows, err := s.DB.QueryContext(ctx, query, argsFeed...)
	if err != nil {
		return nil, 0, 0, err
	}
	defer rows.Close()

	items := make([]AlertFeedItem, 0, filter.Limit)
	for rows.Next() {
		var item AlertFeedItem
		if err := rows.Scan(
			&item.EventID,
			&item.Timestamp,
			&item.DeviceID,
			&item.RequestHost,
			&item.RequestPath,
			&item.RequestMethod,
			&item.SourceApp,
			&item.AIVendor,
			&item.ServiceCategory,
			&item.ActionTaken,
			&item.Severity,
			&item.ClassificationReason,
			&item.ReasonCode,
			&item.ReasonDetail,
			pq.Array(&item.MatchTypes),
			pq.Array(&item.MatchedPatterns),
			pq.Array(&item.MatchedFields),
			&item.ContentType,
			&item.FileCount,
			&item.HasRequestBody,
			&item.Unread,
			&item.ReadAt,
		); err != nil {
			return nil, 0, 0, err
		}
		if item.MatchTypes == nil {
			item.MatchTypes = []string{}
		}
		if item.MatchedPatterns == nil {
			item.MatchedPatterns = []string{}
		}
		if item.MatchedFields == nil {
			item.MatchedFields = []string{}
		}
		if strings.TrimSpace(item.Severity) == "" {
			item.Severity = "low"
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, 0, err
	}
	return items, total, unread, nil
}

// MarkAlertsRead marks alert events as read for a specific admin user.
func (s *Store) MarkAlertsRead(ctx context.Context, orgID, userID string, eventIDs []int64) error {
	if len(eventIDs) == 0 {
		return nil
	}
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO admin_user_alert_reads (user_id, dlp_event_id, read_at)
		SELECT $1::uuid, e.id, now()
		FROM dlp_events e
		WHERE e.org_id = $2
		  AND e.id = ANY($3)
		ON CONFLICT (user_id, dlp_event_id)
		DO UPDATE SET read_at = EXCLUDED.read_at`,
		userID, orgID, pq.Array(eventIDs),
	)
	return err
}
