// Package aiactivity defines the privacy boundary for canonical endpoint AI
// observations. Connector facts never use this event contract.
package aiactivity

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

const EventName = "ai.activity.v1"

var safeIdentifier = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:@/-]{0,254}$`)

// Event contains only centrally permitted endpoint metadata. It deliberately
// has no prompt, body, path, payload, arguments, token, or file fields.
type Event struct {
	VendorKey            string    `json:"vendor_key"`
	ProductKey           string    `json:"product_key"`
	Surface              string    `json:"surface"`
	ActivityKind         string    `json:"activity_kind"`
	ObservedAt           time.Time `json:"observed_at"`
	SourceApplication    string    `json:"source_application,omitempty"`
	ModelIdentifier      string    `json:"model_identifier,omitempty"`
	ProjectIdentifier    string    `json:"project_identifier,omitempty"`
	OpaqueSessionHash    string    `json:"opaque_session_hash,omitempty"`
	Count                int64     `json:"count"`
	OriginKind           string    `json:"origin_kind"`
	SourceEvidenceLevel  string    `json:"source_evidence_level"`
	ReconciliationStatus string    `json:"reconciliation_status"`
	EvidenceLevel        string    `json:"evidence_level"`
	SourceIdentifier     string    `json:"source_identifier"`
	SourceKey            string    `json:"source_key"`
	FreshnessAt          time.Time `json:"freshness_at"`
	Scope                string    `json:"scope"`
}

// NewRequestEvent constructs an observed endpoint event. sourceSeed is hashed
// before leaving the endpoint and must not itself be sent to the gateway.
func NewRequestEvent(vendorKey, productKey, surface, sourceApplication, sourceIdentifier, sourceSeed string, at time.Time) Event {
	return NewObservedEvent(vendorKey, productKey, surface, "request", sourceApplication, sourceIdentifier, sourceSeed, at)
}

// NewObservedEvent constructs an aggregate endpoint observation without
// accepting any content-bearing value. The caller may select a safe activity
// kind such as process_observed or mcp_configured.
func NewObservedEvent(vendorKey, productKey, surface, activityKind, sourceApplication, sourceIdentifier, sourceSeed string, at time.Time) Event {
	if at.IsZero() {
		at = time.Now().UTC()
	}
	sessionHash := OpaqueHash(sourceSeed)
	return Event{
		VendorKey:            normalize(vendorKey),
		ProductKey:           normalize(productKey),
		Surface:              normalize(surface),
		ActivityKind:         normalize(activityKind),
		ObservedAt:           at.UTC(),
		SourceApplication:    safeApplicationName(sourceApplication),
		OpaqueSessionHash:    sessionHash,
		Count:                1,
		OriginKind:           "endpoint",
		SourceEvidenceLevel:  "observed",
		ReconciliationStatus: "not_applicable",
		EvidenceLevel:        "observed",
		SourceIdentifier:     strings.TrimSpace(sourceIdentifier),
		SourceKey:            sessionHash,
		FreshnessAt:          at.UTC(),
		Scope:                "device",
	}
}

// OpaqueHash returns a one-way stable hash suitable for session correlation.
func OpaqueHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func (e Event) Validate() error {
	var errs []error
	if e.VendorKey == "" || !safeIdentifier.MatchString(e.VendorKey) {
		errs = append(errs, errors.New("vendor_key is required and must be a safe identifier"))
	}
	if e.ProductKey == "" || !safeIdentifier.MatchString(e.ProductKey) {
		errs = append(errs, errors.New("product_key is required and must be a safe identifier"))
	}
	if e.Surface == "" || !safeIdentifier.MatchString(e.Surface) {
		errs = append(errs, errors.New("surface is required and must be a safe identifier"))
	}
	if e.ActivityKind == "" || !safeIdentifier.MatchString(e.ActivityKind) {
		errs = append(errs, errors.New("activity_kind is required and must be a safe identifier"))
	}
	if e.ObservedAt.IsZero() || e.FreshnessAt.IsZero() {
		errs = append(errs, errors.New("observed_at and freshness_at are required"))
	}
	if e.Count < 1 {
		errs = append(errs, errors.New("count must be positive"))
	}
	if e.OriginKind != "endpoint" || e.SourceEvidenceLevel != "observed" || e.EvidenceLevel != "observed" {
		errs = append(errs, errors.New("endpoint activity provenance must remain observed"))
	}
	if e.ReconciliationStatus != "not_applicable" {
		errs = append(errs, errors.New("new endpoint activity must use not_applicable reconciliation"))
	}
	if strings.ContainsAny(e.SourceApplication, `/\`) {
		errs = append(errs, errors.New("source_application must not contain a local path"))
	}
	if e.OpaqueSessionHash != "" && len(e.OpaqueSessionHash) != sha256.Size*2 {
		errs = append(errs, errors.New("opaque_session_hash must be sha256 hex"))
	}
	for name, value := range map[string]string{
		"model_identifier":   e.ModelIdentifier,
		"project_identifier": e.ProjectIdentifier,
		"source_identifier":  e.SourceIdentifier,
		"source_key":         e.SourceKey,
	} {
		if value != "" && !safeIdentifier.MatchString(value) {
			errs = append(errs, fmt.Errorf("%s must be a privacy-safe identifier", name))
		}
	}
	return errors.Join(errs...)
}

// Data returns the exact allowlisted map used by the existing telemetry
// collector envelope.
func (e Event) Data() map[string]interface{} {
	data := map[string]interface{}{
		"vendor_key":            e.VendorKey,
		"product_key":           e.ProductKey,
		"surface":               e.Surface,
		"activity_kind":         e.ActivityKind,
		"observed_at":           e.ObservedAt,
		"count":                 e.Count,
		"origin_kind":           e.OriginKind,
		"source_evidence_level": e.SourceEvidenceLevel,
		"reconciliation_status": e.ReconciliationStatus,
		"evidence_level":        e.EvidenceLevel,
		"source_identifier":     e.SourceIdentifier,
		"source_key":            e.SourceKey,
		"freshness_at":          e.FreshnessAt,
		"scope":                 e.Scope,
	}
	if e.SourceApplication != "" {
		data["source_application"] = e.SourceApplication
	}
	if e.ModelIdentifier != "" {
		data["model_identifier"] = e.ModelIdentifier
	}
	if e.ProjectIdentifier != "" {
		data["project_identifier"] = e.ProjectIdentifier
	}
	if e.OpaqueSessionHash != "" {
		data["opaque_session_hash"] = e.OpaqueSessionHash
	}
	return data
}

var allowedPayloadFields = map[string]struct{}{
	"vendor_key": {}, "product_key": {}, "surface": {}, "activity_kind": {},
	"observed_at": {}, "source_application": {}, "model_identifier": {},
	"project_identifier": {}, "opaque_session_hash": {}, "count": {},
	"origin_kind": {}, "source_evidence_level": {}, "reconciliation_status": {},
	"evidence_level": {}, "source_identifier": {}, "source_key": {},
	"freshness_at": {}, "scope": {},
}

// ValidatePayloadKeys is used at every serialization/ingestion boundary. A
// strict allowlist prevents new content-bearing fields from silently entering
// telemetry or exports.
func ValidatePayloadKeys(data map[string]interface{}) error {
	for key := range data {
		if _, ok := allowedPayloadFields[key]; !ok {
			return fmt.Errorf("ai activity field %q is not permitted", key)
		}
	}
	return nil
}

func normalize(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func safeApplicationName(value string) string {
	value = strings.TrimSpace(value)
	if strings.ContainsAny(value, `/\`) || len(value) > 128 {
		return ""
	}
	return value
}
