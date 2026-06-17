package main

import (
	"os"
	"path/filepath"
	"runtime"
)

type certPathDefault struct {
	label string
	key   string
	path  string
}

var (
	agentBinaryPath   = resolveAgentBinaryPath()
	desktopBinaryPath = resolveDesktopBinaryPath()
	agentConfigPath   = resolveAgentConfigPath()
	statusTokenPath   = resolveStatusTokenPath()
	certPathDefaults  = resolveCertPathDefaults()
)

func resolveAgentBinaryPath() string {
	if runtime.GOOS == "windows" {
		return `C:\Program Files\Themisto\themisto-agent.exe`
	}
	return "/usr/local/bin/themisto-agent"
}

func resolveDesktopBinaryPath() string {
	if runtime.GOOS == "windows" {
		return `C:\Program Files\Themisto\themisto-desktop.exe`
	}
	if exe, err := os.Executable(); err == nil && exe != "" {
		return exe
	}
	return "/Applications/Themisto.app/Contents/MacOS/themisto-desktop"
}

func resolveAgentConfigPath() string {
	if runtime.GOOS == "windows" {
		return `C:\ProgramData\Themisto\agent.json`
	}
	return "/etc/themisto/agent.json"
}

func resolveStatusTokenPath() string {
	if runtime.GOOS == "windows" {
		if pd := os.Getenv("ProgramData"); pd != "" {
			return filepath.Join(pd, "Themisto", "themisto-status-api.token")
		}
		return `C:\ProgramData\Themisto\themisto-status-api.token`
	}
	return "/tmp/themisto-status-api.token"
}

func resolveCertPathDefaults() []certPathDefault {
	if runtime.GOOS == "windows" {
		return []certPathDefault{
			{label: "Device certificate", key: "cert_path", path: `C:\ProgramData\Themisto\certs\device.crt`},
			{label: "Device private key", key: "key_path", path: `C:\ProgramData\Themisto\certs\device.key`},
			{label: "CA certificate", key: "ca_path", path: `C:\ProgramData\Themisto\certs\ca-chain.pem`},
		}
	}
	return []certPathDefault{
		{label: "Device certificate", key: "cert_path", path: "/etc/themisto/identity/device.crt"},
		{label: "Device private key", key: "key_path", path: "/etc/themisto/identity/device.key"},
		{label: "CA certificate", key: "ca_path", path: "/etc/themisto/identity/ca-chain.pem"},
	}
}
