// Package cli provides release functionality integrated with the download command.
package cli

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	"github.com/clean-dependency-project/cdprun/internal/config"
	"github.com/clean-dependency-project/cdprun/internal/packageexec"
	"github.com/clean-dependency-project/cdprun/internal/promotion"
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

	// Collect all runtime artifacts BEFORE creating the release.
	// We skip release creation entirely when no runtime artifacts were downloaded.
	runtimeArtifactFiles := make([]string, 0)
	packageArtifactFiles := make([]string, 0)
	seenRuntimeArtifactPath := make(map[string]struct{})
	seenPackageArtifactPath := make(map[string]struct{})
	appendUniqueArtifact := func(dst *[]string, seen map[string]struct{}, paths ...string) {
		for _, p := range paths {
			clean := strings.TrimSpace(p)
			if clean == "" {
				continue
			}
			if _, ok := seen[clean]; ok {
				continue
			}
			seen[clean] = struct{}{}
			*dst = append(*dst, clean)
		}
	}
	for _, version := range versions {
		runtimeFiles, err := rm.collectRuntimeArtifactFiles(outputDir, runtimeName, version)
		if err != nil {
			rm.stderr.Warn("failed to collect artifact files for version", "version", version, "error", err)
			continue
		}
		appendUniqueArtifact(&runtimeArtifactFiles, seenRuntimeArtifactPath, runtimeFiles...)
		packageFiles, err := rm.collectPackageArtifacts(outputDir, runtimeName, version)
		if err != nil {
			rm.stderr.Warn("failed to collect package artifacts for version", "version", version, "error", err)
			continue
		}
		appendUniqueArtifact(&packageArtifactFiles, seenPackageArtifactPath, packageFiles...)
	}

	// Skip release creation when no runtime artifacts were collected.
	if len(runtimeArtifactFiles) == 0 {
		rm.stdout.Info("skipping release creation - no runtime artifacts to upload",
			"runtime", runtimeName,
			"versions", versions)
		return nil, nil
	}

	// Include package artifacts only when runtime artifacts exist.
	allArtifactFiles := make([]string, 0, len(runtimeArtifactFiles)+len(packageArtifactFiles)+1)
	allArtifactFiles = append(allArtifactFiles, runtimeArtifactFiles...)
	allArtifactFiles = append(allArtifactFiles, packageArtifactFiles...)
	rm.stdout.Info("collected artifacts for upload",
		"runtime_artifact_count", len(runtimeArtifactFiles),
		"package_artifact_count", len(packageArtifactFiles),
		"count", len(allArtifactFiles))

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

	// Best-effort: include a single scripts.zip per release.
	// If scripts/ is missing (e.g., in tests), skip without failing the release.
	var scriptsZipPath string
	if p, err := rm.createScriptsZip(outputDir, "scripts.zip"); err != nil {
		rm.stderr.Warn("failed to create scripts zip", "error", err)
	} else if p != "" {
		scriptsZipPath = p
		appendUniqueArtifact(&allArtifactFiles, make(map[string]struct{}), scriptsZipPath)
		// cleanup after upload/buildArtifactsJSON completes
		defer func() {
			_ = os.Remove(scriptsZipPath)
			_ = os.RemoveAll(filepath.Dir(scriptsZipPath))
		}()
	}

	// Create GitHub release
	ghRelease, releaseURL, err := rm.createGitHubRelease(releaseTag, releaseName, releaseBody, releaseConfig.DraftRelease)
	if err != nil {
		return nil, err
	}

	// Upload all collected artifacts
	uploadedArtifacts, err := rm.uploadArtifacts(ghRelease.GetID(), allArtifactFiles)
	if err != nil {
		return nil, err
	}

	rm.stdout.Info("all artifacts uploaded successfully", "count", len(uploadedArtifacts))

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

// collectRuntimeArtifactFiles scans the output directory for runtime files related to this release.
// This includes binaries, audit.json files, signatures (.sig/.asc), certificates (.cert),
// and metadata/proof files (*.metadata.json).
// Empty (0-byte) files are skipped as they indicate failed downloads.
// For security-only releases where the binary version differs from the recorded version,
// this function uses the database to find the actual filenames.
func (rm *ReleaseManager) collectRuntimeArtifactFiles(outputDir, runtimeName, version string) ([]string, error) {
	files := make([]string, 0)
	seen := make(map[string]struct{})
	appendFile := func(path string, info os.FileInfo) {
		if info == nil || info.IsDir() || info.Size() == 0 {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		files = append(files, path)
	}

	// Get filenames from database for this version
	// This handles cases where the binary version differs from recorded version
	// (e.g., Python 3.12.12 uses python-3.12.10-amd64.exe for Windows/macOS)
	dbFilenames := make(map[string]bool)
	downloads, err := rm.db.ListByVersion(runtimeName, version)
	if err != nil {
		rm.stderr.Warn("failed to list downloads from database", "runtime", runtimeName, "version", version, "error", err)
	} else {
		for _, dl := range downloads {
			dbFilenames[dl.Filename] = true
			// Also add related files (signature, audit, metadata/proof).
			dbFilenames[dl.Filename+".asc"] = true
			dbFilenames[dl.Filename+".sig"] = true
			dbFilenames[dl.Filename+".audit.json"] = true
			dbFilenames[dl.Filename+".metadata.json"] = true
			ext := filepath.Ext(dl.Filename)
			stem := strings.TrimSuffix(dl.Filename, ext)
			if stem != dl.Filename {
				dbFilenames[stem+".audit.json"] = true
				dbFilenames[stem+".metadata.json"] = true
			}
		}
	}

	// Walk the output directory
	err = filepath.Walk(outputDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		// Skip empty files (failed downloads create 0-byte files)
		if info.Size() == 0 {
			rm.stdout.Debug("skipping empty file", "file", info.Name())
			return nil
		}

		// Include files that:
		// 1. Match the version pattern in filename (original behavior)
		// 2. OR are recorded in the database for this version (handles security-only releases)
		if strings.Contains(info.Name(), version) || dbFilenames[info.Name()] {
			appendFile(path, info)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return files, nil
}

func (rm *ReleaseManager) collectPackageArtifacts(outputDir, runtimeName, version string) ([]string, error) {
	files := make([]string, 0)
	seen := make(map[string]struct{})
	appendFile := func(path string, info os.FileInfo) {
		if info == nil || info.IsDir() || info.Size() == 0 {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		files = append(files, path)
	}

	packageEvidenceFiles := map[string]struct{}{
		"package-build-results.json":   {},
		"package-test-results.json":    {},
		"package-manifest.built.json":  {},
		"package-manifest.tested.json": {},
		"package-promote-summary.json": {},
	}

	baseDir := filepath.Dir(outputDir)
	artifactsDir := filepath.Join(baseDir, "artifacts")

	for evidenceFilename := range packageEvidenceFiles {
		evidencePath := filepath.Join(artifactsDir, evidenceFilename)
		info, err := os.Stat(evidencePath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("stat %s: %w", evidencePath, err)
		}
		appendFile(evidencePath, info)
	}

	candidatePackagePaths := make(map[string]struct{})
	buildResultsPath := filepath.Join(artifactsDir, "package-build-results.json")
	buildResults, err := readPackageBuildResults(buildResultsPath)
	if err != nil {
		rm.stderr.Warn("failed to parse package build results", "path", buildResultsPath, "error", err)
	}
	for _, record := range buildResults.Results {
		if !record.Success {
			continue
		}
		if record.Target.Runtime != runtimeName || record.Target.Version != version {
			continue
		}
		packagePath := strings.TrimSpace(record.Build.PackagePath)
		if packagePath != "" {
			candidatePackagePaths[packagePath] = struct{}{}
		}
	}

	testResultsPath := filepath.Join(artifactsDir, "package-test-results.json")
	testResults, err := readPackageTestResults(testResultsPath)
	if err != nil {
		rm.stderr.Warn("failed to parse package test results", "path", testResultsPath, "error", err)
	}
	for _, record := range testResults.Results {
		if !record.Passed {
			continue
		}
		if record.Runtime != runtimeName || record.Version != version {
			continue
		}
		packagePath := strings.TrimSpace(record.PackagePath)
		if packagePath != "" {
			candidatePackagePaths[packagePath] = struct{}{}
		}
	}

	for relPath := range candidatePackagePaths {
		fullPath := relPath
		if !filepath.IsAbs(fullPath) {
			fullPath = filepath.Join(baseDir, relPath)
		}
		info, err := os.Stat(fullPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("stat %s: %w", fullPath, err)
		}
		if packageArtifactFilename(filepath.Base(fullPath)) {
			appendFile(fullPath, info)
		}
	}

	return files, nil
}

func readPackageBuildResults(path string) (packageexec.BuildResultsFile, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return packageexec.BuildResultsFile{}, nil
		}
		return packageexec.BuildResultsFile{}, err
	}
	var results packageexec.BuildResultsFile
	if err := json.Unmarshal(content, &results); err != nil {
		return packageexec.BuildResultsFile{}, err
	}
	return results, nil
}

func readPackageTestResults(path string) (promotion.TestResultsFile, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return promotion.TestResultsFile{}, nil
		}
		return promotion.TestResultsFile{}, err
	}
	var results promotion.TestResultsFile
	if err := json.Unmarshal(content, &results); err != nil {
		return promotion.TestResultsFile{}, err
	}
	return results, nil
}

// createScriptsZip creates a scripts zip from the repo's scripts/ directory.
// The archive contains files under the "scripts/" prefix.
//
// If scripts/ does not exist (e.g., unit tests), it returns ("", nil).
func (rm *ReleaseManager) createScriptsZip(outputDir, zipName string) (string, error) {
	scriptsDir := "scripts"
	info, err := os.Stat(scriptsDir)
	if err != nil {
		if os.IsNotExist(err) {
			rm.stdout.Debug("scripts directory not found, skipping scripts zip", "scripts_dir", scriptsDir)
			return "", nil
		}
		return "", fmt.Errorf("stat scripts directory: %w", err)
	}
	if !info.IsDir() {
		rm.stdout.Debug("scripts path is not a directory, skipping scripts zip", "scripts_dir", scriptsDir)
		return "", nil
	}

	tmpDir, err := os.MkdirTemp("", "cdprun-scripts-")
	if err != nil {
		return "", fmt.Errorf("create temp dir for scripts zip: %w", err)
	}

	finalPath := filepath.Join(tmpDir, zipName)
	tmpFile, err := os.CreateTemp(tmpDir, zipName+".tmp-*")
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		return "", fmt.Errorf("create temp zip: %w", err)
	}
	tmpPath := tmpFile.Name()

	cleanup := func(closeErr error) error {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		_ = os.RemoveAll(tmpDir)
		return closeErr
	}

	zw := zip.NewWriter(tmpFile)
	if err := filepath.WalkDir(scriptsDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}

		rel, err := filepath.Rel(scriptsDir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)

		fi, err := d.Info()
		if err != nil {
			return err
		}
		hdr, err := zip.FileInfoHeader(fi)
		if err != nil {
			return err
		}
		hdr.Name = filepath.ToSlash(filepath.Join("scripts", rel))
		hdr.Method = zip.Deflate

		w, err := zw.CreateHeader(hdr)
		if err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()

		if _, err := io.Copy(w, f); err != nil {
			return err
		}
		return nil
	}); err != nil {
		if closeErr := zw.Close(); closeErr != nil {
			rm.stderr.Warn("failed to close zip writer after error", "error", closeErr)
		}
		return "", cleanup(fmt.Errorf("walk scripts dir: %w", err))
	}
	if err := zw.Close(); err != nil {
		return "", cleanup(fmt.Errorf("close zip writer: %w", err))
	}
	if err := tmpFile.Close(); err != nil {
		return "", cleanup(fmt.Errorf("close temp file: %w", err))
	}

	// Replace existing zip if present.
	_ = os.Remove(finalPath)
	if err := os.Rename(tmpPath, finalPath); err != nil {
		return "", cleanup(fmt.Errorf("rename scripts zip into place: %w", err))
	}

	rm.stdout.Debug("created scripts zip", "file", finalPath)
	return finalPath, nil
}

// artifactInfo contains metadata about an uploaded artifact.
type artifactInfo struct {
	URL              string
	SHA256           string
	Size             int64
	OriginalFilename string
	UploadedFilename string
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
	seenUploadNames := make(map[string]int)

	for _, filePath := range files {
		originalFilename := filepath.Base(filePath)
		uploadFilename := uniqueUploadFilename(filePath, seenUploadNames)
		rm.stdout.Info("uploading artifact", "file", originalFilename, "upload_filename", uploadFilename)

		var size int64
		if fi, err := os.Stat(filePath); err == nil {
			size = fi.Size()
		} else {
			rm.stderr.Warn("failed to stat file before upload", "file", originalFilename, "error", err)
		}

		// Calculate SHA256 before upload
		sha256Hash, err := calculateFileSHA256(filePath)
		if err != nil {
			rm.stderr.Warn("failed to calculate SHA256", "file", originalFilename, "error", err)
			// Continue with upload even if hash fails
		}

		uploadPath, cleanup, err := prepareUploadPath(filePath, uploadFilename)
		if err != nil {
			return nil, fmt.Errorf("failed to prepare upload path for %s: %w", originalFilename, err)
		}
		asset, err := rm.github.UploadAsset(releaseID, uploadPath)
		cleanup()
		if err != nil {
			return nil, fmt.Errorf("failed to upload %s: %w", originalFilename, err)
		}

		downloadURL := rm.github.GetAssetDownloadURL(asset)
		uploaded[uploadFilename] = artifactInfo{
			URL:              downloadURL,
			SHA256:           sha256Hash,
			Size:             size,
			OriginalFilename: originalFilename,
			UploadedFilename: uploadFilename,
		}

		rm.stdout.Info("artifact uploaded", "file", originalFilename, "upload_filename", uploadFilename, "url", downloadURL, "sha256", sha256Hash)
	}

	return uploaded, nil
}

func prepareUploadPath(filePath, uploadFilename string) (string, func(), error) {
	if filepath.Base(filePath) == uploadFilename {
		return filePath, func() {}, nil
	}

	tmpDir, err := os.MkdirTemp("", "cdprun-upload-*")
	if err != nil {
		return "", func() {}, fmt.Errorf("create temp dir: %w", err)
	}

	src, err := os.Open(filePath)
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		return "", func() {}, fmt.Errorf("open source file: %w", err)
	}
	defer func() { _ = src.Close() }()

	uploadPath := filepath.Join(tmpDir, uploadFilename)
	dst, err := os.Create(uploadPath)
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		return "", func() {}, fmt.Errorf("create upload file: %w", err)
	}
	if _, err := io.Copy(dst, src); err != nil {
		_ = dst.Close()
		_ = os.RemoveAll(tmpDir)
		return "", func() {}, fmt.Errorf("copy file content: %w", err)
	}
	if err := dst.Close(); err != nil {
		_ = os.RemoveAll(tmpDir)
		return "", func() {}, fmt.Errorf("close upload file: %w", err)
	}

	cleanup := func() {
		_ = os.RemoveAll(tmpDir)
	}
	return uploadPath, cleanup, nil
}

func uniqueUploadFilename(filePath string, seen map[string]int) string {
	base := filepath.Base(filePath)
	if seen[base] == 0 {
		seen[base] = 1
		return base
	}

	qualifier := platformQualifierFromPath(filePath)
	candidate := base
	if qualifier != "" {
		candidate = qualifier + "__" + base
	}
	if seen[candidate] == 0 {
		seen[candidate] = 1
		return candidate
	}

	for i := 2; ; i++ {
		named := fmt.Sprintf("%s__%d", candidate, i)
		if seen[named] == 0 {
			seen[named] = 1
			return named
		}
	}
}

func platformQualifierFromPath(filePath string) string {
	parent := filepath.Base(filepath.Dir(filePath))
	normalized := strings.ToLower(parent)
	if strings.HasPrefix(normalized, "windows-") || strings.HasPrefix(normalized, "linux-") ||
		strings.HasPrefix(normalized, "mac-") || strings.HasPrefix(normalized, "darwin-") {
		return parent
	}
	return ""
}

// buildArtifactsJSON creates the JSON structure for storage in the database.
func (rm *ReleaseManager) buildArtifactsJSON(
	uploadedArtifacts map[string]artifactInfo,
	downloadResults []runtime.DownloadResult,
) (string, error) {
	// Group artifacts by platform
	platforms := make(map[string]*storage.PlatformArtifact)
	commonFiles := []storage.CommonFile{}

	for key, info := range uploadedArtifacts {
		uploadedFilename := info.UploadedFilename
		if uploadedFilename == "" {
			uploadedFilename = key
		}
		originalFilename := info.OriginalFilename
		if originalFilename == "" {
			originalFilename = uploadedFilename
		}

		fileInfo, err := getFileInfo(originalFilename, info.URL, downloadResults)
		if err != nil {
			rm.stderr.Warn("failed to get file info", "file", originalFilename, "error", err)
			continue
		}

		if fileInfo.IsCommonFile {
			size := fileInfo.Size
			if size == 0 {
				size = info.Size
			}
			commonFiles = append(commonFiles, storage.CommonFile{
				Type:       fileInfo.Type,
				Filename:   uploadedFilename,
				Size:       size,
				SHA256:     info.SHA256,
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
		artifactSize := fileInfo.Size
		if artifactSize == 0 {
			artifactSize = info.Size
		}

		// Categorize the file
		switch {
		case strings.HasSuffix(originalFilename, ".audit.json"):
			plat.Audit = &storage.AuditArtifact{
				Filename:   uploadedFilename,
				Size:       artifactSize,
				URL:        info.URL,
				UploadedAt: time.Now(),
			}
		case strings.HasSuffix(originalFilename, ".sig"), strings.HasSuffix(originalFilename, ".asc"):
			plat.Signature = &storage.ArtifactFile{
				Filename:   uploadedFilename,
				Size:       artifactSize,
				SHA256:     info.SHA256,
				URL:        info.URL,
				UploadedAt: time.Now(),
			}
		case strings.HasSuffix(originalFilename, ".cert"):
			plat.Certificate = &storage.ArtifactFile{
				Filename:   uploadedFilename,
				Size:       artifactSize,
				SHA256:     info.SHA256,
				URL:        info.URL,
				UploadedAt: time.Now(),
			}
		case strings.HasSuffix(originalFilename, ".metadata.json"):
			plat.MetadataFile = &storage.ArtifactFile{
				Filename:   uploadedFilename,
				Size:       artifactSize,
				SHA256:     info.SHA256,
				URL:        info.URL,
				UploadedAt: time.Now(),
			}
		default:
			// Binary artifact
			plat.Binary = &storage.ArtifactFile{
				Filename:   uploadedFilename,
				Size:       artifactSize,
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
	switch filename {
	case "package-build-results.json":
		return &fileInfo{Type: "package_build_results", IsCommonFile: true}, nil
	case "package-test-results.json":
		return &fileInfo{Type: "package_test_results", IsCommonFile: true}, nil
	case "package-manifest.built.json":
		return &fileInfo{Type: "package_manifest_built", IsCommonFile: true}, nil
	case "package-manifest.tested.json":
		return &fileInfo{Type: "package_manifest_tested", IsCommonFile: true}, nil
	case "package-promote-summary.json":
		return &fileInfo{Type: "package_promote_summary", IsCommonFile: true}, nil
	}

	// scripts.zip (release-level) and scripts-{version}.zip (legacy) are common files we attach to releases.
	if filename == "scripts.zip" || (strings.HasPrefix(filename, "scripts-") && strings.HasSuffix(filename, ".zip")) {
		return &fileInfo{
			Type:         "scripts",
			IsCommonFile: true,
		}, nil
	}

	// Tomcat publishes per-artifact checksum files as <artifact>.sha512.
	// These should not be treated as platform binaries in the index.
	if strings.HasSuffix(filename, ".sha512") {
		return &fileInfo{
			Type:         "checksum_file",
			IsCommonFile: true,
		}, nil
	}

	// Check if it's a common file (checksum, signature)
	if strings.Contains(filename, "SHASUMS") || strings.Contains(filename, "checksums") {
		return &fileInfo{
			Type:         "checksum_file",
			IsCommonFile: true,
		}, nil
	}

	if packageArtifactFilename(filename) {
		return &fileInfo{
			Version: extractVersionFromFilename(filename),
			OS:      "linux",
			Arch:    packageArchFromFilename(filename),
			Type:    "binary",
		}, nil
	}

	// Related verification/proof files may append known suffixes to the binary name.
	// Try resolving back to the original binary filename and map platform via downloadResults.
	baseFilename := filename
	for _, suffix := range []string{".audit.json", ".metadata.json", ".asc", ".sig", ".cert"} {
		if strings.HasSuffix(baseFilename, suffix) {
			baseFilename = strings.TrimSuffix(baseFilename, suffix)
			break
		}
	}
	if baseFilename != filename {
		for _, result := range downloadResults {
			binaryFilename := filepath.Base(result.LocalPath)
			binaryStem := strings.TrimSuffix(binaryFilename, filepath.Ext(binaryFilename))
			if binaryFilename == baseFilename || binaryStem == baseFilename {
				return &fileInfo{
					Size:    result.FileSize,
					Version: result.Version,
					OS:      result.Platform.OS,
					Arch:    result.Platform.Arch,
					Type:    "artifact",
				}, nil
			}
		}
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
//
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
	body += "- checksum verified\n"
	if !hasGPGFailure {
		body += "- GPG signature verified\n"
	}
	body += "- ClamAV scanned\n"

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
	versionPattern := regexp.MustCompile(`\d+\.\d+\.\d+`)
	if version := versionPattern.FindString(filename); version != "" {
		return version
	}

	// Try to find version pattern like v22.15.0
	parts := strings.Split(filename, "-")
	for _, part := range parts {
		if strings.HasPrefix(part, "v") && len(part) > 1 {
			return strings.TrimPrefix(part, "v")
		}
	}
	return ""
}

func packageArtifactFilename(filename string) bool {
	lower := strings.ToLower(strings.TrimSpace(filename))
	return strings.HasSuffix(lower, ".rpm") ||
		strings.HasSuffix(lower, ".apk") ||
		strings.HasSuffix(lower, ".tgz") ||
		strings.HasSuffix(lower, ".tar.gz")
}

func packageArchFromFilename(filename string) string {
	lower := strings.ToLower(filename)
	switch {
	case strings.Contains(lower, "aarch64"), strings.Contains(lower, "arm64"):
		return "aarch64"
	case strings.Contains(lower, "x86_64"), strings.Contains(lower, "amd64"), strings.Contains(lower, "x64"):
		return "x64"
	default:
		return "x64"
	}
}
