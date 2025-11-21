package logger

import (
	"errors"
	"log/slog"
	"os"
	"strings"
)

// New sets up the slog logger with level and format from arguments.
// logLevel: "info", "debug", "warn", "error"
// logFormat: "json" or "text"
// Returns (*slog.Logger, error)
func New(logLevel, logFormat string) (*slog.Logger, error) {
	if strings.TrimSpace(logLevel) == "" || strings.TrimSpace(logFormat) == "" {
		return nil, errors.New("logLevel and logFormat must not be empty")
	}
	var level slog.Level
	switch strings.ToLower(logLevel) {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	case "info":
		level = slog.LevelInfo
	default:
		return nil, errors.New("invalid logLevel: " + logLevel)
	}

	var handler slog.Handler
	switch strings.ToLower(logFormat) {
	case "json":
		handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	case "text":
		handler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	default:
		return nil, errors.New("invalid logFormat: " + logFormat)
	}

	logger := slog.New(handler)
	slog.SetDefault(logger)
	return logger, nil
}
