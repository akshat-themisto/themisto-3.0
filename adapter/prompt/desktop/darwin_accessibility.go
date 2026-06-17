//go:build darwin

package desktop

import "context"

// DarwinAccessibilityAdapter is the macOS pre-send capture adapter.
// The production implementation should subscribe to AX events on supported
// desktop AI clients and call the local /v1/prompt/evaluate API.
type DarwinAccessibilityAdapter struct{}

func NewDarwinAccessibilityAdapter() *DarwinAccessibilityAdapter {
	return &DarwinAccessibilityAdapter{}
}

func (a *DarwinAccessibilityAdapter) Name() string { return "darwin_accessibility" }

func (a *DarwinAccessibilityAdapter) Start(context.Context) error { return nil }

func (a *DarwinAccessibilityAdapter) Stop(context.Context) error { return nil }
