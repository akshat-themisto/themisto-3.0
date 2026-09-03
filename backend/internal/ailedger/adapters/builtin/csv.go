package builtin

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/themisto/backend/internal/ailedger"
)

const CSVSchemaVersion = "themisto_ai_ledger_csv_v1"

func CSV() ailedger.ConnectorAdapter {
	return adapter{
		descriptor: ailedger.ConnectorDescriptor{
			AdapterKey: "csv_v1", DisplayName: "Versioned CSV import",
			Capabilities: []ailedger.Capability{
				ailedger.CapabilityProducts, ailedger.CapabilityDirectory,
				ailedger.CapabilityLicenses, ailedger.CapabilityUsage, ailedger.CapabilityCosts,
			},
			CredentialSchema: map[string]ailedger.FieldSchema{},
			ConfigSchema: map[string]ailedger.FieldSchema{
				"csv_data": {Type: "string", Label: "CSV data", Required: true},
			},
			AllowedHosts: []string{"offline.invalid"},
		},
		validate: func(_ ailedger.Credentials, config ailedger.ConnectorConfig) error {
			raw, _ := config["csv_data"].(string)
			_, err := ValidateCSV(raw)
			return err
		},
		sync: syncCSV,
	}
}

type CSVValidation struct {
	SchemaVersion string   `json:"schema_version"`
	RecordCount   int      `json:"record_count"`
	Errors        []string `json:"errors"`
}

func ValidateCSV(raw string) (CSVValidation, error) {
	rows, err := parseCSV(raw)
	if err != nil {
		return CSVValidation{}, err
	}
	result := CSVValidation{SchemaVersion: CSVSchemaVersion}
	if len(rows) < 2 {
		return result, fmt.Errorf("CSV must contain a header and at least one record")
	}
	want := []string{
		"schema_version", "record_type", "source_key", "vendor_key", "product_key",
		"display_name", "functional_category", "identity_source_key", "identity_kind",
		"external_id", "email", "license_status", "paid", "metric_key", "metric_value",
		"unit", "is_activity", "amount", "currency", "cost_kind", "project_key",
		"period_start", "period_end", "freshness_at", "scope",
	}
	if len(rows[0]) != len(want) {
		return result, fmt.Errorf("CSV header has %d fields; expected %d", len(rows[0]), len(want))
	}
	for i := range want {
		if strings.TrimSpace(rows[0][i]) != want[i] {
			return result, fmt.Errorf("CSV header field %d is %q; expected %q", i+1, rows[0][i], want[i])
		}
	}
	for i, row := range rows[1:] {
		if len(row) != len(want) {
			result.Errors = append(result.Errors, fmt.Sprintf("row %d has %d fields", i+2, len(row)))
			continue
		}
		if row[0] != CSVSchemaVersion {
			result.Errors = append(result.Errors, fmt.Sprintf("row %d has unsupported schema_version", i+2))
		}
		switch row[1] {
		case "product", "identity", "license", "metric", "cost":
		default:
			result.Errors = append(result.Errors, fmt.Sprintf("row %d has invalid record_type", i+2))
		}
	}
	result.RecordCount = len(rows) - 1
	if len(result.Errors) > 0 {
		return result, fmt.Errorf("CSV validation failed")
	}
	return result, nil
}

func syncCSV(ctx context.Context, checkpoint ailedger.Checkpoint, sink ailedger.FactSink) (ailedger.Checkpoint, error) {
	material, _ := ailedger.ConnectorMaterialFromContext(ctx)
	raw, _ := material.Config["csv_data"].(string)
	if _, err := ValidateCSV(raw); err != nil {
		return checkpoint, err
	}
	rows, _ := parseCSV(raw)
	for _, row := range rows[1:] {
		start, err := time.Parse(time.RFC3339, row[21])
		if err != nil {
			return checkpoint, fmt.Errorf("source %s period_start: %w", row[2], err)
		}
		end, err := time.Parse(time.RFC3339, row[22])
		if err != nil {
			return checkpoint, fmt.Errorf("source %s period_end: %w", row[2], err)
		}
		freshness, err := time.Parse(time.RFC3339, row[23])
		if err != nil {
			return checkpoint, fmt.Errorf("source %s freshness_at: %w", row[2], err)
		}
		provenance := ailedger.Provenance{
			OriginKind: ailedger.OriginImport, SourceEvidenceLevel: ailedger.SourceAuthoritative,
			ReconciliationStatus: ailedger.ReconciliationUnmatched,
			SourceKey:            row[2], FreshnessAt: freshness, Scope: row[24],
			ReportingPeriodStart: &start, ReportingPeriodEnd: &end,
			VendorKey: row[3], ProductKey: row[4],
		}
		switch row[1] {
		case "product":
			err = sink.UpsertProduct(ailedger.ProductFact{Provenance: provenance, DisplayName: row[5], FunctionalCategory: row[6]})
		case "identity":
			err = sink.UpsertIdentity(ailedger.ExternalIdentityFact{
				Provenance: provenance, IdentitySourceKey: row[7], IdentityKind: row[8],
				ExternalID: row[9], NormalizedEmail: row[10], ProjectKey: row[20],
			})
		case "license":
			err = sink.UpsertLicense(ailedger.LicenseFact{
				Provenance: provenance, LicenseKey: row[2], ExternalIdentityID: row[9],
				Status: row[11], Paid: strings.EqualFold(row[12], "true"),
			})
		case "metric":
			err = sink.UpsertMetric(ailedger.MetricFact{
				Provenance: provenance, MetricKey: row[13], Value: row[14], Unit: row[15],
				IsActivity: strings.EqualFold(row[16], "true"), ExternalIdentityID: row[9], ProjectKey: row[20],
			})
		case "cost":
			err = sink.UpsertCost(ailedger.CostFact{
				Provenance: provenance, CostKey: row[2], Amount: row[17], Currency: row[18],
				CostKind: row[19], ExternalIdentityID: row[9], ProjectKey: row[20],
			})
		}
		if err != nil {
			return checkpoint, err
		}
	}
	return ailedger.Checkpoint{"records": len(rows) - 1, "committed_at": time.Now().UTC().Format(time.RFC3339Nano)}, nil
}
