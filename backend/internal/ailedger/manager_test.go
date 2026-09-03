package ailedger

import (
	"context"
	"errors"
	"net/url"
	"testing"
	"time"
)

type testAdapter struct{ key string }

func (a testAdapter) Descriptor() ConnectorDescriptor {
	return ConnectorDescriptor{AdapterKey: a.key, DisplayName: "Test", AllowedHosts: []string{"api.example.test"}}
}
func (testAdapter) Validate(Credentials, ConnectorConfig) error { return nil }
func (testAdapter) Sync(context.Context, Checkpoint, FactSink) (Checkpoint, error) {
	return Checkpoint{"done": true}, nil
}

func TestRegistryOnlyExtensibility(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(testAdapter{key: "acme_ai"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := registry.Lookup("acme_ai"); !ok {
		t.Fatal("registered adapter not found")
	}
	if len(registry.Descriptors()) != 1 {
		t.Fatal("descriptor not surfaced")
	}
}

func TestRestrictedTransportRejectsHTTPAndUnknownHosts(t *testing.T) {
	transport := newRestrictedHTTPClient([]string{"api.example.test"}).Transport.(*restrictedTransport)
	for _, raw := range []string{"http://api.example.test/path", "https://evil.example/path"} {
		requestURL, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		if err := transport.validateURL(requestURL); err == nil {
			t.Fatalf("expected %s to be rejected", raw)
		}
	}
}

func TestBackoffIsBounded(t *testing.T) {
	if got := backoffForFailures(100); got != 128*time.Minute {
		t.Fatalf("unexpected capped backoff: %v", got)
	}
	if retryableConnectorError(context.Canceled) {
		t.Fatal("cancellation must not retry")
	}
	if retryableConnectorError(errors.New("validation")) {
		t.Fatal("validation error must not retry")
	}
}
