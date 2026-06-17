//go:build windows

package main

import "testing"

func TestParseDesktopListenAddr(t *testing.T) {
	host, port, err := parseDesktopListenAddr("127.0.0.1:8080")
	if err != nil {
		t.Fatalf("parseDesktopListenAddr() unexpected error: %v", err)
	}
	if host != "127.0.0.1" || port != 8080 {
		t.Fatalf("parseDesktopListenAddr() = %q/%d, want 127.0.0.1/8080", host, port)
	}
}

func TestParseDesktopListenAddrRejectsInvalid(t *testing.T) {
	if _, _, err := parseDesktopListenAddr("not-an-addr"); err == nil {
		t.Fatal("expected invalid listen addr to fail")
	}
}
