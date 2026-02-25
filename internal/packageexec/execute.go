package packageexec

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/clean-dependency-project/cdprun/internal/packaging"
	"github.com/clean-dependency-project/cdprun/internal/promotion"
)

type Target struct {
	Runtime       string `json:"runtime"`
	Version       string `json:"version"`
	Target        string `json:"target"`
	InputMode     string `json:"input_mode"`
	InputPath     string `json:"input_path"`
	InputSHA256   string `json:"input_sha256"`
	InputPlatform string `json:"input_platform"`
	InputArch     string `json:"input_arch"`
	PackageName   string `json:"package_name"`
	InstallPrefix string `json:"install_prefix"`
}

type BuildResultRecord struct {
	Target  Target               `json:"target"`
	Build   packaging.BuildResult `json:"build,omitempty"`
	Success bool                 `json:"success"`
	Error   string               `json:"error,omitempty"`
}

type BuildResultsFile struct {
	RunID   string              `json:"run_id"`
	Results []BuildResultRecord `json:"results"`
}

type Options struct {
	WorkspaceDir   string
	BinaryPath     string
	DockerPlatform string
	Executors      ExecutorsConfig
}

func BuildTargets(ctx context.Context, runner packaging.CommandRunner, opts Options, runID string, targets []Target) (BuildResultsFile, error) {
	normalized, err := normalizeOptions(opts)
	if err != nil {
		return BuildResultsFile{}, err
	}
	results := BuildResultsFile{
		RunID:   runID,
		Results: make([]BuildResultRecord, 0, len(targets)),
	}

	var failed int
	for _, target := range targets {
		record := BuildResultRecord{Target: target}
		buildResult, err := buildOneInContainer(ctx, runner, normalized, target)
		if err != nil {
			record.Success = false
			record.Error = err.Error()
			failed++
		} else {
			record.Success = true
			record.Build = buildResult
		}
		results.Results = append(results.Results, record)
	}
	if failed > 0 {
		return results, fmt.Errorf("build failures: %d", failed)
	}
	return results, nil
}

func TestBuiltPackages(ctx context.Context, runner packaging.CommandRunner, opts Options, runID string, builds BuildResultsFile) (promotion.TestResultsFile, error) {
	normalized, err := normalizeOptions(opts)
	if err != nil {
		return promotion.TestResultsFile{}, err
	}
	results := promotion.TestResultsFile{
		RunID:   runID,
		Results: make([]promotion.TestResult, 0, len(builds.Results)),
	}

	var failed int
	for _, buildRecord := range builds.Results {
		if !buildRecord.Success {
			results.Results = append(results.Results, promotion.TestResult{
				Runtime:         buildRecord.Target.Runtime,
				Version:         buildRecord.Target.Version,
				Target:          buildRecord.Target.Target,
				InputSHA256:     buildRecord.Target.InputSHA256,
				PackageName:     buildRecord.Target.PackageName,
				InstallPrefix:   buildRecord.Target.InstallPrefix,
				Passed:          false,
				PackageFilename: buildRecord.Build.PackageFilename,
				PackagePath:     buildRecord.Build.PackagePath,
				PackageSHA256:   buildRecord.Build.PackageSHA256,
			})
			failed++
			continue
		}

		err := testOneInContainer(ctx, runner, normalized, buildRecord.Target, buildRecord.Build)
		size := int64(0)
		if absPath, statErr := resolvePath(normalized.WorkspaceDir, buildRecord.Build.PackagePath); statErr == nil {
			if info, infoErr := os.Stat(absPath); infoErr == nil {
				size = info.Size()
			}
		}
		result := promotion.TestResult{
			Runtime:         buildRecord.Target.Runtime,
			Version:         buildRecord.Target.Version,
			Target:          buildRecord.Target.Target,
			InputSHA256:     buildRecord.Target.InputSHA256,
			PackageName:     buildRecord.Target.PackageName,
			InstallPrefix:   buildRecord.Target.InstallPrefix,
			Passed:          err == nil,
			PackageFilename: buildRecord.Build.PackageFilename,
			PackagePath:     buildRecord.Build.PackagePath,
			PackageSHA256:   buildRecord.Build.PackageSHA256,
			PackageSize:     size,
		}
		results.Results = append(results.Results, result)
		if err != nil {
			failed++
		}
	}

	if failed > 0 {
		return results, fmt.Errorf("test failures: %d", failed)
	}
	return results, nil
}

func normalizeOptions(opts Options) (Options, error) {
	workspace := strings.TrimSpace(opts.WorkspaceDir)
	if workspace == "" {
		return Options{}, fmt.Errorf("workspace dir is required")
	}
	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		return Options{}, fmt.Errorf("resolve workspace dir: %w", err)
	}
	binaryPath := strings.TrimSpace(opts.BinaryPath)
	if binaryPath == "" {
		return Options{}, fmt.Errorf("binary path is required")
	}
	platform := strings.TrimSpace(opts.DockerPlatform)
	if platform == "" {
		return Options{}, fmt.Errorf("docker platform is required")
	}
	if len(opts.Executors.Runtimes) == 0 {
		return Options{}, fmt.Errorf("executors config is required")
	}
	return Options{
		WorkspaceDir:   absWorkspace,
		BinaryPath:     binaryPath,
		DockerPlatform: platform,
		Executors:      opts.Executors,
	}, nil
}

func buildOneInContainer(ctx context.Context, runner packaging.CommandRunner, opts Options, target Target) (packaging.BuildResult, error) {
	execSpec, err := opts.Executors.Resolve(target.Runtime, target.Target)
	if err != nil {
		return packaging.BuildResult{}, err
	}

	args := []string{
		"run", "--rm", "--platform=" + opts.DockerPlatform,
		"-v", opts.WorkspaceDir + ":/workspace",
		"-w", "/workspace",
		execSpec.Build.Image,
		execSpec.Build.Shell, "-lc", execSpec.Build.Script,
	}
	stdout, stderr, err := runner.Run(ctx, "", "docker", args, buildEnvForTarget(opts, target))
	if err != nil {
		return packaging.BuildResult{}, fmt.Errorf("docker build container failed: %w (stderr=%s)", err, strings.TrimSpace(stderr))
	}

	var build packaging.BuildResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &build); err != nil {
		return packaging.BuildResult{}, fmt.Errorf("parse build result json: %w (stdout=%s)", err, truncate(strings.TrimSpace(stdout), 400))
	}
	return build, nil
}

func testOneInContainer(ctx context.Context, runner packaging.CommandRunner, opts Options, target Target, build packaging.BuildResult) error {
	packagePath, err := resolvePath(opts.WorkspaceDir, build.PackagePath)
	if err != nil {
		return err
	}

	execSpec, err := opts.Executors.Resolve(target.Runtime, target.Target)
	if err != nil {
		return err
	}
	_, stderr, runErr := runner.Run(ctx, "", "docker", []string{
		"run", "--rm", "--platform=" + opts.DockerPlatform,
		"-v", opts.WorkspaceDir + ":/workspace",
		"-w", "/workspace",
		execSpec.Test.Image,
		execSpec.Test.Shell, "-lc", execSpec.Test.Script,
	}, testEnvForTarget(opts, target, packagePath))
	if runErr != nil {
		return fmt.Errorf("%s test failed: %w (stderr=%s)", target.Target, runErr, strings.TrimSpace(stderr))
	}
	return nil
}

func buildEnvForTarget(opts Options, target Target) []string {
	return []string{
		"CDP_BINARY_PATH=" + targetBinary(opts.BinaryPath),
		"CDP_RUNTIME=" + target.Runtime,
		"CDP_VERSION=" + target.Version,
		"CDP_PACKAGE_NAME=" + target.PackageName,
		"CDP_INSTALL_PREFIX=" + target.InstallPrefix,
		"CDP_INPUT_MODE=" + target.InputMode,
		"CDP_INPUT_PATH=" + target.InputPath,
		"CDP_INPUT_SHA256=" + target.InputSHA256,
		"CDP_OUTPUT_DIR=./packages",
		"CDP_RELEASE=1",
		"CDP_ARCH=" + defaultPackageArch(target.InputArch),
	}
}

func testEnvForTarget(opts Options, target Target, packagePath string) []string {
	return []string{
		"CDP_RUNTIME=" + target.Runtime,
		"CDP_VERSION=" + target.Version,
		"CDP_INSTALL_PREFIX=" + target.InstallPrefix,
		"CDP_PACKAGE_PATH=" + pathInWorkspace(packagePath, opts.WorkspaceDir),
	}
}

func defaultPackageArch(inputArch string) string {
	switch strings.ToLower(strings.TrimSpace(inputArch)) {
	case "x64", "amd64", "x86_64":
		return "x86_64"
	case "aarch64", "arm64":
		return "aarch64"
	default:
		return "x86_64"
	}
}

func targetBinary(binaryPath string) string {
	if strings.HasPrefix(binaryPath, "/workspace/") {
		return binaryPath
	}
	if strings.HasPrefix(binaryPath, "./") {
		return "/workspace/" + strings.TrimPrefix(binaryPath, "./")
	}
	if strings.HasPrefix(binaryPath, "/") {
		return binaryPath
	}
	return "/workspace/" + binaryPath
}

func pathInWorkspace(absPath, workspaceDir string) string {
	rel, err := filepath.Rel(workspaceDir, absPath)
	if err != nil {
		return absPath
	}
	return "/workspace/" + filepath.ToSlash(rel)
}

func resolvePath(workspaceDir, maybeRelative string) (string, error) {
	if strings.TrimSpace(maybeRelative) == "" {
		return "", fmt.Errorf("path is required")
	}
	if filepath.IsAbs(maybeRelative) {
		return maybeRelative, nil
	}
	return filepath.Abs(filepath.Join(workspaceDir, maybeRelative))
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
