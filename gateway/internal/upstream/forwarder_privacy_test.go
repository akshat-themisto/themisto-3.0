package upstream

import (
	"net/http"
	"strings"
	"testing"
)

func TestRemoveInternalHeaders(t *testing.T) {
	headers := http.Header{
		"X-Themisto-Agent-Id":     {"device"},
		"X-Themisto-Process-Path": {"/private/user/repo"},
		"X-Themisto-Original-Url": {"https://example.invalid"},
		"Authorization":           {"Bearer provider-token"},
		"Content-Type":            {"application/json"},
	}
	removeInternalHeaders(headers)
	for key := range headers {
		if strings.HasPrefix(strings.ToLower(key), "x-themisto-") {
			t.Fatalf("internal header escaped to provider: %s", key)
		}
	}
	if headers.Get("Authorization") == "" || headers.Get("Content-Type") == "" {
		t.Fatal("provider headers were removed with internal metadata")
	}
}
