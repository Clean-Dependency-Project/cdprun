package cli

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/clean-dependency-project/cdprun/internal/config"
	"github.com/clean-dependency-project/cdprun/internal/packaging"
	"github.com/clean-dependency-project/cdprun/internal/storage"
)

var (
	ErrResolvePackagingTargetsNilConfig = errors.New("config is required")
	ErrResolvePackagingTargetsNilDB     = errors.New("database is required")
	ErrResolvePackagingTargetsNilRunID  = errors.New("run_id is required")
)

type PackagingResolveSkipReason string

const (
	ResolveSkipPackagingDisabled PackagingResolveSkipReason = "packaging_disabled"
	ResolveSkipTargetNotEnabled  PackagingResolveSkipReason = "target_not_enabled"
	ResolveSkipNoVerifiedInput   PackagingResolveSkipReason = "no_verified_download"
)

type packagingDownloadsStore interface {
	ListByRuntime(runtime string) ([]*storage.Download, error)
}

type PackagingResolveOptions struct {
	// RunID limits results to downloads from a specific run.
	RunID string
}

type PackagingResolveSkip struct {
	Runtime string                     `json:"runtime"`
	Reason  PackagingResolveSkipReason `json:"reason"`
	Detail  string                     `json:"detail,omitempty"`
}

type PackagingResolveTarget struct {
	Runtime       string                `json:"runtime"`
	Version       string                `json:"version"`
	Target        packaging.PackageType `json:"target"`
	InputMode     packaging.InputMode   `json:"input_mode"`
	InputPlatform string                `json:"input_platform"`
	InputArch     string                `json:"input_arch"`
	InputPath     string                `json:"input_path"`
	InputSHA256   string                `json:"input_sha256"`
	InputRunID    string                `json:"input_run_id,omitempty"`
	PackageName   string                `json:"package_name"`
	InstallPrefix string                `json:"install_prefix"`
}

type PackagingResolveResult struct {
	Targets []PackagingResolveTarget `json:"targets"`
	Skipped []PackagingResolveSkip   `json:"skipped"`
}

const (
	verificationStatusSuccess = "success"
	platformOSLinux           = "linux"
)

// ResolvePackagingTargets converts current-run download records into package build targets.
//
// Bigger picture:
// - Download stage writes verified artifacts into downloads.db with a run_id.
// - Resolver stage reads only that run_id and prepares package work items.
// - Build/test/promotion stages consume these targets without re-discovering inputs.
func ResolvePackagingTargets(cfg *config.Config, db packagingDownloadsStore, opts PackagingResolveOptions) (PackagingResolveResult, error) {
	if err := validateResolvePackagingInputs(cfg, db, opts); err != nil {
		return PackagingResolveResult{}, err
	}

	result := PackagingResolveResult{
		Targets: make([]PackagingResolveTarget, 0),
		Skipped: make([]PackagingResolveSkip, 0),
	}

	enabledRuntimes := cfg.GetEnabledRuntimes()
	for _, runtimeName := range sortedEnabledRuntimeNames(enabledRuntimes) {
		runtimeCfg := enabledRuntimes[runtimeName]
		targets, skip, err := resolveRuntimePackagingTargets(runtimeName, runtimeCfg, db, opts.RunID)
		if err != nil {
			return PackagingResolveResult{}, err
		}
		if skip != nil {
			result.Skipped = append(result.Skipped, *skip)
			continue
		}
		result.Targets = append(result.Targets, targets...)
	}

	return result, nil
}

// validateResolvePackagingInputs ensures resolver prerequisites are present.
// This keeps ResolvePackagingTargets focused on orchestration logic.
func validateResolvePackagingInputs(cfg *config.Config, db packagingDownloadsStore, opts PackagingResolveOptions) error {
	if cfg == nil {
		return ErrResolvePackagingTargetsNilConfig
	}
	if db == nil {
		return ErrResolvePackagingTargetsNilDB
	}
	if strings.TrimSpace(opts.RunID) == "" {
		return ErrResolvePackagingTargetsNilRunID
	}
	return nil
}

// sortedEnabledRuntimeNames returns enabled runtime names in deterministic order.
// Deterministic ordering makes logs/results stable across reruns.
func sortedEnabledRuntimeNames(enabledRuntimes map[string]config.Runtime) []string {
	runtimeNames := make([]string, 0, len(enabledRuntimes))
	for runtimeName := range enabledRuntimes {
		runtimeNames = append(runtimeNames, runtimeName)
	}
	sort.Strings(runtimeNames)
	return runtimeNames
}

// resolveRuntimePackagingTargets resolves package targets for a single runtime.
// It either returns resolved targets, or a skip reason when runtime is not package-eligible.
func resolveRuntimePackagingTargets(
	runtimeName string,
	runtimeCfg config.Runtime,
	db packagingDownloadsStore,
	runID string,
) ([]PackagingResolveTarget, *PackagingResolveSkip, error) {
	if !runtimeCfg.Packaging.Enabled {
		return nil, &PackagingResolveSkip{
			Runtime: runtimeName,
			Reason:  ResolveSkipPackagingDisabled,
		}, nil
	}

	targets := normalizePackageTargets(runtimeCfg.Packaging.Targets)
	if len(targets) == 0 {
		return nil, &PackagingResolveSkip{
			Runtime: runtimeName,
			Reason:  ResolveSkipTargetNotEnabled,
			Detail:  "no supported targets (rpm/apk)",
		}, nil
	}

	downloads, err := db.ListByRuntime(runtimeName)
	if err != nil {
		return nil, nil, fmt.Errorf("list downloads for %s: %w", runtimeName, err)
	}

	candidates := filterPackagingInputDownloads(downloads, runID)
	if len(candidates) == 0 {
		return nil, &PackagingResolveSkip{
			Runtime: runtimeName,
			Reason:  ResolveSkipNoVerifiedInput,
			Detail:  "no linux archive artifact with success verification and sha256",
		}, nil
	}

	sortResolveCandidates(candidates)
	unique := dedupeDownloadsByInput(candidates)
	return buildResolveTargetsForDownloads(runtimeName, runtimeCfg, targets, unique), nil, nil
}

// sortResolveCandidates applies stable deterministic ordering so output is reproducible.
func sortResolveCandidates(candidates []*storage.Download) {
	sort.SliceStable(candidates, func(i, j int) bool {
		// Newest first; tie-break by version/platform/arch.
		if candidates[i].DownloadedAt.Equal(candidates[j].DownloadedAt) {
			if candidates[i].Version == candidates[j].Version {
				if candidates[i].Platform == candidates[j].Platform {
					return candidates[i].Architecture < candidates[j].Architecture
				}
				return candidates[i].Platform < candidates[j].Platform
			}
			return candidates[i].Version > candidates[j].Version
		}
		return candidates[i].DownloadedAt.After(candidates[j].DownloadedAt)
	})
}

// dedupeDownloadsByInput keeps one artifact per version/platform/arch identity.
// This prevents duplicate targets from duplicate DB rows in a run.
func dedupeDownloadsByInput(candidates []*storage.Download) []*storage.Download {
	seenInputs := make(map[string]bool)
	out := make([]*storage.Download, 0, len(candidates))
	for _, dl := range candidates {
		inputKey := fmt.Sprintf("%s|%s|%s", dl.Version, dl.Platform, dl.Architecture)
		if seenInputs[inputKey] {
			continue
		}
		seenInputs[inputKey] = true
		out = append(out, dl)
	}
	return out
}

// buildResolveTargetsForDownloads expands deduplicated download inputs into concrete
// per-target package work items (rpm/apk).
func buildResolveTargetsForDownloads(
	runtimeName string,
	runtimeCfg config.Runtime,
	targets []packaging.PackageType,
	downloads []*storage.Download,
) []PackagingResolveTarget {
	resolved := make([]PackagingResolveTarget, 0, len(downloads)*len(targets))
	for _, dl := range downloads {
		packageName := renderPackageName(runtimeCfg.Packaging.PackageNameTemplate, runtimeName, dl.Version)
		installPrefix := renderInstallPrefix(runtimeCfg.Packaging.InstallPrefixTemplate, runtimeName, dl.Version, packageName)
		for _, target := range targets {
			resolved = append(resolved, PackagingResolveTarget{
				Runtime:       runtimeName,
				Version:       dl.Version,
				Target:        target,
				InputMode:     packaging.InputModeArchiveTarball,
				InputPlatform: dl.Platform,
				InputArch:     dl.Architecture,
				InputPath:     dl.LocalPath,
				InputSHA256:   dl.ContentSHA256,
				InputRunID:    dl.RunID,
				PackageName:   packageName,
				InstallPrefix: installPrefix,
			})
		}
	}
	return resolved
}

// normalizePackageTargets converts raw config target strings into a deduplicated,
// ordered set of supported package types. Unsupported values are ignored here
// because config validation is responsible for rejecting invalid targets.
func normalizePackageTargets(raw []string) []packaging.PackageType {
	seen := map[packaging.PackageType]bool{}
	out := make([]packaging.PackageType, 0, len(raw))
	for _, t := range raw {
		switch strings.ToLower(strings.TrimSpace(t)) {
		case string(packaging.PackageTypeRPM):
			if !seen[packaging.PackageTypeRPM] {
				seen[packaging.PackageTypeRPM] = true
				out = append(out, packaging.PackageTypeRPM)
			}
		case string(packaging.PackageTypeAPK):
			if !seen[packaging.PackageTypeAPK] {
				seen[packaging.PackageTypeAPK] = true
				out = append(out, packaging.PackageTypeAPK)
			}
		}
	}
	return out
}

// filterPackagingInputDownloads narrows runtime download rows to package-eligible
// inputs for orchestration. It enforces verification success, required integrity
// fields, run scoping, linux-only packaging, and archive-only inputs.
func filterPackagingInputDownloads(downloads []*storage.Download, runID string) []*storage.Download {
	out := make([]*storage.Download, 0, len(downloads))
	for _, dl := range downloads {
		if dl == nil {
			continue
		}
		if dl.VerificationStatus != verificationStatusSuccess {
			continue
		}
		if strings.TrimSpace(dl.LocalPath) == "" || strings.TrimSpace(dl.ContentSHA256) == "" {
			continue
		}
		if dl.RunID != runID {
			continue
		}
		if !strings.EqualFold(dl.Platform, platformOSLinux) {
			continue
		}
		if !isTarArchivePath(dl.LocalPath) {
			continue
		}
		out = append(out, dl)
	}
	return out
}

// isTarArchivePath reports whether a path looks like a supported tar archive
// input that can be staged into a packaging payload.
func isTarArchivePath(p string) bool {
	lower := strings.ToLower(strings.TrimSpace(filepath.Base(p)))
	return strings.HasSuffix(lower, ".tar.gz") ||
		strings.HasSuffix(lower, ".tgz") ||
		strings.HasSuffix(lower, ".tar.xz") ||
		strings.HasSuffix(lower, ".tar.bz2") ||
		strings.HasSuffix(lower, ".tar")
}

// renderPackageName expands package naming templates from runtime config into a
// concrete package name used by build and promotion identity keys.
func renderPackageName(tpl, runtimeName, version string) string {
	normalized := strings.TrimSpace(tpl)
	if normalized == "" {
		normalized = "OSPO-{runtime}"
	}
	normalized = strings.ReplaceAll(normalized, "{runtime}", runtimeName)
	normalized = strings.ReplaceAll(normalized, "{version}", version)
	return normalized
}

// renderInstallPrefix expands install-prefix templates from runtime config into
// a concrete on-disk installation root used by package build and idempotency keys.
func renderInstallPrefix(tpl, runtimeName, version, packageName string) string {
	normalized := strings.TrimSpace(tpl)
	if normalized == "" {
		normalized = "/export/apps/citools/{pkgname}/{version}"
	}
	normalized = strings.ReplaceAll(normalized, "{runtime}", runtimeName)
	normalized = strings.ReplaceAll(normalized, "{version}", version)
	normalized = strings.ReplaceAll(normalized, "{pkgname}", packageName)
	return normalized
}
