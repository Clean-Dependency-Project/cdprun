// Package storage provides temporary directory management for release operations.
package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// TempDir manages a temporary directory for download and release operations.
type TempDir struct {
	root      string
	downloads string
	created   time.Time
}

// NewTempDir creates a new temporary directory structure for release operations.
// The directory structure is:
//   {base}/cdprun-{runtime}-{version}-{timestamp}/
//     downloads/    - Downloaded binaries and audit files
//     signatures/   - Cosign signature and certificate files
//
// The caller is responsible for cleaning up by calling Remove().
func NewTempDir(runtime, version string) (*TempDir, error) {
	if runtime == "" {
		return nil, fmt.Errorf("runtime cannot be empty")
	}
	if version == "" {
		return nil, fmt.Errorf("version cannot be empty")
	}

	timestamp := time.Now().Format("20060102T150405")
	dirname := fmt.Sprintf("cdprun-%s-%s-%s", runtime, version, timestamp)
	
	root := filepath.Join(os.TempDir(), dirname)
	
	// Create root directory
	if err := os.MkdirAll(root, 0755); err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}

	// Create downloads subdirectory
	downloads := filepath.Join(root, "downloads")
	if err := os.MkdirAll(downloads, 0755); err != nil {
		// Clean up root if subdirectory creation fails
		// Ignore cleanup error as we're already returning an error
		_ = os.RemoveAll(root)
		return nil, fmt.Errorf("failed to create downloads directory: %w", err)
	}

	// Create signatures subdirectory (for Cosign files)
	signatures := filepath.Join(root, "signatures")
	if err := os.MkdirAll(signatures, 0755); err != nil {
		// Clean up on failure - ignore error as we're already returning an error
		_ = os.RemoveAll(root)
		return nil, fmt.Errorf("failed to create signatures directory: %w", err)
	}

	return &TempDir{
		root:      root,
		downloads: downloads,
		created:   time.Now(),
	}, nil
}

// Root returns the root temporary directory path.
// Returns empty string if TempDir was not initialized.
func (t *TempDir) Root() string {
	return t.root
}

// Downloads returns the downloads subdirectory path where binaries are stored.
// Returns empty string if TempDir was not initialized.
func (t *TempDir) Downloads() string {
	return t.downloads
}

// Signatures returns the signatures subdirectory path where Cosign files are stored.
// Returns empty string if TempDir was not initialized.
func (t *TempDir) Signatures() string {
	if t.root == "" {
		return ""
	}
	return filepath.Join(t.root, "signatures")
}

// Remove deletes the temporary directory and all its contents.
// It returns an error if deletion fails, but does not fail if the directory
// doesn't exist (idempotent).
func (t *TempDir) Remove() error {
	if t.root == "" {
		return nil // Nothing to remove
	}

	// Check if directory exists
	if _, err := os.Stat(t.root); os.IsNotExist(err) {
		return nil // Already removed, this is fine
	}

	if err := os.RemoveAll(t.root); err != nil {
		return fmt.Errorf("failed to remove temp directory %s: %w", t.root, err)
	}

	return nil
}

// Age returns how long ago the temporary directory was created.
func (t *TempDir) Age() time.Duration {
	return time.Since(t.created)
}

// ListAllFiles returns all files in both downloads and signatures directories.
// Files are returned as absolute paths.
// Returns an error if TempDir was not initialized.
func (t *TempDir) ListAllFiles() ([]string, error) {
	if t.root == "" {
		return nil, fmt.Errorf("temp directory not initialized: use NewTempDir to create instances")
	}

	var files []string

	// List files in downloads directory
	downloadsFiles, err := listFilesInDir(t.downloads)
	if err != nil {
		return nil, fmt.Errorf("failed to list downloads: %w", err)
	}
	files = append(files, downloadsFiles...)

	// List files in signatures directory
	signaturesDir := t.Signatures()
	signaturesFiles, err := listFilesInDir(signaturesDir)
	if err != nil {
		return nil, fmt.Errorf("failed to list signatures: %w", err)
	}
	files = append(files, signaturesFiles...)

	return files, nil
}

// listFilesInDir returns all regular files (not directories) in the specified directory.
// Returns absolute paths.
func listFilesInDir(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var files []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		files = append(files, filepath.Join(dir, entry.Name()))
	}

	return files, nil
}

