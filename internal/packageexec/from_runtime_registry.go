package packageexec

import (
	"fmt"
	"strings"

	"github.com/clean-dependency-project/cdprun/internal/config"
)

func ExecutorsConfigFromRuntimeRegistry(cfg *config.Config) (ExecutorsConfig, error) {
	if cfg == nil {
		return ExecutorsConfig{}, fmt.Errorf("config is required")
	}
	out := ExecutorsConfig{
		Runtimes: make(map[string]RuntimeExecutorSpec),
	}

	for runtimeName, runtimeCfg := range cfg.Runtimes {
		runtimeKey := strings.ToLower(strings.TrimSpace(runtimeName))
		targetMap := make(map[string]TargetExecutorSpec)
		for targetName, targetCfg := range runtimeCfg.Packaging.Execution.Targets {
			targetKey := strings.ToLower(strings.TrimSpace(targetName))
			spec := TargetExecutorSpec{
				Build: ContainerSpec{
					Image:  strings.TrimSpace(targetCfg.Build.Image),
					Shell:  strings.TrimSpace(targetCfg.Build.Shell),
					Script: strings.TrimSpace(targetCfg.Build.Script),
				},
				Test: ContainerSpec{
					Image:  strings.TrimSpace(targetCfg.Test.Image),
					Shell:  strings.TrimSpace(targetCfg.Test.Shell),
					Script: strings.TrimSpace(targetCfg.Test.Script),
				},
			}
			if err := validateContainerSpec("runtime "+runtimeName+" target "+targetName+" build", spec.Build); err != nil {
				return ExecutorsConfig{}, err
			}
			if err := validateContainerSpec("runtime "+runtimeName+" target "+targetName+" test", spec.Test); err != nil {
				return ExecutorsConfig{}, err
			}
			targetMap[targetKey] = spec
		}
		if len(targetMap) > 0 {
			out.Runtimes[runtimeKey] = RuntimeExecutorSpec{Targets: targetMap}
		}
	}

	if len(out.Runtimes) == 0 {
		return ExecutorsConfig{}, fmt.Errorf("runtime registry must include packaging.execution targets")
	}
	return out, nil
}
