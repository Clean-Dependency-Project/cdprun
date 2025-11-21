// Package cli provides the command-line interface for the runtime download system.
package cli

import (
	"log/slog"
	"os"
)

// LoggerConfig contains configuration for setting up loggers.
type LoggerConfig struct {
	Level slog.Level
}

// NewLoggers creates default loggers with JSON output.
func NewLoggers(level slog.Level) (*slog.Logger, *slog.Logger) {
	return NewLoggersWithOutputFormat(level, "json")
}

// NewLoggersWithOutputFormat creates loggers with awareness of output format.
// All logs are sent to stderr to keep stdout clean for JSON output.
// The outputFormat parameter is kept for backward compatibility but all output
// will be JSON formatted.
func NewLoggersWithOutputFormat(level slog.Level, outputFormat string) (*slog.Logger, *slog.Logger) {
	// Create JSON handler for logs with stderr as output
	handler := slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			// Add outputFormat to all log entries
			if a.Key == slog.TimeKey {
				return slog.Attr{
					Key:   "timestamp",
					Value: a.Value,
				}
			}
			return a
		},
	})

	// Both loggers write to stderr to keep stdout clean for JSON output
	stdout := slog.New(handler)
	stderr := slog.New(handler)

	return stdout, stderr
}

// ParseLogLevelOrDefault parses a log level string or returns a default level.
func ParseLogLevelOrDefault(levelStr string) slog.Level {
	switch levelStr {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo // Default to info level
	}
}

// NewAuditLogger creates a dedicated logger for verification audit trails.
// Returns the logger, cleanup function, and error.
// NOTE: Audit logging configuration was removed, this function is deprecated
func NewAuditLogger() (*slog.Logger, func(), error) {
	return nil, func() {}, nil // Always disabled
}
