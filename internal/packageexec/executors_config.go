package packageexec

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type ContainerSpec struct {
	Image  string `yaml:"image"`
	Shell  string `yaml:"shell"`
	Script string `yaml:"script"`
}

type TargetExecutorSpec struct {
	Build ContainerSpec `yaml:"build"`
	Test  ContainerSpec `yaml:"test"`
}

type RuntimeExecutorSpec struct {
	Targets map[string]TargetExecutorSpec `yaml:"targets"`
}

type ExecutorsConfig struct {
	Runtimes map[string]RuntimeExecutorSpec `yaml:"runtimes"`
}

func LoadExecutorsConfig(path string) (ExecutorsConfig, error) {
	if strings.TrimSpace(path) == "" {
		return ExecutorsConfig{}, fmt.Errorf("executors config path is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ExecutorsConfig{}, fmt.Errorf("read executors config: %w", err)
	}
	var cfg ExecutorsConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return ExecutorsConfig{}, fmt.Errorf("parse executors config: %w", err)
	}
	if len(cfg.Runtimes) == 0 {
		return ExecutorsConfig{}, fmt.Errorf("executors config must define runtimes")
	}
	for runtime, runtimeSpec := range cfg.Runtimes {
		if len(runtimeSpec.Targets) == 0 {
			return ExecutorsConfig{}, fmt.Errorf("runtime %q must define targets", runtime)
		}
		for target, targetSpec := range runtimeSpec.Targets {
			if err := validateContainerSpec("runtime "+runtime+" target "+target+" build", targetSpec.Build); err != nil {
				return ExecutorsConfig{}, err
			}
			if err := validateContainerSpec("runtime "+runtime+" target "+target+" test", targetSpec.Test); err != nil {
				return ExecutorsConfig{}, err
			}
		}
	}
	return cfg, nil
}

func (c ExecutorsConfig) Resolve(runtime, target string) (TargetExecutorSpec, error) {
	runtimeKey := strings.ToLower(strings.TrimSpace(runtime))
	targetKey := strings.ToLower(strings.TrimSpace(target))
	runtimeSpec, ok := c.Runtimes[runtimeKey]
	if !ok {
		return TargetExecutorSpec{}, fmt.Errorf("no executor runtime config for %q", runtime)
	}
	targetSpec, ok := runtimeSpec.Targets[targetKey]
	if !ok {
		return TargetExecutorSpec{}, fmt.Errorf("no executor target config for runtime %q target %q", runtime, target)
	}
	return targetSpec, nil
}

func validateContainerSpec(prefix string, spec ContainerSpec) error {
	if strings.TrimSpace(spec.Image) == "" {
		return fmt.Errorf("%s image is required", prefix)
	}
	if strings.TrimSpace(spec.Shell) == "" {
		return fmt.Errorf("%s shell is required", prefix)
	}
	if strings.TrimSpace(spec.Script) == "" {
		return fmt.Errorf("%s script is required", prefix)
	}
	return nil
}
