// Package temurin provides the Temurin (Eclipse Adoptium) runtime adapter for the unified runtime download system.
// It integrates with the existing endoflife package and temurin-specific functionality
// to provide version discovery, policy application, and download coordination with GPG verification.
package temurin

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/clean-dependency-project/cdprun/internal/config"
	"github.com/clean-dependency-project/cdprun/internal/endoflife"
	"github.com/clean-dependency-project/cdprun/internal/gpg"
	"github.com/clean-dependency-project/cdprun/internal/platform"
	"github.com/clean-dependency-project/cdprun/internal/runtime"
)

const (
	Temurin = "temurin"
)

//go:embed keys/*.asc
var embeddedTemurinKeys embed.FS

// AdoptiumAPIResponse represents the response from the Adoptium API
type AdoptiumAPIResponse []AdoptiumRelease

// AdoptiumRelease represents a single release from the Adoptium API
type AdoptiumRelease struct {
	Binaries      []AdoptiumBinary `json:"binaries"`
	DownloadCount int64            `json:"download_count"`
	ID            string           `json:"id"`
	ReleaseLink   string           `json:"release_link"`
	ReleaseName   string           `json:"release_name"`
	ReleaseType   string           `json:"release_type"`
	Source        AdoptiumSource   `json:"source"`
	Timestamp     string           `json:"timestamp"`
	UpdatedAt     string           `json:"updated_at"`
	Vendor        string           `json:"vendor"`
	VersionData   AdoptiumVersion  `json:"version_data"`
}

// AdoptiumBinary represents a binary package from the Adoptium API
type AdoptiumBinary struct {
	Architecture  string           `json:"architecture"`
	DownloadCount int64            `json:"download_count"`
	HeapSize      string           `json:"heap_size"`
	ImageType     string           `json:"image_type"`
	JVMImpl       string           `json:"jvm_impl"`
	OS            string           `json:"os"`
	Package       AdoptiumPackage  `json:"package"`
	Installer     *AdoptiumPackage `json:"installer,omitempty"`
	Project       string           `json:"project"`
	SCMRef        string           `json:"scm_ref"`
	UpdatedAt     string           `json:"updated_at"`
	CLib          string           `json:"c_lib,omitempty"`
}

// AdoptiumPackage represents package information from the Adoptium API
type AdoptiumPackage struct {
	Checksum      string `json:"checksum"`
	ChecksumLink  string `json:"checksum_link"`
	DownloadCount int64  `json:"download_count"`
	Link          string `json:"link"`
	MetadataLink  string `json:"metadata_link"`
	Name          string `json:"name"`
	SignatureLink string `json:"signature_link"`
	Size          int64  `json:"size"`
}

// AdoptiumSource represents source information from the Adoptium API
type AdoptiumSource struct {
	Link string `json:"link"`
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// AdoptiumVersion represents version information from the Adoptium API
type AdoptiumVersion struct {
	Build          int    `json:"build"`
	Major          int    `json:"major"`
	Minor          int    `json:"minor"`
	OpenJDKVersion string `json:"openjdk_version"`
	Security       int    `json:"security"`
	Semver         string `json:"semver"`
}

// AdoptiumReleaseVersions represents the response from the Adoptium release versions API
type AdoptiumReleaseVersions struct {
	Versions []AdoptiumVersionInfo `json:"versions"`
}

// AdoptiumVersionInfo represents version information from the Adoptium API
type AdoptiumVersionInfo struct {
	Build          int    `json:"build"`
	Major          int    `json:"major"`
	Minor          int    `json:"minor"`
	OpenJDKVersion string `json:"openjdk_version"`
	Optional       string `json:"optional,omitempty"`
	Security       int    `json:"security"`
	Semver         string `json:"semver"`
}

// TemurinAdapter implements the RuntimeProvider interface for Temurin (Eclipse Adoptium).
// It bridges the existing temurin package functionality with the unified runtime system.
type TemurinAdapter struct {
	endoflifeClient endoflife.Client
	policyLoader    endoflife.PolicyLoader
	downloader      *runtime.ConcurrentDownloader
	config          *config.Runtime
	globalConfig    *config.GlobalConfig
	stdout          *slog.Logger
	stderr          *slog.Logger
	httpClient      *http.Client
}

// NewAdapter creates a new Temurin adapter with default configuration.
func NewAdapter(eolClient endoflife.Client) runtime.RuntimeProvider {
	// Check for GitHub token since Temurin downloads come from GitHub
	if getGitHubTokenFromEnv() == "" {
		slog.Default().Warn("GitHub token not found for Temurin downloads",
			"impact", "Temurin downloads will fail due to GitHub rate limits (60 requests/hour)",
			"solution", "set GITHUB_TOKEN environment variable before downloading",
			"setup_url", "https://github.com/settings/tokens",
			"env_vars", "GITHUB_TOKEN, GH_TOKEN, or GITHUB_ACCESS_TOKEN")
	}

	return &TemurinAdapter{
		endoflifeClient: eolClient,
		policyLoader:    endoflife.NewJSONPolicyLoader(),
		downloader:      runtime.NewConcurrentDownloader(3, 30*time.Second, slog.Default(), slog.Default()),
		stdout:          slog.Default(),
		stderr:          slog.Default(),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// NewAdapterWithConfig creates a new Temurin adapter with custom configuration.
func NewAdapterWithConfig(eolClient endoflife.Client, cfg *config.Runtime, globalCfg *config.GlobalConfig, stdout, stderr *slog.Logger) runtime.RuntimeProvider {
	// Check for GitHub token since Temurin downloads come from GitHub
	// NOTE: DownloadConfig currently has no disable flag for this warning.
	if getGitHubTokenFromEnv() == "" {
		stderr.Warn("GitHub token not found for Temurin downloads",
			"impact", "Temurin downloads will fail due to GitHub rate limits (60 requests/hour)",
			"solution", "set GITHUB_TOKEN environment variable before downloading",
			"setup_url", "https://github.com/settings/tokens",
			"env_vars", "GITHUB_TOKEN, GH_TOKEN, or GITHUB_ACCESS_TOKEN")
	}

	// Parse timeout from global config, fallback to 30s if not available
	timeout := 30 * time.Second
	if globalCfg != nil {
		timeout = globalCfg.GetDownloadTimeout()
	}

	return &TemurinAdapter{
		endoflifeClient: eolClient,
		policyLoader:    endoflife.NewJSONPolicyLoader(),
		downloader:      runtime.NewConcurrentDownloader(3, timeout, stdout, stderr),
		config:          cfg,
		globalConfig:    globalCfg,
		stdout:          stdout,
		stderr:          stderr,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

// SetConfig sets the runtime configuration for this adapter.
func (a *TemurinAdapter) SetConfig(cfg *config.Runtime) {
	a.config = cfg
}

// GetName returns the unique identifier for the Temurin runtime.
func (a *TemurinAdapter) GetName() string {
	return "temurin"
}

// GetEndOfLifeProduct returns the endoflife.date product name.
func (a *TemurinAdapter) GetEndOfLifeProduct() string {
	return "temurin" // Use correct API endpoint name
}

// GetSupportedPlatforms returns the list of platforms that Temurin supports.
func (a *TemurinAdapter) GetSupportedPlatforms() []platform.Platform {
	return []platform.Platform{
		// Windows x64
		{OS: "windows", Arch: "x64", FileExt: "msi", DownloadName: "windows", Classifier: "windows-x64"},
		{OS: "windows", Arch: "aarch64", FileExt: "msi", DownloadName: "windows", Classifier: "windows-aarch64"},
		// macOS - use .pkg files
		{OS: "mac", Arch: "x64", FileExt: "pkg", DownloadName: "mac", Classifier: "mac-x64"},
		{OS: "mac", Arch: "aarch64", FileExt: "pkg", DownloadName: "mac", Classifier: "mac-aarch64"},
		// Linux x64
		{OS: "linux", Arch: "x64", FileExt: "tar.gz", DownloadName: "linux", Classifier: "linux-x64"},
	}
}

// GetMaintainedVersions returns all non-EOL versions from endoflife API.
// Includes both actively maintained and security-only (EOAS) versions.
func (a *TemurinAdapter) GetMaintainedVersions(ctx context.Context) ([]endoflife.VersionInfo, error) {
	if a.endoflifeClient == nil {
		return nil, fmt.Errorf("endoflife client is not initialized")
	}
	return a.endoflifeClient.GetMaintainedReleases(ctx, a.GetEndOfLifeProduct())
}

// ListVersions retrieves available Temurin versions using the endoflife.date API.
func (a *TemurinAdapter) ListVersions(ctx context.Context) ([]endoflife.VersionInfo, error) {
	a.stdout.Debug("fetching Temurin versions from endoflife.date API")

	// Get product info from endoflife.date API
	productInfo, err := a.endoflifeClient.GetProductInfo(ctx, a.GetEndOfLifeProduct())
	if err != nil {
		return nil, fmt.Errorf("failed to get Temurin info from endoflife.date: %w", err)
	}

	var versions []endoflife.VersionInfo

	// Convert endoflife.date releases to VersionInfo
	for _, release := range productInfo.Result.Releases {
		versionInfo := endoflife.VersionInfo{
			Version:       release.Name,        // "21", "17", "11", etc.
			LatestPatch:   release.Latest.Name, // "21.0.7+6", "17.0.15+6", etc.
			IsLTS:         release.IsLTS,
			IsSupported:   !release.IsEOL && release.IsMaintained,
			IsRecommended: release.IsLTS, // LTS versions are recommended
			IsEOL:         release.IsEOL,
			IsEOAS:        release.IsEOAS, // End of Active Support for security-only detection
			IsMaintained:  release.IsMaintained,
			RuntimeName:   "temurin",
		}

		// Set dates if available
		if release.EOLFrom != nil && *release.EOLFrom != "" {
			versionInfo.EOLDate = *release.EOLFrom
		}
		if release.ReleaseDate != "" {
			versionInfo.ReleaseDate = release.ReleaseDate
		}

		versions = append(versions, versionInfo)
	}

	// Sort by major version (descending) - latest first
	sort.Slice(versions, func(i, j int) bool {
		iMajor := extractMajorVersion(versions[i].Version)
		jMajor := extractMajorVersion(versions[j].Version)
		return iMajor > jMajor
	})

	a.stdout.Debug("retrieved Temurin versions from endoflife.date API", "count", len(versions))
	return versions, nil
}

// extractMajorVersion extracts the major version number from a version string
func extractMajorVersion(version string) int {
	// Handle different version formats: "21.0.7+6-LTS", "8u452-b09", etc.
	parts := strings.Split(version, ".")
	if len(parts) > 0 {
		majorStr := parts[0]
		// Remove any non-numeric suffixes
		for i, r := range majorStr {
			if r < '0' || r > '9' {
				majorStr = majorStr[:i]
				break
			}
		}
		if major, err := strconv.Atoi(majorStr); err == nil {
			return major
		}
	}
	return 0
}

// GetLatestVersion returns the latest Temurin version that matches the specified options.
func (a *TemurinAdapter) GetLatestVersion(ctx context.Context, opts runtime.VersionOptions) (endoflife.VersionInfo, error) {

	versions, err := a.ListVersions(ctx)
	if err != nil {
		return endoflife.VersionInfo{}, fmt.Errorf("failed to list versions: %w", err)
	}

	if len(versions) == 0 {
		return endoflife.VersionInfo{}, fmt.Errorf("no versions available")
	}

	// Handle exact match first
	if opts.ExactMatch && opts.VersionPattern != "" {
		for _, version := range versions {
			if version.Version == opts.VersionPattern || version.LatestPatch == opts.VersionPattern {
				return version, nil
			}
		}

		// Collect available versions for error message
		availableVersions := make([]string, 0, len(versions))
		for _, v := range versions {
			if v.LatestPatch != "" {
				availableVersions = append(availableVersions, v.LatestPatch)
			} else {
				availableVersions = append(availableVersions, v.Version)
			}
		}

		return endoflife.VersionInfo{}, fmt.Errorf("exact version %s not found for runtime temurin. Available versions: %s",
			opts.VersionPattern, strings.Join(availableVersions, ", "))
	}

	// Filter versions based on options
	filteredVersions := make([]endoflife.VersionInfo, 0, len(versions))

	for _, version := range versions {
		// Filter by pattern if specified
		if opts.VersionPattern != "" {
			matched := false

			// Exact match
			if version.Version == opts.VersionPattern {
				matched = true
			} else {
				// Try partial matching for major version (e.g., "21" matches "21.0.7+6")
				if strings.HasPrefix(version.Version, opts.VersionPattern+".") {
					matched = true
				} else {
					// Try regex matching as fallback
					regex, err := regexp.Compile(opts.VersionPattern)
					if err == nil && regex.MatchString(version.Version) {
						matched = true
					}
				}
			}

			if !matched {
				continue
			}
		}

		// Filter by LTS if requested
		if opts.LTSOnly && !version.IsLTS {
			continue
		}

		// Filter by recommended if requested
		if opts.RecommendedOnly && !version.IsRecommended {
			continue
		}

		filteredVersions = append(filteredVersions, version)
	}

	if len(filteredVersions) == 0 {
		return endoflife.VersionInfo{}, fmt.Errorf("no versions match the specified criteria")
	}

	// Return the first (latest) version
	latestVersion := filteredVersions[0]
	a.stdout.Debug("resolved latest Temurin version",
		"version", latestVersion.Version,
		"is_lts", latestVersion.IsLTS,
		"is_supported", latestVersion.IsSupported)

	return latestVersion, nil
}

// CreateDownloadTasks creates download tasks for the specified version and platforms.
func (a *TemurinAdapter) CreateDownloadTasks(version endoflife.VersionInfo, platforms []platform.Platform, outputDir string) ([]runtime.DownloadTask, error) {
	a.stdout.Debug("creating Temurin download tasks",
		"version", version.Version,
		"latest_patch", version.LatestPatch,
		"platforms", len(platforms),
		"output_dir", outputDir)

	// Use the latest patch version for Adoptium API (e.g., "21.0.7+6" instead of "21")
	downloadVersion := version.LatestPatch
	if downloadVersion == "" {
		downloadVersion = version.Version
	}

	// Get Adoptium release information using the full patch version
	release, err := a.getAdoptiumRelease(downloadVersion)
	if err != nil {
		return nil, fmt.Errorf("failed to get Adoptium release for version %s (latest patch: %s): %w", version.Version, downloadVersion, err)
	}

	tasks := make([]runtime.DownloadTask, 0)
	userAgent := "downloadruntime/1.0 (temurin)"

	for _, plat := range platforms {
		// Find the appropriate binary for this platform
		binary := a.findBinaryForPlatform(release, plat)
		if binary == nil {
			// Get available platforms for troubleshooting context
			availablePlatforms := a.getAvailablePlatforms(release)

			a.stderr.Warn("⚠️ no binary found for platform",
				"platform", fmt.Sprintf("%s-%s", plat.OS, plat.Arch),
				"requested_version", version.Version,
				"actual_version_checked", downloadVersion,
				"available_platforms", availablePlatforms,
				"api_source", "adoptium",
				"suggestion", "check if this version has binaries for your platform, or update policy to use a different version")
			continue
		}

		// Determine which package to use (installer preferred over package)
		pkg := a.selectPackage(binary, plat)
		if pkg == nil {
			a.stderr.Warn("⚠️ no suitable package found for platform",
				"platform", fmt.Sprintf("%s-%s", plat.OS, plat.Arch),
				"requested_version", version.Version,
				"actual_version_checked", downloadVersion,
				"binary_available", true,
				"package_types_checked", []string{"installer", "package"},
				"suggestion", "binary exists but no installer/package found - this may indicate an issue with the Adoptium release")
			continue
		}

		// Create output filename
		filename := pkg.Name
		outputPath := filepath.Join(outputDir, fmt.Sprintf("%s-%s", plat.OS, plat.Arch), filename)

		// Main download task
		mainTask := runtime.DownloadTask{
			URL:        pkg.Link,
			OutputPath: outputPath,
			Platform:   plat,
			Runtime:    a.GetName(),
			Version:    version.LatestPatch,
			FileType:   "main",
			Headers:    map[string]string{"User-Agent": userAgent},
			Optional:   false,
		}
		tasks = append(tasks, mainTask)

		// Checksum download task
		if pkg.ChecksumLink != "" {
			checksumPath := outputPath + ".sha256.txt"
			checksumTask := runtime.DownloadTask{
				URL:        pkg.ChecksumLink,
				OutputPath: checksumPath,
				Platform:   plat,
				Runtime:    a.GetName(),
				Version:    version.LatestPatch,
				FileType:   "checksum",
				Headers:    map[string]string{"User-Agent": userAgent},
				Optional:   true,
			}
			tasks = append(tasks, checksumTask)
		}

		// Signature download task
		if pkg.SignatureLink != "" {
			signaturePath := outputPath + ".sig"
			signatureTask := runtime.DownloadTask{
				URL:        pkg.SignatureLink,
				OutputPath: signaturePath,
				Platform:   plat,
				Runtime:    a.GetName(),
				Version:    version.LatestPatch,
				FileType:   "signature",
				Headers:    map[string]string{"User-Agent": userAgent},
				Optional:   true,
			}
			tasks = append(tasks, signatureTask)
		}
	}

	a.stdout.Debug("created Temurin download tasks",
		"version", version.Version,
		"task_count", len(tasks))

	return tasks, nil
}

// ProcessDownloads executes the download tasks using the concurrent downloader.
func (a *TemurinAdapter) ProcessDownloads(ctx context.Context, tasks []runtime.DownloadTask, concurrency int) ([]runtime.DownloadResult, error) {
	a.stdout.Debug("processing Temurin downloads", "task_count", len(tasks), "concurrency", concurrency)

	// Parse timeout from global config, fallback to 30s if not available
	timeout := 30 * time.Second
	if a.globalConfig != nil {
		timeout = a.globalConfig.GetDownloadTimeout()
	}

	// Update downloader concurrency if needed
	if concurrency > 0 {
		a.downloader = runtime.NewConcurrentDownloader(concurrency, timeout, a.stdout, a.stderr)
	}

	results, err := a.downloader.ProcessDownloads(ctx, tasks)
	if err != nil {
		return nil, fmt.Errorf("failed to process downloads: %w", err)
	}

	a.stdout.Debug("completed Temurin downloads",
		"total_tasks", len(tasks),
		"results", len(results))

	return results, nil
}

// getAvailablePlatforms returns a sorted list of os-arch strings (e.g. "linux-x64")
// that are present in the Adoptium release for JDK image_type "jdk" and JVM impl "hotspot".
func (a *TemurinAdapter) getAvailablePlatforms(release *AdoptiumRelease) []string {
	if release == nil {
		return nil
	}

	platSet := make(map[string]struct{})
	for _, bin := range release.Binaries {
		if bin.ImageType != "jdk" || bin.JVMImpl != "hotspot" {
			continue
		}
		key := fmt.Sprintf("%s-%s", bin.OS, bin.Architecture)
		platSet[key] = struct{}{}
	}

	platforms := make([]string, 0, len(platSet))
	for p := range platSet {
		platforms = append(platforms, p)
	}

	sort.Strings(platforms)
	return platforms
}

// GetVerificationStrategy returns the verification strategy for Temurin downloads.
func (a *TemurinAdapter) GetVerificationStrategy() runtime.VerificationStrategy {
	return NewTemurinVerificationStrategy(a.stdout, a.stderr)
}

// LoadTemurinGPGKeys loads GPG keys from embedded filesystem
func (a *TemurinAdapter) LoadTemurinGPGKeys() (gpg.KeyRing, error) {
	// Load GPG keys from embedded filesystem
	keyRing, err := gpg.LoadKeyRingFromEmbedFS(embeddedTemurinKeys, "keys")
	if err != nil {
		return nil, fmt.Errorf("failed to load embedded Temurin GPG keys: %w", err)
	}

	a.stdout.Info("loaded Temurin GPG keyring",
		"runtime", Temurin,
		"keys_source", "embedded")

	return keyRing, nil
}

// VerifySignature verifies a GPG signature using the embedded Temurin keys
func (a *TemurinAdapter) VerifySignature(dataFilePath, signatureFilePath string) error {
	keyRing, err := a.LoadTemurinGPGKeys()
	if err != nil {
		return fmt.Errorf("failed to load Temurin GPG keys: %w", err)
	}

	err = gpg.VerifyDetachedSignature(keyRing, dataFilePath, signatureFilePath)
	if err != nil {
		return fmt.Errorf("temurin GPG signature verification failed: %w", err)
	}

	a.stdout.Info("Temurin GPG signature verification successful",
		"data_file", dataFilePath,
		"signature_file", signatureFilePath)

	return nil
}

// TemurinVerificationStrategy combines checksum and GPG verification for Temurin downloads
type TemurinVerificationStrategy struct {
	stdout *slog.Logger
	stderr *slog.Logger
}

// NewTemurinVerificationStrategy creates a new verification strategy for Temurin that combines
// checksum verification with GPG signature verification using embedded keys
func NewTemurinVerificationStrategy(stdout, stderr *slog.Logger) runtime.VerificationStrategy {
	return &TemurinVerificationStrategy{
		stdout: stdout,
		stderr: stderr,
	}
}

// GetType returns the type of verification strategy
func (v *TemurinVerificationStrategy) GetType() string {
	return "temurin-checksum-sha256"
}

// RequiresAdditionalFiles indicates that this verifier needs additional files (signatures)
func (v *TemurinVerificationStrategy) RequiresAdditionalFiles() bool {
	return true
}

// Verify implements the VerificationStrategy interface for Temurin
func (v *TemurinVerificationStrategy) Verify(ctx context.Context, result runtime.DownloadResult) error {
	var checksumVerified bool
	var gpgVerified bool
	var verificationStatus string

	// First, verify SHA256 checksum against the companion .sha256.txt file.
	checksumFile := result.LocalPath + ".sha256.txt"
	if err := v.verifyChecksum(result.LocalPath, checksumFile); err != nil {
		v.stderr.Error("Temurin checksum verification failed", "file", result.LocalPath, "error", err)
		checksumVerified = false
		verificationStatus = "checksum_verification_failed"

		// Create individual audit file even if checksum verification failed
		if auditErr := v.createIndividualAuditFile(result, checksumVerified, false, verificationStatus); auditErr != nil {
			v.stderr.Error("Failed to create individual audit file",
				"main_file", result.LocalPath,
				"error", auditErr)
		}

		return fmt.Errorf("checksum verification failed: %w", err)
	}

	checksumVerified = true
	v.stdout.Debug("Temurin checksum verification passed", "file", result.LocalPath)

	// For GPG verification, look for signature files
	// Temurin provides .sig files for each download
	baseFileName := result.LocalPath
	signatureFile := baseFileName + ".sig"

	// Check if signature file exists for GPG verification
	if _, err := os.Stat(signatureFile); os.IsNotExist(err) {
		v.stderr.Warn("Temurin GPG signature file not found, skipping GPG verification",
			"main_file", result.LocalPath,
			"signature_file", signatureFile)

		// No signature file available, skip GPG verification
		gpgVerified = false
		verificationStatus = "signature_file_missing"

		// Create individual audit file
		if auditErr := v.createIndividualAuditFile(result, checksumVerified, gpgVerified, verificationStatus); auditErr != nil {
			v.stderr.Error("Failed to create individual audit file",
				"main_file", result.LocalPath,
				"error", auditErr)
		}

		return nil
	}

	// Try GPG verification
	if err := v.verifyGPGSignature(result.LocalPath, signatureFile); err != nil {
		v.stderr.Warn("Temurin GPG verification failed",
			"main_file", result.LocalPath,
			"signature_file", signatureFile,
			"error", err)

		// GPG verification failed
		gpgVerified = false
		verificationStatus = "gpg_verification_failed"

		// Create individual audit file
		if auditErr := v.createIndividualAuditFile(result, checksumVerified, gpgVerified, verificationStatus); auditErr != nil {
			v.stderr.Error("Failed to create individual audit file",
				"main_file", result.LocalPath,
				"error", auditErr)
		}

		// Don't fail the entire verification if GPG fails, just warn
		return nil
	}

	v.stdout.Info("Temurin GPG verification successful",
		"main_file", result.LocalPath,
		"signature_file", signatureFile)

	// Both verifications succeeded
	gpgVerified = true
	verificationStatus = "success"

	// Create individual audit file
	if auditErr := v.createIndividualAuditFile(result, checksumVerified, gpgVerified, verificationStatus); auditErr != nil {
		v.stderr.Error("Failed to create individual audit file",
			"main_file", result.LocalPath,
			"error", auditErr)
	}

	return nil
}

func (v *TemurinVerificationStrategy) verifyChecksum(filePath, checksumPath string) error {
	checksumData, err := os.ReadFile(checksumPath)
	if err != nil {
		return fmt.Errorf("failed to read checksum file: %w", err)
	}

	parts := strings.Fields(strings.TrimSpace(string(checksumData)))
	if len(parts) < 1 {
		return fmt.Errorf("invalid checksum file format")
	}
	expectedChecksum := strings.ToLower(parts[0])

	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file for checksum verification: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return fmt.Errorf("failed to calculate checksum: %w", err)
	}
	actualChecksum := fmt.Sprintf("%x", hasher.Sum(nil))

	if actualChecksum != expectedChecksum {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedChecksum, actualChecksum)
	}

	return nil
}

// verifyGPGSignature performs GPG signature verification using embedded Temurin keys
func (v *TemurinVerificationStrategy) verifyGPGSignature(dataFile, signatureFile string) error {
	// Load GPG keys from embedded filesystem
	keyRing, err := gpg.LoadKeyRingFromEmbedFS(embeddedTemurinKeys, "keys")
	if err != nil {
		return fmt.Errorf("failed to load embedded Temurin GPG keys: %w", err)
	}

	// Verify the signature
	err = gpg.VerifyDetachedSignature(keyRing, dataFile, signatureFile)
	if err != nil {
		return fmt.Errorf("GPG signature verification failed: %w", err)
	}

	return nil
}

// createIndividualAuditFile creates an individual audit file for a downloaded Temurin file
func (v *TemurinVerificationStrategy) createIndividualAuditFile(result runtime.DownloadResult, checksumVerified, gpgVerified bool, verificationStatus string) error {
	// Extract actual checksum value from checksum file if available
	checksumValue := ""
	checksumAlgorithm := "sha256"
	checksumFile := result.LocalPath + ".sha256.txt"
	if checksumVerified {
		if checksumBytes, err := os.ReadFile(checksumFile); err == nil {
			// Temurin checksum files contain: "<hash>  <filename>"
			checksumContent := strings.TrimSpace(string(checksumBytes))
			if parts := strings.Fields(checksumContent); len(parts) >= 1 {
				checksumValue = parts[0] // First part is the hash
			}
		}
	}

	// Reconstruct related URLs (these are educated guesses based on main file URL)
	// NOTE: Actual URLs come from Adoptium API (ChecksumLink/SignatureLink) but we only
	// have access to the main file's DownloadResult here. For Temurin, these URLs typically
	// follow the pattern of main URL + extension, but this is not guaranteed.
	var checksumFileURL, signatureFileURL string
	var urlsAreReconstructed = true

	if result.URL != "" {
		checksumFileURL = result.URL + ".sha256.txt"
		signatureFileURL = result.URL + ".sig"
	}

	// Create audit record
	auditRecord := map[string]interface{}{
		"timestamp":           time.Now().UTC().Format(time.RFC3339),
		"file_path":           result.LocalPath,
		"file_url":            result.URL,
		"runtime":             "temurin",
		"checksum_verified":   checksumVerified,
		"gpg_verified":        gpgVerified,
		"verification_status": verificationStatus,
	}

	// Add checksum-specific details if checksum file exists
	if _, err := os.Stat(checksumFile); err == nil {
		auditRecord["checksum_validation_method"] = "sha256_checksum_verification"
		auditRecord["checksum_algorithm"] = checksumAlgorithm
		auditRecord["checksum_file"] = checksumFile
		auditRecord["checksum_file_url"] = checksumFileURL
		auditRecord["checksum_url_reconstructed"] = urlsAreReconstructed
		if checksumValue != "" {
			auditRecord["checksum_value"] = checksumValue
		}
	}

	// Add GPG-specific details if signature file exists
	signatureFile := result.LocalPath + ".sig"
	if _, err := os.Stat(signatureFile); err == nil {
		auditRecord["gpg_validation_method"] = "detached_signature_verification"
		auditRecord["gpg_keyring_source"] = "embedded_temurin_keys"
		auditRecord["signature_file"] = signatureFile
		auditRecord["signature_file_url"] = signatureFileURL
		auditRecord["signature_url_reconstructed"] = urlsAreReconstructed
	}

	// Get file info for additional metadata
	if fileInfo, err := os.Stat(result.LocalPath); err == nil {
		auditRecord["file_size"] = fileInfo.Size()
		auditRecord["file_modified"] = fileInfo.ModTime().UTC().Format(time.RFC3339)
	}

	// Marshal to JSON
	jsonData, err := json.MarshalIndent(auditRecord, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal audit record to JSON: %w", err)
	}

	// Create audit file path (same as main file but with .audit.json extension)
	auditFilePath := result.LocalPath + ".audit.json"

	// Write audit file
	if err := os.WriteFile(auditFilePath, jsonData, 0644); err != nil {
		return fmt.Errorf("failed to write audit file %s: %w", auditFilePath, err)
	}

	v.stdout.Info("Created individual audit file for Temurin download",
		"main_file", result.LocalPath,
		"audit_file", auditFilePath,
		"checksum_verified", checksumVerified,
		"gpg_verified", gpgVerified,
		"verification_status", verificationStatus,
		"checksum_value", checksumValue,
		"file_url", result.URL)

	return nil
}

// LoadPolicy loads Temurin policy configuration from the specified file path.
func (a *TemurinAdapter) LoadPolicy(filePath string) ([]endoflife.PolicyVersion, error) {
	a.stdout.Debug("loading Temurin policy", "file_path", filePath)

	// Read file
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read Temurin policy file: %w", err)
	}

	// Parse JSON directly as array of PolicyVersion
	var versions []endoflife.PolicyVersion
	if err := json.Unmarshal(data, &versions); err != nil {
		return nil, fmt.Errorf("failed to parse Temurin policy JSON: %w", err)
	}

	if len(versions) == 0 {
		return nil, fmt.Errorf("no temurin versions found in policy")
	}

	return versions, nil
}

// ApplyPolicy filters Temurin versions based on the provided policy configuration.
func (a *TemurinAdapter) ApplyPolicy(versions []endoflife.VersionInfo, policyVersions []endoflife.PolicyVersion) ([]endoflife.VersionInfo, error) {
	if len(policyVersions) == 0 {
		a.stdout.Debug("no policy versions specified, returning all versions")
		return versions, nil
	}

	a.stdout.Debug("applying Temurin policy",
		"total_versions", len(versions),
		"policy_versions", len(policyVersions))

	// Debug: Log all input versions
	for i, version := range versions {
		a.stdout.Debug("input version",
			"index", i,
			"version", version.Version,
			"latest_patch", version.LatestPatch,
			"is_supported", version.IsSupported,
			"is_lts", version.IsLTS)
	}

	// Debug: Log all policy versions
	for i, policyVersion := range policyVersions {
		a.stdout.Debug("policy version",
			"index", i,
			"version", policyVersion.Version,
			"supported", policyVersion.Supported,
			"under_review", policyVersion.UnderReview,
			"recommended", policyVersion.Recommended)
	}

	filteredVersions := make([]endoflife.VersionInfo, 0)

	for _, policyVersion := range policyVersions {
		// Only process versions that are supported or under review in policy
		if !policyVersion.Supported && !policyVersion.UnderReview {
			a.stdout.Debug("skipping policy version - not supported or under review",
				"version", policyVersion.Version,
				"supported", policyVersion.Supported,
				"under_review", policyVersion.UnderReview)
			continue
		}

		a.stdout.Debug("processing policy version",
			"version", policyVersion.Version,
			"supported", policyVersion.Supported,
			"under_review", policyVersion.UnderReview)

		versionMatched := false
		for _, version := range versions {
			// Match by major version (e.g., policy "21" matches API "21")
			matched := false

			if policyVersion.Version != "" {
				if version.Version == policyVersion.Version {
					matched = true
					a.stdout.Debug("exact version match",
						"policy_version", policyVersion.Version,
						"api_version", version.Version)
				} else {
					// Try major version matching - extract major from both
					policyMajor := extractMajorVersion(policyVersion.Version)
					versionMajor := extractMajorVersion(version.Version)
					a.stdout.Debug("major version comparison",
						"policy_version", policyVersion.Version,
						"policy_major", policyMajor,
						"api_version", version.Version,
						"api_major", versionMajor)
					if policyMajor > 0 && versionMajor > 0 && policyMajor == versionMajor {
						matched = true
						a.stdout.Debug("major version match",
							"policy_version", policyVersion.Version,
							"api_version", version.Version,
							"major", policyMajor)
					}
				}
			}

			if matched {
				// Update version info with policy data
				version.IsSupported = policyVersion.Supported
				version.IsRecommended = policyVersion.Recommended

				// Update latest patch version from policy if specified
				if policyVersion.LatestPatchVersion != "" {
					version.LatestPatch = policyVersion.LatestPatchVersion
				}

				a.stdout.Debug("version approved by policy",
					"version", version.Version,
					"policy_version", policyVersion.Version,
					"is_supported", version.IsSupported,
					"is_recommended", version.IsRecommended)

				filteredVersions = append(filteredVersions, version)
				versionMatched = true
				break // Avoid duplicates
			}
		}

		if !versionMatched {
			a.stdout.Debug("no API version matched policy version",
				"policy_version", policyVersion.Version)
		}
	}

	a.stdout.Debug("applied Temurin policy",
		"filtered_versions", len(filteredVersions))

	return filteredVersions, nil
}

// getAdoptiumRelease fetches release information from the Adoptium API
func (a *TemurinAdapter) getAdoptiumRelease(version string) (*AdoptiumRelease, error) {
	// Try multiple version formats since endoflife.date and Adoptium API formats can differ
	versionsToTry := []string{
		version,          // Try original format first (e.g., "21.0.7+6")
		version + "-LTS", // Try with LTS suffix for LTS versions (e.g., "21.0.7+6-LTS")
	}

	var lastErr error
	for _, versionToTry := range versionsToTry {
		// Format version for Adoptium API - add "jdk-" prefix if not present
		adoptiumVersion := versionToTry
		if !strings.HasPrefix(adoptiumVersion, "jdk-") {
			adoptiumVersion = "jdk-" + adoptiumVersion
		}

		// Encode the version for URL
		encodedVersion := url.QueryEscape(adoptiumVersion)
		apiURL := fmt.Sprintf("https://api.adoptium.net/v3/assets/version/%s", encodedVersion)

		a.stdout.Debug("trying Adoptium release fetch",
			"url", apiURL,
			"original_version", version,
			"adoptium_version", adoptiumVersion)

		req, err := http.NewRequest("GET", apiURL, nil)
		if err != nil {
			lastErr = fmt.Errorf("failed to create request: %w", err)
			continue
		}

		req.Header.Set("User-Agent", "downloadruntime/1.0 (temurin)")
		req.Header.Set("Accept", "application/json")

		resp, err := a.httpClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("failed to fetch Adoptium release: %w", err)
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusNotFound {
			// Try next version format
			lastErr = fmt.Errorf("version %s not found (tried %s)", version, adoptiumVersion)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("adoptium API returned status %d for version %s (formatted as %s)", resp.StatusCode, version, adoptiumVersion)
			continue
		}

		var releases AdoptiumAPIResponse
		if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
			lastErr = fmt.Errorf("failed to decode Adoptium API response: %w", err)
			continue
		}

		if len(releases) == 0 {
			lastErr = fmt.Errorf("no releases found for version %s", version)
			continue
		}

		// Success! Return the first (and usually only) release
		release := &releases[0]
		a.stdout.Debug("retrieved Adoptium release",
			"version", version,
			"binaries", len(release.Binaries),
			"release_name", release.ReleaseName)

		return release, nil
	}

	// If we get here, all version formats failed
	return nil, fmt.Errorf("failed to find Adoptium release for version %s after trying multiple formats: %w", version, lastErr)
}

// findBinaryForPlatform finds the appropriate binary for the given platform
func (a *TemurinAdapter) findBinaryForPlatform(release *AdoptiumRelease, plat platform.Platform) *AdoptiumBinary {
	for _, binary := range release.Binaries {
		// Only consider JDK binaries (not JRE, debugimage, etc.)
		if binary.ImageType != "jdk" {
			continue
		}

		// Only consider hotspot JVM implementation
		if binary.JVMImpl != "hotspot" {
			continue
		}

		// Map platform to Adoptium format
		adoptiumOS := mapPlatformOS(plat.OS)
		adoptiumArch := mapPlatformArch(plat.Arch)

		if binary.OS == adoptiumOS && binary.Architecture == adoptiumArch {
			return &binary
		}
	}

	return nil
}

// selectPackage selects the appropriate package (installer vs package) for the platform
func (a *TemurinAdapter) selectPackage(binary *AdoptiumBinary, plat platform.Platform) *AdoptiumPackage {
	// For Mac, specifically prefer .pkg files
	if plat.OS == "mac" {
		// Check installer first for .pkg files
		if binary.Installer != nil && strings.HasSuffix(binary.Installer.Name, ".pkg") {
			return binary.Installer
		}
		// Check package for .pkg files
		if strings.HasSuffix(binary.Package.Name, ".pkg") {
			return &binary.Package
		}
		// If no .pkg found, prefer installer over package as fallback
		if binary.Installer != nil {
			return binary.Installer
		}
	}

	// For Windows, prefer installer (typically .msi)
	if plat.OS == "windows" && binary.Installer != nil {
		return binary.Installer
	}

	// Fall back to regular package for all other cases
	return &binary.Package
}

// mapPlatformOS maps our platform OS to Adoptium API OS
func mapPlatformOS(os string) string {
	switch os {
	case "windows":
		return "windows"
	case "mac":
		return "mac"
	case "darwin":
		return "mac"
	case "linux":
		return "linux"
	default:
		return os
	}
}

// mapPlatformArch maps our platform architecture to Adoptium API architecture
func mapPlatformArch(arch string) string {
	switch arch {
	case "x64", "amd64":
		return "x64"
	case "aarch64", "arm64":
		return "aarch64"
	case "x32", "386":
		return "x32"
	case "arm":
		return "arm"
	default:
		return arch
	}
}

// getGitHubTokenFromEnv returns the GitHub token from environment variable
func getGitHubTokenFromEnv() string {
	// Try common environment variable names
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		return token
	}
	if token := os.Getenv("GH_TOKEN"); token != "" {
		return token
	}
	if token := os.Getenv("GITHUB_ACCESS_TOKEN"); token != "" {
		return token
	}
	return ""
}
