package config

import (
	"os"
	"testing"
)

func TestReleaseConfig_Parsing(t *testing.T) {
	yamlContent := `version: "1.0"
runtimes:
  test-runtime:
    enabled: true
    name: "Test Runtime"
    description: "Test"
    endoflife_product: "test"
    policy_file: "test.json"
    release:
      auto_release: true
      github_repository: "test-owner/test-repo"
      draft_release: true
      release_name_template: "Test {version}"
`

	tmpfile, err := os.CreateTemp("", "test-config-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpfile.Name()) }()

	if _, err := tmpfile.Write([]byte(yamlContent)); err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}
	if err := tmpfile.Close(); err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}

	cfg, err := LoadConfig(tmpfile.Name())
	if err != nil {
		t.Fatalf("LoadConfig() error: %v", err)
	}

	testRuntime, ok := cfg.Runtimes["test-runtime"]
	if !ok {
		t.Fatal("test-runtime not found in config")
	}

	// Test release config fields
	tests := []struct {
		name     string
		got      interface{}
		want     interface{}
		testName string
	}{
		{"auto_release", testRuntime.Release.AutoRelease, true, "AutoRelease"},
		{"github_repository", testRuntime.Release.GitHubRepository, "test-owner/test-repo", "GitHubRepository"},
		{"draft_release", testRuntime.Release.DraftRelease, true, "DraftRelease"},
		{"release_name_template", testRuntime.Release.ReleaseNameTemplate, "Test {version}", "ReleaseNameTemplate"},
	}

	for _, tt := range tests {
		t.Run(tt.testName, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("Release.%s = %v, want %v", tt.testName, tt.got, tt.want)
			}
		})
	}
}

func TestReleaseConfig_DefaultValues(t *testing.T) {
	yamlContent := `version: "1.0"
runtimes:
  test-runtime:
    enabled: true
    name: "Test Runtime"
    description: "Test"
    endoflife_product: "test"
    policy_file: "test.json"
`

	tmpfile, err := os.CreateTemp("", "test-config-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpfile.Name()) }()

	if _, err := tmpfile.Write([]byte(yamlContent)); err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}
	if err := tmpfile.Close(); err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}

	cfg, err := LoadConfig(tmpfile.Name())
	if err != nil {
		t.Fatalf("LoadConfig() error: %v", err)
	}

	testRuntime, ok := cfg.Runtimes["test-runtime"]
	if !ok {
		t.Fatal("test-runtime not found in config")
	}

	// Test default values (should be zero values when not specified)
	if testRuntime.Release.AutoRelease {
		t.Error("Release.AutoRelease should default to false")
	}
	if testRuntime.Release.GitHubRepository != "" {
		t.Errorf("Release.GitHubRepository should default to empty, got %q", testRuntime.Release.GitHubRepository)
	}
	if testRuntime.Release.DraftRelease {
		t.Error("Release.DraftRelease should default to false")
	}
	if testRuntime.Release.ReleaseNameTemplate != "" {
		t.Errorf("Release.ReleaseNameTemplate should default to empty, got %q", testRuntime.Release.ReleaseNameTemplate)
	}
}

func TestRealConfig_ReleaseFields(t *testing.T) {
	// Test that the actual runtime-registry.yaml file can be loaded
	// and has the expected structure
	cfg, err := LoadConfig("../../runtime-registry.yaml")
	if err != nil {
		t.Fatalf("LoadConfig() error: %v", err)
	}

	nodejs, ok := cfg.Runtimes["nodejs"]
	if !ok {
		t.Fatal("nodejs runtime not found")
	}

	// Verify the Release field exists and is accessible
	// (Even if auto_release is false by default)
	if nodejs.Release.AutoRelease {
		t.Log("Node.js auto_release is enabled")
	} else {
		t.Log("Node.js auto_release is disabled (expected default)")
	}

	// Verify other fields are accessible
	t.Logf("GitHub Repository: %q", nodejs.Release.GitHubRepository)
	t.Logf("Draft Release: %v", nodejs.Release.DraftRelease)
	t.Logf("Release Name Template: %q", nodejs.Release.ReleaseNameTemplate)
}

