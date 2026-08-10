package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// HashFrontendDir returns a single deterministic sha256 over the frontend build
// directory. Files are hashed individually, ordered by their slash-separated
// relative path, and folded into one digest so the result is independent of
// filesystem traversal order. A missing or empty directory is an error because
// a release payload must embed a built frontend.
func HashFrontendDir(dir string) (string, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return "", fmt.Errorf("frontend dir %q: %w", dir, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("frontend path %q is not a directory", dir)
	}

	type entry struct {
		path string
		hash string
	}
	var entries []entry
	walkErr := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(dir, p)
		if relErr != nil {
			return relErr
		}
		h, hashErr := hashFile(p)
		if hashErr != nil {
			return hashErr
		}
		entries = append(entries, entry{path: filepath.ToSlash(rel), hash: h})
		return nil
	})
	if walkErr != nil {
		return "", walkErr
	}
	if len(entries) == 0 {
		return "", fmt.Errorf("frontend dir %q is empty", dir)
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].path < entries[j].path })

	h := sha256.New()
	for _, e := range entries {
		fmt.Fprintf(h, "%s\x00%s\n", e.path, e.hash)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
