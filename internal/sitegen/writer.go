package sitegen

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"

	"log/slog"
)

// writeFileIfChanged writes content to a file only if it differs from existing content.
// Returns true if the file was written, false if it was unchanged.
// This ensures idempotent generation - regenerating with the same data produces no changes.
func writeFileIfChanged(path string, content []byte, logger *slog.Logger) error {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Check if file exists and has same content
	if existingContent, err := os.ReadFile(path); err == nil {
		if contentMatches(existingContent, content) {
			logger.Debug("file unchanged, skipping", "path", path)
			return nil
		}
	}

	// Write file
	if err := os.WriteFile(path, content, 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	logger.Debug("file written", "path", path)
	return nil
}

// contentMatches compares two byte slices for equality.
// Uses SHA256 hash comparison for efficiency with large files.
func contentMatches(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}

	// For small files, direct comparison is faster
	if len(a) < 1024 {
		for i := range a {
			if a[i] != b[i] {
				return false
			}
		}
		return true
	}

	// For larger files, compare hashes
	hashA := sha256.Sum256(a)
	hashB := sha256.Sum256(b)
	return hashA == hashB
}

