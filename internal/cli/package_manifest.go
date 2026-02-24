package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// PackageManifest captures package orchestration state for a run.
// In Phase 1 this is emitted at the "resolved" stage and becomes the handoff
// contract for subsequent build/test/promotion phases.
type PackageManifest struct {
	RunID       string                 `json:"run_id"`
	Stage       string                 `json:"stage"`
	GeneratedAt time.Time              `json:"generated_at"`
	Targets     []PackageManifestEntry `json:"targets"`
	Skipped     []PackagingResolveSkip `json:"skipped"`
}

// PackageManifestEntry represents one packaging unit of work for a runtime/version/target.
// Status fields are placeholders for upcoming phases.
type PackageManifestEntry struct {
	Runtime       string                `json:"runtime"`
	Version       string                `json:"version"`
	Target        string                `json:"target"`
	InputMode     string                `json:"input_mode"`
	InputPlatform string                `json:"input_platform"`
	InputArch     string                `json:"input_arch"`
	InputPath     string                `json:"input_path"`
	InputSHA256   string                `json:"input_sha256"`
	InputRunID    string                `json:"input_run_id,omitempty"`
	PackageName   string                `json:"package_name"`
	InstallPrefix string                `json:"install_prefix"`
	Status        PackageManifestStatus `json:"status"`
}

// PackageManifestStatus tracks state transitions for one target.
type PackageManifestStatus struct {
	Resolved bool `json:"resolved"`
	Built    bool `json:"built"`
	Tested   bool `json:"tested"`
	Promoted bool `json:"promoted"`
}

// NewResolvedPackageManifest converts resolver output into a stage-scoped manifest.
func NewResolvedPackageManifest(runID string, result PackagingResolveResult) PackageManifest {
	targets := make([]PackageManifestEntry, 0, len(result.Targets))
	for _, target := range result.Targets {
		targets = append(targets, PackageManifestEntry{
			Runtime:       target.Runtime,
			Version:       target.Version,
			Target:        string(target.Target),
			InputMode:     string(target.InputMode),
			InputPlatform: target.InputPlatform,
			InputArch:     target.InputArch,
			InputPath:     target.InputPath,
			InputSHA256:   target.InputSHA256,
			InputRunID:    target.InputRunID,
			PackageName:   target.PackageName,
			InstallPrefix: target.InstallPrefix,
			Status: PackageManifestStatus{
				Resolved: true,
				Built:    false,
				Tested:   false,
				Promoted: false,
			},
		})
	}

	return PackageManifest{
		RunID:       runID,
		Stage:       "resolved",
		GeneratedAt: time.Now().UTC(),
		Targets:     targets,
		Skipped:     result.Skipped,
	}
}

// WritePackageManifest writes a manifest as formatted JSON and ensures parent
// directories exist for predictable CI/local behavior.
func WritePackageManifest(path string, manifest PackageManifest) error {
	if path == "" {
		return fmt.Errorf("manifest path is required")
	}
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0700); err != nil {
		return fmt.Errorf("create manifest parent directory: %w", err)
	}

	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal package manifest: %w", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("write package manifest: %w", err)
	}
	return nil
}
