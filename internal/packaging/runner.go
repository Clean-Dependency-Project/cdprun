package packaging

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

type CommandRunner interface {
	Run(ctx context.Context, dir string, name string, args []string, env []string) (stdout string, stderr string, err error)
}

type RealRunner struct{}

func (r RealRunner) Run(ctx context.Context, dir string, name string, args []string, env []string) (string, string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	if len(env) > 0 {
		cmd.Env = append(cmd.Environ(), env...)
	}

	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	err := cmd.Run()
	stdout := strings.TrimSpace(outBuf.String())
	stderr := strings.TrimSpace(errBuf.String())
	if err != nil {
		return stdout, stderr, fmt.Errorf("run %s %v: %w", name, args, err)
	}
	return stdout, stderr, nil
}
