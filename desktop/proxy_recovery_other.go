//go:build !windows && !darwin

package main

// ProxyRecoveryStatus is a backend-authored signal that tells the frontend
// whether proxy recovery should be offered and why.
type ProxyRecoveryStatus struct {
	ShouldOffer    bool   `json:"should_offer"`
	NeedsElevation bool   `json:"needs_elevation"`
	Reason         string `json:"reason"`
	ProxyHost      string `json:"proxy_host"`
	ProxyPort      uint16 `json:"proxy_port"`
}

// ResetProxyResult describes the outcome of a proxy recovery attempt.
type ResetProxyResult struct {
	Success bool   `json:"success"`
	Partial bool   `json:"partial"`
	Pending bool   `json:"pending"`
	Message string `json:"message"`
}

// GetProxyRecoveryStatus is a no-op on non-Windows platforms.
func (a *App) GetProxyRecoveryStatus() ProxyRecoveryStatus {
	return ProxyRecoveryStatus{ShouldOffer: false}
}

// ResetSystemProxy is a no-op on non-Windows platforms.
func (a *App) ResetSystemProxy() ResetProxyResult {
	return ResetProxyResult{
		Success: false,
		Message: "Proxy recovery is only available on Windows.",
	}
}
