//go:build windows

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type installerEnrollmentPromptResult struct {
	Link    string
	Skipped bool
	Cancelled bool
}

var errInstallerCancelled = errors.New("installer canceled")

func installerEnrollmentArg() string {
	for i := 1; i < len(os.Args); i++ {
		arg := strings.TrimSpace(os.Args[i])
		switch {
		case arg == "--enroll-url" || arg == "-enroll-url":
			if i+1 < len(os.Args) {
				return strings.TrimSpace(os.Args[i+1])
			}
		case strings.HasPrefix(arg, "--enroll-url="):
			return strings.TrimSpace(strings.TrimPrefix(arg, "--enroll-url="))
		case strings.HasPrefix(arg, "-enroll-url="):
			return strings.TrimSpace(strings.TrimPrefix(arg, "-enroll-url="))
		}
	}
	return ""
}

func maybeCollectInstallerEnrollmentConfig(cfg map[string]interface{}) (map[string]interface{}, error) {
	if cfg == nil {
		cfg = map[string]interface{}{}
	}
	if hasUsableCredentialFiles(cfg) || shouldAttemptEnrollment(cfg) {
		return cfg, nil
	}

	if raw := installerEnrollmentArg(); raw != "" {
		merged, _, err := loadInstallerEnrollmentConfig(cfg, raw)
		if err != nil {
			return cfg, err
		}
		return merged, nil
	}

	message := ""
	prefill := ""
	for {
		res := promptInstallerEnrollmentLink(prefill, message)
		if res.Cancelled {
			return cfg, errInstallerCancelled
		}
		if res.Skipped {
			return cfg, nil
		}

		merged, source, err := loadInstallerEnrollmentConfig(cfg, res.Link)
		if err == nil {
			fmt.Printf("      Enrollment config loaded from %s\n", source)
			return merged, nil
		}

		prefill = strings.TrimSpace(res.Link)
		message = err.Error()
	}
}

func loadInstallerEnrollmentConfig(cfg map[string]interface{}, raw string) (map[string]interface{}, string, error) {
	link, err := resolveInstallerEnrollmentLink(raw, cfg)
	if err != nil {
		return cfg, "", err
	}

	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			Proxy: nil,
			DialContext: (&net.Dialer{
				Timeout:   5 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
		},
	}

	resp, err := client.Get(link)
	if err != nil {
		return cfg, link, fmt.Errorf("could not download the enrollment link: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return cfg, link, fmt.Errorf("could not read the enrollment payload: %w", err)
	}
	if resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(body))
		if msg == "" {
			msg = http.StatusText(resp.StatusCode)
		}
		return cfg, link, fmt.Errorf("the enrollment link returned %d: %s", resp.StatusCode, msg)
	}

	var imported map[string]interface{}
	if err := json.Unmarshal(body, &imported); err != nil {
		return cfg, link, fmt.Errorf("the enrollment link did not return a valid agent.json")
	}
	if len(imported) == 0 {
		return cfg, link, fmt.Errorf("the enrollment link returned an empty config")
	}

	return mergeInstallerEnrollmentConfig(cfg, imported), link, nil
}

func resolveInstallerEnrollmentLink(raw string, cfg map[string]interface{}) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("paste the enrollment link or choose Set up later")
	}

	if u, err := url.Parse(raw); err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" {
		return raw, nil
	}

	backendURL := strings.TrimSpace(getString(cfg, "backend_url"))
	if backendURL == "" {
		return "", fmt.Errorf("please paste the full enrollment link from the dashboard")
	}

	code := strings.TrimPrefix(strings.TrimPrefix(raw, "/"), "enroll/")
	code = strings.TrimPrefix(code, "/")
	if code == "" {
		return "", fmt.Errorf("the enrollment short code is empty")
	}

	return strings.TrimRight(backendURL, "/") + "/enroll/" + code, nil
}

func mergeInstallerEnrollmentConfig(cfg, imported map[string]interface{}) map[string]interface{} {
	merged := cloneConfigMap(cfg)
	if merged == nil {
		merged = map[string]interface{}{}
	}

	for key, value := range imported {
		merged[key] = value
	}

	if strings.TrimSpace(getString(merged, "agent_id")) == "" && strings.TrimSpace(getString(merged, "device_id")) != "" {
		merged["agent_id"] = getString(merged, "device_id")
	}

	ensureInstallerConfigDefaults(merged)
	canonicalizeInstalledCredentialPaths(merged)
	return merged
}
