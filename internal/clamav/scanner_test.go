package clamav

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestIsDockerAvailable(t *testing.T) {
	tests := []struct {
		name   string
		runner *MockCommandRunner
		want   bool
	}{
		{
			name:   "docker available",
			runner: &MockCommandRunner{Output: []byte("Docker version 20.10.0"), Err: nil},
			want:   true,
		},
		{
			name:   "docker unavailable",
			runner: &MockCommandRunner{Output: nil, Err: fmt.Errorf("command not found")},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isDockerAvailable(tt.runner); got != tt.want {
				t.Errorf("isDockerAvailable() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildDockerArgs(t *testing.T) {
	args := buildDockerArgs("clamav/clamav-debian:latest", "/tmp/test.txt", "/scan")

	expected := []string{
		"run",
		"--rm",
		"-v", "/tmp/test.txt:/scan:ro",
		"clamav/clamav-debian:latest",
		"clamscan",
		"--stdout",
		"/scan",
	}

	if len(args) != len(expected) {
		t.Errorf("buildDockerArgs() returned %d args, want %d", len(args), len(expected))
		return
	}

	for i, arg := range args {
		if arg != expected[i] {
			t.Errorf("buildDockerArgs()[%d] = %v, want %v", i, arg, expected[i])
		}
	}
}

func TestDockerScanner_Scan_Clean(t *testing.T) {
	cleanOutput := []byte(`/scan: OK

----------- SCAN SUMMARY -----------
Known viruses: 8708721
Engine version: 1.5.1
Scanned directories: 0
Scanned files: 1
Infected files: 0
Data scanned: 52 B
Data read: 26 B (ratio 2.00:1)
Time: 8.997 sec (0 m 8 s)
Start Date: 2025:11:01 18:59:37
End Date:   2025:11:01 18:59:46
`)

	// Use a custom mock that returns different values per call
	callCount := 0
	mockRunner := &mockCommandRunnerMulti{
		responses: []mockResponse{
			{output: []byte("Docker version 20.10.0"), err: nil},                      // docker --version
			{output: nil, err: nil},                                                   // docker image inspect (exists)
			{output: []byte("ClamAV 1.5.1/27805/Mon Oct 27 09:50:30 2025"), err: nil}, // clamscan --version
			{output: cleanOutput, err: nil},                                           // clamscan scan
		},
		callCount: &callCount,
	}

	scanner := NewDockerScanner(mockRunner, "clamav/clamav-debian:latest", nil)

	result, err := scanner.Scan(context.Background(), "/tmp/test.txt")
	if err != nil {
		t.Fatalf("Scan() unexpected error: %v", err)
	}

	if !result.Clean {
		t.Error("expected Clean=true for clean file")
	}

	if len(result.Threats) != 0 {
		t.Errorf("expected no threats, got %v", result.Threats)
	}
}

func TestDockerScanner_Scan_Infected(t *testing.T) {
	infectedOutput := []byte(`/scan: Eicar-Signature FOUND

----------- SCAN SUMMARY -----------
Known viruses: 8708721
Engine version: 1.5.1
Scanned directories: 0
Scanned files: 1
Infected files: 1
Data scanned: 69 B
Data read: 69 B (ratio 1.00:1)
Time: 9.002 sec (0 m 9 s)
Start Date: 2025:11:01 18:59:52
End Date:   2025:11:01 19:00:01
`)

	// Use a custom mock that returns different values per call
	callCount := 0
	mockRunner := &mockCommandRunnerMulti{
		responses: []mockResponse{
			{output: []byte("Docker version 20.10.0"), err: nil},                      // docker --version
			{output: nil, err: nil},                                                   // docker image inspect (exists)
			{output: []byte("ClamAV 1.5.1/27805/Mon Oct 27 09:50:30 2025"), err: nil}, // clamscan --version
			{output: infectedOutput, err: &mockExitError{code: 1}},                    // clamscan scan
		},
		callCount: &callCount,
	}

	scanner := NewDockerScanner(mockRunner, "clamav/clamav-debian:latest", nil)

	result, err := scanner.Scan(context.Background(), "/tmp/eicar.txt")
	if err != nil {
		t.Fatalf("Scan() unexpected error: %v", err)
	}

	if result.Clean {
		t.Error("expected Clean=false for infected file")
	}

	if len(result.Threats) != 1 {
		t.Fatalf("expected 1 threat, got %d", len(result.Threats))
	}

	if result.Threats[0] != "Eicar-Signature" {
		t.Errorf("expected threat 'Eicar-Signature', got %s", result.Threats[0])
	}
}

// mockExitError simulates an exec.ExitError
type mockExitError struct {
	code int
}

func (e *mockExitError) Error() string {
	return fmt.Sprintf("exit status %d", e.code)
}

func (e *mockExitError) ExitCode() int {
	return e.code
}

// mockResponse represents a single call response
type mockResponse struct {
	output []byte
	err    error
}

// mockCommandRunnerMulti returns different responses per call
type mockCommandRunnerMulti struct {
	responses []mockResponse
	callCount *int
}

func (m *mockCommandRunnerMulti) Run(_ context.Context, _ string, _ ...string) ([]byte, error) {
	if *m.callCount >= len(m.responses) {
		return nil, fmt.Errorf("unexpected call %d", *m.callCount)
	}
	resp := m.responses[*m.callCount]
	*m.callCount++
	return resp.output, resp.err
}

func TestDockerScanner_Scan_DockerUnavailable(t *testing.T) {
	runner := &MockCommandRunner{
		Output: nil,
		Err:    fmt.Errorf("docker: command not found"),
	}

	scanner := NewDockerScanner(runner, "clamav/clamav-debian:latest", nil)

	_, err := scanner.Scan(context.Background(), "/tmp/test.txt")
	if err == nil {
		t.Fatal("expected error when Docker unavailable")
	}

	if !errors.Is(err, ErrDockerUnavailable) {
		t.Errorf("expected ErrDockerUnavailable, got: %v", err)
	}
}
