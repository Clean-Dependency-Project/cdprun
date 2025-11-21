// Package clamav provides malware scanning using ClamAV in Docker containers.
package clamav

import (
	"context"
	"os/exec"
)

// CommandRunner executes external commands.
// This interface enables testing without actual command execution.
type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

// RealCommandRunner executes actual system commands.
type RealCommandRunner struct{}

// NewRealCommandRunner creates a command runner that executes real commands.
func NewRealCommandRunner() *RealCommandRunner {
	return &RealCommandRunner{}
}

// Run executes a command and returns combined stdout/stderr output.
func (r *RealCommandRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.CombinedOutput()
}

// MockCommandRunner is a test double for CommandRunner.
type MockCommandRunner struct {
	Output []byte
	Err    error
	Calls  [][]string // Track calls for debugging
}

// Run returns the configured output and error.
func (m *MockCommandRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	if m.Calls == nil {
		m.Calls = [][]string{}
	}
	call := append([]string{name}, args...)
	m.Calls = append(m.Calls, call)
	return m.Output, m.Err
}

