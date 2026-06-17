package main

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCopyDirRecursiveCopiesNestedFiles(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src")
	dest := filepath.Join(t.TempDir(), "dest")

	mustWriteFile(t, filepath.Join(src, "manifest.json"), `{"name":"root"}`)
	mustWriteFile(t, filepath.Join(src, "nested", "content.js"), "console.log('nested');")

	n, err := copyDir(src, dest)
	if err != nil {
		t.Fatalf("copyDir() error = %v", err)
	}
	if n != 2 {
		t.Fatalf("copyDir() copied %d files, want 2", n)
	}

	for _, rel := range []string{"manifest.json", filepath.Join("nested", "content.js")} {
		path := filepath.Join(dest, rel)
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected copied file %q: %v", path, err)
		}
	}
}

func TestCreateXPIRecursiveIncludesNestedFiles(t *testing.T) {
	src := filepath.Join(t.TempDir(), "firefox")
	xpi := filepath.Join(t.TempDir(), "themisto.xpi")

	mustWriteFile(t, filepath.Join(src, "manifest.json"), `{"name":"root"}`)
	mustWriteFile(t, filepath.Join(src, "nested", "content.js"), "console.log('nested');")

	if err := createXPI(src, xpi); err != nil {
		t.Fatalf("createXPI() error = %v", err)
	}

	zr, err := zip.OpenReader(xpi)
	if err != nil {
		t.Fatalf("zip.OpenReader() error = %v", err)
	}
	defer zr.Close()

	entries := map[string]bool{}
	for _, f := range zr.File {
		entries[f.Name] = true
		if strings.Contains(f.Name, "\\") {
			t.Fatalf("zip entry %q used backslashes; want forward slashes", f.Name)
		}
	}

	for _, want := range []string{"manifest.json", "nested/content.js"} {
		if !entries[want] {
			t.Fatalf("zip missing entry %q; got %v", want, entries)
		}
	}
}

func mustWriteFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", path, err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
}
