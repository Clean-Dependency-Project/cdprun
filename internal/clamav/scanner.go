package clamav

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Sentinel errors
var (
	ErrDockerUnavailable = errors.New("docker command not available")
)

// Scanner scans files for malware.
type Scanner interface {
	Scan(ctx context.Context, path string) (Result, error)
}

// Result represents the outcome of a malware scan.
type Result struct {
	Clean    bool
	Threats  []string
	Metadata Metadata
}

// Metadata contains information about the scan environment.
type Metadata struct {
	EngineVersion string
	DatabaseDate  string
	ScanDuration  time.Duration
}

// DockerScanner implements Scanner using ClamAV in a Docker container.
type DockerScanner struct {
	runner CommandRunner
	image  string
	logger *slog.Logger
}

// NewDockerScanner creates a scanner that uses ClamAV in Docker.
func NewDockerScanner(runner CommandRunner, image string, logger *slog.Logger) *DockerScanner {
	return &DockerScanner{
		runner: runner,
		image:  image,
		logger: logger,
	}
}

// Scan scans a file for malware using ClamAV in a Docker container.
func (s *DockerScanner) Scan(ctx context.Context, path string) (Result, error) {
	start := time.Now()

	// Check Docker availability
	if !isDockerAvailable(s.runner) {
		return Result{}, ErrDockerUnavailable
	}

	// Ensure ClamAV image is present
	if err := ensureImage(ctx, s.runner, s.image); err != nil {
		return Result{}, fmt.Errorf("failed to ensure image: %w", err)
	}

	// Get ClamAV version
	version, err := s.getVersion(ctx)
	if err != nil {
		// Log but don't fail - version is nice to have but not critical
		if s.logger != nil {
			s.logger.Warn("failed to get ClamAV version", "error", err)
		}
		version = "unknown"
	}

	// Get absolute path for volume mount
	absPath, err := filepath.Abs(path)
	if err != nil {
		return Result{}, fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Run clamscan in container
	output, err := runClamscan(ctx, s.runner, s.image, absPath)

	// Extract exit code
	exitCode := 0
	if err != nil {
		exitCode = extractExitCode(err)
		if exitCode < 0 {
			// Non-exit error (e.g., command not found, context cancelled)
			return Result{}, fmt.Errorf("failed to run clamscan: %w", err)
		}
	}

	// Parse results
	result, err := parseResult(output, exitCode, version)
	if err != nil {
		return Result{}, err
	}

	result.Metadata.ScanDuration = time.Since(start)

	return result, nil
}

// getVersion retrieves the ClamAV version from the container.
func (s *DockerScanner) getVersion(ctx context.Context) (string, error) {
	output, err := s.runner.Run(ctx, "docker", "run", "--rm", s.image, "clamscan", "--version")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// isDockerAvailable checks if the docker command is available.
func isDockerAvailable(runner CommandRunner) bool {
	_, err := runner.Run(context.Background(), "docker", "--version")
	return err == nil
}

// ensureImage ensures the ClamAV image is available locally.
func ensureImage(ctx context.Context, runner CommandRunner, image string) error {
	// Check if image exists
	_, err := runner.Run(ctx, "docker", "image", "inspect", image)
	if err == nil {
		return nil // Image exists
	}

	// Pull image
	_, err = runner.Run(ctx, "docker", "pull", image)
	if err != nil {
		return fmt.Errorf("failed to pull image %s: %w", image, err)
	}

	return nil
}

// runClamscan executes clamscan in a Docker container.
func runClamscan(ctx context.Context, runner CommandRunner, image, path string) ([]byte, error) {
	args := buildDockerArgs(image, path, "/scan")
	return runner.Run(ctx, "docker", args...)
}

// buildDockerArgs constructs arguments for docker run command.
func buildDockerArgs(image, hostPath, containerPath string) []string {
	return []string{
		"run",
		"--rm",                                                 // Remove container after scan
		"-v", fmt.Sprintf("%s:%s:ro", hostPath, containerPath), // Mount as read-only
		image,
		"clamscan",
		"--stdout", // Output to stdout
		containerPath,
	}
}

// extractExitCode attempts to extract an exit code from an error.
// Returns -1 if the error is not an exit error.
func extractExitCode(err error) int {
	// Try exec.ExitError first (real commands)
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode()
	}

	// Try interface with ExitCode() method (mocks)
	type exitCoder interface {
		ExitCode() int
	}
	if exitErr, ok := err.(exitCoder); ok {
		return exitErr.ExitCode()
	}

	return -1
}

// ParseDockerOutput extracts useful information from Docker command output.
// This helper is exported for testing purposes.
func ParseDockerOutput(output string) string {
	// Remove Docker pull progress lines
	lines := strings.Split(output, "\n")
	var filtered []string

	for _, line := range lines {
		// Skip Docker pull progress
		if strings.Contains(line, "Pulling from") ||
			strings.Contains(line, "Digest:") ||
			strings.Contains(line, "Status:") ||
			strings.Contains(line, "Downloaded") {
			continue
		}
		filtered = append(filtered, line)
	}

	return strings.Join(filtered, "\n")
}
