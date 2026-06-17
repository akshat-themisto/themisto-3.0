//go:build darwin

// Package darwin implements the macOS-specific OS adapter. It satisfies
// iface.OSAdapter by composing five sub-implementations:
//
//   - darwinSystemProxy  (networksetup CLI)
//   - darwinCertStore    (security CLI + System Keychain)
//   - darwinProcessResolver (libproc + lsof)
//   - darwinServiceManager  (launchd/launchctl)
//   - darwinNetworkInfo     (route CLI + net.Interfaces)
//
// All files in this package carry the //go:build darwin constraint and are
// only compiled when GOOS=darwin.
package darwin

import (
	"github.com/themisto/agent/core/adapter/iface"
	"github.com/themisto/agent/pkg/log"
)

// Adapter is the macOS OSAdapter. It embeds all five sub-interface
// implementations so that it satisfies iface.OSAdapter directly.
type Adapter struct {
	*darwinSystemProxy
	*darwinCertStore
	*darwinProcessResolver
	*darwinServiceManager
	*darwinNetworkInfo
	*darwinNotifier
}

// New creates a fully initialised macOS adapter.
func New(logger log.Logger) iface.OSAdapter {
	return &Adapter{
		darwinSystemProxy:     newSystemProxy(logger),
		darwinCertStore:       newCertStore(logger),
		darwinProcessResolver: newProcessResolver(logger),
		darwinServiceManager:  newServiceManager(logger),
		darwinNetworkInfo:     newNetworkInfo(logger),
		darwinNotifier:        newNotifier(logger),
	}
}
