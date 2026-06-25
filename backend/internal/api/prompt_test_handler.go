package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type promptPolicyTestRequest struct {
	PromptText      string `json:"prompt_text"`
	Surface         string `json:"surface"`
	AppName         string `json:"app_name"`
	DestinationURL  string `json:"destination_url"`
	DestinationHost string `json:"destination_host"`
	DestinationPath string `json:"destination_path"`
	Vendor          string `json:"vendor"`
	ServiceCategory string `json:"service_category"`
}

func (s *Server) handlePromptPolicyTest(w http.ResponseWriter, r *http.Request) {
	user := getUserFromContext(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}

	var req promptPolicyTestRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON body")
		return
	}
	req.PromptText = strings.TrimSpace(req.PromptText)
	if req.PromptText == "" {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "prompt_text is required")
		return
	}
	if strings.TrimSpace(req.Surface) == "" {
		req.Surface = "browser_chromium"
	}
	if strings.TrimSpace(req.DestinationURL) == "" {
		req.DestinationURL = "https://chatgpt.com/"
	}
	if strings.TrimSpace(req.Vendor) == "" {
		req.Vendor = "openai"
	}
	if strings.TrimSpace(req.ServiceCategory) == "" {
		req.ServiceCategory = "ai_llm"
	}

	target := strings.TrimSpace(s.promptTestURL)
	if target == "" {
		writeError(w, http.StatusServiceUnavailable, "PROMPT_EVALUATOR_UNCONFIGURED", "prompt evaluator URL is not configured")
		return
	}

	payload := map[string]interface{}{
		"prompt_text":      req.PromptText,
		"surface":          req.Surface,
		"app_name":         req.AppName,
		"destination_url":  req.DestinationURL,
		"destination_host": req.DestinationHost,
		"destination_path": req.DestinationPath,
		"vendor":           req.Vendor,
		"service_category": req.ServiceCategory,
		"metadata": map[string]string{
			"semantic_test": "true",
			"admin_user":    user.Email,
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}

	ctx := r.Context()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "PROMPT_EVALUATOR_UNAVAILABLE", fmt.Sprintf("real prompt evaluator unavailable: %v", err))
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var out map[string]interface{}
	_ = json.Unmarshal(respBody, &out)
	if resp.StatusCode >= 400 {
		writeError(w, http.StatusBadGateway, "PROMPT_EVALUATOR_FAILED", strings.TrimSpace(string(respBody)))
		return
	}
	if out == nil {
		writeError(w, http.StatusBadGateway, "PROMPT_EVALUATOR_FAILED", "prompt evaluator returned invalid JSON")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"source": "real_prompt_evaluator",
		"result": out,
	})
}
