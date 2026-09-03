package transport

import (
	"strconv"
	"time"

	"github.com/themisto/agent/core/domain"
)

// BuildWrapperHeaders constructs the X-Themisto-* metadata headers that
// accompany every forwarded request. It is a pure function with no side
// effects.
func BuildWrapperHeaders(
	agentID string,
	proc domain.ProcessInfo,
	primaryIface string,
	vpnActive bool,
	policyVersion string,
	decision domain.Decision,
	serviceCategory string,
	aiVendor string,
	ruleID string,
	requestID string,
) map[string]string {
	h := map[string]string{
		"X-Themisto-Protocol-Version": domain.ProtocolVersion,
		"X-Themisto-Agent-ID":         agentID,
		"X-Themisto-Request-ID":       requestID,
		"X-Themisto-Timestamp":        time.Now().UTC().Format(time.RFC3339Nano),
		"X-Themisto-Decision":         decision.String(),
	}

	setNonEmpty(h, "X-Themisto-Rule-ID", ruleID)
	setNonEmpty(h, "X-Themisto-Policy-Version", policyVersion)
	setNonEmpty(h, "X-Themisto-Service-Category", serviceCategory)
	setNonEmpty(h, "X-Themisto-AI-Vendor", aiVendor)

	if proc.PID != 0 {
		h["X-Themisto-Process-PID"] = strconv.Itoa(proc.PID)
	}
	setNonEmpty(h, "X-Themisto-Process-Name", proc.Name)
	setNonEmpty(h, "X-Themisto-Process-Bundle", proc.BundleID)
	setNonEmpty(h, "X-Themisto-Process-Signer", proc.SignerID)
	if proc.Signed {
		h["X-Themisto-Process-Signed"] = "true"
	}

	setNonEmpty(h, "X-Themisto-Network-Interface", primaryIface)
	if vpnActive {
		h["X-Themisto-VPN-Active"] = "true"
	}

	return h
}

func setNonEmpty(m map[string]string, key, val string) {
	if val != "" {
		m[key] = val
	}
}
