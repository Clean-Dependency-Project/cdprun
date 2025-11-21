package cli

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/clean-dependency-project/cdprun/internal/config"
	"github.com/clean-dependency-project/cdprun/internal/platform"
	"github.com/clean-dependency-project/cdprun/internal/runtime"
	"github.com/clean-dependency-project/cdprun/internal/storage"
)

func TestNewReleaseManager(t *testing.T) {
	stdout := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	stderr := slog.New(slog.NewJSONHandler(os.Stderr, nil))

	tests := []struct {
		name        string
		config      *config.ReleaseConfig
		github      GitHubReleaser
		db          DatabaseStore
		wantErr     bool
		wantNil     bool
		errContains string
	}{
		{
			name: "auto_release disabled",
			config: &config.ReleaseConfig{
				AutoRelease: false,
			},
			github:  &mockGitHubReleaser{},
			db:      &mockDatabaseStore{},
			wantErr: false,
			wantNil: true, // Should return nil when disabled
		},
		{
			name: "auto_release enabled, no repo",
			config: &config.ReleaseConfig{
				AutoRelease:      true,
				GitHubRepository: "",
			},
			github:      &mockGitHubReleaser{},
			db:          &mockDatabaseStore{},
			wantErr:     true,
			errContains: "github_repository is required",
		},
		{
			name: "auto_release enabled, nil github client",
			config: &config.ReleaseConfig{
				AutoRelease:      true,
				GitHubRepository: "owner/repo",
			},
			github:      nil,
			db:          &mockDatabaseStore{},
			wantErr:     true,
			errContains: "github client is required",
		},
		{
			name: "auto_release enabled, nil database",
			config: &config.ReleaseConfig{
				AutoRelease:      true,
				GitHubRepository: "owner/repo",
			},
			github:      &mockGitHubReleaser{},
			db:          nil,
			wantErr:     true,
			errContains: "database is required",
		},
		{
			name: "valid configuration",
			config: &config.ReleaseConfig{
				AutoRelease:      true,
				GitHubRepository: "owner/repo",
			},
			github:  &mockGitHubReleaser{},
			db:      &mockDatabaseStore{},
			wantErr: false,
			wantNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager, err := NewReleaseManager(tt.config, tt.github, tt.db, stdout, stderr)

			if tt.wantErr {
				if err == nil {
					t.Errorf("NewReleaseManager() expected error, got nil")
					return
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("NewReleaseManager() error = %v, want error containing %q", err, tt.errContains)
				}
				return
			}

			if err != nil {
				t.Errorf("NewReleaseManager() unexpected error: %v", err)
				return
			}

			if tt.wantNil {
				if manager != nil {
					t.Errorf("NewReleaseManager() expected nil when disabled, got %v", manager)
				}
				return
			}

			if manager == nil {
				t.Error("NewReleaseManager() returned nil manager")
				return
			}

			// Verify fields are set
			if manager.db == nil {
				t.Error("ReleaseManager.db is nil")
			}
			if manager.github == nil {
				t.Error("ReleaseManager.github is nil")
			}
		})
	}
}

func TestCollectArtifactFiles(t *testing.T) {
	stdout := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	stderr := slog.New(slog.NewJSONHandler(os.Stderr, nil))

	// Create temporary output directory with test files
	tempDir := t.TempDir()

	// Create test files
	testFiles := []string{
		"node-v22.15.0-linux-x64.tar.xz",
		"node-v22.15.0-linux-x64.tar.xz.audit.json",
		"node-v22.15.0-linux-x64.tar.xz.sig",
		"node-v22.15.0-linux-x64.tar.xz.cert",
		"node-v22.15.0-darwin-arm64.pkg",
		"node-v22.15.0-darwin-arm64.pkg.audit.json",
		"SHASUMS256.txt",
		"SHASUMS256.txt.asc",
		"unrelated-file.txt", // Should not be collected
	}

	for _, filename := range testFiles {
		filePath := filepath.Join(tempDir, filename)
		if err := os.WriteFile(filePath, []byte("test content"), 0644); err != nil {
			t.Fatalf("Failed to create test file %s: %v", filename, err)
		}
	}

	rm := &ReleaseManager{
		db:     &mockDatabaseStore{},
		github: &mockGitHubReleaser{},
		stdout: stdout,
		stderr: stderr,
	}

	files, err := rm.collectArtifactFiles(tempDir, "nodejs", "22.15.0")
	if err != nil {
		t.Fatalf("collectArtifactFiles() error: %v", err)
	}

	// Should collect files with version in name (6 files: 2 binaries + 2 audit.json + 2 sig/cert)
	// Note: SHASUMS files don't contain version, so they won't be collected by this logic
	// This is intentional - SHASUMS are per-version-family, not per exact version
	expectedMinCount := 6
	if len(files) < expectedMinCount {
		t.Errorf("collectArtifactFiles() collected %d files, want at least %d", len(files), expectedMinCount)
		t.Logf("Files collected: %v", files)
	}

	// Verify all collected files contain the version (excluding SHASUMS type files)
	for _, file := range files {
		filename := filepath.Base(file)
		if strings.Contains(filename, "22.15.0") {
			// This is a version-specific file - good
			continue
		}
		// If it doesn't contain version, it should be a SHASUMS type file
		if !strings.Contains(filename, "SHASUMS") && !strings.Contains(filename, "checksums") {
			t.Errorf("collected file %s does not contain version and is not a checksum file", filename)
		}
	}
}

func TestCollectArtifactFiles_EmptyDir(t *testing.T) {
	stdout := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	stderr := slog.New(slog.NewJSONHandler(os.Stderr, nil))

	tempDir := t.TempDir()

	rm := &ReleaseManager{
		db:     &mockDatabaseStore{},
		github: &mockGitHubReleaser{},
		stdout: stdout,
		stderr: stderr,
	}

	files, err := rm.collectArtifactFiles(tempDir, "nodejs", "22.15.0")
	if err != nil {
		t.Fatalf("collectArtifactFiles() error: %v", err)
	}

	if len(files) != 0 {
		t.Errorf("collectArtifactFiles() collected %d files from empty dir, want 0", len(files))
	}
}

func TestGetFileInfo(t *testing.T) {
	// Create mock download results
	downloadResults := []runtime.DownloadResult{
		{
			LocalPath: "/tmp/node-v22.15.0-linux-x64.tar.xz",
			FileSize:  30023544,
			Platform: platform.Platform{
				OS:   "linux",
				Arch: "x64",
			},
		},
		{
			LocalPath: "/tmp/node-v22.15.0-darwin-arm64.pkg",
			FileSize:  25000000,
			Platform: platform.Platform{
				OS:   "darwin",
				Arch: "arm64",
			},
		},
	}

	tests := []struct {
		name             string
		filename         string
		url              string
		wantIsCommonFile bool
		wantOS           string
		wantArch         string
		wantType         string
	}{
		{
			name:             "checksum file",
			filename:         "SHASUMS256.txt",
			url:              "https://example.com/SHASUMS256.txt",
			wantIsCommonFile: true,
			wantType:         "checksum_file",
		},
		{
			name:             "binary file - exact match",
			filename:         "node-v22.15.0-linux-x64.tar.xz",
			url:              "https://example.com/node-v22.15.0-linux-x64.tar.xz",
			wantIsCommonFile: false,
			wantOS:           "linux",
			wantArch:         "x64",
			wantType:         "binary",
		},
		{
			name:             "audit.json file",
			filename:         "node-v22.15.0-linux-x64.tar.xz.audit.json",
			url:              "https://example.com/audit.json",
			wantIsCommonFile: false,
			wantOS:           "linux",
			wantArch:         "x64",
			wantType:         "artifact",
		},
		{
			name:             "signature file",
			filename:         "node-v22.15.0-darwin-arm64.pkg.sig",
			url:              "https://example.com/sig",
			wantIsCommonFile: false,
			wantOS:           "darwin",
			wantArch:         "arm64",
			wantType:         "artifact",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := getFileInfo(tt.filename, tt.url, downloadResults)
			if err != nil {
				t.Fatalf("getFileInfo() error: %v", err)
			}

			if info.IsCommonFile != tt.wantIsCommonFile {
				t.Errorf("getFileInfo() IsCommonFile = %v, want %v", info.IsCommonFile, tt.wantIsCommonFile)
			}

			if !info.IsCommonFile {
				if info.OS != tt.wantOS {
					t.Errorf("getFileInfo() OS = %q, want %q", info.OS, tt.wantOS)
				}
				if info.Arch != tt.wantArch {
					t.Errorf("getFileInfo() Arch = %q, want %q", info.Arch, tt.wantArch)
				}
			}

			if info.Type != tt.wantType {
				t.Errorf("getFileInfo() Type = %q, want %q", info.Type, tt.wantType)
			}
		})
	}
}

func TestFormatReleaseName(t *testing.T) {
	stdout := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	stderr := slog.New(slog.NewJSONHandler(os.Stderr, nil))

	rm := &ReleaseManager{
		db:     &mockDatabaseStore{},
		github: &mockGitHubReleaser{},
		stdout: stdout,
		stderr: stderr,
	}

	tests := []struct {
		name     string
		template string
		runtime  string
		version  string
		want     string
	}{
		{
			name:     "empty template",
			template: "",
			runtime:  "nodejs",
			version:  "22.15.0",
			want:     "nodejs 22.15.0",
		},
		{
			name:     "template with runtime",
			template: "Release {runtime}",
			runtime:  "nodejs",
			version:  "22.15.0",
			want:     "Release nodejs",
		},
		{
			name:     "template with version",
			template: "Version {version}",
			runtime:  "nodejs",
			version:  "22.15.0",
			want:     "Version 22.15.0",
		},
		{
			name:     "template with both",
			template: "{runtime} v{version}",
			runtime:  "nodejs",
			version:  "22.15.0",
			want:     "nodejs v22.15.0",
		},
		{
			name:     "complex template",
			template: "Node.js {version} Release",
			runtime:  "nodejs",
			version:  "22.15.0",
			want:     "Node.js 22.15.0 Release",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rm.formatReleaseName(tt.template, tt.runtime, tt.version)
			if got != tt.want {
				t.Errorf("formatReleaseName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGenerateReleaseBody(t *testing.T) {
	stdout := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	stderr := slog.New(slog.NewJSONHandler(os.Stderr, nil))

	rm := &ReleaseManager{
		db:     &mockDatabaseStore{},
		github: &mockGitHubReleaser{},
		stdout: stdout,
		stderr: stderr,
	}

	results := []runtime.DownloadResult{
		{
			Success:   true,
			LocalPath: "/tmp/node-v22.15.0-linux-x64.tar.xz",
			Platform: platform.Platform{
				OS:   "linux",
				Arch: "x64",
			},
		},
		{
			Success:   true,
			LocalPath: "/tmp/node-v22.15.0-darwin-arm64.pkg",
			Platform: platform.Platform{
				OS:   "darwin",
				Arch: "arm64",
			},
		},
		{
			Success:   false, // Failed download - should not be included
			LocalPath: "/tmp/node-v22.15.0-win-x64.msi",
			Platform: platform.Platform{
				OS:   "windows",
				Arch: "x64",
			},
		},
	}

	body := rm.generateAggregatedReleaseBody("nodejs", []string{"22.15.0"}, results)

	// Verify content - aggregated release body format uses Title case
	if !strings.Contains(body, "Nodejs") || !strings.Contains(body, "22.15.0") {
		t.Error("Release body missing runtime and version header")
	}

	if !strings.Contains(body, "linux-x64") {
		t.Error("Release body missing linux-x64 platform")
	}

	if !strings.Contains(body, "darwin-arm64") {
		t.Error("Release body missing darwin-arm64 platform")
	}

	if strings.Contains(body, "windows-x64") {
		t.Error("Release body should not include failed download (windows-x64)")
	}

	if !strings.Contains(body, "Checksum verified") {
		t.Error("Release body missing verification info")
	}

	if !strings.Contains(body, "ClamAV scanned") {
		t.Error("Release body missing ClamAV info")
	}

	// Note: Cosign info is not included in aggregated release body format
}

func TestBuildArtifactsJSON(t *testing.T) {
	stdout := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	stderr := slog.New(slog.NewJSONHandler(os.Stderr, nil))

	rm := &ReleaseManager{
		db:     &mockDatabaseStore{},
		github: &mockGitHubReleaser{},
		stdout: stdout,
		stderr: stderr,
	}

	uploadedArtifacts := map[string]artifactInfo{
		"node-v22.15.0-linux-x64.tar.xz": {
			URL:    "https://github.com/owner/repo/releases/download/tag/node-v22.15.0-linux-x64.tar.xz",
			SHA256: "dafe2e8f82cb97de1bd10db9e2ec4c07bbf53389b0799b1e095a918951e78fd4",
		},
		"node-v22.15.0-linux-x64.tar.xz.audit.json": {
			URL:    "https://github.com/owner/repo/releases/download/tag/node-v22.15.0-linux-x64.tar.xz.audit.json",
			SHA256: "",
		},
		"node-v22.15.0-linux-x64.tar.xz.sig": {
			URL:    "https://github.com/owner/repo/releases/download/tag/node-v22.15.0-linux-x64.tar.xz.sig",
			SHA256: "abc123def456",
		},
		"node-v22.15.0-linux-x64.tar.xz.cert": {
			URL:    "https://github.com/owner/repo/releases/download/tag/node-v22.15.0-linux-x64.tar.xz.cert",
			SHA256: "xyz789abc012",
		},
		"SHASUMS256.txt": {
			URL:    "https://github.com/owner/repo/releases/download/tag/SHASUMS256.txt",
			SHA256: "",
		},
	}

	downloadResults := []runtime.DownloadResult{
		{
			LocalPath: "/tmp/node-v22.15.0-linux-x64.tar.xz",
			FileSize:  30023544,
			Platform: platform.Platform{
				OS:   "linux",
				Arch: "x64",
			},
		},
	}

	jsonStr, err := rm.buildArtifactsJSON(uploadedArtifacts, downloadResults)
	if err != nil {
		t.Fatalf("buildArtifactsJSON() error: %v", err)
	}

	if jsonStr == "" {
		t.Fatal("buildArtifactsJSON() returned empty string")
	}

	// Verify it's valid JSON
	var artifacts storage.ReleaseArtifacts
	if err := json.Unmarshal([]byte(jsonStr), &artifacts); err != nil {
		t.Fatalf("buildArtifactsJSON() produced invalid JSON: %v", err)
	}

	// Verify structure
	if len(artifacts.Platforms) == 0 {
		t.Error("buildArtifactsJSON() produced no platforms")
	}

	// Verify common files
	foundChecksum := false
	for _, cf := range artifacts.CommonFiles {
		if cf.Filename == "SHASUMS256.txt" {
			foundChecksum = true
			if cf.Type != "checksum_file" {
				t.Errorf("common file type = %q, want %q", cf.Type, "checksum_file")
			}
		}
	}
	if !foundChecksum {
		t.Error("buildArtifactsJSON() did not create common file for SHASUMS256.txt")
	}

	// Verify metadata
	if artifacts.Metadata.TotalArtifacts != len(uploadedArtifacts) {
		t.Errorf("metadata.TotalArtifacts = %d, want %d", artifacts.Metadata.TotalArtifacts, len(uploadedArtifacts))
	}

	if artifacts.Metadata.PlatformCount == 0 {
		t.Error("metadata.PlatformCount is 0")
	}
}

// Helper function to create test database
func createTestDB(t *testing.T) (*storage.DB, func()) {
	t.Helper()

	tmpfile, err := os.CreateTemp("", "test-releases-*.db")
	if err != nil {
		t.Fatalf("Failed to create temp db file: %v", err)
	}
	dbPath := tmpfile.Name()
	_ = tmpfile.Close()

	db, err := storage.InitDB(storage.Config{
		DatabasePath: dbPath,
		LogLevel:     "silent",
	})
	if err != nil {
		_ = os.Remove(dbPath)
		t.Fatalf("Failed to initialize test database: %v", err)
	}

	cleanup := func() {
		_ = db.Close()
		_ = os.Remove(dbPath)
	}

	return db, cleanup
}
