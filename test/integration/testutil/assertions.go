package testutil

import (
	"strings"
	"testing"
)

// AssertContains fails the test if s does not contain substr.
func AssertContains(t *testing.T, s, substr, msg string) {
	t.Helper()
	if !strings.Contains(s, substr) {
		t.Errorf("%s: expected %q to contain %q", msg, s, substr)
	}
}

// AssertNoError fails the test if err is non-nil.
func AssertNoError(t *testing.T, err error, msg string) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: unexpected error: %v", msg, err)
	}
}

// AssertError fails the test if err is nil.
func AssertError(t *testing.T, err error, msg string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: expected error but got nil", msg)
	}
}

// AssertEqual fails the test if got != want.
func AssertEqual(t *testing.T, got, want interface{}, msg string) {
	t.Helper()
	if got != want {
		t.Errorf("%s: got %v, want %v", msg, got, want)
	}
}

// AssertTrue fails the test if v is false.
func AssertTrue(t *testing.T, v bool, msg string) {
	t.Helper()
	if !v {
		t.Errorf("%s: expected true", msg)
	}
}

// AssertFalse fails the test if v is true.
func AssertFalse(t *testing.T, v bool, msg string) {
	t.Helper()
	if v {
		t.Errorf("%s: expected false", msg)
	}
}

// HasHeader checks if a header key exists in the request log entry.
func HasHeader(entry RequestLogEntry, key string) bool {
	_, ok := entry.Headers[key]
	return ok
}

// HeaderValue returns the first value for a header key.
func HeaderValue(entry RequestLogEntry, key string) string {
	vals := entry.Headers[key]
	if len(vals) == 0 {
		return ""
	}
	return vals[0]
}
