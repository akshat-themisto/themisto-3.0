package handler

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/themisto/gateway/internal/telemetry"
)

var aiActivitySafeIdentifier = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:@/-]{0,254}$`)

var aiActivityAllowedFields = map[string]struct{}{
	"vendor_key": {}, "product_key": {}, "surface": {}, "activity_kind": {},
	"observed_at": {}, "source_application": {}, "model_identifier": {},
	"project_identifier": {}, "opaque_session_hash": {}, "count": {},
	"origin_kind": {}, "source_evidence_level": {}, "reconciliation_status": {},
	"evidence_level": {}, "source_identifier": {}, "source_key": {},
	"freshness_at": {}, "scope": {},
}

func decodeAIActivity(data map[string]interface{}, envelopeTime time.Time, deviceID, orgID string) (telemetry.AIActivityEvent, error) {
	for key := range data {
		if _, ok := aiActivityAllowedFields[key]; !ok {
			return telemetry.AIActivityEvent{}, fmt.Errorf("field %q is not permitted", key)
		}
	}
	required := func(key string) (string, error) {
		value, ok := asString(data[key])
		value = strings.TrimSpace(value)
		if !ok || value == "" || !aiActivitySafeIdentifier.MatchString(value) {
			return "", fmt.Errorf("%s is missing or invalid", key)
		}
		return value, nil
	}
	vendor, err := required("vendor_key")
	if err != nil {
		return telemetry.AIActivityEvent{}, err
	}
	product, err := required("product_key")
	if err != nil {
		return telemetry.AIActivityEvent{}, err
	}
	surface, err := required("surface")
	if err != nil {
		return telemetry.AIActivityEvent{}, err
	}
	kind, err := required("activity_kind")
	if err != nil {
		return telemetry.AIActivityEvent{}, err
	}
	sourceKey, err := required("source_key")
	if err != nil {
		return telemetry.AIActivityEvent{}, err
	}
	if value, _ := asString(data["origin_kind"]); value != "endpoint" {
		return telemetry.AIActivityEvent{}, fmt.Errorf("origin_kind must be endpoint")
	}
	if value, _ := asString(data["source_evidence_level"]); value != "observed" {
		return telemetry.AIActivityEvent{}, fmt.Errorf("source evidence must be observed")
	}
	if value, _ := asString(data["evidence_level"]); value != "observed" {
		return telemetry.AIActivityEvent{}, fmt.Errorf("effective evidence must be observed")
	}
	if value, _ := asString(data["reconciliation_status"]); value != "not_applicable" {
		return telemetry.AIActivityEvent{}, fmt.Errorf("new endpoint activity reconciliation must be not_applicable")
	}
	count, ok := asInt64(data["count"])
	if !ok || count < 1 || count > 1000000 {
		return telemetry.AIActivityEvent{}, fmt.Errorf("count is invalid")
	}
	observedAt := parseActivityTime(data["observed_at"], envelopeTime)
	freshnessAt := parseActivityTime(data["freshness_at"], observedAt)
	sourceApplication, _ := asString(data["source_application"])
	if strings.ContainsAny(sourceApplication, `/\`) || len(sourceApplication) > 128 {
		return telemetry.AIActivityEvent{}, fmt.Errorf("source_application contains a local path or is too long")
	}
	model, _ := asString(data["model_identifier"])
	project, _ := asString(data["project_identifier"])
	sessionHash, _ := asString(data["opaque_session_hash"])
	for key, value := range map[string]string{"model_identifier": model, "project_identifier": project, "opaque_session_hash": sessionHash} {
		if value != "" && !aiActivitySafeIdentifier.MatchString(value) {
			return telemetry.AIActivityEvent{}, fmt.Errorf("%s is not a safe identifier", key)
		}
	}
	return telemetry.AIActivityEvent{
		Timestamp:         observedAt,
		DeviceID:          deviceID,
		OrgID:             orgID,
		VendorKey:         strings.ToLower(vendor),
		ProductKey:        strings.ToLower(product),
		Surface:           strings.ToLower(surface),
		ActivityKind:      strings.ToLower(kind),
		SourceApplication: sourceApplication,
		ModelIdentifier:   model,
		ProjectIdentifier: project,
		OpaqueSessionHash: sessionHash,
		Count:             count,
		SourceKey:         sourceKey,
		FreshnessAt:       freshnessAt,
	}, nil
}

func parseActivityTime(raw interface{}, fallback time.Time) time.Time {
	value, _ := raw.(string)
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed.UTC()
	}
	if fallback.IsZero() {
		return time.Now().UTC()
	}
	return fallback.UTC()
}
