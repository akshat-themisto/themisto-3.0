package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/themisto/agent/core/domain"
)

type copilotSessionState struct {
	SessionID    string `json:"session_id"`
	EvaluationID string `json:"evaluation_id"`
	Reason       string `json:"reason"`
	CreatedAtUTC string `json:"created_at_utc"`
}

func runCopilotHook() int {
	logPath := promptHookLogPath("copilot-hook.log")
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

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(inputJSON, &payload); err != nil {
		promptHookLog(logPath, fmt.Sprintf("invalid json error=%v -- allow", err))
		return 0
	}

	hookName := firstHookString(payload, "hook_event_name", "hookEventName")
	sessionID := firstHookString(payload, "session_id", "sessionId")
	if hookName == "" {
		switch {
		case firstHookString(payload, "prompt") != "":
			hookName = "UserPromptSubmit"
		case firstHookString(payload, "tool_name", "toolName") != "":
			hookName = "PreToolUse"
		}
	}

	switch hookName {
	case "UserPromptSubmit":
		return runCopilotUserPromptSubmit(logPath, payload, sessionID)
	case "PreToolUse":
		return runCopilotPreToolUse(logPath, payload, sessionID)
	default:
		promptHookLog(logPath, fmt.Sprintf("unknown hook %q -- allow", hookName))
		return 0
	}
}

func runCopilotUserPromptSubmit(logPath string, payload map[string]json.RawMessage, sessionID string) int {
	promptText := firstHookString(payload, "prompt")
	if promptText == "" {
		removeCopilotSessionState(sessionID)
		promptHookLog(logPath, "no prompt field -- allow")
		return 0
	}

	evalReq := domain.PromptEvaluationRequest{
		PromptText:      promptText,
		Surface:         domain.CaptureSurfaceGitHubCopilot,
		AppName:         "GitHub Copilot",
		Vendor:          "github",
		ServiceCategory: "ai_code",
		DestinationHost: "api.githubcopilot.com",
		Metadata: map[string]string{
			"hook_event_name": firstHookString(payload, "hook_event_name", "hookEventName"),
			"session_id":      sessionID,
			"transcript_path": firstHookString(payload, "transcript_path"),
			"cwd":             firstHookString(payload, "cwd"),
		},
	}

	var evalResp domain.PromptEvaluationResponse
	status, err := promptHookPostJSON(promptEvaluateURL, evalReq, &evalResp, 10*time.Second)
	if err != nil {
		promptHookLog(logPath, fmt.Sprintf("evaluator error status=%d error=%v -- audit fail-open", status, err))
		removeCopilotSessionState(sessionID)
		_, _ = promptHookPostJSON(promptOutcomeURL, domain.PromptOutcomeRequest{
			Surface:          domain.CaptureSurfaceGitHubCopilot,
			Outcome:          domain.CaptureOutcomeDegradedFailOpen,
			DestinationHost:  "api.githubcopilot.com",
			Vendor:           "github",
			ServiceCategory:  "ai_code",
			AppName:          "GitHub Copilot",
			UserMessageShown: false,
			Error:            err.Error(),
		}, nil, 3*time.Second)
		return 0
	}

	if evalResp.Decision == domain.DecisionBlock {
		writeCopilotSessionState(copilotSessionState{
			SessionID:    sessionID,
			EvaluationID: evalResp.EvaluationID,
			Reason:       evalResp.Message,
			CreatedAtUTC: time.Now().UTC().Format(time.RFC3339),
		})
		_, _ = promptHookPostJSON(promptOutcomeURL, domain.PromptOutcomeRequest{
			EvaluationID:     evalResp.EvaluationID,
			Surface:          domain.CaptureSurfaceGitHubCopilot,
			Outcome:          domain.CaptureOutcomeWouldBlock,
			DestinationHost:  "api.githubcopilot.com",
			Vendor:           "github",
			ServiceCategory:  "ai_code",
			AppName:          "GitHub Copilot",
			UserMessageShown: false,
		}, nil, 3*time.Second)
		return 0
	}

	removeCopilotSessionState(sessionID)
	_, _ = promptHookPostJSON(promptOutcomeURL, domain.PromptOutcomeRequest{
		EvaluationID:     evalResp.EvaluationID,
		Surface:          domain.CaptureSurfaceGitHubCopilot,
		Outcome:          domain.CaptureOutcomeAllowed,
		DestinationHost:  "api.githubcopilot.com",
		Vendor:           "github",
		ServiceCategory:  "ai_code",
		AppName:          "GitHub Copilot",
		UserMessageShown: false,
	}, nil, 3*time.Second)
	return 0
}

func runCopilotPreToolUse(logPath string, payload map[string]json.RawMessage, sessionID string) int {
	toolName := firstHookString(payload, "tool_name", "toolName")
	if toolName != "run_in_terminal" {
		return 0
	}

	state, ok := readCopilotSessionState(sessionID)
	if !ok {
		return 0
	}

	reason := strings.TrimSpace(state.Reason)
	if reason == "" {
		reason = "A previous Copilot prompt matched Themisto policy, so terminal execution is denied for this session."
	}

	deny := map[string]interface{}{
		"hookSpecificOutput": map[string]interface{}{
			"hookEventName":            "PreToolUse",
			"permissionDecision":       "deny",
			"permissionDecisionReason": reason,
		},
	}
	raw, _ := json.Marshal(deny)
	_, _ = os.Stdout.Write(append(raw, '\n'))
	promptHookLog(logPath, fmt.Sprintf("denying tool %s for session %s", toolName, sessionID))
	return 0
}

func firstHookString(payload map[string]json.RawMessage, keys ...string) string {
	for _, key := range keys {
		raw, ok := payload[key]
		if !ok || len(raw) == 0 {
			continue
		}
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

func copilotStateDir() string {
	if dir := strings.TrimSpace(os.Getenv("THEMISTO_COPILOT_STATE_DIR")); dir != "" {
		return dir
	}
	if runtime.GOOS == "windows" {
		if localAppData := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); localAppData != "" {
			return filepath.Join(localAppData, "Themisto", "hooks", "copilot", "copilot-state")
		}
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if runtime.GOOS == "darwin" {
			return filepath.Join(home, "Library", "Application Support", "Themisto", "hooks", "copilot", "copilot-state")
		}
		return filepath.Join(home, ".local", "share", "themisto", "hooks", "copilot", "copilot-state")
	}
	return filepath.Join(os.TempDir(), "themisto-copilot-state")
}

func copilotSessionStatePath(sessionID string) string {
	safe := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= 'A' && r <= 'Z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '.', r == '-', r == '_':
			return r
		default:
			return '_'
		}
	}, sessionID)
	return filepath.Join(copilotStateDir(), safe+".json")
}

func writeCopilotSessionState(state copilotSessionState) {
	if strings.TrimSpace(state.SessionID) == "" {
		return
	}
	dir := copilotStateDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return
	}
	_ = os.WriteFile(copilotSessionStatePath(state.SessionID), raw, 0644)
}

func readCopilotSessionState(sessionID string) (copilotSessionState, bool) {
	if strings.TrimSpace(sessionID) == "" {
		return copilotSessionState{}, false
	}
	raw, err := os.ReadFile(copilotSessionStatePath(sessionID))
	if err != nil {
		return copilotSessionState{}, false
	}
	var state copilotSessionState
	if err := json.Unmarshal(raw, &state); err != nil {
		return copilotSessionState{}, false
	}
	if createdAt, err := time.Parse(time.RFC3339, state.CreatedAtUTC); err == nil {
		if time.Since(createdAt.UTC()) > time.Hour {
			removeCopilotSessionState(sessionID)
			return copilotSessionState{}, false
		}
	}
	return state, true
}

func removeCopilotSessionState(sessionID string) {
	if strings.TrimSpace(sessionID) == "" {
		return
	}
	_ = os.Remove(copilotSessionStatePath(sessionID))
}
