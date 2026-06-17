//go:build windows

package main

import (
	"strings"
	"testing"
)

func TestLaunchOnLoginCommand(t *testing.T) {
	got := launchOnLoginCommand(`C:\Program Files\Themisto\themisto-desktop.exe`)
	want := `"C:\Program Files\Themisto\themisto-desktop.exe" --background`
	if got != want {
		t.Fatalf("launchOnLoginCommand() = %q, want %q", got, want)
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
