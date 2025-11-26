// Package cli provides release functionality integrated with the download command.
package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	"github.com/clean-dependency-project/cdprun/internal/config"
	"github.com/clean-dependency-project/cdprun/internal/runtime"
	"github.com/clean-dependency-project/cdprun/internal/storage"
	"github.com/google/go-github/v57/github"
)

// ReleaseManager handles the GitHub release process after successful downloads.
// It accepts interfaces for testability (Dave Cheney's "accept interfaces, return structs").
type ReleaseManager struct {
	db     DatabaseStore
	github GitHubReleaser
	stdout *slog.Logger
	stderr *slog.Logger
}

// NewReleaseManager creates a new release manager with the provided dependencies.
// Returns nil if auto_release is disabled in the configuration.
// GitHub client and database are passed as interfaces for testability.
func NewReleaseManager(cfg *config.ReleaseConfig, github GitHubReleaser, db DatabaseStore, stdout, stderr *slog.Logger) (*ReleaseManager, error) {
	if !cfg.AutoRelease {
		return nil, nil // Release disabled
	}

	if cfg.GitHubRepository == "" {
		return nil, fmt.Errorf("github_repository is required when auto_release is enabled")
	}

	if github == nil {
		return nil, fmt.Errorf("github client is required when auto_release is enabled")
	}

	if db == nil {
		return nil, fmt.Errorf("database is required when auto_release is enabled")
	}

	return &ReleaseManager{
		db:     db,
		github: github,
		stdout: stdout,
		stderr: stderr,
	}, nil
}

// CreateAggregatedRelease creates a single GitHub release with artifacts from multiple versions.
// This is called after successful downloads for multiple runtime versions.
func (rm *ReleaseManager) CreateAggregatedRelease(
	runtimeName string,
	versions []string,
	downloadResults []runtime.DownloadResult,
	outputDir string,
	releaseConfig *config.ReleaseConfig,
) (*storage.Release, error) {
	rm.stdout.Info("creating aggregated GitHub release",
		"runtime", runtimeName,
		"versions", versions)

	// Use first version for semver (or could use latest)
	// For aggregated releases, semver is less meaningful
	var major, minor, patch int
	if len(versions) > 0 {
		var err error
		major, minor, patch, err = storage.ParseSemver(versions[0])
		if err != nil {
			// If semver parsing fails, use zeros (aggregated releases may have non-standard versions)
			major, minor, patch = 0, 0, 0
		}
	}

	// Generate release metadata
	timestamp := time.Now().UTC().Format("20060102T150405Z")
	releaseTag := fmt.Sprintf("%s-multi-%s", runtimeName, timestamp)
	releaseName := fmt.Sprintf("%s (multi) %s", cases.Title(language.English).String(runtimeName), time.Now().UTC().Format("2006-01-02"))
	releaseBody := rm.generateAggregatedReleaseBody(runtimeName, versions, downloadResults)

	// Create GitHub release
	ghRelease, releaseURL, err := rm.createGitHubRelease(releaseTag, releaseName, releaseBody, releaseConfig.DraftRelease)
	if err != nil {
		return nil, err
	}

	// Upload all artifacts (across all versions)
	uploadedArtifacts, err := rm.uploadAllAggregatedArtifacts(ghRelease.GetID(), outputDir, runtimeName, versions)
	if err != nil {
		return nil, err
	}

	// Build artifacts JSON structure
	artifactsJSON, err := rm.buildArtifactsJSON(uploadedArtifacts, downloadResults)
	if err != nil {
		return nil, fmt.Errorf("failed to build artifacts JSON: %w", err)
	}

	// Create database release record with aggregated version string
	versionStr := strings.Join(versions, ",")
	release := &storage.Release{
		Runtime:     runtimeName,
		Version:     versionStr,
		SemverMajor: major,
		SemverMinor: minor,
		SemverPatch: patch,
		ReleaseTag:  releaseTag,
		ReleaseURL:  releaseURL,
		Artifacts:   artifactsJSON,
		CreatedAt:   time.Now(),
	}

	if err := rm.db.CreateRelease(release); err != nil {
		return nil, fmt.Errorf("failed to record release in database: %w", err)
	}

	rm.stdout.Info("aggregated release recorded to database", "id", release.ID)

	return release, nil
}

// createGitHubRelease creates a GitHub release and returns the release and its URL.
func (rm *ReleaseManager) createGitHubRelease(tag, name, body string, draft bool) (*github.RepositoryRelease, string, error) {
	rm.stdout.Info("creating GitHub release", "tag", tag, "name", name)

	ghRelease, err := rm.github.CreateRelease(tag, name, body, draft)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create GitHub release: %w", err)
	}

	releaseURL := rm.github.GetReleaseURL(ghRelease)
	rm.stdout.Info("GitHub release created", "url", releaseURL)

	return ghRelease, releaseURL, nil
}

// uploadAllAggregatedArtifacts collects and uploads artifacts for multiple versions.
func (rm *ReleaseManager) uploadAllAggregatedArtifacts(releaseID int64, outputDir, runtimeName string, versions []string) (map[string]artifactInfo, error) {
	var allArtifactFiles []string

	// Collect artifacts for all versions
	for _, version := range versions {
		artifactFiles, err := rm.collectArtifactFiles(outputDir, runtimeName, version)
		if err != nil {
			rm.stderr.Warn("failed to collect artifact files for version", "version", version, "error", err)
			continue
		}
		allArtifactFiles = append(allArtifactFiles, artifactFiles...)
	}

	rm.stdout.Info("collected artifacts for upload", "count", len(allArtifactFiles))

	uploadedArtifacts, err := rm.uploadArtifacts(releaseID, allArtifactFiles)
	if err != nil {
		return nil, fmt.Errorf("failed to upload artifacts: %w", err)
	}

	rm.stdout.Info("all artifacts uploaded successfully", "count", len(uploadedArtifacts))

	return uploadedArtifacts, nil
}

// collectArtifactFiles scans the output directory for all artifact files related to this release.
// This includes binaries, audit.json files, signatures (.sig), and certificates (.cert).
func (rm *ReleaseManager) collectArtifactFiles(outputDir, runtimeName, version string) ([]string, error) {
	var files []string

	// Walk the output directory
	err := filepath.Walk(outputDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		// Include all files that match the version pattern
		// This covers binaries, audit.json, signatures, and certificates
		if strings.Contains(info.Name(), version) {
			files = append(files, path)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return files, nil
}

// artifactInfo contains metadata about an uploaded artifact.
type artifactInfo struct {
	URL    string
	SHA256 string
}

// calculateFileSHA256 computes the SHA256 hash of a file.
func calculateFileSHA256(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open file: %w", err)
	}
	defer func() { _ = file.Close() }()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("failed to hash file: %w", err)
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

// uploadArtifacts uploads all artifact files to the GitHub release.
// Returns a map of filename -> artifact info (URL + SHA256).
func (rm *ReleaseManager) uploadArtifacts(releaseID int64, files []string) (map[string]artifactInfo, error) {
	uploaded := make(map[string]artifactInfo)

	for _, filePath := range files {
		filename := filepath.Base(filePath)
		rm.stdout.Info("uploading artifact", "file", filename)

		// Calculate SHA256 before upload
		sha256Hash, err := calculateFileSHA256(filePath)
		if err != nil {
			rm.stderr.Warn("failed to calculate SHA256", "file", filename, "error", err)
			// Continue with upload even if hash fails
		}

		asset, err := rm.github.UploadAsset(releaseID, filePath)
		if err != nil {
			return nil, fmt.Errorf("failed to upload %s: %w", filename, err)
		}

		downloadURL := rm.github.GetAssetDownloadURL(asset)
		uploaded[filename] = artifactInfo{
			URL:    downloadURL,
			SHA256: sha256Hash,
		}

		rm.stdout.Info("artifact uploaded", "file", filename, "url", downloadURL, "sha256", sha256Hash)
	}

	return uploaded, nil
}

// buildArtifactsJSON creates the JSON structure for storage in the database.
func (rm *ReleaseManager) buildArtifactsJSON(
	uploadedArtifacts map[string]artifactInfo,
	downloadResults []runtime.DownloadResult,
) (string, error) {
	// Group artifacts by platform
	platforms := make(map[string]*storage.PlatformArtifact)
	commonFiles := []storage.CommonFile{}

	for filename, info := range uploadedArtifacts {
		fileInfo, err := getFileInfo(filename, info.URL, downloadResults)
		if err != nil {
			rm.stderr.Warn("failed to get file info", "file", filename, "error", err)
			continue
		}

		if fileInfo.IsCommonFile {
			commonFiles = append(commonFiles, storage.CommonFile{
				Type:       fileInfo.Type,
				Filename:   filename,
				Size:       fileInfo.Size,
				URL:        info.URL,
				UploadedAt: time.Now(),
			})
			continue
		}

		// Platform-specific artifact
		// Use version + platform as key to avoid overwriting different versions of the same platform
		platformKey := fmt.Sprintf("%s-%s-%s", fileInfo.Version, fileInfo.OS, fileInfo.Arch)
		basePlatformKey := fmt.Sprintf("%s-%s", fileInfo.OS, fileInfo.Arch)
		if _, exists := platforms[platformKey]; !exists {
			platforms[platformKey] = &storage.PlatformArtifact{
				Platform:     basePlatformKey,
				PlatformOS:   fileInfo.OS,
				PlatformArch: fileInfo.Arch,
			}
		}

		plat := platforms[platformKey]

		// Categorize the file
		switch {
		case strings.HasSuffix(filename, ".audit.json"):
			plat.Audit = &storage.AuditArtifact{
				Filename:   filename,
				Size:       fileInfo.Size,
				URL:        info.URL,
				UploadedAt: time.Now(),
			}
		case strings.HasSuffix(filename, ".sig"):
			plat.Signature = &storage.ArtifactFile{
				Filename:   filename,
				Size:       fileInfo.Size,
				SHA256:     info.SHA256,
				URL:        info.URL,
				UploadedAt: time.Now(),
			}
		case strings.HasSuffix(filename, ".cert"):
			plat.Certificate = &storage.ArtifactFile{
				Filename:   filename,
				Size:       fileInfo.Size,
				SHA256:     info.SHA256,
				URL:        info.URL,
				UploadedAt: time.Now(),
			}
		default:
			// Binary artifact
			plat.Binary = &storage.ArtifactFile{
				Filename:   filename,
				Size:       fileInfo.Size,
				SHA256:     info.SHA256,
				URL:        info.URL,
				UploadedAt: time.Now(),
			}
		}
	}

	// Convert map to slice
	var platformSlice []storage.PlatformArtifact
	for _, plat := range platforms {
		platformSlice = append(platformSlice, *plat)
	}

	artifacts := storage.ReleaseArtifacts{
		Platforms:   platformSlice,
		CommonFiles: commonFiles,
		Metadata: storage.ArtifactsMetadata{
			TotalArtifacts: len(uploadedArtifacts),
			PlatformCount:  len(platforms),
		},
	}

	data, err := json.Marshal(artifacts)
	if err != nil {
		return "", err
	}

	return string(data), nil
}

// fileInfo contains extracted information about a file.
type fileInfo struct {
	Size         int64
	Version      string
	OS           string
	Arch         string
	Type         string
	IsCommonFile bool
}

// getFileInfo extracts information about a file from its name and download results.
func getFileInfo(filename, url string, downloadResults []runtime.DownloadResult) (*fileInfo, error) {
	// Check if it's a common file (checksum, signature)
	if strings.Contains(filename, "SHASUMS") || strings.Contains(filename, "checksums") {
		return &fileInfo{
			Type:         "checksum_file",
			IsCommonFile: true,
		}, nil
	}

	// Try to find matching download result for platform-specific files
	for _, result := range downloadResults {
		if filepath.Base(result.LocalPath) == filename {
			return &fileInfo{
				Size:    result.FileSize,
				Version: result.Version,
				OS:      result.Platform.OS,
				Arch:    result.Platform.Arch,
				Type:    "binary",
			}, nil
		}
	}

	// If no match found, extract from filename pattern (best effort)
	// This handles audit.json, .sig, .cert files
	// Extract version from filename (e.g., node-v22.15.0-linux-x64.tar.xz -> 22.15.0)
	version := extractVersionFromFilename(filename)

	parts := strings.Split(filename, "-")
	if len(parts) >= 2 {
		// Common patterns: node-v22.15.0-linux-x64.tar.xz
		for i, part := range parts {
			if part == "linux" || part == "darwin" || part == "win" {
				os := part
				arch := "x64" // default
				if i+1 < len(parts) {
					arch = strings.Split(parts[i+1], ".")[0]
				}
				return &fileInfo{
					Version: version,
					OS:      os,
					Arch:    arch,
					Type:    "artifact",
				}, nil
			}
		}
	}

	return &fileInfo{
		Version: version,
		Type:    "unknown",
	}, nil
}

// formatReleaseName generates the release name from template.
// This function is used in tests.
//nolint:unused // Used in release_test.go
func (rm *ReleaseManager) formatReleaseName(template, runtime, version string) string {
	if template == "" {
		return fmt.Sprintf("%s %s", runtime, version)
	}

	name := strings.ReplaceAll(template, "{runtime}", runtime)
	name = strings.ReplaceAll(name, "{version}", version)
	return name
}

// generateAggregatedReleaseBody creates a release body for multiple versions.
// Includes sections for platform binaries, common files, and verification status.
func (rm *ReleaseManager) generateAggregatedReleaseBody(runtime string, versions []string, results []runtime.DownloadResult) string {
	// Header with all versions
	versionList := strings.Join(versions, ", ")
	body := fmt.Sprintf("# %s %s\n\n", cases.Title(language.English).String(runtime), versionList)
	body += "Automatically generated release containing verified runtime binaries.\n\n"

	// Group files by type
	type platformFile struct {
		os       string
		arch     string
		version  string
		filename string
	}
	var binaries []platformFile
	var commonFiles []string

	// Check for GPG verification failures in any result
	hasGPGFailure := false

	for _, result := range results {
		if !result.Success {
			continue
		}

		filename := filepath.Base(result.LocalPath)

		// Check if this is a common file (SHASUMS, etc)
		if strings.HasPrefix(filename, "SHASUMS") ||
			strings.HasPrefix(filename, "SHA256SUMS") ||
			strings.HasSuffix(filename, ".txt.sig") ||
			strings.HasSuffix(filename, ".txt.asc") {
			if !contains(commonFiles, filename) {
				commonFiles = append(commonFiles, filename)
			}
			continue
		}

		// Extract version from filename (e.g., "node-v22.15.0-linux-x64.tar.xz" -> "22.15.0")
		fileVersion := extractVersionFromFilename(filename)

		pf := platformFile{
			os:       result.Platform.OS,
			arch:     result.Platform.Arch,
			version:  fileVersion,
			filename: filename,
		}

		// Check for GPG failures in audit files
		if strings.HasSuffix(filename, ".audit.json") {
			auditPath := result.LocalPath
			if data, err := os.ReadFile(auditPath); err == nil {
				if strings.Contains(string(data), "\"gpg_verified\":false") ||
					strings.Contains(string(data), "GPG verification failed") {
					hasGPGFailure = true
				}
			}
		} else if !strings.HasSuffix(filename, ".sig") && !strings.HasSuffix(filename, ".cert") {
			binaries = append(binaries, pf)
		}
	}

	// Included Platforms section
	body += "## Included Platforms\n\n"

	// Group by platform
	platformGroups := make(map[string][]platformFile)
	for _, bin := range binaries {
		key := fmt.Sprintf("%s-%s", bin.os, bin.arch)
		platformGroups[key] = append(platformGroups[key], bin)
	}

	for platform, files := range platformGroups {
		for _, file := range files {
			body += fmt.Sprintf("- %s (%s)\n", platform, file.filename)
		}
	}

	// Verification section
	body += "\n## Verification\n\n"
	body += "All binaries have been:\n"
	body += "- ✓ Checksum verified\n"
	if !hasGPGFailure {
		body += "- ✓ GPG signature verified\n"
	}
	body += "- ✓ ClamAV scanned\n"

	return body
}

// contains checks if a string slice contains a value.
func contains(slice []string, val string) bool {
	for _, item := range slice {
		if item == val {
			return true
		}
	}
	return false
}

// extractVersionFromFilename extracts version string from a filename.
// Example: "node-v22.15.0-linux-x64.tar.xz" -> "22.15.0"
func extractVersionFromFilename(filename string) string {
	// Try to find version pattern like v22.15.0
	parts := strings.Split(filename, "-")
	for _, part := range parts {
		if strings.HasPrefix(part, "v") && len(part) > 1 {
			return strings.TrimPrefix(part, "v")
		}
	}
	return ""
}
