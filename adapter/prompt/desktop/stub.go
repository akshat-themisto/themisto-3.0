//go:build !darwin && !windows

package desktop

import (
	"context"
	"errors"
)

var errUnsupportedPlatform = errors.New("desktop prompt capture adapter not supported on this platform")

type StubAdapter struct{}

func NewStubAdapter() *StubAdapter { return &StubAdapter{} }

func (a *StubAdapter) Name() string { return "stub" }

func (a *StubAdapter) Start(context.Context) error { return errUnsupportedPlatform }

func (a *StubAdapter) Stop(context.Context) error { return nil }
