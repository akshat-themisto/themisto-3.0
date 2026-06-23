package store

import (
	"testing"
	"time"
)

func TestDeriveFleetConnectivity(t *testing.T) {
	now := time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)
	secondsAgo := func(seconds int) *time.Time {
		value := now.Add(-time.Duration(seconds) * time.Second)
		return &value
	}

	tests := []struct {
		name   string
		device FleetDevice
		want   string
	}{
		{
			name: "connected healthy device",
			device: FleetDevice{
				OrganizationStatus: "active", EnrollmentStatus: "active",
				LastSeenAt: secondsAgo(20), GatewayConnected: true, ProxyListenerAlive: true,
				ProxyIntegrity: "ok", PromptCapture: "healthy", SemanticClassifier: "healthy",
			},
			want: "connected",
		},
		{
			name: "recent component failure",
			device: FleetDevice{
				OrganizationStatus: "active", EnrollmentStatus: "active",
				LastSeenAt: secondsAgo(20), GatewayConnected: false, ProxyListenerAlive: true,
				ProxyIntegrity: "ok", PromptCapture: "healthy", SemanticClassifier: "healthy",
			},
			want: "degraded",
		},
		{
			name: "unresolved tamper outranks connectivity",
			device: FleetDevice{
				OrganizationStatus: "active", EnrollmentStatus: "active",
				LastSeenAt: secondsAgo(10), LastTamperAt: secondsAgo(15), LastHealthyAt: secondsAgo(30),
			},
			want: "suspected_tamper",
		},
		{
			name: "healthy heartbeat resolves tamper",
			device: FleetDevice{
				OrganizationStatus: "active", EnrollmentStatus: "active",
				LastSeenAt: secondsAgo(10), LastTamperAt: secondsAgo(30), LastHealthyAt: secondsAgo(10),
				GatewayConnected: true, ProxyListenerAlive: true, ProxyIntegrity: "ok",
				PromptCapture: "healthy", SemanticClassifier: "disabled",
			},
			want: "connected",
		},
		{
			name: "offline after heartbeat silence",
			device: FleetDevice{
				OrganizationStatus: "active", EnrollmentStatus: "active", LastSeenAt: secondsAgo(301),
			},
			want: "offline",
		},
		{
			name: "inactive management state",
			device: FleetDevice{
				OrganizationStatus: "suspended", EnrollmentStatus: "suspended", LastSeenAt: secondsAgo(10),
			},
			want: "managed_inactive",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, _ := deriveFleetConnectivity(test.device, now)
			if got != test.want {
				t.Fatalf("connectivity = %q, want %q", got, test.want)
			}
		})
	}
}
