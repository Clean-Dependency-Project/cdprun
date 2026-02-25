package packageexec

import (
	"testing"

	"github.com/clean-dependency-project/cdprun/internal/config"
)

func TestExecutorsConfigFromRuntimeRegistry(t *testing.T) {
	cfg := &config.Config{
		Runtimes: map[string]config.Runtime{
			"python": {
				Packaging: config.PackagingConfig{
					Execution: config.PackagingExecutionTargets{
						Targets: map[string]config.PackagingExecutionTarget{
							"rpm": {
								Build: config.PackagingExecutionContainer{
									Image:  "amazonlinux:2023",
									Shell:  "/bin/bash",
									Script: "bash /workspace/scripts/package/python/rpm/build.sh",
								},
								Test: config.PackagingExecutionContainer{
									Image:  "amazonlinux:2023",
									Shell:  "/bin/bash",
									Script: "bash /workspace/scripts/package/python/rpm/test.sh",
								},
							},
						},
					},
				},
			},
		},
	}

	execs, err := ExecutorsConfigFromRuntimeRegistry(cfg)
	if err != nil {
		t.Fatalf("ExecutorsConfigFromRuntimeRegistry() error: %v", err)
	}
	spec, err := execs.Resolve("python", "rpm")
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}
	if spec.Build.Script != "bash /workspace/scripts/package/python/rpm/build.sh" {
		t.Fatalf("build script = %q", spec.Build.Script)
	}
}

