package main

import (
	"github.com/themisto/agent/core/cursorcleanup"
)

// CursorCleanupFinding wraps cursorcleanup.Finding for Wails JSON serialization.
type CursorCleanupFinding = cursorcleanup.Finding

// CursorCleanupResult wraps cursorcleanup.Result for Wails JSON serialization.
type CursorCleanupResult = cursorcleanup.Result

// CursorCleanupPreview wraps cursorcleanup.Preview for Wails JSON serialization.
type CursorCleanupPreview = cursorcleanup.Preview

func newCursorCleaner() *cursorcleanup.Cleaner {
	return cursorcleanup.NewCleaner(cursorcleanup.DefaultPaths{})
}

func cursorCleanupPreview() CursorCleanupPreview {
	return newCursorCleaner().GetPreview()
}

func scanCursorData() CursorCleanupResult {
	return newCursorCleaner().Scan()
}

func cleanCursorData() CursorCleanupResult {
	return newCursorCleaner().Clean()
}
