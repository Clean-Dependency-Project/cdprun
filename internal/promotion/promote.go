package promotion

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/clean-dependency-project/cdprun/internal/storage"
)

var (
	// ErrBlocked indicates at least one target failed gating and no database
	// mutation should be considered valid for this promotion attempt.
	ErrBlocked = errors.New("promotion blocked")
)

// BlockedReason describes why a promotion target was rejected.
type BlockedReason string

const (
	BlockedReasonTestFailed        BlockedReason = "test_failed"
	BlockedReasonMissingTestResult BlockedReason = "missing_test_result"
	BlockedReasonManifestNotTested BlockedReason = "manifest_not_tested"
	BlockedReasonReleaseNotFound   BlockedReason = "release_not_found"
)

// Target is a normalized promotion input entry derived from package manifest.
type Target struct {
	Runtime       string
	Version       string
	Target        string
	InputPlatform string
	InputArch     string
	InputSHA256   string
	PackageName   string
	InstallPrefix string
	Tested        bool
}

// TestResult captures one package test result record.
type TestResult struct {
	Runtime       string `json:"runtime"`
	Version       string `json:"version"`
	Target        string `json:"target"`
	InputSHA256   string `json:"input_sha256"`
	PackageName   string `json:"package_name"`
	InstallPrefix string `json:"install_prefix"`

	Passed bool `json:"passed"`

	PackageFilename string `json:"package_filename"`
	PackagePath     string `json:"package_path,omitempty"`
	PackageSHA256   string `json:"package_sha256"`
	PackageSize     int64  `json:"package_size"`
	PackageURL      string `json:"package_url,omitempty"`
}

// TestResultsFile is the test-stage JSON contract consumed by promotion.
type TestResultsFile struct {
	RunID   string       `json:"run_id"`
	Results []TestResult `json:"results"`
}

// BlockedEntry captures a blocked target and its reason.
type BlockedEntry struct {
	Runtime string       `json:"runtime"`
	Version string       `json:"version"`
	Target  string       `json:"target"`
	Reason  BlockedReason `json:"reason"`
	Detail  string       `json:"detail,omitempty"`
}

// Summary is the machine-readable promotion result.
type Summary struct {
	RunID           string         `json:"run_id"`
	EligibleEntries int            `json:"eligible_entries"`
	PromotedEntries int            `json:"promoted_entries"`
	BlockedEntries  int            `json:"blocked_entries"`
	Blocked         []BlockedEntry `json:"blocked,omitempty"`
}

// Store defines the database operations needed by promotion logic.
type Store interface {
	GetRelease(runtime, version string) (*storage.Release, error)
	AppendOrMergePlatformArtifact(runtime, version string, artifact storage.PlatformArtifact) error
	UpsertPackageRecord(record *storage.PackageRecord) error
}

// ReleaseAssetUploader uploads package assets to an existing release and
// returns the asset download URL.
type ReleaseAssetUploader interface {
	UploadPackageAsset(runtime, releaseTag, packagePath, uploadFilename string) (string, error)
}

// PromoteTestedPackages applies strict all-or-nothing promotion gating.
//
// If any target is blocked, ErrBlocked is returned and callers should treat
// the operation as failed.
func PromoteTestedPackages(db Store, uploader ReleaseAssetUploader, runID string, targets []Target, testResults TestResultsFile) (Summary, error) {
	if db == nil {
		return Summary{}, fmt.Errorf("database is required")
	}

	summary := Summary{
		RunID:   runID,
		Blocked: make([]BlockedEntry, 0),
	}

	resultsByKey := indexResultsByKey(testResults.Results)
	eligible, blocked, err := resolveEligibleEntries(db, targets, resultsByKey)
	if err != nil {
		return Summary{}, err
	}
	summary.Blocked = append(summary.Blocked, blocked...)

	summary.EligibleEntries = len(eligible)
	summary.BlockedEntries = len(summary.Blocked)
	if summary.BlockedEntries > 0 {
		return summary, ErrBlocked
	}

	promotedCount, err := applyEligiblePromotions(db, uploader, eligible)
	if err != nil {
		return Summary{}, err
	}
	summary.PromotedEntries = promotedCount

	return summary, nil
}

type entryAndResult struct {
	target Target
	result TestResult
}

func indexResultsByKey(results []TestResult) map[string]TestResult {
	out := make(map[string]TestResult, len(results))
	for _, result := range results {
		key := entryKey(result.Runtime, result.Version, result.Target, result.InputSHA256, result.PackageName, result.InstallPrefix)
		out[key] = result
	}
	return out
}

func resolveEligibleEntries(
	db Store,
	targets []Target,
	resultsByKey map[string]TestResult,
) ([]entryAndResult, []BlockedEntry, error) {
	eligible := make([]entryAndResult, 0, len(targets))
	blocked := make([]BlockedEntry, 0)
	for _, target := range targets {
		result, blockedEntry, err := gateTarget(db, target, resultsByKey)
		if err != nil {
			return nil, nil, err
		}
		if blockedEntry != nil {
			blocked = append(blocked, *blockedEntry)
			continue
		}
		eligible = append(eligible, entryAndResult{target: target, result: result})
	}
	return eligible, blocked, nil
}

func gateTarget(
	db Store,
	target Target,
	resultsByKey map[string]TestResult,
) (TestResult, *BlockedEntry, error) {
	if !target.Tested {
		return TestResult{}, &BlockedEntry{
			Runtime: target.Runtime,
			Version: target.Version,
			Target:  target.Target,
			Reason:  BlockedReasonManifestNotTested,
			Detail:  "manifest target is not marked tested",
		}, nil
	}

	key := entryKey(target.Runtime, target.Version, target.Target, target.InputSHA256, target.PackageName, target.InstallPrefix)
	result, ok := resultsByKey[key]
	if !ok {
		return TestResult{}, &BlockedEntry{
			Runtime: target.Runtime,
			Version: target.Version,
			Target:  target.Target,
			Reason:  BlockedReasonMissingTestResult,
			Detail:  "no matching test result entry",
		}, nil
	}
	if !result.Passed {
		return TestResult{}, &BlockedEntry{
			Runtime: target.Runtime,
			Version: target.Version,
			Target:  target.Target,
			Reason:  BlockedReasonTestFailed,
			Detail:  "test result is not passing",
		}, nil
	}
	if _, err := db.GetRelease(target.Runtime, target.Version); err != nil {
		if errors.Is(err, storage.ErrReleaseNotFound) {
			return TestResult{}, &BlockedEntry{
				Runtime: target.Runtime,
				Version: target.Version,
				Target:  target.Target,
				Reason:  BlockedReasonReleaseNotFound,
				Detail:  "release row missing for runtime/version",
			}, nil
		}
		return TestResult{}, nil, fmt.Errorf("get release for %s@%s: %w", target.Runtime, target.Version, err)
	}

	return result, nil, nil
}

func applyEligiblePromotions(db Store, uploader ReleaseAssetUploader, eligible []entryAndResult) (int, error) {
	now := time.Now().UTC()
	promotedCount := 0
	for _, item := range eligible {
		if err := promoteOne(db, uploader, item, now); err != nil {
			return 0, err
		}
		promotedCount++
	}
	return promotedCount, nil
}

func promoteOne(db Store, uploader ReleaseAssetUploader, item entryAndResult, promotedAt time.Time) error {
	resultWithURL, err := withResolvedPackageURL(db, uploader, item.target, item.result)
	if err != nil {
		return fmt.Errorf("resolve package url for %s@%s %s: %w", item.target.Runtime, item.target.Version, item.target.Target, err)
	}
	artifact, err := toPlatformArtifact(item.target, resultWithURL)
	if err != nil {
		return fmt.Errorf("build platform artifact for %s@%s %s: %w", item.target.Runtime, item.target.Version, item.target.Target, err)
	}
	if err := db.AppendOrMergePlatformArtifact(item.target.Runtime, item.target.Version, artifact); err != nil {
		return fmt.Errorf("append/merge release artifact for %s@%s %s: %w", item.target.Runtime, item.target.Version, item.target.Target, err)
	}
	record := &storage.PackageRecord{
		Runtime:         item.target.Runtime,
		Version:         item.target.Version,
		PackageType:     item.target.Target,
		PlatformOS:      item.target.InputPlatform,
		PlatformArch:    item.target.InputArch,
		InputSHA256:     item.target.InputSHA256,
		PackageName:     item.target.PackageName,
		InstallPrefix:   item.target.InstallPrefix,
		PackageFilename: item.result.PackageFilename,
		PackageSHA256:   item.result.PackageSHA256,
		BuildStatus:     "success",
		TestStatus:      "success",
		Promoted:        true,
		PromotedAt:      &promotedAt,
	}
	if err := db.UpsertPackageRecord(record); err != nil {
		return fmt.Errorf("upsert package record for %s@%s %s: %w", item.target.Runtime, item.target.Version, item.target.Target, err)
	}
	return nil
}

func withResolvedPackageURL(db Store, uploader ReleaseAssetUploader, target Target, result TestResult) (TestResult, error) {
	url := strings.TrimSpace(result.PackageURL)
	if url != "" {
		return result, nil
	}
	filename := strings.TrimSpace(result.PackageFilename)
	if filename == "" {
		filename = filepath.Base(strings.TrimSpace(result.PackagePath))
	}
	if filename == "" {
		return TestResult{}, fmt.Errorf("package filename is required when package_url is empty")
	}

	release, err := db.GetRelease(target.Runtime, target.Version)
	if err != nil {
		return TestResult{}, fmt.Errorf("get release: %w", err)
	}
	var artifacts storage.ReleaseArtifacts
	if err := json.Unmarshal([]byte(strings.TrimSpace(release.Artifacts)), &artifacts); err != nil {
		return TestResult{}, fmt.Errorf("parse release artifacts: %w", err)
	}

	for _, platformArtifact := range artifacts.Platforms {
		if platformArtifact.Binary == nil {
			continue
		}
		candidate := strings.TrimSpace(platformArtifact.Binary.Filename)
		if candidate == "" {
			continue
		}
		if candidate == filename || strings.HasSuffix(candidate, "__"+filename) {
			resolvedURL := strings.TrimSpace(platformArtifact.Binary.URL)
			if resolvedURL == "" {
				return TestResult{}, fmt.Errorf("release artifact %q has empty url", candidate)
			}
			result.PackageURL = resolvedURL
			return result, nil
		}
	}
	if uploader == nil {
		return TestResult{}, fmt.Errorf("uploaded release URL not found for package filename %q and uploader is not configured", filename)
	}
	packagePath := strings.TrimSpace(result.PackagePath)
	if packagePath == "" {
		return TestResult{}, fmt.Errorf("uploaded release URL not found for package filename %q and package_path is empty", filename)
	}
	releaseTag := strings.TrimSpace(release.ReleaseTag)
	if releaseTag == "" {
		return TestResult{}, fmt.Errorf("uploaded release URL not found for package filename %q and release_tag is empty", filename)
	}

	uploadedURL, err := uploader.UploadPackageAsset(target.Runtime, releaseTag, packagePath, filename)
	if err != nil {
		return TestResult{}, fmt.Errorf("upload package asset for %s@%s %s: %w", target.Runtime, target.Version, target.Target, err)
	}
	if strings.TrimSpace(uploadedURL) == "" {
		return TestResult{}, fmt.Errorf("upload package asset for %s@%s %s returned empty url", target.Runtime, target.Version, target.Target)
	}
	result.PackageURL = strings.TrimSpace(uploadedURL)
	return result, nil
}

func entryKey(runtime, version, target, inputSHA256, packageName, installPrefix string) string {
	return strings.ToLower(strings.TrimSpace(runtime)) + "|" +
		strings.ToLower(strings.TrimSpace(version)) + "|" +
		strings.ToLower(strings.TrimSpace(target)) + "|" +
		strings.ToLower(strings.TrimSpace(inputSHA256)) + "|" +
		strings.ToLower(strings.TrimSpace(packageName)) + "|" +
		strings.ToLower(strings.TrimSpace(installPrefix))
}

func toPlatformArtifact(target Target, result TestResult) (storage.PlatformArtifact, error) {
	filename := strings.TrimSpace(result.PackageFilename)
	if filename == "" {
		filename = filepath.Base(strings.TrimSpace(result.PackagePath))
	}
	if filename == "" {
		return storage.PlatformArtifact{}, fmt.Errorf("package filename is required")
	}
	if strings.TrimSpace(result.PackageSHA256) == "" {
		return storage.PlatformArtifact{}, fmt.Errorf("package sha256 is required")
	}

	url := strings.TrimSpace(result.PackageURL)
	if url == "" {
		return storage.PlatformArtifact{}, fmt.Errorf("package url is required")
	}

	platform := fmt.Sprintf("%s-%s-%s", target.InputPlatform, target.InputArch, strings.ToLower(strings.TrimSpace(target.Target)))
	artifact := storage.PlatformArtifact{
		Platform:     platform,
		PlatformOS:   target.InputPlatform,
		PlatformArch: target.InputArch,
		Binary: &storage.ArtifactFile{
			Filename:   filename,
			Size:       result.PackageSize,
			SHA256:     result.PackageSHA256,
			URL:        url,
			UploadedAt: time.Now().UTC(),
		},
	}

	// Include audit file info for RPM packages (audit file is uploaded alongside the RPM)
	if strings.ToLower(strings.TrimSpace(target.Target)) == "rpm" {
		auditFilename := filename + ".audit.json"
		artifact.Audit = &storage.AuditArtifact{
			Filename:   auditFilename,
			URL:        url + ".audit.json", // Same URL pattern as the RPM with .audit.json suffix
			UploadedAt: time.Now().UTC(),
		}
	}

	return artifact, nil
}
