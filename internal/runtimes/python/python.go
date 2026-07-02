// Package python provides a Python runtime adapter for the unified runtime download system.
// It integrates with the existing endoflife package and python-specific functionality
// to provide version discovery, policy application, and download coordination.
package python

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/clean-dependency-project/cdprun/internal/config"
	"github.com/clean-dependency-project/cdprun/internal/endoflife"
	"github.com/clean-dependency-project/cdprun/internal/platform"
	"github.com/clean-dependency-project/cdprun/internal/runtime"
	"github.com/clean-dependency-project/cdprun/internal/version"
)

const (
	Python = "python"
)

// PythonRelease represents a Python release from the endoflife API
type PythonRelease struct {
	Cycle             string `json:"cycle"`
	ReleaseDate       string `json:"releaseDate"`
	EOL               string `json:"eol"`
	Latest            string `json:"latest"`
	LatestReleaseDate string `json:"latestReleaseDate"`
	Link              string `json:"link,omitempty"`
}

// GetReleases fetches all Python releases from the endoflife API
func GetReleases() ([]PythonRelease, error) {
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	resp, err := client.Get("https://endoflife.date/api/python.json")
	if err != nil {
		return nil, fmt.Errorf("failed to fetch Python release info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned non-200 status: %d", resp.StatusCode)
	}

	var releases []PythonRelease
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, fmt.Errorf("failed to decode Python releases: %w", err)
	}

	return releases, nil
}

// PythonAdapter implements the RuntimeProvider interface for Python.
// It bridges the existing python package functionality with the unified runtime system.
type PythonAdapter struct {
	endoflifeClient endoflife.Client
	policyLoader    endoflife.PolicyLoader
	downloader      *runtime.ConcurrentDownloader
	config          *config.Runtime
	globalConfig    *config.GlobalConfig
	stdout          *slog.Logger
	stderr          *slog.Logger
}

// NewAdapter creates a new Python runtime adapter with the specified endoflife client.
func NewAdapter(eolClient endoflife.Client) runtime.RuntimeProvider {
	// Use default loggers if none provided
	stdout := slog.Default()
	stderr := slog.Default()

	return &PythonAdapter{
		endoflifeClient: eolClient,
		policyLoader:    endoflife.NewJSONPolicyLoader(),
		downloader:      runtime.NewConcurrentDownloader(5, 30*time.Second, stdout, stderr),
		config:          nil,
		globalConfig:    nil,
		stdout:          stdout,
		stderr:          stderr,
	}
}

// NewAdapterWithConfig creates a new Python runtime adapter with configuration and loggers.
func NewAdapterWithConfig(eolClient endoflife.Client, cfg *config.Runtime, globalCfg *config.GlobalConfig, stdout, stderr *slog.Logger) runtime.RuntimeProvider {
	// Parse timeout from global config, fallback to 30s if not available
	timeout := 30 * time.Second
	if globalCfg != nil {
		timeout = globalCfg.GetDownloadTimeout()
	}

	return &PythonAdapter{
		endoflifeClient: eolClient,
		policyLoader:    endoflife.NewJSONPolicyLoader(),
		downloader:      runtime.NewConcurrentDownloader(5, timeout, stdout, stderr),
		config:          cfg,
		globalConfig:    globalCfg,
		stdout:          stdout,
		stderr:          stderr,
	}
}

// SetConfig sets the runtime configuration for this adapter.
func (a *PythonAdapter) SetConfig(cfg *config.Runtime) {
	a.config = cfg
}

// GetName returns the unique identifier for the Python runtime.
func (a *PythonAdapter) GetName() string {
	return Python
}

// GetEndOfLifeProduct returns the product name used in the endoflife.date API.
func (a *PythonAdapter) GetEndOfLifeProduct() string {
	return Python
}

// GetSupportedPlatforms returns the list of platforms that Python supports.
func (a *PythonAdapter) GetSupportedPlatforms() []platform.Platform {
	// If configuration is available, use configured platforms
	if a.config != nil {
		return a.config.GetConfiguredPlatforms()
	}

	// Fallback to default platforms if no config available
	return []platform.Platform{
		{OS: "windows", Arch: "x64", FileExt: "msi", DownloadName: "windows", Classifier: "windows-x64"},
		{OS: "mac", Arch: "x64", FileExt: "pkg", DownloadName: "mac", Classifier: "mac-x64"},
		{OS: "linux", Arch: "x64", FileExt: "tar.xz", DownloadName: "linux", Classifier: "linux-x64"},
	}
}

// GetMaintainedVersions returns all non-EOL versions from endoflife API.
// Includes both actively maintained and security-only (EOAS) versions.
func (a *PythonAdapter) GetMaintainedVersions(ctx context.Context) ([]endoflife.VersionInfo, error) {
	if a.endoflifeClient == nil {
		return nil, fmt.Errorf("endoflife client is not initialized")
	}
	return a.endoflifeClient.GetMaintainedReleases(ctx, a.GetEndOfLifeProduct())
}

// ListVersions retrieves all available Python versions by combining data from
// the existing python package with endoflife.date API information.
func (a *PythonAdapter) ListVersions(ctx context.Context) ([]endoflife.VersionInfo, error) {
	// Get Python releases from the endoflife API
	pythonReleases, err := GetReleases()
	if err != nil {
		return nil, fmt.Errorf("failed to get Python releases: %w", err)
	}

	// Get endoflife data for python
	productInfo, err := a.endoflifeClient.GetProductInfo(ctx, Python)
	if err != nil {
		// Log error but continue with release data only
		fmt.Printf("Warning: Could not get endoflife data for python: %v\n", err)
	}

	// Convert Python releases to endoflife.VersionInfo
	var versions []endoflife.VersionInfo
	for _, pyRelease := range pythonReleases {
		versionInfo := endoflife.VersionInfo{
			Version:        pyRelease.Cycle,
			LatestPatch:    pyRelease.Latest,
			IsSupported:    false, // Will be set by policy
			IsRecommended:  false, // Will be set by policy
			IsLTS:          false, // Python doesn't have LTS versions
			IsEOL:          isEOL(pyRelease),
			IsMaintained:   !isEOL(pyRelease),
			EOLDate:        pyRelease.EOL,
			ReleaseDate:    pyRelease.ReleaseDate,
			RuntimeName:    Python,
			VersionPattern: version.PatternMajorMinor,
		}

		// Update with endoflife.date API data if available
		if productInfo != nil {
			for _, release := range productInfo.Result.Releases {
				if release.Name == pyRelease.Cycle {
					versionInfo.IsEOL = release.IsEOL
					versionInfo.IsEOAS = release.IsEOAS // End of Active Support - crucial for security-only detection
					versionInfo.IsMaintained = release.IsMaintained
					versionInfo.IsLTS = release.IsLTS
					if release.Latest.Name != "" {
						versionInfo.LatestPatch = release.Latest.Name
					}
					if release.EOLFrom != nil && *release.EOLFrom != "" {
						versionInfo.EOLDate = *release.EOLFrom
					}
					if release.ReleaseDate != "" {
						versionInfo.ReleaseDate = release.ReleaseDate
					}
					break
				}
			}
		}

		// Add download URLs using Python-specific logic
		versionInfo.DownloadURLs = a.getDownloadURLs(pyRelease)

		versions = append(versions, versionInfo)
	}

	return versions, nil
}

// GetLatestVersion returns the latest Python version that matches the specified options.
func (a *PythonAdapter) GetLatestVersion(ctx context.Context, opts runtime.VersionOptions) (endoflife.VersionInfo, error) {
	// Handle exact match - bypass endoflife.date validation and create synthetic version
	if opts.ExactMatch && opts.VersionPattern != "" {

		// Create synthetic version info for exact matches
		// This allows downloading any version the user specifies without API validation
		syntheticVersion := endoflife.VersionInfo{
			Version:       opts.VersionPattern,
			LatestPatch:   opts.VersionPattern,
			RuntimeName:   "python",
			IsSupported:   true,  // Assume supported for exact matches
			IsRecommended: false, // Conservative default
			IsLTS:         false, // Python doesn't have LTS
			IsEOL:         false, // Conservative default for exact matches
			IsMaintained:  true,  // Conservative default
		}

		return syntheticVersion, nil
	}

	versions, err := a.ListVersions(ctx)
	if err != nil {
		return endoflife.VersionInfo{}, fmt.Errorf("failed to list versions: %w", err)
	}

	if len(versions) == 0 {
		return endoflife.VersionInfo{}, fmt.Errorf("no Python versions available")
	}

	// Apply filters
	filtered := make([]endoflife.VersionInfo, 0, len(versions))
	for _, v := range versions {
		// Filter by version pattern if specified
		if opts.VersionPattern != "" {
			// Match exact version or major.minor version
			if v.Version != opts.VersionPattern && !strings.HasPrefix(v.Version, opts.VersionPattern) {
				continue
			}
		}

		if opts.RecommendedOnly && !v.IsRecommended {
			continue
		}
		if opts.LTSOnly && !v.IsLTS {
			continue // Python doesn't have LTS, so this would filter all
		}
		filtered = append(filtered, v)
	}

	if len(filtered) == 0 {
		if opts.VersionPattern != "" {
			return endoflife.VersionInfo{}, fmt.Errorf("no Python versions match version pattern '%s'", opts.VersionPattern)
		}
		return endoflife.VersionInfo{}, fmt.Errorf("no Python versions match the specified criteria")
	}

	if opts.Latest {
		// Return the first (latest) version
		return filtered[0], nil
	}

	// Default to returning the latest version
	return filtered[0], nil
}

// CreateDownloadTasks generates download tasks for the specified Python version and platforms.
func (a *PythonAdapter) CreateDownloadTasks(version endoflife.VersionInfo, platforms []platform.Platform, outputDir string) ([]runtime.DownloadTask, error) {
	// POLICY VALIDATION: Check if the version is supported or under_review before creating download tasks
	// Skip policy validation for synthetic versions (exact matches bypass validation)
	if version.RuntimeName == "python" && version.Version == version.LatestPatch {
		// This is likely a synthetic version from exact match - skip policy validation
	} else {
		if err := a.validateVersionPolicy(version); err != nil {
			return nil, fmt.Errorf("policy validation failed: %w", err)
		}
	}

	var tasks []runtime.DownloadTask

	// If no platforms specified, use all supported platforms
	if len(platforms) == 0 {
		platforms = a.GetSupportedPlatforms()
	}

	// For security-only (EOAS) Python releases, upstream stops publishing the
	// Windows .exe and macOS .pkg installers. We deliberately do NOT fall back
	// to a previous patch's installer because that produced misleading entries
	// in the published index (e.g., python-3.12.10-macos11.pkg listed under
	// version 3.12.12). Linux is still built from source for the security
	// patch, so only Windows/macOS are skipped.
	if version.IsEOAS {
		a.stdout.Info("processing security-only Python release; skipping windows/mac binaries",
			"version", version.Version,
			"latest_patch", version.LatestPatch,
			"is_eoas", version.IsEOAS)
	}

	// Fix platform file extensions to match Python's specific requirements
	// The CLI might pass platforms with generic file extensions that don't match Python's actual files
	var fixedPlatforms []platform.Platform
	for _, plat := range platforms {
		fixedPlat := plat // Copy the platform

		// Override file extension based on Python's actual file naming
		switch plat.OS {
		case "windows":
			if fixedPlat.FileExt != "exe" && fixedPlat.FileExt != "msi" {
				fixedPlat.FileExt = "exe" // Default to exe for Windows Python
			}
		case "mac":
			if fixedPlat.FileExt != "pkg" {
				fixedPlat.FileExt = "pkg" // Python macOS uses pkg files
			}
		case "linux":
			if fixedPlat.FileExt != "tar.xz" {
				fixedPlat.FileExt = "tar.xz" // Python Linux uses tar.xz files
			}
		}

		fixedPlatforms = append(fixedPlatforms, fixedPlat)
	}

	// Get user agent from config or use default
	userAgent := "downloadruntime/1.0 (Python)"
	if a.config != nil && a.config.Download.UserAgent != "" {
		userAgent = a.config.Download.UserAgent
	}

	// Track which versions are used for verification tasks
	var platformVersions []platformVersion

	// Create tasks for main binary/source files
	for _, plat := range fixedPlatforms {
		// Skip Windows/macOS for security-only releases: upstream does not
		// publish installers for these patch versions and we will not
		// substitute an older patch's installer.
		if version.IsEOAS && (plat.OS == "windows" || plat.OS == "mac") {
			a.stdout.Info("skipping platform for security-only Python release",
				"platform_os", plat.OS,
				"version", version.LatestPatch)
			continue
		}

		downloadVersion := version.LatestPatch

		url := a.constructDownloadURL(downloadVersion, plat)
		if url == "" {
			continue // Skip unsupported platform combinations
		}

		// Determine filename from URL
		parts := strings.Split(url, "/")
		fileName := parts[len(parts)-1]
		outputPath := filepath.Join(outputDir, fileName)

		task := runtime.DownloadTask{
			URL:        url,
			OutputPath: outputPath,
			Platform:   plat,
			Runtime:    Python,
			Version:    downloadVersion,
			FileType:   "main",
			Headers:    map[string]string{"User-Agent": userAgent},
		}

		tasks = append(tasks, task)
		platformVersions = append(platformVersions, platformVersion{plat: plat, version: downloadVersion})
	}

	// Add verification files based on configuration
	// Pass the actual platforms being downloaded with their respective versions
	if a.shouldDownloadVerificationFiles() {
		verificationTasks := a.createVerificationTasksWithVersions(platformVersions, outputDir, userAgent)
		tasks = append(tasks, verificationTasks...)
	}

	return tasks, nil
}

// shouldDownloadVerificationFiles determines if verification files should be downloaded based on config
func (a *PythonAdapter) shouldDownloadVerificationFiles() bool {
	if a.config == nil {
		return true // Default to enabled if no config specified
	}
	// Sigstore takes precedence over GPG; either method needs sidecar files.
	return a.config.SigstoreEnabled() || a.config.GPGEnabled()
}

// platformVersion pairs a platform with its specific version for verification tasks
type platformVersion struct {
	plat    platform.Platform
	version string
}

// createVerificationTasksWithVersions creates download tasks for verification
// files using the specific version recorded for each platform's main artifact.
// Sigstore takes precedence over GPG: when enabled, .sigstore sidecars are
// downloaded and .asc tasks are suppressed entirely.
func (a *PythonAdapter) createVerificationTasksWithVersions(platformVersions []platformVersion, outputDir, userAgent string) []runtime.DownloadTask {
	baseURL := "https://www.python.org/ftp/python"
	if a.config != nil && a.config.Download.BaseURL != "" {
		baseURL = a.config.Download.BaseURL
	}

	switch {
	case a.config != nil && a.config.SigstoreEnabled():
		return a.buildSidecarTasks(platformVersions, baseURL, outputDir, userAgent,
			a.config.Verification.Methods.Sigstore.BundleSuffixOrDefault(), "sigstore")
	case a.config == nil || a.config.GPGEnabled():
		return a.buildSidecarTasks(platformVersions, baseURL, outputDir, userAgent, ".asc", "signature")
	}
	return nil
}

// buildSidecarTasks emits optional sidecar download tasks (one per main
// artifact) deriving each sidecar URL from the main artifact's URL plus suffix.
// Used for both Sigstore (.sigstore) and GPG (.asc) sidecars.
func (a *PythonAdapter) buildSidecarTasks(platformVersions []platformVersion, baseURL, outputDir, userAgent, suffix, fileType string) []runtime.DownloadTask {
	tasks := make([]runtime.DownloadTask, 0, len(platformVersions))
	for _, pv := range platformVersions {
		mainURL := a.constructDownloadURL(pv.version, pv.plat)
		if mainURL == "" {
			continue
		}
		parts := strings.Split(mainURL, "/")
		fileName := parts[len(parts)-1]
		sidecarName := fileName + suffix

		tasks = append(tasks, runtime.DownloadTask{
			URL:        fmt.Sprintf("%s/%s/%s", baseURL, pv.version, sidecarName),
			OutputPath: filepath.Join(outputDir, sidecarName),
			Platform:   pv.plat,
			Runtime:    Python,
			Version:    pv.version,
			FileType:   fileType,
			Headers:    map[string]string{"User-Agent": userAgent},
			Optional:   true, // missing sidecars skip verification rather than failing
		})
	}
	return tasks
}

// ProcessDownloads executes the download tasks using the concurrent downloader.
func (a *PythonAdapter) ProcessDownloads(ctx context.Context, tasks []runtime.DownloadTask, concurrency int) ([]runtime.DownloadResult, error) {
	a.stdout.Debug("processing python downloads",
		"task_count", len(tasks),
		"concurrency", concurrency)

	if concurrency <= 0 {
		concurrency = 5 // Default concurrency
	}

	// Parse timeout from global config, fallback to 30s if not available
	timeout := 30 * time.Second
	if a.globalConfig != nil {
		timeout = a.globalConfig.GetDownloadTimeout()
	}

	// Use the stored downloader if available, otherwise create a new one with current loggers
	downloader := a.downloader
	if downloader == nil {
		a.stdout.Debug("creating new downloader for python", "concurrency", concurrency)
		downloader = runtime.NewConcurrentDownloader(concurrency, timeout, a.stdout, a.stderr)
	}

	results, err := downloader.ProcessDownloads(ctx, tasks)
	if err != nil {
		a.stderr.Error("python downloads failed",
			"task_count", len(tasks),
			"concurrency", concurrency,
			"error", err)
		return nil, fmt.Errorf("failed to process Python downloads: %w", err)
	}

	// Count successes and failures
	successCount := 0
	failureCount := 0
	for _, result := range results {
		if result.Error != nil {
			failureCount++
		} else {
			successCount++
		}
	}

	a.stdout.Debug("python downloads completed",
		"total_tasks", len(tasks),
		"successful", successCount,
		"failed", failureCount)

	return results, nil
}

// GetVerificationStrategy returns the verification strategy for Python downloads.
// Sigstore takes precedence over GPG: when sigstore is enabled, the Sigstore
// verifier is used and GPG is ignored entirely.
func (a *PythonAdapter) GetVerificationStrategy() runtime.VerificationStrategy {
	if a.config != nil && a.config.SigstoreEnabled() {
		return NewPythonSigstoreVerifier(a.config, a.stdout)
	}
	return NewPythonGPGVerifier(a.stdout)
}

// LoadPolicy loads Python policy configuration from the specified file path.
func (a *PythonAdapter) LoadPolicy(filePath string) ([]endoflife.PolicyVersion, error) {
	// Use the existing policy loader to load array-based policy
	policy, err := a.policyLoader.LoadArrayPolicy(filePath, Python, "major_minor")
	if err != nil {
		return nil, fmt.Errorf("failed to load Python policy: %w", err)
	}

	if len(policy.Runtimes) == 0 {
		return nil, fmt.Errorf("no python runtime configuration found in policy")
	}

	return policy.Runtimes[0].Versions, nil
}

// ApplyPolicy filters Python versions based on the provided policy configuration.
func (a *PythonAdapter) ApplyPolicy(versions []endoflife.VersionInfo, policyVersions []endoflife.PolicyVersion) ([]endoflife.VersionInfo, error) {
	// Create lookup map for policy versions
	policyMap := make(map[string]endoflife.PolicyVersion)
	for _, pv := range policyVersions {
		policyMap[pv.Version] = pv
	}

	var filtered []endoflife.VersionInfo
	for _, version := range versions {
		pv, exists := policyMap[version.Version]
		if !exists || !pv.Supported {
			continue
		}

		// Update version info with policy data
		version.IsSupported = pv.Supported
		version.IsRecommended = pv.Recommended
		if pv.EOL != "" {
			version.EOLDate = pv.EOL
		}

		filtered = append(filtered, version)
	}

	return filtered, nil
}

// constructDownloadURL builds the download URL for Python based on version and platform.
func (a *PythonAdapter) constructDownloadURL(version string, plat platform.Platform) string {
	// Use configured URL pattern if available
	if a.config != nil && a.config.Download.BaseURL != "" {
		return a.constructConfiguredURL(version, plat)
	}

	// Fallback to hardcoded logic
	return a.constructFallbackURL(version, plat)
}

// constructConfiguredURL builds URL using YAML configuration
func (a *PythonAdapter) constructConfiguredURL(version string, plat platform.Platform) string {
	baseURL := a.config.Download.BaseURL
	pattern := a.config.Download.URLPattern

	if pattern == "" {
		return a.constructFallbackURL(version, plat)
	}

	// Python has specific naming patterns that don't follow a simple template
	// We need to construct the correct filename based on platform
	var fileName string

	switch plat.OS {
	case "windows":
		// Windows uses different patterns: python-3.13.3-amd64.exe, python-3.13.3.exe, python-3.13.3-arm64.exe
		if plat.Arch == "x64" {
			fileName = fmt.Sprintf("python-%s-amd64.%s", version, plat.FileExt)
		} else {
			fileName = fmt.Sprintf("python-%s.%s", version, plat.FileExt)
		}
	case "mac":
		// macOS uses: python-3.13.3-macos11.pkg
		fileName = fmt.Sprintf("python-%s-macos11.%s", version, plat.FileExt)
	case "linux":
		// Linux uses: Python-3.13.3.tar.xz (note the capital P)
		fileName = fmt.Sprintf("Python-%s.%s", version, plat.FileExt)
	default:
		// Fallback to the pattern-based approach
		platformName := plat.DownloadName
		archName := plat.Arch

		url := strings.ReplaceAll(pattern, "{base_url}", baseURL)
		url = strings.ReplaceAll(url, "{version}", version)
		url = strings.ReplaceAll(url, "{platform}", platformName)
		url = strings.ReplaceAll(url, "{arch}", archName)
		url = strings.ReplaceAll(url, "{ext}", plat.FileExt)
		return url
	}

	return fmt.Sprintf("%s/%s/%s", baseURL, version, fileName)
}

// constructFallbackURL builds URL using hardcoded logic (original implementation)
func (a *PythonAdapter) constructFallbackURL(version string, plat platform.Platform) string {
	// Python download URL pattern
	baseURL := "https://www.python.org/ftp/python"

	// Ensure version has all three components (major.minor.patch)
	versionParts := strings.Split(version, ".")
	if len(versionParts) == 2 {
		version = version + ".0"
	}

	var fileName string

	switch plat.OS {
	case "windows":
		// Windows uses .msi installers with amd64 suffix for x64
		if plat.Arch == "x64" {
			fileName = fmt.Sprintf("python-%s-amd64.%s", version, plat.FileExt)
		} else {
			fileName = fmt.Sprintf("python-%s.%s", version, plat.FileExt)
		}

	case "mac":
		// macOS uses .pkg installers with macos11 suffix
		fileName = fmt.Sprintf("python-%s-macos11.%s", version, plat.FileExt)

	case "linux":
		// Linux uses source distribution
		fileName = fmt.Sprintf("Python-%s.%s", version, plat.FileExt)

	default:
		return "" // Unsupported platform
	}

	return fmt.Sprintf("%s/%s/%s", baseURL, version, fileName)
}

// getDownloadURLs constructs download URLs for a Python release.
func (a *PythonAdapter) getDownloadURLs(release PythonRelease) []string {
	var urls []string

	// Get supported platforms
	platforms := a.GetSupportedPlatforms()

	for _, plat := range platforms {
		url := a.constructDownloadURL(release.Latest, plat)
		if url != "" {
			urls = append(urls, url)
		}
	}

	return urls
}

// isEOL returns true if the Python release is end-of-life.
func isEOL(release PythonRelease) bool {
	if release.EOL == "" {
		return false
	}

	// Parse EOL date and compare with current time
	eolTime, err := time.Parse("2006-01-02", release.EOL)
	if err != nil {
		// If we can't parse the date, assume it's not EOL
		return false
	}

	return time.Now().After(eolTime)
}

// validateVersionPolicy checks if the version is supported or under_review according to the policy
func (a *PythonAdapter) validateVersionPolicy(version endoflife.VersionInfo) error {
	// Refuse download if policy file is not configured
	if a.config == nil || a.config.PolicyFile == "" {
		return fmt.Errorf("no policy file configured for Python runtime - downloads require explicit policy approval")
	}

	// Load policy file using the existing LoadPolicy method
	policyVersions, err := a.LoadPolicy(a.config.PolicyFile)
	if err != nil {
		return fmt.Errorf("failed to load policy file %s: %w", a.config.PolicyFile, err)
	}

	// Check if the requested version is in the policy and is supported or under_review
	for _, policyVersion := range policyVersions {
		if policyVersion.Version == version.Version {
			if policyVersion.Supported || policyVersion.UnderReview {
				// Log policy validation result to stderr (structured logging) instead of stdout
				status := "supported"
				if !policyVersion.Supported {
					status = "under review"
				}
				a.stdout.Info("Python version policy validation passed",
					"version", version.Version,
					"status", status,
					"supported", policyVersion.Supported,
					"under_review", policyVersion.UnderReview)
				return nil
			} else {
				return fmt.Errorf("python version %s is not supported or under review according to policy (supported=%t, under_review=%t)",
					version.Version, policyVersion.Supported, policyVersion.UnderReview)
			}
		}
	}

	// If version not found in policy, reject the download
	return fmt.Errorf("python version %s not found in policy file %s", version.Version, a.config.PolicyFile)
}
