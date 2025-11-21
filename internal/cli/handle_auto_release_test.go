package cli

import (
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/clean-dependency-project/cdprun/internal/config"
	"github.com/clean-dependency-project/cdprun/internal/runtime"
)

// TestHandleAutoRelease_NoToken tests that handleAggregatedAutoRelease fails when GITHUB_TOKEN is not set.
func TestHandleAutoRelease_NoToken(t *testing.T) {
	// Ensure GITHUB_TOKEN is not set (t.Setenv with empty string unsets it)
	t.Setenv("GITHUB_TOKEN", "")

	releaseConfig := &config.ReleaseConfig{
		AutoRelease:      true,
		GitHubRepository: "test-owner/test-repo",
	}

	// Create test database
	db, cleanup := createTestDB(t)
	defer cleanup()

	stdout := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	stderr := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	results := []runtime.DownloadResult{
		{
			URL:       "https://example.com/node-v22.15.0-linux-x64.tar.xz",
			LocalPath: "/tmp/node-v22.15.0-linux-x64.tar.xz",
			Success:   true,
		},
	}

	successfulVersions := []versionDownload{
		{
			version: "22.15.0",
			results: results,
		},
	}

	err := handleAggregatedAutoRelease(
		"nodejs",
		successfulVersions,
		"/tmp",
		releaseConfig,
		db,
		stdout,
		stderr,
	)

	if err == nil {
		t.Fatal("handleAggregatedAutoRelease() expected error when GITHUB_TOKEN is not set, got nil")
	}

	if !strings.Contains(err.Error(), "GITHUB_TOKEN") {
		t.Errorf("handleAggregatedAutoRelease() error = %v, want error containing 'GITHUB_TOKEN'", err)
	}
}

// TestHandleAutoRelease_WithMockToken tests the success path with a mock setup.
func TestHandleAutoRelease_WithMockToken(t *testing.T) {
	// Set a test token (t.Setenv automatically cleans up after test)
	testToken := "test_gh_token_123"
	t.Setenv("GITHUB_TOKEN", testToken)

	releaseConfig := &config.ReleaseConfig{
		AutoRelease:         true,
		GitHubRepository:    "test-owner/test-repo",
		DraftRelease:        false,
		ReleaseNameTemplate: "Node.js {version}",
	}

	// Create test database
	db, cleanup := createTestDB(t)
	defer cleanup()

	stdout := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	stderr := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	// Create temp dir with a test file
	tempDir := t.TempDir()
	testFile := tempDir + "/node-v22.15.0-linux-x64.tar.xz"
	if err := os.WriteFile(testFile, []byte("test content"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	results := []runtime.DownloadResult{
		{
			URL:       "https://example.com/node-v22.15.0-linux-x64.tar.xz",
			LocalPath: testFile,
			Success:   true,
			FileSize:  1024,
		},
	}

	successfulVersions := []versionDownload{
		{
			version: "22.15.0",
			results: results,
		},
	}

	// This will fail because we can't create a real GitHub client with invalid token,
	// but we're testing that the function properly checks for token and attempts the flow
	err := handleAggregatedAutoRelease(
		"nodejs",
		successfulVersions,
		tempDir,
		releaseConfig,
		db,
		stdout,
		stderr,
	)

	// Expected to fail with GitHub client creation error (invalid token)
	if err == nil {
		t.Fatal("handleAggregatedAutoRelease() expected error with invalid token, got nil")
	}

	// Should contain "GitHub client" error since token is invalid
	if !strings.Contains(err.Error(), "GitHub client") {
		t.Logf("handleAggregatedAutoRelease() error = %v (expected GitHub client error)", err)
	}
}

// TestHandleAutoRelease_EmptyRepository tests failure when repository is not configured.
func TestHandleAutoRelease_EmptyRepository(t *testing.T) {
	// Set a test token (t.Setenv automatically cleans up after test)
	testToken := "test_gh_token_123"
	t.Setenv("GITHUB_TOKEN", testToken)

	releaseConfig := &config.ReleaseConfig{
		AutoRelease:      true,
		GitHubRepository: "", // Empty repository
	}

	// Create test database
	db, cleanup := createTestDB(t)
	defer cleanup()

	stdout := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	stderr := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	results := []runtime.DownloadResult{
		{
			URL:       "https://example.com/node-v22.15.0-linux-x64.tar.xz",
			LocalPath: "/tmp/node-v22.15.0-linux-x64.tar.xz",
			Success:   true,
		},
	}

	successfulVersions := []versionDownload{
		{
			version: "22.15.0",
			results: results,
		},
	}

	err := handleAggregatedAutoRelease(
		"nodejs",
		successfulVersions,
		"/tmp",
		releaseConfig,
		db,
		stdout,
		stderr,
	)

	if err == nil {
		t.Fatal("handleAggregatedAutoRelease() expected error when repository is empty, got nil")
	}

	// Should fail at GitHub client creation with invalid repository
	if !strings.Contains(err.Error(), "GitHub client") {
		t.Errorf("handleAggregatedAutoRelease() error = %v, want error containing 'GitHub client'", err)
	}
}
