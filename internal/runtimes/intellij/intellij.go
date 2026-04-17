// Package intellij provides an IntelliJ IDEA runtime adapter using shared JetBrains runtime logic.
package intellij

import (
	"log/slog"

	"github.com/clean-dependency-project/cdprun/internal/config"
	"github.com/clean-dependency-project/cdprun/internal/runtime"
	"github.com/clean-dependency-project/cdprun/internal/runtimes/jetbrains"
)

const (
	// IntelliJUltimateRuntime is the registry key for IntelliJ IDEA Ultimate (IIU).
	IntelliJUltimateRuntime = "intellij_idea_ultimate"
	defaultProductCode      = "IIU"
)

// Adapter is a thin IntelliJ wrapper around the shared JetBrains adapter.
type Adapter struct {
	*jetbrains.Adapter
}

// NewAdapterWithConfig builds an IntelliJ adapter (Ultimate by default when jetbrains_code is empty).
func NewAdapterWithConfig(cfg *config.Runtime, globalCfg *config.GlobalConfig, stdout, stderr *slog.Logger) runtime.RuntimeProvider {
	return &Adapter{
		Adapter: jetbrains.NewAdapterWithConfig(cfg, globalCfg, jetbrains.ProductOptions{
			RuntimeName:        IntelliJUltimateRuntime,
			DefaultProductCode: defaultProductCode,
			DefaultReleaseType: "release",
			DefaultReleasesURL: "https://data.services.jetbrains.com/products/releases",
			DefaultUserAgent:   "cdprun/1.0 (IntelliJ)",
			ArtifactPrefix:     "intellij-idea",
		}, stdout, stderr),
	}
}

// setExpectedSHA256 remains for package-level tests.
func (a *Adapter) setExpectedSHA256(classifier, hash string) {
	a.SetExpectedSHA256(classifier, hash)
}

// expectedSHA256 remains for package-level tests.
func (a *Adapter) expectedSHA256(classifier string) string {
	return a.ExpectedSHA256(classifier)
}

// parseChecksumLine remains for package-level tests.
func parseChecksumLine(body string) (string, error) {
	return jetbrains.ParseChecksumLine(body)
}
