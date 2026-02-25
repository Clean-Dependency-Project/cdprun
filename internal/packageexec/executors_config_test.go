package packageexec

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadExecutorsConfigAndResolve(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "executors.yaml")
	content := `
runtimes:
  nodejs:
    targets:
      rpm:
        build:
          image: "amazonlinux:2023"
          shell: "/bin/bash"
          script: "bash /workspace/scripts/package/rpm/build.sh"
        test:
          image: "amazonlinux:2023"
          shell: "/bin/bash"
          script: "bash /workspace/scripts/package/rpm/test.sh"
`
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}

	cfg, err := LoadExecutorsConfig(path)
	if err != nil {
		t.Fatalf("LoadExecutorsConfig() error: %v", err)
	}
	spec, err := cfg.Resolve("nodejs", "rpm")
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}
	if spec.Build.Image != "amazonlinux:2023" {
		t.Fatalf("build image = %q, want amazonlinux:2023", spec.Build.Image)
	}
}

