package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadIgnoreConfig(t *testing.T) {
	tests := []struct {
		name     string
		filePath string
		content  string
		format   string // "json" or "yaml"
		wantErr  bool
		wantLen  int
	}{
		{
			name:     "empty file path",
			filePath: "",
			wantErr:  false,
			wantLen:  0,
		},
		{
			name:     "valid JSON file",
			filePath: "test.json",
			content:  `{"nodejs": {"linux": ["22.0"]}}`,
			format:   "json",
			wantErr:  false,
			wantLen:  1,
		},
		{
			name:     "valid YAML file",
			filePath: "test.yaml",
			content:  "nodejs:\n  linux:\n    - \"22.0\"",
			format:   "yaml",
			wantErr:  false,
			wantLen:  1,
		},
		{
			name:     "valid YML file",
			filePath: "test.yml",
			content:  "nodejs:\n  linux:\n    - \"22.0\"",
			format:   "yaml",
			wantErr:  false,
			wantLen:  1,
		},
		{
			name:     "invalid JSON",
			filePath: "test.json",
			content:  `{"nodejs": {invalid json}}`,
			format:   "json",
			wantErr:  true,
		},
		{
			name:     "invalid YAML",
			filePath: "test.yaml",
			content:  "nodejs:\n  linux: [invalid",
			format:   "yaml",
			wantErr:  true,
		},
		{
			name:     "file not found",
			filePath: "nonexistent.json",
			wantErr:  true,
		},
		{
			name:     "complex nested structure",
			filePath: "test.json",
			content:  `{"nodejs": {"all": ["22"], "windows": ["20"], "mac": {"all": ["18"], "aarch64": ["20.11"]}}}`,
			format:   "json",
			wantErr:  false,
			wantLen:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var filePath string
			if tt.filePath != "" && tt.filePath != "nonexistent.json" {
				tempDir := t.TempDir()
				filePath = filepath.Join(tempDir, tt.filePath)
				if err := os.WriteFile(filePath, []byte(tt.content), 0644); err != nil {
					t.Fatalf("Failed to create test file: %v", err)
				}
			} else if tt.filePath == "nonexistent.json" {
				tempDir := t.TempDir()
				filePath = filepath.Join(tempDir, tt.filePath)
			} else {
				filePath = tt.filePath
			}

			config, err := LoadIgnoreConfig(filePath)
			if tt.wantErr {
				if err == nil {
					t.Errorf("LoadIgnoreConfig() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("LoadIgnoreConfig() unexpected error: %v", err)
				return
			}
			if len(config) != tt.wantLen {
				t.Errorf("LoadIgnoreConfig() config length = %d, want %d", len(config), tt.wantLen)
			}
		})
	}
}

func TestIsVersionIgnored(t *testing.T) {
	config := IgnoreConfig{
		"nodejs": map[string]any{
			"linux": []any{"22.0"},
		},
	}

	// This function always returns false currently
	if config.IsVersionIgnored("nodejs", "22.0") {
		t.Error("IsVersionIgnored() expected false (not implemented), got true")
	}
	if config.IsVersionIgnored("nodejs", "21.0") {
		t.Error("IsVersionIgnored() expected false, got true")
	}
	if config.IsVersionIgnored("python", "3.13") {
		t.Error("IsVersionIgnored() expected false, got true")
	}
}

func TestIsPlatformIgnored(t *testing.T) {
	tests := []struct {
		name       string
		config     IgnoreConfig
		runtime    string
		version    string
		osName     string
		arch       string
		wantIgnored bool
	}{
		{
			name: "runtime not in config",
			config: IgnoreConfig{
				"python": map[string]any{
					"linux": []any{"3.13"},
				},
			},
			runtime:     "nodejs",
			version:    "22.0",
			osName:     "linux",
			arch:       "x64",
			wantIgnored: false,
		},
		{
			name: "exact version match in OS array",
			config: IgnoreConfig{
				"nodejs": map[string]any{
					"linux": []any{"22.0"},
				},
			},
			runtime:     "nodejs",
			version:    "22.0",
			osName:     "linux",
			arch:       "x64",
			wantIgnored: true,
		},
		{
			name: "prefix version match in OS array",
			config: IgnoreConfig{
				"nodejs": map[string]any{
					"linux": []any{"22"},
				},
			},
			runtime:     "nodejs",
			version:    "22.15.0",
			osName:     "linux",
			arch:       "x64",
			wantIgnored: true,
		},
		{
			name: "version not matching",
			config: IgnoreConfig{
				"nodejs": map[string]any{
					"linux": []any{"22.0"},
				},
			},
			runtime:     "nodejs",
			version:    "21.0",
			osName:     "linux",
			arch:       "x64",
			wantIgnored: false,
		},
		{
			name: "OS not in config",
			config: IgnoreConfig{
				"nodejs": map[string]any{
					"windows": []any{"22.0"},
				},
			},
			runtime:     "nodejs",
			version:    "22.0",
			osName:     "linux",
			arch:       "x64",
			wantIgnored: false,
		},
		{
			name: "OS-wide pattern with 'all'",
			config: IgnoreConfig{
				"nodejs": map[string]any{
					"linux": map[string]any{
						"all": []any{"22"},
					},
				},
			},
			runtime:     "nodejs",
			version:    "22.15.0",
			osName:     "linux",
			arch:       "x64",
			wantIgnored: true,
		},
		{
			name: "arch-specific pattern",
			config: IgnoreConfig{
				"nodejs": map[string]any{
					"linux": map[string]any{
						"x64": []any{"22.0"},
					},
				},
			},
			runtime:     "nodejs",
			version:    "22.0",
			osName:     "linux",
			arch:       "x64",
			wantIgnored: true,
		},
		{
			name: "arch-specific pattern - different arch",
			config: IgnoreConfig{
				"nodejs": map[string]any{
					"linux": map[string]any{
						"x64": []any{"22.0"},
					},
				},
			},
			runtime:     "nodejs",
			version:    "22.0",
			osName:     "linux",
			arch:       "arm64",
			wantIgnored: false,
		},
		{
			name: "complex nested structure - OS 'all' and arch",
			config: IgnoreConfig{
				"nodejs": map[string]any{
					"mac": map[string]any{
						"all":     []any{"18"},
						"aarch64": []any{"20.11"},
					},
				},
			},
			runtime:     "nodejs",
			version:    "20.11.0",
			osName:     "mac",
			arch:       "aarch64",
			wantIgnored: true,
		},
		{
			name: "complex nested structure - OS 'all' matches",
			config: IgnoreConfig{
				"nodejs": map[string]any{
					"mac": map[string]any{
						"all":     []any{"18"},
						"aarch64": []any{"20.11"},
					},
				},
			},
			runtime:     "nodejs",
			version:    "18.17.0",
			osName:     "mac",
			arch:       "x64",
			wantIgnored: true,
		},
		{
			name: "invalid config structure - not a map",
			config: IgnoreConfig{
				"nodejs": []any{"invalid"},
			},
			runtime:     "nodejs",
			version:    "22.0",
			osName:     "linux",
			arch:       "x64",
			wantIgnored: false,
		},
		{
			name: "empty config",
			config: IgnoreConfig{},
			runtime:     "nodejs",
			version:    "22.0",
			osName:     "linux",
			arch:       "x64",
			wantIgnored: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.config.IsPlatformIgnored(tt.runtime, tt.version, tt.osName, tt.arch)
			if got != tt.wantIgnored {
				t.Errorf("IsPlatformIgnored(%q, %q, %q, %q) = %v, want %v",
					tt.runtime, tt.version, tt.osName, tt.arch, got, tt.wantIgnored)
			}
		})
	}
}

func TestMatchPatterns(t *testing.T) {
	tests := []struct {
		name     string
		patterns []any
		version  string
		want     bool
	}{
		{
			name:     "exact match",
			patterns: []any{"22.0"},
			version:  "22.0",
			want:     true,
		},
		{
			name:     "prefix match",
			patterns: []any{"22"},
			version:  "22.15.0",
			want:     true,
		},
		{
			name:     "no match",
			patterns: []any{"22.0"},
			version:  "21.0",
			want:     false,
		},
		{
			name:     "multiple patterns - first matches",
			patterns: []any{"22", "21"},
			version:  "22.15.0",
			want:     true,
		},
		{
			name:     "multiple patterns - second matches",
			patterns: []any{"22", "21"},
			version:  "21.5.0",
			want:     true,
		},
		{
			name:     "multiple patterns - none match",
			patterns: []any{"22", "21"},
			version:  "20.0",
			want:     false,
		},
		{
			name:     "empty patterns",
			patterns: []any{},
			version:  "22.0",
			want:     false,
		},
		{
			name:     "non-string pattern ignored",
			patterns: []any{123, "22"},
			version:  "22.0",
			want:     true,
		},
		{
			name:     "empty string pattern ignored",
			patterns: []any{"", "22"},
			version:  "22.0",
			want:     true,
		},
		{
			name:     "prefix too long",
			patterns: []any{"22.15.0.1"},
			version:  "22.15.0",
			want:     false,
		},
		{
			name:     "exact match with longer version",
			patterns: []any{"22"},
			version:  "22",
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchPatterns(tt.patterns, tt.version)
			if got != tt.want {
				t.Errorf("matchPatterns(%v, %q) = %v, want %v", tt.patterns, tt.version, got, tt.want)
			}
		})
	}
}

