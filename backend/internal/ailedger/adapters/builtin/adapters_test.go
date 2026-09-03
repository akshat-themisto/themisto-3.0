package builtin

import (
	"context"
	"testing"

	"github.com/themisto/backend/internal/ailedger"
)

type memorySink struct {
	products int
	metrics  int
	costs    int
}

func (s *memorySink) UpsertProduct(ailedger.ProductFact) error           { s.products++; return nil }
func (s *memorySink) UpsertIdentity(ailedger.ExternalIdentityFact) error { return nil }
func (s *memorySink) UpsertLicense(ailedger.LicenseFact) error           { return nil }
func (s *memorySink) UpsertMetric(ailedger.MetricFact) error             { s.metrics++; return nil }
func (s *memorySink) UpsertCost(ailedger.CostFact) error                 { s.costs++; return nil }

func TestAcmeAdapterProvesRegistryOnlyExtensibility(t *testing.T) {
	registry := ailedger.NewRegistry()
	for _, adapter := range All() {
		if err := registry.Register(adapter); err != nil {
			t.Fatal(err)
		}
	}
	adapter, ok := registry.Lookup("acme_ai")
	if !ok {
		t.Fatal("acme_ai not registered")
	}
	sink := &memorySink{}
	ctx := ailedger.WithConnectorMaterial(context.Background(), ailedger.ConnectorMaterial{}, nil)
	if _, err := adapter.Sync(ctx, ailedger.Checkpoint{}, sink); err != nil {
		t.Fatal(err)
	}
	if sink.products != 1 || sink.metrics != 1 || sink.costs != 1 {
		t.Fatalf("unexpected facts: %+v", sink)
	}
}

func TestProviderDescriptorsAreComplete(t *testing.T) {
	for _, adapter := range All() {
		descriptor := adapter.Descriptor()
		if descriptor.AdapterKey == "" || descriptor.DisplayName == "" || len(descriptor.Capabilities) == 0 || len(descriptor.AllowedHosts) == 0 {
			t.Fatalf("incomplete descriptor: %+v", descriptor)
		}
	}
}
