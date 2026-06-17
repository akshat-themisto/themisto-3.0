package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/themisto/agent/core/domain"
)

type claudeCodeHookInput struct {
	Prompt         string `json:"prompt"`
	SessionID      string `json:"session_id"`
	CWD            string `json:"cwd"`
	HookEventName  string `json:"hook_event_name"`
	TranscriptPath string `json:"transcript_path"`
	PermissionMode string `json:"permission_mode"`
}

func runClaudeCodeHook() int {
	logPath := promptHookLogPath("claude-code-hook.log")
	promptHookLog(logPath, fmt.Sprintf("hook invoked pid=%d", os.Getpid()))

	inputJSON, err := io.ReadAll(os.Stdin)
	if err != nil {
		promptHookLog(logPath, fmt.Sprintf("stdin read error=%v -- allow", err))
		return 0
	}
	promptHookLog(logPath, fmt.Sprintf("stdin received bytes=%d", len(inputJSON)))

	if len(bytesTrimSpace(inputJSON)) == 0 {
		promptHookLog(logPath, "empty stdin -- allow")
		return 0
	}

	var payload claudeCodeHookInput
	if err := json.Unmarshal(inputJSON, &payload); err != nil {
		promptHookLog(logPath, fmt.Sprintf("invalid json error=%v -- allow", err))
		return 0
	}

	promptText := strings.TrimSpace(payload.Prompt)
	if promptText == "" {
		promptHookLog(logPath, "no prompt field -- allow")
		return 0
	}

	evalReq := domain.PromptEvaluationRequest{
		PromptText:      promptText,
		Surface:         domain.CaptureSurfaceClaudeCode,
		AppName:         "Claude Code",
		Vendor:          "anthropic",
		ServiceCategory: "ai_code",
		DestinationHost: "api.anthropic.com",
		Metadata: map[string]string{
			"session_id":      payload.SessionID,
			"cwd":             payload.CWD,
			"hook_event":      payload.HookEventName,
			"transcript_path": payload.TranscriptPath,
			"permission_mode": payload.PermissionMode,
		},
	}

	var evalResp domain.PromptEvaluationResponse
	status, err := promptHookPostJSON(promptEvaluateURL, evalReq, &evalResp, 10*time.Second)
	if err != nil {
		promptHookLog(logPath, fmt.Sprintf("evaluator error status=%d error=%v -- allow", status, err))
		_, _ = promptHookPostJSON(promptOutcomeURL, domain.PromptOutcomeRequest{
			Surface:          domain.CaptureSurfaceClaudeCode,
			Outcome:          domain.CaptureOutcomeDegradedFailOpen,
			DestinationHost:  "api.anthropic.com",
			Vendor:           "anthropic",
			ServiceCategory:  "ai_code",
			AppName:          "Claude Code",
			UserMessageShown: false,
			Error:            err.Error(),
		}, nil, 3*time.Second)
		return 0
	}

	promptHookLog(logPath, fmt.Sprintf("evaluator responded decision=%s evaluation_id=%s", evalResp.Decision, evalResp.EvaluationID))

	if evalResp.Decision == domain.DecisionBlock {
		_, _ = promptHookPostJSON(promptOutcomeURL, domain.PromptOutcomeRequest{
			EvaluationID:     evalResp.EvaluationID,
			Surface:          domain.CaptureSurfaceClaudeCode,
			Outcome:          domain.CaptureOutcomeBlocked,
			AppName:          "Claude Code",
			UserMessageShown: true,
		}, nil, 3*time.Second)

		message := strings.TrimSpace(evalResp.Message)
		if message == "" {
			message = "Blocked by organization policy."
		}
		_, _ = fmt.Fprintln(os.Stderr, "[Themisto] "+message)
		return 2
	}

	if evalResp.Decision == domain.DecisionAlert {
		_, _ = promptHookPostJSON(promptOutcomeURL, domain.PromptOutcomeRequest{
			EvaluationID:     evalResp.EvaluationID,
			Surface:          domain.CaptureSurfaceClaudeCode,
			Outcome:          domain.CaptureOutcomeAllowed,
			AppName:          "Claude Code",
			UserMessageShown: true,
		}, nil, 3*time.Second)

		message := strings.TrimSpace(evalResp.Message)
		if message == "" {
			message = "Themisto flagged this prompt for sensitive content."
		}
		promptHookLog(logPath, fmt.Sprintf("alert decision evaluation_id=%s -- allow with warning", evalResp.EvaluationID))
		_, _ = fmt.Fprintln(os.Stderr, "[Themisto] Warning: "+message)
		return 0
	}

	_, _ = promptHookPostJSON(promptOutcomeURL, domain.PromptOutcomeRequest{
		EvaluationID:     evalResp.EvaluationID,
		Surface:          domain.CaptureSurfaceClaudeCode,
		Outcome:          domain.CaptureOutcomeAllowed,
		AppName:          "Claude Code",
		UserMessageShown: false,
	}, nil, 3*time.Second)
	return 0
}

func bytesTrimSpace(v []byte) []byte {
	return []byte(strings.TrimSpace(string(v)))
}
