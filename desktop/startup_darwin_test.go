//go:build darwin

package main

import (
	"strings"
	"testing"
)

func TestDesktopLaunchAgentPlist(t *testing.T) {
	got := desktopLaunchAgentPlist("/Applications/Themisto.app/Contents/MacOS/themisto-desktop")
	for _, want := range []string{
		darwinDesktopLaunchAgentLabel,
		"/Applications/Themisto.app/Contents/MacOS/themisto-desktop",
		"<string>--background</string>",
		"<string>Aqua</string>",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("desktopLaunchAgentPlist() missing %q", want)
		}
	}
}

func TestIsMacAppBundleBinary(t *testing.T) {
	if !isMacAppBundleBinary("/Applications/Themisto.app/Contents/MacOS/themisto-desktop") {
		t.Fatal("expected .app bundle binary path to be accepted")
	}
	if isMacAppBundleBinary("/tmp/wails-dev/themisto-desktop") {
		t.Fatal("expected non-.app binary path to be rejected")
	}
}

func TestSetLaunchOnLoginRequiresAdmin(t *testing.T) {
	app := NewApp()
	err := app.SetLaunchOnLogin(false, "")
	if err == nil {
		t.Fatal("expected admin-gated SetLaunchOnLogin to reject an empty token")
	}
	if !strings.Contains(err.Error(), "unauthorized") {
		t.Fatalf("expected unauthorized error, got %v", err)
	}
}
