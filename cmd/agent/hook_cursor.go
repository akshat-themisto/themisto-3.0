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

type cursorHookInput struct {
	Prompt         string          `json:"prompt"`
	ConversationID string          `json:"conversation_id"`
	GenerationID   string          `json:"generation_id"`
	Model          string          `json:"model"`
	HookEventName  string          `json:"hook_event_name"`
	CursorVersion  string          `json:"cursor_version"`
	UserEmail      string          `json:"user_email"`
	TranscriptPath string          `json:"transcript_path"`
	Attachments    json.RawMessage `json:"attachments"`
	WorkspaceRoots json.RawMessage `json:"workspace_roots"`
}

type cursorHookOutput struct {
	Continue    bool   `json:"continue"`
	UserMessage string `json:"user_message,omitempty"`
}

func runCursorHook() int {
	logPath := promptHookLogPath("cursor-hook.log")
	promptHookLog(logPath, fmt.Sprintf("hook invoked pid=%d", os.Getpid()))

	inputJSON, err := io.ReadAll(os.Stdin)
	if err != nil {
		promptHookLog(logPath, fmt.Sprintf("stdin read error=%v -- allow", err))
		writeCursorHookOutput(cursorHookOutput{Continue: true})
		return 0
	}
	promptHookLog(logPath, fmt.Sprintf("stdin received bytes=%d", len(inputJSON)))

	if len(bytesTrimSpace(inputJSON)) == 0 {
		promptHookLog(logPath, "empty stdin -- allow")
		writeCursorHookOutput(cursorHookOutput{Continue: true})
		return 0
	}

	var payload cursorHookInput
	if err := json.Unmarshal(inputJSON, &payload); err != nil {
		promptHookLog(logPath, fmt.Sprintf("invalid json error=%v -- allow", err))
		writeCursorHookOutput(cursorHookOutput{Continue: true})
		return 0
	}

	promptText := strings.TrimSpace(payload.Prompt)
	if promptText == "" {
		promptHookLog(logPath, "no prompt field -- allow")
		writeCursorHookOutput(cursorHookOutput{Continue: true})
		return 0
	}

	metadata := map[string]string{
		"conversation_id": payload.ConversationID,
		"generation_id":   payload.GenerationID,
		"model":           payload.Model,
		"hook_event":      payload.HookEventName,
		"cursor_version":  payload.CursorVersion,
		"user_email":      payload.UserEmail,
		"transcript_path": payload.TranscriptPath,
	}
	if raw := trimJSONString(payload.Attachments); raw != "" {
		metadata["attachments_json"] = raw
	}
	if raw := trimJSONString(payload.WorkspaceRoots); raw != "" {
		metadata["workspace_roots_json"] = raw
	}

	evalReq := domain.PromptEvaluationRequest{
		PromptText:      promptText,
		Surface:         domain.CaptureSurfaceCursor,
		AppName:         "Cursor",
		Vendor:          "cursor",
		ServiceCategory: "ai_code",
		DestinationHost: "api2.cursor.sh",
		Metadata:        metadata,
	}

	var evalResp domain.PromptEvaluationResponse
	status, err := promptHookPostJSON(promptEvaluateURL, evalReq, &evalResp, 10*time.Second)
	if err != nil {
		promptHookLog(logPath, fmt.Sprintf("evaluator error status=%d error=%v -- allow", status, err))
		_, _ = promptHookPostJSON(promptOutcomeURL, domain.PromptOutcomeRequest{
			Surface:          domain.CaptureSurfaceCursor,
			Outcome:          domain.CaptureOutcomeDegradedFailOpen,
			DestinationHost:  "api2.cursor.sh",
			Vendor:           "cursor",
			ServiceCategory:  "ai_code",
			AppName:          "Cursor",
			UserMessageShown: false,
			Error:            err.Error(),
		}, nil, 3*time.Second)
		writeCursorHookOutput(cursorHookOutput{Continue: true})
		return 0
	}

	promptHookLog(logPath, fmt.Sprintf("evaluator responded decision=%s evaluation_id=%s", evalResp.Decision, evalResp.EvaluationID))

	if evalResp.Decision == domain.DecisionBlock {
		_, _ = promptHookPostJSON(promptOutcomeURL, domain.PromptOutcomeRequest{
			EvaluationID:     evalResp.EvaluationID,
			Surface:          domain.CaptureSurfaceCursor,
			Outcome:          domain.CaptureOutcomeBlocked,
			AppName:          "Cursor",
			UserMessageShown: true,
		}, nil, 3*time.Second)

		message := strings.TrimSpace(evalResp.Message)
		if message == "" {
			message = "Themisto blocked this prompt: sensitive data detected."
		}
		message += " Start a new chat -- this conversation may still show the blocked content in Cursor's local history. Themisto is cleaning up local storage automatically."
		writeCursorHookOutput(cursorHookOutput{
			Continue:    false,
			UserMessage: message,
		})
		return 0
	}

	if evalResp.Decision == domain.DecisionAlert {
		_, _ = promptHookPostJSON(promptOutcomeURL, domain.PromptOutcomeRequest{
			EvaluationID:     evalResp.EvaluationID,
			Surface:          domain.CaptureSurfaceCursor,
			Outcome:          domain.CaptureOutcomeAllowed,
			AppName:          "Cursor",
			UserMessageShown: true,
		}, nil, 3*time.Second)

		message := strings.TrimSpace(evalResp.Message)
		if message == "" {
			message = "Themisto flagged this prompt for sensitive content. Review before sending."
		}
		promptHookLog(logPath, fmt.Sprintf("alert decision evaluation_id=%s -- allow with warning", evalResp.EvaluationID))
		writeCursorHookOutput(cursorHookOutput{Continue: true, UserMessage: message})
		return 0
	}

	_, _ = promptHookPostJSON(promptOutcomeURL, domain.PromptOutcomeRequest{
		EvaluationID:     evalResp.EvaluationID,
		Surface:          domain.CaptureSurfaceCursor,
		Outcome:          domain.CaptureOutcomeAllowed,
		AppName:          "Cursor",
		UserMessageShown: false,
	}, nil, 3*time.Second)

	writeCursorHookOutput(cursorHookOutput{Continue: true})
	return 0
}

func writeCursorHookOutput(output cursorHookOutput) {
	raw, err := json.Marshal(output)
	if err != nil {
		raw = []byte(`{"continue":true}`)
	}
	_, _ = os.Stdout.Write(append(raw, '\n'))
}

func trimJSONString(raw json.RawMessage) string {
	text := strings.TrimSpace(string(raw))
	if text == "" || text == "null" {
		return ""
	}
	return text
}
