package nodejs

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crypto/sha256"

	"github.com/clean-dependency-project/cdprun/internal/config"
	"github.com/clean-dependency-project/cdprun/internal/endoflife"
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

	// If no productInfo is set, return a properly structured mock response
	if m.productInfo == nil {
		return &endoflife.ProductInfo{
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
				Name:     "nodejs",
				Label:    "Node.js",
				Category: "runtime",
				Releases: []endoflife.Release{}, // Empty releases array
			},
		}, nil
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

func createTestNodeJSPolicyFile(t *testing.T) string {
	tempDir := t.TempDir()
	policyPath := filepath.Join(tempDir, "nodejs-policy.json")

	// Create a simple policy file content for Node.js (major versions)
	policyContent := `[
		{
			"version": "20",
			"supported": true,
			"recommended": true,
			"lts": true,
			"latest_patch_version": "20.15.0"
		},
		{
			"version": "18",
			"supported": true,
			"recommended": false,
			"lts": true,
			"latest_patch_version": "18.20.4"
		}
	]`

	err := os.WriteFile(policyPath, []byte(policyContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test policy file: %v", err)
	}

	return policyPath
}

func createTestPolicyFileFromVersions(t *testing.T, policyVersions []endoflife.PolicyVersion) string {
	tempDir := t.TempDir()
	policyPath := filepath.Join(tempDir, "nodejs-policy.json")

	// Create a policy file content for Node.js based on the provided versions
	policyContent := "["
	for i, pv := range policyVersions {
		latestPatch := pv.LatestPatchVersion
		if latestPatch == "" {
			latestPatch = pv.Version + ".15.0" // Default patch version
		}

		policyContent += fmt.Sprintf(`{
			"version": "%s",
			"supported": %t,
			"recommended": %t,
			"lts": %t,
			"latest_patch_version": "%s"
		}`, pv.Version, pv.Supported, pv.Recommended, pv.LTS, latestPatch)

		if i < len(policyVersions)-1 {
			policyContent += ","
		}
	}
	policyContent += "]"

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

	nodejsAdapter, ok := adapter.(*NodeJSAdapter)
	if !ok {
		t.Error("NewAdapter did not return a NodeJSAdapter")
	}

	if nodejsAdapter.endoflifeClient != client {
		t.Error("NewAdapter did not set endoflife client correctly")
	}
}

func TestNodeJSAdapter_GetName(t *testing.T) {
	adapter := NewAdapter(&mockEndOfLifeClient{})
	name := adapter.GetName()

	if name != "nodejs" {
		t.Errorf("GetName() = %s, want nodejs", name)
	}
}

func TestNodeJSAdapter_GetEndOfLifeProduct(t *testing.T) {
	adapter := NewAdapter(&mockEndOfLifeClient{})
	product := adapter.GetEndOfLifeProduct()

	if product != "nodejs" {
		t.Errorf("GetEndOfLifeProduct() = %s, want nodejs", product)
	}
}

func TestNodeJSAdapter_GetSupportedPlatforms(t *testing.T) {
	adapter := NewAdapter(&mockEndOfLifeClient{})
	platforms := adapter.GetSupportedPlatforms()

	if len(platforms) == 0 {
		t.Error("GetSupportedPlatforms() returned empty list")
	}

	// Check that we have expected platforms
	hasWindows := false
	hasLinux := false
	hasMac := false

	for _, p := range platforms {
		switch p.OS {
		case "windows":
			hasWindows = true
		case "linux":
			hasLinux = true
		case "mac":
			hasMac = true
		}
	}

	if !hasWindows || !hasLinux || !hasMac {
		t.Errorf("GetSupportedPlatforms() missing expected platforms. Got: %v", platforms)
	}

	// Check for both x64 and aarch64 architectures
	hasX64 := false
	hasAarch64 := false

	for _, p := range platforms {
		if p.Arch == "x64" {
			hasX64 = true
		}
		if p.Arch == "aarch64" {
			hasAarch64 = true
		}
	}

	if !hasX64 || !hasAarch64 {
		t.Errorf("GetSupportedPlatforms() missing expected architectures. Got: %v", platforms)
	}
}

func TestNodeJSAdapter_GetLatestVersion(t *testing.T) {
	adapter := NewAdapter(&mockEndOfLifeClient{})

	tests := []struct {
		name    string
		opts    runtime.VersionOptions
		wantErr bool
	}{
		{
			name:    "default options",
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
			wantErr: false, // Node.js has LTS versions
		},
		{
			name:    "recommended only",
			opts:    runtime.VersionOptions{RecommendedOnly: true},
			wantErr: true, // No recommended versions initially
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			version, err := adapter.GetLatestVersion(context.Background(), tt.opts)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetLatestVersion() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && version.Version == "" {
				t.Error("GetLatestVersion() returned empty version")
			}
		})
	}
}

func TestNodeJSAdapter_LoadPolicy(t *testing.T) {
	adapter := NewAdapter(&mockEndOfLifeClient{})
	policyPath := createTestNodeJSPolicyFile(t)

	policy, err := adapter.LoadPolicy(policyPath)
	if err != nil {
		t.Errorf("LoadPolicy() error = %v", err)
		return
	}

	if len(policy) == 0 {
		t.Error("LoadPolicy() returned empty policy")
	}

	// Check that we have expected versions
	hasV20 := false
	hasV18 := false

	for _, pv := range policy {
		switch pv.Version {
		case "20":
			hasV20 = true
			if !pv.Supported {
				t.Error("Node.js 20 should be supported in test policy")
			}
			if !pv.LTS {
				t.Error("Node.js 20 should be LTS in test policy")
			}
		case "18":
			hasV18 = true
			if !pv.Supported {
				t.Error("Node.js 18 should be supported in test policy")
			}
			if !pv.LTS {
				t.Error("Node.js 18 should be LTS in test policy")
			}
		}
	}

	if !hasV20 || !hasV18 {
		t.Errorf("LoadPolicy() missing expected versions. Got: %v", policy)
	}
}

func TestNodeJSAdapter_LoadPolicy_NonExistentFile(t *testing.T) {
	adapter := NewAdapter(&mockEndOfLifeClient{})

	_, err := adapter.LoadPolicy("/nonexistent/path/policy.json")
	if err == nil {
		t.Error("LoadPolicy() should have failed for non-existent file")
	}
}

func TestNodeJSAdapter_ApplyPolicy(t *testing.T) {
	adapter := NewAdapter(&mockEndOfLifeClient{})

	versions := []endoflife.VersionInfo{
		{
			Version:     "20",
			IsSupported: false, // Will be updated by policy
			IsLTS:       false, // Will be updated by policy
		},
		{
			Version:     "18",
			IsSupported: false, // Will be updated by policy
			IsLTS:       false, // Will be updated by policy
		},
		{
			Version:     "16",
			IsSupported: false, // Not in policy, should be filtered out
		},
	}

	policy := []endoflife.PolicyVersion{
		{
			Version:   "20",
			Supported: true,
			LTS:       true,
		},
		{
			Version:   "18",
			Supported: true,
			LTS:       true,
		},
	}

	filtered, err := adapter.ApplyPolicy(versions, policy)
	if err != nil {
		t.Errorf("ApplyPolicy() error = %v", err)
		return
	}

	if len(filtered) != 2 {
		t.Errorf("ApplyPolicy() returned %d versions, want 2", len(filtered))
	}

	// Check that all returned versions are supported and LTS
	for _, v := range filtered {
		if !v.IsSupported {
			t.Errorf("Version %s should be supported after applying policy", v.Version)
		}
		if !v.IsLTS {
			t.Errorf("Version %s should be LTS after applying policy", v.Version)
		}
	}
}

func TestNodeJSAdapter_CreateDownloadTasks(t *testing.T) {
	adapter := NewAdapter(&mockEndOfLifeClient{})

	// Create a test policy file for Node.js version 20
	policyVersions := []endoflife.PolicyVersion{
		{Version: "20", Supported: true, Recommended: true},
	}
	policyPath := createTestPolicyFileFromVersions(t, policyVersions)

	// Set config with policy file
	nodejsAdapter := adapter.(*NodeJSAdapter)
	nodejsAdapter.SetConfig(&config.Runtime{
		PolicyFile: policyPath,
	})

	version := endoflife.VersionInfo{
		Version:     "20",
		LatestPatch: "20.15.0",
		RuntimeName: "nodejs",
	}

	platforms := []platform.Platform{
		{OS: "linux", Arch: "x64", FileExt: "tar.gz", Classifier: "linux-x64"},
		{OS: "windows", Arch: "x64", FileExt: "zip", Classifier: "windows-x64"},
	}

	outputDir := "/tmp/test"

	tasks, err := adapter.CreateDownloadTasks(version, platforms, outputDir)
	if err != nil {
		t.Errorf("CreateDownloadTasks() error = %v", err)
		return
	}

	// Should have main tasks + verification tasks (checksum + signature files)
	// 2 platforms * 1 main task + 2 verification tasks (SHASUMS256.txt, SHASUMS256.txt.asc, SHASUMS256.txt.sig)
	expectedMinTasks := len(platforms) // At least one task per platform
	if len(tasks) < expectedMinTasks {
		t.Errorf("CreateDownloadTasks() returned %d tasks, want at least %d", len(tasks), expectedMinTasks)
		return
	}

	// Count task types
	mainTasks := 0
	checksumTasks := 0
	signatureTasks := 0

	for _, task := range tasks {
		switch task.FileType {
		case "main":
			mainTasks++
		case "checksum":
			checksumTasks++
		case "signature":
			signatureTasks++
		}
	}

	// Should have one main task per platform
	if mainTasks != len(platforms) {
		t.Errorf("CreateDownloadTasks() returned %d main tasks, want %d", mainTasks, len(platforms))
	}

	// Verify main task properties (without brittle version checks)
	for _, task := range tasks {
		if task.FileType != "main" {
			continue // Skip verification tasks for this check
		}

		if task.Runtime != "nodejs" {
			t.Errorf("Task runtime = %s, want nodejs", task.Runtime)
		}

		// Don't check specific version as it changes - just verify it's not empty
		if task.Version == "" {
			t.Error("Task version is empty")
		}

		if task.URL == "" {
			t.Error("Task URL is empty")
		}

		if task.OutputPath == "" {
			t.Error("Task output path is empty")
		}

		// Check User-Agent header
		userAgent, exists := task.Headers["User-Agent"]
		if !exists {
			t.Error("Task missing User-Agent header")
		} else if userAgent != "cdprun/1.0 (Node.js)" {
			t.Errorf("Task User-Agent = %s, want cdprun/1.0 (Node.js)", userAgent)
		}

		// Verify URL has nodejs.org domain (without checking specific version)
		if !strings.Contains(task.URL, "https://nodejs.org/dist/") {
			t.Errorf("Task URL does not contain expected base pattern. Got: %s", task.URL)
		}
	}
}

func TestNodeJSAdapter_ProcessDownloads(t *testing.T) {
	adapter := NewAdapter(&mockEndOfLifeClient{})

	// Create test tasks that won't actually download (invalid URLs)
	tasks := []runtime.DownloadTask{
		{
			URL:        "http://example.com/test.tar.gz",
			OutputPath: "/tmp/test.tar.gz",
			Runtime:    "nodejs",
			Version:    "20",
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

func TestNodeJSAdapter_GetVerificationStrategy(t *testing.T) {
	adapter := NewAdapter(&mockEndOfLifeClient{})

	strategy := adapter.GetVerificationStrategy()

	if strategy == nil {
		t.Error("GetVerificationStrategy() returned nil")
		return
	}

	expectedType := "nodejs-checksum-gpg"
	if strategy.GetType() != expectedType {
		t.Errorf("Verification strategy type = %s, want %s", strategy.GetType(), expectedType)
	}
}

func TestNodeJSAdapter_ConstructDownloadURL(t *testing.T) {
	adapter := NewAdapter(&mockEndOfLifeClient{}).(*NodeJSAdapter)

	tests := []struct {
		name            string
		version         string
		platform        platform.Platform
		wantContains    string // Check if URL contains this substring instead of exact match
		wantEmpty       bool
	}{
		{
			name:    "Linux x64",
			version: "20.15.0",
			platform: platform.Platform{
				OS:      "linux",
				Arch:    "x64",
				FileExt: "tar.xz",
			},
			wantContains: "linux-x64.tar.xz",
		},
		{
			name:    "Windows x64",
			version: "20.15.0",
			platform: platform.Platform{
				OS:      "windows",
				Arch:    "x64",
				FileExt: "msi",
			},
			wantContains: "x64.msi",
		},
		{
			name:    "macOS ARM64",
			version: "20.15.0",
			platform: platform.Platform{
				OS:      "mac",
				Arch:    "aarch64",
				FileExt: "pkg",
			},
			wantContains: ".pkg",
		},
		{
			name:    "Unsupported platform",
			version: "20.15.0",
			platform: platform.Platform{
				OS:   "unsupported",
				Arch: "x64",
			},
			wantEmpty: true,
		},
		{
			name:    "Unsupported architecture",
			version: "20.15.0",
			platform: platform.Platform{
				OS:   "linux",
				Arch: "unsupported",
			},
			wantEmpty: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := adapter.constructDownloadURL(tt.version, tt.platform)
			if tt.wantEmpty {
				if got != "" {
					t.Errorf("constructDownloadURL() = %s, want empty string", got)
				}
			} else {
				if !strings.Contains(got, tt.wantContains) {
					t.Errorf("constructDownloadURL() = %s, want to contain %s", got, tt.wantContains)
				}
				if !strings.Contains(got, "https://nodejs.org/dist/") {
					t.Errorf("constructDownloadURL() = %s, should contain nodejs.org base URL", got)
				}
			}
		})
	}
}

func TestHelperFunctions(t *testing.T) {
	t.Run("isLTS", func(t *testing.T) {
		tests := []struct {
			name    string
			release NodeRelease
			want    bool
		}{
			{
				name: "LTS version (boolean true)",
				release: NodeRelease{
					LTS: StringOrBool{BoolValue: true, IsString: false},
				},
				want: true,
			},
			{
				name: "LTS version (string)",
				release: NodeRelease{
					LTS: StringOrBool{StringValue: "Hydrogen", IsString: true},
				},
				want: true,
			},
			{
				name: "Non-LTS version",
				release: NodeRelease{
					LTS: StringOrBool{BoolValue: false, IsString: false},
				},
				want: false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				if got := isLTS(tt.release); got != tt.want {
					t.Errorf("isLTS() = %v, want %v", got, tt.want)
				}
			})
		}
	})

	t.Run("getNodeEOLDate", func(t *testing.T) {
		tests := []struct {
			name    string
			release NodeRelease
			want    string
		}{
			{
				name: "EOL date as string",
				release: NodeRelease{
					EOL: StringOrBool{StringValue: "2025-04-30", IsString: true},
				},
				want: "2025-04-30",
			},
			{
				name: "EOL date as boolean",
				release: NodeRelease{
					EOL: StringOrBool{BoolValue: false, IsString: false},
				},
				want: "",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				if got := getNodeEOLDate(tt.release); got != tt.want {
					t.Errorf("getNodeEOLDate() = %v, want %v", got, tt.want)
				}
			})
		}
	})

	t.Run("getNodeReleaseDate", func(t *testing.T) {
		tests := []struct {
			name    string
			release NodeRelease
			want    string
		}{
			{
				name: "Release date as string",
				release: NodeRelease{
					ReleaseDate: StringOrBool{StringValue: "2023-04-18", IsString: true},
				},
				want: "2023-04-18",
			},
			{
				name: "Release date as boolean",
				release: NodeRelease{
					ReleaseDate: StringOrBool{BoolValue: false, IsString: false},
				},
				want: "",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				if got := getNodeReleaseDate(tt.release); got != tt.want {
					t.Errorf("getNodeReleaseDate() = %v, want %v", got, tt.want)
				}
			})
		}
	})
}

func TestNodeJSAdapter_Integration(t *testing.T) {
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

	if firstVersion.RuntimeName != "nodejs" {
		t.Errorf("Integration test: version runtime name = %s, want nodejs", firstVersion.RuntimeName)
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

	t.Logf("Integration test: Found %d Node.js versions, latest: %s", len(versions), firstVersion.Version)
}

func TestNodeJSAdapter_Debug(t *testing.T) {
	adapter := NewAdapter(&mockEndOfLifeClient{})

	// Test that adapter is not nil
	if adapter == nil {
		t.Fatal("NewAdapter returned nil")
	}

	// Type assert to check it's the right type
	nodejsAdapter, ok := adapter.(*NodeJSAdapter)
	if !ok {
		t.Fatal("NewAdapter did not return a NodeJSAdapter")
	}

	// Check that logger fields are properly initialized
	if nodejsAdapter.stdout == nil {
		t.Fatal("stdout logger is nil")
	}
	if nodejsAdapter.stderr == nil {
		t.Fatal("stderr logger is nil")
	}
	if nodejsAdapter.endoflifeClient == nil {
		t.Fatal("endoflife client is nil")
	}

	// Test a simple method call
	name := adapter.GetName()
	if name != "nodejs" {
		t.Errorf("GetName() = %s, want nodejs", name)
	}

	// Test that we can get supported platforms without crash
	platforms := adapter.GetSupportedPlatforms()
	if len(platforms) == 0 {
		t.Error("GetSupportedPlatforms() returned empty list")
	}
}

// Test helpers for audit file testing
func createMockDownloadResult(t *testing.T, tempDir string) runtime.DownloadResult {
	testFile := filepath.Join(tempDir, "node-v20.0.0-win-x64.msi")

	// Create a test file
	err := os.WriteFile(testFile, []byte("test content"), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	return runtime.DownloadResult{
		URL:       "https://nodejs.org/dist/v20.0.0/node-v20.0.0-win-x64.msi",
		LocalPath: testFile,
		FilePath:  testFile,
		Runtime:   "nodejs",
		Version:   "20.0.0",
		Platform:  platform.Platform{OS: "windows", Arch: "x64", Classifier: "win-x64"},
		FileSize:  12,
		Success:   true,
	}
}

func TestNodeJSVerificationStrategy_CreateIndividualAuditFile_Success(t *testing.T) {
	tempDir := t.TempDir()

	// Create mock logger (can be nil for this test)
	strategy := &NodeJSVerificationStrategy{
		stdout: nil,
		stderr: nil,
		Logger: nil,
	}

	result := createMockDownloadResult(t, tempDir)

	// Create checksum and signature files for more comprehensive test
	baseDir := filepath.Dir(result.LocalPath)
	checksumFile := filepath.Join(baseDir, "SHASUMS256.txt")
	signatureFile := filepath.Join(baseDir, "SHASUMS256.txt.sig")

	checksumContent := "a1b2c3d4e5f6  node-v20.0.0-win-x64.msi\n"
	err := os.WriteFile(checksumFile, []byte(checksumContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create checksum file: %v", err)
	}

	err = os.WriteFile(signatureFile, []byte("fake signature"), 0644)
	if err != nil {
		t.Fatalf("Failed to create signature file: %v", err)
	}

	// Test successful audit file creation
	err = strategy.createIndividualAuditFile(result, true, true, "success", "")
	if err != nil {
		t.Fatalf("createIndividualAuditFile failed: %v", err)
	}

	// Verify audit file was created
	auditFilePath := result.LocalPath + ".audit.json"
	if _, err := os.Stat(auditFilePath); os.IsNotExist(err) {
		t.Fatalf("Audit file was not created: %s", auditFilePath)
	}

	// Read and verify audit file content
	auditContent, err := os.ReadFile(auditFilePath)
	if err != nil {
		t.Fatalf("Failed to read audit file: %v", err)
	}

	// Parse JSON and verify key fields
	var auditData map[string]interface{}
	if err := json.Unmarshal(auditContent, &auditData); err != nil {
		t.Fatalf("Failed to parse audit JSON: %v", err)
	}

	// Verify key fields
	expectedFields := map[string]interface{}{
		"file_name":                  "node-v20.0.0-win-x64.msi",
		"runtime":                    "nodejs",
		"version":                    "20.0.0",
		"platform":                   "win-x64",
		"checksum_verified":          true,
		"checksum_algorithm":         "sha256",
		"checksum_validation_method": "sha256_hash_comparison",
		"gpg_verified":               true,
		"gpg_validation_method":      "detached_signature_verification",
		"gpg_keyring_source":         "embedded_nodejs_keys",
		"verification_status":        "success",
		"verification_type":          "nodejs-checksum-gpg",
	}

	for key, expected := range expectedFields {
		if actual, exists := auditData[key]; !exists {
			t.Errorf("Audit file missing field: %s", key)
		} else if actual != expected {
			t.Errorf("Audit file field %s = %v, want %v", key, actual, expected)
		}
	}

	// Verify verification_files section
	if verificationFiles, exists := auditData["verification_files"]; exists {
		if vf, ok := verificationFiles.(map[string]interface{}); ok {
			if checksumExists, exists := vf["checksum_file_exists"]; !exists || checksumExists != true {
				t.Error("Audit file should indicate checksum file exists")
			}
			if signatureExists, exists := vf["signature_file_exists"]; !exists || signatureExists != true {
				t.Error("Audit file should indicate signature file exists")
			}
		}
	} else {
		t.Error("Audit file missing verification_files section")
	}

	// Verify that audit_format_version field is NOT present (removed)
	if _, exists := auditData["audit_format_version"]; exists {
		t.Error("Audit file should not contain audit_format_version field")
	}

	// Verify checksum validation details are present
	if checksumSourceURL, exists := auditData["checksum_source_url"]; !exists || checksumSourceURL != "https://nodejs.org/dist/v20.0.0/SHASUMS256.txt" {
		t.Errorf("Audit file should contain correct checksum_source_url, got: %v", checksumSourceURL)
	}

	// Verify GPG validation details are present
	if gpgSourceURL, exists := auditData["gpg_signature_source_url"]; !exists || gpgSourceURL != "https://nodejs.org/dist/v20.0.0/SHASUMS256.txt.sig" {
		t.Errorf("Audit file should contain correct gpg_signature_source_url, got: %v", gpgSourceURL)
	}
}

func TestNodeJSVerificationStrategy_CreateIndividualAuditFile_Failure(t *testing.T) {
	tempDir := t.TempDir()

	strategy := &NodeJSVerificationStrategy{
		stdout: nil,
		stderr: nil,
		Logger: nil,
	}

	result := createMockDownloadResult(t, tempDir)

	// Test audit file creation with verification failure
	errorMsg := "checksum verification failed"
	err := strategy.createIndividualAuditFile(result, false, false, "failed", errorMsg)
	if err != nil {
		t.Fatalf("createIndividualAuditFile failed: %v", err)
	}

	// Verify audit file was created
	auditFilePath := result.LocalPath + ".audit.json"
	if _, err := os.Stat(auditFilePath); os.IsNotExist(err) {
		t.Fatalf("Audit file was not created: %s", auditFilePath)
	}

	// Read and verify audit file content
	auditContent, err := os.ReadFile(auditFilePath)
	if err != nil {
		t.Fatalf("Failed to read audit file: %v", err)
	}

	// Parse JSON and verify failure fields
	var auditData map[string]interface{}
	if err := json.Unmarshal(auditContent, &auditData); err != nil {
		t.Fatalf("Failed to parse audit JSON: %v", err)
	}

	// Verify failure fields
	expectedFields := map[string]interface{}{
		"checksum_verified":          false,
		"checksum_algorithm":         "sha256",
		"checksum_validation_method": "unknown",
		"gpg_verified":               false,
		"verification_status":        "failed",
		"error":                      errorMsg,
	}

	for key, expected := range expectedFields {
		if actual, exists := auditData[key]; !exists {
			t.Errorf("Audit file missing field: %s", key)
		} else if actual != expected {
			t.Errorf("Audit file field %s = %v, want %v", key, actual, expected)
		}
	}

	// Verify that audit_format_version field is NOT present (removed)
	if _, exists := auditData["audit_format_version"]; exists {
		t.Error("Audit file should not contain audit_format_version field")
	}
}

func TestNodeJSVerificationStrategy_CreateIndividualAuditFile_WriteError(t *testing.T) {
	tempDir := t.TempDir()

	strategy := &NodeJSVerificationStrategy{
		stdout: nil,
		stderr: nil,
		Logger: nil,
	}

	result := createMockDownloadResult(t, tempDir)

	// Create a directory with the same name as the audit file to cause write error
	auditFilePath := result.LocalPath + ".audit.json"
	err := os.Mkdir(auditFilePath, 0755)
	if err != nil {
		t.Fatalf("Failed to create directory: %v", err)
	}

	// Test audit file creation with write error
	err = strategy.createIndividualAuditFile(result, true, true, "success", "")
	if err == nil {
		t.Error("Expected createIndividualAuditFile to fail with write error")
	}

	if !strings.Contains(err.Error(), "failed to write audit file") {
		t.Errorf("Expected write error, got: %v", err)
	}
}

func TestNodeJSVerificationStrategy_WithAuditFileCreation(t *testing.T) {
	t.Skip("Skipping test - checksumVerifier field was removed")
	tempDir := t.TempDir()

	// Create a verification strategy
	strategy := &NodeJSVerificationStrategy{
		stdout:           nil,
		stderr:           nil,
		Logger:           nil,
	}

	result := createMockDownloadResult(t, tempDir)

	// Create checksum file with correct content
	baseDir := filepath.Dir(result.LocalPath)
	checksumFile := filepath.Join(baseDir, "SHASUMS256.txt")
	filename := filepath.Base(result.LocalPath)

	// Calculate actual checksum for test file
	testContent := []byte("test content")
	actualChecksum := fmt.Sprintf("%x", sha256.Sum256(testContent))
	checksumContent := fmt.Sprintf("%s  %s\n", actualChecksum, filename)

	err := os.WriteFile(checksumFile, []byte(checksumContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create checksum file: %v", err)
	}

	// Test verification process - this should create an audit file
	err = strategy.Verify(context.Background(), result)
	if err != nil {
		t.Fatalf("Verification failed: %v", err)
	}

	// Verify audit file was created during verification
	auditFilePath := result.LocalPath + ".audit.json"
	if _, err := os.Stat(auditFilePath); os.IsNotExist(err) {
		t.Errorf("Audit file was not created during verification: %s", auditFilePath)
	}
}

// Test StringOrBool.String() method
func TestStringOrBool_String(t *testing.T) {
	tests := []struct {
		name     string
		value    StringOrBool
		expected string
	}{
		{
			name: "string value",
			value: StringOrBool{
				StringValue: "2024-01-01",
				IsString:    true,
			},
			expected: "2024-01-01",
		},
		{
			name: "boolean true",
			value: StringOrBool{
				BoolValue: true,
				IsString:  false,
			},
			expected: "true",
		},
		{
			name: "boolean false",
			value: StringOrBool{
				BoolValue: false,
				IsString:  false,
			},
			expected: "false",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.value.String()
			if result != tt.expected {
				t.Errorf("String() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// Test NewAdapterWithConfig
func TestNewAdapterWithConfig(t *testing.T) {
	eolClient := &mockEndOfLifeClient{}
	runtimeCfg := &config.Runtime{
		Name: "nodejs",
		Download: config.DownloadConfig{
			BaseURL: "https://nodejs.org/dist",
		},
	}
	globalCfg := &config.GlobalConfig{}

	adapter := NewAdapterWithConfig(eolClient, runtimeCfg, globalCfg, nil, nil)

	if adapter == nil {
		t.Fatal("NewAdapterWithConfig() returned nil")
	}

	nodeAdapter, ok := adapter.(*NodeJSAdapter)
	if !ok {
		t.Fatal("NewAdapterWithConfig() did not return *NodeJSAdapter")
	}

	if nodeAdapter.config != runtimeCfg {
		t.Error("NewAdapterWithConfig() did not set runtime config")
	}

	if nodeAdapter.globalConfig != globalCfg {
		t.Error("NewAdapterWithConfig() did not set global config")
	}
}

// Test NodeJSVerificationStrategy.RequiresAdditionalFiles
func TestNodeJSVerificationStrategy_RequiresAdditionalFiles(t *testing.T) {
	strategy := &NodeJSVerificationStrategy{}
	
	if !strategy.RequiresAdditionalFiles() {
		t.Error("RequiresAdditionalFiles() should return true for Node.js")
	}
}

// Test NodeJSVerificationStrategy.GetType
func TestNodeJSVerificationStrategy_GetType(t *testing.T) {
	strategy := &NodeJSVerificationStrategy{}
	
	if strategy.GetType() != "nodejs-checksum-gpg" {
		t.Errorf("GetType() = %v, want 'nodejs-checksum-gpg'", strategy.GetType())
	}
}


// Test verifyChecksum (via NodeJSVerificationStrategy.Verify)
func TestVerifyChecksum(t *testing.T) {
	tempDir := t.TempDir()

	// Create a test file
	testFile := filepath.Join(tempDir, "node-v20.15.0-linux-x64.tar.gz")
	testContent := []byte("test content for checksum")
	err := os.WriteFile(testFile, testContent, 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Calculate checksum
	hash := sha256.Sum256(testContent)
	checksum := fmt.Sprintf("%x", hash)

	// Create checksum file
	checksumFile := filepath.Join(tempDir, "SHASUMS256.txt")
	checksumContent := fmt.Sprintf("%s  node-v20.15.0-linux-x64.tar.gz\n", checksum)
	err = os.WriteFile(checksumFile, []byte(checksumContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create checksum file: %v", err)
	}

	// Create verification strategy
	strategy := &NodeJSVerificationStrategy{
		stdout: nil,
		stderr: nil,
		Logger: nil,
	}

	// Create result
	result := runtime.DownloadResult{
		LocalPath: testFile,
		FilePath:  testFile,
		URL:       "https://nodejs.org/dist/v20.15.0/node-v20.15.0-linux-x64.tar.gz",
		Runtime:   "nodejs",
		Version:   "v20.15.0",
		Platform:  platform.Platform{OS: "linux", Arch: "x64", FileExt: "tar.gz", Classifier: "linux-x64"},
		FileSize:  int64(len(testContent)),
		Success:   true,
	}

	// Verify - this should pass checksum but fail GPG (no signature file)
	err = strategy.Verify(context.Background(), result)
	if err != nil {
		t.Errorf("Verify() with valid checksum error = %v", err)
	}
}

// Test verifyChecksum failure
func TestVerifyChecksum_Failure(t *testing.T) {
	tempDir := t.TempDir()

	// Create a test file
	testFile := filepath.Join(tempDir, "node-v20.15.0-linux-x64.tar.gz")
	testContent := []byte("test content for checksum")
	err := os.WriteFile(testFile, testContent, 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Create checksum file with WRONG checksum
	checksumFile := filepath.Join(tempDir, "SHASUMS256.txt")
	checksumContent := "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef  node-v20.15.0-linux-x64.tar.gz\n"
	err = os.WriteFile(checksumFile, []byte(checksumContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create checksum file: %v", err)
	}

	// Create verification strategy
	strategy := &NodeJSVerificationStrategy{
		stdout: nil,
		stderr: nil,
		Logger: nil,
	}

	// Create result
	result := runtime.DownloadResult{
		LocalPath: testFile,
		FilePath:  testFile,
		URL:       "https://nodejs.org/dist/v20.15.0/node-v20.15.0-linux-x64.tar.gz",
		Runtime:   "nodejs",
		Version:   "v20.15.0",
		Platform:  platform.Platform{OS: "linux", Arch: "x64", FileExt: "tar.gz", Classifier: "linux-x64"},
		FileSize:  int64(len(testContent)),
		Success:   true,
	}

	// Verify - this should fail checksum
	err = strategy.Verify(context.Background(), result)
	if err == nil {
		t.Error("Verify() with invalid checksum should fail")
	}

	if !strings.Contains(err.Error(), "checksum") {
		t.Errorf("Error should mention checksum: %v", err)
	}
}
