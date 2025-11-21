// Package runtime provides the core abstraction layer for the unified runtime download system.
// It defines interfaces and types for runtime providers, version management, and download coordination.
package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/clean-dependency-project/cdprun/internal/clamav"
	"github.com/clean-dependency-project/cdprun/internal/config"
	"github.com/clean-dependency-project/cdprun/internal/endoflife"
	"github.com/clean-dependency-project/cdprun/internal/platform"
	"github.com/clean-dependency-project/cdprun/internal/storage"
)

// RuntimeProvider defines the interface that all runtime adapters must implement.
// It provides unified access to version discovery, download tasks, and verification strategies.
type RuntimeProvider interface {
	// Metadata operations
	GetName() string
	GetEndOfLifeProduct() string
	GetSupportedPlatforms() []platform.Platform

	// Version operations using existing endoflife.VersionInfo
	ListVersions(ctx context.Context) ([]endoflife.VersionInfo, error)
	GetLatestVersion(ctx context.Context, opts VersionOptions) (endoflife.VersionInfo, error)

	// Download operations
	CreateDownloadTasks(version endoflife.VersionInfo, platforms []platform.Platform, outputDir string) ([]DownloadTask, error)
	ProcessDownloads(ctx context.Context, tasks []DownloadTask, concurrency int) ([]DownloadResult, error)

	// Verification operations
	GetVerificationStrategy() VerificationStrategy

	// Policy operations using existing endoflife types
	LoadPolicy(filePath string) ([]endoflife.PolicyVersion, error)
	ApplyPolicy(versions []endoflife.VersionInfo, policy []endoflife.PolicyVersion) ([]endoflife.VersionInfo, error)
}

// VersionOptions provides options for version selection.
type VersionOptions struct {
	VersionPattern  string // Specific version pattern to match
	ExactMatch      bool   // Force exact version matching (no pattern matching)
	LTSOnly         bool   // Only return LTS versions
	RecommendedOnly bool   // Only return recommended versions
	Latest          bool   // Return the latest matching version
}

// DownloadTask represents a single download operation.
type DownloadTask struct {
	URL        string            // Download URL
	OutputPath string            // Local output path
	Platform   platform.Platform // Target platform
	Runtime    string            // Runtime name
	Version    string            // Version being downloaded
	FileType   string            // Type of file (main, checksum, signature)
	Headers    map[string]string // HTTP headers to send
	Optional   bool              // Whether this download is optional (won't fail if 404/error)
}

// DownloadResult represents the result of a download operation.
type DownloadResult struct {
	URL       string            // Download URL
	LocalPath string            // Local file path
	FilePath  string            // Alias for LocalPath (for compatibility)
	Platform  platform.Platform // Target platform
	Runtime   string            // Runtime name
	Version   string            // Version downloaded
	Success   bool              // Whether download succeeded
	Error     error             // Error if download failed
	FileSize  int64             // Size of downloaded file
	Size      int64             // Alias for FileSize (for compatibility)
	Duration  time.Duration     // Time taken for download
	Task      *DownloadTask     // Reference to the original task
}

// VerificationStrategy defines the interface for download verification.
type VerificationStrategy interface {
	Verify(ctx context.Context, result DownloadResult) error
	GetType() string
	RequiresAdditionalFiles() bool
}

// Registry manages registered runtime providers.
type Registry struct {
	mu        sync.RWMutex
	providers map[string]RuntimeProvider
}

// NewRegistry creates a new runtime registry.
func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[string]RuntimeProvider),
	}
}

// Register adds a runtime provider to the registry.
func (r *Registry) Register(name string, provider RuntimeProvider) error {
	if name == "" {
		return fmt.Errorf("runtime name cannot be empty")
	}
	if provider == nil {
		return fmt.Errorf("provider cannot be nil")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.providers[name]; exists {
		return fmt.Errorf("runtime %s is already registered", name)
	}

	r.providers[name] = provider
	return nil
}

// Get retrieves a runtime provider by name.
func (r *Registry) Get(name string) (RuntimeProvider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	provider, exists := r.providers[name]
	if !exists {
		return nil, fmt.Errorf("runtime %s not found", name)
	}

	return provider, nil
}

// List returns all registered runtime names.
func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.providers))
	for name := range r.providers {
		names = append(names, name)
	}

	return names
}

// Manager coordinates operations across multiple runtime providers.
type Manager struct {
	registry       *Registry
	httpClient     *http.Client
	stdout         *slog.Logger
	stderr         *slog.Logger
	db             *storage.DB
	config         *config.Config
	clamavScanner  clamav.Scanner
}

// NewManager creates a new runtime manager with the specified registry, database, and loggers.
func NewManager(registry *Registry, db *storage.DB, stdout, stderr *slog.Logger) *Manager {
	return &Manager{
		registry: registry,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		stdout: stdout,
		stderr: stderr,
		db:     db,
	}
}

// SetConfig sets the configuration for the manager.
// This enables ClamAV scanning if configured.
func (m *Manager) SetConfig(cfg *config.Config) {
	m.config = cfg
}

// initClamAVScanner initializes ClamAV scanner if enabled in config for the given runtime.
func (m *Manager) initClamAVScanner(runtimeName string) error {
	if m.config == nil {
		return nil
	}
	
	rtConfig, exists := m.config.GetRuntimeConfig(runtimeName)
	if !exists || !rtConfig.Verification.Enabled || !rtConfig.Verification.Methods.ClamAV.Enabled {
		return nil
	}
	
	// Initialize ClamAV scanner if not already done
	if m.clamavScanner == nil {
		runner := clamav.NewRealCommandRunner()
		image := rtConfig.Verification.Methods.ClamAV.Image
		if image == "" {
			image = "clamav/clamav-debian:latest"
		}
		m.clamavScanner = clamav.NewDockerScanner(runner, image, m.stdout)
		m.stdout.Info("initialized ClamAV scanner", "image", image)
	}
	
	return nil
}

// updateAuditWithClamAV updates the existing audit JSON file with ClamAV scan results.
func (m *Manager) updateAuditWithClamAV(filePath string, scanResult clamav.Result, success bool) {
	auditPath := filePath + ".audit.json"
	
	// Read existing audit file
	data, err := os.ReadFile(auditPath)
	if err != nil {
		m.stderr.Warn("failed to read audit file for ClamAV update",
			"file", filePath,
			"audit_file", auditPath,
			"error", err)
		return
	}
	
	// Parse JSON
	var auditData map[string]interface{}
	if err := json.Unmarshal(data, &auditData); err != nil {
		m.stderr.Warn("failed to parse audit JSON",
			"audit_file", auditPath,
			"error", err)
		return
	}
	
	// Add ClamAV scan results
	auditData["clamav_scanned"] = true
	auditData["clamav_clean"] = scanResult.Clean
	auditData["clamav_threats"] = scanResult.Threats
	auditData["clamav_engine_version"] = scanResult.Metadata.EngineVersion
	auditData["clamav_database_date"] = scanResult.Metadata.DatabaseDate
	auditData["clamav_scan_duration_ms"] = scanResult.Metadata.ScanDuration.Milliseconds()
	
	// Update verification type to include ClamAV
	if verType, ok := auditData["verification_type"].(string); ok {
		auditData["verification_type"] = verType + "+clamav"
	}
	
	// Update overall status if malware detected
	if !scanResult.Clean {
		auditData["verification_status"] = "failed_malware_detected"
	}
	
	// Marshal back to JSON
	updatedData, err := json.MarshalIndent(auditData, "", "  ")
	if err != nil {
		m.stderr.Warn("failed to marshal updated audit data",
			"audit_file", auditPath,
			"error", err)
		return
	}
	
	// Write updated audit file
	if err := os.WriteFile(auditPath, updatedData, 0644); err != nil {
		m.stderr.Warn("failed to write updated audit file",
			"audit_file", auditPath,
			"error", err)
		return
	}
	
	m.stdout.Debug("updated audit file with ClamAV results",
		"file", filePath,
		"audit_file", auditPath,
		"clean", scanResult.Clean)
}

// ListVersions retrieves versions for the specified runtime.
func (m *Manager) ListVersions(ctx context.Context, runtimeName string) ([]endoflife.VersionInfo, error) {
	m.stdout.Debug("listing versions", "runtime", runtimeName)

	provider, err := m.registry.Get(runtimeName)
	if err != nil {
		m.stderr.Error("failed to get runtime provider", "runtime", runtimeName, "error", err)
		return nil, fmt.Errorf("failed to get runtime provider: %w", err)
	}

	versions, err := provider.ListVersions(ctx)
	if err != nil {
		m.stderr.Error("provider failed to list versions", "runtime", runtimeName, "error", err)
		return nil, err
	}

	m.stdout.Debug("versions listed successfully",
		"runtime", runtimeName,
		"version_count", len(versions))

	return versions, nil
}

// GetProvider retrieves a runtime provider by name.
func (m *Manager) GetProvider(runtimeName string) (RuntimeProvider, error) {
	return m.registry.Get(runtimeName)
}

// GetLatestVersion retrieves the latest version for the specified runtime and options.
func (m *Manager) GetLatestVersion(ctx context.Context, runtimeName string, opts VersionOptions) (endoflife.VersionInfo, error) {
	m.stdout.Debug("getting latest version",
		"runtime", runtimeName,
		"version_pattern", opts.VersionPattern,
		"lts_only", opts.LTSOnly,
		"recommended_only", opts.RecommendedOnly)

	provider, err := m.registry.Get(runtimeName)
	if err != nil {
		m.stderr.Error("failed to get runtime provider", "runtime", runtimeName, "error", err)
		return endoflife.VersionInfo{}, fmt.Errorf("failed to get runtime provider: %w", err)
	}

	version, err := provider.GetLatestVersion(ctx, opts)
	if err != nil {
		m.stderr.Error("provider failed to get latest version",
			"runtime", runtimeName,
			"version_pattern", opts.VersionPattern,
			"error", err)
		return endoflife.VersionInfo{}, err
	}

	m.stdout.Debug("latest version resolved",
		"runtime", runtimeName,
		"resolved_version", version.Version,
		"latest_patch", version.LatestPatch,
		"is_lts", version.IsLTS,
		"is_supported", version.IsSupported)

	return version, nil
}

// DownloadRuntime downloads the specified runtime version for the given platforms.
func (m *Manager) DownloadRuntime(ctx context.Context, runtimeName string, version endoflife.VersionInfo, platforms []platform.Platform, outputDir string, concurrency int) ([]DownloadResult, error) {
	m.stdout.Info("starting runtime download",
		"runtime", runtimeName,
		"version", version.Version,
		"latest_patch", version.LatestPatch,
		"platform_count", len(platforms),
		"output_dir", outputDir,
		"concurrency", concurrency)

	provider, err := m.registry.Get(runtimeName)
	if err != nil {
		m.stderr.Error("failed to get runtime provider", "runtime", runtimeName, "error", err)
		return nil, fmt.Errorf("failed to get runtime provider: %w", err)
	}

	// Check database to skip already downloaded files
	if m.db != nil {
		var platformsToDownload []platform.Platform
		for _, plat := range platforms {
			alreadyDownloaded, err := m.db.IsAlreadyDownloaded(
				runtimeName, version.LatestPatch, plat.OS, plat.Arch)
			if err != nil {
				m.stderr.Warn("failed to check download status in database",
					"runtime", runtimeName, "version", version.LatestPatch,
					"platform", plat.Classifier, "error", err)
				platformsToDownload = append(platformsToDownload, plat)
				continue
			}

			if alreadyDownloaded {
				m.stdout.Info("skipping already downloaded and verified file",
					"runtime", runtimeName, "version", version.LatestPatch,
					"platform", plat.Classifier)
				continue
			}
			platformsToDownload = append(platformsToDownload, plat)
		}

		if len(platformsToDownload) == 0 {
			m.stdout.Info("all files already downloaded, skipping",
				"runtime", runtimeName, "version", version.Version)
			return []DownloadResult{}, nil
		}
		platforms = platformsToDownload
	}

	// Create download tasks
	m.stdout.Debug("creating download tasks", "runtime", runtimeName, "version", version.Version)
	tasks, err := provider.CreateDownloadTasks(version, platforms, outputDir)
	if err != nil {
		m.stderr.Error("failed to create download tasks",
			"runtime", runtimeName,
			"version", version.Version,
			"error", err)
		return nil, fmt.Errorf("failed to create download tasks: %w", err)
	}

	m.stdout.Debug("download tasks created",
		"runtime", runtimeName,
		"version", version.Version,
		"task_count", len(tasks))

	// Process downloads
	results, err := provider.ProcessDownloads(ctx, tasks, concurrency)
	if err != nil {
		m.stderr.Error("failed to process downloads",
			"runtime", runtimeName,
			"version", version.Version,
			"task_count", len(tasks),
			"error", err)
		return nil, fmt.Errorf("failed to process downloads: %w", err)
	}

	// Count successes and failures before verification
	downloadSuccessCount := 0
	downloadFailureCount := 0
	for _, result := range results {
		if result.Error != nil {
			downloadFailureCount++
		} else {
			downloadSuccessCount++
		}
	}

	m.stdout.Debug("downloads completed",
		"runtime", runtimeName,
		"version", version.Version,
		"total_files", len(results),
		"successful", downloadSuccessCount,
		"failed", downloadFailureCount)

	// Verify downloads using the runtime's verification strategy (checksum, GPG, etc.)
	verificationStrategy := provider.GetVerificationStrategy()
	if verificationStrategy != nil {
		m.stdout.Debug("starting verification",
			"runtime", runtimeName,
			"version", version.Version,
			"verification_type", verificationStrategy.GetType())

		verificationFailures := 0

		// Only verify main file downloads (not verification files themselves)
		for i, result := range results {
			if result.Error != nil {
				m.stderr.Error("skipping verification for failed download",
					"runtime", runtimeName,
					"url", result.URL,
					"error", result.Error)
				continue
			}

			// Only verify main files, skip checksum and signature files
			if result.Task != nil && result.Success && result.Task.FileType == "main" {
				m.stdout.Debug("verifying file",
					"runtime", runtimeName,
					"file", result.LocalPath,
					"verification_type", verificationStrategy.GetType())

				if err := verificationStrategy.Verify(ctx, result); err != nil {
					// Update result to indicate verification failure
					results[i].Success = false
					results[i].Error = fmt.Errorf("verification failed: %w", err)
					verificationFailures++

					m.stderr.Error("verification failed",
						"runtime", runtimeName,
						"file", result.LocalPath,
						"verification_type", verificationStrategy.GetType(),
						"error", err)
				} else {
					m.stdout.Debug("verification successful",
						"runtime", runtimeName,
						"file", result.LocalPath,
						"verification_type", verificationStrategy.GetType())
				}
			}
		}

		if verificationFailures > 0 {
			m.stderr.Warn("verification completed with failures",
				"runtime", runtimeName,
				"version", version.Version,
				"verification_failures", verificationFailures)
		} else {
			m.stdout.Debug("all verifications successful",
				"runtime", runtimeName,
				"version", version.Version)
		}
	} else {
		m.stdout.Debug("no verification strategy configured",
			"runtime", runtimeName,
			"version", version.Version)
	}

	// Run ClamAV malware scanning on all main files (universal security check)
	if err := m.initClamAVScanner(runtimeName); err != nil {
		m.stderr.Warn("failed to initialize ClamAV scanner", "error", err)
	}
	
	if m.clamavScanner != nil {
		m.stdout.Info("starting ClamAV malware scan", "runtime", runtimeName)
		clamavFailures := 0
		
		for i, result := range results {
			// Only scan main files that passed previous verification
			if result.Error != nil || !result.Success {
				continue
			}
			
			if result.Task != nil && result.Task.FileType == "main" {
				m.stdout.Debug("scanning file for malware",
					"runtime", runtimeName,
					"file", result.LocalPath)
				
				scanResult, err := m.clamavScanner.Scan(ctx, result.LocalPath)
				if err != nil {
					results[i].Success = false
					results[i].Error = fmt.Errorf("malware scan failed: %w", err)
					clamavFailures++
					
					m.stderr.Error("ClamAV scan failed",
						"runtime", runtimeName,
						"file", result.LocalPath,
						"error", err)
					continue
				}
				
				if !scanResult.Clean {
					// Malware detected - delete file and fail
					results[i].Success = false
					results[i].Error = fmt.Errorf("malware detected: %v", scanResult.Threats)
					clamavFailures++
					
					m.stderr.Error("malware detected",
						"runtime", runtimeName,
						"file", result.LocalPath,
						"threats", scanResult.Threats,
						"engine_version", scanResult.Metadata.EngineVersion)
					
					// Update audit file with malware detection
					m.updateAuditWithClamAV(result.LocalPath, scanResult, false)
					
					// Delete infected file
					if err := os.Remove(result.LocalPath); err != nil {
						m.stderr.Error("failed to delete infected file",
							"file", result.LocalPath,
							"error", err)
					} else {
						m.stdout.Info("deleted infected file", "file", result.LocalPath)
					}
				} else {
					m.stdout.Info("ClamAV scan passed",
						"runtime", runtimeName,
						"file", result.LocalPath,
						"engine_version", scanResult.Metadata.EngineVersion,
						"scan_duration", scanResult.Metadata.ScanDuration)
					
					// Update audit file with clean scan result
					m.updateAuditWithClamAV(result.LocalPath, scanResult, true)
				}
			}
		}
		
		if clamavFailures > 0 {
			m.stderr.Warn("ClamAV scan completed with failures",
				"runtime", runtimeName,
				"clamav_failures", clamavFailures)
		}
	}

	// Record downloads to database
	if m.db != nil {
		for _, result := range results {
			// Only record main files that were successfully downloaded and verified
			if result.Error != nil || result.Task == nil || result.Task.FileType != "main" {
				continue
			}

			major, minor, patch, err := storage.ParseSemver(version.LatestPatch)
			if err != nil {
				m.stderr.Warn("failed to parse semver", "version", version.LatestPatch, "error", err)
				major, minor, patch = 0, 0, 0
			}

			// Determine verification type including ClamAV if it was run
			verificationType := provider.GetVerificationStrategy().GetType()
			if m.clamavScanner != nil {
				verificationType += "+clamav"
			}

			download := &storage.Download{
				Runtime:            runtimeName,
				Version:            version.LatestPatch,
				VersionMajor:       major,
				VersionMinor:       minor,
				VersionPatch:       patch,
				Platform:           result.Platform.OS,
				Architecture:       result.Platform.Arch,
				Filename:           storage.ExtractFilename(result.LocalPath),
				FileExtension:      storage.ExtractExtension(result.LocalPath),
				FileSize:           result.FileSize,
				SourceURL:          result.URL,
				DownloadedAt:       time.Now(),
				VerificationStatus: "success",
				VerificationType:   verificationType,
			}

			if err := m.db.RecordDownload(download); err != nil {
				m.stderr.Warn("failed to record download", "error", err)
			} else {
				m.stdout.Debug("recorded download to database",
					"runtime", runtimeName, "version", version.LatestPatch,
					"file", result.LocalPath,
					"verification_type", verificationType)
			}
		}
	}

	// Final count after verification
	finalSuccessCount := 0
	finalFailureCount := 0
	for _, result := range results {
		if result.Error != nil {
			finalFailureCount++
		} else {
			finalSuccessCount++
		}
	}

	m.stdout.Info("runtime download completed",
		"runtime", runtimeName,
		"version", version.Version,
		"total_files", len(results),
		"successful", finalSuccessCount,
		"failed", finalFailureCount)

	return results, nil
}

// verifyDownloads verifies downloaded files
func (m *Manager) verifyDownloads(ctx context.Context, results []DownloadResult, strategy VerificationStrategy, auditLogger *slog.Logger) {
	// Inject audit logger into strategy if it has a Logger field
	// This uses type assertion to check if the strategy has a Logger field that can be set
	if auditLogger != nil {
		// Try to set the logger on strategies that support it using reflection
		// For now, we'll just skip this and let individual strategies handle their own logging
		_ = auditLogger
	}

	m.stdout.Debug("starting verification", "verification_type", strategy.GetType())

	verificationFailures := 0
	for i, result := range results {
		if result.Error != nil || !result.Success {
			continue
		}

		if result.Task != nil && result.Task.FileType == "main" {
			if err := strategy.Verify(ctx, result); err != nil {
				results[i].Success = false
				results[i].Error = fmt.Errorf("verification failed: %w", err)
				verificationFailures++

				m.stderr.Error("verification failed",
					"file", result.LocalPath,
					"error", err)
			}
		}
	}

	if verificationFailures > 0 {
		m.stderr.Warn("verification completed with failures",
			"failures", verificationFailures)
	}
}

// VersionInfo represents version information combining endoflife.VersionInfo.
// This is an alias to maintain compatibility while potentially extending functionality.
type VersionInfo = endoflife.VersionInfo

// ConcurrentDownloader handles concurrent download operations.
type ConcurrentDownloader struct {
	concurrency int
	timeout     time.Duration
	httpClient  *http.Client
	stdout      *slog.Logger
	stderr      *slog.Logger
}

// NewConcurrentDownloader creates a new concurrent downloader with loggers.
func NewConcurrentDownloader(concurrency int, timeout time.Duration, stdout, stderr *slog.Logger) *ConcurrentDownloader {
	return &ConcurrentDownloader{
		concurrency: concurrency,
		timeout:     timeout,
		httpClient: &http.Client{
			Timeout: timeout,
		},
		stdout: stdout,
		stderr: stderr,
	}
}

// ProcessDownloads executes multiple download tasks concurrently.
func (d *ConcurrentDownloader) ProcessDownloads(ctx context.Context, tasks []DownloadTask) ([]DownloadResult, error) {
	if len(tasks) == 0 {
		d.stdout.Debug("no download tasks to process")
		return []DownloadResult{}, nil
	}

	d.stdout.Info("starting concurrent downloads",
		"task_count", len(tasks),
		"concurrency", d.concurrency,
		"timeout", d.timeout)

	// Channel to control concurrency
	semaphore := make(chan struct{}, d.concurrency)
	results := make([]DownloadResult, len(tasks))
	var wg sync.WaitGroup
	var mu sync.Mutex // Mutex to protect results slice

	for i, task := range tasks {
		wg.Add(1)
		go func(index int, t DownloadTask) {
			defer wg.Done()

			// Acquire semaphore with context support
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				mu.Lock()
				results[index] = DownloadResult{
					Task:  &t,
					Error: ctx.Err(),
				}
				mu.Unlock()
				return
			}

			result := d.downloadFile(ctx, t)

			// Use mutex to safely write to results slice
			mu.Lock()
			results[index] = result
			mu.Unlock()
		}(i, task)
	}

	wg.Wait()

	// Count successes and failures
	successCount := 0
	failureCount := 0
	totalSize := int64(0)
	totalDuration := time.Duration(0)

	for _, result := range results {
		if result.Error != nil {
			failureCount++
		} else {
			successCount++
			totalSize += result.FileSize
		}
		totalDuration += result.Duration
	}

	d.stdout.Info("concurrent downloads completed",
		"total_tasks", len(tasks),
		"successful", successCount,
		"failed", failureCount,
		"total_size_bytes", totalSize,
		"total_duration_ms", totalDuration.Milliseconds())

	return results, nil
}

// downloadFile downloads a single file.
func (d *ConcurrentDownloader) downloadFile(ctx context.Context, task DownloadTask) DownloadResult {
	start := time.Now()
	result := DownloadResult{
		URL:       task.URL,
		LocalPath: task.OutputPath,
		FilePath:  task.OutputPath, // Compatibility alias
		Platform:  task.Platform,
		Runtime:   task.Runtime,
		Version:   task.Version,
		Success:   false,
		Task:      &task, // Reference to original task
	}

	d.stdout.Debug("starting file download",
		"url", task.URL,
		"output_path", task.OutputPath,
		"runtime", task.Runtime,
		"version", task.Version,
		"file_type", task.FileType,
		"optional", task.Optional)

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "GET", task.URL, nil)
	if err != nil {
		result.Error = fmt.Errorf("failed to create request: %w", err)
		result.Duration = time.Since(start)
		d.stderr.Error("failed to create HTTP request",
			"url", task.URL,
			"error", err,
			"duration_ms", result.Duration.Milliseconds())
		return result
	}

	// Set basic headers
	req.Header.Set("User-Agent", fmt.Sprintf("cdprun/1.0 (%s)", task.Runtime))

	// Set custom headers from task
	for k, v := range task.Headers {
		req.Header.Set(k, v)
	}

	// Create directory for output file
	if err := os.MkdirAll(filepath.Dir(task.OutputPath), 0755); err != nil {
		result.Error = fmt.Errorf("failed to create output directory: %w", err)
		result.Duration = time.Since(start)
		d.stderr.Error("failed to create output directory",
			"output_path", task.OutputPath,
			"error", err,
			"duration_ms", result.Duration.Milliseconds())
		return result
	}

	// Create output file
	out, err := os.Create(task.OutputPath)
	if err != nil {
		result.Error = fmt.Errorf("failed to create output file: %w", err)
		result.Duration = time.Since(start)
		d.stderr.Error("failed to create output file",
			"output_path", task.OutputPath,
			"error", err,
			"duration_ms", result.Duration.Milliseconds())
		return result
	}
	defer func() {
		_ = out.Close() // Ignore close error to not override return error
	}()

	// Make HTTP request
	resp, err := d.httpClient.Do(req)
	if err != nil {
		result.Error = fmt.Errorf("failed to send HTTP request: %w", err)
		result.Duration = time.Since(start)
		d.stderr.Error("HTTP request failed",
			"url", task.URL,
			"error", err,
			"duration_ms", result.Duration.Milliseconds())
		return result
	}
	defer func() {
		_ = resp.Body.Close() // Ignore close error to not override return error
	}()

	// Handle optional files that return 404 or similar errors
	if resp.StatusCode != http.StatusOK {
		if task.Optional && (resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusForbidden) {
			// For optional files, treat 404/403 as success but don't create the file
			result.Success = true
			result.Duration = time.Since(start)
			result.FileSize = 0
			result.Size = 0
			// Remove the empty file we created
			_ = os.Remove(task.OutputPath) // Ignore error if file doesn't exist

			d.stdout.Debug("optional file not available",
				"url", task.URL,
				"status_code", resp.StatusCode,
				"file_type", task.FileType,
				"duration_ms", result.Duration.Milliseconds())
			return result
		}
		result.Error = fmt.Errorf("download returned status %d: %s", resp.StatusCode, resp.Status)
		result.Duration = time.Since(start)
		d.stderr.Error("download failed with HTTP error",
			"url", task.URL,
			"status_code", resp.StatusCode,
			"status", resp.Status,
			"optional", task.Optional,
			"duration_ms", result.Duration.Milliseconds())
		return result
	}

	// Copy response body to file and track size
	size, err := io.Copy(out, resp.Body)
	if err != nil {
		result.Error = fmt.Errorf("failed to write file: %w", err)
		result.Duration = time.Since(start)
		d.stderr.Error("failed to write downloaded content",
			"url", task.URL,
			"output_path", task.OutputPath,
			"error", err,
			"duration_ms", result.Duration.Milliseconds())
		return result
	}

	result.Success = true
	result.Duration = time.Since(start)
	result.FileSize = size
	result.Size = size // Compatibility alias

	d.stdout.Debug("file download completed",
		"url", task.URL,
		"output_path", task.OutputPath,
		"size_bytes", size,
		"duration_ms", result.Duration.Milliseconds(),
		"file_type", task.FileType)

	return result
}

// DownloadRuntimeWithAudit downloads runtime with optional audit logging
func (m *Manager) DownloadRuntimeWithAudit(ctx context.Context, runtimeName string, version endoflife.VersionInfo, platforms []platform.Platform, outputDir string, concurrency int, auditLogger *slog.Logger) ([]DownloadResult, error) {
	m.stdout.Info("starting runtime download",
		"runtime", runtimeName,
		"version", version.Version,
		"latest_patch", version.LatestPatch,
		"platform_count", len(platforms),
		"output_dir", outputDir,
		"audit_enabled", auditLogger != nil)

	provider, err := m.registry.Get(runtimeName)
	if err != nil {
		m.stderr.Error("failed to get runtime provider", "runtime", runtimeName, "error", err)
		return nil, fmt.Errorf("failed to get runtime provider: %w", err)
	}

	// Create download tasks
	m.stdout.Debug("creating download tasks", "runtime", runtimeName, "version", version.Version)
	tasks, err := provider.CreateDownloadTasks(version, platforms, outputDir)
	if err != nil {
		m.stderr.Error("failed to create download tasks",
			"runtime", runtimeName,
			"version", version.Version,
			"error", err)
		return nil, fmt.Errorf("failed to create download tasks: %w", err)
	}

	m.stdout.Info("download tasks created",
		"runtime", runtimeName,
		"version", version.Version,
		"task_count", len(tasks))

	// Process downloads
	results, err := provider.ProcessDownloads(ctx, tasks, concurrency)
	if err != nil {
		m.stderr.Error("failed to process downloads",
			"runtime", runtimeName,
			"version", version.Version,
			"task_count", len(tasks),
			"error", err)
		return nil, fmt.Errorf("failed to process downloads: %w", err)
	}

	// Verify downloads with audit logging
	verificationStrategy := provider.GetVerificationStrategy()
	if verificationStrategy != nil {
		m.verifyDownloads(ctx, results, verificationStrategy, auditLogger)
	}

	return results, nil
}
