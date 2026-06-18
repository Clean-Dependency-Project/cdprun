// Package pycharm provides a unified PyCharm runtime adapter using shared JetBrains runtime logic.
package pycharm

import (
	"log/slog"

	"github.com/clean-dependency-project/cdprun/internal/config"
	"github.com/clean-dependency-project/cdprun/internal/runtime"
	"github.com/clean-dependency-project/cdprun/internal/runtimes/jetbrains"
)

const (
	// PyCharmRuntime is the registry key for the unified PyCharm product (PCP).
	PyCharmRuntime     = "pycharm"
	defaultProductCode = "PCP"
)

// Adapter is a thin PyCharm wrapper around the shared JetBrains adapter.
type Adapter struct {
	*jetbrains.Adapter
}

// NewAdapterWithConfig builds a unified PyCharm adapter.
func NewAdapterWithConfig(cfg *config.Runtime, globalCfg *config.GlobalConfig, stdout, stderr *slog.Logger) runtime.RuntimeProvider {
	return &Adapter{
		Adapter: jetbrains.NewAdapterWithConfig(cfg, globalCfg, jetbrains.ProductOptions{
			RuntimeName:        PyCharmRuntime,
			DefaultProductCode: defaultProductCode,
			DefaultReleaseType: "release",
			DefaultReleasesURL: "https://data.services.jetbrains.com/products/releases",
			DefaultUserAgent:   "cdprun/1.0 (PyCharm)",
			ArtifactPrefix:     "pycharm",
		}, stdout, stderr),
	}
}
