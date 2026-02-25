package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/clean-dependency-project/cdprun/internal/promotion"
)

// PromoteTestedPackagesFromFiles promotes tested package entries from files.
//
// It reads a manifest + test-result file, validates all entries up front, then
// updates release artifacts and package records only if all entries are eligible.
func PromoteTestedPackagesFromFiles(
	db promotion.Store,
	manifestPath string,
	testResultsPath string,
) (promotion.Summary, error) {
	manifest, err := ReadPackageManifest(manifestPath)
	if err != nil {
		return promotion.Summary{}, fmt.Errorf("read package manifest: %w", err)
	}
	testResults, err := ReadPackageTestResultsFile(testResultsPath)
	if err != nil {
		return promotion.Summary{}, fmt.Errorf("read package test results: %w", err)
	}
	targets := make([]promotion.Target, 0, len(manifest.Targets))
	for _, target := range manifest.Targets {
		targets = append(targets, promotion.Target{
			Runtime:       target.Runtime,
			Version:       target.Version,
			Target:        target.Target,
			InputPlatform: target.InputPlatform,
			InputArch:     target.InputArch,
			InputSHA256:   target.InputSHA256,
			PackageName:   target.PackageName,
			InstallPrefix: target.InstallPrefix,
			Tested:        target.Status.Tested,
		})
	}
	return promotion.PromoteTestedPackages(db, manifest.RunID, targets, testResults)
}

// ReadPackageManifest reads a package manifest JSON from disk.
func ReadPackageManifest(path string) (PackageManifest, error) {
	if strings.TrimSpace(path) == "" {
		return PackageManifest{}, fmt.Errorf("manifest path is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return PackageManifest{}, fmt.Errorf("read manifest file: %w", err)
	}
	var manifest PackageManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return PackageManifest{}, fmt.Errorf("unmarshal package manifest: %w", err)
	}
	return manifest, nil
}

// ReadPackageTestResultsFile reads package test results JSON from disk.
func ReadPackageTestResultsFile(path string) (promotion.TestResultsFile, error) {
	if strings.TrimSpace(path) == "" {
		return promotion.TestResultsFile{}, fmt.Errorf("test results path is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return promotion.TestResultsFile{}, fmt.Errorf("read test results file: %w", err)
	}
	var out promotion.TestResultsFile
	if err := json.Unmarshal(data, &out); err != nil {
		return promotion.TestResultsFile{}, fmt.Errorf("unmarshal package test results: %w", err)
	}
	return out, nil
}
