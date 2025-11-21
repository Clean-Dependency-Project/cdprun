package runtime

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/clean-dependency-project/cdprun/internal/endoflife"
	"github.com/clean-dependency-project/cdprun/internal/platform"
)

// mockRuntimeProvider implements RuntimeProvider for testing
type mockRuntimeProvider struct {
	name         string
	eolProduct   string
	platforms    []platform.Platform
	versions     []endoflife.VersionInfo
	policy       []endoflife.PolicyVersion
	verification VerificationStrategy
	tasks        []DownloadTask
	results      []DownloadResult
	shouldError  bool
	errorOnLoad  bool
	errorOnApply bool
}

func (m *mockRuntimeProvider) GetName() string {
	return m.name
}

func (m *mockRuntimeProvider) GetEndOfLifeProduct() string {
	return m.eolProduct
}

func (m *mockRuntimeProvider) GetSupportedPlatforms() []platform.Platform {
	return m.platforms
}

func (m *mockRuntimeProvider) ListVersions(ctx context.Context) ([]endoflife.VersionInfo, error) {
	if m.shouldError {
		return nil, fmt.Errorf("mock error")
	}
	return m.versions, nil
}

func (m *mockRuntimeProvider) GetLatestVersion(ctx context.Context, opts VersionOptions) (endoflife.VersionInfo, error) {
	if m.shouldError {
		return endoflife.VersionInfo{}, fmt.Errorf("mock error")
	}
	if len(m.versions) == 0 {
		return endoflife.VersionInfo{}, fmt.Errorf("no versions available")
	}

	// Filter based on options
	for _, version := range m.versions {
		if opts.LTSOnly && !version.IsLTS {
			continue
		}
		if opts.RecommendedOnly && !version.IsRecommended {
			continue
		}
		// Note: IsSupported filtering is handled through policy application, not here
		return version, nil
	}

	return endoflife.VersionInfo{}, fmt.Errorf("no versions match criteria")
}

func (m *mockRuntimeProvider) CreateDownloadTasks(version endoflife.VersionInfo, platforms []platform.Platform, outputDir string) ([]DownloadTask, error) {
	if m.shouldError {
		return nil, fmt.Errorf("mock error")
	}
	return m.tasks, nil
}

func (m *mockRuntimeProvider) ProcessDownloads(ctx context.Context, tasks []DownloadTask, concurrency int) ([]DownloadResult, error) {
	if m.shouldError {
		return nil, fmt.Errorf("mock error")
	}
	return m.results, nil
}

func (m *mockRuntimeProvider) GetVerificationStrategy() VerificationStrategy {
	return m.verification
}

func (m *mockRuntimeProvider) LoadPolicy(filePath string) ([]endoflife.PolicyVersion, error) {
	if m.shouldError || m.errorOnLoad {
		return nil, fmt.Errorf("mock error loading policy")
	}
	return m.policy, nil
}

func (m *mockRuntimeProvider) ApplyPolicy(versions []endoflife.VersionInfo, policy []endoflife.PolicyVersion) ([]endoflife.VersionInfo, error) {
	if m.shouldError || m.errorOnApply {
		return nil, fmt.Errorf("mock error applying policy")
	}
	// Simple filter: only return supported versions
	var filtered []endoflife.VersionInfo
	for _, v := range versions {
		if v.IsSupported {
			filtered = append(filtered, v)
		}
	}
	return filtered, nil
}

// mockVerificationStrategy implements VerificationStrategy for testing
type mockVerificationStrategy struct {
	strategyType string
	shouldError  bool
}

func (m *mockVerificationStrategy) Verify(ctx context.Context, result DownloadResult) error {
	if m.shouldError {
		return fmt.Errorf("verification failed")
	}
	// For "none" type verification, allow failed downloads to pass verification
	if m.strategyType == "none" && result.Error != nil {
		return nil // No verification strategy should not fail even for failed downloads
	}
	if result.Error != nil {
		return fmt.Errorf("cannot verify failed download")
	}
	return nil
}

func (m *mockVerificationStrategy) GetType() string {
	return m.strategyType
}

func (m *mockVerificationStrategy) RequiresAdditionalFiles() bool {
	return false
}

func createTestVersionInfo(version string, supported bool) endoflife.VersionInfo {
	return endoflife.VersionInfo{
		Version:       version,
		LatestPatch:   version + ".1",
		IsSupported:   supported,
		IsRecommended: supported,
		IsLTS:         false,
		RuntimeName:   "test",
	}
}

func createLTSVersionInfo(version string, supported bool, isLTS bool) endoflife.VersionInfo {
	return endoflife.VersionInfo{
		Version:       version,
		LatestPatch:   version + ".1",
		IsSupported:   supported,
		IsRecommended: supported,
		IsLTS:         isLTS,
		RuntimeName:   "test",
	}
}

func TestRegistry_Register(t *testing.T) {
	tests := []struct {
		name     string
		runtime  string
		provider RuntimeProvider
		wantErr  bool
	}{
		{
			name:     "successful registration",
			runtime:  "python",
			provider: &mockRuntimeProvider{name: "python"},
			wantErr:  false,
		},
		{
			name:     "empty runtime name",
			runtime:  "",
			provider: &mockRuntimeProvider{name: "python"},
			wantErr:  true,
		},
		{
			name:     "nil provider",
			runtime:  "python",
			provider: nil,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewRegistry()
			err := registry.Register(tt.runtime, tt.provider)
			if (err != nil) != tt.wantErr {
				t.Errorf("Registry.Register() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRegistry_RegisterDuplicate(t *testing.T) {
	registry := NewRegistry()
	provider := &mockRuntimeProvider{name: "python"}

	// First registration should succeed
	err := registry.Register("python", provider)
	if err != nil {
		t.Errorf("First registration failed: %v", err)
	}

	// Second registration should fail
	err = registry.Register("python", provider)
	if err == nil {
		t.Error("Duplicate registration should have failed")
	}
}

func TestRegistry_ConcurrentAccess(t *testing.T) {
	registry := NewRegistry()

	// Test concurrent registration and access
	var wg sync.WaitGroup
	errors := make(chan error, 100)

	// Start multiple goroutines trying to register different runtimes
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			provider := &mockRuntimeProvider{name: fmt.Sprintf("runtime%d", id)}
			err := registry.Register(fmt.Sprintf("runtime%d", id), provider)
			if err != nil {
				errors <- err
			}
		}(i)
	}

	// Start goroutines trying to list and get runtimes
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			registry.List()
			_, _ = registry.Get("runtime1") // May or may not exist
		}()
	}

	wg.Wait()
	close(errors)

	// Check for any errors
	for err := range errors {
		t.Errorf("Concurrent access error: %v", err)
	}

	// Verify all runtimes were registered
	runtimes := registry.List()
	if len(runtimes) != 10 {
		t.Errorf("Expected 10 runtimes, got %d", len(runtimes))
	}
}

func TestRegistry_Get(t *testing.T) {
	registry := NewRegistry()
	provider := &mockRuntimeProvider{name: "python"}
	_ = registry.Register("python", provider)

	tests := []struct {
		name    string
		runtime string
		wantErr bool
	}{
		{
			name:    "existing runtime",
			runtime: "python",
			wantErr: false,
		},
		{
			name:    "non-existing runtime",
			runtime: "nodejs",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := registry.Get(tt.runtime)
			if (err != nil) != tt.wantErr {
				t.Errorf("Registry.Get() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got == nil {
				t.Error("Registry.Get() returned nil provider")
			}
		})
	}
}

func TestRegistry_List(t *testing.T) {
	registry := NewRegistry()

	// Test empty registry
	names := registry.List()
	if len(names) != 0 {
		t.Errorf("Empty registry should return 0 names, got %d", len(names))
	}

	// Add providers
	_ = registry.Register("python", &mockRuntimeProvider{name: "python"})
	_ = registry.Register("nodejs", &mockRuntimeProvider{name: "nodejs"})

	names = registry.List()
	if len(names) != 2 {
		t.Errorf("Expected 2 registered runtimes, got %d", len(names))
	}

	// Check that both names are present
	hasP := false
	hasN := false
	for _, name := range names {
		if name == "python" {
			hasP = true
		}
		if name == "nodejs" {
			hasN = true
		}
	}
	if !hasP || !hasN {
		t.Errorf("Missing expected runtime names. Got: %v", names)
	}
}

func TestManager_ListVersions(t *testing.T) {
	registry := NewRegistry()
	provider := &mockRuntimeProvider{
		name: "python",
		versions: []endoflife.VersionInfo{
			createTestVersionInfo("3.13", true),
			createTestVersionInfo("3.12", false),
		},
		policy: []endoflife.PolicyVersion{
			{Version: "3.13", Supported: true},
		},
	}
	_ = registry.Register("python", provider)

	manager := NewManager(registry, nil, slog.Default(), slog.Default())

	tests := []struct {
		name        string
		runtimeName string
		wantCount   int
		wantErr     bool
	}{
		{
			name:        "list versions",
			runtimeName: "python",
			wantCount:   2,
			wantErr:     false,
		},
		{
			name:        "non-existing runtime",
			runtimeName: "nonexistent",
			wantCount:   0,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			versions, err := manager.ListVersions(context.Background(), tt.runtimeName)
			if (err != nil) != tt.wantErr {
				t.Errorf("Manager.ListVersions() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && len(versions) != tt.wantCount {
				t.Errorf("Manager.ListVersions() got %d versions, want %d", len(versions), tt.wantCount)
			}
		})
	}
}

func TestManager_ListVersions_ErrorCases(t *testing.T) {
	registry := NewRegistry()

	// Test provider that errors on ListVersions
	errorProvider := &mockRuntimeProvider{
		name:        "error-runtime",
		shouldError: true,
	}
	_ = registry.Register("error-runtime", errorProvider)

	// Test provider that errors on LoadPolicy
	policyErrorProvider := &mockRuntimeProvider{
		name:        "policy-error",
		errorOnLoad: true,
		versions: []endoflife.VersionInfo{
			createTestVersionInfo("1.0", true),
		},
	}
	_ = registry.Register("policy-error", policyErrorProvider)

	// Test provider that errors on ApplyPolicy
	applyErrorProvider := &mockRuntimeProvider{
		name:         "apply-error",
		errorOnApply: true,
		versions: []endoflife.VersionInfo{
			createTestVersionInfo("1.0", true),
		},
		policy: []endoflife.PolicyVersion{
			{Version: "1.0", Supported: true},
		},
	}
	_ = registry.Register("apply-error", applyErrorProvider)

	manager := NewManager(registry, nil, slog.Default(), slog.Default())

	tests := []struct {
		name        string
		runtimeName string
		wantErr     bool
	}{
		{
			name:        "error on ListVersions",
			runtimeName: "error-runtime",
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := manager.ListVersions(context.Background(), tt.runtimeName)
			if (err != nil) != tt.wantErr {
				t.Errorf("Manager.ListVersions() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestManager_GetLatestVersion(t *testing.T) {
	registry := NewRegistry()

	// Provider with LTS and non-LTS versions
	provider := &mockRuntimeProvider{
		name: "nodejs",
		versions: []endoflife.VersionInfo{
			createLTSVersionInfo("20", true, true),   // LTS, supported, recommended
			createLTSVersionInfo("19", true, false),  // Non-LTS, supported, recommended
			createLTSVersionInfo("18", true, true),   // LTS, supported, recommended
			createLTSVersionInfo("17", false, false), // Non-LTS, not supported
		},
	}
	_ = registry.Register("nodejs", provider)

	manager := NewManager(registry, nil, slog.Default(), slog.Default())

	tests := []struct {
		name    string
		runtime string
		opts    VersionOptions
		wantVer string
		wantErr bool
	}{
		{
			name:    "get latest version no filter",
			runtime: "nodejs",
			opts:    VersionOptions{},
			wantVer: "20",
			wantErr: false,
		},
		{
			name:    "get latest LTS version",
			runtime: "nodejs",
			opts:    VersionOptions{LTSOnly: true},
			wantVer: "20",
			wantErr: false,
		},
		{
			name:    "get latest recommended version",
			runtime: "nodejs",
			opts:    VersionOptions{RecommendedOnly: true},
			wantVer: "20",
			wantErr: false,
		},
		{
			name:    "non-existing runtime",
			runtime: "nonexistent",
			opts:    VersionOptions{},
			wantVer: "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			version, err := manager.GetLatestVersion(context.Background(), tt.runtime, tt.opts)
			if (err != nil) != tt.wantErr {
				t.Errorf("Manager.GetLatestVersion() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && version.Version != tt.wantVer {
				t.Errorf("Manager.GetLatestVersion() got version %s, want %s", version.Version, tt.wantVer)
			}
		})
	}
}

func TestManager_GetLatestVersion_ErrorCases(t *testing.T) {
	registry := NewRegistry()

	// Provider that errors
	errorProvider := &mockRuntimeProvider{
		name:        "error-runtime",
		shouldError: true,
	}
	_ = registry.Register("error-runtime", errorProvider)

	// Provider with no matching versions
	noMatchProvider := &mockRuntimeProvider{
		name: "no-match",
		versions: []endoflife.VersionInfo{
			createLTSVersionInfo("1.0", false, false), // Not LTS, not supported
		},
	}
	_ = registry.Register("no-match", noMatchProvider)

	manager := NewManager(registry, nil, slog.Default(), slog.Default())

	tests := []struct {
		name    string
		runtime string
		opts    VersionOptions
		wantErr bool
	}{
		{
			name:    "provider error",
			runtime: "error-runtime",
			opts:    VersionOptions{},
			wantErr: true,
		},
		{
			name:    "no LTS versions available",
			runtime: "no-match",
			opts:    VersionOptions{LTSOnly: true},
			wantErr: true,
		},
		{
			name:    "no recommended versions available",
			runtime: "no-match",
			opts:    VersionOptions{RecommendedOnly: true},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := manager.GetLatestVersion(context.Background(), tt.runtime, tt.opts)
			if (err != nil) != tt.wantErr {
				t.Errorf("Manager.GetLatestVersion() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestManager_DownloadRuntime(t *testing.T) {
	registry := NewRegistry()

	platforms := []platform.Platform{
		{OS: "linux", Arch: "x64", FileExt: "tar.gz", Classifier: "linux-x64"},
	}

	tasks := []DownloadTask{
		{
			URL:        "http://example.com/test.tar.gz",
			OutputPath: "/tmp/test.tar.gz",
			Platform:   platforms[0],
			Runtime:    "python",
			Version:    "3.13",
			FileType:   "main",
		},
	}

	results := []DownloadResult{
		{
			Task:      &tasks[0],
			URL:       "http://example.com/test.tar.gz",
			LocalPath: "/tmp/test.tar.gz",
			FilePath:  "/tmp/test.tar.gz",
			Platform:  platforms[0],
			Runtime:   "python",
			Version:   "3.13",
			Success:   true,
			Size:      1024,
			FileSize:  1024,
			Duration:  time.Second,
			Error:     nil,
		},
	}

	provider := &mockRuntimeProvider{
		name: "python",
		versions: []endoflife.VersionInfo{
			createTestVersionInfo("3.13", true),
		},
		tasks:        tasks,
		results:      results,
		verification: &mockVerificationStrategy{strategyType: "mock", shouldError: false},
	}
	_ = registry.Register("python", provider)

	manager := NewManager(registry, nil, slog.Default(), slog.Default())

	// Create version info to pass to DownloadRuntime
	versionInfo := createTestVersionInfo("3.13", true)

	downloadResults, err := manager.DownloadRuntime(
		context.Background(),
		"python",
		versionInfo,
		platforms,
		"/tmp",
		1,
	)

	if err != nil {
		t.Errorf("Manager.DownloadRuntime() error = %v", err)
		return
	}

	if len(downloadResults) != 1 {
		t.Errorf("Manager.DownloadRuntime() got %d results, want 1", len(downloadResults))
		return
	}

	if downloadResults[0].Size != 1024 {
		t.Errorf("Manager.DownloadRuntime() got size %d, want 1024", downloadResults[0].Size)
	}
}

func TestManager_DownloadRuntime_ErrorCases(t *testing.T) {
	registry := NewRegistry()

	// Provider with version but no tasks
	provider := &mockRuntimeProvider{
		name: "python",
		versions: []endoflife.VersionInfo{
			createTestVersionInfo("3.12", true),
		},
		tasks: []DownloadTask{}, // Empty tasks - this should succeed but with no downloads
	}
	_ = registry.Register("python", provider)

	// Provider that errors on CreateDownloadTasks
	errorProvider := &mockRuntimeProvider{
		name: "error-runtime",
		versions: []endoflife.VersionInfo{
			createTestVersionInfo("1.0", true),
		},
		shouldError: true, // This will cause CreateDownloadTasks to fail
	}
	_ = registry.Register("error-runtime", errorProvider)

	manager := NewManager(registry, nil, slog.Default(), slog.Default())

	tests := []struct {
		name        string
		runtime     string
		version     endoflife.VersionInfo // Fix: use VersionInfo instead of string
		platforms   []platform.Platform
		wantErr     bool
		errContains string
	}{
		{
			name:        "empty tasks (should succeed)",
			runtime:     "python",
			version:     createTestVersionInfo("3.12", true), // Use existing version
			platforms:   []platform.Platform{},
			wantErr:     false, // Should not error, just return empty results
			errContains: "",
		},
		{
			name:        "runtime not found",
			runtime:     "nonexistent",
			version:     createTestVersionInfo("1.0", true), // Fix: create VersionInfo
			platforms:   []platform.Platform{},
			wantErr:     true,
			errContains: "not found", // Updated to match actual error message
		},
		{
			name:      "error creating tasks",
			runtime:   "error-runtime",
			version:   createTestVersionInfo("1.0", true), // Fix: create VersionInfo
			platforms: []platform.Platform{{OS: "linux", Arch: "x64"}},
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := manager.DownloadRuntime(
				context.Background(),
				tt.runtime,
				tt.version, // Fix: use VersionInfo
				tt.platforms,
				"/tmp",
				1,
			)

			if (err != nil) != tt.wantErr {
				t.Errorf("Manager.DownloadRuntime() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
				t.Errorf("Manager.DownloadRuntime() error = %v, want error containing %s", err, tt.errContains)
			}
		})
	}
}

func TestVerificationStrategies(t *testing.T) {
	result := DownloadResult{
		FilePath: "/tmp/test.txt",
		Size:     100,
		Error:    nil,
	}

	tests := []struct {
		name     string
		strategy VerificationStrategy
		wantType string
	}{
		{
			name:     "checksum verification",
			strategy: &mockVerificationStrategy{strategyType: "checksum-sha256"},
			wantType: "checksum-sha256",
		},
		{
			name:     "gpg verification",
			strategy: &mockVerificationStrategy{strategyType: "gpg-signature"},
			wantType: "gpg-signature",
		},
		{
			name:     "no verification",
			strategy: &mockVerificationStrategy{strategyType: "none"},
			wantType: "none",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.strategy.GetType() != tt.wantType {
				t.Errorf("Strategy.GetType() = %s, want %s", tt.strategy.GetType(), tt.wantType)
			}

			err := tt.strategy.Verify(context.Background(), result)
			if err != nil {
				t.Errorf("Strategy.Verify() returned error: %v", err)
			}
		})
	}
}

func TestVerificationStrategies_FailedDownload(t *testing.T) {
	result := DownloadResult{
		FilePath: "/tmp/test.txt",
		Size:     0,
		Error:    fmt.Errorf("download failed"),
	}

	strategies := []VerificationStrategy{
		&mockVerificationStrategy{strategyType: "checksum-sha256"},
		&mockVerificationStrategy{strategyType: "gpg-signature"},
	}

	for _, strategy := range strategies {
		err := strategy.Verify(context.Background(), result)
		if err == nil {
			t.Errorf("Strategy %s should fail verification for failed download", strategy.GetType())
		}
	}

	// NoVerificationStrategy should still pass
	noVerify := &mockVerificationStrategy{strategyType: "none"}
	err := noVerify.Verify(context.Background(), result)
	if err != nil {
		t.Errorf("NoVerificationStrategy should not fail even for failed downloads: %v", err)
	}
}

func TestHTTPDownloader_Download_Success(t *testing.T) {
	// Create test server
	testContent := "test file content"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(testContent)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(testContent))
	}))
	defer server.Close()

	// Create temporary directory and file
	tempDir, err := os.MkdirTemp("", "runtime-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	outputPath := filepath.Join(tempDir, "test.txt")

	downloader := NewConcurrentDownloader(1, 5*time.Second, slog.Default(), slog.Default())

	task := DownloadTask{
		URL:        server.URL,
		OutputPath: outputPath,
		Runtime:    "test",
		Version:    "1.0",
		FileType:   "main",
	}

	results, err := downloader.ProcessDownloads(context.Background(), []DownloadTask{task})
	if err != nil {
		t.Errorf("ProcessDownloads failed: %v", err)
		return
	}

	if len(results) != 1 {
		t.Errorf("Expected 1 result, got %d", len(results))
		return
	}

	result := results[0]
	if result.Error != nil {
		t.Errorf("Download failed: %v", result.Error)
		return
	}

	if result.Size != int64(len(testContent)) {
		t.Errorf("Download size = %d, want %d", result.Size, len(testContent))
	}

	// Verify file was created
	if _, err := os.Stat(outputPath); os.IsNotExist(err) {
		t.Error("Downloaded file does not exist")
	}

	// Verify file content
	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Errorf("Failed to read downloaded file: %v", err)
	}

	if string(content) != testContent {
		t.Errorf("File content = %s, want %s", string(content), testContent)
	}
}

func TestHTTPDownloader_Download_ErrorCases(t *testing.T) {
	downloader := NewConcurrentDownloader(1, 5*time.Second, slog.Default(), slog.Default())

	tests := []struct {
		name string
		task DownloadTask
	}{
		{
			name: "invalid URL",
			task: DownloadTask{
				URL:        "://invalid-url",
				OutputPath: "/tmp/test.txt",
				Runtime:    "test",
				Version:    "1.0",
			},
		},
		{
			name: "non-existent URL",
			task: DownloadTask{
				URL:        "http://non-existent-host-12345.com/file.txt",
				OutputPath: "/tmp/test.txt",
				Runtime:    "test",
				Version:    "1.0",
			},
		},
		{
			name: "invalid output path",
			task: DownloadTask{
				URL:        "http://example.com/file.txt",
				OutputPath: "/invalid/path/that/does/not/exist/file.txt",
				Runtime:    "test",
				Version:    "1.0",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := downloader.ProcessDownloads(context.Background(), []DownloadTask{tt.task})
			if err != nil {
				t.Errorf("ProcessDownloads should not error: %v", err)
				return
			}
			if len(results) != 1 {
				t.Errorf("Expected 1 result, got %d", len(results))
				return
			}
			if results[0].Error == nil {
				t.Error("Download should have failed")
			}
		})
	}
}

func TestHTTPDownloader_Download_HTTPErrors(t *testing.T) {
	// Create test server that returns errors
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/404":
			http.Error(w, "Not Found", http.StatusNotFound)
		case "/500":
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		case "/401":
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
		default:
			http.Error(w, "Unknown", http.StatusBadRequest)
		}
	}))
	defer server.Close()

	downloader := NewConcurrentDownloader(1, 5*time.Second, slog.Default(), slog.Default())

	tests := []struct {
		name string
		path string
	}{
		{"404 error", "/404"},
		{"500 error", "/500"},
		{"401 error", "/401"},
		{"400 error", "/unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := DownloadTask{
				URL:        server.URL + tt.path,
				OutputPath: "/tmp/test.txt",
				Runtime:    "test",
				Version:    "1.0",
			}

			results, err := downloader.ProcessDownloads(context.Background(), []DownloadTask{task})
			if err != nil {
				t.Errorf("ProcessDownloads should not error: %v", err)
				return
			}
			if len(results) != 1 {
				t.Errorf("Expected 1 result, got %d", len(results))
				return
			}
			if results[0].Error == nil {
				t.Error("Download should have failed for HTTP error")
			}
		})
	}
}

func TestHTTPDownloader_Download_Timeout(t *testing.T) {
	// Create test server that delays response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second) // Longer than timeout
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("delayed content"))
	}))
	defer server.Close()

	// Use very short timeout
	downloader := NewConcurrentDownloader(1, 100*time.Millisecond, slog.Default(), slog.Default())

	task := DownloadTask{
		URL:        server.URL,
		OutputPath: "/tmp/test.txt",
		Runtime:    "test",
		Version:    "1.0",
	}

	results, err := downloader.ProcessDownloads(context.Background(), []DownloadTask{task})
	if err != nil {
		t.Errorf("ProcessDownloads should not error: %v", err)
		return
	}
	if len(results) != 1 {
		t.Errorf("Expected 1 result, got %d", len(results))
		return
	}
	if results[0].Error == nil {
		t.Error("Download should have timed out")
	}
}

func TestHTTPDownloader_Download_CancelledContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(1 * time.Second)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("content"))
	}))
	defer server.Close()

	downloader := NewConcurrentDownloader(1, 5*time.Second, slog.Default(), slog.Default())

	// Create cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	task := DownloadTask{
		URL:        server.URL,
		OutputPath: "/tmp/test.txt",
		Runtime:    "test",
		Version:    "1.0",
	}

	results, err := downloader.ProcessDownloads(ctx, []DownloadTask{task})
	if err != nil {
		t.Errorf("ProcessDownloads should not error: %v", err)
		return
	}
	if len(results) != 1 {
		t.Errorf("Expected 1 result, got %d", len(results))
		return
	}
	if results[0].Error == nil {
		t.Error("Download should have been cancelled")
	}
}

func TestConcurrentDownloader_ProcessDownloads(t *testing.T) {
	downloader := NewConcurrentDownloader(2, 5*time.Second, slog.Default(), slog.Default())

	// Test with empty tasks
	results, err := downloader.ProcessDownloads(context.Background(), []DownloadTask{})
	if err != nil {
		t.Errorf("ProcessDownloads() with empty tasks failed: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("ProcessDownloads() with empty tasks returned %d results, want 0", len(results))
	}
}

func TestConcurrentDownloader_ProcessDownloads_MultipleFiles(t *testing.T) {
	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		content := fmt.Sprintf("content for %s", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(content))
	}))
	defer server.Close()

	tempDir, err := os.MkdirTemp("", "runtime-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	downloader := NewConcurrentDownloader(3, 5*time.Second, slog.Default(), slog.Default())

	tasks := []DownloadTask{
		{
			URL:        server.URL + "/file1.txt",
			OutputPath: filepath.Join(tempDir, "file1.txt"),
			Runtime:    "test",
			Version:    "1.0",
		},
		{
			URL:        server.URL + "/file2.txt",
			OutputPath: filepath.Join(tempDir, "file2.txt"),
			Runtime:    "test",
			Version:    "1.0",
		},
		{
			URL:        server.URL + "/file3.txt",
			OutputPath: filepath.Join(tempDir, "file3.txt"),
			Runtime:    "test",
			Version:    "1.0",
		},
	}

	results, err := downloader.ProcessDownloads(context.Background(), tasks)
	if err != nil {
		t.Errorf("ProcessDownloads() failed: %v", err)
		return
	}

	if len(results) != 3 {
		t.Errorf("ProcessDownloads() returned %d results, want 3", len(results))
		return
	}

	// Verify all downloads succeeded
	for i, result := range results {
		if result.Error != nil {
			t.Errorf("Download %d failed: %v", i, result.Error)
		}
		if result.Size == 0 {
			t.Errorf("Download %d has zero size", i)
		}
	}
}

func TestConcurrentDownloader_ProcessDownloads_Context(t *testing.T) {
	downloader := NewConcurrentDownloader(1, 5*time.Second, slog.Default(), slog.Default())

	// Create a context that's already cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	tasks := []DownloadTask{
		{
			URL:        "http://example.com/test.txt",
			OutputPath: "/tmp/test.txt",
			Runtime:    "test",
			Version:    "1.0",
		},
	}

	results, err := downloader.ProcessDownloads(ctx, tasks)
	if err != nil {
		t.Errorf("ProcessDownloads() returned error: %v", err)
	}

	if len(results) != 1 {
		t.Errorf("ProcessDownloads() returned %d results, want 1", len(results))
		return
	}

	if results[0].Error == nil {
		t.Error("ProcessDownloads() should have failed due to cancelled context")
	}
}

func TestConcurrentDownloader_ProcessDownloads_MixedResults(t *testing.T) {
	// Create test server with mixed responses
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/success":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("success content"))
		case "/error":
			http.Error(w, "Server Error", http.StatusInternalServerError)
		default:
			http.Error(w, "Not Found", http.StatusNotFound)
		}
	}))
	defer server.Close()

	tempDir, err := os.MkdirTemp("", "runtime-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	downloader := NewConcurrentDownloader(2, 5*time.Second, slog.Default(), slog.Default())

	tasks := []DownloadTask{
		{
			URL:        server.URL + "/success",
			OutputPath: filepath.Join(tempDir, "success.txt"),
			Runtime:    "test",
			Version:    "1.0",
		},
		{
			URL:        server.URL + "/error",
			OutputPath: filepath.Join(tempDir, "error.txt"),
			Runtime:    "test",
			Version:    "1.0",
		},
	}

	results, err := downloader.ProcessDownloads(context.Background(), tasks)
	if err != nil {
		t.Errorf("ProcessDownloads() failed: %v", err)
		return
	}

	if len(results) != 2 {
		t.Errorf("ProcessDownloads() returned %d results, want 2", len(results))
		return
	}

	// Check that one succeeded and one failed
	successCount := 0
	errorCount := 0
	for _, result := range results {
		if result.Error == nil {
			successCount++
		} else {
			errorCount++
		}
	}

	if successCount != 1 {
		t.Errorf("Expected 1 successful download, got %d", successCount)
	}
	if errorCount != 1 {
		t.Errorf("Expected 1 failed download, got %d", errorCount)
	}
}

func TestNewManager(t *testing.T) {
	registry := NewRegistry()

	manager := NewManager(registry, nil, slog.Default(), slog.Default())
	if manager == nil {
		t.Fatal("NewManager() returned nil")
	}

	if manager.registry != registry {
		t.Error("NewManager() did not set registry correctly")
	}

	if manager.httpClient == nil {
		t.Error("NewManager() did not initialize HTTP client")
	}
}

func TestNewConcurrentDownloader(t *testing.T) {
	concurrency := 5
	timeout := 10 * time.Second

	downloader := NewConcurrentDownloader(concurrency, timeout, slog.Default(), slog.Default())

	if downloader == nil {
		t.Error("NewConcurrentDownloader() returned nil")
	}
}

func TestNewHTTPDownloader(t *testing.T) {
	// HTTPDownloader functionality is now part of ConcurrentDownloader
	// Test the ConcurrentDownloader instead
	timeout := 10 * time.Second
	downloader := NewConcurrentDownloader(1, timeout, slog.Default(), slog.Default())

	if downloader == nil {
		t.Error("NewConcurrentDownloader() returned nil")
	}
}

// Test GetProvider method
func TestManager_GetProvider(t *testing.T) {
	registry := NewRegistry()
	provider := &mockRuntimeProvider{name: "python"}
	_ = registry.Register("python", provider)

	manager := NewManager(registry, nil, slog.Default(), slog.Default())

	tests := []struct {
		name        string
		runtimeName string
		wantErr     bool
	}{
		{
			name:        "existing provider",
			runtimeName: "python",
			wantErr:     false,
		},
		{
			name:        "non-existing provider",
			runtimeName: "nonexistent",
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := manager.GetProvider(tt.runtimeName)
			if (err != nil) != tt.wantErr {
				t.Errorf("Manager.GetProvider() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && p == nil {
				t.Error("Manager.GetProvider() returned nil provider")
			}
		})
	}
}

// Test DownloadRuntimeWithAudit
func TestManager_DownloadRuntimeWithAudit(t *testing.T) {
	registry := NewRegistry()

	platforms := []platform.Platform{
		{OS: "linux", Arch: "x64", FileExt: "tar.gz", Classifier: "linux-x64"},
	}

	tasks := []DownloadTask{
		{
			URL:        "http://example.com/test.tar.gz",
			OutputPath: "/tmp/test.tar.gz",
			Platform:   platforms[0],
			Runtime:    "python",
			Version:    "3.13",
			FileType:   "main",
		},
	}

	results := []DownloadResult{
		{
			Task:      &tasks[0],
			URL:       "http://example.com/test.tar.gz",
			LocalPath: "/tmp/test.tar.gz",
			FilePath:  "/tmp/test.tar.gz",
			Platform:  platforms[0],
			Runtime:   "python",
			Version:   "3.13",
			Success:   true,
			Size:      1024,
			FileSize:  1024,
			Duration:  time.Second,
			Error:     nil,
		},
	}

	provider := &mockRuntimeProvider{
		name: "python",
		versions: []endoflife.VersionInfo{
			createTestVersionInfo("3.13", true),
		},
		tasks:        tasks,
		results:      results,
		verification: &mockVerificationStrategy{strategyType: "mock", shouldError: false},
	}
	_ = registry.Register("python", provider)

	manager := NewManager(registry, nil, slog.Default(), slog.Default())

	versionInfo := createTestVersionInfo("3.13", true)

	// Test with nil audit logger
	downloadResults, err := manager.DownloadRuntimeWithAudit(
		context.Background(),
		"python",
		versionInfo,
		platforms,
		"/tmp",
		1,
		nil,
	)

	if err != nil {
		t.Errorf("Manager.DownloadRuntimeWithAudit() error = %v", err)
		return
	}

	if len(downloadResults) != 1 {
		t.Errorf("Manager.DownloadRuntimeWithAudit() got %d results, want 1", len(downloadResults))
	}

	// Test with audit logger
	auditLogger := slog.Default()
	downloadResults, err = manager.DownloadRuntimeWithAudit(
		context.Background(),
		"python",
		versionInfo,
		platforms,
		"/tmp",
		1,
		auditLogger,
	)

	if err != nil {
		t.Errorf("Manager.DownloadRuntimeWithAudit() with audit logger error = %v", err)
		return
	}

	if len(downloadResults) != 1 {
		t.Errorf("Manager.DownloadRuntimeWithAudit() with audit logger got %d results, want 1", len(downloadResults))
	}
}

// Test optional file downloads (404/403 handling)
func TestHTTPDownloader_OptionalFileHandling(t *testing.T) {
	// Create test server that returns 404 for optional files
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "optional") {
			http.Error(w, "Not Found", http.StatusNotFound)
		} else if strings.Contains(r.URL.Path, "forbidden") {
			http.Error(w, "Forbidden", http.StatusForbidden)
		} else {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("test content"))
		}
	}))
	defer server.Close()

	tempDir, err := os.MkdirTemp("", "runtime-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	downloader := NewConcurrentDownloader(1, 5*time.Second, slog.Default(), slog.Default())

	tests := []struct {
		name        string
		url         string
		optional    bool
		wantSuccess bool
		wantError   bool
	}{
		{
			name:        "optional file with 404",
			url:         server.URL + "/optional",
			optional:    true,
			wantSuccess: true,
			wantError:   false,
		},
		{
			name:        "optional file with 403",
			url:         server.URL + "/forbidden",
			optional:    true,
			wantSuccess: true,
			wantError:   false,
		},
		{
			name:        "required file with 404",
			url:         server.URL + "/optional",
			optional:    false,
			wantSuccess: false,
			wantError:   true,
		},
		{
			name:        "successful download",
			url:         server.URL + "/success",
			optional:    false,
			wantSuccess: true,
			wantError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := DownloadTask{
				URL:        tt.url,
				OutputPath: filepath.Join(tempDir, "test.txt"),
				Runtime:    "test",
				Version:    "1.0",
				Optional:   tt.optional,
				FileType:   "signature",
			}

			results, err := downloader.ProcessDownloads(context.Background(), []DownloadTask{task})
			if err != nil {
				t.Errorf("ProcessDownloads() error = %v", err)
				return
			}

			if len(results) != 1 {
				t.Errorf("Expected 1 result, got %d", len(results))
				return
			}

			result := results[0]
			if result.Success != tt.wantSuccess {
				t.Errorf("Result.Success = %v, want %v", result.Success, tt.wantSuccess)
			}

			if (result.Error != nil) != tt.wantError {
				t.Errorf("Result.Error = %v, wantError %v", result.Error, tt.wantError)
			}
		})
	}
}

// Test custom headers in download
func TestHTTPDownloader_CustomHeaders(t *testing.T) {
	// Create test server that checks for custom headers
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check for custom header
		if r.Header.Get("X-Custom-Header") == "test-value" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("success"))
		} else {
			http.Error(w, "Missing header", http.StatusBadRequest)
		}
	}))
	defer server.Close()

	tempDir, err := os.MkdirTemp("", "runtime-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	downloader := NewConcurrentDownloader(1, 5*time.Second, slog.Default(), slog.Default())

	task := DownloadTask{
		URL:        server.URL,
		OutputPath: filepath.Join(tempDir, "test.txt"),
		Runtime:    "test",
		Version:    "1.0",
		Headers: map[string]string{
			"X-Custom-Header": "test-value",
		},
	}

	results, err := downloader.ProcessDownloads(context.Background(), []DownloadTask{task})
	if err != nil {
		t.Errorf("ProcessDownloads() error = %v", err)
		return
	}

	if len(results) != 1 {
		t.Errorf("Expected 1 result, got %d", len(results))
		return
	}

	if results[0].Error != nil {
		t.Errorf("Download with custom headers failed: %v", results[0].Error)
	}
}

// Test verification failures in DownloadRuntime
func TestManager_DownloadRuntime_VerificationFailure(t *testing.T) {
	registry := NewRegistry()

	platforms := []platform.Platform{
		{OS: "linux", Arch: "x64", FileExt: "tar.gz", Classifier: "linux-x64"},
	}

	tasks := []DownloadTask{
		{
			URL:        "http://example.com/test.tar.gz",
			OutputPath: "/tmp/test.tar.gz",
			Platform:   platforms[0],
			Runtime:    "python",
			Version:    "3.13",
			FileType:   "main",
		},
	}

	results := []DownloadResult{
		{
			Task:      &tasks[0],
			URL:       "http://example.com/test.tar.gz",
			LocalPath: "/tmp/test.tar.gz",
			FilePath:  "/tmp/test.tar.gz",
			Platform:  platforms[0],
			Runtime:   "python",
			Version:   "3.13",
			Success:   true,
			Size:      1024,
			FileSize:  1024,
			Duration:  time.Second,
			Error:     nil,
		},
	}

	// Provider with verification strategy that fails
	provider := &mockRuntimeProvider{
		name: "python",
		versions: []endoflife.VersionInfo{
			createTestVersionInfo("3.13", true),
		},
		tasks:        tasks,
		results:      results,
		verification: &mockVerificationStrategy{strategyType: "mock", shouldError: true},
	}
	_ = registry.Register("python", provider)

	manager := NewManager(registry, nil, slog.Default(), slog.Default())
	versionInfo := createTestVersionInfo("3.13", true)

	downloadResults, err := manager.DownloadRuntime(
		context.Background(),
		"python",
		versionInfo,
		platforms,
		"/tmp",
		1,
	)

	if err != nil {
		t.Errorf("Manager.DownloadRuntime() error = %v", err)
		return
	}

	// Verification failure should mark result as failed
	if len(downloadResults) != 1 {
		t.Errorf("Expected 1 result, got %d", len(downloadResults))
		return
	}

	if downloadResults[0].Success {
		t.Error("Result should be marked as failed after verification failure")
	}

	if downloadResults[0].Error == nil {
		t.Error("Result should have error after verification failure")
	}
}

// Test no verification strategy in DownloadRuntime
func TestManager_DownloadRuntime_NoVerification(t *testing.T) {
	registry := NewRegistry()

	platforms := []platform.Platform{
		{OS: "linux", Arch: "x64", FileExt: "tar.gz", Classifier: "linux-x64"},
	}

	tasks := []DownloadTask{
		{
			URL:        "http://example.com/test.tar.gz",
			OutputPath: "/tmp/test.tar.gz",
			Platform:   platforms[0],
			Runtime:    "python",
			Version:    "3.13",
			FileType:   "main",
		},
	}

	results := []DownloadResult{
		{
			Task:      &tasks[0],
			URL:       "http://example.com/test.tar.gz",
			LocalPath: "/tmp/test.tar.gz",
			FilePath:  "/tmp/test.tar.gz",
			Platform:  platforms[0],
			Runtime:   "python",
			Version:   "3.13",
			Success:   true,
			Size:      1024,
			FileSize:  1024,
			Duration:  time.Second,
			Error:     nil,
		},
	}

	// Provider with nil verification strategy
	provider := &mockRuntimeProvider{
		name: "python",
		versions: []endoflife.VersionInfo{
			createTestVersionInfo("3.13", true),
		},
		tasks:        tasks,
		results:      results,
		verification: nil, // No verification
	}
	_ = registry.Register("python", provider)

	manager := NewManager(registry, nil, slog.Default(), slog.Default())
	versionInfo := createTestVersionInfo("3.13", true)

	downloadResults, err := manager.DownloadRuntime(
		context.Background(),
		"python",
		versionInfo,
		platforms,
		"/tmp",
		1,
	)

	if err != nil {
		t.Errorf("Manager.DownloadRuntime() error = %v", err)
		return
	}

	if len(downloadResults) != 1 {
		t.Errorf("Expected 1 result, got %d", len(downloadResults))
		return
	}

	// Without verification, result should still be successful
	if !downloadResults[0].Success {
		t.Error("Result should be successful without verification")
	}
}
