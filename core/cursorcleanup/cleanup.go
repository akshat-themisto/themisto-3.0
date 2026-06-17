// Package cursorcleanup provides scanning and redaction of sensitive data
// stored in Cursor IDE's local SQLite databases and transcript files.
package cursorcleanup

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/themisto/agent/core/dlp"

	_ "modernc.org/sqlite"
)

// maxValueSize is the upper bound on a single DB value we'll regex-scan.
const maxValueSize = 10 * 1024 * 1024 // 10 MB

// Finding represents a single sensitive-data match.
type Finding struct {
	Source      string `json:"source"` // "database" or "transcript"
	FilePath    string `json:"file_path"`
	Key         string `json:"key,omitempty"` // DB key; empty for transcripts
	PatternName string `json:"pattern_name"`
	PatternType string `json:"pattern_type"` // "pii" or "credential"
	Excerpt     string `json:"excerpt"`
	MatchCount  int    `json:"match_count"`
}

// Result is returned from scan and clean operations.
type Result struct {
	Success          bool      `json:"success"`
	Message          string    `json:"message"`
	DatabasesFound   int       `json:"databases_found"`
	TranscriptsFound int       `json:"transcripts_found"`
	TotalFindings    int       `json:"total_findings"`
	RedactedCount    int       `json:"redacted_count"`
	Findings         []Finding `json:"findings"`
	Errors           []string  `json:"errors,omitempty"`
}

// Preview is a lightweight summary (no regex work).
type Preview struct {
	DatabaseCount   int  `json:"database_count"`
	TranscriptCount int  `json:"transcript_count"`
	ScanAvailable   bool `json:"scan_available"`
}

// PathResolver provides platform-specific Cursor storage paths.
type PathResolver interface {
	WorkspaceStorageRoot() (string, error)
	TranscriptsRoot() (string, error)
}

// Cleaner handles discovery, scanning, and redaction.
type Cleaner struct {
	paths PathResolver
}

// NewCleaner creates a Cleaner with the given platform path resolver.
func NewCleaner(paths PathResolver) *Cleaner {
	return &Cleaner{paths: paths}
}

// --- Discovery ---

func (c *Cleaner) discoverDatabases() ([]string, error) {
	root, err := c.paths.WorkspaceStorageRoot()
	if err != nil {
		return nil, err
	}
	matches, err := filepath.Glob(filepath.Join(root, "*", "state.vscdb"))
	if err != nil {
		return nil, err
	}

	// Also check global and user-level state.vscdb.
	parent := filepath.Dir(root) // .../Cursor/User
	for _, sub := range []string{"globalStorage", "."} {
		p := filepath.Join(parent, sub, "state.vscdb")
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			matches = append(matches, p)
		}
	}
	return matches, nil
}

func (c *Cleaner) discoverTranscripts() ([]string, error) {
	root, err := c.paths.TranscriptsRoot()
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return nil, nil
	}
	var files []string
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() && hasPathSegment(path, "agent-transcripts") && isTranscriptFile(path) {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

// --- Preview ---

// GetPreview returns a lightweight count of discoverable files.
func (c *Cleaner) GetPreview() Preview {
	dbs, _ := c.discoverDatabases()
	transcripts, _ := c.discoverTranscripts()
	return Preview{
		DatabaseCount:   len(dbs),
		TranscriptCount: len(transcripts),
		ScanAvailable:   len(dbs) > 0 || len(transcripts) > 0,
	}
}

// --- Scan (read-only) ---

// Scan performs a read-only scan of all discoverable Cursor data.
func (c *Cleaner) Scan() Result {
	patterns := dlp.RedactionPatterns()
	result := Result{Success: true}

	dbs, err := c.discoverDatabases()
	if err != nil {
		result.Errors = append(result.Errors, "database discovery: "+err.Error())
	}
	result.DatabasesFound = len(dbs)

	transcripts, err := c.discoverTranscripts()
	if err != nil {
		result.Errors = append(result.Errors, "transcript discovery: "+err.Error())
	}
	result.TranscriptsFound = len(transcripts)

	for _, dbPath := range dbs {
		findings, err := ScanVscdb(dbPath, patterns)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %s", filepath.Base(filepath.Dir(dbPath)), err.Error()))
			continue
		}
		result.Findings = append(result.Findings, findings...)
	}

	for _, tPath := range transcripts {
		findings, err := ScanTranscriptFile(tPath, patterns)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %s", filepath.Base(tPath), err.Error()))
			continue
		}
		result.Findings = append(result.Findings, findings...)
	}

	SortFindings(result.Findings)
	result.TotalFindings = len(result.Findings)
	if result.TotalFindings == 0 {
		result.Message = "No sensitive data found in Cursor local storage."
	} else {
		result.Message = fmt.Sprintf("Found %d sensitive data match(es) across %d database(s) and %d transcript(s).",
			result.TotalFindings, result.DatabasesFound, result.TranscriptsFound)
	}
	return result
}

// --- Clean (destructive) ---

// Clean redacts sensitive data in all discoverable Cursor data.
func (c *Cleaner) Clean() Result {
	patterns := dlp.RedactionPatterns()
	result := Result{Success: true}

	dbs, err := c.discoverDatabases()
	if err != nil {
		result.Errors = append(result.Errors, "database discovery: "+err.Error())
	}
	result.DatabasesFound = len(dbs)

	transcripts, err := c.discoverTranscripts()
	if err != nil {
		result.Errors = append(result.Errors, "transcript discovery: "+err.Error())
	}
	result.TranscriptsFound = len(transcripts)

	for _, dbPath := range dbs {
		n, err := RedactVscdb(dbPath, patterns)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %s", filepath.Base(filepath.Dir(dbPath)), err.Error()))
			continue
		}
		result.RedactedCount += n
	}

	for _, tPath := range transcripts {
		n, err := RedactTranscriptFile(tPath, patterns)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %s", filepath.Base(tPath), err.Error()))
			continue
		}
		result.RedactedCount += n
	}

	// Post-redaction scan.
	postScan := c.Scan()
	result.Findings = postScan.Findings
	result.TotalFindings = postScan.TotalFindings

	if result.RedactedCount == 0 && result.TotalFindings == 0 {
		result.Message = "No sensitive data found. Nothing to redact."
	} else if result.TotalFindings == 0 {
		result.Message = fmt.Sprintf("Redacted %d row(s)/file(s). All sensitive data has been removed.", result.RedactedCount)
	} else {
		result.Message = fmt.Sprintf("Redacted %d row(s)/file(s). %d finding(s) could not be cleaned (check errors).", result.RedactedCount, result.TotalFindings)
		result.Success = len(result.Errors) == 0
	}
	return result
}

// CleanBlockedPromptTranscript performs the safe automatic cleanup path used
// after a blocked Cursor prompt. It only touches the specific transcript file
// that Cursor reported for the blocked prompt, and skips cleanup entirely when
// that artifact is not available.
func CleanBlockedPromptTranscript(transcriptPath string) Result {
	result := Result{Success: true}

	transcriptPath = filepath.Clean(strings.TrimSpace(transcriptPath))
	switch {
	case transcriptPath == "":
		result.Message = "Automatic Cursor cleanup skipped because no transcript path was provided."
		return result
	case !hasPathSegment(transcriptPath, "agent-transcripts"):
		result.Message = "Automatic Cursor cleanup skipped because the reported file was not a Cursor transcript artifact."
		return result
	case !isTranscriptFile(transcriptPath):
		result.Message = "Automatic Cursor cleanup skipped because the reported file was not a supported transcript file."
		return result
	}

	result.TranscriptsFound = 1
	patterns := dlp.RedactionPatterns()

	n, err := RedactTranscriptFile(transcriptPath, patterns)
	if err != nil {
		result.Success = false
		result.Errors = append(result.Errors, fmt.Sprintf("%s: %s", filepath.Base(transcriptPath), err.Error()))
		result.Message = "Automatic Cursor transcript cleanup failed."
		return result
	}
	result.RedactedCount = n

	findings, err := ScanTranscriptFile(transcriptPath, patterns)
	if err != nil {
		result.Success = false
		result.Errors = append(result.Errors, fmt.Sprintf("%s: %s", filepath.Base(transcriptPath), err.Error()))
		result.Message = "Automatic Cursor transcript cleanup completed, but verification failed."
		return result
	}
	result.Findings = findings
	result.TotalFindings = len(findings)

	switch {
	case result.RedactedCount == 0 && result.TotalFindings == 0:
		result.Message = "No sensitive data was found in the blocked Cursor transcript."
	case result.TotalFindings == 0:
		result.Message = "Blocked Cursor transcript was redacted successfully."
	default:
		result.Success = false
		result.Message = fmt.Sprintf("Blocked Cursor transcript was redacted, but %d finding(s) remain.", result.TotalFindings)
	}

	return result
}

// --- Low-level scan/redact functions (exported for testing) ---

// ScanVscdb scans a single state.vscdb for sensitive data (read-only).
func ScanVscdb(dbPath string, patterns []dlp.RedactionPattern) ([]Finding, error) {
	db, err := sql.Open("sqlite", dbPath+"?mode=ro")
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	defer db.Close()

	rows, err := db.Query("SELECT key, value FROM ItemTable")
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	var findings []Finding
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			continue
		}
		if len(value) == 0 || len(value) > maxValueSize {
			continue
		}
		for _, p := range patterns {
			locs := p.Re.FindAllStringIndex(value, -1)
			if len(locs) == 0 {
				continue
			}
			findings = append(findings, Finding{
				Source:      "database",
				FilePath:    dbPath,
				Key:         key,
				PatternName: p.Name,
				PatternType: p.Type,
				Excerpt:     SafeExcerpt(value, locs[0]),
				MatchCount:  len(locs),
			})
		}
	}
	return findings, rows.Err()
}

// ScanTranscriptFile scans a single transcript file for sensitive data.
func ScanTranscriptFile(path string, patterns []dlp.RedactionPattern) ([]Finding, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || len(data) > maxValueSize {
		return nil, nil
	}
	text := string(data)

	var findings []Finding
	for _, p := range patterns {
		locs := p.Re.FindAllStringIndex(text, -1)
		if len(locs) == 0 {
			continue
		}
		findings = append(findings, Finding{
			Source:      "transcript",
			FilePath:    path,
			PatternName: p.Name,
			PatternType: p.Type,
			Excerpt:     SafeExcerpt(text, locs[0]),
			MatchCount:  len(locs),
		})
	}
	return findings, nil
}

// RedactVscdb redacts sensitive data in a single state.vscdb (destructive).
func RedactVscdb(dbPath string, patterns []dlp.RedactionPattern) (int, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return 0, fmt.Errorf("open: %w", err)
	}
	defer db.Close()

	if _, err := db.Exec("BEGIN IMMEDIATE; ROLLBACK"); err != nil {
		return 0, fmt.Errorf("database is locked (close Cursor and retry): %w", err)
	}

	tx, err := db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	rows, err := tx.Query("SELECT key, value FROM ItemTable")
	if err != nil {
		return 0, fmt.Errorf("query: %w", err)
	}

	type rowUpdate struct {
		key      string
		newValue string
	}
	var updates []rowUpdate

	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			continue
		}
		if len(value) == 0 || len(value) > maxValueSize {
			continue
		}
		original := value
		for _, p := range patterns {
			value = p.Re.ReplaceAllString(value, dlp.Replacement)
		}
		if value != original {
			updates = append(updates, rowUpdate{key: key, newValue: value})
		}
	}
	rows.Close()

	if len(updates) == 0 {
		return 0, nil
	}

	stmt, err := tx.Prepare("UPDATE ItemTable SET value = ? WHERE key = ?")
	if err != nil {
		return 0, fmt.Errorf("prepare update: %w", err)
	}
	defer stmt.Close()

	for _, u := range updates {
		if _, err := stmt.Exec(u.newValue, u.key); err != nil {
			return 0, fmt.Errorf("update key %q: %w", u.key, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}
	return len(updates), nil
}

// RedactTranscriptFile redacts sensitive data in a single transcript file.
func RedactTranscriptFile(path string, patterns []dlp.RedactionPattern) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	if len(data) == 0 || len(data) > maxValueSize {
		return 0, nil
	}

	text := string(data)
	original := text
	for _, p := range patterns {
		text = p.Re.ReplaceAllString(text, dlp.Replacement)
	}
	if text == original {
		return 0, nil
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(text), 0644); err != nil {
		return 0, fmt.Errorf("write tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return 0, fmt.Errorf("rename: %w", err)
	}
	return 1, nil
}

// --- Helpers ---

// SafeExcerpt returns a redacted preview: first 3 + "***" + last 3 chars.
func SafeExcerpt(text string, loc []int) string {
	start, end := loc[0], loc[1]
	if end > len(text) {
		end = len(text)
	}
	if end-start > 8 {
		return text[start:start+3] + "***" + text[end-3:end]
	}
	return "***"
}

func findingPriority(f Finding) int {
	if f.PatternType == "credential" {
		return 0
	}
	switch f.PatternName {
	case "ssn", "credit_card":
		return 1
	case "phone_us", "uk_ni_number", "passport":
		return 2
	case "email":
		return 3
	default:
		return 2
	}
}

// SortFindings sorts findings by severity: credentials first, then SSN/cards, then emails last.
func SortFindings(findings []Finding) {
	sort.SliceStable(findings, func(i, j int) bool {
		return findingPriority(findings[i]) < findingPriority(findings[j])
	})
}

func hasPathSegment(path, segment string) bool {
	for _, part := range strings.Split(filepath.Clean(path), string(filepath.Separator)) {
		if strings.EqualFold(part, segment) {
			return true
		}
	}
	return false
}

func isTranscriptFile(path string) bool {
	name := strings.ToLower(filepath.Base(path))
	return strings.HasSuffix(name, ".md") || strings.HasSuffix(name, ".jsonl")
}
