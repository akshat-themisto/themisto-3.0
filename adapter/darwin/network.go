//go:build darwin

package darwin

import (
	"net"
	"os/exec"
	"strings"

	"github.com/themisto/agent/pkg/log"
)

// darwinNetworkInfo implements iface.NetworkInfo using macOS networking APIs
// and CLI tools.
type darwinNetworkInfo struct {
	log log.Logger
}

func newNetworkInfo(logger log.Logger) *darwinNetworkInfo {
	return &darwinNetworkInfo{log: logger}
}

// PrimaryInterface returns the name of the primary outbound network interface
// (e.g. "en0"). Uses route -n get default to find the interface used for the
// default route.
func (n *darwinNetworkInfo) PrimaryInterface() string {
	out, err := exec.Command("route", "-n", "get", "default").Output()
	if err != nil {
		n.log.Debug("failed to determine primary interface", "error", err)
		return ""
	}

	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "interface:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "interface:"))
		}
	}
	return ""
}

// IsVPNActive reports whether a VPN connection appears active by checking for
// utun interfaces (used by IPsec, WireGuard, and most macOS VPN clients).
func (n *darwinNetworkInfo) IsVPNActive() bool {
	ifaces, err := net.Interfaces()
	if err != nil {
		return false
	}
	for _, iface := range ifaces {
		name := strings.ToLower(iface.Name)
		// utun interfaces are used by IPsec/IKEv2, WireGuard, and most
		// VPN clients on macOS. ppp interfaces cover legacy PPTP/L2TP.
		if strings.HasPrefix(name, "utun") || strings.HasPrefix(name, "ppp") {
			if iface.Flags&net.FlagUp != 0 {
				return true
			}
		}
	}
	return false
}
