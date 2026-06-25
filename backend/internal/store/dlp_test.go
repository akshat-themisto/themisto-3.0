package store

import "testing"

func TestDLPOrderClauseWhitelistsSortFields(t *testing.T) {
	got := dlpOrderClause("severity", "asc")
	want := "CASE e.severity WHEN 'critical' THEN 4 WHEN 'high' THEN 3 WHEN 'medium' THEN 2 WHEN 'low' THEN 1 ELSE 0 END ASC, e.timestamp DESC"
	if got != want {
		t.Fatalf("severity order = %q want %q", got, want)
	}

	got = dlpOrderClause("timestamp; DROP TABLE dlp_events", "desc")
	if got != "e.timestamp DESC" {
		t.Fatalf("unsafe sort should fall back to timestamp, got %q", got)
	}
}
