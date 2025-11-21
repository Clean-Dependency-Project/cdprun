// Package endoflife provides integration with the endoflife.date API
// for retrieving runtime version information and lifecycle data.
package endoflife

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/clean-dependency-project/cdprun/internal/version"
)

const (
	// DefaultBaseURL is the default endoflife.date API base URL
	DefaultBaseURL = "https://endoflife.date/api/v1"

	// DefaultTimeout is the default HTTP client timeout
	DefaultTimeout = 30 * time.Second

	// DefaultUserAgent is the default User-Agent header
	DefaultUserAgent = "cdprun/1.0"

	// MajorVersionPattern is the major version pattern
	MajorVersionPattern = "major"

	// MajorMinorVersionPattern is the major minor version pattern
	MajorMinorVersionPattern = "major_minor"
)

// Custom error types for better error handling
var (
	// ErrProductNotFound indicates the requested product was not found
	ErrProductNotFound = fmt.Errorf("product not found")

	// ErrInvalidResponse indicates the API response was invalid
	ErrInvalidResponse = fmt.Errorf("invalid API response")

	// ErrNetworkError indicates a network-related error
	ErrNetworkError = fmt.Errorf("network error")
)

// ErrAPIError represents an API-specific error
type ErrAPIError struct {
	StatusCode int
	Message    string
	Product    string
}

func (e ErrAPIError) Error() string {
	if e.Product != "" {
		return fmt.Sprintf("API error for product %s: %d %s", e.Product, e.StatusCode, e.Message)
	}
	return fmt.Sprintf("API error: %d %s", e.StatusCode, e.Message)
}

func (e ErrAPIError) Is(target error) bool {
	if target == ErrProductNotFound && e.StatusCode == 404 {
		return true
	}
	if target == ErrInvalidResponse && e.StatusCode >= 400 && e.StatusCode < 500 {
		return true
	}
	if target == ErrNetworkError && e.StatusCode >= 500 {
		return true
	}
	return false
}

// ErrPolicyValidation represents a policy validation error
type ErrPolicyValidation struct {
	Field   string
	Value   string
	Reason  string
	Runtime string
}

func (e ErrPolicyValidation) Error() string {
	return fmt.Sprintf("policy validation failed for %s.%s=%s: %s", e.Runtime, e.Field, e.Value, e.Reason)
}

// ProductInfo represents the product information from endoflife.date API
type ProductInfo struct {
	SchemaVersion string `json:"schema_version"`
	GeneratedAt   string `json:"generated_at"`
	LastModified  string `json:"last_modified"`
	Result        struct {
		Name           string       `json:"name"`
		Aliases        []string     `json:"aliases"`
		Label          string       `json:"label"`
		Category       string       `json:"category"`
		Tags           []string     `json:"tags"`
		VersionCommand string       `json:"versionCommand,omitempty"`
		Identifiers    []Identifier `json:"identifiers,omitempty"`
		Labels         Labels       `json:"labels,omitempty"`
		Links          Links        `json:"links,omitempty"`
		Releases       []Release    `json:"releases"`
	} `json:"result"`
}

// Identifier represents a package identifier
type Identifier struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// Labels represents the lifecycle labels
type Labels struct {
	EOAS         *string `json:"eoas"`
	Discontinued *string `json:"discontinued"`
	EOL          *string `json:"eol"`
	EOES         *string `json:"eoes"`
}

// Links represents related links
type Links struct {
	Icon          string `json:"icon,omitempty"`
	HTML          string `json:"html,omitempty"`
	ReleasePolicy string `json:"releasePolicy,omitempty"`
}

// Release represents a single release from the API
type Release struct {
	Name         string  `json:"name"`
	Codename     *string `json:"codename"`
	Label        string  `json:"label"`
	ReleaseDate  string  `json:"releaseDate"`
	IsLTS        bool    `json:"isLts"`
	LTSFrom      *string `json:"ltsFrom"`
	IsEOAS       bool    `json:"isEoas"`
	EOASFrom     *string `json:"eoasFrom"`
	IsEOL        bool    `json:"isEol"`
	EOLFrom      *string `json:"eolFrom"`
	IsEOES       *bool   `json:"isEoes"`
	EOESFrom     *string `json:"eoesFrom"`
	IsMaintained bool    `json:"isMaintained"`
	Latest       struct {
		Name string `json:"name"`
		Date string `json:"date"`
		Link string `json:"link"`
	} `json:"latest"`
}

// PolicyRuntime represents a runtime configuration in the policy file
type PolicyRuntime struct {
	Name           string                 `json:"name"`
	VersionPattern string                 `json:"version_pattern"`
	Versions       []PolicyVersion        `json:"versions"`
	Settings       map[string]interface{} `json:"settings,omitempty"`
}

// PolicyVersion represents a version configuration in the policy
type PolicyVersion struct {
	Version            string `json:"version"`
	ReleaseDate        string `json:"releaseDate,omitempty"`
	EOL                string `json:"eol,omitempty"`
	LatestPatchVersion string `json:"latest_patch_version,omitempty"`
	LatestReleaseDate  string `json:"latestReleaseDate,omitempty"`
	LTS                bool   `json:"lts"`
	Recommended        bool   `json:"recommended"`
	Supported          bool   `json:"supported"`
	UnderReview        bool   `json:"under_review"`
	Notes              string `json:"notes,omitempty"`
}

// Policy represents the complete policy configuration
type Policy struct {
	Version  string          `json:"version"`
	Updated  string          `json:"updated"`
	Runtimes []PolicyRuntime `json:"runtimes"`
}

// SingleRuntimePolicy represents a policy for a single runtime
type SingleRuntimePolicy struct {
	Version     string        `json:"version"`
	Updated     string        `json:"updated"`
	Description string        `json:"description,omitempty"`
	Runtime     PolicyRuntime `json:"runtime"`
}

// VersionInfo represents enriched version information combining policy and API data
type VersionInfo struct {
	Version        string
	LatestPatch    string
	IsSupported    bool
	IsRecommended  bool
	IsLTS          bool
	IsEOL          bool
	IsEOAS         bool // End of Active Support - indicates security-only releases
	IsMaintained   bool
	EOLDate        string
	ReleaseDate    string
	DownloadURLs   []string
	RuntimeName    string
	VersionPattern version.VersionPattern
}

// IsSecurityOnly returns true if this version only receives security fixes.
// This indicates that binary installers may not be available.
func (v *VersionInfo) IsSecurityOnly() bool {
	return v.IsEOAS && !v.IsEOL && v.IsMaintained
}

// ShouldSkipBinaryDownloads returns true if binary downloads should be skipped
// for this version. This is typically the case for security-only releases.
func (v *VersionInfo) ShouldSkipBinaryDownloads() bool {
	return v.IsSecurityOnly()
}

// GetLifecycleStatus returns a human-readable lifecycle status string
func (v *VersionInfo) GetLifecycleStatus() string {
	if v.IsEOL {
		return "End of Life"
	}
	if v.IsSecurityOnly() {
		return "Security Support Only"
	}
	if v.IsMaintained {
		return "Active Support"
	}
	return "Unknown"
}

// Client defines the interface for endoflife.date API client
type Client interface {
	// GetProductInfo retrieves product information for a given runtime
	GetProductInfo(ctx context.Context, product string) (*ProductInfo, error)

	// GetSupportedVersions returns supported versions based on policy
	GetSupportedVersions(ctx context.Context, runtime PolicyRuntime) ([]VersionInfo, error)

	// ValidatePolicy validates a policy configuration
	ValidatePolicy(policy *Policy) error

	// EnrichVersionInfo enriches policy versions with API data
	EnrichVersionInfo(ctx context.Context, runtime PolicyRuntime, policyVersion PolicyVersion) (*VersionInfo, error)
}

// HTTPClient defines the interface for HTTP operations
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// Config holds configuration for the endoflife client
type Config struct {
	BaseURL    string
	UserAgent  string
	Timeout    time.Duration
	HTTPClient HTTPClient
}

// DefaultConfig returns a default configuration
func DefaultConfig() Config {
	return Config{
		BaseURL:   DefaultBaseURL,
		UserAgent: DefaultUserAgent,
		Timeout:   DefaultTimeout,
		HTTPClient: &http.Client{
			Timeout: DefaultTimeout,
		},
	}
}

// client implements the Client interface
type client struct {
	config    Config
	validator version.Validator
}

// NewClient creates a new endoflife.date API client
func NewClient(config Config) Client {
	if config.BaseURL == "" {
		config.BaseURL = DefaultBaseURL
	}
	if config.UserAgent == "" {
		config.UserAgent = DefaultUserAgent
	}
	if config.Timeout == 0 {
		config.Timeout = DefaultTimeout
	}
	if config.HTTPClient == nil {
		config.HTTPClient = &http.Client{
			Timeout: config.Timeout,
		}
	}

	return &client{
		config:    config,
		validator: version.New(),
	}
}

// GetProductInfo retrieves product information for a given runtime
func (c *client) GetProductInfo(ctx context.Context, product string) (*ProductInfo, error) {
	if product == "" {
		return nil, ErrAPIError{
			StatusCode: 400,
			Message:    "product name cannot be empty",
			Product:    product,
		}
	}

	// Construct URL
	apiURL, err := url.JoinPath(c.config.BaseURL, "products", product)
	if err != nil {
		return nil, fmt.Errorf("failed to construct API URL: %w", err)
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.config.UserAgent)

	// Send request
	resp, err := c.config.HTTPClient.Do(req)
	if err != nil {
		return nil, ErrAPIError{
			StatusCode: 0,
			Message:    err.Error(),
			Product:    product,
		}
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	// Check status code
	if resp.StatusCode != http.StatusOK {
		return nil, ErrAPIError{
			StatusCode: resp.StatusCode,
			Message:    resp.Status,
			Product:    product,
		}
	}

	// Parse response
	var productInfo ProductInfo
	if err := json.NewDecoder(resp.Body).Decode(&productInfo); err != nil {
		return nil, ErrAPIError{
			StatusCode: resp.StatusCode,
			Message:    fmt.Sprintf("failed to decode response: %v", err),
			Product:    product,
		}
	}

	return &productInfo, nil
}

// GetSupportedVersions returns supported versions based on policy
func (c *client) GetSupportedVersions(ctx context.Context, runtime PolicyRuntime) ([]VersionInfo, error) {
	// Get product info from API
	productInfo, err := c.GetProductInfo(ctx, runtime.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to get product info for %s: %w", runtime.Name, err)
	}

	var supportedVersions []VersionInfo

	// Process each policy version
	for _, policyVersion := range runtime.Versions {
		if !policyVersion.Supported {
			continue
		}

		versionInfo, err := c.EnrichVersionInfo(ctx, runtime, policyVersion)
		if err != nil {
			// Log error but continue processing other versions
			continue
		}

		// Find matching release from API
		for _, release := range productInfo.Result.Releases {
			// Convert version pattern
			var versionPattern version.VersionPattern
			switch strings.ToLower(runtime.VersionPattern) {
			case MajorVersionPattern:
				versionPattern = version.PatternMajor
			case MajorMinorVersionPattern:
				versionPattern = version.PatternMajorMinor
			default:
				versionPattern = version.PatternMajorMinor
			}

			// Check if this release matches the policy version
			supported, err := c.validator.IsSupported(policyVersion.Version, versionPattern, release.Name)
			if err != nil {
				continue
			}

			if supported {
				// Update with latest patch version
				if release.Latest.Name != "" {
					versionInfo.LatestPatch = release.Latest.Name
				}

				// Update lifecycle status from API
				versionInfo.IsEOL = release.IsEOL
				versionInfo.IsEOAS = release.IsEOAS
				versionInfo.IsMaintained = release.IsMaintained
				versionInfo.IsLTS = release.IsLTS

				// Update dates
				if release.EOLFrom != nil && *release.EOLFrom != "" {
					versionInfo.EOLDate = *release.EOLFrom
				}
				if release.ReleaseDate != "" {
					versionInfo.ReleaseDate = release.ReleaseDate
				}

				break
			}
		}

		supportedVersions = append(supportedVersions, *versionInfo)
	}

	return supportedVersions, nil
}

// ValidatePolicy validates a policy configuration
func (c *client) ValidatePolicy(policy *Policy) error {
	if policy == nil {
		return ErrPolicyValidation{
			Field:  "policy",
			Value:  "<nil>",
			Reason: "policy cannot be nil",
		}
	}

	if policy.Version == "" {
		return ErrPolicyValidation{
			Field:  "version",
			Value:  "",
			Reason: "policy version cannot be empty",
		}
	}

	if len(policy.Runtimes) == 0 {
		return ErrPolicyValidation{
			Field:  "runtimes",
			Value:  "[]",
			Reason: "policy must contain at least one runtime",
		}
	}

	// Validate each runtime
	for _, runtime := range policy.Runtimes {
		if err := c.validateRuntime(runtime); err != nil {
			return err
		}
	}

	return nil
}

// validateRuntime validates a single runtime configuration
func (c *client) validateRuntime(runtime PolicyRuntime) error {
	if runtime.Name == "" {
		return ErrPolicyValidation{
			Field:   "name",
			Value:   "",
			Reason:  "runtime name cannot be empty",
			Runtime: runtime.Name,
		}
	}

	// Validate version pattern
	switch strings.ToLower(runtime.VersionPattern) {
	case MajorVersionPattern, MajorMinorVersionPattern:
		// Valid patterns
	default:
		return ErrPolicyValidation{
			Field:   "version_pattern",
			Value:   runtime.VersionPattern,
			Reason:  "must be 'major' or 'major_minor'",
			Runtime: runtime.Name,
		}
	}

	if len(runtime.Versions) == 0 {
		return ErrPolicyValidation{
			Field:   "versions",
			Value:   "[]",
			Reason:  "runtime must contain at least one version",
			Runtime: runtime.Name,
		}
	}

	// Validate each version
	for _, ver := range runtime.Versions {
		if err := c.validateVersion(runtime.Name, ver); err != nil {
			return err
		}
	}

	return nil
}

// validateVersion validates a single version configuration
func (c *client) validateVersion(runtimeName string, ver PolicyVersion) error {
	if ver.Version == "" {
		return ErrPolicyValidation{
			Field:   "version",
			Value:   "",
			Reason:  "version cannot be empty",
			Runtime: runtimeName,
		}
	}

	// Validate version format using semver
	if err := c.validator.ValidateVersion(ver.Version); err != nil {
		return ErrPolicyValidation{
			Field:   "version",
			Value:   ver.Version,
			Reason:  fmt.Sprintf("invalid semver format: %v", err),
			Runtime: runtimeName,
		}
	}

	return nil
}

// EnrichVersionInfo enriches policy versions with API data
func (c *client) EnrichVersionInfo(ctx context.Context, runtime PolicyRuntime, policyVersion PolicyVersion) (*VersionInfo, error) {
	// Convert version pattern
	var versionPattern version.VersionPattern
	switch strings.ToLower(runtime.VersionPattern) {
	case MajorVersionPattern:
		versionPattern = version.PatternMajor
	case MajorMinorVersionPattern:
		versionPattern = version.PatternMajorMinor
	default:
		versionPattern = version.PatternMajorMinor
	}

	versionInfo := &VersionInfo{
		Version:        policyVersion.Version,
		LatestPatch:    policyVersion.LatestPatchVersion,
		IsSupported:    policyVersion.Supported,
		IsRecommended:  policyVersion.Recommended,
		IsLTS:          policyVersion.LTS,
		RuntimeName:    runtime.Name,
		VersionPattern: versionPattern,
	}

	// Parse EOL date from policy if available
	if policyVersion.EOL != "" {
		versionInfo.EOLDate = policyVersion.EOL
	}

	return versionInfo, nil
}

// PolicyLoader defines the interface for loading policy configurations
type PolicyLoader interface {
	LoadPolicy(filePath string) (*Policy, error)
	LoadSingleRuntimePolicy(filePath string) (*SingleRuntimePolicy, error)
	LoadArrayPolicy(filePath string, runtimeName string, versionPattern string) (*Policy, error)
}

// JSONPolicyLoader implements PolicyLoader for JSON files
type JSONPolicyLoader struct{}

// NewJSONPolicyLoader creates a new JSON policy loader
func NewJSONPolicyLoader() PolicyLoader {
	return &JSONPolicyLoader{}
}

// LoadPolicy loads a policy from a JSON file
func (l *JSONPolicyLoader) LoadPolicy(filePath string) (*Policy, error) {
	if filePath == "" {
		return nil, ErrPolicyValidation{
			Field:  "filePath",
			Value:  "",
			Reason: "file path cannot be empty",
		}
	}

	// Read file
	data, err := readFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read policy file %s: %w", filePath, err)
	}

	// Parse JSON
	var policy Policy
	if err := json.Unmarshal(data, &policy); err != nil {
		return nil, fmt.Errorf("failed to parse policy JSON from %s: %w", filePath, err)
	}

	return &policy, nil
}

// LoadSingleRuntimePolicy loads a single runtime policy from a JSON file
func (l *JSONPolicyLoader) LoadSingleRuntimePolicy(filePath string) (*SingleRuntimePolicy, error) {
	if filePath == "" {
		return nil, ErrPolicyValidation{
			Field:  "filePath",
			Value:  "",
			Reason: "file path cannot be empty",
		}
	}

	// Read file
	data, err := readFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read single runtime policy file %s: %w", filePath, err)
	}

	// Parse JSON
	var policy SingleRuntimePolicy
	if err := json.Unmarshal(data, &policy); err != nil {
		return nil, fmt.Errorf("failed to parse single runtime policy JSON from %s: %w", filePath, err)
	}

	return &policy, nil
}

// ConvertToPolicy converts a SingleRuntimePolicy to a Policy
func (l *JSONPolicyLoader) ConvertToPolicy(singlePolicy *SingleRuntimePolicy) *Policy {
	return &Policy{
		Version:  singlePolicy.Version,
		Updated:  singlePolicy.Updated,
		Runtimes: []PolicyRuntime{singlePolicy.Runtime},
	}
}

// LoadArrayPolicy loads an array-based policy from a JSON file and converts it to a Policy
func (l *JSONPolicyLoader) LoadArrayPolicy(filePath string, runtimeName string, versionPattern string) (*Policy, error) {
	if filePath == "" {
		return nil, ErrPolicyValidation{
			Field:  "filePath",
			Value:  "",
			Reason: "file path cannot be empty",
		}
	}

	if runtimeName == "" {
		return nil, ErrPolicyValidation{
			Field:  "runtimeName",
			Value:  "",
			Reason: "runtime name cannot be empty",
		}
	}

	// Read file
	data, err := readFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read array policy file %s: %w", filePath, err)
	}

	// Parse JSON as array of PolicyVersion
	var versions []PolicyVersion
	if err := json.Unmarshal(data, &versions); err != nil {
		return nil, fmt.Errorf("failed to parse array policy JSON from %s: %w", filePath, err)
	}

	// Convert to Policy structure
	policy := &Policy{
		Version: "1.0.0", // Default version
		Updated: time.Now().Format(time.RFC3339),
		Runtimes: []PolicyRuntime{
			{
				Name:           runtimeName,
				VersionPattern: versionPattern,
				Versions:       versions,
			},
		},
	}

	return policy, nil
}

// FileReader defines the interface for reading files (for testing)
type FileReader func(filename string) ([]byte, error)

// Default file reader
var readFile FileReader = os.ReadFile

// SetFileReader allows setting a custom file reader (for testing)
func SetFileReader(reader FileReader) {
	readFile = reader
}
