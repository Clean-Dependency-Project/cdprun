package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadUnsupportedConfig_EmptyPath(t *testing.T) {
	cfg, err := LoadUnsupportedConfig("")
	if err != nil {
		t.Fatalf("expected no error for empty path, got %v", err)
	}
	if len(cfg) != 0 {
		t.Fatalf("expected empty config, got %v", cfg)
	}
}

func TestLoadUnsupportedConfig_YAML(t *testing.T) {
	content := `
nodejs:
  - version: "16"
    reason: "EOL since 2023-09-11"
    eol_date: "2023-09-11"
python:
  - version: "3.8"
    eol_date: "2024-10-07"
`
	f := writeTempFile(t, "unsupported-*.yaml", content)
	cfg, err := LoadUnsupportedConfig(f)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg["nodejs"]) != 1 {
		t.Fatalf("expected 1 nodejs rule, got %d", len(cfg["nodejs"]))
	}
	if cfg["nodejs"][0].Version != "16" {
		t.Errorf("expected version 16, got %q", cfg["nodejs"][0].Version)
	}
	if cfg["nodejs"][0].Reason != "EOL since 2023-09-11" {
		t.Errorf("unexpected reason: %q", cfg["nodejs"][0].Reason)
	}
	if cfg["nodejs"][0].EOLDate != "2023-09-11" {
		t.Errorf("unexpected eol_date: %q", cfg["nodejs"][0].EOLDate)
	}
	if len(cfg["python"]) != 1 || cfg["python"][0].Version != "3.8" {
		t.Errorf("unexpected python rules: %v", cfg["python"])
	}
}

func TestLoadUnsupportedConfig_JSON(t *testing.T) {
	content := `{"nodejs":[{"version":"18","reason":"EOL","eol_date":"2025-04-30"}]}`
	f := writeTempFile(t, "unsupported-*.json", content)
	cfg, err := LoadUnsupportedConfig(f)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg["nodejs"]) != 1 || cfg["nodejs"][0].Version != "18" {
		t.Errorf("unexpected nodejs rules: %v", cfg["nodejs"])
	}
}

func TestLoadUnsupportedConfig_FileNotFound(t *testing.T) {
	_, err := LoadUnsupportedConfig("/nonexistent/path/unsupported.yaml")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestLoadUnsupportedConfig_InvalidYAML(t *testing.T) {
	// Unclosed flow sequence is a genuine YAML parse error.
	f := writeTempFile(t, "unsupported-*.yaml", "nodejs: [unclosed")
	_, err := LoadUnsupportedConfig(f)
	if err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
}

func TestLoadUnsupportedConfig_InvalidJSON(t *testing.T) {
	f := writeTempFile(t, "unsupported-*.json", "{bad json")
	_, err := LoadUnsupportedConfig(f)
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestUnsupportedConfig_IsVersionUnsupported(t *testing.T) {
	uc := UnsupportedConfig{
		"nodejs": {
			{Version: "16", Reason: "EOL", EOLDate: "2023-09-11"},
			{Version: "14", EOLDate: "2023-04-30"},
		},
		"python": {
			{Version: "3.8", EOLDate: "2024-10-07"},
		},
	}

	tests := []struct {
		runtime string
		version string
		want    bool
	}{
		{"nodejs", "16.20.2", true},
		{"nodejs", "16.0.0", true},
		{"nodejs", "16", true},      // exact match
		{"nodejs", "160.0.1", false}, // false positive guard
		{"nodejs", "14.21.3", true},
		{"nodejs", "18.20.0", false}, // not in rules
		{"python", "3.8.12", true},
		{"python", "3.80.0", false}, // false positive guard
		{"python", "3.9.0", false},
		{"tomcat", "10.1.0", false}, // unknown runtime
		{"nodejs", "", false},       // empty version
	}

	for _, tt := range tests {
		got := uc.IsVersionUnsupported(tt.runtime, tt.version)
		if got != tt.want {
			t.Errorf("IsVersionUnsupported(%q, %q) = %v, want %v", tt.runtime, tt.version, got, tt.want)
		}
	}
}

func TestUnsupportedConfig_FindMatchingRule(t *testing.T) {
	uc := UnsupportedConfig{
		"nodejs": {
			{Version: "16", Reason: "EOL", EOLDate: "2023-09-11"},
		},
	}

	rule := uc.FindMatchingRule("nodejs", "16.20.2")
	if rule == nil {
		t.Fatal("expected rule to match 16.20.2, got nil")
	}
	if rule.EOLDate != "2023-09-11" {
		t.Errorf("unexpected EOLDate: %q", rule.EOLDate)
	}

	if uc.FindMatchingRule("nodejs", "18.0.0") != nil {
		t.Error("expected nil for non-matching version 18.0.0")
	}
	if uc.FindMatchingRule("python", "3.8.1") != nil {
		t.Error("expected nil for unknown runtime python")
	}
}

func TestUnsupportedConfig_EmptyConfig(t *testing.T) {
	var uc UnsupportedConfig
	if uc.IsVersionUnsupported("nodejs", "16.20.2") {
		t.Error("nil config should never match")
	}
	if uc.FindMatchingRule("nodejs", "16.20.2") != nil {
		t.Error("nil config FindMatchingRule should return nil")
	}
}

func TestGlobalConfig_UnsupportedFileField(t *testing.T) {
	yml := `
version: "1.0"
config:
  unsupported_file: policies/unsupported-versions.yaml
  storage:
    database_path: ./downloads.db
runtimes:
  nodejs:
    enabled: false
    endoflife_product: nodejs
    policy_file: policies/nodejs-policy.json
`
	f := writeTempFile(t, "registry-*.yaml", yml)
	cfg, err := LoadConfig(f)
	if err != nil {
		t.Fatalf("unexpected error loading config: %v", err)
	}
	if cfg.Config.UnsupportedFile != "policies/unsupported-versions.yaml" {
		t.Errorf("expected unsupported_file to be set, got %q", cfg.Config.UnsupportedFile)
	}
}

// writeTempFile creates a temporary file with the given content and returns its path.
// The file is removed when the test completes.
func writeTempFile(t *testing.T, pattern, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), pattern)
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("failed to close temp file: %v", err)
	}
	return filepath.Clean(f.Name())
}
