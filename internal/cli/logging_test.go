package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
)

func TestNewLoggers(t *testing.T) {
	tests := []struct {
		name  string
		level slog.Level
	}{
		{
			name:  "debug level",
			level: slog.LevelDebug,
		},
		{
			name:  "info level",
			level: slog.LevelInfo,
		},
		{
			name:  "warn level",
			level: slog.LevelWarn,
		},
		{
			name:  "error level",
			level: slog.LevelError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr := NewLoggers(tt.level)
			if stdout == nil {
				t.Error("NewLoggers() stdout is nil")
			}
			if stderr == nil {
				t.Error("NewLoggers() stderr is nil")
			}
		})
	}
}

func TestNewLoggersWithOutputFormat(t *testing.T) {
	tests := []struct {
		name        string
		level       slog.Level
		outputFormat string
	}{
		{
			name:        "json format",
			level:       slog.LevelInfo,
			outputFormat: "json",
		},
		{
			name:        "text format (backward compatibility)",
			level:       slog.LevelInfo,
			outputFormat: "text",
		},
		{
			name:        "empty format",
			level:       slog.LevelInfo,
			outputFormat: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr := NewLoggersWithOutputFormat(tt.level, tt.outputFormat)
			if stdout == nil {
				t.Error("NewLoggersWithOutputFormat() stdout is nil")
			}
			if stderr == nil {
				t.Error("NewLoggersWithOutputFormat() stderr is nil")
			}
		})
	}
}

func TestNewLoggersWithOutputFormat_JSONOutput(t *testing.T) {
	// Capture stderr to verify JSON format
	oldStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("Failed to create pipe: %v", err)
	}
	os.Stderr = w

	defer func() {
		os.Stderr = oldStderr
		_ = w.Close()
	}()

	stdout, stderr := NewLoggersWithOutputFormat(slog.LevelInfo, "json")
	if stdout == nil || stderr == nil {
		t.Fatal("NewLoggersWithOutputFormat() returned nil loggers")
	}

	// Log a message
	stderr.Info("test message", "key", "value")

	// Close write end and read output
	_ = w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)

	output := buf.String()
	if !strings.Contains(output, "test message") {
		t.Errorf("Expected log output to contain 'test message', got: %s", output)
	}

	// Verify it's valid JSON
	var jsonData map[string]interface{}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) > 0 {
		if err := json.Unmarshal([]byte(lines[len(lines)-1]), &jsonData); err != nil {
			t.Errorf("Log output is not valid JSON: %v, output: %s", err, output)
		}
	}

	// Verify timestamp field name
	if jsonData != nil {
		if _, ok := jsonData["timestamp"]; !ok {
			t.Errorf("Expected 'timestamp' field in JSON output, got: %v", jsonData)
		}
	}
}

func TestParseLogLevelOrDefault(t *testing.T) {
	tests := []struct {
		name     string
		levelStr string
		want     slog.Level
	}{
		{
			name:     "debug level",
			levelStr: "debug",
			want:     slog.LevelDebug,
		},
		{
			name:     "info level",
			levelStr: "info",
			want:     slog.LevelInfo,
		},
		{
			name:     "warn level",
			levelStr: "warn",
			want:     slog.LevelWarn,
		},
		{
			name:     "error level",
			levelStr: "error",
			want:     slog.LevelError,
		},
		{
			name:     "invalid level defaults to info",
			levelStr: "invalid",
			want:     slog.LevelInfo,
		},
		{
			name:     "empty string defaults to info",
			levelStr: "",
			want:     slog.LevelInfo,
		},
		{
			name:     "uppercase debug",
			levelStr: "DEBUG",
			want:     slog.LevelInfo, // Should default since it's case-sensitive
		},
		{
			name:     "mixed case",
			levelStr: "Info",
			want:     slog.LevelInfo, // Should default since it's case-sensitive
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseLogLevelOrDefault(tt.levelStr)
			if got != tt.want {
				t.Errorf("ParseLogLevelOrDefault(%q) = %v, want %v", tt.levelStr, got, tt.want)
			}
		})
	}
}

func TestNewAuditLogger(t *testing.T) {
	// This function is deprecated and always returns nil logger
	logger, cleanup, err := NewAuditLogger()
	if err != nil {
		t.Errorf("NewAuditLogger() error = %v, want nil", err)
	}
	if logger != nil {
		t.Error("NewAuditLogger() expected nil logger (deprecated)")
	}
	if cleanup == nil {
		t.Error("NewAuditLogger() cleanup function is nil")
	}

	// Call cleanup to ensure it doesn't panic
	cleanup()
}

func TestLoggersWriteToStderr(t *testing.T) {
	// Verify that both loggers write to stderr
	oldStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("Failed to create pipe: %v", err)
	}
	os.Stderr = w

	defer func() {
		os.Stderr = oldStderr
		_ = w.Close()
	}()

	stdout, stderr := NewLoggers(slog.LevelInfo)

	// Log from both loggers
	stdout.Info("stdout message")
	stderr.Info("stderr message")

	// Close write end and read output
	_ = w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)

	output := buf.String()
	if !strings.Contains(output, "stdout message") {
		t.Error("Expected stdout logger to write to stderr")
	}
	if !strings.Contains(output, "stderr message") {
		t.Error("Expected stderr logger to write to stderr")
	}
}

