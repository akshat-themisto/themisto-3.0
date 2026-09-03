//go:build darwin

package darwin

import (
	"testing"

	"github.com/themisto/agent/core/aiactivity"
	"github.com/themisto/agent/core/domain"
)

func TestMatchProcessObservationsUsesCatalog(t *testing.T) {
	catalog := []domain.AIProductCatalogEntry{{
		VendorKey: "acme", ProductKey: "agent", ProcessNames: []string{"acme-agent"}, Surfaces: []string{"coding_agent"},
	}}
	got := matchProcessObservations(catalog, []processSnapshot{{pid: 42, name: "acme-agent"}})
	if len(got) != 1 || got[0].VendorKey != "acme" || got[0].ProductKey != "agent" {
		t.Fatalf("unexpected observations: %#v", got)
	}
}

func TestMatchMCPObservationsDoesNotExposeServerDetails(t *testing.T) {
	catalog := []domain.AIProductCatalogEntry{{
		VendorKey: "acme", ProductKey: "client", MCPConfigKeys: []string{"acme_client"},
	}}
	got := matchMCPObservations(catalog, []aiactivity.MCPObservation{{
		ClientKey: "acme_client", Configured: true, ServerCount: 2, TransportKinds: []string{"stdio"},
	}})
	if len(got) != 1 || got[0].Count != 2 || got[0].SourceApplication != "acme_client" {
		t.Fatalf("unexpected MCP observations: %#v", got)
	}
}

func TestProcessMatchPrefersBundleOrExecutableName(t *testing.T) {
	product := domain.AIProductCatalogEntry{ProcessNames: []string{"agent"}, BundleIDs: []string{"com.acme.agent"}}
	if !matchesProcess(product, processSnapshot{name: "helper", bundleID: "com.acme.agent"}) {
		t.Fatal("bundle identifier should match")
	}
	if !matchesProcess(product, processSnapshot{name: "agent"}) {
		t.Fatal("executable name should match")
	}
}
