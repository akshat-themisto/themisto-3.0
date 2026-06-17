//go:build !darwin && !windows

package main

import (
	"context"
	"fmt"

	"github.com/themisto/agent/core/adapter/iface"
	"github.com/themisto/agent/core/domain"
	"github.com/themisto/agent/pkg/log"
)

// stubAdapter is a no-op OSAdapter used for builds without a real platform
// adapter. It satisfies all interface methods and returns ErrUnsupported for
// mutating operations. Replace with adapter/darwin or adapter/windows via
// build tags in production builds.
type stubAdapter struct {
	log log.Logger
}

func newPlatformAdapter(logger log.Logger) iface.OSAdapter {
	return &stubAdapter{log: logger}
}

// SystemProxy
func (s *stubAdapter) Register(_ context.Context, _ string, _ uint16) error {
	s.log.Warn("stub adapter: Register called (no-op)")
	return nil
}
func (s *stubAdapter) Unregister(_ context.Context) error { return nil }
func (s *stubAdapter) State() (domain.ProxyState, error)  { return domain.ProxyState{}, nil }
func (s *stubAdapter) VerifyIntegrity() (bool, error)     { return true, nil }

// CertStore
func (s *stubAdapter) InstallCA(_ context.Context, _ string, _ []byte) error {
	return fmt.Errorf("install CA: %w", iface.ErrUnsupported)
}
func (s *stubAdapter) RemoveCA(_ context.Context, _ string) error { return nil }
func (s *stubAdapter) HasCA(_ string) (bool, error)               { return false, nil }

// ProcessResolver
func (s *stubAdapter) ResolveByPID(_ int) (domain.ProcessInfo, error) {
	return domain.ProcessInfo{}, fmt.Errorf("resolve by PID: %w", iface.ErrUnsupported)
}
func (s *stubAdapter) ResolveByConnection(_ string, _ uint16, _ string, _ uint16) (domain.ProcessInfo, error) {
	return domain.ProcessInfo{}, fmt.Errorf("resolve by connection: %w", iface.ErrUnsupported)
}

// ServiceManager
func (s *stubAdapter) Install(_ context.Context) error { return nil }
func (s *stubAdapter) Start(_ context.Context, ready func()) error {
	ready()
	<-context.Background().Done()
	return nil
}
func (s *stubAdapter) Stop(_ context.Context) error          { return nil }
func (s *stubAdapter) Uninstall(_ context.Context) error     { return nil }
func (s *stubAdapter) Status() (domain.ServiceStatus, error) { return domain.ServiceRunning, nil }

// NetworkInfo
func (s *stubAdapter) PrimaryInterface() string { return "" }
func (s *stubAdapter) IsVPNActive() bool        { return false }

// UserNotifier
func (s *stubAdapter) Notify(_ context.Context, _ string, _ string) error { return nil }
