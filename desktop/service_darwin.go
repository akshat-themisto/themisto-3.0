//go:build darwin

package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const (
	darwinServiceLabel     = "com.themisto.agent"
	darwinServicePlistPath = "/Library/LaunchDaemons/com.themisto.agent.plist"
)

func isServiceInstalled() bool {
	_, err := os.Stat(darwinServicePlistPath)
	return err == nil
}

func queryServiceState() (string, string) {
	if !isServiceInstalled() {
		return "not_found", "Themisto launchd service is not installed."
	}

	out, err := exec.Command("launchctl", "print", "system/"+darwinServiceLabel).CombinedOutput()
	output := strings.TrimSpace(string(out))
	if err != nil {
		if strings.Contains(output, "Could not find service") {
			return "stopped", "LaunchDaemon plist is present but the service is not loaded."
		}
		if output == "" {
			output = err.Error()
		}
		return "unknown", "Could not query the macOS launchd service."
	}

	if pid := extractLaunchctlField(output, "pid = "); pid != "" {
		p, _ := strconv.Atoi(pid)
		if p > 0 {
			return "running", fmt.Sprintf("Themisto launchd service is running (pid %d).", p)
		}
	}

	if exitCode := extractLaunchctlField(output, "last exit code = "); exitCode != "" {
		return "stopped", fmt.Sprintf("Themisto launchd service is installed but stopped (last exit code %s).", exitCode)
	}

	return "stopped", "Themisto launchd service is installed but stopped."
}

func startService() error {
	if !isServiceInstalled() {
		return fmt.Errorf("start service: launchd service is not installed; repair agent service first")
	}
	return runAdminShellScript(fmt.Sprintf("/bin/launchctl kickstart -k system/%s", darwinServiceLabel))
}

func stopService() error {
	if !isServiceInstalled() {
		return nil
	}
	return runAdminShellScript(fmt.Sprintf("/bin/launchctl bootout system %s >/dev/null 2>&1 || true", shellQuote(darwinServicePlistPath)))
}

func repairAgentService() error {
	if err := validateStartupFiles(agentBinaryPath, agentConfigPath); err != nil {
		return err
	}

	tempPlist, err := writeDarwinServicePlistTemp()
	if err != nil {
		return fmt.Errorf("repair agent service: write plist: %w", err)
	}
	defer os.Remove(tempPlist)

	script := strings.Join([]string{
		"/bin/mkdir -p /Library/LaunchDaemons /var/log/themisto",
		fmt.Sprintf("/usr/bin/install -m 0644 %s %s", shellQuote(tempPlist), shellQuote(darwinServicePlistPath)),
		fmt.Sprintf("/bin/launchctl bootout system %s >/dev/null 2>&1 || true", shellQuote(darwinServicePlistPath)),
		fmt.Sprintf("/bin/launchctl bootstrap system %s", shellQuote(darwinServicePlistPath)),
		fmt.Sprintf("/bin/launchctl kickstart -k system/%s", darwinServiceLabel),
	}, " && ")

	if err := runAdminShellScript(script); err != nil {
		return fmt.Errorf("repair agent service: %w", err)
	}

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		state, _ := queryServiceState()
		if state == "running" {
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}

	return fmt.Errorf("repair agent service: service did not reach running state")
}

func writeDarwinServicePlistTemp() (string, error) {
	f, err := os.CreateTemp("", "themisto-launchd-*.plist")
	if err != nil {
		return "", err
	}
	defer f.Close()

	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>%s</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>-config</string>
    <string>%s</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>ThrottleInterval</key>
  <integer>10</integer>
  <key>StandardOutPath</key>
  <string>/var/log/themisto/agent.stdout.log</string>
  <key>StandardErrorPath</key>
  <string>/var/log/themisto/agent.stderr.log</string>
  <key>UserName</key>
  <string>root</string>
  <key>GroupName</key>
  <string>wheel</string>
</dict>
</plist>
`, darwinServiceLabel, agentBinaryPath, agentConfigPath)

	if _, err := f.WriteString(plist); err != nil {
		return "", err
	}
	return f.Name(), nil
}

func runAdminShellScript(script string) error {
	appleScript := fmt.Sprintf(`do shell script "%s" with administrator privileges`, escapeAppleScript(script))
	out, err := exec.Command("osascript", "-e", appleScript).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return fmt.Errorf("%s", msg)
		}
		return err
	}
	return nil
}

func shellQuote(v string) string {
	return "'" + strings.ReplaceAll(v, "'", `'"'"'`) + "'"
}

func escapeAppleScript(v string) string {
	v = strings.ReplaceAll(v, "\\", "\\\\")
	v = strings.ReplaceAll(v, "\"", "\\\"")
	v = strings.ReplaceAll(v, "\n", " ")
	return v
}

func extractLaunchctlField(output, prefix string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
		if idx := strings.Index(line, prefix); idx >= 0 {
			val := strings.TrimSpace(line[idx+len(prefix):])
			val = strings.TrimRight(val, ";")
			return strings.TrimSpace(val)
		}
	}
	return ""
}
