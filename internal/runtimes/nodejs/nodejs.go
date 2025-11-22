// Package nodejs provides a Node.js runtime adapter for the unified runtime download system.
// It integrates with the existing endoflife package and nodejs-specific functionality
// to provide version discovery, policy application, and download coordination.
package nodejs

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/clean-dependency-project/cdprun/internal/config"
	"github.com/clean-dependency-project/cdprun/internal/endoflife"
	"github.com/clean-dependency-project/cdprun/internal/gpg"
	"github.com/clean-dependency-project/cdprun/internal/platform"
	"github.com/clean-dependency-project/cdprun/internal/runtime"
	"github.com/clean-dependency-project/cdprun/internal/version"
)

const (
	NodeJS = "nodejs"
)

// StringOrBool represents a value that can be either a string or a boolean
type StringOrBool struct {
	StringValue string
	BoolValue   bool
	IsString    bool
}

func (v *StringOrBool) UnmarshalJSON(data []byte) error {
	// Try string first
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		v.StringValue = s
		v.IsString = true
		return nil
	}

	// Try boolean
	var b bool
	if err := json.Unmarshal(data, &b); err == nil {
		v.BoolValue = b
		v.IsString = false
		return nil
	}

	return fmt.Errorf("value must be either string or boolean")
}

func (v StringOrBool) String() string {
	if v.IsString {
		return v.StringValue
	}
	if v.BoolValue {
		return "true"
	}
	return "false"
}

type NodeRelease struct {
	Cycle        string       `json:"cycle"`
	ReleaseDate  StringOrBool `json:"releaseDate"`
	Support      StringOrBool `json:"support"`
	EOL          StringOrBool `json:"eol"`
	Latest       string       `json:"latest"`
	LatestReason string       `json:"latestReason"`
	LTS          StringOrBool `json:"lts"`
}

// GetLTSVersions returns all LTS (Long Term Support) versions of Node.js
func GetLTSVersions() ([]NodeRelease, error) {
	resp, err := http.Get("https://endoflife.date/api/nodejs.json")
	if err != nil {
		return nil, fmt.Errorf("failed to fetch versions: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	var releases []NodeRelease
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, fmt.Errorf("failed to decode versions: %w", err)
	}

	// Filter LTS versions
	ltsReleases := make([]NodeRelease, 0)
	for _, release := range releases {
		if release.LTS.IsString || release.LTS.BoolValue {
			ltsReleases = append(ltsReleases, release)
		}
	}

	return ltsReleases, nil
}

// NodeJSAdapter implements the RuntimeProvider interface for Node.js.
// It bridges the existing nodejs package functionality with the unified runtime system.
type NodeJSAdapter struct {
	endoflifeClient endoflife.Client
	policyLoader    endoflife.PolicyLoader
	downloader      *runtime.ConcurrentDownloader
	config          *config.Runtime
	globalConfig    *config.GlobalConfig
	stdout          *slog.Logger
	stderr          *slog.Logger
	gpgKeyRing      gpg.KeyRing // Cached GPG keyring for Node.js verification
}

// NewAdapter creates a new Node.js runtime adapter with the specified endoflife client and configuration.
func NewAdapter(eolClient endoflife.Client) runtime.RuntimeProvider {
	// Use default loggers if none provided
	stdout := slog.Default()
	stderr := slog.Default()

	return &NodeJSAdapter{
		endoflifeClient: eolClient,
		policyLoader:    endoflife.NewJSONPolicyLoader(),
		downloader:      runtime.NewConcurrentDownloader(5, 30*time.Second, stdout, stderr),
		config:          nil, // Will be set when configuration is available
		globalConfig:    nil, // Will be set when global configuration is available
		stdout:          stdout,
		stderr:          stderr,
	}
}

// NewAdapterWithConfig creates a new Node.js runtime adapter with configuration and loggers.
func NewAdapterWithConfig(eolClient endoflife.Client, cfg *config.Runtime, globalCfg *config.GlobalConfig, stdout, stderr *slog.Logger) runtime.RuntimeProvider {
	// Parse timeout from global config, fallback to 30s if not available
	timeout := 30 * time.Second
	if globalCfg != nil {
		timeout = globalCfg.GetDownloadTimeout()
	}

	return &NodeJSAdapter{
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
func (a *NodeJSAdapter) SetConfig(cfg *config.Runtime) {
	a.config = cfg
}

// GetName returns the unique identifier for the Node.js runtime.
func (a *NodeJSAdapter) GetName() string {
	return NodeJS
}

// GetEndOfLifeProduct returns the product name used in the endoflife.date API.
func (a *NodeJSAdapter) GetEndOfLifeProduct() string {
	return NodeJS
}

// GetSupportedPlatforms returns the list of platforms that Node.js supports.
func (a *NodeJSAdapter) GetSupportedPlatforms() []platform.Platform {
	// If configuration is available, use configured platforms
	if a.config != nil {
		platforms := a.config.GetConfiguredPlatforms()
		a.stdout.Debug("using configured platforms",
			"runtime", NodeJS,
			"platform_count", len(platforms))
		return platforms
	}

	// Fallback to default platforms if no config available
	defaultPlatforms := []platform.Platform{
		{OS: "windows", Arch: "x64", FileExt: "zip", DownloadName: "win", Classifier: "windows-x64"},
		{OS: "windows", Arch: "aarch64", FileExt: "zip", DownloadName: "win", Classifier: "windows-aarch64"},
		{OS: "mac", Arch: "x64", FileExt: "pkg", DownloadName: "darwin", Classifier: "mac-x64"},
		{OS: "mac", Arch: "aarch64", FileExt: "pkg", DownloadName: "darwin", Classifier: "mac-aarch64"},
		{OS: "linux", Arch: "x64", FileExt: "tar.xz", DownloadName: "linux", Classifier: "linux-x64"},
		{OS: "linux", Arch: "aarch64", FileExt: "tar.xz", DownloadName: "linux", Classifier: "linux-aarch64"},
	}
	a.stdout.Debug("using default platforms",
		"runtime", NodeJS,
		"platform_count", len(defaultPlatforms))
	return defaultPlatforms
}

// ListVersions retrieves all available Node.js versions by combining data from
// the existing nodejs package with endoflife.date API information.
func (a *NodeJSAdapter) ListVersions(ctx context.Context) ([]endoflife.VersionInfo, error) {
	a.stdout.Debug("listing nodejs versions")

	// Get LTS versions from the existing nodejs package
	ltsVersions, err := GetLTSVersions()
	if err != nil {
		a.stderr.Error("failed to get nodejs LTS versions", "error", err)
		return nil, fmt.Errorf("failed to get Node.js LTS versions: %w", err)
	}

	a.stdout.Debug("retrieved LTS versions",
		"runtime", NodeJS,
		"lts_version_count", len(ltsVersions))

	// Get endoflife data for nodejs
	productInfo, err := a.endoflifeClient.GetProductInfo(ctx, NodeJS)
	if err != nil {
		// Log error but continue with LTS data only
		a.stderr.Warn("could not get endoflife data", "runtime", NodeJS, "error", err)
	} else {
		a.stdout.Debug("retrieved endoflife data",
			"runtime", NodeJS,
			"release_count", len(productInfo.Result.Releases))
	}

	// Convert Node.js releases to endoflife.VersionInfo
	var versions []endoflife.VersionInfo
	for _, nodeVersion := range ltsVersions {
		// Extract major version from cycle (e.g., "20" from "20.x")
		majorVersion := strings.TrimSuffix(nodeVersion.Cycle, ".x")

		versionInfo := endoflife.VersionInfo{
			Version:        majorVersion,
			LatestPatch:    nodeVersion.Latest,
			IsSupported:    false, // Will be set by policy
			IsRecommended:  false, // Will be set by policy
			IsLTS:          isLTS(nodeVersion),
			IsEOL:          isNodeEOL(nodeVersion),
			IsMaintained:   !isNodeEOL(nodeVersion),
			EOLDate:        getNodeEOLDate(nodeVersion),
			ReleaseDate:    getNodeReleaseDate(nodeVersion),
			RuntimeName:    NodeJS,
			VersionPattern: version.PatternMajor,
		}

		// Update with endoflife.date API data if available
		if productInfo != nil {
			for _, release := range productInfo.Result.Releases {
				if release.Name == majorVersion {
					versionInfo.IsEOL = release.IsEOL
					versionInfo.IsMaintained = release.IsMaintained
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

		// Add download URLs using Node.js-specific logic
		versionInfo.DownloadURLs = a.getDownloadURLs(nodeVersion)

		versions = append(versions, versionInfo)
	}

	a.stdout.Debug("version list completed",
		"runtime", NodeJS,
		"total_versions", len(versions))

	return versions, nil
}

// GetLatestVersion returns the latest Node.js version that matches the specified options.
func (a *NodeJSAdapter) GetLatestVersion(ctx context.Context, opts runtime.VersionOptions) (endoflife.VersionInfo, error) {
	versions, err := a.ListVersions(ctx)
	if err != nil {
		return endoflife.VersionInfo{}, fmt.Errorf("failed to list versions: %w", err)
	}

	if len(versions) == 0 {
		return endoflife.VersionInfo{}, fmt.Errorf("no Node.js versions available")
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

		return endoflife.VersionInfo{}, fmt.Errorf("exact version %s not found for runtime nodejs. Available versions: %s",
			opts.VersionPattern, strings.Join(availableVersions, ", "))
	}

	// Apply filters
	filtered := make([]endoflife.VersionInfo, 0, len(versions))
	for _, v := range versions {
		// Filter by version pattern if specified
		if opts.VersionPattern != "" {
			// Match exact version or major version
			if v.Version != opts.VersionPattern && !strings.HasPrefix(v.Version, opts.VersionPattern) {
				continue
			}
		}

		if opts.RecommendedOnly && !v.IsRecommended {
			continue
		}
		if opts.LTSOnly && !v.IsLTS {
			continue
		}
		filtered = append(filtered, v)
	}

	if len(filtered) == 0 {
		if opts.VersionPattern != "" {
			return endoflife.VersionInfo{}, fmt.Errorf("no Node.js versions match version pattern '%s'", opts.VersionPattern)
		}
		return endoflife.VersionInfo{}, fmt.Errorf("no Node.js versions match the specified criteria")
	}

	if opts.Latest {
		// Return the first (latest) version
		return filtered[0], nil
	}

	// Default to returning the latest version
	return filtered[0], nil
}

// CreateDownloadTasks generates download tasks for the specified Node.js version and platforms.
func (a *NodeJSAdapter) CreateDownloadTasks(version endoflife.VersionInfo, platforms []platform.Platform, outputDir string) ([]runtime.DownloadTask, error) {
	// POLICY VALIDATION: Check if the version is supported or under_review before creating download tasks
	if err := a.validateVersionPolicy(version); err != nil {
		return nil, fmt.Errorf("policy validation failed: %w", err)
	}

	var tasks []runtime.DownloadTask

	// If no platforms specified, use all supported platforms
	if len(platforms) == 0 {
		platforms = a.GetSupportedPlatforms()
	}

	// Fix platform file extensions to match Node.js's specific requirements
	// The CLI might pass platforms with generic file extensions that don't match Node.js's actual files
	fixedPlatforms := make([]platform.Platform, len(platforms))
	for i, plat := range platforms {
		fixedPlat := plat // Copy the platform

		// Override file extension based on Node.js's actual file naming
		switch plat.OS {
		case "windows":
			if fixedPlat.FileExt != "msi" && fixedPlat.FileExt != "zip" {
				fixedPlat.FileExt = "msi" // Default to msi for Windows Node.js
			}
		case "mac":
			if fixedPlat.FileExt != "pkg" {
				fixedPlat.FileExt = "pkg" // Node.js macOS uses pkg files
			}
		case "linux":
			if fixedPlat.FileExt != "tar.xz" {
				fixedPlat.FileExt = "tar.xz" // Node.js Linux uses tar.xz files
			}
		}

		fixedPlatforms[i] = fixedPlat
	}
	platforms = fixedPlatforms

	// Get user agent from config or use default
	userAgent := "cdprun/1.0 (Node.js)"
	if a.config != nil && a.config.Download.UserAgent != "" {
		userAgent = a.config.Download.UserAgent
	}

	// Create tasks for main binary files
	for _, plat := range platforms {
		url := a.constructDownloadURL(version.LatestPatch, plat)
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
			Runtime:    NodeJS,
			Version:    version.LatestPatch,
			FileType:   "main",
			Headers:    map[string]string{"User-Agent": userAgent},
		}

		tasks = append(tasks, task)
	}

	// Add verification files based on configuration
	if a.shouldDownloadVerificationFiles() {
		verificationTasks := a.createVerificationTasks(version, outputDir, userAgent)
		tasks = append(tasks, verificationTasks...)
	}

	return tasks, nil
}

// shouldDownloadVerificationFiles determines if verification files should be downloaded based on config
func (a *NodeJSAdapter) shouldDownloadVerificationFiles() bool {
	if a.config == nil {
		return true // Default to downloading verification files
	}

	// Check if verification is enabled
	if !a.config.Verification.Enabled {
		return false
	}

	// Download if checksum or GPG verification is enabled
	return a.config.Verification.Methods.Checksum.Enabled || a.config.Verification.Methods.GPG.Enabled
}

// createVerificationTasks creates download tasks for verification files
func (a *NodeJSAdapter) createVerificationTasks(version endoflife.VersionInfo, outputDir, userAgent string) []runtime.DownloadTask {
	var tasks []runtime.DownloadTask

	// Get base URL from config or use default
	baseURL := "https://nodejs.org/dist"
	if a.config != nil && a.config.Download.BaseURL != "" {
		baseURL = a.config.Download.BaseURL
	}

	versionBaseURL := fmt.Sprintf("%s/v%s", baseURL, version.LatestPatch)

	// Add checksum file if checksum verification is enabled
	if a.config == nil || a.config.Verification.Methods.Checksum.Enabled {
		checksumPattern := "SHASUMS256.txt" // Default
		if a.config != nil && a.config.Verification.Methods.Checksum.FilePattern != "" {
			checksumPattern = a.config.Verification.Methods.Checksum.FilePattern
		}

		checksumTask := runtime.DownloadTask{
			URL:        fmt.Sprintf("%s/%s", versionBaseURL, checksumPattern),
			OutputPath: filepath.Join(outputDir, checksumPattern),
			Platform:   platform.Platform{OS: "any", Arch: "any", Classifier: "checksum"},
			Runtime:    NodeJS,
			Version:    version.LatestPatch,
			FileType:   "checksum",
			Headers:    map[string]string{"User-Agent": userAgent},
			Optional:   true, // Checksum files are optional
		}
		tasks = append(tasks, checksumTask)
	}

	// Add GPG signature files if GPG verification is enabled
	if a.config == nil || a.config.Verification.Methods.GPG.Enabled {
		// SHASUMS256.txt.sig file (Node.js uses this format reliably)
		sigTask := runtime.DownloadTask{
			URL:        fmt.Sprintf("%s/SHASUMS256.txt.sig", versionBaseURL),
			OutputPath: filepath.Join(outputDir, "SHASUMS256.txt.sig"),
			Platform:   platform.Platform{OS: "any", Arch: "any", Classifier: "signature"},
			Runtime:    NodeJS,
			Version:    version.LatestPatch,
			FileType:   "signature",
			Headers:    map[string]string{"User-Agent": userAgent},
			Optional:   true, // Signature files are optional
		}
		tasks = append(tasks, sigTask)
	}

	return tasks
}

// ProcessDownloads executes the download tasks using the concurrent downloader.
func (a *NodeJSAdapter) ProcessDownloads(ctx context.Context, tasks []runtime.DownloadTask, concurrency int) ([]runtime.DownloadResult, error) {
	a.stdout.Debug("processing nodejs downloads",
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
		a.stdout.Debug("creating new downloader for nodejs", "concurrency", concurrency)
		downloader = runtime.NewConcurrentDownloader(concurrency, timeout, a.stdout, a.stderr)
	}

	results, err := downloader.ProcessDownloads(ctx, tasks)
	if err != nil {
		a.stderr.Error("nodejs downloads failed",
			"task_count", len(tasks),
			"concurrency", concurrency,
			"error", err)
		return nil, fmt.Errorf("failed to process Node.js downloads: %w", err)
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

	a.stdout.Debug("nodejs downloads completed",
		"total_tasks", len(tasks),
		"successful", successCount,
		"failed", failureCount)

	return results, nil
}

// GetVerificationStrategy returns the verification strategy for Node.js downloads.
func (a *NodeJSAdapter) GetVerificationStrategy() runtime.VerificationStrategy {
	return NewNodeJSVerificationStrategy(a.stdout, a.stderr)
}

// LoadNodeJSGPGKeys loads the embedded Node.js GPG keys and returns a GPG keyring
func (a *NodeJSAdapter) LoadNodeJSGPGKeys() (gpg.KeyRing, error) {
	// Return cached keyring if already loaded
	if a.gpgKeyRing != nil {
		return a.gpgKeyRing, nil
	}

	// Load GPG keys from embedded filesystem
	keyRing, err := gpg.LoadKeyRingFromEmbedFS(gpg.EmbeddedKeysFS, "nodejs-keys")
	if err != nil {
		return nil, fmt.Errorf("failed to load embedded Node.js GPG keys: %w", err)
	}

	// Cache the keyring for future use
	a.gpgKeyRing = keyRing

	a.stdout.Info("loaded Node.js GPG keyring",
		"runtime", NodeJS,
		"keys_source", "embedded")

	return keyRing, nil
}

// VerifySignature verifies a GPG signature using the embedded Node.js keys
func (a *NodeJSAdapter) VerifySignature(dataFilePath, signatureFilePath string) error {
	keyRing, err := a.LoadNodeJSGPGKeys()
	if err != nil {
		return fmt.Errorf("failed to load Node.js GPG keys: %w", err)
	}

	err = gpg.VerifyDetachedSignature(keyRing, dataFilePath, signatureFilePath)
	if err != nil {
		return fmt.Errorf("node.js GPG signature verification failed: %w", err)
	}

	a.stdout.Info("Node.js GPG signature verification successful",
		"data_file", dataFilePath,
		"signature_file", signatureFilePath)

	return nil
}

// LoadPolicy loads Node.js policy configuration from the specified file path.
func (a *NodeJSAdapter) LoadPolicy(filePath string) ([]endoflife.PolicyVersion, error) {
	// Use the existing policy loader to load array-based policy
	policy, err := a.policyLoader.LoadArrayPolicy(filePath, NodeJS, "major")
	if err != nil {
		return nil, fmt.Errorf("failed to load Node.js policy: %w", err)
	}

	if len(policy.Runtimes) == 0 {
		return nil, fmt.Errorf("no nodejs runtime configuration found in policy")
	}

	return policy.Runtimes[0].Versions, nil
}

// ApplyPolicy filters Node.js versions based on the provided policy configuration.
func (a *NodeJSAdapter) ApplyPolicy(versions []endoflife.VersionInfo, policyVersions []endoflife.PolicyVersion) ([]endoflife.VersionInfo, error) {
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
		version.IsLTS = pv.LTS
		if pv.EOL != "" {
			version.EOLDate = pv.EOL
		}
		// Note: LatestPatch comes from endoflife.date API, not policy file
		// Policy file should only control which versions are supported/allowed

		filtered = append(filtered, version)
	}

	return filtered, nil
}

// constructDownloadURL builds the download URL for Node.js based on version and platform.
func (a *NodeJSAdapter) constructDownloadURL(version string, plat platform.Platform) string {
	if a.hasConfiguredDownload() {
		return a.constructConfiguredURL(version, plat)
	}
	return a.constructFallbackURL(version, plat)
}

// hasConfiguredDownload checks if download configuration is available.
func (a *NodeJSAdapter) hasConfiguredDownload() bool {
	return a.config != nil &&
		a.config.Download.BaseURL != "" &&
		a.config.Download.URLPattern != ""
}

// constructConfiguredURL builds URL using YAML configuration.
func (a *NodeJSAdapter) constructConfiguredURL(version string, plat platform.Platform) string {
	baseURL := a.config.Download.BaseURL
	pattern := a.config.Download.URLPattern

	// Adjust pattern for file type-specific naming conventions
	adjustedPattern := a.adjustPatternForFileType(pattern, plat.FileExt)

	// Build URL with substitutions
	return a.buildURL(adjustedPattern, baseURL, version, plat)
}

// adjustPatternForFileType modifies the URL pattern based on Node.js file naming conventions.
// MSI files: node-v20.0.0-x64.msi (no platform, has arch)
// PKG files: node-v20.0.0.pkg (no platform or arch)
// Others: node-v20.0.0-linux-x64.tar.gz (has both)
func (a *NodeJSAdapter) adjustPatternForFileType(pattern, fileExt string) string {
	switch fileExt {
	case "msi":
		// Remove platform, keep arch
		return strings.Replace(pattern, "-{platform}-{arch}", "-{arch}", 1)
	case "pkg":
		// Remove both platform and arch
		return strings.Replace(pattern, "-{platform}-{arch}", "", 1)
	default:
		return pattern
	}
}

// buildURL performs template substitution to create the final download URL.
func (a *NodeJSAdapter) buildURL(pattern, baseURL, version string, plat platform.Platform) string {
	platformName := a.mapPlatformName(plat.OS, plat.DownloadName)
	archName := a.mapArchName(plat.Arch)

	replacements := map[string]string{
		"{base_url}": baseURL,
		"{version}":  version,
		"{platform}": platformName,
		"{arch}":     archName,
		"{ext}":      plat.FileExt,
	}

	url := pattern
	for placeholder, value := range replacements {
		url = strings.ReplaceAll(url, placeholder, value)
	}

	return url
}

// mapPlatformName converts generic OS names to Node.js-specific platform names.
func (a *NodeJSAdapter) mapPlatformName(os, downloadName string) string {
	switch os {
	case "windows":
		return "win"
	case "mac":
		return "darwin"
	case "linux":
		return "linux"
	default:
		return downloadName
	}
}

// mapArchName converts generic architecture names to Node.js-specific names.
func (a *NodeJSAdapter) mapArchName(arch string) string {
	switch arch {
	case "aarch64":
		return "arm64"
	default:
		return arch
	}
}

// constructFallbackURL builds URL using hardcoded logic (original implementation)
func (a *NodeJSAdapter) constructFallbackURL(version string, plat platform.Platform) string {
	// Node.js download URL pattern varies by file type
	baseURL := "https://nodejs.org/dist"

	var platformName, archName string

	// Map OS to Node.js-specific platform names (regardless of DownloadName from CLI)
	switch plat.OS {
	case "windows":
		platformName = "win" // Node.js uses "win" not "windows"
		switch plat.Arch {
		case "x64":
			archName = "x64"
		case "aarch64":
			archName = "arm64" // Node.js uses "arm64" not "aarch64"
		default:
			return "" // Unsupported architecture
		}
	case "mac":
		platformName = "darwin" // Node.js uses "darwin" not "mac"
		switch plat.Arch {
		case "x64":
			archName = "x64"
		case "aarch64":
			archName = "arm64"
		default:
			return "" // Unsupported architecture
		}
	case "linux":
		platformName = "linux"
		switch plat.Arch {
		case "x64":
			archName = "x64"
		case "aarch64":
			archName = "arm64"
		default:
			return "" // Unsupported architecture
		}
	default:
		return "" // Unsupported OS
	}

	// Special case for MSI and PKG files: they don't include platform in filename
	if plat.FileExt == "msi" {
		return fmt.Sprintf("%s/v%s/node-v%s-%s.%s", baseURL, version, version, archName, plat.FileExt)
	}

	if plat.FileExt == "pkg" {
		// For PKG files, no platform or arch in filename: node-v22.4.1.pkg
		return fmt.Sprintf("%s/v%s/node-v%s.%s", baseURL, version, version, plat.FileExt)
	}

	// Standard naming pattern for other file types
	return fmt.Sprintf("%s/v%s/node-v%s-%s-%s.%s", baseURL, version, version, platformName, archName, plat.FileExt)
}

// getDownloadURLs constructs download URLs for a Node.js release.
func (a *NodeJSAdapter) getDownloadURLs(release NodeRelease) []string {
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

// Helper functions for Node.js-specific data processing

// isLTS determines if a Node.js release is LTS
func isLTS(release NodeRelease) bool {
	return release.LTS.IsString || release.LTS.BoolValue
}

// isNodeEOL determines if a Node.js release is end-of-life
func isNodeEOL(release NodeRelease) bool {
	// This is a simplified check - in practice, you'd parse the EOL field
	// and compare with current date
	if release.EOL.IsString && release.EOL.StringValue != "" {
		// Parse and compare date (simplified implementation)
		return false // For now, assume not EOL
	}
	return false
}

// getNodeEOLDate extracts the EOL date from a Node.js release
func getNodeEOLDate(release NodeRelease) string {
	if release.EOL.IsString {
		return release.EOL.StringValue
	}
	return ""
}

// getNodeReleaseDate extracts the release date from a Node.js release
func getNodeReleaseDate(release NodeRelease) string {
	if release.ReleaseDate.IsString {
		return release.ReleaseDate.StringValue
	}
	return ""
}

// validateVersionPolicy checks if the version is supported or under_review according to the policy
func (a *NodeJSAdapter) validateVersionPolicy(version endoflife.VersionInfo) error {
	// Refuse download if policy file is not configured
	if a.config == nil || a.config.PolicyFile == "" {
		return fmt.Errorf("no policy file configured for Node.js runtime - downloads require explicit policy approval")
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
				// Log policy validation result instead of printing to stdout
				status := "supported"
				if !policyVersion.Supported {
					status = "under review"
				}
				a.stdout.Info("Node.js version policy validation passed",
					"version", version.Version,
					"status", status,
					"supported", policyVersion.Supported,
					"under_review", policyVersion.UnderReview)
				return nil
			} else {
				return fmt.Errorf("node.js version %s is not supported or under review according to policy (supported=%t, under_review=%t)",
					version.Version, policyVersion.Supported, policyVersion.UnderReview)
			}
		}
	}

	// If version not found in policy, reject the download
	return fmt.Errorf("node.js version %s not found in policy file %s", version.Version, a.config.PolicyFile)
}

// NodeJSVerificationStrategy combines checksum and GPG verification for Node.js downloads
type NodeJSVerificationStrategy struct {
	stdout *slog.Logger
	stderr *slog.Logger
	Logger *slog.Logger // Audit logger injected by audit wrapper
}

// createIndividualAuditFile creates an individual audit file for a downloaded file
func (v *NodeJSVerificationStrategy) createIndividualAuditFile(result runtime.DownloadResult, checksumVerified, gpgVerified bool, verificationStatus, errorMsg string) error {
	// Create audit file path (filename.ext.audit.json)
	auditFilePath := result.LocalPath + ".audit.json"

	// Extract filename for cleaner data
	filename := filepath.Base(result.LocalPath)

	// Get checksum validation details
	baseDir := filepath.Dir(result.LocalPath)
	checksumFile := filepath.Join(baseDir, "SHASUMS256.txt")
	signatureFilePath := filepath.Join(baseDir, "SHASUMS256.txt.sig")

	// Try to extract the actual checksum value and validation details
	checksumValue := ""
	checksumValidationMethod := "unknown"
	checksumSourceURL := ""

	// Always try to extract checksum value (regardless of verification status)
	// This ensures metadata.json contains the checksum for audit purposes even if verification failed
	if _, err := os.Stat(checksumFile); err == nil {
		checksumValidationMethod = "sha256_hash_comparison"
		checksumSourceURL = fmt.Sprintf("https://nodejs.org/dist/v%s/SHASUMS256.txt", result.Version)

		if checksums, err := v.getChecksumData(checksumFile); err == nil {
			if hash, found := checksums[filename]; found {
				checksumValue = hash
			}
		}
	}

	// Update validation method if checksum verification failed
	if !checksumVerified && checksumValidationMethod == "sha256_hash_comparison" {
		checksumValidationMethod = "sha256_hash_comparison_failed"
	}

	// Create audit record structure
	auditRecord := map[string]interface{}{
		"file_name":                  filename,
		"file_path":                  result.LocalPath,
		"source_url":                 result.URL,
		"file_size":                  result.FileSize,
		"runtime":                    result.Runtime,
		"version":                    result.Version,
		"platform":                   result.Platform.Classifier,
		"checksum_verified":          checksumVerified,
		"checksum_algorithm":         "sha256",
		"checksum_value":             checksumValue,
		"checksum_source_file":       checksumFile,
		"checksum_source_url":        checksumSourceURL,
		"checksum_validation_method": checksumValidationMethod,
		"gpg_verified":               gpgVerified,
		"verification_status":        verificationStatus,
		"verification_type":          "nodejs-checksum-gpg",
		"timestamp":                  time.Now().Format(time.RFC3339),
	}

	// Add error message if verification failed
	if errorMsg != "" {
		auditRecord["error"] = errorMsg
	}

	// Add additional verification file details
	auditRecord["verification_files"] = map[string]interface{}{
		"checksum_file_exists":  fileExists(checksumFile),
		"signature_file_exists": fileExists(signatureFilePath),
		"checksum_file_path":    checksumFile,
		"signature_file_path":   signatureFilePath,
	}

	// Add GPG validation details if signature file exists (GPG verification was attempted)
	if _, err := os.Stat(signatureFilePath); err == nil {
		auditRecord["gpg_signature_source_url"] = fmt.Sprintf("https://nodejs.org/dist/v%s/SHASUMS256.txt.sig", result.Version)
		auditRecord["gpg_validation_method"] = "detached_signature_verification"
		auditRecord["gpg_keyring_source"] = "embedded_nodejs_keys"
	}

	// Marshal to JSON with pretty printing
	auditJSON, err := json.MarshalIndent(auditRecord, "", "  ")
	if err != nil {
		if v.stderr != nil {
			v.stderr.Error("failed to marshal audit record to JSON",
				"file", result.LocalPath,
				"audit_file", auditFilePath,
				"error", err)
		}
		return fmt.Errorf("failed to marshal audit record: %w", err)
	}

	// Write audit file
	if err := os.WriteFile(auditFilePath, auditJSON, 0644); err != nil {
		if v.stderr != nil {
			v.stderr.Error("failed to write individual audit file",
				"file", result.LocalPath,
				"audit_file", auditFilePath,
				"error", err)
		}
		return fmt.Errorf("failed to write audit file: %w", err)
	}

	if v.stdout != nil {
		v.stdout.Info("created individual audit file",
			"file", result.LocalPath,
			"audit_file", auditFilePath,
			"verification_status", verificationStatus)
	}

	return nil
}

// fileExists checks if a file exists
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// getChecksumData parses the checksum file and returns a map of filename -> checksum
func (v *NodeJSVerificationStrategy) getChecksumData(checksumFile string) (map[string]string, error) {
	content, err := os.ReadFile(checksumFile)
	if err != nil {
		return nil, err
	}

	checksums := make(map[string]string)
	lines := strings.Split(string(content), "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Format: "checksum  filename"
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			checksum := parts[0]
			filename := parts[1]
			checksums[filename] = checksum
		}
	}

	return checksums, nil
}

// NewNodeJSVerificationStrategy creates a new verification strategy for Node.js that combines
// checksum verification with GPG signature verification using embedded keys
func NewNodeJSVerificationStrategy(stdout, stderr *slog.Logger) runtime.VerificationStrategy {
	return &NodeJSVerificationStrategy{
		stdout: stdout,
		stderr: stderr,
	}
}

// GetType returns the type of verification strategy
func (v *NodeJSVerificationStrategy) GetType() string {
	return "nodejs-checksum-gpg"
}

// RequiresAdditionalFiles indicates that this verifier needs additional files (signatures)
func (v *NodeJSVerificationStrategy) RequiresAdditionalFiles() bool {
	return true
}

// Verify implements the VerificationStrategy interface for Node.js
func (v *NodeJSVerificationStrategy) Verify(ctx context.Context, result runtime.DownloadResult) error {
	// Log verification start event to audit trail
	if v.Logger != nil {
		v.Logger.Info("verification_started",
			"event", "verification_started",
			"file_path", result.LocalPath,
			"file_size", result.FileSize,
			"url", result.URL,
			"verification_type", "nodejs-checksum-gpg",
			"runtime", result.Runtime,
			"version", result.Version,
			"platform", result.Platform)
	}

	// First, perform checksum verification
	checksumFile := filepath.Join(filepath.Dir(result.LocalPath), "SHASUMS256.txt")
	if err := v.verifyChecksum(result.LocalPath, checksumFile); err != nil {
		if v.stderr != nil {
			v.stderr.Error("Node.js checksum verification failed", "file", result.LocalPath, "error", err)
		}
		// Log verification failure to audit trail
		if v.Logger != nil {
			v.Logger.Error("verification_failed",
				"event", "verification_failed",
				"file_path", result.LocalPath,
				"verification_step", "checksum_verification",
				"error", err.Error(),
				"runtime", result.Runtime,
				"version", result.Version,
				"platform", result.Platform)
		}

		// Create individual audit file for checksum failure
		if auditErr := v.createIndividualAuditFile(result, false, false, "failed", err.Error()); auditErr != nil {
			if v.stderr != nil {
				v.stderr.Warn("failed to create audit file for verification failure",
					"file", result.LocalPath,
					"error", auditErr)
			}
		}

		// Log comprehensive audit summary for checksum failure
		if v.Logger != nil {
			filename := filepath.Base(result.LocalPath)
			v.Logger.Error("audit_summary",
				"event", "audit_summary",
				"file_name", filename,
				"file_path", result.LocalPath,
				"source_url", result.URL,
				"file_size", result.FileSize,
				"runtime", result.Runtime,
				"version", result.Version,
				"platform", result.Platform.Classifier,
				"checksum_verified", false,
				"gpg_verified", false,
				"verification_status", "failed",
				"verification_type", "nodejs-checksum-gpg",
				"error", err.Error(),
				"timestamp", time.Now().Format(time.RFC3339))
		}
		return fmt.Errorf("checksum verification failed: %w", err)
	}

	if v.stdout != nil {
		v.stdout.Debug("Node.js checksum verification passed", "file", result.LocalPath)
	}
	// Log checksum verification success to audit trail
	if v.Logger != nil {
		v.Logger.Info("checksum_verification_success",
			"event", "checksum_verification_success",
			"file_path", result.LocalPath,
			"verification_step", "checksum_verification",
			"runtime", result.Runtime,
			"version", result.Version,
			"platform", result.Platform)
	}

	// For GPG verification, we need to check if signature files exist
	// Node.js provides SHASUMS256.txt.sig files (we skip .asc files as they have compatibility issues)
	// checksumFile is already defined above for checksum verification
	signatureFile := filepath.Join(filepath.Dir(result.LocalPath), "SHASUMS256.txt.sig")

	// Determine verification status summary for audit file
	checksumVerified := true // Always true if we get here (checksum verification passed)
	gpgVerified := false

	// Check if checksum file and signature file exist for GPG verification
	if _, err := os.Stat(checksumFile); err == nil {
		if _, err := os.Stat(signatureFile); err == nil {
			// Try GPG verification
			if err := v.verifyGPGSignature(checksumFile, signatureFile); err != nil {
				gpgVerified = false
				if v.stderr != nil {
					v.stderr.Warn("Node.js GPG verification failed",
						"checksum_file", checksumFile,
						"signature_file", signatureFile,
						"error", err)
				}
				// Log GPG verification failure to audit trail
				if v.Logger != nil {
					v.Logger.Warn("gpg_verification_failed",
						"event", "gpg_verification_failed",
						"file_path", result.LocalPath,
						"checksum_file", checksumFile,
						"signature_file", signatureFile,
						"verification_step", "gpg_verification",
						"error", err.Error(),
						"runtime", result.Runtime,
						"version", result.Version,
						"platform", result.Platform)
				}
				// Don't fail the entire verification if GPG fails, just warn
			} else {
				gpgVerified = true
				if v.stdout != nil {
					v.stdout.Info("Node.js GPG verification successful",
						"checksum_file", checksumFile,
						"signature_file", signatureFile)
				}
				// Log GPG verification success to audit trail
				if v.Logger != nil {
					v.Logger.Info("gpg_verification_success",
						"event", "gpg_verification_success",
						"file_path", result.LocalPath,
						"checksum_file", checksumFile,
						"signature_file", signatureFile,
						"verification_step", "gpg_verification",
						"runtime", result.Runtime,
						"version", result.Version,
						"platform", result.Platform)
				}
			}
		}
	}

	// Log overall verification completion to audit trail
	if v.Logger != nil {
		v.Logger.Info("verification_completed",
			"event", "verification_completed",
			"file_path", result.LocalPath,
			"verification_type", "nodejs-checksum-gpg",
			"runtime", result.Runtime,
			"version", result.Version,
			"platform", result.Platform,
			"status", "success")
	}

	// Create individual audit file for successful verification
	if auditErr := v.createIndividualAuditFile(result, checksumVerified, gpgVerified, "success", ""); auditErr != nil {
		if v.stderr != nil {
			v.stderr.Warn("failed to create audit file for successful verification",
				"file", result.LocalPath,
				"error", auditErr)
		}
	}

	// Log comprehensive audit summary for this file
	if v.Logger != nil {
		// Extract filename for cleaner logging
		filename := filepath.Base(result.LocalPath)

		v.Logger.Info("audit_summary",
			"event", "audit_summary",
			"file_name", filename,
			"file_path", result.LocalPath,
			"source_url", result.URL,
			"file_size", result.FileSize,
			"runtime", result.Runtime,
			"version", result.Version,
			"platform", result.Platform.Classifier,
			"checksum_verified", checksumVerified,
			"gpg_verified", gpgVerified,
			"verification_status", "success",
			"verification_type", "nodejs-checksum-gpg",
			"timestamp", time.Now().Format(time.RFC3339))
	}

	return nil
}

// verifyChecksum verifies the checksum of a file against the SHASUMS256.txt file
func (v *NodeJSVerificationStrategy) verifyChecksum(filePath, checksumFile string) error {
	// Read the checksum file
	checksums, err := v.getChecksumData(checksumFile)
	if err != nil {
		return fmt.Errorf("failed to read checksum file: %w", err)
	}

	// Get the filename
	filename := filepath.Base(filePath)

	// Find the expected checksum
	expectedChecksum, found := checksums[filename]
	if !found {
		return fmt.Errorf("checksum not found for file %s in checksum file", filename)
	}

	// Calculate actual checksum
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

	// Compare checksums
	if actualChecksum != expectedChecksum {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedChecksum, actualChecksum)
	}

	return nil
}

// verifyGPGSignature performs GPG signature verification using embedded Node.js keys
func (v *NodeJSVerificationStrategy) verifyGPGSignature(dataFile, signatureFile string) error {
	// Load GPG keys from embedded filesystem
	keyRing, err := gpg.LoadKeyRingFromEmbedFS(gpg.EmbeddedKeysFS, "nodejs-keys")
	if err != nil {
		return fmt.Errorf("failed to load embedded Node.js GPG keys: %w", err)
	}

	// Verify the signature
	err = gpg.VerifyDetachedSignature(keyRing, dataFile, signatureFile)
	if err != nil {
		return fmt.Errorf("GPG signature verification failed: %w", err)
	}

	return nil
}
