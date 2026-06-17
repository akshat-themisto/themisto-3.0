//go:build windows

// Package windows implements the Windows-specific OS adapter. It satisfies
// iface.OSAdapter by composing five sub-implementations:
//
//   - winSystemProxy      (registry WinINET + netsh WinHTTP)
//   - winCertStore         (certutil CLI + Local Machine Root store)
//   - winProcessResolver   (Win32 APIs + GetExtendedTcpTable)
//   - winServiceManager    (SCM via golang.org/x/sys/windows/svc)
//   - winNetworkInfo       (PowerShell + net.Interfaces)
//
// All files in this package carry the //go:build windows constraint.
package windows

import (
	"github.com/themisto/agent/core/adapter/iface"
	"github.com/themisto/agent/pkg/log"
)

// Adapter is the Windows OSAdapter. It embeds all five sub-interface
// implementations so that it satisfies iface.OSAdapter directly.
type Adapter struct {
	*winSystemProxy
	*winCertStore
	*winProcessResolver
	*winServiceManager
	*winNetworkInfo
	*winNotifier
}

// New creates a fully initialised Windows adapter.
func New(logger log.Logger) iface.OSAdapter {
	return &Adapter{
		winSystemProxy:     newSystemProxy(logger),
		winCertStore:       newCertStore(logger),
		winProcessResolver: newProcessResolver(logger),
		winServiceManager:  newServiceManager(logger),
		winNetworkInfo:     newNetworkInfo(logger),
		winNotifier:        newNotifier(logger),
	}
}
