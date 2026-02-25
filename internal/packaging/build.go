package packaging

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Build is the packaging entry point.
//
// It accepts a CommandRunner (interface) and returns a BuildResult struct.
func Build(ctx context.Context, runner CommandRunner, req BuildRequest) (BuildResult, error) {
	if runner == nil {
		return BuildResult{}, fmt.Errorf("runner is required")
	}

	switch req.PackageType {
	case PackageTypeRPM:
		switch req.InputMode {
		case InputModePayloadDir:
			return buildRPMFromPayload(ctx, runner, req, req.PayloadDir)
		case InputModeArchiveTarball:
			return buildRPMFromArchiveTarball(ctx, runner, req)
		default:
			return BuildResult{}, fmt.Errorf("unsupported input mode: %q", req.InputMode)
		}
	case PackageTypeAPK:
		switch req.InputMode {
		case InputModePayloadDir:
			return buildAPKFromPayload(ctx, runner, req, req.PayloadDir)
		case InputModeArchiveTarball:
			return buildAPKFromArchiveTarball(ctx, runner, req)
		default:
			return BuildResult{}, fmt.Errorf("unsupported input mode: %q", req.InputMode)
		}
	default:
		return BuildResult{}, fmt.Errorf("unsupported package type: %q", req.PackageType)
	}
}

func buildRPMFromArchiveTarball(ctx context.Context, runner CommandRunner, req BuildRequest) (BuildResult, error) {
	if strings.TrimSpace(req.Input.Path) == "" {
		return BuildResult{}, fmt.Errorf("input.path is required")
	}
	if err := RequireSHA256Match(req.Input.Path, req.Input.SHA256); err != nil {
		return BuildResult{}, err
	}

	payloadDir, err := os.MkdirTemp("", "cdprun-payload-*")
	if err != nil {
		return BuildResult{}, fmt.Errorf("create payload dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(payloadDir) }()

	if err := StageArchiveTarball(ctx, runner, ArchiveStageOptions{
		TarballPath:   req.Input.Path,
		InstallPrefix: req.InstallPrefix,
		PayloadDir:    payloadDir,
	}); err != nil {
		return BuildResult{}, err
	}

	return buildRPMFromPayload(ctx, runner, req, payloadDir)
}

func buildRPMFromPayload(ctx context.Context, runner CommandRunner, req BuildRequest, payloadDir string) (BuildResult, error) {
	if strings.TrimSpace(payloadDir) == "" {
		return BuildResult{}, fmt.Errorf("payload dir is required")
	}
	if req.OutDir == "" {
		return BuildResult{}, fmt.Errorf("out_dir is required")
	}
	if req.PackageName == "" {
		return BuildResult{}, fmt.Errorf("package_name is required")
	}
	if req.Release == "" {
		return BuildResult{}, fmt.Errorf("release is required")
	}
	if req.Version == "" {
		return BuildResult{}, fmt.Errorf("version is required")
	}
	if req.Runtime == "" {
		return BuildResult{}, fmt.Errorf("runtime is required")
	}

	absPayloadDir, err := filepath.Abs(payloadDir)
	if err != nil {
		return BuildResult{}, fmt.Errorf("abs payload dir: %w", err)
	}

	result, err := BuildRPMFromPayload(ctx, runner, RPMBuildOptions{
		Runtime:       req.Runtime,
		Name:          req.PackageName,
		Version:       req.Version,
		Release:       req.Release,
		Arch:          req.Arch,
		Summary:       req.Summary,
		License:       req.License,
		URL:           req.URL,
		InstallPrefix: req.InstallPrefix,
		PayloadDir:    absPayloadDir,
		OutDir:        req.OutDir,
	})
	if err != nil {
		return BuildResult{}, err
	}

	result.Input = req.Input
	return result, nil
}

func buildAPKFromArchiveTarball(ctx context.Context, runner CommandRunner, req BuildRequest) (BuildResult, error) {
	if strings.TrimSpace(req.Input.Path) == "" {
		return BuildResult{}, fmt.Errorf("input.path is required")
	}
	if err := RequireSHA256Match(req.Input.Path, req.Input.SHA256); err != nil {
		return BuildResult{}, err
	}

	payloadDir, err := os.MkdirTemp("", "cdprun-payload-*")
	if err != nil {
		return BuildResult{}, fmt.Errorf("create payload dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(payloadDir) }()

	if err := StageArchiveTarball(ctx, runner, ArchiveStageOptions{
		TarballPath:   req.Input.Path,
		InstallPrefix: req.InstallPrefix,
		PayloadDir:    payloadDir,
	}); err != nil {
		return BuildResult{}, err
	}

	return buildAPKFromPayload(ctx, runner, req, payloadDir)
}

func buildAPKFromPayload(ctx context.Context, runner CommandRunner, req BuildRequest, payloadDir string) (BuildResult, error) {
	if strings.TrimSpace(payloadDir) == "" {
		return BuildResult{}, fmt.Errorf("payload dir is required")
	}
	if req.OutDir == "" {
		return BuildResult{}, fmt.Errorf("out_dir is required")
	}
	if req.PackageName == "" {
		return BuildResult{}, fmt.Errorf("package_name is required")
	}
	if req.Release == "" {
		return BuildResult{}, fmt.Errorf("release is required")
	}
	if req.Version == "" {
		return BuildResult{}, fmt.Errorf("version is required")
	}
	if req.Runtime == "" {
		return BuildResult{}, fmt.Errorf("runtime is required")
	}

	absPayloadDir, err := filepath.Abs(payloadDir)
	if err != nil {
		return BuildResult{}, fmt.Errorf("abs payload dir: %w", err)
	}

	result, err := BuildAPKFromPayload(ctx, runner, APKBuildOptions{
		Runtime:       req.Runtime,
		Name:          req.PackageName,
		Version:       req.Version,
		Release:       req.Release,
		Arch:          req.Arch,
		Summary:       req.Summary,
		License:       req.License,
		URL:           req.URL,
		InstallPrefix: req.InstallPrefix,
		PayloadDir:    absPayloadDir,
		OutDir:        req.OutDir,
	})
	if err != nil {
		return BuildResult{}, err
	}

	result.Input = req.Input
	return result, nil
}
