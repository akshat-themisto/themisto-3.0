//go:build windows

package main

import "testing"

func TestIsCursorUninstallEntry(t *testing.T) {
	cases := []struct {
		name  string
		match bool
	}{
		{name: "Cursor", match: true},
		{name: "Cursor (User)", match: true},
		{name: "cursor", match: true},
		{name: "cursor-theme", match: false},
		{name: "Visual Studio Code", match: false},
	}

	for _, tc := range cases {
		if got := isCursorUninstallEntry(tc.name); got != tc.match {
			t.Fatalf("isCursorUninstallEntry(%q) = %v, want %v", tc.name, got, tc.match)
		}
	}
}
