// Package dlp provides deterministic data loss prevention scanning for request
// bodies. It inspects structured text payloads (JSON, form-data, plain text,
// and selected file types) and classifies findings into severity levels.
package dlp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/themisto/agent/core/domain"
)

// MaxBodySize is the maximum body size in bytes that will be scanned.
const MaxBodySize = 256 * 1024 // 256 KB

const maxMatches = 20

var suppressedPathTokens = []string{
	"/telemetry",
	"/analytics",
	"/events",
	"/event",
	"/track",
	"/collect",
	"/metrics",
}

// ShouldSuppressPath returns true when request path is low-value telemetry noise.
func ShouldSuppressPath(path string) bool {
	path = strings.ToLower(strings.TrimSpace(path))
	if path == "" {
		return false
	}
	for _, token := range suppressedPathTokens {
		if strings.Contains(path, token) {
			return true
		}
	}
	return false
}

type pattern struct {
	name string
	re   *regexp.Regexp
}

type extractedField struct {
	name string
	text string
}

// Scanner scans byte slices for sensitive content patterns.
type Scanner struct {
	piiPatterns        []pattern
	credentialPatterns []pattern
	codePatterns       []pattern
	customKeywords     []string
}

var defaultScanner = newDefaultScanner()

func newDefaultScanner() *Scanner {
	s := &Scanner{}

	s.piiPatterns = []pattern{
		{name: "ssn", re: regexp.MustCompile(`(?:^|[^a-zA-Z0-9_-])(?:\d{3}-\d{2}-\d{4}|\d{9})(?:$|[^a-zA-Z0-9_-])`)},
		{name: "credit_card", re: regexp.MustCompile(`\b(?:4[0-9]{12}(?:[0-9]{3})?|5[1-5][0-9]{14}|3[47][0-9]{13}|6(?:011|5[0-9]{2})[0-9]{12}|3(?:0[0-5]|[68][0-9])[0-9]{11}|(?:2131|1800|35\d{3})\d{11})\b`)},
		{name: "email", re: regexp.MustCompile(`\b[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}\b`)},
		{name: "phone_us", re: regexp.MustCompile(`\b(?:\+?1[-.\s]?)?\(?[2-9]\d{2}\)?[-.\s]?\d{3}[-.\s]?\d{4}\b`)},
		{name: "uk_ni_number", re: regexp.MustCompile(`\b[A-CEGHJ-PR-TW-Z]{2}\d{6}[A-D]\b`)},
		{name: "passport", re: regexp.MustCompile(`\b[A-Z]{1,2}[0-9]{6,9}\b`)},
	}

	s.credentialPatterns = []pattern{
		{name: "api_key_environment", re: regexp.MustCompile(`\bsk-(?:live|test|prod|production|dev|stage|staging)-[a-zA-Z0-9\-_]{8,}\b`)},
		{name: "api_key_openai", re: regexp.MustCompile(`\bsk-(?:[a-zA-Z0-9]+-)?[a-zA-Z0-9\-_]{16,}\b`)},
		{name: "api_key_anthropic", re: regexp.MustCompile(`\bsk-ant-[a-zA-Z0-9\-_]{20,}\b`)},
		{name: "github_token", re: regexp.MustCompile(`\bghp_[a-zA-Z0-9]{36,}\b`)},
		{name: "github_oauth_token", re: regexp.MustCompile(`\bgho_[a-zA-Z0-9]{36,}\b`)},
		{name: "github_app_token", re: regexp.MustCompile(`\bghu_[a-zA-Z0-9]{36,}\b`)},
		{name: "aws_access_key", re: regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`)},
		// Tightened: require explicit context keys to reduce random-token false positives.
		{name: "aws_secret_key", re: regexp.MustCompile(`(?i)aws(?:_|\s)?secret(?:_|\s)?access(?:_|\s)?key\s*[:=]\s*['"]?[a-zA-Z0-9+/=]{40}['"]?`)},
		{name: "private_key_block", re: regexp.MustCompile(`-----BEGIN (?:RSA |EC |DSA |OPENSSH )?PRIVATE KEY-----`)},
		{name: "google_api_key", re: regexp.MustCompile(`\bAIza[a-zA-Z0-9\-_]{35}\b`)},
		{name: "slack_token", re: regexp.MustCompile(`\bxox[baprs]-[a-zA-Z0-9\-]{10,}\b`)},
		{name: "stripe_key", re: regexp.MustCompile(`\b(?:sk|pk)_(?:live|test)_[a-zA-Z0-9]{24,}\b`)},
		{name: "jwt_token", re: regexp.MustCompile(`\beyJ[a-zA-Z0-9\-_]+\.eyJ[a-zA-Z0-9\-_]+\.[a-zA-Z0-9\-_]+\b`)},
		{name: "generic_secret", re: regexp.MustCompile(`(?i)(?:secret|password|passwd|token|api[-_]?key|authorization)\s*[:=]\s*['"]?([a-zA-Z0-9+/=\-_]{16,})['"]?`)},
	}

	s.codePatterns = []pattern{
		{name: "python_import", re: regexp.MustCompile(`(?m)^(?:import|from)\s+[a-zA-Z_][a-zA-Z0-9_.]*`)},
		{name: "go_import", re: regexp.MustCompile(`(?m)^(?:package\s+\w+|import\s+\()`)},
		{name: "js_require", re: regexp.MustCompile(`(?m)(?:require\(['"][^'"]+['"]\)|import\s+\S+\s+from\s+['"][^'"]+['"])`)},
		{name: "function_def", re: regexp.MustCompile(`(?m)(?:func\s+\w+|def\s+\w+|function\s+\w+|const\s+\w+\s*=\s*(?:async\s+)?(?:\([^)]*\)|[^=]+)\s*=>)\s*\(`)},
		{name: "class_def", re: regexp.MustCompile(`(?m)(?:class\s+[A-Z][a-zA-Z0-9_]*|struct\s+[A-Z][a-zA-Z0-9_]*)`)},
		{name: "sql_statement", re: regexp.MustCompile(`(?mi)(?:SELECT\s+.+\s+FROM|INSERT\s+INTO|UPDATE\s+\w+\s+SET|DELETE\s+FROM|CREATE\s+TABLE)\s+\w+`)},
		{name: "shebang", re: regexp.MustCompile(`(?m)^#!(?:/usr/bin/env\s+|/usr/bin/|/bin/)(?:python|node|bash|sh|ruby|perl)\b`)},
	}

	return s
}

// Scan keeps backward compatibility for text-only scanning.
func Scan(body []byte, customKeywords []string) domain.DLPInfo {
	return defaultScanner.ScanRequest("", "", body, customKeywords)
}

// ScanRequest scans an HTTP request body using content-type aware extraction.
func ScanRequest(contentType, path string, body []byte, customKeywords []string) domain.DLPInfo {
	return defaultScanner.ScanRequest(contentType, path, body, customKeywords)
}

// Scan inspects body for sensitive content and returns a DLPInfo.
func (s *Scanner) Scan(body []byte, customKeywords []string) domain.DLPInfo {
	return s.ScanRequest("", "", body, customKeywords)
}

// ScanRequest inspects request payloads with structure-aware extraction.
func (s *Scanner) ScanRequest(contentType, _ string, body []byte, customKeywords []string) domain.DLPInfo {
	info := domain.DLPInfo{
		Scanned:       true,
		Severity:      "low",
		ContentType:   normalizeContentType(contentType),
		MatchedFields: []string{},
	}
	if len(body) == 0 {
		info.ClassificationReason = "no_match"
		return info
	}

	if len(body) > MaxBodySize {
		body = body[:MaxBodySize]
	}

	fields, fileCount := extractFieldsForInspection(contentType, body)
	info.FileCount = fileCount
	if len(fields) == 0 {
		text := normalizeExtractedText(body)
		if text != "" {
			fields = append(fields, extractedField{name: "body", text: text})
		}
	}

	for _, f := range fields {
		s.scanField(&info, f.name, []byte(f.text), customKeywords)
		if len(info.Matches) >= maxMatches {
			break
		}
	}

	info.BodySample = buildBodySample(fields, body)
	finalizeClassification(&info)
	return info
}

func (s *Scanner) scanField(info *domain.DLPInfo, fieldName string, body []byte, customKeywords []string) {
	if len(body) == 0 || len(info.Matches) >= maxMatches {
		return
	}

	for _, p := range s.piiPatterns {
		if loc := p.re.FindIndex(body); loc != nil {
			info.ContainsPII = true
			info.Matches = append(info.Matches, domain.DLPMatch{
				Type:    domain.DLPMatchPII,
				Pattern: p.name,
				Excerpt: safeExcerpt(body, loc),
			})
			info.MatchedFields = append(info.MatchedFields, fieldName)
			if len(info.Matches) >= maxMatches {
				return
			}
		}
	}

	for _, p := range s.credentialPatterns {
		if loc := p.re.FindIndex(body); loc != nil {
			_ = loc
			info.ContainsCredentials = true
			info.Matches = append(info.Matches, domain.DLPMatch{
				Type:    domain.DLPMatchCredentials,
				Pattern: p.name,
				Excerpt: "[REDACTED]",
			})
			info.MatchedFields = append(info.MatchedFields, fieldName)
			if len(info.Matches) >= maxMatches {
				return
			}
		}
	}

	codeHits := 0
	for _, p := range s.codePatterns {
		if p.re.Match(body) {
			codeHits++
		}
	}
	if codeHits >= 2 {
		if !info.ContainsSourceCode {
			info.ContainsSourceCode = true
			info.Matches = append(info.Matches, domain.DLPMatch{
				Type:    domain.DLPMatchSourceCode,
				Pattern: "source_code_heuristic",
				Excerpt: "",
			})
		}
		info.MatchedFields = append(info.MatchedFields, fieldName)
	}

	bodyLower := bytes.ToLower(body)
	for _, kw := range customKeywords {
		kw = strings.TrimSpace(kw)
		if kw == "" {
			continue
		}
		if bytes.Contains(bodyLower, []byte(strings.ToLower(kw))) {
			info.Matches = append(info.Matches, domain.DLPMatch{
				Type:    domain.DLPMatchKeyword,
				Pattern: "keyword:" + kw,
				Excerpt: "",
			})
			info.MatchedFields = append(info.MatchedFields, fieldName)
			if len(info.Matches) >= maxMatches {
				return
			}
		}
	}
}

func normalizeContentType(contentType string) string {
	contentType = strings.TrimSpace(strings.ToLower(contentType))
	if contentType == "" {
		return "text/plain"
	}
	if mediaType, _, err := mime.ParseMediaType(contentType); err == nil && mediaType != "" {
		return mediaType
	}
	if i := strings.Index(contentType, ";"); i > 0 {
		return strings.TrimSpace(contentType[:i])
	}
	return contentType
}

func extractFieldsForInspection(contentType string, body []byte) ([]extractedField, int) {
	ct := normalizeContentType(contentType)
	switch ct {
	case "application/json", "text/json":
		return parseJSONFields(body, "json"), 0
	case "application/x-www-form-urlencoded":
		return parseFormFields(body), 0
	case "multipart/form-data":
		return parseMultipartFields(contentType, body)
	case "application/pdf":
		text := extractPDFTextLayer(body)
		if text == "" {
			return nil, 1
		}
		return []extractedField{{name: "body", text: text}}, 1
	default:
		if strings.HasPrefix(ct, "text/") || ct == "application/xml" || ct == "text/xml" || ct == "application/yaml" || ct == "application/x-yaml" || ct == "text/yaml" {
			text := normalizeExtractedText(body)
			if text == "" {
				return nil, 0
			}
			return []extractedField{{name: "body", text: text}}, 0
		}
		text := normalizeExtractedText(body)
		if text == "" {
			return nil, 0
		}
		return []extractedField{{name: "body", text: text}}, 0
	}
}

func parseJSONFields(body []byte, prefix string) []extractedField {
	var decoded interface{}
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil
	}
	out := make([]extractedField, 0, 16)
	collectJSONStrings(decoded, prefix, &out)
	return dedupeFields(out)
}

func collectJSONStrings(v interface{}, path string, out *[]extractedField) {
	switch t := v.(type) {
	case map[string]interface{}:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			next := k
			if path != "" {
				next = path + "." + k
			}
			collectJSONStrings(t[k], next, out)
		}
	case []interface{}:
		for i, item := range t {
			next := fmt.Sprintf("%s[%d]", path, i)
			collectJSONStrings(item, next, out)
		}
	case string:
		txt := strings.TrimSpace(t)
		if txt == "" {
			return
		}
		field := path
		if field == "" {
			field = "json"
		}
		*out = append(*out, extractedField{name: field, text: txt})
	}
}

func parseFormFields(body []byte) []extractedField {
	values, err := url.ParseQuery(string(body))
	if err != nil {
		return nil
	}
	out := make([]extractedField, 0, len(values))
	for k, arr := range values {
		for _, v := range arr {
			v = strings.TrimSpace(v)
			if v == "" {
				continue
			}
			out = append(out, extractedField{name: "form." + k, text: v})
		}
	}
	return dedupeFields(out)
}

func parseMultipartFields(contentType string, body []byte) ([]extractedField, int) {
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return nil, 0
	}
	boundary := params["boundary"]
	if boundary == "" {
		return nil, 0
	}

	r := multipart.NewReader(bytes.NewReader(body), boundary)
	out := make([]extractedField, 0, 16)
	fileCount := 0
	partCount := 0
	for {
		if partCount >= 64 {
			break
		}
		part, err := r.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
		partCount++

		data, _ := io.ReadAll(io.LimitReader(part, 128*1024))
		name := strings.TrimSpace(part.FormName())
		if name == "" {
			name = "multipart"
		}
		filename := strings.TrimSpace(part.FileName())
		if filename == "" {
			text := normalizeExtractedText(data)
			if text != "" {
				out = append(out, extractedField{name: "multipart." + name, text: text})
			}
			continue
		}

		fileCount++
		if !isSupportedTextFile(filename, part.Header.Get("Content-Type")) {
			continue
		}

		fileFieldPrefix := "file:" + filename
		ext := strings.ToLower(filepath.Ext(filename))
		ct := normalizeContentType(part.Header.Get("Content-Type"))
		if ext == ".json" || ct == "application/json" || ct == "text/json" {
			jsonFields := parseJSONFields(data, fileFieldPrefix)
			out = append(out, jsonFields...)
			continue
		}
		if ext == ".pdf" || ct == "application/pdf" {
			text := extractPDFTextLayer(data)
			if text != "" {
				out = append(out, extractedField{name: fileFieldPrefix, text: text})
			}
			continue
		}
		text := normalizeExtractedText(data)
		if text != "" {
			out = append(out, extractedField{name: fileFieldPrefix, text: text})
		}
	}

	return dedupeFields(out), fileCount
}

func isSupportedTextFile(filename, contentType string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".txt", ".json", ".csv", ".md", ".markdown", ".go", ".py", ".js", ".jsx", ".ts", ".tsx",
		".java", ".rb", ".rs", ".c", ".cc", ".cpp", ".h", ".hpp", ".cs", ".php", ".swift", ".kt", ".kts",
		".sql", ".xml", ".yaml", ".yml", ".pdf", ".sh", ".bash", ".zsh":
		return true
	}
	ct := normalizeContentType(contentType)
	if strings.HasPrefix(ct, "text/") {
		return true
	}
	switch ct {
	case "application/json", "text/json", "application/xml", "text/xml", "application/pdf", "application/yaml", "application/x-yaml", "text/yaml":
		return true
	default:
		return false
	}
}

func extractPDFTextLayer(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	// Heuristic extraction from PDF text operators (Tj/TJ). OCR is intentionally out of scope.
	re := regexp.MustCompile(`\(([^\)]{1,512})\)\s*T[Jj]`)
	matches := re.FindAllSubmatch(body, 200)
	if len(matches) > 0 {
		parts := make([]string, 0, len(matches))
		for _, m := range matches {
			t := strings.TrimSpace(string(m[1]))
			if t != "" {
				t = strings.ReplaceAll(t, `\\(`, "(")
				t = strings.ReplaceAll(t, `\\)`, ")")
				parts = append(parts, t)
			}
		}
		return strings.TrimSpace(strings.Join(parts, " "))
	}
	return normalizeExtractedText(body)
}

func normalizeExtractedText(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	total := len(body)
	if total == 0 {
		return ""
	}
	nonPrintable := 0
	for _, b := range body {
		if b == 0 {
			return ""
		}
		if (b < 0x20 || b > 0x7e) && b != '\n' && b != '\r' && b != '\t' {
			nonPrintable++
		}
	}
	if float64(nonPrintable)/float64(total) > 0.30 {
		return ""
	}
	text := strings.TrimSpace(string(body))
	if len(text) > 64*1024 {
		text = text[:64*1024]
	}
	return text
}

func buildBodySample(fields []extractedField, fallback []byte) string {
	if len(fields) == 0 {
		return normalizeExtractedText(fallback)
	}
	var sb strings.Builder
	for i, f := range fields {
		if i >= 3 {
			break
		}
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		line := strings.TrimSpace(f.text)
		if len(line) > 1200 {
			line = line[:1200]
		}
		if f.name != "" {
			sb.WriteString(f.name)
			sb.WriteString(": ")
		}
		sb.WriteString(line)
	}
	out := sb.String()
	if len(out) > 16*1024 {
		out = out[:16*1024]
	}
	return out
}

func dedupeFields(in []extractedField) []extractedField {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]extractedField, 0, len(in))
	for _, f := range in {
		name := strings.TrimSpace(f.name)
		text := strings.TrimSpace(f.text)
		if text == "" {
			continue
		}
		key := name + "\x00" + text
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, extractedField{name: name, text: text})
	}
	return out
}

func finalizeClassification(info *domain.DLPInfo) {
	if len(info.Matches) == 0 {
		info.Severity = "low"
		info.ClassificationReason = "no_match"
		info.MatchedFields = []string{}
		return
	}

	hasKeyword := false
	for _, m := range info.Matches {
		if m.Type == domain.DLPMatchKeyword {
			hasKeyword = true
			break
		}
	}

	switch {
	case info.ContainsCredentials:
		info.Severity = "critical"
		info.ClassificationReason = "credentials_or_secrets_detected"
	case info.ContainsPII && info.ContainsSourceCode:
		info.Severity = "medium"
		info.ClassificationReason = "pii_and_source_code_detected"
	case info.ContainsPII:
		info.Severity = "medium"
		info.ClassificationReason = "pii_detected"
	case info.ContainsSourceCode:
		info.Severity = "medium"
		info.ClassificationReason = "source_code_detected"
	case hasKeyword:
		info.Severity = "low"
		info.ClassificationReason = "keyword_policy_match"
	default:
		info.Severity = "low"
		info.ClassificationReason = "dlp_match"
	}

	if len(info.Matches) > 0 {
		info.ClassificationReason = info.ClassificationReason + ":" + info.Matches[0].Pattern
	}

	if len(info.MatchedFields) > 0 {
		set := make(map[string]struct{}, len(info.MatchedFields))
		out := make([]string, 0, len(info.MatchedFields))
		for _, field := range info.MatchedFields {
			field = strings.TrimSpace(field)
			if field == "" {
				continue
			}
			if _, ok := set[field]; ok {
				continue
			}
			set[field] = struct{}{}
			out = append(out, field)
		}
		sort.Strings(out)
		info.MatchedFields = out
	}
}

func safeExcerpt(body []byte, loc []int) string {
	start := loc[0]
	end := loc[1]
	if end-start > 8 {
		return string(body[start:start+3]) + "***" + string(body[end-3:end])
	}
	return "***"
}
