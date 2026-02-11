package tomcat

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/clean-dependency-project/cdprun/internal/config"
	"github.com/clean-dependency-project/cdprun/internal/endoflife"
	"github.com/clean-dependency-project/cdprun/internal/platform"
	"github.com/clean-dependency-project/cdprun/internal/runtime"
	"github.com/clean-dependency-project/cdprun/internal/version"
)

// TomcatFileInfo represents information about a Tomcat download file
type TomcatFileInfo struct {
	Filename    string
	DownloadURL string
	Platform    platform.Platform
	Version     string
	Exists      bool
}

// TomcatReleaseInfo represents available files for a Tomcat version
type TomcatReleaseInfo struct {
	Version string
	Files   []TomcatFileInfo
}

// TomcatAdapter implements runtime.RuntimeProvider for Apache Tomcat
type TomcatAdapter struct {
	eolClient            endoflife.Client
	config               *config.Runtime
	globalConfig         *config.GlobalConfig
	stdout               *slog.Logger
	stderr               *slog.Logger
	verificationStrategy runtime.VerificationStrategy
	httpClient           *http.Client
	versionValidator     version.Validator

	// Proactive filtering results for reporting
	lastProactiveResult *ProactiveFilterResult
}

// NewAdapter creates a new Tomcat adapter
func NewAdapter(eolClient endoflife.Client, stdout, stderr *slog.Logger) *TomcatAdapter {
	return &TomcatAdapter{
		eolClient: eolClient,
		stdout:    stdout,
		stderr:    stderr,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		versionValidator: version.New(),
	}
}

// NewAdapterWithConfig creates a new Tomcat adapter with configuration
func NewAdapterWithConfig(eolClient endoflife.Client, runtimeConfig *config.Runtime, globalConfig *config.GlobalConfig, stdout, stderr *slog.Logger) *TomcatAdapter {
	// Parse timeout from global config, fallback to 30s if not available
	timeout := 30 * time.Second
	if globalConfig != nil {
		timeout = globalConfig.GetDownloadTimeout()
	}

	adapter := &TomcatAdapter{
		eolClient:        eolClient,
		config:           runtimeConfig,
		globalConfig:     globalConfig,
		stdout:           stdout,
		stderr:           stderr,
		versionValidator: version.New(),
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}

	// Initialize verification strategy if enabled
	if runtimeConfig.Verification.Enabled {
		adapter.verificationStrategy = NewTomcatVerificationStrategy(runtimeConfig, stdout, stderr)
	}

	return adapter
}

// GetName returns the runtime name
func (t *TomcatAdapter) GetName() string {
	return "tomcat"
}

// GetEndOfLifeProduct returns the endoflife.date product name
func (t *TomcatAdapter) GetEndOfLifeProduct() string {
	return "tomcat"
}

// GetSupportedPlatforms returns the list of supported platforms
func (t *TomcatAdapter) GetSupportedPlatforms() []platform.Platform {
	if t.config == nil {
		// Default platforms based on actual Tomcat distributions
		return []platform.Platform{
			{OS: "windows", Arch: "x64"}, // 64-bit Windows zip
			{OS: "windows", Arch: "x86"}, // 32-bit Windows zip
			{OS: "linux", Arch: "x64"},   // Generic tar.gz (works on Linux/Unix/Mac)
		}
	}

	var platforms []platform.Platform
	for _, p := range t.config.SupportedPlatforms {
		for _, arch := range p.Arch {
			platforms = append(platforms, platform.Platform{
				OS:   p.OS,
				Arch: arch,
			})
		}
	}
	return platforms
}

// ListVersions retrieves available Tomcat versions from endoflife.date
func (t *TomcatAdapter) ListVersions(ctx context.Context) ([]endoflife.VersionInfo, error) {
	t.stdout.Debug("listing Tomcat versions", "product", "tomcat")

	// Get product info from endoflife.date
	productInfo, err := t.eolClient.GetProductInfo(ctx, "tomcat")
	if err != nil {
		t.stderr.Error("failed to get Tomcat product info", "error", err)
		return nil, fmt.Errorf("failed to get Tomcat product info: %w", err)
	}

	// Convert product info releases to version info
	var versions []endoflife.VersionInfo
	for _, release := range productInfo.Result.Releases {
		version := endoflife.VersionInfo{
			Version:     release.Name,
			ReleaseDate: release.ReleaseDate,
			IsSupported: !release.IsEOL,
			IsLTS:       release.IsLTS,
			LatestPatch: release.Latest.Name,
		}

		// For Tomcat, use the latest patch version as the main version
		if version.LatestPatch != "" {
			version.Version = version.LatestPatch
		}

		versions = append(versions, version)
	}

	t.stdout.Debug("found Tomcat versions", "count", len(versions))
	return versions, nil
}

// GetMaintainedVersions returns all non-EOL versions from endoflife API.
// Includes both actively maintained and security-only (EOAS) versions.
func (t *TomcatAdapter) GetMaintainedVersions(ctx context.Context) ([]endoflife.VersionInfo, error) {
	if t.eolClient == nil {
		return nil, fmt.Errorf("endoflife client is not initialized")
	}
	return t.eolClient.GetMaintainedReleases(ctx, t.GetEndOfLifeProduct())
}

// GetLatestVersion returns the latest version matching the given options
func (t *TomcatAdapter) GetLatestVersion(ctx context.Context, opts runtime.VersionOptions) (endoflife.VersionInfo, error) {
	t.stdout.Debug("getting latest Tomcat version",
		"version_pattern", opts.VersionPattern,
		"lts_only", opts.LTSOnly)

	versions, err := t.ListVersions(ctx)
	if err != nil {
		return endoflife.VersionInfo{}, err
	}

	// Apply filters
	var filtered []endoflife.VersionInfo
	for _, v := range versions {
		// Filter by LTS if requested
		if opts.LTSOnly && !v.IsLTS {
			continue
		}

		// Filter by version pattern if provided
		if opts.VersionPattern != "" {
			if !t.matchesVersionPattern(v.Version, opts.VersionPattern) {
				continue
			}
		}

		// Only include supported versions
		if v.IsSupported {
			filtered = append(filtered, v)
		}
	}

	if len(filtered) == 0 {
		return endoflife.VersionInfo{}, fmt.Errorf("no matching Tomcat versions found")
	}

	// Return the first one (versions are already sorted by endoflife.date)
	latest := filtered[0]

	// Use LatestPatch if available, otherwise use Version
	if latest.LatestPatch != "" {
		latest.Version = latest.LatestPatch
	}

	t.stdout.Debug("resolved latest Tomcat version",
		"version", latest.Version,
		"is_lts", latest.IsLTS)

	return latest, nil
}

// ProactiveFilterResult tracks what was filtered during proactive checking
type ProactiveFilterResult struct {
	SkippedPlatforms   []ProactiveSkip `json:"skipped_platforms"`
	AvailablePlatforms []string        `json:"available_platforms"`
	ProcessedPlatforms int             `json:"processed_platforms"`
}

// ProactiveSkip represents a platform that was skipped during proactive filtering
type ProactiveSkip struct {
	Platform   platform.Platform `json:"platform"`
	Reason     string            `json:"reason"`
	Version    string            `json:"version"`
	Suggestion string            `json:"suggestion"`
}

// CreateDownloadTasks creates download tasks for the specified version and platforms using proactive filtering
func (t *TomcatAdapter) CreateDownloadTasks(version endoflife.VersionInfo, platforms []platform.Platform, outputDir string) ([]runtime.DownloadTask, error) {
	// Use the actual version string (should already be the patch version)
	versionStr := version.Version

	t.stdout.Debug("creating Tomcat download tasks",
		"version", versionStr,
		"platforms", len(platforms))

	// ✅ PROACTIVE: Query available files from upstream first
	release, err := t.getAvailableFiles(versionStr)
	if err != nil {
		return nil, fmt.Errorf("failed to get Tomcat release info for version %s: %w", versionStr, err)
	}

	var tasks []runtime.DownloadTask
	var proactiveSkips []ProactiveSkip
	availablePlatforms := t.getAvailablePlatforms(release)

	for _, plat := range platforms {
		// ✅ PROACTIVE: Check if file exists BEFORE creating download task
		file := t.findFileForPlatform(release, plat)
		if file == nil {
			// Track the skip for reporting
			skip := ProactiveSkip{
				Platform:   plat,
				Reason:     "binary_not_available_upstream",
				Version:    version.Version,
				Suggestion: "this version may not have binaries for your platform, or update policy to use a different version",
			}
			proactiveSkips = append(proactiveSkips, skip)

			t.stderr.Warn("⚠️ no Tomcat binary found for platform",
				"platform", fmt.Sprintf("%s-%s", plat.OS, plat.Arch),
				"requested_version", version.Version,
				"available_platforms", availablePlatforms,
				"source", "apache_tomcat_archive",
				"suggestion", skip.Suggestion)
			continue // Skip this platform gracefully
		}

		// Create output path with platform subdirectory
		platformDir := fmt.Sprintf("%s-%s", plat.OS, plat.Arch)
		outputPath := filepath.Join(outputDir, platformDir, file.Filename)

		// Main binary download task
		mainTask := runtime.DownloadTask{
			URL:        file.DownloadURL,
			OutputPath: outputPath,
			Platform:   plat,
			Runtime:    "tomcat",
			Version:    versionStr,
			FileType:   "main",
		}
		tasks = append(tasks, mainTask)

		// SHA512 checksum file
		checksumTask := runtime.DownloadTask{
			URL:        file.DownloadURL + ".sha512",
			OutputPath: outputPath + ".sha512",
			Platform:   plat,
			Runtime:    "tomcat",
			Version:    versionStr,
			FileType:   "checksum",
		}
		tasks = append(tasks, checksumTask)

		// GPG signature file (optional)
		signatureTask := runtime.DownloadTask{
			URL:        file.DownloadURL + ".asc",
			OutputPath: outputPath + ".asc",
			Platform:   plat,
			Runtime:    "tomcat",
			Version:    versionStr,
			FileType:   "signature",
			Optional:   true,
		}
		tasks = append(tasks, signatureTask)
	}

	// Store proactive filtering results for reporting
	t.lastProactiveResult = &ProactiveFilterResult{
		SkippedPlatforms:   proactiveSkips,
		AvailablePlatforms: availablePlatforms,
		ProcessedPlatforms: len(platforms),
	}

	// Log summary of proactive filtering
	if len(proactiveSkips) > 0 {
		t.stdout.Info("proactive platform filtering completed",
			"version", versionStr,
			"total_platforms", len(platforms),
			"skipped_platforms", len(proactiveSkips),
			"available_platforms", len(availablePlatforms),
			"created_tasks", len(tasks))
	}

	t.stdout.Debug("created Tomcat download tasks", "task_count", len(tasks))
	return tasks, nil
}

// GetProactiveFilterResult returns the results of the last proactive filtering operation
func (t *TomcatAdapter) GetProactiveFilterResult() *ProactiveFilterResult {
	return t.lastProactiveResult
}

// GetProactiveSkipDetails returns details about proactively skipped platforms for reporting
func (t *TomcatAdapter) GetProactiveSkipDetails() []ProactiveSkip {
	if t.lastProactiveResult == nil {
		return nil
	}
	return t.lastProactiveResult.SkippedPlatforms
}

// ProcessDownloads processes the download tasks
func (t *TomcatAdapter) ProcessDownloads(ctx context.Context, tasks []runtime.DownloadTask, concurrency int) ([]runtime.DownloadResult, error) {
	t.stdout.Debug("processing downloads",
		"task_count", len(tasks),
		"concurrency", concurrency)

	// Use the runtime's concurrent downloader
	downloader := runtime.NewConcurrentDownloader(concurrency, t.globalConfig.GetDownloadTimeout(), t.stdout, t.stderr)
	results, err := downloader.ProcessDownloads(ctx, tasks)
	if err != nil {
		t.stderr.Error("failed to process downloads", "error", err)
		return nil, err
	}

	// Count successes and failures
	successCount := 0
	failureCount := 0
	for _, result := range results {
		if result.Error != nil {
			failureCount++
			// ✅ CLEAN: No more reactive error checking - problems are prevented upfront
			t.stderr.Error("download failed",
				"url", result.URL,
				"error", result.Error)
		} else {
			successCount++
		}
	}

	t.stdout.Info("downloads completed",
		"total", len(results),
		"successful", successCount,
		"failed", failureCount)

	return results, nil
}

// GetVerificationStrategy returns the verification strategy for Tomcat
func (t *TomcatAdapter) GetVerificationStrategy() runtime.VerificationStrategy {
	return t.verificationStrategy
}

// LoadPolicy loads the policy file for Tomcat
func (t *TomcatAdapter) LoadPolicy(filePath string) ([]endoflife.PolicyVersion, error) {
	// Read the JSON file
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read policy file: %w", err)
	}

	// Unmarshal into PolicyVersion array
	var policies []endoflife.PolicyVersion
	if err := json.Unmarshal(data, &policies); err != nil {
		return nil, fmt.Errorf("failed to parse policy file: %w", err)
	}

	return policies, nil
}

// ApplyPolicy applies version policy filtering
// Tomcat policy is matched at MAJOR level to avoid over-constraining by patch/minor.
// We normalize both policy entries and available versions to their major component.
func (t *TomcatAdapter) ApplyPolicy(versions []endoflife.VersionInfo, policy []endoflife.PolicyVersion) ([]endoflife.VersionInfo, error) {
	if len(policy) == 0 {
		return versions, nil
	}

	// Build a set of approved major versions from policy
	supportedMajors := make(map[string]bool)
	for _, p := range policy {
		if !p.Supported {
			continue
		}
		major := t.extractMajorVersion(p.Version)
		if major == "" {
			continue
		}
		supportedMajors[major] = true
	}

	// Filter available versions whose major matches approved majors
	var filtered []endoflife.VersionInfo
	for _, v := range versions {
		major := t.extractMajorVersion(v.Version)
		if major == "" {
			continue
		}
		if ok := supportedMajors[major]; ok {
			filtered = append(filtered, v)
		}
	}

	return filtered, nil
}

// Helper functions

// getFilename returns the appropriate filename for the platform
func (t *TomcatAdapter) getFilename(version string, plat platform.Platform) string {
	if plat.OS == "windows" {
		// Windows has architecture-specific ZIP files
		arch := plat.Arch
		if arch == "aarch64" {
			arch = "arm64" // Normalize if needed
		}
		// Use ZIP files for Windows distributions
		return fmt.Sprintf("apache-tomcat-%s-windows-%s.zip", version, arch)
	}
	// Linux/Unix systems use the generic tar.gz (Java is platform-independent)
	return fmt.Sprintf("apache-tomcat-%s.tar.gz", version)
}

// matchesVersionPattern checks if a version matches the given pattern using semver
func (t *TomcatAdapter) matchesVersionPattern(ver, pattern string) bool {
	// Handle exact match first
	if ver == pattern {
		return true
	}

	// Determine the pattern type based on the number of components in the pattern
	patternParts := strings.Split(pattern, ".")
	var versionPattern version.VersionPattern

	switch len(patternParts) {
	case 1:
		// Major version pattern (e.g., "10" matches "10.1.43")
		versionPattern = version.PatternMajor
	case 2:
		// Major.minor pattern (e.g., "10.1" matches "10.1.43")
		versionPattern = version.PatternMajorMinor
	default:
		// For more specific patterns, fall back to exact match
		return false
	}

	// Use semver validation to check if the version matches the pattern
	supported, err := t.versionValidator.IsSupported(pattern, versionPattern, ver)
	if err != nil {
		// If semver parsing fails, fall back to prefix matching for backward compatibility
		t.stderr.Debug("semver parsing failed, falling back to prefix matching",
			"version", ver,
			"pattern", pattern,
			"error", err)
		return strings.HasPrefix(ver, pattern+".")
	}

	return supported
}

// extractMajorVersion extracts the major version from a full version string using semver
func (t *TomcatAdapter) extractMajorVersion(ver string) string {
	extracted, err := t.versionValidator.ExtractPattern(ver, version.PatternMajor)
	if err != nil {
		// Fallback to manual parsing for backward compatibility
		t.stderr.Debug("semver extraction failed, falling back to manual parsing",
			"version", ver,
			"pattern", "major",
			"error", err)
		parts := strings.Split(ver, ".")
		if len(parts) > 0 {
			return parts[0]
		}
		return ver
	}
	return extracted
}

// extractMajorMinorVersion extracts major.minor from a full version string using semver
func (t *TomcatAdapter) extractMajorMinorVersion(ver string) string {
	extracted, err := t.versionValidator.ExtractPattern(ver, version.PatternMajorMinor)
	if err != nil {
		// Fallback to manual parsing for backward compatibility
		t.stderr.Debug("semver extraction failed, falling back to manual parsing",
			"version", ver,
			"pattern", "major_minor",
			"error", err)
		parts := strings.Split(ver, ".")
		if len(parts) >= 2 {
			return fmt.Sprintf("%s.%s", parts[0], parts[1])
		}
		return ver
	}
	return extracted
}

// getAvailableFiles queries Tomcat download servers to get actually available files for a version
func (t *TomcatAdapter) getAvailableFiles(version string) (*TomcatReleaseInfo, error) {
	t.stdout.Debug("checking available Tomcat files", "version", version)

	majorVersion := t.extractMajorVersion(version)
	releaseInfo := &TomcatReleaseInfo{
		Version: version,
		Files:   make([]TomcatFileInfo, 0),
	}

	// Get all potential platforms we might want to check
	allPlatforms := t.getAllPossiblePlatforms()

	// Check each platform to see if files actually exist
	baseURL := "https://archive.apache.org/dist/tomcat" // fallback when config not set
	if t.config != nil && t.config.Download.BaseURL != "" {
		baseURL = t.config.Download.BaseURL
	}
	for _, plat := range allPlatforms {
		filename := t.getFilename(version, plat)
		downloadURL := fmt.Sprintf("%s/tomcat-%s/v%s/bin/%s", baseURL, majorVersion, version, filename)

		// Check if file exists by making a HEAD request
		exists := t.checkFileExists(downloadURL)

		fileInfo := TomcatFileInfo{
			Filename:    filename,
			DownloadURL: downloadURL,
			Platform:    plat,
			Version:     version,
			Exists:      exists,
		}

		releaseInfo.Files = append(releaseInfo.Files, fileInfo)

		if exists {
			t.stdout.Debug("confirmed Tomcat file exists",
				"platform", fmt.Sprintf("%s-%s", plat.OS, plat.Arch),
				"filename", filename)
		}
	}

	return releaseInfo, nil
}

// getAllPossiblePlatforms returns all platforms that Tomcat actually supports
func (t *TomcatAdapter) getAllPossiblePlatforms() []platform.Platform {
	return []platform.Platform{
		{OS: "windows", Arch: "x64"}, // 64-bit Windows zip
		{OS: "windows", Arch: "x86"}, // 32-bit Windows zip
		{OS: "linux", Arch: "x64"},   // Generic tar.gz
		{OS: "mac", Arch: "x64"},     // mac uses same tar.gz as Linux
		{OS: "mac", Arch: "aarch64"}, // mac ARM uses same tar.gz as Linux
	}
}

// checkFileExists checks if a file exists at the given URL using a HEAD request
func (t *TomcatAdapter) checkFileExists(url string) bool {
	req, err := http.NewRequest("HEAD", url, nil)
	if err != nil {
		t.stderr.Debug("failed to create HEAD request", "url", url, "error", err)
		return false
	}

	req.Header.Set("User-Agent", "downloadruntime/1.0 (tomcat)")

	resp, err := t.httpClient.Do(req)
	if err != nil {
		t.stderr.Debug("HEAD request failed", "url", url, "error", err)
		return false
	}
	defer resp.Body.Close()

	exists := resp.StatusCode == http.StatusOK
	t.stdout.Debug("checked file existence",
		"url", url,
		"status_code", resp.StatusCode,
		"exists", exists)

	return exists
}

// findFileForPlatform finds an available file for the given platform
func (t *TomcatAdapter) findFileForPlatform(release *TomcatReleaseInfo, plat platform.Platform) *TomcatFileInfo {
	for _, file := range release.Files {
		if file.Platform.OS == plat.OS && file.Platform.Arch == plat.Arch && file.Exists {
			return &file
		}
	}
	return nil
}

// getAvailablePlatforms returns a sorted list of platform strings that actually exist upstream
func (t *TomcatAdapter) getAvailablePlatforms(release *TomcatReleaseInfo) []string {
	if release == nil {
		return nil
	}

	platSet := make(map[string]struct{})
	for _, file := range release.Files {
		if file.Exists {
			key := fmt.Sprintf("%s-%s", file.Platform.OS, file.Platform.Arch)
			platSet[key] = struct{}{}
		}
	}

	platforms := make([]string, 0, len(platSet))
	for p := range platSet {
		platforms = append(platforms, p)
	}

	sort.Strings(platforms)
	return platforms
}
