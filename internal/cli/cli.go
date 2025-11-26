// Package cli provides a unified command-line interface for the runtime download system.
// It supports YAML configuration files and integrates with all runtime providers.
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/urfave/cli/v2"

	"github.com/clean-dependency-project/cdprun/internal/config"
	"github.com/clean-dependency-project/cdprun/internal/endoflife"
	gh "github.com/clean-dependency-project/cdprun/internal/github"
	"github.com/clean-dependency-project/cdprun/internal/platform"
	"github.com/clean-dependency-project/cdprun/internal/runtime"
	nodejsAdapter "github.com/clean-dependency-project/cdprun/internal/runtimes/nodejs"
	"github.com/clean-dependency-project/cdprun/internal/sitegen"
	"github.com/clean-dependency-project/cdprun/internal/storage"
)

// DownloadResult represents a download operation result for JSON output
type DownloadResult struct {
	Runtime    string `json:"runtime"`
	Version    string `json:"version"`
	Platform   string `json:"platform"`
	URL        string `json:"url"`
	LocalPath  string `json:"local_path"`
	FileSize   int64  `json:"file_size"`
	Success    bool   `json:"success"`
	Error      string `json:"error,omitempty"`
	DurationMs int64  `json:"duration_ms"`
}

// DownloadSummary represents the summary of download operations for JSON output
type DownloadSummary struct {
	Runtime    string           `json:"runtime"`
	Version    string           `json:"version"`
	TotalFiles int              `json:"total_files"`
	Successful int              `json:"successful"`
	Failed     int              `json:"failed"`
	OutputDir  string           `json:"output_dir"`
	Results    []DownloadResult `json:"results"`
}

// NewApp creates and configures the main CLI application.
func NewApp() *cli.App {
	return &cli.App{
		Name:     "cdprun",
		Usage:    "Download and manage runtime binaries",
		Version:  "1.0.0",
		Compiled: time.Now(),
		Authors: []*cli.Author{
			{
				Name:  "Clean Dependency Project",
				Email: "info@example.com",
			},
		},
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "config",
				Aliases: []string{"c"},
				Value:   "runtime-registry.yaml",
				Usage:   "path to runtime registry configuration file",
				EnvVars: []string{"CDPRUN_CONFIG"},
			},
			&cli.StringFlag{
				Name:    "log-level",
				Value:   "info",
				Usage:   "log level for structured JSON output (debug, info, warn, error)",
				EnvVars: []string{"CDPRUN_LOG_LEVEL"},
			},
		},
		Commands: []*cli.Command{
			{
				Name:  "download",
				Usage: "Download runtime binaries from runtime-registry.yaml",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:    "runtime",
						Aliases: []string{"r"},
						Usage:   "runtime name (nodejs). If not specified, downloads all enabled runtimes from config",
					},
					&cli.StringFlag{
						Name:    "version",
						Aliases: []string{"v"},
						Usage:   "version to download (e.g., 20, 20.19.5). If not specified, downloads all supported versions from policy",
					},
					&cli.BoolFlag{
						Name:  "exact",
						Usage: "download exact version (no pattern matching)",
					},
					&cli.StringSliceFlag{
						Name:    "platform",
						Aliases: []string{"p"},
						Usage:   "target platforms (e.g., windows-x64, linux-x64, darwin-arm64)",
					},
					&cli.StringFlag{
						Name:  "output-dir",
						Value: "./downloads",
						Usage: "output directory for downloads",
					},
					&cli.IntFlag{
						Name:  "concurrency",
						Value: 5,
						Usage: "number of concurrent downloads",
					},
					&cli.BoolFlag{
						Name:  "verify",
						Value: true,
						Usage: "verify downloads using checksums",
					},
					&cli.StringFlag{
						Name:  "output",
						Value: "text",
						Usage: "output format (text, json)",
					},
				},
				Action: downloadRuntime,
			},
			{
				Name:  "sitegen",
				Usage: "Generate static HTML site from release database",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:     "db",
						Usage:    "path to SQLite database file",
						Required: false, // Will use config if not provided
						EnvVars:  []string{"SITEGEN_DB"},
					},
					&cli.StringFlag{
						Name:     "out",
						Usage:    "output directory for generated HTML files",
						Required: true,
						EnvVars:  []string{"SITEGEN_OUT"},
					},
					&cli.BoolFlag{
						Name:  "dry-run",
						Usage: "validate without writing files",
					},
				},
				Action: sitegenCommand,
			},
		},
	}
}

// initDB initializes the database connection based on the provided configuration.
// Returns an error if the database file cannot be created or opened.
func initDB(cfg *config.Config) (*storage.DB, error) {
	return storage.InitDB(storage.Config{
		DatabasePath: cfg.Config.Storage.DatabasePath,
		LogLevel:     "warn",
	})
}

// initializeManager creates a runtime manager with registered adapters based on config.
func initializeManager(configPath string, db *storage.DB, stdout, stderr *slog.Logger) (*runtime.Manager, *config.Config, error) {
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load config: %w", err)
	}

	eolClient := endoflife.NewClient(endoflife.DefaultConfig())
	registry := runtime.NewRegistry()

	enabledRuntimes := cfg.GetEnabledRuntimes()
	for runtimeName, runtimeConfig := range enabledRuntimes {
		switch runtimeName {
		case "nodejs":
			adapter := nodejsAdapter.NewAdapterWithConfig(eolClient, &runtimeConfig, &cfg.Config, stdout, stderr)
			if err := registry.Register("nodejs", adapter); err != nil {
				return nil, nil, fmt.Errorf("failed to register nodejs adapter: %w", err)
			}
			stdout.Info("registered runtime adapter",
				"runtime", runtimeName,
				"endoflife_product", runtimeConfig.EndOfLifeProduct,
				"policy_file", runtimeConfig.PolicyFile)
		default:
			stderr.Warn("unsupported runtime", "runtime", runtimeName)
		}
	}

	manager := runtime.NewManager(registry, db, stdout, stderr)
	manager.SetConfig(cfg) // Enable ClamAV if configured
	return manager, cfg, nil
}

// parsePlatforms parses platform strings into platform.Platform objects.
func parsePlatforms(platformStrs []string) ([]platform.Platform, error) {
	if len(platformStrs) == 0 {
		// Default to current platform
		currentPlatform := platform.CurrentPlatform()
		return []platform.Platform{currentPlatform}, nil
	}

	var platforms []platform.Platform
	for _, platformStr := range platformStrs {
		plat, err := platform.FindPlatform(platformStr)
		if err != nil {
			return nil, fmt.Errorf("invalid platform %q: %w", platformStr, err)
		}
		platforms = append(platforms, plat)
	}
	return platforms, nil
}

// downloadRuntime implements the download command.
func downloadRuntime(c *cli.Context) error {
	runtimeName := c.String("runtime")
	version := c.String("version")
	platformStrs := c.StringSlice("platform")
	outputDir := c.String("output-dir")
	concurrency := c.Int("concurrency")
	outputFormat := c.String("output")

	// Create loggers from CLI flag with output format awareness
	logLevel := ParseLogLevelOrDefault(c.String("log-level"))
	stdout, stderr := NewLoggersWithOutputFormat(logLevel, outputFormat)

	stdout.Info("starting download",
		"runtime", runtimeName,
		"version", version,
		"platforms", platformStrs,
		"output_dir", outputDir,
		"concurrency", concurrency)

	// Load configuration
	cfg, err := config.LoadConfig(c.String("config"))
	if err != nil {
		stderr.Error("failed to load config", "error", err)
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Initialize database
	db, err := initDB(cfg)
	if err != nil {
		stderr.Error("failed to initialize database", "error", err)
		return fmt.Errorf("failed to initialize database: %w", err)
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			// Log close error but don't fail - we're in cleanup
			stderr.Warn("failed to close database", "error", closeErr)
		}
	}()

	// Initialize manager (loads policy, registers adapters with endoflife client)
	manager, cfg, err := initializeManager(c.String("config"), db, stdout, stderr)
	if err != nil {
		stderr.Error("failed to initialize manager", "error", err)
		return fmt.Errorf("failed to initialize manager: %w", err)
	}

	// Determine which runtimes to download
	var runtimesToDownload []string
	if runtimeName == "" {
		// No runtime specified - download all enabled runtimes from config
		enabledRuntimes := cfg.GetEnabledRuntimes()
		for name := range enabledRuntimes {
			runtimesToDownload = append(runtimesToDownload, name)
		}
		stdout.Info("no runtime specified, downloading all enabled runtimes from config",
			"runtimes", runtimesToDownload)
	} else {
		// Specific runtime requested
		runtimeConfig, exists := cfg.GetRuntimeConfig(runtimeName)
		if !exists {
			stderr.Error("runtime not configured", "runtime", runtimeName)
			return fmt.Errorf("runtime %s not configured or disabled", runtimeName)
		}
		if !runtimeConfig.Enabled {
			stderr.Error("runtime is disabled", "runtime", runtimeName)
			return fmt.Errorf("runtime %s is disabled in configuration", runtimeName)
		}
		runtimesToDownload = []string{runtimeName}
	}

	// Download each runtime
	overallSuccess := 0
	overallFailed := 0

	for _, runtime := range runtimesToDownload {
		stdout.Info("processing runtime", "runtime", runtime)

		err := downloadSingleRuntime(c, manager, cfg, db, runtime, version, platformStrs, outputDir, concurrency, outputFormat, stdout, stderr)
		if err != nil {
			stderr.Error("runtime download failed", "runtime", runtime, "error", err)
			overallFailed++
			continue
		}
		overallSuccess++
	}

	stdout.Info("all runtimes processed",
		"total", len(runtimesToDownload),
		"successful", overallSuccess,
		"failed", overallFailed)

	return nil
}

// downloadSingleRuntime handles downloading a single runtime
func downloadSingleRuntime(c *cli.Context, manager *runtime.Manager, cfg *config.Config, db *storage.DB, runtimeName, version string, platformStrs []string, outputDir string, concurrency int, outputFormat string, stdout, stderr *slog.Logger) error {
	// Get runtime configuration
	runtimeConfig, exists := cfg.GetRuntimeConfig(runtimeName)
	if !exists {
		return fmt.Errorf("runtime %s not configured or disabled", runtimeName)
	}

	// Determine platforms to download
	var platforms []platform.Platform
	var err error
	if len(platformStrs) == 0 && cfg.Config.AutoDownloadAllPlatforms {
		// Use all configured platforms from the YAML config
		platforms = runtimeConfig.GetConfiguredPlatforms()
		stdout.Info("auto-downloading for all configured platforms",
			"platforms", getClassifiersFromPlatforms(platforms))
	} else {
		// Parse user-specified platforms
		platforms, err = parsePlatforms(platformStrs)
		if err != nil {
			stderr.Error("failed to parse platforms", "error", err)
			return err
		}
		stdout.Info("downloading for specified platforms", "platforms", platformStrs)
	}

	// Create output directory
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		stderr.Error("failed to create output directory", "output_dir", outputDir, "error", err)
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Get runtime provider
	provider, err := manager.GetProvider(runtimeName)
	if err != nil {
		stderr.Error("failed to get provider", "runtime", runtimeName, "error", err)
		return fmt.Errorf("failed to get provider: %w", err)
	}

	// Determine versions to download
	var versionsToDownload []runtime.VersionInfo

	if version == "" {
		// No version specified - download all supported versions from policy
		stdout.Info("no version specified, downloading all supported versions from policy", "runtime", runtimeName)

		// Get all versions from endoflife.date API
		allVersions, err := manager.ListVersions(context.Background(), runtimeName)
		if err != nil {
			stderr.Error("failed to list versions", "runtime", runtimeName, "error", err)
			return fmt.Errorf("failed to list versions: %w", err)
		}
		stdout.Info("retrieved versions from endoflife.date", "runtime", runtimeName, "version_count", len(allVersions))

		// Load policy file
		policyVersions, err := provider.LoadPolicy(runtimeConfig.PolicyFile)
		if err != nil {
			stderr.Error("failed to load policy", "policy_file", runtimeConfig.PolicyFile, "error", err)
			return fmt.Errorf("failed to load policy: %w", err)
		}
		stdout.Info("loaded policy file", "policy_file", runtimeConfig.PolicyFile, "policy_version_count", len(policyVersions))

		// Apply policy to filter supported versions
		supportedVersions, err := provider.ApplyPolicy(allVersions, policyVersions)
		if err != nil {
			stderr.Error("failed to apply policy", "error", err)
			return fmt.Errorf("failed to apply policy: %w", err)
		}
		stdout.Info("filtered by policy", "supported_count", len(supportedVersions))

		if len(supportedVersions) == 0 {
			stderr.Warn("no supported versions found in policy")
			return fmt.Errorf("no supported versions found in policy")
		}

		versionsToDownload = supportedVersions
	} else {
		// Specific version requested
		exactMatch := c.Bool("exact")
		versionInfo, err := manager.GetLatestVersion(context.Background(), runtimeName, runtime.VersionOptions{
			VersionPattern:  version,
			ExactMatch:      exactMatch,
			LTSOnly:         false,
			RecommendedOnly: false,
		})
		if err != nil {
			stderr.Error("failed to get version info",
				"runtime", runtimeName,
				"version", version,
				"error", err)
			return fmt.Errorf("failed to get version info: %w", err)
		}

		stdout.Info("resolved version",
			"runtime", runtimeName,
			"requested_version", version,
			"resolved_version", versionInfo.Version,
			"latest_patch", versionInfo.LatestPatch)

		versionsToDownload = []runtime.VersionInfo{versionInfo}
	}

	// Load ignore configuration if specified
	var ignoreConfig config.IgnoreConfig
	if cfg.Config.IgnoreFile != "" {
		ignoreConfig, err = config.LoadIgnoreConfig(cfg.Config.IgnoreFile)
		if err != nil {
			stderr.Warn("failed to load ignore config", "ignore_file", cfg.Config.IgnoreFile, "error", err)
			ignoreConfig = config.IgnoreConfig{} // Use empty config on error
		} else {
			stdout.Debug("loaded ignore configuration", "ignore_file", cfg.Config.IgnoreFile)
		}
	}

	// Download all versions
	var allResults []DownloadResult
	totalSuccess := 0
	totalFailed := 0

	// Collect all successful downloads for aggregated release
	var successfulVersions []versionDownload

	for _, versionInfo := range versionsToDownload {
		stdout.Info("processing version",
			"runtime", runtimeName,
			"version", versionInfo.Version,
			"latest_patch", versionInfo.LatestPatch)

		// Filter platforms based on ignore configuration
		var filteredPlatforms []platform.Platform
		for _, plat := range platforms {
			if ignoreConfig.IsPlatformIgnored(runtimeName, versionInfo.LatestPatch, plat.OS, plat.Arch) {
				stdout.Debug("skipping ignored platform",
					"runtime", runtimeName,
					"version", versionInfo.LatestPatch,
					"platform", plat.Classifier)
				continue
			}
			filteredPlatforms = append(filteredPlatforms, plat)
		}

		if len(filteredPlatforms) == 0 {
			stdout.Info("all platforms ignored for version",
				"runtime", runtimeName,
				"version", versionInfo.Version)
			continue
		}

		// Download runtime
		// This calls:
		// - Policy validation (internal/runtimes/nodejs)
		// - Database check for duplicates (internal/runtime)
		// - Download files (internal/runtime + internal/runtimes/nodejs)
		// - Verify checksums + GPG (internal/runtimes/nodejs)
		// - Record to database (internal/runtime)
		results, err := manager.DownloadRuntime(
			context.Background(),
			runtimeName,
			versionInfo,
			filteredPlatforms,
			outputDir,
			concurrency,
		)

		if err != nil {
			stderr.Error("download failed for version",
				"runtime", runtimeName,
				"version", versionInfo.Version,
				"error", err)
			// Continue with next version instead of failing entirely
			totalFailed++
			continue
		}

		// Log per-version summary
		successCount := 0
		for _, result := range results {
			if result.Error == nil {
				successCount++
				stdout.Info("download completed",
					"version", versionInfo.Version,
					"url", result.URL,
					"local_path", result.LocalPath,
					"size_bytes", result.FileSize,
					"duration_ms", result.Duration.Milliseconds())
			} else {
				stderr.Error("download failed",
					"version", versionInfo.Version,
					"url", result.URL,
					"local_path", result.LocalPath,
					"error", result.Error)
			}

			// Convert to DownloadResult for summary
			var errorMsg string
			if result.Error != nil {
				errorMsg = result.Error.Error()
			}

			platformStr := ""
			if len(platforms) > 0 {
				platformStr = platforms[0].Classifier
			}

			allResults = append(allResults, DownloadResult{
				Runtime:    runtimeName,
				Version:    versionInfo.Version,
				Platform:   platformStr,
				URL:        result.URL,
				LocalPath:  result.LocalPath,
				FileSize:   result.FileSize,
				Success:    result.Error == nil,
				Error:      errorMsg,
				DurationMs: result.Duration.Milliseconds(),
			})
		}

		if successCount > 0 {
			totalSuccess++
			// Collect for aggregated release
			successfulVersions = append(successfulVersions, versionDownload{
				version: versionInfo.LatestPatch,
				results: results,
			})
		}

		stdout.Info("version download summary",
			"runtime", runtimeName,
			"version", versionInfo.Version,
			"total_files", len(results),
			"successful", successCount,
			"failed", len(results)-successCount)
	}

	// Handle aggregated auto-release if enabled and we have successful downloads
	if runtimeConfig.Release.AutoRelease && len(successfulVersions) > 0 {
		if err := handleAggregatedAutoRelease(runtimeName, successfulVersions, outputDir, &runtimeConfig.Release, db, stdout, stderr); err != nil {
			stderr.Error("aggregated auto-release failed", "error", err)
			return fmt.Errorf("aggregated auto-release failed: %w", err)
		}
	}

	stdout.Info("overall download summary",
		"runtime", runtimeName,
		"versions_processed", len(versionsToDownload),
		"versions_successful", totalSuccess,
		"versions_failed", totalFailed,
		"total_files", len(allResults))

	// Create structured output for JSON
	summary := DownloadSummary{
		Runtime:    runtimeName,
		Version:    fmt.Sprintf("%d versions", len(versionsToDownload)),
		TotalFiles: len(allResults),
		Successful: totalSuccess,
		Failed:     totalFailed,
		OutputDir:  outputDir,
		Results:    allResults,
	}

	// Output results based on format
	if outputFormat == "json" {
		output, err := json.MarshalIndent(summary, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal JSON: %w", err)
		}
		fmt.Println(string(output))
		return nil
	}

	// No text output - all output is via structured logging (JSON)
	return nil
}

// getClassifiersFromPlatforms extracts platform classifiers for display.
func getClassifiersFromPlatforms(platforms []platform.Platform) []string {
	var classifiers []string
	for _, p := range platforms {
		classifiers = append(classifiers, p.Classifier)
	}
	return classifiers
}

// versionDownload represents download results for a single version.
type versionDownload struct {
	version string
	results []runtime.DownloadResult
}

// handleAggregatedAutoRelease creates a single aggregated GitHub release for multiple downloaded versions.
// This function is called after successful downloads when auto_release is enabled.
// It fails immediately if GITHUB_TOKEN is not set, as it's required for creating releases.
// In GitHub Actions, GITHUB_TOKEN is automatically provided if the workflow has 'contents: write' permission.
func handleAggregatedAutoRelease(
	runtimeName string,
	successfulVersions []versionDownload,
	outputDir string,
	releaseConfig *config.ReleaseConfig,
	db *storage.DB,
	stdout, stderr *slog.Logger,
) error {
	if len(successfulVersions) == 0 {
		return nil
	}

	stdout.Info("auto_release enabled, creating aggregated GitHub release",
		"runtime", runtimeName,
		"version_count", len(successfulVersions))

	// Get GitHub token from environment
	// In GitHub Actions, this is automatically provided if workflow has 'contents: write' permission
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		return fmt.Errorf("GITHUB_TOKEN environment variable is required for auto_release")
	}

	// Create GitHub client
	githubClient, err := gh.NewClient(token, releaseConfig.GitHubRepository)
	if err != nil {
		return fmt.Errorf("failed to create GitHub client: %w", err)
	}

	// Initialize release manager
	releaseManager, err := NewReleaseManager(releaseConfig, githubClient, db, stdout, stderr)
	if err != nil {
		return fmt.Errorf("failed to initialize release manager: %w", err)
	}

	if releaseManager == nil {
		return fmt.Errorf("release manager is nil (auto_release may be disabled)")
	}

	// Extract versions list
	var versions []string
	var allResults []runtime.DownloadResult
	for _, vd := range successfulVersions {
		versions = append(versions, vd.version)
		allResults = append(allResults, vd.results...)
	}

	// Create aggregated release with all artifacts
	release, err := releaseManager.CreateAggregatedRelease(
		runtimeName,
		versions,
		allResults,
		outputDir,
		releaseConfig,
	)
	if err != nil {
		return fmt.Errorf("failed to create aggregated release: %w", err)
	}

	stdout.Info("aggregated release created successfully",
		"tag", release.ReleaseTag,
		"url", release.ReleaseURL,
		"versions", versions)

	return nil
}

// sitegenCommand implements the sitegen command.
func sitegenCommand(c *cli.Context) error {
	outputDir := c.String("out")
	dryRun := c.Bool("dry-run")

	// Create loggers
	logLevel := ParseLogLevelOrDefault(c.String("log-level"))
	stdout, stderr := NewLoggers(logLevel)

	// Determine database path
	dbPath := c.String("db")
	if dbPath == "" {
		// Try to get from config
		configPath := c.String("config")
		cfg, err := config.LoadConfig(configPath)
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}
		dbPath = cfg.Config.Storage.DatabasePath
		if dbPath == "" {
			return fmt.Errorf("database path not specified and not found in config")
		}
	}

	// Open database
	db, err := storage.InitDB(storage.Config{
		DatabasePath: dbPath,
		LogLevel:     "silent", // Database logs are verbose, suppress them
	})
	if err != nil {
		return fmt.Errorf("failed to initialize database: %w", err)
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			stderr.Error("failed to close database", "error", closeErr)
		}
	}()

	// Create generator
	generator := sitegen.NewGenerator(db, stdout)

	// Generate site
	opts := sitegen.GenerateOptions{
		OutputDir: outputDir,
		DryRun:    dryRun,
	}

	if err := generator.Generate(c.Context, opts); err != nil {
		return fmt.Errorf("site generation failed: %w", err)
	}

	stdout.Info("site generation completed successfully")
	return nil
}
