package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	promptEvaluateURL = "http://127.0.0.1:17175/v1/prompt/evaluate"
	promptOutcomeURL  = "http://127.0.0.1:17175/v1/prompt/outcome"
)

func promptHookPostJSON(url string, payload interface{}, out interface{}, timeout time.Duration) (int, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	client := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			Proxy: nil,
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(respBody))
		if msg == "" {
			msg = resp.Status
		}
		return resp.StatusCode, fmt.Errorf("%s", msg)
	}

	if out != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, out); err != nil {
			return resp.StatusCode, err
		}
	}
	return resp.StatusCode, nil
}

func promptHookLogPath(filename string) string {
	if runtime.GOOS == "windows" {
		if localAppData := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); localAppData != "" {
			return filepath.Join(localAppData, "Themisto", "logs", filename)
		}
	}
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		if runtime.GOOS == "darwin" {
			return filepath.Join(home, "Library", "Application Support", "Themisto", "logs", filename)
		}
		return filepath.Join(home, ".local", "share", "themisto", "logs", filename)
	}
	return filepath.Join(os.TempDir(), filename)
}

func promptHookLog(path, msg string) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return
	}
	defer f.Close()

	ts := time.Now().Format("2006-01-02 15:04:05.000")
	_, _ = fmt.Fprintf(f, "%s  %s\n", ts, msg)
}
