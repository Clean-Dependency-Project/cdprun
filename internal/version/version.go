// Package version provides semantic version validation and pattern matching
package version

import (
	"errors"
	"fmt"

	"github.com/Masterminds/semver/v3"
)

// String constants for version patterns
const (
	PatternMajorString      = "major"
	PatternMajorMinorString = "major_minor"
)

// String constants for operations (used in VersionError)
const (
	OpParseCheckVersion     = "parse_check_version"
	OpParseSupportedVersion = "parse_supported_version"
	OpValidatePattern       = "validate_pattern"
	OpExtractPattern        = "extract_pattern"
	OpValidateVersion       = "validate_version"
	OpParseVersion1         = "parse_version1"
	OpParseVersion2         = "parse_version2"
)

// VersionPattern defines how versions are stored and validated
type VersionPattern string

const (
	PatternMajor      VersionPattern = PatternMajorString      // Major versions (Temurin JDK, Python, Node.js, .NET)
	PatternMajorMinor VersionPattern = PatternMajorMinorString // Major.Minor versions (e.g., "3.13" for Python and Go)
)

// Custom error types for better error handling and comparison
var (
	ErrInvalidVersion     = errors.New("invalid version format")
	ErrInvalidPattern     = errors.New("invalid version pattern")
	ErrRuntimeNil         = errors.New("runtime cannot be nil")
	ErrNoVersionsProvided = errors.New("no versions provided")
)

// ErrRuntimeNotActive represents an error when runtime is not active
type ErrRuntimeNotActive struct {
	Runtime string
}

func (e ErrRuntimeNotActive) Error() string {
	return fmt.Sprintf("runtime %s is not active", e.Runtime)
}

func (e ErrRuntimeNotActive) Is(target error) bool {
	var runtimeErr ErrRuntimeNotActive
	return errors.As(target, &runtimeErr)
}

// ErrVersionParseFailed represents a version parsing error
type ErrVersionParseFailed struct {
	Version string
	Op      string
	Cause   error
}

func (e ErrVersionParseFailed) Error() string {
	return fmt.Sprintf("failed to parse version %s in operation %s: %v", e.Version, e.Op, e.Cause)
}

func (e ErrVersionParseFailed) Unwrap() error {
	return e.Cause
}

func (e ErrVersionParseFailed) Is(target error) bool {
	var parseErr ErrVersionParseFailed
	return errors.As(target, &parseErr)
}

// ErrPatternValidation represents a pattern validation error
type ErrPatternValidation struct {
	Pattern VersionPattern
	Op      string
}

func (e ErrPatternValidation) Error() string {
	return fmt.Sprintf("invalid pattern %s in operation %s", e.Pattern, e.Op)
}

func (e ErrPatternValidation) Is(target error) bool {
	var patternErr ErrPatternValidation
	return errors.As(target, &patternErr)
}

// VersionError provides context for version-related errors (legacy, kept for compatibility)
type VersionError struct {
	Op      string // Operation that failed
	Runtime string // Runtime name (optional)
	Version string // Version string
	Err     error  // Underlying error
}

func (e *VersionError) Error() string {
	if e.Runtime != "" {
		return fmt.Sprintf("version operation %s failed for %s:%s - %v",
			e.Op, e.Runtime, e.Version, e.Err)
	}
	return fmt.Sprintf("version operation %s failed for %s - %v",
		e.Op, e.Version, e.Err)
}

func (e *VersionError) Unwrap() error {
	return e.Err
}

func (e *VersionError) Is(target error) bool {
	if target == nil {
		return false
	}
	var verErr *VersionError
	if errors.As(target, &verErr) {
		return e.Op == verErr.Op && e.Runtime == verErr.Runtime && e.Version == verErr.Version
	}
	return errors.Is(e.Err, target)
}

// Validator provides version validation using semver
type Validator interface {
	// IsSupported checks if a version matches the supported version pattern
	// supportedVersion is the version that is supported by the policy
	// pattern is the version pattern that is supported by the policy
	// checkVersion is the version that is being checked
	// Example:
	// supportedVersion = "3.13"
	// pattern = PatternMajorMinor
	// checkVersion = "3.13.2"
	// Returns true if the checkVersion is supported by the policy
	// Returns false if the checkVersion is not supported by the policy
	// Returns an error if the checkVersion is not a valid semver
	IsSupported(supportedVersion string, pattern VersionPattern, checkVersion string) (bool, error)

	// ExtractPattern extracts version components based on pattern
	// version is the version that is being extracted
	// pattern is the version pattern that is being extracted
	// Example:
	// version = "3.13.2"
	// pattern = PatternMajorMinor
	// Returns the major version if pattern is PatternMajor
	// Returns the major.minor version if pattern is PatternMajorMinor
	// return is 3.13
	ExtractPattern(version string, pattern VersionPattern) (string, error)

	// ValidateVersion validates that a version string is valid semver
	ValidateVersion(version string) error

	// CompareVersions compares two versions (-1, 0, 1)
	// v1 is the first version
	// v2 is the second version
	// Example:
	// v1 = "3.13.2"
	// v2 = "3.13.3"
	// Returns -1 if v1 < v2
	// Returns 0 if v1 == v2
	CompareVersions(v1, v2 string) (int, error)

	// FilterVersionsByPattern filters a list of versions by pattern match
	// versions is the list of versions to filter
	// supportedVersion is the version that is supported by the policy
	// pattern is the version pattern that is supported by the policy
	// Example:
	// versions = ["3.13.2", "3.13.3", "3.14.0"]
	// supportedVersion = "3.13"
	// pattern = PatternMajorMinor
	// Returns the list of versions that are supported by the policy
	// returns ["3.13.2", "3.13.3"]
	FilterVersionsByPattern(versions []string, supportedVersion string, pattern VersionPattern) ([]string, error)
}

// semverValidator implements Validator using Masterminds/semver
type semverValidator struct{}

// New creates a new version validator
func New() Validator {
	return &semverValidator{}
}

// IsSupported checks if a version matches the supported version pattern
func (v *semverValidator) IsSupported(supportedVersion string, pattern VersionPattern, checkVersion string) (bool, error) {
	// Parse the version to check
	checkVer, err := semver.NewVersion(checkVersion)
	if err != nil {
		return false, ErrVersionParseFailed{
			Version: checkVersion,
			Op:      OpParseCheckVersion,
			Cause:   err,
		}
	}

	// Parse the supported version
	supportedVer, err := semver.NewVersion(supportedVersion)
	if err != nil {
		return false, ErrVersionParseFailed{
			Version: supportedVersion,
			Op:      OpParseSupportedVersion,
			Cause:   err,
		}
	}

	// Compare based on pattern
	switch pattern {
	case PatternMajor:
		// For PatternMajor, compare major numbers only
		return checkVer.Major() == supportedVer.Major(), nil
	case PatternMajorMinor:
		return checkVer.Major() == supportedVer.Major() &&
			checkVer.Minor() == supportedVer.Minor(), nil
	default:
		return false, ErrPatternValidation{
			Pattern: pattern,
			Op:      OpValidatePattern,
		}
	}
}

// ExtractPattern extracts version components based on pattern
func (v *semverValidator) ExtractPattern(version string, pattern VersionPattern) (string, error) {
	sv, err := semver.NewVersion(version)
	if err != nil {
		return "", ErrVersionParseFailed{
			Version: version,
			Op:      OpExtractPattern,
			Cause:   err,
		}
	}

	switch pattern {
	case PatternMajor:
		return fmt.Sprintf("%d", sv.Major()), nil
	case PatternMajorMinor:
		return fmt.Sprintf("%d.%d", sv.Major(), sv.Minor()), nil
	default:
		return "", ErrPatternValidation{
			Pattern: pattern,
			Op:      OpExtractPattern,
		}
	}
}

// ValidateVersion validates that a version string is valid semver
func (v *semverValidator) ValidateVersion(version string) error {
	_, err := semver.NewVersion(version)
	if err != nil {
		return ErrVersionParseFailed{
			Version: version,
			Op:      OpValidateVersion,
			Cause:   err,
		}
	}
	return nil
}

// CompareVersions compares two versions (-1 if v1 < v2, 0 if equal, 1 if v1 > v2)
func (v *semverValidator) CompareVersions(v1, v2 string) (int, error) {
	ver1, err := semver.NewVersion(v1)
	if err != nil {
		return 0, ErrVersionParseFailed{
			Version: v1,
			Op:      OpParseVersion1,
			Cause:   err,
		}
	}

	ver2, err := semver.NewVersion(v2)
	if err != nil {
		return 0, ErrVersionParseFailed{
			Version: v2,
			Op:      OpParseVersion2,
			Cause:   err,
		}
	}

	return ver1.Compare(ver2), nil
}

// FilterVersionsByPattern filters a list of versions by pattern match
func (v *semverValidator) FilterVersionsByPattern(versions []string, supportedVersion string, pattern VersionPattern) ([]string, error) {
	if len(versions) == 0 {
		return []string{}, nil
	}

	filtered := make([]string, 0, len(versions))
	for _, version := range versions {
		supported, err := v.IsSupported(supportedVersion, pattern, version)
		if err != nil {
			// Skip invalid versions rather than failing the entire operation
			continue
		}

		if supported {
			filtered = append(filtered, version)
		}
	}

	return filtered, nil
}

// Runtime represents a runtime configuration for version checking
type Runtime struct {
	Name           string
	VersionPattern VersionPattern
	Version        string
	IsActive       bool
}

// RuntimeChecker provides higher-level operations for runtime version checking
type RuntimeChecker struct {
	validator Validator
}

// NewRuntimeChecker creates a new runtime checker
func NewRuntimeChecker() *RuntimeChecker {
	return &RuntimeChecker{
		validator: New(),
	}
}

// CheckRuntimeVersion checks if a version is supported for a runtime
func (rc *RuntimeChecker) CheckRuntimeVersion(runtime *Runtime, version string) (bool, error) {
	if runtime == nil {
		return false, ErrRuntimeNil
	}

	if !runtime.IsActive {
		return false, ErrRuntimeNotActive{Runtime: runtime.Name}
	}

	return rc.validator.IsSupported(runtime.Version, runtime.VersionPattern, version)
}

// FilterUpstreamVersions filters upstream versions for a runtime
func (rc *RuntimeChecker) FilterUpstreamVersions(runtime *Runtime, upstreamVersions []string) ([]string, error) {
	if runtime == nil {
		return nil, ErrRuntimeNil
	}

	if !runtime.IsActive {
		return nil, ErrRuntimeNotActive{Runtime: runtime.Name}
	}

	return rc.validator.FilterVersionsByPattern(upstreamVersions, runtime.Version, runtime.VersionPattern)
}

// GetLatestVersion returns the latest version from a list of versions
func (rc *RuntimeChecker) GetLatestVersion(versions []string) (string, error) {
	if len(versions) == 0 {
		return "", ErrNoVersionsProvided
	}

	latest := versions[0]
	for _, version := range versions[1:] {
		cmp, err := rc.validator.CompareVersions(version, latest)
		if err != nil {
			continue // Skip invalid versions
		}
		if cmp > 0 {
			latest = version
		}
	}

	return latest, nil
}

// SortVersions sorts versions in ascending order
func (rc *RuntimeChecker) SortVersions(versions []string) ([]string, error) {
	if len(versions) == 0 {
		return []string{}, nil
	}

	// Convert to semver for sorting
	var semverVersions []*semver.Version
	validVersions := make([]string, 0, len(versions))

	for _, v := range versions {
		if sv, err := semver.NewVersion(v); err == nil {
			semverVersions = append(semverVersions, sv)
			validVersions = append(validVersions, v)
		}
	}

	// Sort using semver comparison
	for i := 0; i < len(semverVersions)-1; i++ {
		for j := i + 1; j < len(semverVersions); j++ {
			if semverVersions[i].GreaterThan(semverVersions[j]) {
				semverVersions[i], semverVersions[j] = semverVersions[j], semverVersions[i]
				validVersions[i], validVersions[j] = validVersions[j], validVersions[i]
			}
		}
	}

	return validVersions, nil
}
