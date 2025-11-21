package cli

import (
	"testing"

	"github.com/clean-dependency-project/cdprun/internal/platform"
)

// TestParsePlatforms tests the platform parsing logic.
func TestParsePlatforms(t *testing.T) {
	tests := []struct {
		name          string
		inputs        []string
		wantCount     int
		wantErr       bool
		checkPlatform func(*testing.T, []platform.Platform)
	}{
		{
			name:      "single platform",
			inputs:    []string{"linux-x64"},
			wantCount: 1,
			wantErr:   false,
			checkPlatform: func(t *testing.T, platforms []platform.Platform) {
				if platforms[0].OS != "linux" {
					t.Errorf("Expected OS linux, got %s", platforms[0].OS)
				}
				if platforms[0].Arch != "x64" {
					t.Errorf("Expected Arch x64, got %s", platforms[0].Arch)
				}
				if platforms[0].Classifier != "linux-x64" {
					t.Errorf("Expected Classifier linux-x64, got %s", platforms[0].Classifier)
				}
			},
		},
		{
			name:      "multiple platforms",
			inputs:    []string{"linux-x64", "mac-aarch64", "windows-x64"},
			wantCount: 3,
			wantErr:   false,
		},
		{
			name:      "empty input returns current platform",
			inputs:    []string{},
			wantCount: 1, // Returns current platform
			wantErr:   false,
		},
		{
			name:    "invalid platform format - no dash",
			inputs:  []string{"invalid"},
			wantErr: true,
		},
		{
			name:    "invalid platform format - empty string",
			inputs:  []string{""},
			wantErr: true,
		},
		{
			name:    "invalid platform format - only os",
			inputs:  []string{"linux-"},
			wantErr: true,
		},
		{
			name:    "invalid platform format - only arch",
			inputs:  []string{"-x64"},
			wantErr: true,
		},
		{
			name:    "mixed valid and invalid",
			inputs:  []string{"linux-x64", "invalid", "darwin-arm64"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			platforms, err := parsePlatforms(tt.inputs)

			if tt.wantErr {
				if err == nil {
					t.Errorf("parsePlatforms() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("parsePlatforms() unexpected error: %v", err)
				return
			}

			if len(platforms) != tt.wantCount {
				t.Errorf("parsePlatforms() got %d platforms, want %d", len(platforms), tt.wantCount)
			}

			if tt.checkPlatform != nil {
				tt.checkPlatform(t, platforms)
			}
		})
	}
}

// TestGetClassifiersFromPlatforms tests classifier extraction from platforms.
func TestGetClassifiersFromPlatforms(t *testing.T) {
	tests := []struct {
		name       string
		platforms  []platform.Platform
		wantCount  int
		wantValues []string
	}{
		{
			name: "single platform",
			platforms: []platform.Platform{
				{OS: "linux", Arch: "x64", Classifier: "linux-x64"},
			},
			wantCount:  1,
			wantValues: []string{"linux-x64"},
		},
		{
			name: "multiple platforms",
			platforms: []platform.Platform{
				{OS: "linux", Arch: "x64", Classifier: "linux-x64"},
				{OS: "mac", Arch: "aarch64", Classifier: "mac-aarch64"},
				{OS: "windows", Arch: "x64", Classifier: "windows-x64"},
			},
			wantCount:  3,
			wantValues: []string{"linux-x64", "mac-aarch64", "windows-x64"},
		},
		{
			name:       "empty platforms",
			platforms:  []platform.Platform{},
			wantCount:  0,
			wantValues: []string{},
		},
		{
			name: "platforms with empty classifier",
			platforms: []platform.Platform{
				{OS: "linux", Arch: "x64", Classifier: ""},
				{OS: "mac", Arch: "aarch64", Classifier: "mac-aarch64"},
			},
			wantCount:  2,
			wantValues: []string{"", "mac-aarch64"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			classifiers := getClassifiersFromPlatforms(tt.platforms)

			if len(classifiers) != tt.wantCount {
				t.Errorf("getClassifiersFromPlatforms() got %d classifiers, want %d", len(classifiers), tt.wantCount)
			}

			for i, want := range tt.wantValues {
				if i >= len(classifiers) {
					t.Errorf("getClassifiersFromPlatforms() missing classifier at index %d", i)
					continue
				}
				if classifiers[i] != want {
					t.Errorf("getClassifiersFromPlatforms()[%d] = %s, want %s", i, classifiers[i], want)
				}
			}
		})
	}
}
