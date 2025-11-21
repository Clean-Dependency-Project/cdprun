package cli

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/clean-dependency-project/cdprun/internal/config"
	gh "github.com/clean-dependency-project/cdprun/internal/github"
	"github.com/clean-dependency-project/cdprun/internal/platform"
	"github.com/clean-dependency-project/cdprun/internal/runtime"
	"github.com/clean-dependency-project/cdprun/internal/storage"
	"github.com/google/go-github/v57/github"
)

// TestReleaseManager_CreateRelease_Integration tests the full release flow with mock GitHub API.
func TestReleaseManager_CreateRelease_Integration(t *testing.T) {
	// Create test database
	db, dbCleanup := createTestDB(t)
	defer dbCleanup()

	// Create temporary directory with test artifacts
	tempDir := t.TempDir()

	// Create test artifact files
	testFiles := map[string]string{
		"node-v22.15.0-linux-x64.tar.xz":            "binary content",
		"node-v22.15.0-linux-x64.tar.xz.audit.json": `{"clamav_clean":true}`,
		"node-v22.15.0-linux-x64.tar.xz.sig":        "signature",
		"node-v22.15.0-linux-x64.tar.xz.cert":       "certificate",
	}

	for filename, content := range testFiles {
		filePath := filepath.Join(tempDir, filename)
		if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create test file %s: %v", filename, err)
		}
	}

	// Create download results
	downloadResults := []runtime.DownloadResult{
		{
			Success:   true,
			LocalPath: filepath.Join(tempDir, "node-v22.15.0-linux-x64.tar.xz"),
			FileSize:  14,
			Platform: platform.Platform{
				OS:   "linux",
				Arch: "x64",
			},
		},
	}

	// Mock GitHub API server
	releaseCreated := false
	uploadedFiles := make(map[string]bool)

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Logf("Mock server received: %s %s", r.Method, r.URL.Path)

		// Handle release creation
		if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/releases") && !strings.Contains(r.URL.Path, "/assets") {
			releaseCreated = true
			t.Logf("Creating release")

			// Return mock release
			mockRelease := &github.RepositoryRelease{
				ID:      github.Int64(12345),
				TagName: github.String("nodejs-v22.15.0-20231109T120000Z"),
				Name:    github.String("Node.js 22.15.0"),
				HTMLURL: github.String("https://github.com/test-owner/test-repo/releases/tag/nodejs-v22.15.0"),
			}

			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(mockRelease)
			return
		}

		// Handle asset uploads - GitHub API uses /releases/:id/assets
		if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/assets") {
			// Extract filename from query params
			filename := r.URL.Query().Get("name")
			t.Logf("Uploading asset: %s", filename)

			if filename != "" {
				uploadedFiles[filename] = true
			}

			// Return mock asset
			mockAsset := &github.ReleaseAsset{
				ID:                 github.Int64(67890),
				Name:               github.String(filename),
				BrowserDownloadURL: github.String("https://github.com/test-owner/test-repo/releases/download/tag/" + filename),
			}

			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(mockAsset)
			return
		}

		// Default response
		t.Logf("Unhandled request: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer mockServer.Close()

	// Create release config
	releaseConfig := &config.ReleaseConfig{
		AutoRelease:         true,
		GitHubRepository:    "test-owner/test-repo",
		DraftRelease:        false,
		ReleaseNameTemplate: "Node.js {version}",
	}

	// Create release manager with mock GitHub client
	stdout := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	stderr := slog.New(slog.NewJSONHandler(os.Stderr, nil))

	// Create mock GitHub client pointing to test server
	mockGitHub := createMockGitHubClient(t, mockServer.URL, "test_token", "test-owner/test-repo")

	releaseManager, err := NewReleaseManager(releaseConfig, mockGitHub, db, stdout, stderr)
	if err != nil {
		t.Fatalf("NewReleaseManager() error: %v", err)
	}

	// Execute the release creation
	release, err := releaseManager.CreateAggregatedRelease(
		"nodejs",
		[]string{"22.15.0"},
		downloadResults,
		tempDir,
		releaseConfig,
	)

	if err != nil {
		t.Fatalf("CreateAggregatedRelease() error: %v", err)
	}

	if release == nil {
		t.Fatalf("CreateAggregatedRelease() returned nil release")
		return // Help staticcheck understand execution stops
	}

	// Verify release was created
	if !releaseCreated {
		t.Error("GitHub release was not created")
	}

	// Verify all files were uploaded
	for filename := range testFiles {
		if !uploadedFiles[filename] {
			t.Errorf("File %s was not uploaded", filename)
		}
	}

	t.Logf("Uploaded %d files: %v", len(uploadedFiles), uploadedFiles)

	// Verify database record
	if release.Runtime != "nodejs" {
		t.Errorf("Release.Runtime = %s, want nodejs", release.Runtime)
	}

	if release.Version != "22.15.0" {
		t.Errorf("Release.Version = %s, want 22.15.0", release.Version)
	}

	if release.SemverMajor != 22 {
		t.Errorf("Release.SemverMajor = %d, want 22", release.SemverMajor)
	}

	if release.ReleaseURL == "" {
		t.Error("Release.ReleaseURL is empty")
	}

	// Verify artifacts JSON is valid
	if release.Artifacts == "" {
		t.Fatal("Release.Artifacts is empty")
	}

	var artifacts storage.ReleaseArtifacts
	if err := json.Unmarshal([]byte(release.Artifacts), &artifacts); err != nil {
		t.Fatalf("Failed to unmarshal artifacts JSON: %v", err)
	}

	// Verify artifacts structure
	if len(artifacts.Platforms) == 0 {
		t.Error("No platforms in artifacts JSON")
	}

	if artifacts.Metadata.TotalArtifacts != len(testFiles) {
		t.Errorf("Metadata.TotalArtifacts = %d, want %d", artifacts.Metadata.TotalArtifacts, len(testFiles))
	}

	// Verify we can retrieve the release from database
	retrieved, err := db.GetRelease("nodejs", "22.15.0")
	if err != nil {
		t.Fatalf("Failed to retrieve release from database: %v", err)
	}

	if retrieved.ID != release.ID {
		t.Errorf("Retrieved release ID = %d, want %d", retrieved.ID, release.ID)
	}
}

// TestUploadArtifacts_Integration tests artifact upload with mock server.
func TestUploadArtifacts_Integration(t *testing.T) {
	db, cleanup := createTestDB(t)
	defer cleanup()

	// Create temp files
	tempDir := t.TempDir()
	testFiles := []string{
		filepath.Join(tempDir, "test1.txt"),
		filepath.Join(tempDir, "test2.bin"),
	}

	for _, filePath := range testFiles {
		if err := os.WriteFile(filePath, []byte("test content"), 0644); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}
	}

	uploadCount := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uploadCount++
		filename := r.URL.Query().Get("name")

		mockAsset := &github.ReleaseAsset{
			ID:                 github.Int64(int64(uploadCount)),
			Name:               github.String(filename),
			BrowserDownloadURL: github.String("https://example.com/" + filename),
		}

		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(mockAsset)
	}))
	defer mockServer.Close()

	stdout := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	stderr := slog.New(slog.NewJSONHandler(os.Stderr, nil))

	rm := &ReleaseManager{
		db:     db,
		github: createMockGitHubClient(t, mockServer.URL, "test_token", "owner/repo"),
		stdout: stdout,
		stderr: stderr,
	}

	uploaded, err := rm.uploadArtifacts(12345, testFiles)
	if err != nil {
		t.Fatalf("uploadArtifacts() error: %v", err)
	}

	if len(uploaded) != len(testFiles) {
		t.Errorf("uploadArtifacts() uploaded %d files, want %d", len(uploaded), len(testFiles))
	}

	if uploadCount != len(testFiles) {
		t.Errorf("Mock server received %d uploads, want %d", uploadCount, len(testFiles))
	}

	// Verify all URLs and SHA256 hashes are present
	for _, filePath := range testFiles {
		filename := filepath.Base(filePath)
		info, exists := uploaded[filename]
		if !exists {
			t.Errorf("No info for uploaded file %s", filename)
		}
		if info.URL == "" {
			t.Errorf("Empty URL for file %s", filename)
		}
		if !strings.Contains(info.URL, filename) {
			t.Errorf("URL %s does not contain filename %s", info.URL, filename)
		}
		if info.SHA256 == "" {
			t.Errorf("Empty SHA256 for file %s", filename)
		}
	}
}

// Helper to create mock GitHub client that points to mock server
// Following the official go-github testing pattern
func createMockGitHubClient(t *testing.T, mockServerURL, token, repository string) *gh.Client {
	t.Helper()

	// Create standard HTTP client (no custom transport needed!)
	httpClient := &http.Client{}

	// Use the test helper from github package
	client, err := gh.NewTestClient(httpClient, mockServerURL, repository)
	if err != nil {
		t.Fatalf("NewTestClient() error: %v", err)
	}

	return client
}
