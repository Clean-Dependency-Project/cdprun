package temurin

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/clean-dependency-project/cdprun/internal/endoflife"
	"github.com/clean-dependency-project/cdprun/internal/gpg"
	"github.com/clean-dependency-project/cdprun/internal/platform"
	"github.com/clean-dependency-project/cdprun/internal/runtime"
)

// mockEndOfLifeClient implements endoflife.Client for testing
type mockEndOfLifeClient struct {
	productInfo *endoflife.ProductInfo
	shouldError bool
}

func (m *mockEndOfLifeClient) GetProductInfo(ctx context.Context, product string) (*endoflife.ProductInfo, error) {
	if m.shouldError {
		return nil, fmt.Errorf("mock error")
	}
	return m.productInfo, nil
}

func (m *mockEndOfLifeClient) GetSupportedVersions(ctx context.Context, runtime endoflife.PolicyRuntime) ([]endoflife.VersionInfo, error) {
	if m.shouldError {
		return nil, fmt.Errorf("mock error")
	}
	return []endoflife.VersionInfo{}, nil
}

func (m *mockEndOfLifeClient) ValidatePolicy(policy *endoflife.Policy) error {
	if m.shouldError {
		return fmt.Errorf("mock error")
	}
	return nil
}

func (m *mockEndOfLifeClient) EnrichVersionInfo(ctx context.Context, runtime endoflife.PolicyRuntime, policyVersion endoflife.PolicyVersion) (*endoflife.VersionInfo, error) {
	if m.shouldError {
		return nil, fmt.Errorf("mock error")
	}
	return &endoflife.VersionInfo{
		Version:     policyVersion.Version,
		IsSupported: policyVersion.Supported,
	}, nil
}

func (m *mockEndOfLifeClient) GetMaintainedReleases(ctx context.Context, product string) ([]endoflife.VersionInfo, error) {
	if m.shouldError {
		return nil, fmt.Errorf("mock error")
	}
	return []endoflife.VersionInfo{}, nil
}

func createTestTemurinPolicyFile(t *testing.T) string {
	tempDir := t.TempDir()
	policyPath := filepath.Join(tempDir, "temurin-policy.json")

	// Create a policy file in the format expected by the Temurin LoadPolicy method
	policyContent := `[
		{
			"version": "21",
			"supported": true,
			"recommended": true,
			"lts": true,
			"latest_patch_version": "21.0.1",
			"under_review": false
		},
		{
			"version": "17",
			"supported": true,
			"recommended": false,
			"lts": true,
			"latest_patch_version": "17.0.9",
			"under_review": false
		},
		{
			"version": "11",
			"supported": true,
			"recommended": false,
			"lts": true,
			"latest_patch_version": "11.0.21",
			"under_review": false
		}
	]`

	err := os.WriteFile(policyPath, []byte(policyContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test policy file: %v", err)
	}

	return policyPath
}

func TestNewAdapter(t *testing.T) {
	client := &mockEndOfLifeClient{}
	adapter := NewAdapter(client)

	if adapter == nil {
		t.Error("NewAdapter returned nil")
	}

	temurinAdapter, ok := adapter.(*TemurinAdapter)
	if !ok {
		t.Error("NewAdapter did not return a TemurinAdapter")
	}

	if temurinAdapter.endoflifeClient != client {
		t.Error("NewAdapter did not set endoflife client correctly")
	}
}

func TestTemurinAdapter_GetName(t *testing.T) {
	adapter := NewAdapter(&mockEndOfLifeClient{})
	name := adapter.GetName()

	if name != "temurin" {
		t.Errorf("GetName() = %s, want temurin", name)
	}
}

func TestTemurinAdapter_GetEndOfLifeProduct(t *testing.T) {
	adapter := NewAdapter(&mockEndOfLifeClient{})
	product := adapter.GetEndOfLifeProduct()

	if product != "temurin" {
		t.Errorf("GetEndOfLifeProduct() = %s, want temurin", product)
	}
}

func TestTemurinAdapter_GetSupportedPlatforms(t *testing.T) {
	adapter := NewAdapter(&mockEndOfLifeClient{})
	platforms := adapter.GetSupportedPlatforms()

	if len(platforms) == 0 {
		t.Error("GetSupportedPlatforms() returned empty list")
	}

	// Check that we have expected platforms (OS/Arch combinations)
	hasWindows := false
	hasLinux := false
	hasMac := false

	for _, p := range platforms {
		switch p.OS {
		case "windows":
			hasWindows = true
		case "linux":
			hasLinux = true
		case "mac": // Temurin uses "mac" not "darwin"
			hasMac = true
		}
	}

	if !hasWindows || !hasLinux || !hasMac {
		t.Errorf("GetSupportedPlatforms() missing expected platforms. Got: %v", platforms)
	}
}

func TestTemurinAdapter_ListVersions(t *testing.T) {
	// Create a mock client with properly structured test data
	mockClient := &mockEndOfLifeClient{
		productInfo: &endoflife.ProductInfo{
			SchemaVersion: "1.0",
			Result: struct {
				Name           string                 `json:"name"`
				Aliases        []string               `json:"aliases"`
				Label          string                 `json:"label"`
				Category       string                 `json:"category"`
				Tags           []string               `json:"tags"`
				VersionCommand string                 `json:"versionCommand,omitempty"`
				Identifiers    []endoflife.Identifier `json:"identifiers,omitempty"`
				Labels         endoflife.Labels       `json:"labels,omitempty"`
				Links          endoflife.Links        `json:"links,omitempty"`
				Releases       []endoflife.Release    `json:"releases"`
			}{
				Name:     "temurin",
				Label:    "Temurin",
				Category: "runtime",
				Releases: []endoflife.Release{
					{
						Name:         "21",
						ReleaseDate:  "2023-09-19",
						IsLTS:        true,
						IsMaintained: true,
						IsEOL:        false,
						Latest: struct {
							Name string `json:"name"`
							Date string `json:"date"`
							Link string `json:"link"`
						}{
							Name: "21.0.7+6",
							Date: "2024-01-16",
						},
					},
					{
						Name:         "17",
						ReleaseDate:  "2021-09-14",
						IsLTS:        true,
						IsMaintained: true,
						IsEOL:        false,
						Latest: struct {
							Name string `json:"name"`
							Date string `json:"date"`
							Link string `json:"link"`
						}{
							Name: "17.0.9+9",
							Date: "2024-01-16",
						},
					},
				},
			},
		},
	}

	adapter := NewAdapter(mockClient)

	versions, err := adapter.ListVersions(context.Background())
	if err != nil {
		t.Errorf("ListVersions() error = %v", err)
		return
	}

	if len(versions) == 0 {
		t.Error("ListVersions() returned empty list")
		return
	}

	// Check that versions are properly structured
	for _, v := range versions {
		if v.RuntimeName != "temurin" {
			t.Errorf("Version %s has runtime name %s, want temurin", v.Version, v.RuntimeName)
		}

		// Version should not be empty
		if v.Version == "" {
			t.Error("Version has empty version string")
		}
	}

	// Verify we got the expected versions
	expectedVersions := map[string]bool{
		"21": false,
		"17": false,
	}

	for _, v := range versions {
		if _, exists := expectedVersions[v.Version]; exists {
			expectedVersions[v.Version] = true
		}
	}

	// Check that all expected versions were found
	for version, found := range expectedVersions {
		if !found {
			t.Errorf("Expected version %s not found in results", version)
		}
	}

	t.Logf("Found %d Temurin versions", len(versions))
}

func TestTemurinAdapter_GetLatestVersion(t *testing.T) {
	// Create a mock client with test data
	mockClient := &mockEndOfLifeClient{
		productInfo: &endoflife.ProductInfo{
			SchemaVersion: "1.0",
			Result: struct {
				Name           string                 `json:"name"`
				Aliases        []string               `json:"aliases"`
				Label          string                 `json:"label"`
				Category       string                 `json:"category"`
				Tags           []string               `json:"tags"`
				VersionCommand string                 `json:"versionCommand,omitempty"`
				Identifiers    []endoflife.Identifier `json:"identifiers,omitempty"`
				Labels         endoflife.Labels       `json:"labels,omitempty"`
				Links          endoflife.Links        `json:"links,omitempty"`
				Releases       []endoflife.Release    `json:"releases"`
			}{
				Name:     "temurin",
				Label:    "Temurin",
				Category: "runtime",
				Releases: []endoflife.Release{
					{
						Name:         "21",
						ReleaseDate:  "2023-09-19",
						IsLTS:        true,
						IsMaintained: true,
						IsEOL:        false,
						Latest: struct {
							Name string `json:"name"`
							Date string `json:"date"`
							Link string `json:"link"`
						}{
							Name: "21.0.7+6",
							Date: "2024-01-16",
						},
					},
					{
						Name:         "17",
						ReleaseDate:  "2021-09-14",
						IsLTS:        true,
						IsMaintained: true,
						IsEOL:        false,
						Latest: struct {
							Name string `json:"name"`
							Date string `json:"date"`
							Link string `json:"link"`
						}{
							Name: "17.0.9+9",
							Date: "2024-01-16",
						},
					},
				},
			},
		},
	}

	adapter := NewAdapter(mockClient)

	tests := []struct {
		name    string
		opts    runtime.VersionOptions
		wantErr bool
	}{
		{
			name:    "default options - should return LTS",
			opts:    runtime.VersionOptions{},
			wantErr: false,
		},
		{
			name:    "latest only",
			opts:    runtime.VersionOptions{Latest: true},
			wantErr: false,
		},
		{
			name:    "LTS only",
			opts:    runtime.VersionOptions{LTSOnly: true},
			wantErr: false,
		},
		{
			name:    "recommended only",
			opts:    runtime.VersionOptions{RecommendedOnly: true},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			version, err := adapter.GetLatestVersion(context.Background(), tt.opts)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetLatestVersion() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if version.Version == "" {
					t.Error("GetLatestVersion() returned empty version")
				}
				if version.RuntimeName != "temurin" {
					t.Errorf("GetLatestVersion() runtime name = %s, want temurin", version.RuntimeName)
				}
				// For LTS tests, verify we got an LTS version
				if tt.opts.LTSOnly && !version.IsLTS {
					t.Error("GetLatestVersion() with LTSOnly returned non-LTS version")
				}
			}
		})
	}
}

func TestTemurinAdapter_LoadPolicy(t *testing.T) {
	adapter := NewAdapter(&mockEndOfLifeClient{})
	policyPath := createTestTemurinPolicyFile(t)

	policy, err := adapter.LoadPolicy(policyPath)
	if err != nil {
		t.Errorf("LoadPolicy() error = %v", err)
		return
	}

	if len(policy) == 0 {
		t.Error("LoadPolicy() returned empty policy")
	}

	// Check that we have expected versions
	hasV21 := false
	hasV17 := false
	hasV11 := false

	for _, pv := range policy {
		switch pv.Version {
		case "21":
			hasV21 = true
			if !pv.Supported {
				t.Error("Temurin 21 should be supported in test policy")
			}
			if !pv.LTS {
				t.Error("Temurin 21 should be LTS in test policy")
			}
		case "17":
			hasV17 = true
			if !pv.Supported {
				t.Error("Temurin 17 should be supported in test policy")
			}
			if !pv.LTS {
				t.Error("Temurin 17 should be LTS in test policy")
			}
		case "11":
			hasV11 = true
			if !pv.Supported {
				t.Error("Temurin 11 should be supported in test policy")
			}
			if !pv.LTS {
				t.Error("Temurin 11 should be LTS in test policy")
			}
		}
	}

	if !hasV21 || !hasV17 || !hasV11 {
		t.Errorf("LoadPolicy() missing expected versions. Got: %v", policy)
	}
}

func TestTemurinAdapter_LoadPolicy_NonExistentFile(t *testing.T) {
	adapter := NewAdapter(&mockEndOfLifeClient{})

	_, err := adapter.LoadPolicy("/nonexistent/path/policy.json")
	if err == nil {
		t.Error("LoadPolicy() should have failed for non-existent file")
	}
}

func TestTemurinAdapter_ApplyPolicy(t *testing.T) {
	adapter := NewAdapter(&mockEndOfLifeClient{})

	versions := []endoflife.VersionInfo{
		{
			Version:     "21.0.7+6-LTS",
			IsSupported: true, // Set as supported to pass validation
			IsLTS:       true,
		},
		{
			Version:     "17.0.15+6-LTS",
			IsSupported: true, // Set as supported to pass validation
			IsLTS:       true,
		},
		{
			Version:     "16.0.2+7",
			IsSupported: false, // Not in policy, should be filtered out
		},
	}

	policy := []endoflife.PolicyVersion{
		{
			Version:   "21.*", // Use pattern matching
			Supported: true,
			LTS:       true,
		},
		{
			Version:   "17.*", // Use pattern matching
			Supported: true,
			LTS:       true,
		},
	}

	filtered, err := adapter.ApplyPolicy(versions, policy)
	if err != nil {
		t.Errorf("ApplyPolicy() error = %v", err)
		return
	}

	// Should have the two supported versions
	if len(filtered) < 2 {
		t.Errorf("ApplyPolicy() returned %d versions, want at least 2", len(filtered))
	}
}

func TestTemurinAdapter_CreateDownloadTasks(t *testing.T) {
	// Skip this test in short mode as it makes real API calls
	if testing.Short() {
		t.Skip("Skipping TestTemurinAdapter_CreateDownloadTasks in short mode")
	}

	adapter := NewAdapter(&mockEndOfLifeClient{})

	version := endoflife.VersionInfo{
		Version:     "21.0.1+12-LTS", // Use a real version format
		LatestPatch: "21.0.1+12-LTS",
		RuntimeName: "temurin",
		IsSupported: true, // Make sure it's supported to pass validation
	}

	// Use Linux arch pairs Adoptium consistently ships for this LTS build. Windows
	// (and other) installers are not guaranteed for every patch; the adapter skips
	// missing platforms, which made the old linux+windows expectation flaky.
	platforms := []platform.Platform{
		{OS: "linux", Arch: "x64", FileExt: "tar.gz", Classifier: "linux-x64"},
		{OS: "linux", Arch: "aarch64", FileExt: "tar.gz", Classifier: "linux-aarch64"},
	}

	outputDir := "/tmp/test"

	tasks, err := adapter.CreateDownloadTasks(version, platforms, outputDir)
	if err != nil {
		t.Errorf("CreateDownloadTasks() error = %v", err)
		return
	}

	// Each platform should generate 3 tasks: main, checksum, signature
	expectedTaskCount := len(platforms) * 3
	if len(tasks) != expectedTaskCount {
		t.Errorf("CreateDownloadTasks() returned %d tasks, want %d", len(tasks), expectedTaskCount)
		return
	}

	// Group tasks by platform and verify types
	tasksByPlatform := make(map[string][]runtime.DownloadTask)
	for _, task := range tasks {
		key := task.Platform.Classifier
		tasksByPlatform[key] = append(tasksByPlatform[key], task)
	}

	for platformKey, platformTasks := range tasksByPlatform {
		if len(platformTasks) != 3 {
			t.Errorf("Platform %s has %d tasks, want 3", platformKey, len(platformTasks))
			continue
		}

		// Check that we have all three file types
		hasMain := false
		hasChecksum := false
		hasSignature := false

		for _, task := range platformTasks {
			if task.Runtime != "temurin" {
				t.Errorf("Task runtime = %s, want temurin", task.Runtime)
			}

			switch task.FileType {
			case "main":
				hasMain = true
			case "checksum":
				hasChecksum = true
				if !strings.HasSuffix(task.URL, ".sha256.txt") {
					t.Errorf("Checksum task URL should end with .sha256.txt, got: %s", task.URL)
				}
			case "signature":
				hasSignature = true
				if !strings.HasSuffix(task.URL, ".sig") {
					t.Errorf("Signature task URL should end with .sig, got: %s", task.URL)
				}
			}

			// Check User-Agent header
			userAgent, exists := task.Headers["User-Agent"]
			if !exists {
				t.Errorf("Task missing User-Agent header")
			} else if userAgent != "downloadruntime/1.0 (temurin)" {
				t.Errorf("Task User-Agent = %s, want downloadruntime/1.0 (temurin)", userAgent)
			}
		}

		if !hasMain || !hasChecksum || !hasSignature {
			t.Errorf("Platform %s missing expected task types. main=%t, checksum=%t, signature=%t",
				platformKey, hasMain, hasChecksum, hasSignature)
		}
	}
}

func TestTemurinAdapter_ProcessDownloads(t *testing.T) {
	// Skip this test in short mode as it's time-consuming
	if testing.Short() {
		t.Skip("Skipping TestTemurinAdapter_ProcessDownloads in short mode")
	}

	adapter := NewAdapter(&mockEndOfLifeClient{})

	// Create test tasks that won't actually download (invalid URLs)
	tasks := []runtime.DownloadTask{
		{
			URL:        "http://example.com/test.tar.gz",
			OutputPath: "/tmp/test.tar.gz",
			Runtime:    "temurin",
			Version:    "21",
			FileType:   "main",
		},
	}

	// Test with different concurrency values
	tests := []struct {
		name        string
		concurrency int
		wantErr     bool
	}{
		{
			name:        "default concurrency",
			concurrency: 0, // Should default to 5
			wantErr:     false,
		},
		{
			name:        "custom concurrency",
			concurrency: 2,
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := adapter.ProcessDownloads(context.Background(), tasks, tt.concurrency)
			if (err != nil) != tt.wantErr {
				t.Errorf("ProcessDownloads() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if len(results) != len(tasks) {
				t.Errorf("ProcessDownloads() returned %d results, want %d", len(results), len(tasks))
			}
		})
	}
}

func TestTemurinAdapter_GetVerificationStrategy(t *testing.T) {
	adapter := NewAdapter(&mockEndOfLifeClient{})
	strategy := adapter.GetVerificationStrategy()

	if strategy == nil {
		t.Error("GetVerificationStrategy() returned nil")
		return
	}

	expectedType := "temurin-checksum-sha256"
	if strategy.GetType() != expectedType {
		t.Errorf("Verification strategy type = %s, want %s", strategy.GetType(), expectedType)
	}
}

func TestTemurinAdapter_GetAdoptiumAPI(t *testing.T) {
	// Skip this test if not in integration mode since it requires real API calls
	if testing.Short() {
		t.Skip("Skipping Adoptium API test in short mode")
	}

	adapter := NewAdapter(&mockEndOfLifeClient{}).(*TemurinAdapter)

	tests := []struct {
		name    string
		version string
		wantErr bool
	}{
		{
			name:    "Valid LTS version",
			version: "21.0.1+12-LTS",
			wantErr: false,
		},
		{
			name:    "Valid non-LTS version",
			version: "24.0.1+9.1",
			wantErr: false,
		},
		{
			name:    "Invalid version",
			version: "invalid-version",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			release, err := adapter.getAdoptiumRelease(tt.version)
			if (err != nil) != tt.wantErr {
				t.Errorf("getAdoptiumRelease() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if release == nil {
					t.Error("getAdoptiumRelease() returned nil release for valid version")
				} else {
					if len(release.Binaries) == 0 {
						t.Error("getAdoptiumRelease() returned release with no binaries")
					}
					t.Logf("Found %d binaries for version %s", len(release.Binaries), tt.version)
				}
			}
		})
	}
}

func TestTemurinAdapter_Integration(t *testing.T) {
	// Skip integration test if not in integration mode
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	adapter := NewAdapter(endoflife.NewClient(endoflife.DefaultConfig()))

	// Test the complete flow
	ctx := context.Background()

	// List versions
	versions, err := adapter.ListVersions(ctx)
	if err != nil {
		t.Errorf("Integration test ListVersions() failed: %v", err)
		return
	}

	if len(versions) == 0 {
		t.Error("Integration test: no versions returned")
		return
	}

	// Verify version structure
	firstVersion := versions[0]
	if firstVersion.Version == "" {
		t.Error("Integration test: first version has empty version string")
	}

	if firstVersion.RuntimeName != "temurin" {
		t.Errorf("Integration test: version runtime name = %s, want temurin", firstVersion.RuntimeName)
	}

	// Check that we have LTS versions
	hasLTS := false
	for _, v := range versions {
		if v.IsLTS {
			hasLTS = true
			break
		}
	}

	if !hasLTS {
		t.Error("Integration test: no LTS versions found")
	}

	// Verify that common JDK versions are present
	expectedMajorVersions := []string{"21", "17", "11", "8"}
	foundVersions := make(map[string]bool)

	for _, v := range versions {
		foundVersions[v.Version] = true
	}

	for _, expected := range expectedMajorVersions {
		if !foundVersions[expected] {
			t.Logf("Integration test: Expected version %s not found (may be normal)", expected)
		}
	}

	t.Logf("Integration test: Found %d Temurin versions, latest: %s", len(versions), firstVersion.Version)
}

func TestTemurinAdapter_LoadTemurinGPGKeys(t *testing.T) {
	adapter := NewAdapter(&mockEndOfLifeClient{}).(*TemurinAdapter)

	keyRing, err := adapter.LoadTemurinGPGKeys()
	if err != nil {
		t.Fatalf("LoadTemurinGPGKeys() error = %v", err)
	}

	if keyRing == nil {
		t.Error("LoadTemurinGPGKeys() returned nil keyring")
	}

	// Test that the keyring has some basic functionality
	err = keyRing.VerifyDetached([]byte("test message"), []byte("test signature"))
	if err == nil {
		t.Log("Keyring verification test passed (unexpected but possible)")
	} else {
		// We expect this to fail since we're using mock data, but the keyring should be functional
		if strings.Contains(err.Error(), "no keys available") {
			t.Error("Keyring reports no keys available - key loading failed")
		} else {
			t.Logf("Keyring verification failed as expected: %v", err)
		}
	}
}

func TestTemurinVerificationStrategy(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	strategy := NewTemurinVerificationStrategy(logger, logger)
	if strategy == nil {
		t.Fatal("NewTemurinVerificationStrategy returned nil")
	}

	if strategy.GetType() != "temurin-checksum-sha256" {
		t.Errorf("Expected strategy type 'temurin-checksum-gpg', got %s", strategy.GetType())
	}

	if !strategy.RequiresAdditionalFiles() {
		t.Error("Expected strategy to require additional files")
	}

	t.Logf("Verification strategy: %s, requires additional files: %t",
		strategy.GetType(), strategy.RequiresAdditionalFiles())
}

func TestTemurinAdapter_VerifySignature(t *testing.T) {
	adapter := NewAdapter(&mockEndOfLifeClient{}).(*TemurinAdapter)

	// Create test files
	tempDir := t.TempDir()
	dataFile := filepath.Join(tempDir, "test-data.txt")
	sigFile := filepath.Join(tempDir, "test-data.txt.sig")

	// Create a test data file
	testData := "Test content for Temurin GPG verification"
	err := os.WriteFile(dataFile, []byte(testData), 0644)
	if err != nil {
		t.Fatalf("Failed to create test data file: %v", err)
	}

	// Create a mock signature file (this will fail verification, but tests the loading)
	mockSig := "-----BEGIN PGP SIGNATURE-----\nMock signature for testing\n-----END PGP SIGNATURE-----"
	err = os.WriteFile(sigFile, []byte(mockSig), 0644)
	if err != nil {
		t.Fatalf("Failed to create test signature file: %v", err)
	}

	// Test verification (should fail due to mock signature, but should not error on key loading)
	err = adapter.VerifySignature(dataFile, sigFile)
	if err == nil {
		t.Log("Signature verification unexpectedly passed")
	} else {
		// We expect this to fail with a verification error, not a key loading error
		if strings.Contains(err.Error(), "failed to load") {
			t.Errorf("Key loading failed: %v", err)
		} else {
			t.Logf("Signature verification failed as expected: %v", err)
		}
	}
}

func TestTemurinGPGIntegration(t *testing.T) {
	// Test that embedded keys can be loaded and GPG verification system is functional
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	// Load keys directly from embedded filesystem
	keyRing, err := gpg.LoadKeyRingFromEmbedFS(embeddedTemurinKeys, "keys")
	if err != nil {
		t.Fatalf("Failed to load Temurin GPG keys from embedded filesystem: %v", err)
	}

	if keyRing == nil {
		t.Fatal("Keyring is nil")
	}

	logger.Info("Successfully loaded Temurin GPG keys for testing")
	t.Log("Temurin GPG key loading integration test passed")
}
