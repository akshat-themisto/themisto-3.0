// Command packext stages browser extension source files into
// cmd/installer/assets/extensions/ so the installer's go:embed directive
// can find them. Run this before building the installer binary.
//
// It also packages the Firefox extension into a .xpi (ZIP) archive.
//
// CRX generation is intentionally omitted pending deployment model decision
// (Finding 1). Chromium files are staged as unpacked extension source.
package main

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// src → dest mappings (relative to repo root).
var extensions = []struct {
	srcDir  string
	destDir string
}{
	{"adapter/prompt/browser/chromium", "cmd/installer/assets/extensions/chromium"},
	{"adapter/prompt/browser/firefox", "cmd/installer/assets/extensions/firefox"},
	{"adapter/prompt/browser/safari", "cmd/installer/assets/extensions/safari"},
}

func main() {
	root, err := repoRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "packext: %v\n", err)
		os.Exit(1)
	}

	var staged int
	for _, ext := range extensions {
		src := filepath.Join(root, ext.srcDir)
		dest := filepath.Join(root, ext.destDir)
		// Before copying, remove stale output so deleted/renamed source files
		// don't survive from a previous run.
		if err := os.RemoveAll(dest); err != nil {
			fmt.Fprintf(os.Stderr, "packext: clean %s: %v\n", ext.destDir, err)
			os.Exit(1)
		}
		n, err := copyDir(src, dest)
		if err != nil {
			fmt.Fprintf(os.Stderr, "packext: copy %s → %s: %v\n", ext.srcDir, ext.destDir, err)
			os.Exit(1)
		}
		staged += n
	}

	// Package Firefox .xpi
	firefoxSrc := filepath.Join(root, "adapter/prompt/browser/firefox")
	xpiDest := filepath.Join(root, "cmd/installer/assets/extensions/firefox/themisto.xpi")
	if err := createXPI(firefoxSrc, xpiDest); err != nil {
		fmt.Fprintf(os.Stderr, "packext: create xpi: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("packext: staged %d files + themisto.xpi\n", staged)
}

// repoRoot walks up from the current working directory looking for go.mod.
func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("could not find repo root (no go.mod found)")
		}
		dir = parent
	}
}

// copyDir recursively copies all files from src to dest, creating dest and
// any subdirectories as needed. The output is a faithful mirror of the source
// tree — combined with the RemoveAll before each copy, this ensures
// deleted/renamed source files do not survive from a previous run.
// Returns the number of files copied.
func copyDir(src, dest string) (int, error) {
	if err := os.MkdirAll(dest, 0755); err != nil {
		return 0, err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return 0, err
	}
	var count int
	for _, e := range entries {
		srcPath := filepath.Join(src, e.Name())
		destPath := filepath.Join(dest, e.Name())
		if e.IsDir() {
			n, err := copyDir(srcPath, destPath)
			if err != nil {
				return count, err
			}
			count += n
			continue
		}
		if err := copyFile(srcPath, destPath); err != nil {
			return count, fmt.Errorf("copy %s: %w", e.Name(), err)
		}
		count++
	}
	return count, nil
}

func copyFile(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

// createXPI packages all files in srcDir into a ZIP file at xpiPath.
// Walks the directory tree recursively so subdirectories are included.
func createXPI(srcDir, xpiPath string) error {
	f, err := os.Create(xpiPath)
	if err != nil {
		return err
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	err = filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		// Use forward slashes in ZIP entries regardless of OS.
		rel = filepath.ToSlash(rel)
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		fw, err := zw.Create(rel)
		if err != nil {
			return err
		}
		_, err = fw.Write(data)
		return err
	})
	if err != nil {
		return err
	}
	if err := zw.Close(); err != nil {
		return err
	}
	return f.Close()
}
