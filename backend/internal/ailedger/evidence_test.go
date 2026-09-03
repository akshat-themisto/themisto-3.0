package ailedger

import (
	"testing"
	"time"
)

func TestEvidenceTransitions(t *testing.T) {
	cases := []struct {
		name           string
		origin         OriginKind
		source         SourceEvidenceLevel
		reconciliation ReconciliationStatus
		want           EvidenceLevel
	}{
		{"endpoint identity match stays observed", OriginEndpoint, SourceObserved, ReconciliationMatched, EvidenceObserved},
		{"unmatched connector stays authoritative", OriginConnector, SourceAuthoritative, ReconciliationUnmatched, EvidenceAuthoritative},
		{"matched connector becomes reconciled", OriginConnector, SourceAuthoritative, ReconciliationMatched, EvidenceReconciled},
		{"estimate never upgrades", OriginConnector, SourceEstimated, ReconciliationMatched, EvidenceEstimated},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := EffectiveEvidence(tc.origin, tc.source, tc.reconciliation); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestEndpointProvenanceCannotUpgrade(t *testing.T) {
	p := Provenance{
		OriginKind: OriginEndpoint, SourceEvidenceLevel: SourceAuthoritative,
		ReconciliationStatus: ReconciliationMatched, SourceIdentifier: "device",
		SourceKey: "event", FreshnessAt: time.Now(), Scope: "device",
		VendorKey: "acme", ProductKey: "assistant",
	}
	if err := p.NormalizeAndValidate(); err == nil {
		t.Fatal("expected invalid endpoint evidence to fail")
	}
}
