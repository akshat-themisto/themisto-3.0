//go:build darwin

package main

import (
	"fmt"
	"os"
	"strings"
)

func directEnrollAndStartService(token, backendURL, gatewayURL, orgName, deviceID string) error {
	if err := validateStartupFiles(agentBinaryPath, agentConfigPath); err != nil {
		return err
	}

	tempPlist, err := writeDarwinServicePlistTemp()
	if err != nil {
		return fmt.Errorf("prepare launchd service: %w", err)
	}
	defer func() { _ = os.Remove(tempPlist) }()

	args := []string{
		shellQuote(agentBinaryPath),
		"-activate",
		"-config", shellQuote(agentConfigPath),
		"-backend-url", shellQuote(backendURL),
		"-org-name", shellQuote(orgName),
		"-device-id", shellQuote(deviceID),
		"-enrollment-token", shellQuote(token),
	}
	if strings.TrimSpace(gatewayURL) != "" {
		args = append(args, "-gateway-url", shellQuote(gatewayURL))
	}

	script := strings.Join([]string{
		strings.Join(args, " "),
		"/bin/mkdir -p /Library/LaunchDaemons /var/log/themisto",
		fmt.Sprintf("/usr/bin/install -m 0644 %s %s", shellQuote(tempPlist), shellQuote(darwinServicePlistPath)),
		fmt.Sprintf("/bin/launchctl bootout system %s >/dev/null 2>&1 || true", shellQuote(darwinServicePlistPath)),
		fmt.Sprintf("/bin/launchctl bootstrap system %s", shellQuote(darwinServicePlistPath)),
		fmt.Sprintf("/bin/launchctl kickstart -k system/%s", darwinServiceLabel),
	}, " && ")

	if err := runAdminShellScript(script); err != nil {
		return fmt.Errorf("direct enrollment failed: %w", err)
	}

	return nil
}
