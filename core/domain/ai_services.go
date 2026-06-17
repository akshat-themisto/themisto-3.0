package domain

import (
	"net"
	"net/url"
	"strings"
)

// AIServiceInfo describes a known AI service for shadow AI detection.
type AIServiceInfo struct {
	Vendor     string // short identifier, e.g. "openai"
	Name       string // display name, e.g. "OpenAI"
	Category   string // "ai_llm", "ai_image", "ai_code", "ai_search"
	RiskTier   string // "high", "medium", "low"
	Sanctioned bool   // whether sanctioned by default (org can override)
}

// knownAIServices maps exact hostnames to their AI service metadata.
var knownAIServices = map[string]AIServiceInfo{
	"api.openai.com":                      {Vendor: "openai", Name: "OpenAI API", Category: "ai_llm", RiskTier: "high"},
	"chat.openai.com":                     {Vendor: "openai", Name: "ChatGPT", Category: "ai_llm", RiskTier: "high"},
	"chatgpt.com":                         {Vendor: "openai", Name: "ChatGPT", Category: "ai_llm", RiskTier: "high"},
	"platform.openai.com":                 {Vendor: "openai", Name: "OpenAI Platform", Category: "ai_llm", RiskTier: "high"},
	"api.anthropic.com":                   {Vendor: "anthropic", Name: "Claude API", Category: "ai_llm", RiskTier: "high"},
	"claude.ai":                           {Vendor: "anthropic", Name: "Claude", Category: "ai_llm", RiskTier: "high"},
	"generativelanguage.googleapis.com":   {Vendor: "google", Name: "Gemini API", Category: "ai_llm", RiskTier: "high"},
	"content-gemini.googleapis.com":       {Vendor: "google", Name: "Gemini API", Category: "ai_llm", RiskTier: "high"},
	"gemini.google.com":                   {Vendor: "google", Name: "Gemini", Category: "ai_llm", RiskTier: "high"},
	"clients6.google.com":                 {Vendor: "google", Name: "Gemini Web", Category: "ai_llm", RiskTier: "high"},
	"aistudio.google.com":                 {Vendor: "google", Name: "Google AI Studio", Category: "ai_llm", RiskTier: "high"},
	"notebooklm.google.com":               {Vendor: "google", Name: "NotebookLM", Category: "ai_llm", RiskTier: "medium"},
	"bard.google.com":                     {Vendor: "google", Name: "Bard", Category: "ai_llm", RiskTier: "high"},
	"copilot.microsoft.com":               {Vendor: "microsoft", Name: "Copilot", Category: "ai_llm", RiskTier: "medium"},
	"api.githubcopilot.com":               {Vendor: "github", Name: "GitHub Copilot", Category: "ai_code", RiskTier: "high"},
	"copilot-proxy.githubusercontent.com": {Vendor: "github", Name: "GitHub Copilot", Category: "ai_code", RiskTier: "high"},
	"huggingface.co":                      {Vendor: "huggingface", Name: "HuggingFace", Category: "ai_llm", RiskTier: "medium"},
	"api-inference.huggingface.co":        {Vendor: "huggingface", Name: "HuggingFace Inference", Category: "ai_llm", RiskTier: "medium"},
	"api.cohere.ai":                       {Vendor: "cohere", Name: "Cohere", Category: "ai_llm", RiskTier: "high"},
	"api.cohere.com":                      {Vendor: "cohere", Name: "Cohere", Category: "ai_llm", RiskTier: "high"},
	"api.mistral.ai":                      {Vendor: "mistral", Name: "Mistral AI", Category: "ai_llm", RiskTier: "high"},
	"api.together.ai":                     {Vendor: "together", Name: "Together AI", Category: "ai_llm", RiskTier: "high"},
	"api.perplexity.ai":                   {Vendor: "perplexity", Name: "Perplexity API", Category: "ai_llm", RiskTier: "medium"},
	"perplexity.ai":                       {Vendor: "perplexity", Name: "Perplexity", Category: "ai_llm", RiskTier: "medium"},
	"cursor.sh":                           {Vendor: "cursor", Name: "Cursor IDE", Category: "ai_code", RiskTier: "high"},
	"api2.cursor.sh":                      {Vendor: "cursor", Name: "Cursor IDE", Category: "ai_code", RiskTier: "high"},
	"aicursor.com":                        {Vendor: "cursor", Name: "Cursor IDE", Category: "ai_code", RiskTier: "high"},
	"api.stability.ai":                    {Vendor: "stability", Name: "Stability AI", Category: "ai_image", RiskTier: "medium"},
	"midjourney.com":                      {Vendor: "midjourney", Name: "Midjourney", Category: "ai_image", RiskTier: "low"},
	"poe.com":                             {Vendor: "quora", Name: "Poe", Category: "ai_llm", RiskTier: "high"},
	"character.ai":                        {Vendor: "characterai", Name: "Character.AI", Category: "ai_llm", RiskTier: "high"},
	"api.replicate.com":                   {Vendor: "replicate", Name: "Replicate", Category: "ai_llm", RiskTier: "medium"},
	"replicate.com":                       {Vendor: "replicate", Name: "Replicate", Category: "ai_llm", RiskTier: "medium"},
	"writesonic.com":                      {Vendor: "writesonic", Name: "Writesonic", Category: "ai_llm", RiskTier: "medium"},
	"jasper.ai":                           {Vendor: "jasper", Name: "Jasper AI", Category: "ai_llm", RiskTier: "medium"},
	"api.groq.com":                        {Vendor: "groq", Name: "Groq", Category: "ai_llm", RiskTier: "high"},
	"console.groq.com":                    {Vendor: "groq", Name: "Groq Console", Category: "ai_llm", RiskTier: "medium"},
	"api.x.ai":                            {Vendor: "xai", Name: "xAI Grok", Category: "ai_llm", RiskTier: "high"},
	"windsurf.ai":                         {Vendor: "windsurf", Name: "Windsurf", Category: "ai_code", RiskTier: "high"},
	"api.codeium.com":                     {Vendor: "windsurf", Name: "Windsurf", Category: "ai_code", RiskTier: "high"},
}

var vendorOwnedDomains = map[string][]string{
	"anthropic": {"anthropic.com", "claude.ai"},
	"cursor":    {"cursor.sh", "aicursor.com"},
	"github":    {"githubcopilot.com", "githubusercontent.com", "github.com"},
	"google":    {"google.com", "googleapis.com", "googleusercontent.com", "gstatic.com"},
	"openai":    {"openai.com", "chatgpt.com", "oaistatic.com", "oaiusercontent.com"},
	"windsurf":  {"windsurf.ai", "codeium.com"},
}

// NormalizeHost canonicalizes host-like values coming from request targets,
// Origin / Referer headers, or CONNECT metadata.
func NormalizeHost(raw string) string {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return ""
	}
	if strings.Contains(raw, "://") {
		if parsed, err := url.Parse(raw); err == nil && parsed.Host != "" {
			raw = parsed.Host
		}
	}
	if host, port, err := net.SplitHostPort(raw); err == nil {
		if port != "" {
			raw = host
		}
	}
	raw = strings.Trim(raw, "[]")
	raw = strings.TrimSuffix(raw, ".")
	return raw
}

// LookupAIService returns the AIServiceInfo for the given hostname if it is a known AI service.
// It checks exact matches first, then suffix matches for subdomains.
func LookupAIService(host string) (AIServiceInfo, bool) {
	host = NormalizeHost(host)
	if info, ok := knownAIServices[host]; ok {
		return info, true
	}
	// Check if host is a subdomain of a known AI service.
	for known, info := range knownAIServices {
		if strings.HasSuffix(host, "."+known) {
			return info, true
		}
	}
	return AIServiceInfo{}, false
}

// InferAIService expands host-only matching with Origin / Referer hints for
// vendor-owned helper domains used by browser-based AI products.
func InferAIService(host string, signals ...string) (AIServiceInfo, bool) {
	if info, ok := LookupAIService(host); ok {
		return info, true
	}

	host = NormalizeHost(host)
	if host == "" {
		return AIServiceInfo{}, false
	}

	for _, signal := range signals {
		signalHost := NormalizeHost(signal)
		if signalHost == "" {
			continue
		}
		info, ok := LookupAIService(signalHost)
		if !ok {
			continue
		}
		if vendorOwnsHost(info.Vendor, host) {
			return info, true
		}
	}

	return AIServiceInfo{}, false
}

func vendorOwnsHost(vendor, host string) bool {
	host = NormalizeHost(host)
	if host == "" {
		return false
	}
	for _, owned := range vendorOwnedDomains[strings.ToLower(strings.TrimSpace(vendor))] {
		owned = NormalizeHost(owned)
		if owned == "" {
			continue
		}
		if host == owned || strings.HasSuffix(host, "."+owned) {
			return true
		}
	}
	return false
}
