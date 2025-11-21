package version

import (
	"errors"
	"testing"
)

func TestVersionPattern_Constants(t *testing.T) {
	tests := []struct {
		pattern  VersionPattern
		expected string
	}{
		{PatternMajor, PatternMajorString},
		{PatternMajorMinor, PatternMajorMinorString},
	}

	for _, tt := range tests {
		t.Run(string(tt.pattern), func(t *testing.T) {
			if string(tt.pattern) != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, string(tt.pattern))
			}
		})
	}
}

func TestStringConstants(t *testing.T) {
	tests := []struct {
		name     string
		constant string
		expected string
	}{
		{"PatternMajorString", PatternMajorString, "major"},
		{"PatternMajorMinorString", PatternMajorMinorString, "major_minor"},
		{"OpParseCheckVersion", OpParseCheckVersion, "parse_check_version"},
		{"OpParseSupportedVersion", OpParseSupportedVersion, "parse_supported_version"},
		{"OpValidatePattern", OpValidatePattern, "validate_pattern"},
		{"OpExtractPattern", OpExtractPattern, "extract_pattern"},
		{"OpValidateVersion", OpValidateVersion, "validate_version"},
		{"OpParseVersion1", OpParseVersion1, "parse_version1"},
		{"OpParseVersion2", OpParseVersion2, "parse_version2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.constant != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, tt.constant)
			}
		})
	}
}

func TestErrRuntimeNotActive(t *testing.T) {
	runtime := "python"
	err := ErrRuntimeNotActive{Runtime: runtime}

	expectedMsg := "runtime python is not active"
	if err.Error() != expectedMsg {
		t.Errorf("expected %q, got %q", expectedMsg, err.Error())
	}

	// Test Is method
	var target ErrRuntimeNotActive
	if !errors.As(err, &target) {
		t.Error("expected ErrRuntimeNotActive to match itself")
	}

	if !err.Is(ErrRuntimeNotActive{}) {
		t.Error("expected Is method to work correctly")
	}
}

func TestErrVersionParseFailed(t *testing.T) {
	version := "invalid"
	op := OpValidateVersion
	cause := errors.New("test cause")

	err := ErrVersionParseFailed{
		Version: version,
		Op:      op,
		Cause:   cause,
	}

	expectedMsg := "failed to parse version invalid in operation validate_version: test cause"
	if err.Error() != expectedMsg {
		t.Errorf("expected %q, got %q", expectedMsg, err.Error())
	}

	// Test Unwrap
	if !errors.Is(err, cause) {
		t.Error("expected error to unwrap to cause")
	}

	// Test Is method
	var target ErrVersionParseFailed
	if !errors.As(err, &target) {
		t.Error("expected ErrVersionParseFailed to match itself")
	}
}

func TestErrPatternValidation(t *testing.T) {
	pattern := VersionPattern("invalid")
	op := OpValidatePattern

	err := ErrPatternValidation{
		Pattern: pattern,
		Op:      op,
	}

	expectedMsg := "invalid pattern invalid in operation validate_pattern"
	if err.Error() != expectedMsg {
		t.Errorf("expected %q, got %q", expectedMsg, err.Error())
	}

	// Test Is method
	var target ErrPatternValidation
	if !errors.As(err, &target) {
		t.Error("expected ErrPatternValidation to match itself")
	}
}

func TestVersionError_Legacy(t *testing.T) {
	baseErr := errors.New("base error")

	tests := []struct {
		name        string
		err         *VersionError
		expectedMsg string
	}{
		{
			name: "with runtime",
			err: &VersionError{
				Op:      "test_op",
				Runtime: "python",
				Version: "3.13.2",
				Err:     baseErr,
			},
			expectedMsg: "version operation test_op failed for python:3.13.2 - base error",
		},
		{
			name: "without runtime",
			err: &VersionError{
				Op:      "test_op",
				Version: "3.13.2",
				Err:     baseErr,
			},
			expectedMsg: "version operation test_op failed for 3.13.2 - base error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err.Error() != tt.expectedMsg {
				t.Errorf("expected %q, got %q", tt.expectedMsg, tt.err.Error())
			}

			if !errors.Is(tt.err, baseErr) {
				t.Error("expected VersionError to unwrap to base error")
			}
		})
	}
}

func TestVersionError_Is(t *testing.T) {
	err1 := &VersionError{Op: "op1", Runtime: "python", Version: "1.0.0"}
	err2 := &VersionError{Op: "op1", Runtime: "python", Version: "1.0.0"}
	err3 := &VersionError{Op: "op2", Runtime: "python", Version: "1.0.0"}

	if !err1.Is(err2) {
		t.Error("expected identical VersionErrors to match")
	}

	if err1.Is(err3) {
		t.Error("expected different VersionErrors not to match")
	}

	if err1.Is(nil) {
		t.Error("expected VersionError not to match nil")
	}
}

func TestValidator_IsSupported(t *testing.T) {
	validator := New()

	tests := []struct {
		name              string
		supportedVersion  string
		pattern           VersionPattern
		checkVersion      string
		expected          bool
		expectError       bool
		checkParseError   bool
		checkPatternError bool
	}{
		{
			name:             "major pattern match",
			supportedVersion: "3",
			pattern:          PatternMajor,
			checkVersion:     "3.13.2",
			expected:         true,
		},
		{
			name:             "major pattern no match",
			supportedVersion: "3",
			pattern:          PatternMajor,
			checkVersion:     "2.12.1",
			expected:         false,
		},
		{
			name:             "major minor pattern match",
			supportedVersion: "1.24",
			pattern:          PatternMajorMinor,
			checkVersion:     "1.24.3",
			expected:         true,
		},
		{
			name:             "major minor pattern no match - different minor",
			supportedVersion: "1.24",
			pattern:          PatternMajorMinor,
			checkVersion:     "1.23.0",
			expected:         false,
		},
		{
			name:             "major minor pattern no match - different major",
			supportedVersion: "1.24",
			pattern:          PatternMajorMinor,
			checkVersion:     "2.24.0",
			expected:         false,
		},
		{
			name:             "invalid check version",
			supportedVersion: "3",
			pattern:          PatternMajor,
			checkVersion:     "invalid-version",
			expectError:      true,
			checkParseError:  true,
		},
		{
			name:             "invalid supported version",
			supportedVersion: "invalid",
			pattern:          PatternMajor,
			checkVersion:     "3.13.0",
			expectError:      true,
			checkParseError:  true,
		},
		{
			name:              "unknown pattern",
			supportedVersion:  "3",
			pattern:           "unknown",
			checkVersion:      "3.13.0",
			expectError:       true,
			checkPatternError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			supported, err := validator.IsSupported(tt.supportedVersion, tt.pattern, tt.checkVersion)
			if tt.expectError {
				if err == nil {
					t.Error("expected error, got nil")
					return
				}

				if tt.checkParseError {
					var parseErr ErrVersionParseFailed
					if !errors.As(err, &parseErr) {
						t.Errorf("expected ErrVersionParseFailed, got %T", err)
					}
				}

				if tt.checkPatternError {
					var patternErr ErrPatternValidation
					if !errors.As(err, &patternErr) {
						t.Errorf("expected ErrPatternValidation, got %T", err)
					}
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if supported != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, supported)
			}
		})
	}
}

func TestValidator_ExtractPattern(t *testing.T) {
	validator := New()

	tests := []struct {
		name              string
		version           string
		pattern           VersionPattern
		expected          string
		expectError       bool
		checkParseError   bool
		checkPatternError bool
	}{
		{
			name:     "major pattern",
			version:  "3.13.2",
			pattern:  PatternMajor,
			expected: "3",
		},
		{
			name:     "major minor pattern",
			version:  "1.24.3",
			pattern:  PatternMajorMinor,
			expected: "1.24",
		},
		{
			name:            "invalid version",
			version:         "invalid",
			pattern:         PatternMajor,
			expectError:     true,
			checkParseError: true,
		},
		{
			name:              "unknown pattern",
			version:           "1.0.0",
			pattern:           "unknown",
			expectError:       true,
			checkPatternError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := validator.ExtractPattern(tt.version, tt.pattern)
			if tt.expectError {
				if err == nil {
					t.Error("expected error, got nil")
					return
				}

				if tt.checkParseError {
					var parseErr ErrVersionParseFailed
					if !errors.As(err, &parseErr) {
						t.Errorf("expected ErrVersionParseFailed, got %T", err)
					}
				}

				if tt.checkPatternError {
					var patternErr ErrPatternValidation
					if !errors.As(err, &patternErr) {
						t.Errorf("expected ErrPatternValidation, got %T", err)
					}
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if result != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestValidator_ValidateVersion(t *testing.T) {
	validator := New()

	tests := []struct {
		name            string
		version         string
		expectError     bool
		checkParseError bool
	}{
		{"valid version", "1.0.0", false, false},
		{"valid version with v prefix", "v1.0.0", false, false},
		{"valid pre-release", "1.0.0-alpha", false, false},
		{"valid version with build", "1.0.0+build", false, false},
		{"valid single number", "1", false, false},
		{"empty version", "", true, true},
		{"invalid version", "invalid", true, true},
		{"non-numeric version", "a.b.c", true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateVersion(tt.version)
			if tt.expectError {
				if err == nil {
					t.Error("expected error, got nil")
					return
				}

				if tt.checkParseError {
					var parseErr ErrVersionParseFailed
					if !errors.As(err, &parseErr) {
						t.Errorf("expected ErrVersionParseFailed, got %T", err)
					}
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestValidator_CompareVersions(t *testing.T) {
	validator := New()

	tests := []struct {
		name            string
		v1              string
		v2              string
		expected        int
		expectError     bool
		checkParseError bool
	}{
		{"v1 greater than v2", "2.0.0", "1.0.0", 1, false, false},
		{"v1 less than v2", "1.0.0", "2.0.0", -1, false, false},
		{"versions equal", "1.0.0", "1.0.0", 0, false, false},
		{"with pre-release", "1.0.0-alpha", "1.0.0", -1, false, false},
		{"with build metadata", "1.0.0+build1", "1.0.0+build2", 0, false, false},
		{"invalid v1", "invalid", "1.0.0", 0, true, true},
		{"invalid v2", "1.0.0", "invalid", 0, true, true},
		{"both invalid", "invalid1", "invalid2", 0, true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := validator.CompareVersions(tt.v1, tt.v2)
			if tt.expectError {
				if err == nil {
					t.Error("expected error, got nil")
					return
				}

				if tt.checkParseError {
					var parseErr ErrVersionParseFailed
					if !errors.As(err, &parseErr) {
						t.Errorf("expected ErrVersionParseFailed, got %T", err)
					}
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if result != tt.expected {
				t.Errorf("expected %d, got %d", tt.expected, result)
			}
		})
	}
}

func TestValidator_FilterVersionsByPattern(t *testing.T) {
	validator := New()

	tests := []struct {
		name             string
		versions         []string
		supportedVersion string
		pattern          VersionPattern
		expected         []string
	}{
		{
			name:             "major pattern filtering",
			versions:         []string{"3.13.0", "3.13.1", "3.12.5", "3.14.0", "invalid"},
			supportedVersion: "3",
			pattern:          PatternMajor,
			expected:         []string{"3.13.0", "3.13.1", "3.12.5", "3.14.0"},
		},
		{
			name:             "major minor pattern filtering",
			versions:         []string{"1.24.0", "1.24.1", "1.23.5", "1.25.0"},
			supportedVersion: "1.24",
			pattern:          PatternMajorMinor,
			expected:         []string{"1.24.0", "1.24.1"},
		},
		{
			name:             "no matching versions",
			versions:         []string{"2.0.0", "3.0.0"},
			supportedVersion: "1",
			pattern:          PatternMajor,
			expected:         []string{},
		},
		{
			name:             "empty input",
			versions:         []string{},
			supportedVersion: "1",
			pattern:          PatternMajor,
			expected:         []string{},
		},
		{
			name:             "all invalid versions",
			versions:         []string{"invalid1", "invalid2", "invalid3"},
			supportedVersion: "1",
			pattern:          PatternMajor,
			expected:         []string{},
		},
		{
			name:             "mixed valid and invalid",
			versions:         []string{"1.0.0", "invalid", "1.0.1", "bad.version"},
			supportedVersion: "1",
			pattern:          PatternMajor,
			expected:         []string{"1.0.0", "1.0.1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := validator.FilterVersionsByPattern(tt.versions, tt.supportedVersion, tt.pattern)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if len(result) != len(tt.expected) {
				t.Errorf("expected %d results, got %d", len(tt.expected), len(result))
				return
			}

			for i, expected := range tt.expected {
				if result[i] != expected {
					t.Errorf("expected result[%d] = %s, got %s", i, expected, result[i])
				}
			}
		})
	}
}

func TestRuntimeChecker_CheckRuntimeVersion(t *testing.T) {
	checker := NewRuntimeChecker()

	tests := []struct {
		name             string
		runtime          *Runtime
		version          string
		expected         bool
		expectError      bool
		checkNilError    bool
		checkActiveError bool
		checkParseError  bool
	}{
		{
			name: "active runtime supported version",
			runtime: &Runtime{
				Name:           "python",
				VersionPattern: PatternMajor,
				Version:        "3",
				IsActive:       true,
			},
			version:  "3.13.2",
			expected: true,
		},
		{
			name: "active runtime unsupported version",
			runtime: &Runtime{
				Name:           "python",
				VersionPattern: PatternMajor,
				Version:        "3",
				IsActive:       true,
			},
			version:  "2.12.1",
			expected: false,
		},
		{
			name: "inactive runtime",
			runtime: &Runtime{
				Name:           "python",
				VersionPattern: PatternMajor,
				Version:        "3",
				IsActive:       false,
			},
			version:          "3.13.2",
			expectError:      true,
			checkActiveError: true,
		},
		{
			name:          "nil runtime",
			runtime:       nil,
			version:       "3.13.2",
			expectError:   true,
			checkNilError: true,
		},
		{
			name: "invalid version format",
			runtime: &Runtime{
				Name:           "python",
				VersionPattern: PatternMajor,
				Version:        "3",
				IsActive:       true,
			},
			version:         "invalid",
			expectError:     true,
			checkParseError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := checker.CheckRuntimeVersion(tt.runtime, tt.version)
			if tt.expectError {
				if err == nil {
					t.Error("expected error, got nil")
					return
				}

				if tt.checkNilError && !errors.Is(err, ErrRuntimeNil) {
					t.Errorf("expected ErrRuntimeNil, got %v", err)
				}

				if tt.checkActiveError {
					var activeErr ErrRuntimeNotActive
					if !errors.As(err, &activeErr) {
						t.Errorf("expected ErrRuntimeNotActive, got %T", err)
					}
				}

				if tt.checkParseError {
					var parseErr ErrVersionParseFailed
					if !errors.As(err, &parseErr) {
						t.Errorf("expected ErrVersionParseFailed, got %T", err)
					}
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestRuntimeChecker_FilterUpstreamVersions(t *testing.T) {
	checker := NewRuntimeChecker()

	tests := []struct {
		name             string
		runtime          *Runtime
		upstreamVersions []string
		expected         []string
		expectError      bool
		checkNilError    bool
		checkActiveError bool
	}{
		{
			name: "active runtime filtering",
			runtime: &Runtime{
				Name:           "python",
				VersionPattern: PatternMajor,
				Version:        "3",
				IsActive:       true,
			},
			upstreamVersions: []string{"3.13.0", "3.13.1", "3.12.5", "3.14.0"},
			expected:         []string{"3.13.0", "3.13.1", "3.12.5", "3.14.0"},
		},
		{
			name: "inactive runtime",
			runtime: &Runtime{
				Name:           "python",
				VersionPattern: PatternMajor,
				Version:        "3",
				IsActive:       false,
			},
			upstreamVersions: []string{"3.13.0"},
			expectError:      true,
			checkActiveError: true,
		},
		{
			name:             "nil runtime",
			runtime:          nil,
			upstreamVersions: []string{"3.13.0"},
			expectError:      true,
			checkNilError:    true,
		},
		{
			name: "empty upstream versions",
			runtime: &Runtime{
				Name:           "python",
				VersionPattern: PatternMajor,
				Version:        "3",
				IsActive:       true,
			},
			upstreamVersions: []string{},
			expected:         []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := checker.FilterUpstreamVersions(tt.runtime, tt.upstreamVersions)
			if tt.expectError {
				if err == nil {
					t.Error("expected error, got nil")
					return
				}

				if tt.checkNilError && !errors.Is(err, ErrRuntimeNil) {
					t.Errorf("expected ErrRuntimeNil, got %v", err)
				}

				if tt.checkActiveError {
					var activeErr ErrRuntimeNotActive
					if !errors.As(err, &activeErr) {
						t.Errorf("expected ErrRuntimeNotActive, got %T", err)
					}
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if len(result) != len(tt.expected) {
				t.Errorf("expected %d results, got %d", len(tt.expected), len(result))
				return
			}

			for i, expected := range tt.expected {
				if result[i] != expected {
					t.Errorf("expected result[%d] = %s, got %s", i, expected, result[i])
				}
			}
		})
	}
}

func TestRuntimeChecker_GetLatestVersion(t *testing.T) {
	checker := NewRuntimeChecker()

	tests := []struct {
		name            string
		versions        []string
		expected        string
		expectError     bool
		expectedErrType error
	}{
		{
			name:     "valid versions",
			versions: []string{"1.0.0", "2.0.0", "1.5.0"},
			expected: "2.0.0",
		},
		{
			name:     "single version",
			versions: []string{"1.0.0"},
			expected: "1.0.0",
		},
		{
			name:            "empty versions",
			versions:        []string{},
			expectError:     true,
			expectedErrType: ErrNoVersionsProvided,
		},
		{
			name:     "versions with invalid",
			versions: []string{"1.0.0", "invalid", "2.0.0"},
			expected: "2.0.0",
		},
		{
			name:     "all invalid versions",
			versions: []string{"invalid1", "invalid2"},
			expected: "invalid1", // Returns first when all are invalid
		},
		{
			name:     "pre-release versions",
			versions: []string{"1.0.0-alpha", "1.0.0", "1.0.0-beta"},
			expected: "1.0.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := checker.GetLatestVersion(tt.versions)
			if tt.expectError {
				if err == nil {
					t.Error("expected error, got nil")
					return
				}

				if !errors.Is(err, tt.expectedErrType) {
					t.Errorf("expected error type %T, got %T", tt.expectedErrType, err)
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if result != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestRuntimeChecker_SortVersions(t *testing.T) {
	checker := NewRuntimeChecker()

	tests := []struct {
		name     string
		versions []string
		expected []string
	}{
		{
			name:     "unsorted versions",
			versions: []string{"2.0.0", "1.0.0", "1.5.0"},
			expected: []string{"1.0.0", "1.5.0", "2.0.0"},
		},
		{
			name:     "already sorted",
			versions: []string{"1.0.0", "1.5.0", "2.0.0"},
			expected: []string{"1.0.0", "1.5.0", "2.0.0"},
		},
		{
			name:     "empty versions",
			versions: []string{},
			expected: []string{},
		},
		{
			name:     "single version",
			versions: []string{"1.0.0"},
			expected: []string{"1.0.0"},
		},
		{
			name:     "versions with invalid (filtered out)",
			versions: []string{"2.0.0", "invalid", "1.0.0"},
			expected: []string{"1.0.0", "2.0.0"},
		},
		{
			name:     "all invalid versions",
			versions: []string{"invalid1", "invalid2", "invalid3"},
			expected: []string{},
		},
		{
			name:     "pre-release versions",
			versions: []string{"1.0.0", "1.0.0-alpha", "1.0.0-beta"},
			expected: []string{"1.0.0-alpha", "1.0.0-beta", "1.0.0"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := checker.SortVersions(tt.versions)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if len(result) != len(tt.expected) {
				t.Errorf("expected %d results, got %d", len(tt.expected), len(result))
				return
			}

			for i, expected := range tt.expected {
				if result[i] != expected {
					t.Errorf("expected result[%d] = %s, got %s", i, expected, result[i])
				}
			}
		})
	}
}

func TestNew(t *testing.T) {
	validator := New()
	if validator == nil {
		t.Error("expected non-nil validator")
	}

	// Test that it implements the interface
	_ = Validator(validator)
}

func TestNewRuntimeChecker(t *testing.T) {
	checker := NewRuntimeChecker()
	if checker == nil {
		t.Fatal("expected non-nil runtime checker")
	}

	if checker.validator == nil {
		t.Error("expected runtime checker to have a validator")
	}
}
