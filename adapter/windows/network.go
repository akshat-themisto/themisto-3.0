//go:build windows

package windows

import (
	"net"
	"os/exec"
	"strings"

	"github.com/themisto/agent/pkg/log"
)

// winNetworkInfo implements iface.NetworkInfo.
type winNetworkInfo struct {
	log log.Logger
}

func newNetworkInfo(logger log.Logger) *winNetworkInfo {
	return &winNetworkInfo{log: logger}
}

// PrimaryInterface returns the name of the primary outbound network
// interface. On Windows, this uses `route print` to find the interface
// associated with the default gateway (0.0.0.0).
func (n *winNetworkInfo) PrimaryInterface() string {
	out, err := exec.Command("powershell", "-NoProfile", "-Command",
		`(Get-NetRoute -DestinationPrefix '0.0.0.0/0' | Sort-Object RouteMetric | Select-Object -First 1).InterfaceAlias`,
	).Output()
	if err != nil {
		n.log.Debug("failed to determine primary interface", "error", err)
		return ""
	}
	return strings.TrimSpace(string(out))
}

// IsVPNActive reports whether a VPN connection appears active by checking
// for common VPN adapter name patterns and PPP/tunnel interfaces.
func (n *winNetworkInfo) IsVPNActive() bool {
	ifaces, err := net.Interfaces()
	if err != nil {
		return false
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		name := strings.ToLower(iface.Name)
		if strings.Contains(name, "vpn") ||
			strings.Contains(name, "wireguard") ||
			strings.Contains(name, "tunnel") ||
			strings.Contains(name, "tap") ||
			strings.Contains(name, "tun") ||
			strings.Contains(name, "pptp") {
			return true
		}
	}

	// Fallback: check RAS (Remote Access Service) connections.
	out, err := exec.Command("rasdial").Output()
	if err == nil {
		output := strings.TrimSpace(string(out))
		if !strings.Contains(output, "No connections") &&
			!strings.Contains(output, "no connections") &&
			len(output) > 0 {
			return true
		}
	}

	return false
}
