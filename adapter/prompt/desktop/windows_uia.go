//go:build windows

package desktop

import (
	"context"
	"errors"
)

// WindowsUIAAdapter is the Windows UI Automation pre-send capture adapter.
// The production implementation should watch supported desktop AI clients and
// call the local /v1/prompt/evaluate API.
type WindowsUIAAdapter struct{}

func NewWindowsUIAAdapter() *WindowsUIAAdapter {
	return &WindowsUIAAdapter{}
}

func (a *WindowsUIAAdapter) Name() string { return "windows_uia" }

func (a *WindowsUIAAdapter) Start(context.Context) error {
	return errors.New("desktop prompt capture adapter is not implemented for Windows UI Automation")
}

func (a *WindowsUIAAdapter) Stop(context.Context) error { return nil }
