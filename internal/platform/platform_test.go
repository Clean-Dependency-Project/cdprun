package platform

import (
	"runtime"
	"testing"
)

// TestPredefinedPlatforms tests that all expected platforms are defined
func TestPredefinedPlatforms(t *testing.T) {
	platforms := PredefinedPlatforms()

	// Verify we have exactly 6 predefined platforms
	if got := len(platforms); got != 6 {
		t.Fatalf("PredefinedPlatforms() count = %d, want 6", got)
	}

	// Verify each platform has required fields
	for i, p := range platforms {
		if p.OS == "" {
			t.Errorf("Platform[%d].OS is empty", i)
		}
		if p.Arch == "" {
			t.Errorf("Platform[%d].Arch is empty", i)
		}
		if p.FileExt == "" {
			t.Errorf("Platform[%d].FileExt is empty", i)
		}
		if p.DownloadName == "" {
			t.Errorf("Platform[%d].DownloadName is empty", i)
		}
		if p.Classifier == "" {
			t.Errorf("Platform[%d].Classifier is empty", i)
		}
	}

	// Verify specific platform combinations exist
	expectedCombinations := []struct {
		os   string
		arch string
		ext  string
	}{
		{"windows", "x64", "zip"},
		{"windows", "aarch64", "zip"},
		{"mac", "x64", "tar.gz"},
		{"mac", "aarch64", "tar.gz"},
		{"linux", "x64", "tar.gz"},
		{"linux", "aarch64", "tar.gz"},
	}

	for _, expected := range expectedCombinations {
		found := false
		for _, p := range platforms {
			if p.OS == expected.os && p.Arch == expected.arch && p.FileExt == expected.ext {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected platform %s-%s with extension %s not found",
				expected.os, expected.arch, expected.ext)
		}
	}
}

// TestFindPlatform tests finding platforms by classifier or OS-Arch format
func TestFindPlatform(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantOS      string
		wantArch    string
		wantErr     bool
		errContains string
	}{
		{
			name:     "find by classifier windows-x64",
			input:    "windows-x64",
			wantOS:   "windows",
			wantArch: "x64",
			wantErr:  false,
		},
		{
			name:     "find by classifier mac-aarch64",
			input:    "mac-aarch64",
			wantOS:   "mac",
			wantArch: "aarch64",
			wantErr:  false,
		},
		{
			name:     "find by classifier linux-x64",
			input:    "linux-x64",
			wantOS:   "linux",
			wantArch: "x64",
			wantErr:  false,
		},
		{
			name:     "find by classifier linux-aarch64",
			input:    "linux-aarch64",
			wantOS:   "linux",
			wantArch: "aarch64",
			wantErr:  false,
		},
		{
			name:     "find by classifier windows-aarch64",
			input:    "windows-aarch64",
			wantOS:   "windows",
			wantArch: "aarch64",
			wantErr:  false,
		},
		{
			name:     "find by classifier mac-x64",
			input:    "mac-x64",
			wantOS:   "mac",
			wantArch: "x64",
			wantErr:  false,
		},
		{
			name:        "invalid platform",
			input:       "invalid-platform",
			wantErr:     true,
			errContains: "unknown platform",
		},
		{
			name:        "empty string",
			input:       "",
			wantErr:     true,
			errContains: "unknown platform",
		},
		{
			name:        "unsupported OS",
			input:       "freebsd-x64",
			wantErr:     true,
			errContains: "unknown platform",
		},
		{
			name:        "unsupported arch",
			input:       "linux-arm",
			wantErr:     true,
			errContains: "unknown platform",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := FindPlatform(tt.input)

			if tt.wantErr {
				if err == nil {
					t.Errorf("FindPlatform(%q) error = nil, want error containing %q",
						tt.input, tt.errContains)
					return
				}
				if tt.errContains != "" && !contains(err.Error(), tt.errContains) {
					t.Errorf("FindPlatform(%q) error = %v, want error containing %q",
						tt.input, err, tt.errContains)
				}
				return
			}

			if err != nil {
				t.Errorf("FindPlatform(%q) unexpected error: %v", tt.input, err)
				return
			}

			if got.OS != tt.wantOS {
				t.Errorf("FindPlatform(%q).OS = %q, want %q", tt.input, got.OS, tt.wantOS)
			}
			if got.Arch != tt.wantArch {
				t.Errorf("FindPlatform(%q).Arch = %q, want %q", tt.input, got.Arch, tt.wantArch)
			}

			// Verify classifier matches
			expectedClassifier := tt.input
			if got.Classifier != expectedClassifier {
				t.Errorf("FindPlatform(%q).Classifier = %q, want %q",
					tt.input, got.Classifier, expectedClassifier)
			}
		})
	}
}

// TestCurrentPlatform tests detecting the current platform
func TestCurrentPlatform(t *testing.T) {
	p := CurrentPlatform()

	// Verify platform has all required fields
	if p.OS == "" {
		t.Error("CurrentPlatform().OS is empty")
	}
	if p.Arch == "" {
		t.Error("CurrentPlatform().Arch is empty")
	}
	if p.FileExt == "" {
		t.Error("CurrentPlatform().FileExt is empty")
	}
	if p.DownloadName == "" {
		t.Error("CurrentPlatform().DownloadName is empty")
	}
	if p.Classifier == "" {
		t.Error("CurrentPlatform().Classifier is empty")
	}

	// Verify OS matches expected based on runtime.GOOS
	expectedOS := mapOS(runtime.GOOS)
	if p.OS != expectedOS {
		t.Errorf("CurrentPlatform().OS = %q, want %q (from runtime.GOOS=%q)",
			p.OS, expectedOS, runtime.GOOS)
	}

	// Verify Arch matches expected based on runtime.GOARCH
	expectedArch := mapArch(runtime.GOARCH)
	if p.Arch != expectedArch {
		t.Errorf("CurrentPlatform().Arch = %q, want %q (from runtime.GOARCH=%q)",
			p.Arch, expectedArch, runtime.GOARCH)
	}

	// Verify file extension is correct for OS
	if p.OS == "windows" && p.FileExt != "zip" {
		t.Errorf("CurrentPlatform().FileExt = %q for Windows, want \"zip\"", p.FileExt)
	}
	if p.OS != "windows" && p.FileExt != "tar.gz" {
		t.Errorf("CurrentPlatform().FileExt = %q for %s, want \"tar.gz\"",
			p.FileExt, p.OS)
	}

	// Verify classifier format
	expectedClassifier := expectedOS + "-" + expectedArch
	if p.Classifier != expectedClassifier {
		t.Errorf("CurrentPlatform().Classifier = %q, want %q",
			p.Classifier, expectedClassifier)
	}
}

// TestResolvePlatforms tests resolving platform flags
func TestResolvePlatforms(t *testing.T) {
	tests := []struct {
		name      string
		flags     []string
		wantCount int
		wantErr   bool
		validate  func(*testing.T, []Platform)
	}{
		{
			name:      "all platforms",
			flags:     []string{"all"},
			wantCount: 6,
			wantErr:   false,
			validate: func(t *testing.T, platforms []Platform) {
				if len(platforms) != 6 {
					t.Errorf("ResolvePlatforms(\"all\") returned %d platforms, want 6",
						len(platforms))
				}
			},
		},
		{
			name:      "all platforms case insensitive",
			flags:     []string{"ALL"},
			wantCount: 6,
			wantErr:   false,
		},
		{
			name:      "empty flags - current platform",
			flags:     []string{},
			wantCount: 1,
			wantErr:   false,
			validate: func(t *testing.T, platforms []Platform) {
				if len(platforms) != 1 {
					t.Errorf("ResolvePlatforms([]) returned %d platforms, want 1",
						len(platforms))
					return
				}
				current := CurrentPlatform()
				if platforms[0].OS != current.OS || platforms[0].Arch != current.Arch {
					t.Errorf("ResolvePlatforms([]) = %s-%s, want current %s-%s",
						platforms[0].OS, platforms[0].Arch, current.OS, current.Arch)
				}
			},
		},
		{
			name:      "nil flags - current platform",
			flags:     nil,
			wantCount: 1,
			wantErr:   false,
		},
		{
			name:      "single valid platform",
			flags:     []string{"linux-x64"},
			wantCount: 1,
			wantErr:   false,
			validate: func(t *testing.T, platforms []Platform) {
				if platforms[0].OS != "linux" || platforms[0].Arch != "x64" {
					t.Errorf("ResolvePlatforms([\"linux-x64\"]) = %s-%s, want linux-x64",
						platforms[0].OS, platforms[0].Arch)
				}
			},
		},
		{
			name:      "multiple valid platforms",
			flags:     []string{"linux-x64", "mac-aarch64", "windows-x64"},
			wantCount: 3,
			wantErr:   false,
			validate: func(t *testing.T, platforms []Platform) {
				if len(platforms) != 3 {
					t.Errorf("ResolvePlatforms returned %d platforms, want 3",
						len(platforms))
					return
				}
				// Verify each requested platform is present
				expected := []struct{ os, arch string }{
					{"linux", "x64"},
					{"mac", "aarch64"},
					{"windows", "x64"},
				}
				for _, exp := range expected {
					found := false
					for _, p := range platforms {
						if p.OS == exp.os && p.Arch == exp.arch {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("Expected platform %s-%s not found in results",
							exp.os, exp.arch)
					}
				}
			},
		},
		{
			name:    "invalid platform",
			flags:   []string{"invalid-platform"},
			wantErr: true,
		},
		{
			name:    "mix of valid and invalid",
			flags:   []string{"linux-x64", "invalid-platform"},
			wantErr: true,
		},
		{
			name:    "empty string in flags",
			flags:   []string{""},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			platforms, err := ResolvePlatforms(tt.flags)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ResolvePlatforms(%v) error = nil, want error", tt.flags)
				}
				return
			}

			if err != nil {
				t.Errorf("ResolvePlatforms(%v) unexpected error: %v", tt.flags, err)
				return
			}

			if len(platforms) != tt.wantCount {
				t.Errorf("ResolvePlatforms(%v) returned %d platforms, want %d",
					tt.flags, len(platforms), tt.wantCount)
			}

			// Run custom validation if provided
			if tt.validate != nil {
				tt.validate(t, platforms)
			}

			// Verify all platforms have required fields
			for i, p := range platforms {
				if p.OS == "" || p.Arch == "" || p.FileExt == "" ||
					p.DownloadName == "" || p.Classifier == "" {
					t.Errorf("Platform[%d] has empty fields: %+v", i, p)
				}
			}
		})
	}
}

// TestMapOS tests OS name mapping
func TestMapOS(t *testing.T) {
	tests := []struct {
		goos string
		want string
	}{
		{"windows", "windows"},
		{"darwin", "mac"},
		{"linux", "linux"},
		{"freebsd", "linux"}, // default case
		{"openbsd", "linux"}, // default case
		{"", "linux"},        // default case
	}

	for _, tt := range tests {
		t.Run(tt.goos, func(t *testing.T) {
			got := mapOS(tt.goos)
			if got != tt.want {
				t.Errorf("mapOS(%q) = %q, want %q", tt.goos, got, tt.want)
			}
		})
	}
}

// TestMapArch tests architecture name mapping
func TestMapArch(t *testing.T) {
	tests := []struct {
		goarch string
		want   string
	}{
		{"arm64", "aarch64"},
		{"amd64", "x64"},
		{"386", "x64"},  // default case
		{"arm", "x64"},  // default case
		{"mips", "x64"}, // default case
		{"", "x64"},     // default case
	}

	for _, tt := range tests {
		t.Run(tt.goarch, func(t *testing.T) {
			got := mapArch(tt.goarch)
			if got != tt.want {
				t.Errorf("mapArch(%q) = %q, want %q", tt.goarch, got, tt.want)
			}
		})
	}
}

// TestBuildPlatform tests building a platform from OS and arch
func TestBuildPlatform(t *testing.T) {
	tests := []struct {
		name    string
		os      string
		arch    string
		wantExt string
	}{
		{
			name:    "windows platform uses zip",
			os:      "windows",
			arch:    "x64",
			wantExt: "zip",
		},
		{
			name:    "linux platform uses tar.gz",
			os:      "linux",
			arch:    "x64",
			wantExt: "tar.gz",
		},
		{
			name:    "mac platform uses tar.gz",
			os:      "mac",
			arch:    "aarch64",
			wantExt: "tar.gz",
		},
		{
			name:    "custom OS defaults to tar.gz",
			os:      "freebsd",
			arch:    "x64",
			wantExt: "tar.gz",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := buildPlatform(tt.os, tt.arch)

			if p.OS != tt.os {
				t.Errorf("buildPlatform(%q, %q).OS = %q, want %q",
					tt.os, tt.arch, p.OS, tt.os)
			}
			if p.Arch != tt.arch {
				t.Errorf("buildPlatform(%q, %q).Arch = %q, want %q",
					tt.os, tt.arch, p.Arch, tt.arch)
			}
			if p.FileExt != tt.wantExt {
				t.Errorf("buildPlatform(%q, %q).FileExt = %q, want %q",
					tt.os, tt.arch, p.FileExt, tt.wantExt)
			}
			if p.DownloadName != tt.os {
				t.Errorf("buildPlatform(%q, %q).DownloadName = %q, want %q",
					tt.os, tt.arch, p.DownloadName, tt.os)
			}

			expectedClassifier := tt.os + "-" + tt.arch
			if p.Classifier != expectedClassifier {
				t.Errorf("buildPlatform(%q, %q).Classifier = %q, want %q",
					tt.os, tt.arch, p.Classifier, expectedClassifier)
			}
		})
	}
}

// TestPlatformStruct tests the Platform struct field assignments
func TestPlatformStruct(t *testing.T) {
	p := Platform{
		OS:           "linux",
		Arch:         "x64",
		FileExt:      "tar.gz",
		DownloadName: "linux",
		Classifier:   "linux-x64",
	}

	if p.OS != "linux" {
		t.Errorf("Platform.OS = %q, want \"linux\"", p.OS)
	}
	if p.Arch != "x64" {
		t.Errorf("Platform.Arch = %q, want \"x64\"", p.Arch)
	}
	if p.FileExt != "tar.gz" {
		t.Errorf("Platform.FileExt = %q, want \"tar.gz\"", p.FileExt)
	}
	if p.DownloadName != "linux" {
		t.Errorf("Platform.DownloadName = %q, want \"linux\"", p.DownloadName)
	}
	if p.Classifier != "linux-x64" {
		t.Errorf("Platform.Classifier = %q, want \"linux-x64\"", p.Classifier)
	}

	// Test zero value
	var zero Platform
	if zero.OS != "" || zero.Arch != "" || zero.FileExt != "" ||
		zero.DownloadName != "" || zero.Classifier != "" {
		t.Error("Zero value Platform should have empty fields")
	}
}

// contains is a helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
