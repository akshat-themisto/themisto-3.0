package ailedger

import (
	"sort"
	"strings"
	"time"
)

type DirectoryUser struct {
	ID                string
	IdentitySourceKey string
	ExternalUserID    string
	NormalizedEmail   string
	Status            string
}

type IdentityCandidate struct {
	ID                string
	IdentitySourceKey string
	IdentityKind      string
	ExternalID        string
	NormalizedEmail   string
	ProjectKey        string
}

type IdentityMatch struct {
	IdentityID      string
	DirectoryUserID string
	Status          ReconciliationStatus
	MatchMethod     string
}

// ReconcileIdentities applies external-ID-first matching and exact normalized
// email fallback. Ambiguity is never resolved heuristically.
func ReconcileIdentities(users []DirectoryUser, identities []IdentityCandidate) []IdentityMatch {
	byExternalID := make(map[string][]DirectoryUser)
	byEmail := make(map[string][]DirectoryUser)
	for _, user := range users {
		externalKey := normalizeKey(user.IdentitySourceKey) + "\x00" + strings.TrimSpace(user.ExternalUserID)
		if user.ExternalUserID != "" {
			byExternalID[externalKey] = append(byExternalID[externalKey], user)
		}
		if email := NormalizeEmail(user.NormalizedEmail); email != "" {
			byEmail[email] = append(byEmail[email], user)
		}
	}
	out := make([]IdentityMatch, 0, len(identities))
	for _, identity := range identities {
		match := IdentityMatch{IdentityID: identity.ID, Status: ReconciliationUnmatched}
		externalKey := normalizeKey(identity.IdentitySourceKey) + "\x00" + strings.TrimSpace(identity.ExternalID)
		if candidates := byExternalID[externalKey]; len(candidates) == 1 {
			match.DirectoryUserID = candidates[0].ID
			match.Status = ReconciliationMatched
			match.MatchMethod = "external_id"
			out = append(out, match)
			continue
		}
		if email := NormalizeEmail(identity.NormalizedEmail); email != "" {
			if candidates := byEmail[email]; len(candidates) == 1 {
				match.DirectoryUserID = candidates[0].ID
				match.Status = ReconciliationMatched
				match.MatchMethod = "normalized_email"
			}
		}
		out = append(out, match)
	}
	return out
}

type DeviceUserAssignment struct {
	DeviceID        string
	DirectoryUserID string
	AssignedFrom    time.Time
	AssignedUntil   *time.Time
}

// ResolveDeviceUser returns a user only when exactly one explicit assignment
// covers the observation. Shared/overlapping assignments remain unassigned.
func ResolveDeviceUser(assignments []DeviceUserAssignment, deviceID string, observedAt time.Time) (string, bool) {
	set := map[string]struct{}{}
	for _, assignment := range assignments {
		if assignment.DeviceID != deviceID || observedAt.Before(assignment.AssignedFrom) {
			continue
		}
		if assignment.AssignedUntil != nil && !observedAt.Before(*assignment.AssignedUntil) {
			continue
		}
		set[assignment.DirectoryUserID] = struct{}{}
	}
	if len(set) != 1 {
		return "", false
	}
	for userID := range set {
		return userID, true
	}
	return "", false
}

func SortIdentityMatches(matches []IdentityMatch) {
	sort.Slice(matches, func(i, j int) bool { return matches[i].IdentityID < matches[j].IdentityID })
}
