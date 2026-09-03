package ailedger

import (
	"testing"
	"time"
)

func TestFindingsRespectEndpointPrecedenceAndCostExclusion(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	snapshot := FindingSnapshot{
		Products: []FindingProduct{{VendorKey: "acme", ProductKey: "assistant", FunctionalCategory: "general_assistant"}},
		Users:    []DirectoryUser{{ID: "u1", Status: "active"}},
		Seats:    []FindingSeat{{LicenseKey: "seat-1", VendorKey: "acme", ProductKey: "assistant", DirectoryUserID: "u1", Paid: true, Status: "assigned", SourceKey: "seat-source"}},
		Activities: []FindingActivity{
			{VendorKey: "acme", ProductKey: "assistant", DirectoryUserID: "u1", OriginKind: OriginEndpoint, IsActivity: true, ObservedAt: now.Add(-time.Hour), SourceKey: "endpoint-source"},
		},
		Costs: []FindingCost{{VendorKey: "acme", ProductKey: "assistant", Amount: 99, Currency: "USD", Day: now, SourceEvidence: SourceAuthoritative, SourceKey: "cost-source"}},
	}
	findings := EvaluateFindings(snapshot, now)
	assertHasFinding(t, findings, FindingShadowAI)
	assertHasFinding(t, findings, FindingUnattributedSpend)
	assertNoFinding(t, findings, FindingInactivePaidSeat)
	assertNoFinding(t, findings, FindingConnectorActivityCoverageGap)
}

func TestCostOnlyDoesNotPreventInactiveSeat(t *testing.T) {
	now := time.Now()
	findings := EvaluateFindings(FindingSnapshot{
		Users: []DirectoryUser{{ID: "u1", Status: "active"}},
		Seats: []FindingSeat{{LicenseKey: "seat", VendorKey: "acme", ProductKey: "assistant", DirectoryUserID: "u1", Paid: true, Status: "assigned"}},
		Costs: []FindingCost{{VendorKey: "acme", ProductKey: "assistant", DirectoryUserID: "u1", Amount: 10, Currency: "USD", Day: now, SourceEvidence: SourceAuthoritative}},
	}, now)
	assertHasFinding(t, findings, FindingInactivePaidSeat)
}

func TestConnectorActivityPreventsInactiveButCreatesCoverageGap(t *testing.T) {
	now := time.Now()
	findings := EvaluateFindings(FindingSnapshot{
		Users:      []DirectoryUser{{ID: "u1", Status: "active"}},
		Seats:      []FindingSeat{{LicenseKey: "seat", VendorKey: "acme", ProductKey: "assistant", DirectoryUserID: "u1", Paid: true, Status: "assigned"}},
		Activities: []FindingActivity{{VendorKey: "acme", ProductKey: "assistant", DirectoryUserID: "u1", OriginKind: OriginConnector, IsActivity: true, ObservedAt: now, SourceKey: "connector-use"}},
	}, now)
	assertNoFinding(t, findings, FindingInactivePaidSeat)
	assertHasFinding(t, findings, FindingConnectorActivityCoverageGap)
}

func TestSpendSpikeUsesCurrencySeparatelyAndPreservesCredits(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	var costs []FindingCost
	for i := 8; i <= 35; i++ {
		costs = append(costs, FindingCost{VendorKey: "acme", ProductKey: "assistant", ProjectKey: "p", Amount: 20, Currency: "USD", Day: now.AddDate(0, 0, -i), SourceEvidence: SourceAuthoritative})
	}
	for i := 1; i <= 7; i++ {
		costs = append(costs,
			FindingCost{VendorKey: "acme", ProductKey: "assistant", ProjectKey: "p", Amount: 100, Currency: "USD", Day: now.AddDate(0, 0, -i), SourceEvidence: SourceAuthoritative},
			FindingCost{VendorKey: "acme", ProductKey: "assistant", ProjectKey: "p", Amount: -10, Currency: "USD", Day: now.AddDate(0, 0, -i), SourceEvidence: SourceAuthoritative},
			FindingCost{VendorKey: "acme", ProductKey: "assistant", ProjectKey: "p", Amount: 5, Currency: "EUR", Day: now.AddDate(0, 0, -i), SourceEvidence: SourceAuthoritative},
		)
	}
	findings := EvaluateFindings(FindingSnapshot{Costs: costs}, now)
	assertHasFinding(t, findings, FindingSpendSpike)
	for _, finding := range findings {
		if finding.Type == FindingSpendSpike && finding.Currency != "USD" {
			t.Fatalf("unexpected mixed-currency spike: %+v", finding)
		}
	}
}

func assertHasFinding(t *testing.T, findings []Finding, kind string) {
	t.Helper()
	for _, finding := range findings {
		if finding.Type == kind {
			return
		}
	}
	t.Fatalf("missing finding %s in %+v", kind, findings)
}

func assertNoFinding(t *testing.T, findings []Finding, kind string) {
	t.Helper()
	for _, finding := range findings {
		if finding.Type == kind {
			t.Fatalf("unexpected finding %s: %+v", kind, finding)
		}
	}
}
