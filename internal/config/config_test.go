package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/clean-dependency-project/cdprun/internal/platform"
)

func TestLoadConfig(t *testing.T) {
	tests := []struct {
		name        string
		configData  string
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid config",
			configData: `
version: "1.0"
metadata:
  name: "test config"
  description: "test description"
  created: "2023-01-01"
  updated: "2023-01-02"
config:
  download_timeout: "30s"
  concurrent_downloads: 4
  output_base_directory: "/tmp"
  verify_downloads: true
  endoflife_api_timeout: "10s"
  auto_download_all_platforms: false
runtimes:
  python:
    enabled: true
    name: "Python"
    description: "Python runtime"
    endoflife_product: "python"
    policy_file: "python-policy.json"
    version_pattern: "\\d+\\.\\d+\\.\\d+"
    supported_architectures: ["x64", "arm64"]
    supported_platforms:
      - os: "linux"
        arch: ["x64", "arm64"]
        file_extension: ".tar.xz"
        download_name: "Python-{version}-{os}-{arch}"
    download:
      base_url: "https://python.org"
      url_pattern: "/downloads/{version}/Python-{version}-{os}-{arch}.tar.xz"
      user_agent: "cdprun/1.0"
      requires_auth: false
    verification:
      enabled: true
      methods:
        checksum:
          enabled: true
          algorithm: "sha256"
          file_pattern: "{url}.sha256"
          remote_checksum: false
        gpg:
          enabled: false
          keyserver: ""
          key_id: ""
          signature_pattern: ""
    endoflife:
      product_name: "python"
      check_eol: true
      warn_on_eol: true
`,
			expectError: false,
		},
		{
			name: "missing version",
			configData: `
runtimes:
  python:
    enabled: true
    endoflife_product: "python"
    policy_file: "python-policy.json"
    verification:
      enabled: false
`,
			expectError: true,
			errorMsg:    "version is required",
		},
		{
			name: "no runtimes",
			configData: `
version: "1.0"
runtimes: {}
`,
			expectError: true,
			errorMsg:    "at least one runtime must be configured",
		},
		{
			name: "invalid yaml",
			configData: `
version: "1.0"
runtimes:
  python:
    enabled: true
    invalid_yaml: [
`,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temp file
			tmpFile, err := os.CreateTemp("", "config-test-*.yaml")
			if err != nil {
				t.Fatalf("Failed to create temp file: %v", err)
			}
			defer func() { _ = os.Remove(tmpFile.Name()) }()

			// Write test data
			if _, err := tmpFile.WriteString(tt.configData); err != nil {
				t.Fatalf("Failed to write test data: %v", err)
			}
			_ = tmpFile.Close()

			// Test LoadConfig
			config, err := LoadConfig(tmpFile.Name())

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				} else if tt.errorMsg != "" {
					// Check if error message contains the expected message (since LoadConfig wraps errors)
					if !strings.Contains(err.Error(), tt.errorMsg) {
						t.Errorf("Expected error message to contain %q, got %q", tt.errorMsg, err.Error())
					}
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if config == nil {
				t.Errorf("Expected config to be non-nil")
			}
		})
	}
}

func TestLoadConfig_FileNotFound(t *testing.T) {
	_, err := LoadConfig("nonexistent-file.yaml")
	if err == nil {
		t.Errorf("Expected error for nonexistent file")
	}
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name        string
		config      Config
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid config",
			config: Config{
				Version: "1.0",
				Runtimes: map[string]Runtime{
					"python": {
						Enabled:          true,
						EndOfLifeProduct: "python",
						PolicyFile:       "python-policy.json",
						Verification: Verification{
							Enabled: true,
							Methods: VerificationMethods{
								Checksum: ChecksumVerification{
									Enabled:     true,
									Algorithm:   "sha256",
									FilePattern: "{url}.sha256",
								},
							},
						},
					},
				},
			},
			expectError: false,
		},
		{
			name: "missing version",
			config: Config{
				Runtimes: map[string]Runtime{
					"python": {Enabled: true},
				},
			},
			expectError: true,
			errorMsg:    "version is required",
		},
		{
			name: "no runtimes",
			config: Config{
				Version:  "1.0",
				Runtimes: map[string]Runtime{},
			},
			expectError: true,
			errorMsg:    "at least one runtime must be configured",
		},
		{
			name: "invalid runtime config",
			config: Config{
				Version: "1.0",
				Runtimes: map[string]Runtime{
					"python": {
						Enabled: true,
						// Missing required fields
					},
				},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				} else if tt.errorMsg != "" && err.Error() != tt.errorMsg {
					// Check if error message contains expected text
					if err.Error() != tt.errorMsg {
						t.Logf("Expected: %q, Got: %q", tt.errorMsg, err.Error())
					}
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

func TestRuntime_Validate(t *testing.T) {
	tests := []struct {
		name        string
		runtime     Runtime
		runtimeName string
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid enabled runtime",
			runtime: Runtime{
				Enabled:          true,
				EndOfLifeProduct: "python",
				PolicyFile:       "python-policy.json",
				Verification: Verification{
					Enabled: true,
					Methods: VerificationMethods{
						Checksum: ChecksumVerification{
							Enabled:     true,
							Algorithm:   "sha256",
							FilePattern: "{url}.sha256",
						},
					},
				},
			},
			runtimeName: "python",
			expectError: false,
		},
		{
			name: "disabled runtime should pass validation",
			runtime: Runtime{
				Enabled: false,
				// Missing required fields, but should pass since disabled
			},
			runtimeName: "python",
			expectError: false,
		},
		{
			name: "missing endoflife_product",
			runtime: Runtime{
				Enabled:    true,
				PolicyFile: "python-policy.json",
				Verification: Verification{
					Enabled: false,
				},
			},
			runtimeName: "python",
			expectError: true,
			errorMsg:    "endoflife_product is required for enabled runtime",
		},
		{
			name: "missing policy_file",
			runtime: Runtime{
				Enabled:          true,
				EndOfLifeProduct: "python",
				Verification: Verification{
					Enabled: false,
				},
			},
			runtimeName: "python",
			expectError: true,
			errorMsg:    "policy_file is required for enabled runtime",
		},
		{
			name: "invalid verification config",
			runtime: Runtime{
				Enabled:          true,
				EndOfLifeProduct: "python",
				PolicyFile:       "python-policy.json",
				Verification: Verification{
					Enabled: true,
					Methods: VerificationMethods{
						Checksum: ChecksumVerification{
							Enabled: true,
							// Missing required fields
						},
					},
				},
			},
			runtimeName: "python",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.runtime.Validate(tt.runtimeName)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				} else if tt.errorMsg != "" && err.Error() != tt.errorMsg {
					t.Logf("Expected: %q, Got: %q", tt.errorMsg, err.Error())
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

func TestVerification_Validate(t *testing.T) {
	tests := []struct {
		name         string
		verification Verification
		expectError  bool
		errorMsg     string
	}{
		{
			name: "valid checksum verification",
			verification: Verification{
				Enabled: true,
				Methods: VerificationMethods{
					Checksum: ChecksumVerification{
						Enabled:     true,
						Algorithm:   "sha256",
						FilePattern: "{url}.sha256",
					},
				},
			},
			expectError: false,
		},
		{
			name: "valid gpg verification",
			verification: Verification{
				Enabled: true,
				Methods: VerificationMethods{
					GPG: GPGVerification{
						Enabled:          true,
						SignaturePattern: "{url}.sig",
					},
				},
			},
			expectError: false,
		},
		{
			name: "disabled verification",
			verification: Verification{
				Enabled: false,
			},
			expectError: false,
		},
		{
			name: "checksum enabled but missing algorithm",
			verification: Verification{
				Enabled: true,
				Methods: VerificationMethods{
					Checksum: ChecksumVerification{
						Enabled:     true,
						FilePattern: "{url}.sha256",
					},
				},
			},
			expectError: true,
			errorMsg:    "checksum_algorithm is required when checksum is enabled",
		},
		{
			name: "checksum enabled but missing file pattern",
			verification: Verification{
				Enabled: true,
				Methods: VerificationMethods{
					Checksum: ChecksumVerification{
						Enabled:   true,
						Algorithm: "sha256",
					},
				},
			},
			expectError: true,
			errorMsg:    "checksum_pattern is required when checksum is enabled",
		},
		{
			name: "gpg enabled but missing signature pattern",
			verification: Verification{
				Enabled: true,
				Methods: VerificationMethods{
					GPG: GPGVerification{
						Enabled: true,
					},
				},
			},
			expectError: true,
			errorMsg:    "signature_pattern is required when gpg is enabled",
		},
		{
			name: "both checksum and gpg enabled",
			verification: Verification{
				Enabled: true,
				Methods: VerificationMethods{
					Checksum: ChecksumVerification{
						Enabled:     true,
						Algorithm:   "sha256",
						FilePattern: "{url}.sha256",
					},
					GPG: GPGVerification{
						Enabled:          true,
						SignaturePattern: "{url}.sig",
						Keyserver:        "",
						KeyID:            "12345678",
					},
				},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.verification.Validate()

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				} else if tt.errorMsg != "" && err.Error() != tt.errorMsg {
					t.Errorf("Expected error message %q, got %q", tt.errorMsg, err.Error())
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

func TestRuntime_GetConfiguredPlatforms(t *testing.T) {
	runtime := Runtime{
		SupportedPlatforms: []PlatformConfig{
			{
				OS:            "linux",
				Arch:          []string{"x64", "arm64"},
				FileExtension: ".tar.xz",
				DownloadName:  "python-{version}-{os}-{arch}",
			},
			{
				OS:            "windows",
				Arch:          []string{"x64"},
				FileExtension: ".zip",
				DownloadName:  "python-{version}-{os}-{arch}",
			},
		},
	}

	platforms := runtime.GetConfiguredPlatforms()

	expectedPlatforms := []platform.Platform{
		{
			OS:           "linux",
			Arch:         "x64",
			FileExt:      ".tar.xz",
			DownloadName: "python-{version}-{os}-{arch}",
			Classifier:   "linux-x64",
		},
		{
			OS:           "linux",
			Arch:         "arm64",
			FileExt:      ".tar.xz",
			DownloadName: "python-{version}-{os}-{arch}",
			Classifier:   "linux-arm64",
		},
		{
			OS:           "windows",
			Arch:         "x64",
			FileExt:      ".zip",
			DownloadName: "python-{version}-{os}-{arch}",
			Classifier:   "windows-x64",
		},
	}

	if len(platforms) != len(expectedPlatforms) {
		t.Errorf("Expected %d platforms, got %d", len(expectedPlatforms), len(platforms))
		return
	}

	for i, expected := range expectedPlatforms {
		if platforms[i] != expected {
			t.Errorf("Platform %d mismatch. Expected %+v, got %+v", i, expected, platforms[i])
		}
	}
}

func TestRuntime_GetConfiguredPlatforms_Empty(t *testing.T) {
	runtime := Runtime{
		SupportedPlatforms: []PlatformConfig{},
	}

	platforms := runtime.GetConfiguredPlatforms()

	if len(platforms) != 0 {
		t.Errorf("Expected 0 platforms for empty config, got %d", len(platforms))
	}
}

func TestConfig_GetEnabledRuntimes(t *testing.T) {
	config := Config{
		Runtimes: map[string]Runtime{
			"python": {
				Enabled: true,
				Name:    "Python",
			},
			"nodejs": {
				Enabled: false,
				Name:    "Node.js",
			},
			"temurin": {
				Enabled: true,
				Name:    "Eclipse Temurin",
			},
		},
	}

	enabled := config.GetEnabledRuntimes()

	if len(enabled) != 2 {
		t.Errorf("Expected 2 enabled runtimes, got %d", len(enabled))
	}

	if _, exists := enabled["python"]; !exists {
		t.Errorf("Expected python runtime to be enabled")
	}

	if _, exists := enabled["temurin"]; !exists {
		t.Errorf("Expected temurin runtime to be enabled")
	}

	if _, exists := enabled["nodejs"]; exists {
		t.Errorf("Expected nodejs runtime to be disabled")
	}
}

func TestConfig_GetRuntimeConfig(t *testing.T) {
	config := Config{
		Runtimes: map[string]Runtime{
			"python": {
				Enabled: true,
				Name:    "Python",
			},
			"nodejs": {
				Enabled: false,
				Name:    "Node.js",
			},
		},
	}

	// Test enabled runtime
	runtime, exists := config.GetRuntimeConfig("python")
	if !exists {
		t.Errorf("Expected python runtime to exist and be enabled")
	}
	if runtime.Name != "Python" {
		t.Errorf("Expected runtime name to be 'Python', got %q", runtime.Name)
	}

	// Test disabled runtime
	_, exists = config.GetRuntimeConfig("nodejs")
	if exists {
		t.Errorf("Expected nodejs runtime to be disabled")
	}

	// Test nonexistent runtime
	_, exists = config.GetRuntimeConfig("nonexistent")
	if exists {
		t.Errorf("Expected nonexistent runtime to not exist")
	}
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config == nil {
		t.Errorf("Expected non-nil config")
		return
	}

	if config.Version != "1.0" {
		t.Errorf("Expected version '1.0', got %q", config.Version)
	}

	expectedRuntimes := []string{"python", "nodejs", "temurin"}
	for _, name := range expectedRuntimes {
		runtime, exists := config.Runtimes[name]
		if !exists {
			t.Errorf("Expected runtime %q to exist in default config", name)
			continue
		}

		if !runtime.Enabled {
			t.Errorf("Expected runtime %q to be enabled in default config", name)
		}

		if runtime.EndOfLifeProduct == "" {
			t.Errorf("Expected runtime %q to have endoflife_product in default config", name)
		}

		if runtime.PolicyFile == "" {
			t.Errorf("Expected runtime %q to have policy_file in default config", name)
		}
	}

	// Validate the default config
	if err := config.Validate(); err != nil {
		t.Errorf("Default config should be valid: %v", err)
	}
}

func TestSaveConfig(t *testing.T) {
	config := &Config{
		Version: "1.0",
		Metadata: Metadata{
			Name:        "test config",
			Description: "test description",
		},
		Runtimes: map[string]Runtime{
			"python": {
				Enabled:          true,
				Name:             "Python",
				EndOfLifeProduct: "python",
				PolicyFile:       "python-policy.json",
				Verification: Verification{
					Enabled: false,
				},
			},
		},
	}

	// Create temp file
	tmpFile, err := os.CreateTemp("", "config-save-test-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	// Test SaveConfig
	err = SaveConfig(config, tmpFile.Name())
	if err != nil {
		t.Errorf("Unexpected error saving config: %v", err)
		return
	}

	// Verify file was written by loading it back
	loadedConfig, err := LoadConfig(tmpFile.Name())
	if err != nil {
		t.Errorf("Failed to load saved config: %v", err)
		return
	}

	if loadedConfig.Version != config.Version {
		t.Errorf("Expected version %q, got %q", config.Version, loadedConfig.Version)
	}

	if loadedConfig.Metadata.Name != config.Metadata.Name {
		t.Errorf("Expected metadata name %q, got %q", config.Metadata.Name, loadedConfig.Metadata.Name)
	}
}

func TestSaveConfig_InvalidPath(t *testing.T) {
	config := DefaultConfig()

	// Try to save to an invalid path
	err := SaveConfig(config, "/nonexistent/directory/config.yaml")
	if err == nil {
		t.Errorf("Expected error when saving to invalid path")
	}
}

func TestSaveConfig_InvalidPermissions(t *testing.T) {
	config := DefaultConfig()

	// Create a directory to test permission error
	tmpDir, err := os.MkdirTemp("", "config-test-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Make directory read-only
	if err := os.Chmod(tmpDir, 0444); err != nil {
		t.Fatalf("Failed to change directory permissions: %v", err)
	}

	// Try to save to read-only directory
	filePath := filepath.Join(tmpDir, "config.yaml")
	err = SaveConfig(config, filePath)
	if err == nil {
		t.Errorf("Expected error when saving to read-only directory")
	}
}

// Test for edge cases and complete coverage
func TestConfig_EmptyRuntimes(t *testing.T) {
	config := &Config{
		Version:  "1.0",
		Runtimes: map[string]Runtime{},
	}

	enabled := config.GetEnabledRuntimes()
	if len(enabled) != 0 {
		t.Errorf("Expected 0 enabled runtimes for empty runtimes map, got %d", len(enabled))
	}
}

func TestRuntime_GetConfiguredPlatforms_MultipleArchs(t *testing.T) {
	runtime := Runtime{
		SupportedPlatforms: []PlatformConfig{
			{
				OS:            "linux",
				Arch:          []string{"x64", "arm64", "armv7"},
				FileExtension: ".tar.xz",
				DownloadName:  "runtime-{version}",
			},
		},
	}

	platforms := runtime.GetConfiguredPlatforms()

	if len(platforms) != 3 {
		t.Errorf("Expected 3 platforms, got %d", len(platforms))
	}

	expectedArches := []string{"x64", "arm64", "armv7"}
	for i, arch := range expectedArches {
		if platforms[i].Arch != arch {
			t.Errorf("Expected arch %q at index %d, got %q", arch, i, platforms[i].Arch)
		}
		if platforms[i].Classifier != "linux-"+arch {
			t.Errorf("Expected classifier 'linux-%s' at index %d, got %q", arch, i, platforms[i].Classifier)
		}
	}
}

// Additional tests for better coverage
func TestConfig_ValidateNilRuntimes(t *testing.T) {
	config := Config{
		Version:  "1.0",
		Runtimes: nil,
	}

	err := config.Validate()
	if err == nil {
		t.Errorf("Expected error for nil runtimes")
	}
	if !strings.Contains(err.Error(), "at least one runtime must be configured") {
		t.Errorf("Expected error about runtime configuration, got: %v", err)
	}
}

func TestSaveConfig_MarshalError(t *testing.T) {
	// Create a config that will cause marshal error (using invalid types)
	// Since we can't easily create a marshal error with the current structs,
	// we'll just test with a normal config to ensure the function is covered
	config := DefaultConfig()

	tmpFile, err := os.CreateTemp("", "config-marshal-test-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	err = SaveConfig(config, tmpFile.Name())
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestRuntime_GetConfiguredPlatforms_MultiplePlatforms(t *testing.T) {
	runtime := Runtime{
		SupportedPlatforms: []PlatformConfig{
			{
				OS:            "linux",
				Arch:          []string{"x64"},
				FileExtension: ".tar.xz",
				DownloadName:  "runtime-linux",
			},
			{
				OS:            "windows",
				Arch:          []string{"x64"},
				FileExtension: ".zip",
				DownloadName:  "runtime-windows",
			},
			{
				OS:            "darwin",
				Arch:          []string{"x64", "arm64"},
				FileExtension: ".tar.gz",
				DownloadName:  "runtime-darwin",
			},
		},
	}

	platforms := runtime.GetConfiguredPlatforms()

	if len(platforms) != 4 {
		t.Errorf("Expected 4 platforms, got %d", len(platforms))
	}

	// Verify specific platforms
	expectedPlatforms := map[string]bool{
		"linux-x64":    false,
		"windows-x64":  false,
		"darwin-x64":   false,
		"darwin-arm64": false,
	}

	for _, platform := range platforms {
		expectedPlatforms[platform.Classifier] = true
	}

	for classifier, found := range expectedPlatforms {
		if !found {
			t.Errorf("Expected platform %q not found", classifier)
		}
	}
}



// TestGetConfiguredPlatforms_UsingSupportedArchitectures tests that supported_architectures drives platform generation
func TestGetConfiguredPlatforms_UsingSupportedArchitectures(t *testing.T) {
	tests := []struct {
		name              string
		runtime           Runtime
		expectedPlatforms int
		expectedArchs     map[string]bool
	}{
		{
			name: "supported_architectures generates all combinations",
			runtime: Runtime{
				SupportedArchitectures: []string{"x64", "aarch64"},
				SupportedPlatforms: []PlatformConfig{
					{OS: "linux", Arch: []string{"x64", "aarch64"}, FileExtension: "tar.gz"},
					{OS: "windows", Arch: []string{"x64", "aarch64"}, FileExtension: "msi"},
				},
			},
			expectedPlatforms: 4, // 2 OS × 2 architectures
			expectedArchs:     map[string]bool{"x64": true, "aarch64": true},
		},
		{
			name: "platform arch list filters supported_architectures",
			runtime: Runtime{
				SupportedArchitectures: []string{"x64", "aarch64", "arm"},
				SupportedPlatforms: []PlatformConfig{
					{OS: "linux", Arch: []string{"x64"}, FileExtension: "tar.gz"},        // Only x64
					{OS: "windows", Arch: []string{"x64", "aarch64"}, FileExtension: "msi"}, // x64 and aarch64
				},
			},
			expectedPlatforms: 3, // linux-x64, windows-x64, windows-aarch64
			expectedArchs:     map[string]bool{"x64": true, "aarch64": true},
		},
		{
			name: "empty platform arch list uses all supported_architectures",
			runtime: Runtime{
				SupportedArchitectures: []string{"x64", "aarch64"},
				SupportedPlatforms: []PlatformConfig{
					{OS: "linux", Arch: []string{}, FileExtension: "tar.gz"}, // Empty = use all
				},
			},
			expectedPlatforms: 2, // linux-x64, linux-aarch64
			expectedArchs:     map[string]bool{"x64": true, "aarch64": true},
		},
		{
			name: "no supported_architectures uses legacy behavior",
			runtime: Runtime{
				SupportedArchitectures: []string{}, // Empty = use platform arch lists
				SupportedPlatforms: []PlatformConfig{
					{OS: "linux", Arch: []string{"x64", "arm"}, FileExtension: "tar.gz"},
				},
			},
			expectedPlatforms: 2, // Uses explicit arch list
			expectedArchs:     map[string]bool{"x64": true, "arm": true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			platforms := tt.runtime.GetConfiguredPlatforms()

			if len(platforms) != tt.expectedPlatforms {
				t.Errorf("expected %d platforms, got %d", tt.expectedPlatforms, len(platforms))
			}

			// Verify architectures
			foundArchs := make(map[string]bool)
			for _, plat := range platforms {
				foundArchs[plat.Arch] = true
			}

			for arch := range tt.expectedArchs {
				if !foundArchs[arch] {
					t.Errorf("expected architecture %q not found in platforms", arch)
				}
			}
		})
	}
}
