package uid

import (
	"regexp"
	"testing"
)

func TestNew_Format(t *testing.T) {
	id := New()
	// UUID v4 format: 8-4-4-4-12 hex chars.
	re := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	if !re.MatchString(id) {
		t.Errorf("uid %q does not match UUID v4 format", id)
	}
}

func TestNew_Unique(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		id := New()
		if seen[id] {
			t.Fatalf("duplicate uid after %d iterations: %s", i, id)
		}
		seen[id] = true
	}
}

func TestNew_Length(t *testing.T) {
	id := New()
	if len(id) != 36 {
		t.Errorf("uid length = %d, want 36", len(id))
	}
}
