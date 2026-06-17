package policy

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/themisto/agent/core/domain"
)

// gatewayDoer is a narrow interface satisfied by transport.GatewayClient so
// the policy package avoids importing transport (prevents import cycle).
type gatewayDoer interface {
	Do(ctx context.Context, req *domain.HTTPRequest) (*domain.HTTPResponse, error)
}

// DefaultFetcher implements the Fetcher interface. It retrieves policy from
// the gateway's /policy endpoint using the provided HTTP client.
type DefaultFetcher struct {
	client       gatewayDoer
	gatewayURL   string
	pollInterval time.Duration
	lastVersion  string
}

// NewFetcher creates a fetcher targeting gatewayURL/policy.
func NewFetcher(client gatewayDoer, gatewayURL string, pollInterval time.Duration) *DefaultFetcher {
	return &DefaultFetcher{
		client:       client,
		gatewayURL:   gatewayURL,
		pollInterval: pollInterval,
	}
}

// Fetch retrieves the latest policy from the gateway. It sends the last known
// version as a conditional header; the gateway may return 304 Not Modified.
func (f *DefaultFetcher) Fetch() (*domain.PolicyPayload, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	headers := map[string][]string{
		"Accept":                         {"application/json"},
		"X-Themisto-Protocol-Version":    {domain.ProtocolVersion},
	}
	if f.lastVersion != "" {
		headers["X-Themisto-Policy-Version"] = []string{f.lastVersion}
		headers["If-None-Match"] = []string{f.lastVersion}
	}

	resp, err := f.client.Do(ctx, &domain.HTTPRequest{
		Method:  "GET",
		URL:     f.gatewayURL + "/policy",
		Headers: headers,
	})
	if err != nil {
		return nil, "", fmt.Errorf("policy fetch: %w", err)
	}

	if resp.StatusCode == 304 {
		return nil, f.lastVersion, nil
	}
	if resp.StatusCode >= 400 {
		return nil, "", fmt.Errorf("policy fetch: gateway returned %d", resp.StatusCode)
	}

	var payload domain.PolicyPayload
	if err := json.Unmarshal(resp.Body, &payload); err != nil {
		return nil, "", fmt.Errorf("policy fetch: decode: %w", err)
	}

	f.lastVersion = payload.Version

	// Allow the gateway to override the poll interval.
	if vals, ok := resp.Headers["X-Themisto-Poll-Interval"]; ok && len(vals) > 0 {
		if secs, err := strconv.Atoi(vals[0]); err == nil && secs > 0 {
			f.pollInterval = time.Duration(secs) * time.Second
		}
	}

	return &payload, payload.Version, nil
}

// PollInterval returns the recommended duration between fetches.
func (f *DefaultFetcher) PollInterval() time.Duration {
	return f.pollInterval
}
