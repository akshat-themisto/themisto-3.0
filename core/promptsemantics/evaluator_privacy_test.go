package promptsemantics

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/themisto/agent/core/domain"
)

type rejectingGateway struct{ called bool }

func (g *rejectingGateway) Do(context.Context, *domain.HTTPRequest) (*domain.HTTPResponse, error) {
	g.called = true
	return nil, errors.New("gateway must not receive prompt content")
}

func TestSemanticFailureNeverFallsBackToGateway(t *testing.T) {
	gateway := &rejectingGateway{}
	evaluator := NewCascadeEvaluator(CascadeOptions{
		LocalURL:       "http://127.0.0.1:1/v1/classify",
		LocalTimeout:   20 * time.Millisecond,
		GatewayURL:     "https://gateway.example",
		Gateway:        gateway,
		GatewayEnabled: true,
	})
	_, err := evaluator.Evaluate(context.Background(), domain.PromptSemanticRequest{
		PromptText: "private prompt", Policy: "local only",
	})
	if err == nil {
		t.Fatal("unavailable local sidecar should return bounded degraded error")
	}
	if gateway.called {
		t.Fatal("prompt content leaked to gateway after local failure")
	}
}
