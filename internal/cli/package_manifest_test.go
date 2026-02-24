package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/clean-dependency-project/cdprun/internal/packaging"
)

func TestNewResolvedPackageManifest(t *testing.T) {
	result := PackagingResolveResult{
		Targets: []PackagingResolveTarget{
			{
				Runtime:       "nodejs",
				Version:       "22.22.0",
				Target:        packaging.PackageTypeRPM,
				InputMode:     packaging.InputModeArchiveTarball,
				InputPlatform: "linux",
				InputArch:     "x64",
				InputPath:     "downloads/node-v22.22.0-linux-x64.tar.xz",
				InputSHA256:   "sha1",
				InputRunID:    "run-1",
				PackageName:   "OSPO-nodejs",
				InstallPrefix: "/export/apps/citools/OSPO-nodejs/22.22.0",
			},
		},
		Skipped: []PackagingResolveSkip{
			{Runtime: "python", Reason: ResolveSkipNoVerifiedInput},
		},
	}

	manifest := NewResolvedPackageManifest("run-1", result)
	if manifest.RunID != "run-1" {
		t.Fatalf("manifest run_id = %q, want %q", manifest.RunID, "run-1")
	}
	if manifest.Stage != "resolved" {
		t.Fatalf("manifest stage = %q, want %q", manifest.Stage, "resolved")
	}
	if len(manifest.Targets) != 1 {
		t.Fatalf("manifest targets len = %d, want 1", len(manifest.Targets))
	}
	if !manifest.Targets[0].Status.Resolved || manifest.Targets[0].Status.Built || manifest.Targets[0].Status.Tested || manifest.Targets[0].Status.Promoted {
		t.Fatalf("unexpected manifest status flags: %+v", manifest.Targets[0].Status)
	}
	if len(manifest.Skipped) != 1 || manifest.Skipped[0].Reason != ResolveSkipNoVerifiedInput {
		t.Fatalf("unexpected manifest skipped values: %+v", manifest.Skipped)
	}
}

func TestWritePackageManifest(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "artifacts", "package-manifest.resolved.json")
	manifest := PackageManifest{
		RunID:   "run-2",
		Stage:   "resolved",
		Targets: []PackageManifestEntry{},
		Skipped: []PackagingResolveSkip{},
	}

	if err := WritePackageManifest(path, manifest); err != nil {
		t.Fatalf("WritePackageManifest() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	var parsed PackageManifest
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if parsed.RunID != "run-2" {
		t.Fatalf("parsed run_id = %q, want %q", parsed.RunID, "run-2")
	}
	if parsed.Stage != "resolved" {
		t.Fatalf("parsed stage = %q, want %q", parsed.Stage, "resolved")
	}
}
