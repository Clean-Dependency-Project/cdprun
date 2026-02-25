package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/urfave/cli/v2"

	"github.com/clean-dependency-project/cdprun/internal/config"
	"github.com/clean-dependency-project/cdprun/internal/packageexec"
	"github.com/clean-dependency-project/cdprun/internal/packaging"
	"github.com/clean-dependency-project/cdprun/internal/promotion"
)

type packageExecuteSummary struct {
	Stage         string `json:"stage"`
	RunID         string `json:"run_id"`
	TargetCount   int    `json:"target_count"`
	BuildFailures int    `json:"build_failures"`
	TestFailures  int    `json:"test_failures"`
	BuiltManifest string `json:"built_manifest"`
	TestedManifest string `json:"tested_manifest"`
	BuildResults  string `json:"build_results"`
	TestResults   string `json:"test_results"`
}

func packageExecute(c *cli.Context) error {
	outputFormat := c.String("output")
	if outputFormat != "json" {
		return fmt.Errorf("only json output is supported")
	}

	manifestPath := strings.TrimSpace(c.String("manifest"))
	stage := strings.ToLower(strings.TrimSpace(c.String("stage")))
	if stage == "" {
		stage = "all"
	}
	switch stage {
	case "all", "build", "test":
	default:
		return fmt.Errorf("invalid stage %q (supported: all, build, test)", stage)
	}

	buildResultsPath := strings.TrimSpace(c.String("build-results"))
	testResultsPath := strings.TrimSpace(c.String("test-results"))
	builtManifestPath := strings.TrimSpace(c.String("built-manifest"))
	testedManifestPath := strings.TrimSpace(c.String("tested-manifest"))

	cfg, err := config.LoadConfig(c.String("config"))
	if err != nil {
		return fmt.Errorf("load runtime registry config: %w", err)
	}
	executorsCfg, err := packageexec.ExecutorsConfigFromRuntimeRegistry(cfg)
	if err != nil {
		return fmt.Errorf("build executors config from runtime registry: %w", err)
	}
	opts := packageexec.Options{
		WorkspaceDir:   strings.TrimSpace(cfg.Config.PackagingExecution.WorkspaceDir),
		BinaryPath:     strings.TrimSpace(cfg.Config.PackagingExecution.BinaryPath),
		DockerPlatform: strings.TrimSpace(cfg.Config.PackagingExecution.DockerPlatform),
		Executors:      executorsCfg,
	}

	runner := packaging.RealRunner{}

	var (
		manifest   PackageManifest
		builds     packageexec.BuildResultsFile
		testResults promotion.TestResultsFile
		buildErr   error
		testErr    error
	)
	switch stage {
	case "build":
		manifest, err = ReadPackageManifest(manifestPath)
		if err != nil {
			return fmt.Errorf("read manifest: %w", err)
		}
		targets := manifestToExecTargets(manifest)
		builds, buildErr = packageexec.BuildTargets(context.Background(), runner, opts, manifest.RunID, targets)
		if err := writeJSONFile(buildResultsPath, builds); err != nil {
			return fmt.Errorf("write build results: %w", err)
		}
		builtManifest := manifestWithBuildStatus(manifest, builds)
		if err := WritePackageManifest(builtManifestPath, builtManifest); err != nil {
			return fmt.Errorf("write built manifest: %w", err)
		}
	case "test":
		manifest, err = ReadPackageManifest(builtManifestPath)
		if err != nil {
			return fmt.Errorf("read built manifest: %w", err)
		}
		builds, err = readBuildResults(buildResultsPath)
		if err != nil {
			return fmt.Errorf("read build results: %w", err)
		}
		testResults, testErr = packageexec.TestBuiltPackages(context.Background(), runner, opts, manifest.RunID, builds)
		if err := writeJSONFile(testResultsPath, testResults); err != nil {
			return fmt.Errorf("write test results: %w", err)
		}
		testedManifest := manifestWithTestStatus(manifest, testResults)
		if err := WritePackageManifest(testedManifestPath, testedManifest); err != nil {
			return fmt.Errorf("write tested manifest: %w", err)
		}
	default: // all
		manifest, err = ReadPackageManifest(manifestPath)
		if err != nil {
			return fmt.Errorf("read manifest: %w", err)
		}
		targets := manifestToExecTargets(manifest)
		builds, buildErr = packageexec.BuildTargets(context.Background(), runner, opts, manifest.RunID, targets)
		if err := writeJSONFile(buildResultsPath, builds); err != nil {
			return fmt.Errorf("write build results: %w", err)
		}
		builtManifest := manifestWithBuildStatus(manifest, builds)
		if err := WritePackageManifest(builtManifestPath, builtManifest); err != nil {
			return fmt.Errorf("write built manifest: %w", err)
		}
		testResults, testErr = packageexec.TestBuiltPackages(context.Background(), runner, opts, manifest.RunID, builds)
		if err := writeJSONFile(testResultsPath, testResults); err != nil {
			return fmt.Errorf("write test results: %w", err)
		}
		testedManifest := manifestWithTestStatus(builtManifest, testResults)
		if err := WritePackageManifest(testedManifestPath, testedManifest); err != nil {
			return fmt.Errorf("write tested manifest: %w", err)
		}
	}

	summary := packageExecuteSummary{
		Stage:         stage,
		RunID:         manifest.RunID,
		TargetCount:   len(manifest.Targets),
		BuildFailures: countBuildFailures(builds),
		TestFailures:  countTestFailures(testResults),
		BuiltManifest: builtManifestPath,
		TestedManifest: testedManifestPath,
		BuildResults:  buildResultsPath,
		TestResults:   testResultsPath,
	}
	payload, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal execute summary: %w", err)
	}
	fmt.Println(string(payload))

	if buildErr != nil || testErr != nil {
		return errors.Join(buildErr, testErr)
	}
	return nil
}

func readBuildResults(path string) (packageexec.BuildResultsFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return packageexec.BuildResultsFile{}, fmt.Errorf("read build results file: %w", err)
	}
	var out packageexec.BuildResultsFile
	if err := json.Unmarshal(data, &out); err != nil {
		return packageexec.BuildResultsFile{}, fmt.Errorf("unmarshal build results: %w", err)
	}
	return out, nil
}

func manifestToExecTargets(manifest PackageManifest) []packageexec.Target {
	targets := make([]packageexec.Target, 0, len(manifest.Targets))
	for _, target := range manifest.Targets {
		targets = append(targets, packageexec.Target{
			Runtime:       target.Runtime,
			Version:       target.Version,
			Target:        target.Target,
			InputMode:     target.InputMode,
			InputPath:     target.InputPath,
			InputSHA256:   target.InputSHA256,
			InputPlatform: target.InputPlatform,
			InputArch:     target.InputArch,
			PackageName:   target.PackageName,
			InstallPrefix: target.InstallPrefix,
		})
	}
	return targets
}

func writeJSONFile(path string, value interface{}) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("path is required")
	}
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0700); err != nil {
		return fmt.Errorf("create parent dir: %w", err)
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal json: %w", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("write json file: %w", err)
	}
	return nil
}

func manifestWithBuildStatus(in PackageManifest, builds packageexec.BuildResultsFile) PackageManifest {
	out := in
	out.Stage = "built"
	out.GeneratedAt = time.Now().UTC()

	buildByKey := make(map[string]bool, len(builds.Results))
	for _, result := range builds.Results {
		buildByKey[manifestEntryKey(
			result.Target.Runtime,
			result.Target.Version,
			result.Target.Target,
			result.Target.InputSHA256,
			result.Target.PackageName,
			result.Target.InstallPrefix,
		)] = result.Success
	}
	for i := range out.Targets {
		target := &out.Targets[i]
		if built, found := buildByKey[manifestEntryKey(
			target.Runtime,
			target.Version,
			target.Target,
			target.InputSHA256,
			target.PackageName,
			target.InstallPrefix,
		)]; found {
			target.Status.Built = built
		}
	}
	return out
}

func manifestWithTestStatus(in PackageManifest, tests promotion.TestResultsFile) PackageManifest {
	out := in
	out.Stage = "tested"
	out.GeneratedAt = time.Now().UTC()
	testByKey := make(map[string]bool, len(tests.Results))
	for _, result := range tests.Results {
		testByKey[manifestEntryKey(
			result.Runtime,
			result.Version,
			result.Target,
			result.InputSHA256,
			result.PackageName,
			result.InstallPrefix,
		)] = result.Passed
	}
	for i := range out.Targets {
		target := &out.Targets[i]
		if passed, found := testByKey[manifestEntryKey(
			target.Runtime,
			target.Version,
			target.Target,
			target.InputSHA256,
			target.PackageName,
			target.InstallPrefix,
		)]; found {
			target.Status.Tested = passed
		}
	}
	return out
}

func countBuildFailures(builds packageexec.BuildResultsFile) int {
	total := 0
	for _, result := range builds.Results {
		if !result.Success {
			total++
		}
	}
	return total
}

func countTestFailures(tests promotion.TestResultsFile) int {
	total := 0
	for _, result := range tests.Results {
		if !result.Passed {
			total++
		}
	}
	return total
}

func manifestEntryKey(runtime, version, target, inputSHA256, packageName, installPrefix string) string {
	return strings.ToLower(strings.TrimSpace(runtime)) + "|" +
		strings.ToLower(strings.TrimSpace(version)) + "|" +
		strings.ToLower(strings.TrimSpace(target)) + "|" +
		strings.ToLower(strings.TrimSpace(inputSHA256)) + "|" +
		strings.ToLower(strings.TrimSpace(packageName)) + "|" +
		strings.ToLower(strings.TrimSpace(installPrefix))
}
