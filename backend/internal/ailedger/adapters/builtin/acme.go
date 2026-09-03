package builtin

import (
	"context"
	"time"

	"github.com/themisto/backend/internal/ailedger"
)

func AcmeAI() ailedger.ConnectorAdapter {
	return adapter{
		descriptor: ailedger.ConnectorDescriptor{
			AdapterKey: "acme_ai", DisplayName: "Acme AI (test adapter)",
			Capabilities: []ailedger.Capability{
				ailedger.CapabilityProducts, ailedger.CapabilityDirectory,
				ailedger.CapabilityLicenses, ailedger.CapabilityUsage, ailedger.CapabilityCosts,
			},
			CredentialSchema: map[string]ailedger.FieldSchema{},
			ConfigSchema: map[string]ailedger.FieldSchema{
				"normalized_facts": {Type: "object", Label: "Test facts"},
			},
			AllowedHosts: []string{"api.acme.invalid"},
		},
		sync: func(ctx context.Context, checkpoint ailedger.Checkpoint, sink ailedger.FactSink) (ailedger.Checkpoint, error) {
			material, _ := ailedger.ConnectorMaterialFromContext(ctx)
			if document, ok, err := decodeConfiguredDocument(material.Config); err != nil {
				return checkpoint, err
			} else if ok {
				if err := emitDocument(document, sink); err != nil {
					return checkpoint, err
				}
				return ailedger.Checkpoint{"synced_at": time.Now().UTC().Format(time.RFC3339Nano)}, nil
			}
			now := time.Now().UTC()
			start := now.Add(-24 * time.Hour)
			base := ailedger.Provenance{
				SourceEvidenceLevel:  ailedger.SourceAuthoritative,
				ReconciliationStatus: ailedger.ReconciliationUnmatched,
				SourceKey:            "acme-product", FreshnessAt: now, Scope: "organization",
				VendorKey: "acme", ProductKey: "assistant",
			}
			if err := sink.UpsertProduct(ailedger.ProductFact{
				Provenance: base, DisplayName: "Acme Assistant",
				FunctionalCategory: "general_assistant", HasContract: true, HasBillingSource: true,
			}); err != nil {
				return checkpoint, err
			}
			metricBase := base
			metricBase.SourceKey = "acme-requests"
			metricBase.ReportingPeriodStart, metricBase.ReportingPeriodEnd = period(start, now)
			if err := sink.UpsertMetric(ailedger.MetricFact{
				Provenance: metricBase, MetricKey: "requests", Value: "7", Unit: "requests", IsActivity: true,
			}); err != nil {
				return checkpoint, err
			}
			costBase := metricBase
			costBase.SourceKey = "acme-cost"
			if err := sink.UpsertCost(ailedger.CostFact{
				Provenance: costBase, CostKey: "daily-total", Amount: "12.34", Currency: "USD", CostKind: "charge",
			}); err != nil {
				return checkpoint, err
			}
			return ailedger.Checkpoint{"synced_at": now.Format(time.RFC3339Nano)}, nil
		},
	}
}
