package ailedger

import (
	"testing"
	"time"
)

func TestReconciliationExternalIDFirstSupportsRename(t *testing.T) {
	users := []DirectoryUser{{ID: "u1", IdentitySourceKey: "directory", ExternalUserID: "stable-1", NormalizedEmail: "new@example.com"}}
	matches := ReconcileIdentities(users, []IdentityCandidate{{
		ID: "i1", IdentitySourceKey: "directory", ExternalID: "stable-1", NormalizedEmail: "old@example.com",
	}})
	if len(matches) != 1 || matches[0].DirectoryUserID != "u1" || matches[0].MatchMethod != "external_id" {
		t.Fatalf("unexpected match: %+v", matches)
	}
}

func TestReconciliationAmbiguousEmailRemainsUnmatched(t *testing.T) {
	users := []DirectoryUser{
		{ID: "u1", NormalizedEmail: "shared@example.com"},
		{ID: "u2", NormalizedEmail: "shared@example.com"},
	}
	matches := ReconcileIdentities(users, []IdentityCandidate{{ID: "i1", NormalizedEmail: "SHARED@example.com"}})
	if matches[0].Status != ReconciliationUnmatched || matches[0].DirectoryUserID != "" {
		t.Fatalf("ambiguous identity matched: %+v", matches[0])
	}
}

func TestSharedDeviceIsNotInferredToAUser(t *testing.T) {
	now := time.Now()
	assignments := []DeviceUserAssignment{
		{DeviceID: "d1", DirectoryUserID: "u1", AssignedFrom: now.Add(-time.Hour)},
		{DeviceID: "d1", DirectoryUserID: "u2", AssignedFrom: now.Add(-time.Hour)},
	}
	if user, ok := ResolveDeviceUser(assignments, "d1", now); ok || user != "" {
		t.Fatalf("shared device was inferred to %q", user)
	}
}
