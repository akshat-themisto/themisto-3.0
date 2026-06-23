package store

import (
	"context"
	"fmt"
	"time"
)

// AIVendorUsageStatV2 includes governance information for vendor usage.
type AIVendorUsageStatV2 struct {
	Vendor         string `json:"vendor"`
	Category       string `json:"category"`
	RequestCount   int    `json:"request_count"`
	BlockedCount   int    `json:"blocked_count"`
	DeviceCount    int    `json:"device_count"`
	BytesSentTotal int64  `json:"bytes_sent_total"`
	Sanctioned     bool   `json:"sanctioned"`
	RiskTier       string `json:"risk_tier"`
}

type CategoryStatV2 struct {
	Category     string `json:"category"`
	RequestCount int    `json:"request_count"`
	DeviceCount  int    `json:"device_count"`
}

type SurfaceUsageStat struct {
	Surface      string `json:"surface"`
	RequestCount int    `json:"request_count"`
	DeviceCount  int    `json:"device_count"`
}

type AIUsageSummaryV2 struct {
	TotalAIRequests   int                   `json:"total_ai_requests"`
	UniqueAIVendors   int                   `json:"unique_ai_vendors"`
	UniqueAIDevices   int                   `json:"unique_ai_devices"`
	UnsanctionedCount int                   `json:"unsanctioned_count"`
	VendorBreakdown   []AIVendorUsageStatV2 `json:"vendor_breakdown"`
	CategoryBreakdown []CategoryStatV2      `json:"category_breakdown"`
	SurfaceBreakdown  []SurfaceUsageStat    `json:"surface_breakdown"`
}

type AIViolatingDeviceStat struct {
	DeviceID           string `json:"device_id"`
	DeviceName         string `json:"device_name"`
	UnsanctionedEvents int    `json:"unsanctioned_events"`
	UniqueVendors      int    `json:"unique_vendors"`
}

func (s *Store) GetAIUsageSummaryV2(ctx context.Context, orgID string, from, to time.Time) (*AIUsageSummaryV2, error) {
	summary := &AIUsageSummaryV2{}

	err := s.DB.QueryRowContext(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE t.ai_vendor IS NOT NULL),
			COUNT(DISTINCT t.ai_vendor) FILTER (WHERE t.ai_vendor IS NOT NULL),
			COUNT(DISTINCT t.device_id) FILTER (WHERE t.ai_vendor IS NOT NULL),
			COUNT(*) FILTER (WHERE t.ai_vendor IS NOT NULL AND COALESCE(g.sanctioned, false) = false)
		FROM telemetry_events t
		LEFT JOIN ai_vendor_governance g
		  ON g.org_id::text = t.org_id AND g.ai_vendor = t.ai_vendor
		WHERE t.org_id = $1 AND t.timestamp >= $2 AND t.timestamp <= $3`,
		orgID, from, to,
	).Scan(&summary.TotalAIRequests, &summary.UniqueAIVendors, &summary.UniqueAIDevices, &summary.UnsanctionedCount)
	if err != nil {
		return nil, fmt.Errorf("ai usage summary v2: %w", err)
	}

	rows, err := s.DB.QueryContext(ctx, `
		SELECT
			t.ai_vendor,
			COALESCE(t.service_category, 'unknown'),
			COUNT(*) as request_count,
			COUNT(*) FILTER (WHERE t.policy_decision = 'block') as blocked_count,
			COUNT(DISTINCT t.device_id) as device_count,
			COALESCE(SUM(t.bytes_sent), 0) as bytes_sent_total,
			COALESCE(g.sanctioned, false) as sanctioned,
			COALESCE(g.risk_tier, 'unknown') as risk_tier
		FROM telemetry_events t
		LEFT JOIN ai_vendor_governance g
		  ON g.org_id::text = t.org_id AND g.ai_vendor = t.ai_vendor
		WHERE t.org_id = $1 AND t.timestamp >= $2 AND t.timestamp <= $3
		  AND t.ai_vendor IS NOT NULL
		GROUP BY t.ai_vendor, COALESCE(t.service_category, 'unknown'), g.sanctioned, g.risk_tier
		ORDER BY request_count DESC`,
		orgID, from, to,
	)
	if err != nil {
		return nil, fmt.Errorf("ai vendor usage v2: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var row AIVendorUsageStatV2
		if err := rows.Scan(
			&row.Vendor, &row.Category, &row.RequestCount, &row.BlockedCount,
			&row.DeviceCount, &row.BytesSentTotal, &row.Sanctioned, &row.RiskTier,
		); err != nil {
			return nil, err
		}
		summary.VendorBreakdown = append(summary.VendorBreakdown, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	catRows, err := s.DB.QueryContext(ctx, `
		SELECT
			service_category,
			COUNT(*) as request_count,
			COUNT(DISTINCT device_id) as device_count
		FROM telemetry_events
		WHERE org_id = $1 AND timestamp >= $2 AND timestamp <= $3
		  AND service_category IS NOT NULL
		GROUP BY service_category
		ORDER BY request_count DESC`,
		orgID, from, to,
	)
	if err != nil {
		return nil, fmt.Errorf("ai category usage v2: %w", err)
	}
	defer catRows.Close()

	for catRows.Next() {
		var row CategoryStatV2
		if err := catRows.Scan(&row.Category, &row.RequestCount, &row.DeviceCount); err != nil {
			return nil, err
		}
		summary.CategoryBreakdown = append(summary.CategoryBreakdown, row)
	}
	if err := catRows.Err(); err != nil {
		return nil, err
	}

	surfRows, err := s.DB.QueryContext(ctx, `
		SELECT
			capture_surface,
			COUNT(*) as request_count,
			COUNT(DISTINCT device_id) as device_count
		FROM telemetry_events
		WHERE org_id = $1 AND timestamp >= $2 AND timestamp <= $3
		  AND capture_surface IS NOT NULL
		GROUP BY capture_surface
		ORDER BY request_count DESC`,
		orgID, from, to,
	)
	if err != nil {
		return nil, fmt.Errorf("ai surface usage: %w", err)
	}
	defer surfRows.Close()

	for surfRows.Next() {
		var row SurfaceUsageStat
		if err := surfRows.Scan(&row.Surface, &row.RequestCount, &row.DeviceCount); err != nil {
			return nil, err
		}
		summary.SurfaceBreakdown = append(summary.SurfaceBreakdown, row)
	}
	return summary, surfRows.Err()
}

func (s *Store) GetTopAIViolatingDevices(ctx context.Context, orgID string, limit int, from, to time.Time) ([]AIViolatingDeviceStat, error) {
	if limit < 1 || limit > 50 {
		limit = 10
	}
	rows, err := s.DB.QueryContext(ctx, `
		SELECT
			t.device_id,
			COALESCE(d.device_name, ''),
			COUNT(*) as unsanctioned_events,
			COUNT(DISTINCT t.ai_vendor) as unique_vendors
		FROM telemetry_events t
		LEFT JOIN ai_vendor_governance g
		  ON g.org_id::text = t.org_id AND g.ai_vendor = t.ai_vendor
		LEFT JOIN devices d
		  ON d.id::text = t.device_id AND d.org_id::text = t.org_id
		WHERE t.org_id = $1 AND t.timestamp >= $2 AND t.timestamp <= $3
		  AND t.ai_vendor IS NOT NULL
		  AND COALESCE(g.sanctioned, false) = false
		GROUP BY t.device_id, d.device_name
		ORDER BY unsanctioned_events DESC
		LIMIT $4`,
		orgID, from, to, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]AIViolatingDeviceStat, 0)
	for rows.Next() {
		var row AIViolatingDeviceStat
		if err := rows.Scan(&row.DeviceID, &row.DeviceName, &row.UnsanctionedEvents, &row.UniqueVendors); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
