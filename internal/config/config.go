// Package config provides configuration management for the unified runtime download system.
// It handles YAML-based runtime registry configuration including verification strategies.
package config

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/clean-dependency-project/cdprun/internal/platform"
	"gopkg.in/yaml.v3"
)

// Sentinel errors for configuration validation
var (
	ErrVersionRequired           = errors.New("version is required")
	ErrNoRuntimes                = errors.New("at least one runtime must be configured")
	ErrEndoflifeProductRequired  = errors.New("endoflife_product is required for enabled runtime")
	ErrPolicyFileRequired        = errors.New("policy_file is required for enabled runtime")
	ErrChecksumAlgorithmRequired = errors.New("checksum_algorithm is required when checksum is enabled")
	ErrChecksumPatternRequired   = errors.New("checksum_pattern is required when checksum is enabled")
	ErrSignaturePatternRequired  = errors.New("signature_pattern is required when gpg is enabled")
	ErrClamAVImageRequired       = errors.New("clamav image is required when clamav is enabled")
)

// Config represents the top-level configuration structure.
type Config struct {
	Version  string             `yaml:"version"`
	Metadata Metadata           `yaml:"metadata"`
	Config   GlobalConfig       `yaml:"config"`
	Runtimes map[string]Runtime `yaml:"runtimes"`
}

// Metadata represents metadata about the configuration.
type Metadata struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Created     string `yaml:"created"`
	Updated     string `yaml:"updated"`
}

// StorageConfig represents storage configuration for download tracking.
type StorageConfig struct {
	DatabasePath string `yaml:"database_path"`
}

// GlobalConfig represents global configuration settings.
type GlobalConfig struct {
	DownloadTimeout          string        `yaml:"download_timeout"`
	AutoDownloadAllPlatforms bool          `yaml:"auto_download_all_platforms"`
	Storage                  StorageConfig `yaml:"storage"`
	IgnoreFile               string        `yaml:"ignore_file"` // Path to JSON file listing versions to ignore per runtime
}

// GetDownloadTimeout parses and returns the download timeout duration
func (g *GlobalConfig) GetDownloadTimeout() time.Duration {
	if g.DownloadTimeout == "" {
		return 30 * time.Second // Default timeout
	}
	timeout, err := time.ParseDuration(g.DownloadTimeout)
	if err != nil {
		return 30 * time.Second // Default on parse error
	}
	return timeout
}

// Runtime represents configuration for a specific runtime.
type Runtime struct {
	Enabled                bool               `yaml:"enabled"`
	Name                   string             `yaml:"name"`
	Description            string             `yaml:"description"`
	EndOfLifeProduct       string             `yaml:"endoflife_product"`
	PolicyFile             string             `yaml:"policy_file"`
	VersionPattern         string             `yaml:"version_pattern"`
	SupportedArchitectures []string           `yaml:"supported_architectures"`
	SupportedPlatforms     []PlatformConfig   `yaml:"supported_platforms"`
	Download               DownloadConfig     `yaml:"download"`
	Verification           Verification       `yaml:"verification"`
	EndOfLife              EndOfLifeConfig    `yaml:"endoflife"`
	Release                ReleaseConfig      `yaml:"release"`
}

// PlatformConfig represents platform-specific configuration.
type PlatformConfig struct {
	OS            string   `yaml:"os"`
	Arch          []string `yaml:"arch"`
	FileExtension string   `yaml:"file_extension"`
	DownloadName  string   `yaml:"download_name"`
}

// DownloadConfig represents download configuration.
type DownloadConfig struct {
	BaseURL      string `yaml:"base_url"`
	URLPattern   string `yaml:"url_pattern"`
	UserAgent    string `yaml:"user_agent"`
	RequiresAuth bool   `yaml:"requires_auth"`
}

// EndOfLifeConfig represents endoflife integration configuration.
type EndOfLifeConfig struct {
	ProductName string `yaml:"product_name"`
	CheckEOL    bool   `yaml:"check_eol"`
	WarnOnEOL   bool   `yaml:"warn_on_eol"`
}

// ReleaseConfig represents GitHub release configuration for a runtime.
type ReleaseConfig struct {
	AutoRelease      bool   `yaml:"auto_release"`       // Enable automatic GitHub release creation
	GitHubRepository string `yaml:"github_repository"`  // Repository in "owner/repo" format
	DraftRelease     bool   `yaml:"draft_release"`      // Create as draft (default: false)
	ReleaseNameTemplate string `yaml:"release_name_template"` // e.g., "Node.js {version}"
}

// Verification represents verification configuration for a runtime.
type Verification struct {
	Enabled bool                `yaml:"enabled"`
	Methods VerificationMethods `yaml:"methods"`
}

// VerificationMethods represents different verification methods.
type VerificationMethods struct {
	Checksum ChecksumVerification `yaml:"checksum"`
	GPG      GPGVerification      `yaml:"gpg"`
	ClamAV   ClamAVVerification   `yaml:"clamav"`
}

// ChecksumVerification represents checksum verification configuration.
type ChecksumVerification struct {
	Enabled        bool   `yaml:"enabled"`
	Algorithm      string `yaml:"algorithm"`
	FilePattern    string `yaml:"file_pattern"`
	RemoteChecksum bool   `yaml:"remote_checksum"`
}

// GPGVerification represents GPG verification configuration.
type GPGVerification struct {
	Enabled          bool   `yaml:"enabled"`
	Keyserver        string `yaml:"keyserver"`
	KeyID            string `yaml:"key_id"`
	SignaturePattern string `yaml:"signature_pattern"`
}

// ClamAVVerification represents ClamAV malware scanning configuration.
type ClamAVVerification struct {
	Enabled           bool   `yaml:"enabled"`
	Image             string `yaml:"image"` // Docker image, e.g., "clamav/clamav-debian:latest"
	DeleteOnDetection bool   `yaml:"delete_on_detection"`
}

// GetConfiguredPlatforms returns platform.Platform objects for the configured platforms.
// If supported_architectures is specified, it generates platforms by combining each OS platform
// with all supported architectures (filtered by platform's arch list if specified).
// Otherwise, it uses the explicit arch list from each platform configuration.
func (r *Runtime) GetConfiguredPlatforms() []platform.Platform {
	var platforms []platform.Platform

	// If supported_architectures is specified, use it as the source of truth
	if len(r.SupportedArchitectures) > 0 {
		return r.generatePlatformsFromSupportedArchitectures()
	}

	// Fallback: use explicit arch lists from platform configs (legacy behavior)
	for _, platConfig := range r.SupportedPlatforms {
		for _, arch := range platConfig.Arch {
			plat := platform.Platform{
				OS:           platConfig.OS,
				Arch:         arch,
				FileExt:      platConfig.FileExtension,
				DownloadName: platConfig.DownloadName,
				Classifier:   fmt.Sprintf("%s-%s", platConfig.OS, arch),
			}
			platforms = append(platforms, plat)
		}
	}
	return platforms
}

// generatePlatformsFromSupportedArchitectures creates platform combinations using supported_architectures.
// For each OS platform, it combines with each architecture from supported_architectures,
// but only if that arch is also in the platform's arch list (when specified).
func (r *Runtime) generatePlatformsFromSupportedArchitectures() []platform.Platform {
	var platforms []platform.Platform

	for _, platConfig := range r.SupportedPlatforms {
		// Build set of platform-specific architectures for filtering
		platformArchs := make(map[string]bool)
		for _, arch := range platConfig.Arch {
			platformArchs[arch] = true
		}

		// For each supported architecture, check if this platform supports it
		for _, arch := range r.SupportedArchitectures {
			// If platform has specific arch list, only use architectures in that list
			// If platform arch list is empty, use all supported architectures
			if len(platConfig.Arch) > 0 && !platformArchs[arch] {
				continue // Skip arch not supported by this platform
			}

			plat := platform.Platform{
				OS:           platConfig.OS,
				Arch:         arch,
				FileExt:      platConfig.FileExtension,
				DownloadName: platConfig.DownloadName,
				Classifier:   fmt.Sprintf("%s-%s", platConfig.OS, arch),
			}
			platforms = append(platforms, plat)
		}
	}

	return platforms
}

// LoadConfig loads and parses the runtime registry configuration from a YAML file.
func LoadConfig(filePath string) (*Config, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", filePath, err)
	}
	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file %s: %w", filePath, err)
	}
	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}
	return &config, nil
}

// Validate validates the configuration structure and required fields.
func (c *Config) Validate() error {
	if c.Version == "" {
		return ErrVersionRequired
	}
	if len(c.Runtimes) == 0 {
		return ErrNoRuntimes
	}
	for name, runtime := range c.Runtimes {
		if err := runtime.Validate(name); err != nil {
			return fmt.Errorf("runtime %s: %w", name, err)
		}
	}
	return nil
}

// Validate validates a runtime configuration.
func (r *Runtime) Validate(name string) error {
	if !r.Enabled {
		return nil // Skip validation for disabled runtimes
	}
	if r.EndOfLifeProduct == "" {
		return ErrEndoflifeProductRequired
	}
	if r.PolicyFile == "" {
		return ErrPolicyFileRequired
	}

	// Note: We don't validate that platform arch lists are subsets of supported_architectures
	// The filtering happens in GetConfiguredPlatforms() instead
	// This allows users to keep arch lists unchanged and control via supported_architectures

	if err := r.Verification.Validate(); err != nil {
		return fmt.Errorf("verification: %w", err)
	}
	return nil
}

// Validate validates verification configuration.
func (v *Verification) Validate() error {
	if v.Methods.Checksum.Enabled {
		if v.Methods.Checksum.Algorithm == "" {
			return ErrChecksumAlgorithmRequired
		}
		if v.Methods.Checksum.FilePattern == "" {
			return ErrChecksumPatternRequired
		}
	}
	if v.Methods.GPG.Enabled {
		if v.Methods.GPG.SignaturePattern == "" {
			return ErrSignaturePatternRequired
		}
		// GPG keyserver is optional, defaults can be used
	}
	if v.Methods.ClamAV.Enabled {
		if v.Methods.ClamAV.Image == "" {
			return ErrClamAVImageRequired
		}
	}
	return nil
}

// GetEnabledRuntimes returns a map of enabled runtime configurations.
func (c *Config) GetEnabledRuntimes() map[string]Runtime {
	enabled := make(map[string]Runtime)
	for name, runtime := range c.Runtimes {
		if runtime.Enabled {
			enabled[name] = runtime
		}
	}
	return enabled
}

// GetRuntimeConfig returns the configuration for a specific runtime.
func (c *Config) GetRuntimeConfig(name string) (Runtime, bool) {
	runtime, exists := c.Runtimes[name]
	return runtime, exists && runtime.Enabled
}

// DefaultConfig returns a default configuration with common runtimes.
func DefaultConfig() *Config {
	return &Config{
		Version: "1.0",
		Runtimes: map[string]Runtime{
			"python": {
				Enabled:          true,
				EndOfLifeProduct: "python",
				PolicyFile:       "policies/python-policy.json",
				Verification: Verification{
					Enabled: true,
					Methods: VerificationMethods{
						Checksum: ChecksumVerification{
							Enabled:        true,
							Algorithm:      "sha256",
							FilePattern:    "{url}.sha256",
							RemoteChecksum: false,
						},
						GPG: GPGVerification{
							Enabled:          false,
							Keyserver:        "",
							KeyID:            "",
							SignaturePattern: "",
						},
					},
				},
			},
			"nodejs": {
				Enabled:          true,
				EndOfLifeProduct: "nodejs",
				PolicyFile:       "policies/nodejs-policy.json",
				Verification: Verification{
					Enabled: true,
					Methods: VerificationMethods{
						Checksum: ChecksumVerification{
							Enabled:        true,
							Algorithm:      "sha256",
							FilePattern:    "SHASUMS256.txt",
							RemoteChecksum: false,
						},
						GPG: GPGVerification{
							Enabled:          true,
							Keyserver:        "keyserver.ubuntu.com",
							KeyID:            "",
							SignaturePattern: "SHASUMS256.txt.asc",
						},
					},
				},
			},
			"temurin": {
				Enabled:          true,
				EndOfLifeProduct: "eclipse-temurin",
				PolicyFile:       "policies/temurin-policy.json",
				Verification: Verification{
					Enabled: true,
					Methods: VerificationMethods{
						Checksum: ChecksumVerification{
							Enabled:        true,
							Algorithm:      "sha256",
							FilePattern:    "{url}.sha256.txt",
							RemoteChecksum: false,
						},
						GPG: GPGVerification{
							Enabled:          true,
							Keyserver:        "keyserver.ubuntu.com",
							KeyID:            "",
							SignaturePattern: "{url}.sig",
						},
					},
				},
			},
		},
	}
}

// SaveConfig saves the configuration to a YAML file.
func SaveConfig(config *Config, filePath string) error {
	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file %s: %w", filePath, err)
	}
	return nil
}
