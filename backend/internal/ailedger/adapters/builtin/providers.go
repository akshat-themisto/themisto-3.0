package builtin

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/themisto/backend/internal/ailedger"
)

// Provider adapters own their API hosts, credentials, pagination checkpoints,
// and raw-to-normalized mappings. The manager and ledger remain unaware of
// these keys.
func OpenAI() ailedger.ConnectorAdapter {
	return normalizedProviderAdapter(
		"openai", "OpenAI API usage and costs",
		[]ailedger.Capability{ailedger.CapabilityProducts, ailedger.CapabilityUsage, ailedger.CapabilityCosts},
		[]string{"api.openai.com"},
		"https://api.openai.com/v1/organization/usage/completions",
	)
}

func Anthropic() ailedger.ConnectorAdapter {
	return normalizedProviderAdapter(
		"anthropic", "Anthropic Claude usage and costs",
		[]ailedger.Capability{ailedger.CapabilityProducts, ailedger.CapabilityUsage, ailedger.CapabilityCosts},
		[]string{"api.anthropic.com"},
		"https://api.anthropic.com/v1/organizations/usage_report/messages",
	)
}

func GoogleWorkspace() ailedger.ConnectorAdapter {
	return normalizedProviderAdapter(
		"google_workspace", "Google Workspace directory, licensing and usage reports",
		[]ailedger.Capability{ailedger.CapabilityDirectory, ailedger.CapabilityLicenses, ailedger.CapabilityUsage},
		[]string{"admin.googleapis.com", "licensing.googleapis.com"},
		"https://admin.googleapis.com/admin/reports/v1/usage/users/all/dates/latest",
	)
}

func GoogleCloudBilling() ailedger.ConnectorAdapter {
	return normalizedProviderAdapter(
		"google_cloud_billing", "Google Cloud billing export",
		[]ailedger.Capability{ailedger.CapabilityProducts, ailedger.CapabilityCosts},
		[]string{"bigquery.googleapis.com", "cloudbilling.googleapis.com"},
		"https://bigquery.googleapis.com/bigquery/v2/projects",
	)
}

func normalizedProviderAdapter(key, name string, capabilities []ailedger.Capability, hosts []string, defaultEndpoint string) ailedger.ConnectorAdapter {
	return adapter{
		descriptor: ailedger.ConnectorDescriptor{
			AdapterKey: key, DisplayName: name, Capabilities: capabilities,
			CredentialSchema: map[string]ailedger.FieldSchema{
				"access_token": {Type: "string", Label: "Access token", Required: true, Secret: true},
			},
			ConfigSchema: map[string]ailedger.FieldSchema{
				"report_url":       {Type: "string", Label: "Report URL", Description: "Optional adapter report override"},
				"normalized_facts": {Type: "object", Label: "Normalized fallback", Description: "Versioned offline/provider export facts"},
			},
			AllowedHosts: hosts,
		},
		validate: func(credentials ailedger.Credentials, config ailedger.ConnectorConfig) error {
			if _, ok := config["normalized_facts"]; ok {
				return nil
			}
			_, err := requireString(credentials, "access_token")
			return err
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
			token, err := requireString(material.Credentials, "access_token")
			if err != nil {
				return checkpoint, err
			}
			target := defaultEndpoint
			if configured, _ := material.Config["report_url"].(string); strings.TrimSpace(configured) != "" {
				target = strings.TrimSpace(configured)
			}
			if page, _ := checkpoint["next_page"].(string); page != "" {
				separator := "?"
				if strings.Contains(target, "?") {
					separator = "&"
				}
				target += separator + "page=" + page
			}
			response, err := authenticatedGET(ctx, target, "Authorization", "Bearer "+token)
			if err != nil {
				return checkpoint, err
			}
			var document normalizedFactDocument
			if err := decodeResponse(response, &document); err != nil {
				return checkpoint, fmt.Errorf("%s report: %w", key, err)
			}
			if err := emitDocument(document, sink); err != nil {
				return checkpoint, err
			}
			next := ailedger.Checkpoint{"synced_at": time.Now().UTC().Format(time.RFC3339Nano)}
			if document.NextPage != "" {
				next["next_page"] = document.NextPage
			}
			return next, nil
		},
	}
}

func All() []ailedger.ConnectorAdapter {
	return []ailedger.ConnectorAdapter{
		OpenAI(), Anthropic(), GoogleWorkspace(), GoogleCloudBilling(), CSV(), AcmeAI(),
	}
}
