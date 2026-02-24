package cli

import (
	"errors"
	"testing"
	"time"

	"github.com/clean-dependency-project/cdprun/internal/config"
	"github.com/clean-dependency-project/cdprun/internal/packaging"
	"github.com/clean-dependency-project/cdprun/internal/storage"
)

type mockPackagingDownloadsStore struct {
	byRuntime map[string][]*storage.Download
	errByName map[string]error
}

func (m *mockPackagingDownloadsStore) ListByRuntime(runtime string) ([]*storage.Download, error) {
	if err, ok := m.errByName[runtime]; ok && err != nil {
		return nil, err
	}
	return m.byRuntime[runtime], nil
}

func TestResolvePackagingTargets_NodeOnlyRPM(t *testing.T) {
	cfg := &config.Config{
		Runtimes: map[string]config.Runtime{
			"nodejs": {
				Enabled: true,
				Packaging: config.PackagingConfig{
					Enabled:               true,
					Targets:               []string{"rpm"},
					PackageNameTemplate:   "OSPO-{runtime}",
					InstallPrefixTemplate: "/export/apps/citools/{pkgname}/{version}",
				},
			},
		},
	}

	now := time.Now()
	store := &mockPackagingDownloadsStore{
		byRuntime: map[string][]*storage.Download{
			"nodejs": {
				{
					Runtime:            "nodejs",
					Version:            "22.22.0",
					Platform:           "linux",
					Architecture:       "x64",
					LocalPath:          "downloads/node-v22.22.0-linux-x64.tar.xz",
					ContentSHA256:      "abc123",
					RunID:              "run-1",
					VerificationStatus: "success",
					DownloadedAt:       now,
				},
			},
		},
	}

	got, err := ResolvePackagingTargets(cfg, store, PackagingResolveOptions{RunID: "run-1"})
	if err != nil {
		t.Fatalf("ResolvePackagingTargets() error = %v", err)
	}

	if len(got.Targets) != 1 {
		t.Fatalf("targets len = %d, want 1", len(got.Targets))
	}
	target := got.Targets[0]
	if target.Target != packaging.PackageTypeRPM {
		t.Fatalf("target type = %s, want rpm", target.Target)
	}
	if target.InputMode != packaging.InputModeArchiveTarball {
		t.Fatalf("input mode = %s, want archive-tarball", target.InputMode)
	}
	if target.PackageName != "OSPO-nodejs" {
		t.Fatalf("package name = %q, want %q", target.PackageName, "OSPO-nodejs")
	}
	if target.InstallPrefix != "/export/apps/citools/OSPO-nodejs/22.22.0" {
		t.Fatalf("install prefix = %q", target.InstallPrefix)
	}
}

func TestResolvePackagingTargets_SkipsDisabledOrInvalid(t *testing.T) {
	cfg := &config.Config{
		Runtimes: map[string]config.Runtime{
			"python": {
				Enabled: true,
				Packaging: config.PackagingConfig{
					Enabled: false,
				},
			},
			"nodejs": {
				Enabled: true,
				Packaging: config.PackagingConfig{
					Enabled: true,
					Targets: []string{"rpm", "apk"},
				},
			},
		},
	}

	store := &mockPackagingDownloadsStore{
		byRuntime: map[string][]*storage.Download{
			"nodejs": {
				{
					Runtime:            "nodejs",
					Version:            "22.22.0",
					Platform:           "windows",
					Architecture:       "x64",
					LocalPath:          "downloads/node-v22.22.0-x64.msi",
					ContentSHA256:      "hash",
					VerificationStatus: "success",
				},
			},
		},
	}

	got, err := ResolvePackagingTargets(cfg, store, PackagingResolveOptions{RunID: "run-1"})
	if err != nil {
		t.Fatalf("ResolvePackagingTargets() error = %v", err)
	}
	if len(got.Targets) != 0 {
		t.Fatalf("targets len = %d, want 0", len(got.Targets))
	}
	if len(got.Skipped) != 2 {
		t.Fatalf("skipped len = %d, want 2", len(got.Skipped))
	}
	reasons := map[PackagingResolveSkipReason]bool{}
	for _, skip := range got.Skipped {
		reasons[skip.Reason] = true
	}
	if !reasons[ResolveSkipPackagingDisabled] {
		t.Fatalf("expected skip reason %q not found", ResolveSkipPackagingDisabled)
	}
	if !reasons[ResolveSkipNoVerifiedInput] {
		t.Fatalf("expected skip reason %q not found", ResolveSkipNoVerifiedInput)
	}
}

func TestResolvePackagingTargets_RunIDFilter(t *testing.T) {
	cfg := &config.Config{
		Runtimes: map[string]config.Runtime{
			"nodejs": {
				Enabled: true,
				Packaging: config.PackagingConfig{
					Enabled: true,
					Targets: []string{"rpm"},
				},
			},
		},
	}

	store := &mockPackagingDownloadsStore{
		byRuntime: map[string][]*storage.Download{
			"nodejs": {
				{
					Runtime:            "nodejs",
					Version:            "22.22.0",
					Platform:           "linux",
					Architecture:       "x64",
					LocalPath:          "downloads/node-v22.22.0-linux-x64.tar.xz",
					ContentSHA256:      "sha-a",
					RunID:              "run-a",
					VerificationStatus: "success",
					DownloadedAt:       time.Now().Add(-time.Minute),
				},
				{
					Runtime:            "nodejs",
					Version:            "22.22.0",
					Platform:           "linux",
					Architecture:       "x64",
					LocalPath:          "downloads/node-v22.22.0-linux-x64.tar.xz",
					ContentSHA256:      "sha-b",
					RunID:              "run-b",
					VerificationStatus: "success",
					DownloadedAt:       time.Now(),
				},
			},
		},
	}

	got, err := ResolvePackagingTargets(cfg, store, PackagingResolveOptions{RunID: "run-b"})
	if err != nil {
		t.Fatalf("ResolvePackagingTargets() error = %v", err)
	}
	if len(got.Targets) != 1 {
		t.Fatalf("targets len = %d, want 1", len(got.Targets))
	}
	if got.Targets[0].InputRunID != "run-b" {
		t.Fatalf("target run id = %q, want %q", got.Targets[0].InputRunID, "run-b")
	}
	if got.Targets[0].InputSHA256 != "sha-b" {
		t.Fatalf("target sha = %q, want %q", got.Targets[0].InputSHA256, "sha-b")
	}
}

func TestResolvePackagingTargets_ErrorsAreValues(t *testing.T) {
	_, err := ResolvePackagingTargets(nil, &mockPackagingDownloadsStore{}, PackagingResolveOptions{})
	if !errors.Is(err, ErrResolvePackagingTargetsNilConfig) {
		t.Fatalf("nil config error = %v, want %v", err, ErrResolvePackagingTargetsNilConfig)
	}

	cfg := &config.Config{Runtimes: map[string]config.Runtime{}}
	_, err = ResolvePackagingTargets(cfg, nil, PackagingResolveOptions{})
	if !errors.Is(err, ErrResolvePackagingTargetsNilDB) {
		t.Fatalf("nil db error = %v, want %v", err, ErrResolvePackagingTargetsNilDB)
	}

	_, err = ResolvePackagingTargets(cfg, &mockPackagingDownloadsStore{}, PackagingResolveOptions{})
	if !errors.Is(err, ErrResolvePackagingTargetsNilRunID) {
		t.Fatalf("nil run_id error = %v, want %v", err, ErrResolvePackagingTargetsNilRunID)
	}
}

func TestResolvePackagingTargets_MatrixSelection(t *testing.T) {
	cfg := &config.Config{
		Runtimes: map[string]config.Runtime{
			"nodejs": {
				Enabled: true,
				Packaging: config.PackagingConfig{
					Enabled:               true,
					Targets:               []string{"rpm", "apk", "rpm"}, // duplicate on purpose
					PackageNameTemplate:   "OSPO-{runtime}",
					InstallPrefixTemplate: "/export/apps/citools/{pkgname}/{version}",
				},
			},
		},
	}

	store := &mockPackagingDownloadsStore{
		byRuntime: map[string][]*storage.Download{
			"nodejs": {
				{
					Runtime:            "nodejs",
					Version:            "22.22.0",
					Platform:           "linux",
					Architecture:       "x64",
					LocalPath:          "downloads/node-v22.22.0-linux-x64.tar.xz",
					ContentSHA256:      "sha-lnx",
					RunID:              "run-1",
					VerificationStatus: "success",
					DownloadedAt:       time.Now(),
				},
				{
					Runtime:            "nodejs",
					Version:            "22.22.0",
					Platform:           "windows",
					Architecture:       "x64",
					LocalPath:          "downloads/node-v22.22.0-x64.msi",
					ContentSHA256:      "sha-win",
					RunID:              "run-1",
					VerificationStatus: "success",
					DownloadedAt:       time.Now(),
				},
			},
		},
	}

	got, err := ResolvePackagingTargets(cfg, store, PackagingResolveOptions{RunID: "run-1"})
	if err != nil {
		t.Fatalf("ResolvePackagingTargets() error = %v", err)
	}

	if len(got.Targets) != 2 {
		t.Fatalf("targets len = %d, want 2 (rpm+apk from one linux input)", len(got.Targets))
	}
	if got.Targets[0].Target != packaging.PackageTypeRPM {
		t.Fatalf("first target = %s, want rpm", got.Targets[0].Target)
	}
	if got.Targets[1].Target != packaging.PackageTypeAPK {
		t.Fatalf("second target = %s, want apk", got.Targets[1].Target)
	}
	for _, target := range got.Targets {
		if target.InputPlatform != "linux" {
			t.Fatalf("input platform = %s, want linux", target.InputPlatform)
		}
		if target.InputPath == "" || target.InputSHA256 == "" {
			t.Fatalf("expected non-empty input path and sha256, got path=%q sha=%q", target.InputPath, target.InputSHA256)
		}
	}
}

func TestResolvePackagingTargets_RunIDIncludesOnlyCurrentRun(t *testing.T) {
	cfg := &config.Config{
		Runtimes: map[string]config.Runtime{
			"nodejs": {
				Enabled: true,
				Packaging: config.PackagingConfig{
					Enabled:               true,
					Targets:               []string{"rpm"},
					PackageNameTemplate:   "OSPO-{runtime}",
					InstallPrefixTemplate: "/export/apps/citools/{pkgname}/{version}",
				},
			},
		},
	}

	store := &mockPackagingDownloadsStore{
		byRuntime: map[string][]*storage.Download{
			"nodejs": {
				{
					Runtime:            "nodejs",
					Version:            "22.22.0",
					Platform:           "linux",
					Architecture:       "x64",
					LocalPath:          "downloads/node-v22.22.0-linux-x64.tar.xz",
					ContentSHA256:      "sha-22",
					RunID:              "run-1",
					VerificationStatus: "success",
					DownloadedAt:       time.Now(),
				},
				{
					Runtime:            "nodejs",
					Version:            "20.20.0",
					Platform:           "linux",
					Architecture:       "x64",
					LocalPath:          "downloads/node-v20.20.0-linux-x64.tar.xz",
					ContentSHA256:      "sha-20",
					RunID:              "run-1",
					VerificationStatus: "success",
					DownloadedAt:       time.Now().Add(-time.Hour),
				},
			},
		},
	}

	got, err := ResolvePackagingTargets(cfg, store, PackagingResolveOptions{RunID: "run-1"})
	if err != nil {
		t.Fatalf("ResolvePackagingTargets() error = %v", err)
	}
	if len(got.Targets) != 2 {
		t.Fatalf("targets len = %d, want 2", len(got.Targets))
	}
	versions := map[string]bool{}
	for _, target := range got.Targets {
		versions[target.Version] = true
	}
	if !versions["22.22.0"] || !versions["20.20.0"] {
		t.Fatalf("target versions = %+v, want both 22.22.0 and 20.20.0", versions)
	}
}
