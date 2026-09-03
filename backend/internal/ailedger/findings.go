package ailedger

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	FindingInactivePaidSeat             = "inactive_paid_seat"
	FindingOrphanedSeat                 = "orphaned_seat"
	FindingShadowAI                     = "shadow_ai"
	FindingUnpricedAI                   = "unpriced_ai"
	FindingOverlappingSeats             = "overlapping_seats"
	FindingSpendSpike                   = "spend_spike"
	FindingUnattributedSpend            = "unattributed_spend"
	FindingConnectorActivityCoverageGap = "connector_activity_coverage_gap"
)

type FindingProduct struct {
	VendorKey          string
	ProductKey         string
	DisplayName        string
	FunctionalCategory string
	Approved           bool
	HasContract        bool
	HasBillingSource   bool
}

type FindingSeat struct {
	LicenseKey      string
	VendorKey       string
	ProductKey      string
	DirectoryUserID string
	Paid            bool
	Status          string
	SourceKey       string
}

type FindingActivity struct {
	VendorKey       string
	ProductKey      string
	DirectoryUserID string
	ProjectKey      string
	OriginKind      OriginKind
	IsActivity      bool
	ObservedAt      time.Time
	SourceKey       string
}

type FindingCost struct {
	VendorKey       string
	ProductKey      string
	DirectoryUserID string
	ProjectKey      string
	TeamKey         string
	Amount          float64
	Currency        string
	Day             time.Time
	SourceEvidence  SourceEvidenceLevel
	SourceKey       string
}

type FindingSnapshot struct {
	Products   []FindingProduct
	Users      []DirectoryUser
	Seats      []FindingSeat
	Activities []FindingActivity
	Costs      []FindingCost
}

type Finding struct {
	Key                string   `json:"finding_key"`
	Type               string   `json:"finding_type"`
	Severity           string   `json:"severity"`
	Title              string   `json:"title"`
	Summary            string   `json:"summary"`
	Recommendation     string   `json:"recommendation"`
	VendorKey          string   `json:"vendor_key,omitempty"`
	ProductKey         string   `json:"product_key,omitempty"`
	DirectoryUserID    string   `json:"directory_user_id,omitempty"`
	Currency           string   `json:"currency,omitempty"`
	Amount             float64  `json:"amount,omitempty"`
	EvidenceSourceKeys []string `json:"evidence_source_keys"`
}

// EvaluateFindings is deterministic and insights-only. It never mutates
// licenses, vendors, procurement state, or endpoint policy.
func EvaluateFindings(snapshot FindingSnapshot, now time.Time) []Finding {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	products := make(map[string]FindingProduct, len(snapshot.Products))
	for _, product := range snapshot.Products {
		products[productKey(product.VendorKey, product.ProductKey)] = product
	}
	users := make(map[string]DirectoryUser, len(snapshot.Users))
	for _, user := range snapshot.Users {
		users[user.ID] = user
	}
	activeSeats := map[string]FindingSeat{}
	for _, seat := range snapshot.Seats {
		if seat.Paid && seat.Status == "assigned" {
			activeSeats[seat.LicenseKey] = seat
		}
	}

	var out []Finding
	recent := now.Add(-30 * 24 * time.Hour)
	hasUsage := func(userID, vendor, product string) bool {
		for _, activity := range snapshot.Activities {
			if !activity.IsActivity || activity.ObservedAt.Before(recent) {
				continue
			}
			if activity.DirectoryUserID == userID && activity.VendorKey == vendor && activity.ProductKey == product {
				return true
			}
		}
		return false
	}

	for _, seat := range activeSeats {
		user := users[seat.DirectoryUserID]
		if user.Status == "suspended" || user.Status == "deleted" {
			out = append(out, Finding{
				Key:  stableFindingKey(FindingOrphanedSeat, seat.LicenseKey),
				Type: FindingOrphanedSeat, Severity: "important", Title: "Orphaned paid AI seat",
				Summary:        "A paid seat is assigned to a suspended or deleted directory user.",
				Recommendation: "Review the assignment and reclaim the seat if it is no longer required.",
				VendorKey:      seat.VendorKey, ProductKey: seat.ProductKey, DirectoryUserID: seat.DirectoryUserID,
				EvidenceSourceKeys: cleanEvidence(seat.SourceKey),
			})
		}
		if !hasUsage(seat.DirectoryUserID, seat.VendorKey, seat.ProductKey) {
			out = append(out, Finding{
				Key:  stableFindingKey(FindingInactivePaidSeat, seat.LicenseKey),
				Type: FindingInactivePaidSeat, Severity: "review", Title: "Inactive paid AI seat",
				Summary:        "No endpoint or non-cost connector activity was recorded for this assigned paid seat in 30 days.",
				Recommendation: "Confirm business need before changing or removing the license.",
				VendorKey:      seat.VendorKey, ProductKey: seat.ProductKey, DirectoryUserID: seat.DirectoryUserID,
				EvidenceSourceKeys: cleanEvidence(seat.SourceKey),
			})
		}
	}

	seenProductFinding := map[string]bool{}
	for _, activity := range snapshot.Activities {
		if activity.OriginKind != OriginEndpoint || !activity.IsActivity {
			continue
		}
		key := productKey(activity.VendorKey, activity.ProductKey)
		product := products[key]
		if !product.Approved && !seenProductFinding[FindingShadowAI+key] {
			seenProductFinding[FindingShadowAI+key] = true
			out = append(out, Finding{
				Key: stableFindingKey(FindingShadowAI, key), Type: FindingShadowAI,
				Severity: "important", Title: "Unapproved AI product observed",
				Summary:        "Endpoint activity confirms use of a product absent from the approved catalog.",
				Recommendation: "Review the product for approval, consolidation, or employee guidance.",
				VendorKey:      activity.VendorKey, ProductKey: activity.ProductKey,
				EvidenceSourceKeys: cleanEvidence(activity.SourceKey),
			})
		}
		hasLicense := false
		for _, seat := range snapshot.Seats {
			if seat.VendorKey == activity.VendorKey && seat.ProductKey == activity.ProductKey {
				hasLicense = true
				break
			}
		}
		if !product.HasContract && !product.HasBillingSource && !hasLicense && !seenProductFinding[FindingUnpricedAI+key] {
			seenProductFinding[FindingUnpricedAI+key] = true
			out = append(out, Finding{
				Key: stableFindingKey(FindingUnpricedAI, key), Type: FindingUnpricedAI,
				Severity: "review", Title: "AI usage has no pricing source",
				Summary:        "Endpoint activity is present without a contract, license, or billing source.",
				Recommendation: "Connect or import an authoritative commercial source for cost accountability.",
				VendorKey:      activity.VendorKey, ProductKey: activity.ProductKey,
				EvidenceSourceKeys: cleanEvidence(activity.SourceKey),
			})
		}
	}

	type categoryUse struct {
		products map[string]struct{}
		evidence []string
	}
	overlap := map[string]*categoryUse{}
	for _, activity := range snapshot.Activities {
		if activity.OriginKind != OriginEndpoint || !activity.IsActivity || activity.DirectoryUserID == "" || activity.ObservedAt.Before(recent) {
			continue
		}
		product := products[productKey(activity.VendorKey, activity.ProductKey)]
		category := strings.TrimSpace(product.FunctionalCategory)
		if category == "" || category == "unknown" {
			continue
		}
		hasPaidSeat := false
		for _, seat := range activeSeats {
			if seat.DirectoryUserID == activity.DirectoryUserID && seat.VendorKey == activity.VendorKey && seat.ProductKey == activity.ProductKey {
				hasPaidSeat = true
				break
			}
		}
		if !hasPaidSeat {
			continue
		}
		key := activity.DirectoryUserID + "\x00" + category
		if overlap[key] == nil {
			overlap[key] = &categoryUse{products: map[string]struct{}{}}
		}
		overlap[key].products[productKey(activity.VendorKey, activity.ProductKey)] = struct{}{}
		overlap[key].evidence = append(overlap[key].evidence, activity.SourceKey)
	}
	for key, use := range overlap {
		if len(use.products) < 2 {
			continue
		}
		parts := strings.SplitN(key, "\x00", 2)
		out = append(out, Finding{
			Key: stableFindingKey(FindingOverlappingSeats, key), Type: FindingOverlappingSeats,
			Severity: "review", Title: "Overlapping actively used AI seats",
			Summary:         fmt.Sprintf("The employee actively uses %d paid products in the %s category.", len(use.products), parts[1]),
			Recommendation:  "Review whether all actively used seats are necessary; preserve workflow needs before consolidation.",
			DirectoryUserID: parts[0], EvidenceSourceKeys: cleanEvidence(use.evidence...),
		})
	}

	out = append(out, spendFindings(snapshot.Costs, now)...)
	out = append(out, coverageGapFindings(snapshot.Activities)...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Type == out[j].Type {
			return out[i].Key < out[j].Key
		}
		return out[i].Type < out[j].Type
	})
	return out
}

func spendFindings(costs []FindingCost, now time.Time) []Finding {
	var out []Finding
	type spendGroup struct {
		daily    map[string]float64
		evidence []string
	}
	groups := map[string]*spendGroup{}
	for _, cost := range costs {
		if cost.SourceEvidence == SourceAuthoritative && cost.DirectoryUserID == "" && cost.ProjectKey == "" && cost.TeamKey == "" {
			out = append(out, Finding{
				Key:  stableFindingKey(FindingUnattributedSpend, cost.SourceKey),
				Type: FindingUnattributedSpend, Severity: "important", Title: "Authoritative AI spend is unattributed",
				Summary:        "A source total cannot be matched to a known project, team, or employee.",
				Recommendation: "Review source coverage or project/account mappings without manufacturing a per-user allocation.",
				VendorKey:      cost.VendorKey, ProductKey: cost.ProductKey, Currency: cost.Currency,
				Amount: cost.Amount, EvidenceSourceKeys: cleanEvidence(cost.SourceKey),
			})
		}
		key := productKey(cost.VendorKey, cost.ProductKey) + "\x00" + strings.ToUpper(cost.Currency)
		if groups[key] == nil {
			groups[key] = &spendGroup{daily: map[string]float64{}}
		}
		groups[key].daily[cost.Day.UTC().Format("2006-01-02")] += cost.Amount
		groups[key].evidence = append(groups[key].evidence, cost.SourceKey)
	}
	end := dayStart(now)
	for key, group := range groups {
		var recentTotal, baselineTotal float64
		for i := 1; i <= 35; i++ {
			amount := group.daily[end.AddDate(0, 0, -i).Format("2006-01-02")]
			if i <= 7 {
				recentTotal += amount
			} else {
				baselineTotal += amount
			}
		}
		recentAverage := recentTotal / 7
		baselineAverage := baselineTotal / 28
		if recentAverage <= baselineAverage*1.5 || recentAverage-baselineAverage < 50 {
			continue
		}
		parts := strings.Split(key, "\x00")
		product := strings.SplitN(parts[0], "/", 2)
		out = append(out, Finding{
			Key: stableFindingKey(FindingSpendSpike, key), Type: FindingSpendSpike,
			Severity: "important", Title: "AI spend increased sharply",
			Summary:        "The seven-day daily average exceeds 150% of the preceding 28-day average with at least a 50-unit daily increase.",
			Recommendation: "Review the authoritative billing source and usage drivers before taking commercial action.",
			VendorKey:      product[0], ProductKey: product[1], Currency: parts[1],
			Amount: recentAverage - baselineAverage, EvidenceSourceKeys: cleanEvidence(group.evidence...),
		})
	}
	return out
}

func coverageGapFindings(activities []FindingActivity) []Finding {
	var out []Finding
	for _, connector := range activities {
		if connector.OriginKind != OriginConnector || !connector.IsActivity {
			continue
		}
		matched := false
		for _, endpoint := range activities {
			if endpoint.OriginKind != OriginEndpoint || !endpoint.IsActivity {
				continue
			}
			if endpoint.VendorKey == connector.VendorKey && endpoint.ProductKey == connector.ProductKey &&
				endpoint.DirectoryUserID != "" && endpoint.DirectoryUserID == connector.DirectoryUserID {
				matched = true
				break
			}
		}
		if matched {
			continue
		}
		out = append(out, Finding{
			Key:  stableFindingKey(FindingConnectorActivityCoverageGap, connector.SourceKey),
			Type: FindingConnectorActivityCoverageGap, Severity: "review",
			Title:          "Connector activity lacks endpoint confirmation",
			Summary:        "Authoritative non-cost connector usage has no matching endpoint observation.",
			Recommendation: "Review endpoint coverage, device assignment, and identity mapping. Keep connector activity separate.",
			VendorKey:      connector.VendorKey, ProductKey: connector.ProductKey,
			DirectoryUserID: connector.DirectoryUserID, EvidenceSourceKeys: cleanEvidence(connector.SourceKey),
		})
	}
	return out
}

func productKey(vendor, product string) string {
	return normalizeKey(vendor) + "/" + normalizeKey(product)
}

func stableFindingKey(kind, subject string) string {
	return kind + ":" + strings.TrimSpace(subject)
}

func cleanEvidence(values ...string) []string {
	set := map[string]struct{}{}
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			set[value] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func dayStart(value time.Time) time.Time {
	value = value.UTC()
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
}
