package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/themisto/agent/core/domain"
)

const (
	windsurfEvaluateURL = "http://127.0.0.1:17175/v1/prompt/evaluate"
	windsurfOutcomeURL  = "http://127.0.0.1:17175/v1/prompt/outcome"
)

type windsurfHookInput struct {
	AgentActionName string               `json:"agent_action_name"`
	TrajectoryID    string               `json:"trajectory_id"`
	ExecutionID     string               `json:"execution_id"`
	Timestamp       string               `json:"timestamp"`
	ToolInfo        windsurfHookToolInfo `json:"tool_info"`
}

type windsurfHookToolInfo struct {
	UserPrompt string `json:"user_prompt"`
}

func runWindsurfHook() int {
	logPath := windsurfHookLogPath()
	windsurfHookLog(logPath, fmt.Sprintf("hook invoked  pid=%d", os.Getpid()))

	inputJSON, err := io.ReadAll(os.Stdin)
	if err != nil {
		windsurfHookLog(logPath, fmt.Sprintf("stdin read error=%v -- allow", err))
		return 0
	}
	windsurfHookLog(logPath, fmt.Sprintf("stdin received  bytes=%d", len(inputJSON)))

	if len(bytes.TrimSpace(inputJSON)) == 0 {
		windsurfHookLog(logPath, "empty stdin -- allow")
		return 0
	}

	var payload windsurfHookInput
	if err := json.Unmarshal(inputJSON, &payload); err != nil {
		windsurfHookLog(logPath, fmt.Sprintf("invalid json  error=%v -- allow", err))
		return 0
	}

	promptText := strings.TrimSpace(payload.ToolInfo.UserPrompt)
	if promptText == "" {
		windsurfHookLog(logPath, "no user_prompt in tool_info -- allow")
		return 0
	}
	windsurfHookLog(logPath, fmt.Sprintf("prompt parsed  len=%d", len(promptText)))

	evalReq := domain.PromptEvaluationRequest{
		PromptText:      promptText,
		Surface:         domain.CaptureSurfaceWindsurf,
		AppName:         "Windsurf",
		Vendor:          "windsurf",
		ServiceCategory: "ai_code",
		DestinationHost: "windsurf.ai",
		Metadata: map[string]string{
			"trajectory_id": payload.TrajectoryID,
			"execution_id":  payload.ExecutionID,
			"action_name":   payload.AgentActionName,
		},
	}

	windsurfHookLog(logPath, "calling evaluator  POST http://127.0.0.1:17175/v1/prompt/evaluate")
	var evalResp domain.PromptEvaluationResponse
	status, err := windsurfHookPostJSON(windsurfEvaluateURL, evalReq, &evalResp, 10*time.Second)
	if err != nil {
		windsurfHookLog(logPath, fmt.Sprintf("evaluator error  status=%d  error=%v -- allow", status, err))
		_, _ = windsurfHookPostJSON(windsurfOutcomeURL, domain.PromptOutcomeRequest{
			Surface:          domain.CaptureSurfaceWindsurf,
			Outcome:          domain.CaptureOutcomeDegradedFailOpen,
			DestinationHost:  "windsurf.ai",
			Vendor:           "windsurf",
			ServiceCategory:  "ai_code",
			AppName:          "Windsurf",
			UserMessageShown: false,
			Error:            err.Error(),
		}, nil, 3*time.Second)
		return 0
	}

	windsurfHookLog(logPath, fmt.Sprintf("evaluator responded  decision=%s  evaluation_id=%s", evalResp.Decision, evalResp.EvaluationID))

	if evalResp.Decision == domain.DecisionBlock {
		windsurfHookLog(logPath, "WOULD_BLOCK  monitoring_only=true  exit=0")
		_, _ = windsurfHookPostJSON(windsurfOutcomeURL, domain.PromptOutcomeRequest{
			EvaluationID:     evalResp.EvaluationID,
			Surface:          domain.CaptureSurfaceWindsurf,
			Outcome:          domain.CaptureOutcomeWouldBlock,
			AppName:          "Windsurf",
			UserMessageShown: false,
		}, nil, 3*time.Second)
		return 0
	}

	windsurfHookLog(logPath, fmt.Sprintf("ALLOWED  decision=%s  exit=0", evalResp.Decision))
	_, _ = windsurfHookPostJSON(windsurfOutcomeURL, domain.PromptOutcomeRequest{
		EvaluationID:     evalResp.EvaluationID,
		Surface:          domain.CaptureSurfaceWindsurf,
		Outcome:          domain.CaptureOutcomeAllowed,
		AppName:          "Windsurf",
		UserMessageShown: false,
	}, nil, 3*time.Second)
	return 0
}

func windsurfHookPostJSON(url string, payload interface{}, out interface{}, timeout time.Duration) (int, error) {
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

func windsurfHookLogPath() string {
	if localAppData := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); localAppData != "" {
		return filepath.Join(localAppData, "Themisto", "logs", "windsurf-hook.log")
	}
	return filepath.Join(os.TempDir(), "themisto-windsurf-hook.log")
}

func windsurfHookLog(path, msg string) {
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
	_, _ = fmt.Fprintf(f, "%s  %s\r\n", ts, msg)
}
