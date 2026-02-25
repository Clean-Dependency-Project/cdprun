package config

import (
	"os"
	"strings"
	"testing"
)

func TestPackagingConfig_Parsing(t *testing.T) {
	yamlContent := `version: "1.0"
runtimes:
  test-runtime:
    enabled: true
    name: "Test Runtime"
    description: "Test"
    endoflife_product: "test"
    policy_file: "test.json"
    packaging:
      enabled: true
      targets: ["rpm", "apk"]
      package_name_template: "OSPO-{runtime}"
      install_prefix_template: "/export/apps/citools/OSPO-{runtime}/{version}"
`

	tmpfile, err := os.CreateTemp("", "test-packaging-config-*.yaml")
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

	rt, ok := cfg.Runtimes["test-runtime"]
	if !ok {
		t.Fatal("test-runtime not found")
	}

	if !rt.Packaging.Enabled {
		t.Fatal("packaging.enabled = false, want true")
	}
	if len(rt.Packaging.Targets) != 2 {
		t.Fatalf("packaging.targets count = %d, want 2", len(rt.Packaging.Targets))
	}
	if rt.Packaging.Targets[0] != "rpm" || rt.Packaging.Targets[1] != "apk" {
		t.Fatalf("packaging.targets = %v, want [rpm apk]", rt.Packaging.Targets)
	}
}

func TestPackagingConfig_Validation(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{
			name: "enabled without targets",
			yaml: `version: "1.0"
runtimes:
  test-runtime:
    enabled: true
    name: "Test Runtime"
    description: "Test"
    endoflife_product: "test"
    policy_file: "test.json"
    packaging:
      enabled: true
      targets: []
`,
			wantErr: "packaging.targets",
		},
		{
			name: "unsupported target",
			yaml: `version: "1.0"
runtimes:
  test-runtime:
    enabled: true
    name: "Test Runtime"
    description: "Test"
    endoflife_product: "test"
    policy_file: "test.json"
    packaging:
      enabled: true
      targets: ["deb"]
`,
			wantErr: "unsupported packaging target",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpfile, err := os.CreateTemp("", "test-packaging-config-invalid-*.yaml")
			if err != nil {
				t.Fatalf("Failed to create temp file: %v", err)
			}
			defer func() { _ = os.Remove(tmpfile.Name()) }()

			if _, err := tmpfile.Write([]byte(tt.yaml)); err != nil {
				t.Fatalf("Failed to write temp file: %v", err)
			}
			if err := tmpfile.Close(); err != nil {
				t.Fatalf("Failed to close temp file: %v", err)
			}

			_, err = LoadConfig(tmpfile.Name())
			if err == nil {
				t.Fatalf("LoadConfig() error = nil, want error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("LoadConfig() error = %v, want error containing %q", err, tt.wantErr)
			}
		})
	}
}

