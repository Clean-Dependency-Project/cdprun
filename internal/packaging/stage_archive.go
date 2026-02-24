package packaging

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type ArchiveStageOptions struct {
	TarballPath   string
	InstallPrefix string
	PayloadDir    string
}

// StageArchiveTarball stages a tarball into PayloadDir under InstallPrefix.
//
// This uses external tooling (`tar`, `cp`) to preserve symlinks and permissions.
func StageArchiveTarball(ctx context.Context, runner CommandRunner, opts ArchiveStageOptions) error {
	if runner == nil {
		return fmt.Errorf("runner is required")
	}
	if strings.TrimSpace(opts.TarballPath) == "" {
		return fmt.Errorf("tarball path is required")
	}
	if strings.TrimSpace(opts.PayloadDir) == "" {
		return fmt.Errorf("payload dir is required")
	}
	if strings.TrimSpace(opts.InstallPrefix) == "" {
		return fmt.Errorf("install prefix is required")
	}
	if !strings.HasPrefix(opts.InstallPrefix, "/") {
		return fmt.Errorf("install prefix must be absolute (starts with /): %q", opts.InstallPrefix)
	}

	extractDir, err := os.MkdirTemp("", "cdprun-archive-extract-*")
	if err != nil {
		return fmt.Errorf("create extract dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(extractDir) }()

	if _, _, err := runner.Run(ctx, "", "tar", []string{"-xf", opts.TarballPath, "-C", extractDir}, nil); err != nil {
		return err
	}

	dest := filepath.Join(opts.PayloadDir, strings.TrimPrefix(opts.InstallPrefix, "/"))
	if err := os.MkdirAll(dest, 0755); err != nil {
		return fmt.Errorf("create install prefix dir: %w", err)
	}

	entries, err := os.ReadDir(extractDir)
	if err != nil {
		return fmt.Errorf("read extract dir: %w", err)
	}
	if len(entries) == 0 {
		return fmt.Errorf("archive extraction produced no files: %s", opts.TarballPath)
	}

	copyRoot := extractDir
	if len(entries) == 1 && entries[0].IsDir() {
		// If archive has one top-level directory, copy its contents.
		copyRoot = filepath.Join(extractDir, entries[0].Name())
	}

	// Use '/.' to copy contents (including dotfiles), preserving permissions/symlinks.
	if _, _, err := runner.Run(ctx, "", "cp", []string{"-a", copyRoot + string(filepath.Separator) + ".", dest + string(filepath.Separator)}, nil); err != nil {
		return err
	}

	return nil
}
