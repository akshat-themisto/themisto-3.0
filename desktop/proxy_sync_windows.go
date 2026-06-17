//go:build windows

package main

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows/registry"
)

const desktopInetSettingsPath = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`

// ensureCurrentUserProxyRegistration makes a best-effort attempt to keep the
// interactive user's WinINET proxy pointed at the local Themisto listener after
// fresh installs/reinstalls. The Windows Service can be healthy while still
// missing HKCU proxy state for the logged-in desktop user, which results in
// stale telemetry. This repair path only acts when the local agent is healthy
// and it will not overwrite a non-Themisto proxy already configured by the user.
func (a *App) ensureCurrentUserProxyRegistration() {
	for attempt := 0; attempt < 20; attempt++ {
		status := a.GetAgentStatus()
		cfg := a.GetConfig()

		if status.Running && status.ProxyListenerReady && cfg.Enrolled {
			host, port, err := parseDesktopListenAddr(cfg.ListenAddr)
			if err == nil {
				_ = syncCurrentUserProxy(host, port)
			}
			return
		}

		time.Sleep(500 * time.Millisecond)
	}
}

func parseDesktopListenAddr(addr string) (string, uint16, error) {
	host, portStr, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		return "", 0, err
	}
	if host == "" {
		host = "127.0.0.1"
	}
	port64, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil {
		return "", 0, err
	}
	return host, uint16(port64), nil
}

func syncCurrentUserProxy(host string, port uint16) error {
	expected := fmt.Sprintf("http=%s:%d;https=%s:%d", host, port, host, port)
	enabled, server := readDesktopWinINETProxy()

	// Respect an unrelated existing user proxy. We only self-heal when the
	// current user is unset/disabled or already pointing at Themisto.
	if enabled && server != "" && server != expected {
		return nil
	}

	k, _, err := registry.CreateKey(registry.CURRENT_USER, desktopInetSettingsPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()

	if err := k.SetDWordValue("ProxyEnable", 1); err != nil {
		return err
	}
	if err := k.SetStringValue("ProxyServer", expected); err != nil {
		return err
	}
	if err := k.SetStringValue("ProxyOverride", "<local>"); err != nil {
		return err
	}

	notifyDesktopProxyChange()
	return nil
}

func readDesktopWinINETProxy() (bool, string) {
	k, err := registry.OpenKey(registry.CURRENT_USER, desktopInetSettingsPath, registry.QUERY_VALUE)
	if err != nil {
		return false, ""
	}
	defer k.Close()

	en, _, _ := k.GetIntegerValue("ProxyEnable")
	sv, _, _ := k.GetStringValue("ProxyServer")
	return en == 1, sv
}

func notifyDesktopProxyChange() {
	mod := syscall.NewLazyDLL("wininet.dll")
	proc := mod.NewProc("InternetSetOptionW")
	if proc.Find() != nil {
		return
	}
	proc.Call(0, 39, 0, 0) // INTERNET_OPTION_SETTINGS_CHANGED
	proc.Call(0, 37, 0, 0) // INTERNET_OPTION_REFRESH
}
