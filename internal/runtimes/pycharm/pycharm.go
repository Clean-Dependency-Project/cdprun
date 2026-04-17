// Package pycharm provides a PyCharm Professional runtime adapter using shared JetBrains runtime logic.
package pycharm

import (
	"log/slog"

	"github.com/clean-dependency-project/cdprun/internal/config"
	"github.com/clean-dependency-project/cdprun/internal/runtime"
	"github.com/clean-dependency-project/cdprun/internal/runtimes/jetbrains"
)

const (
	// PyCharmProfessionalRuntime is the registry key for PyCharm Professional (PCP).
	PyCharmProfessionalRuntime = "pycharm_professional"
	defaultProductCode         = "PCP"
)

// Adapter is a thin PyCharm wrapper around the shared JetBrains adapter.
type Adapter struct {
	*jetbrains.Adapter
}

// NewAdapterWithConfig builds a PyCharm Professional adapter.
func NewAdapterWithConfig(cfg *config.Runtime, globalCfg *config.GlobalConfig, stdout, stderr *slog.Logger) runtime.RuntimeProvider {
	return &Adapter{
		Adapter: jetbrains.NewAdapterWithConfig(cfg, globalCfg, jetbrains.ProductOptions{
			RuntimeName:        PyCharmProfessionalRuntime,
			DefaultProductCode: defaultProductCode,
			DefaultReleaseType: "release",
			DefaultReleasesURL: "https://data.services.jetbrains.com/products/releases",
			DefaultUserAgent:   "cdprun/1.0 (PyCharm)",
			ArtifactPrefix:     "pycharm-professional",
		}, stdout, stderr),
	}
}
