package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/google/go-github/v57/github"
)

// TestClient_CreateRelease_Integration tests the full CreateRelease flow with a mock server.
func TestClient_CreateRelease_Integration(t *testing.T) {
	tests := []struct {
		name           string
		tag            string
		releaseName    string
		body           string
		draft          bool
		mockStatusCode int
		mockResponse   *github.RepositoryRelease
		wantErr        bool
		errContains    string
	}{
		{
			name:           "successful release creation",
			tag:            "v1.0.0",
			releaseName:    "Release 1.0.0",
			body:           "Release notes",
			draft:          false,
			mockStatusCode: http.StatusCreated,
			mockResponse: &github.RepositoryRelease{
				ID:      github.Int64(12345),
				TagName: github.String("v1.0.0"),
				Name:    github.String("Release 1.0.0"),
				HTMLURL: github.String("https://github.com/owner/repo/releases/tag/v1.0.0"),
			},
			wantErr: false,
		},
		{
			name:           "draft release",
			tag:            "v2.0.0-beta",
			releaseName:    "Beta Release",
			body:           "Beta notes",
			draft:          true,
			mockStatusCode: http.StatusCreated,
			mockResponse: &github.RepositoryRelease{
				ID:      github.Int64(67890),
				TagName: github.String("v2.0.0-beta"),
				Name:    github.String("Beta Release"),
				Draft:   github.Bool(true),
				HTMLURL: github.String("https://github.com/owner/repo/releases/tag/v2.0.0-beta"),
			},
			wantErr: false,
		},
		{
			name:           "API error - unauthorized",
			tag:            "v1.0.0",
			releaseName:    "Release",
			body:           "Body",
			draft:          false,
			mockStatusCode: http.StatusUnauthorized,
			mockResponse:   nil,
			wantErr:        true,
			errContains:    "failed to create release",
		},
		{
			name:           "API error - conflict (release exists)",
			tag:            "v1.0.0",
			releaseName:    "Release",
			body:           "Body",
			draft:          false,
			mockStatusCode: http.StatusConflict,
			mockResponse:   nil,
			wantErr:        true,
			errContains:    "failed to create release",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify request
				if r.Method != http.MethodPost {
					t.Errorf("Expected POST request, got %s", r.Method)
				}

				if !strings.Contains(r.URL.Path, "/repos/owner/repo/releases") {
					t.Errorf("Expected /repos/owner/repo/releases path, got %s", r.URL.Path)
				}

				// Parse request body
				var reqBody map[string]interface{}
				bodyBytes, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(bodyBytes, &reqBody)

				// Verify request fields
				if reqBody["tag_name"] != tt.tag {
					t.Errorf("Expected tag_name %s, got %v", tt.tag, reqBody["tag_name"])
				}
				if reqBody["name"] != tt.releaseName {
					t.Errorf("Expected name %s, got %v", tt.releaseName, reqBody["name"])
				}
				if reqBody["draft"] != tt.draft {
					t.Errorf("Expected draft %v, got %v", tt.draft, reqBody["draft"])
				}

				// Send response
				w.WriteHeader(tt.mockStatusCode)
			if tt.mockResponse != nil {
				_ = json.NewEncoder(w).Encode(tt.mockResponse)
			} else {
				_ = json.NewEncoder(w).Encode(map[string]string{
						"message": "Validation Failed",
					})
				}
			}))
			defer server.Close()

			// Create client with mock server
			client := createTestClientWithBaseURL(t, server.URL, "test_token", "owner/repo")

			// Test CreateRelease
			release, err := client.CreateRelease(tt.tag, tt.releaseName, tt.body, tt.draft)

			if tt.wantErr {
				if err == nil {
					t.Errorf("CreateRelease() expected error, got nil")
					return
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("CreateRelease() error = %v, want error containing %q", err, tt.errContains)
				}
				return
			}

			if err != nil {
				t.Errorf("CreateRelease() unexpected error: %v", err)
				return
			}

			if release == nil {
				t.Fatal("CreateRelease() returned nil release")
			}

			// Verify response
			if *release.ID != *tt.mockResponse.ID {
				t.Errorf("Release ID = %d, want %d", *release.ID, *tt.mockResponse.ID)
			}
			if *release.TagName != tt.tag {
				t.Errorf("Release TagName = %s, want %s", *release.TagName, tt.tag)
			}
			if *release.Name != tt.releaseName {
				t.Errorf("Release Name = %s, want %s", *release.Name, tt.releaseName)
			}
		})
	}
}

// TestClient_GetRelease_Integration tests the GetRelease flow with a mock server.
func TestClient_GetRelease_Integration(t *testing.T) {
	tests := []struct {
		name           string
		tag            string
		mockStatusCode int
		mockResponse   *github.RepositoryRelease
		wantErr        bool
		expectNotFound bool
	}{
		{
			name:           "successful get release",
			tag:            "v1.0.0",
			mockStatusCode: http.StatusOK,
			mockResponse: &github.RepositoryRelease{
				ID:      github.Int64(12345),
				TagName: github.String("v1.0.0"),
				HTMLURL: github.String("https://github.com/owner/repo/releases/tag/v1.0.0"),
			},
			wantErr: false,
		},
		{
			name:           "release not found",
			tag:            "v99.0.0",
			mockStatusCode: http.StatusNotFound,
			mockResponse:   nil,
			wantErr:        true,
			expectNotFound: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !strings.Contains(r.URL.Path, fmt.Sprintf("/repos/owner/repo/releases/tags/%s", tt.tag)) {
					t.Errorf("Expected path with tag %s, got %s", tt.tag, r.URL.Path)
				}

				w.WriteHeader(tt.mockStatusCode)
			if tt.mockResponse != nil {
				_ = json.NewEncoder(w).Encode(tt.mockResponse)
			} else {
				_ = json.NewEncoder(w).Encode(map[string]string{
						"message": "Not Found",
					})
				}
			}))
			defer server.Close()

			client := createTestClientWithBaseURL(t, server.URL, "test_token", "owner/repo")

			release, err := client.GetRelease(tt.tag)

			if tt.wantErr {
				if err == nil {
					t.Errorf("GetRelease() expected error, got nil")
					return
				}
				if tt.expectNotFound && err != ErrReleaseNotFound {
					t.Errorf("GetRelease() error = %v, want %v", err, ErrReleaseNotFound)
				}
				return
			}

			if err != nil {
				t.Errorf("GetRelease() unexpected error: %v", err)
				return
			}

			if *release.TagName != tt.tag {
				t.Errorf("Release TagName = %s, want %s", *release.TagName, tt.tag)
			}
		})
	}
}

// TestClient_UploadAsset_Integration tests the UploadAsset flow with a mock server.
func TestClient_UploadAsset_Integration(t *testing.T) {
	// Create a temporary test file
	tmpfile, err := os.CreateTemp("", "test-asset-*.txt")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpfile.Name()) }()

	testContent := "test asset content"
	if _, err := tmpfile.Write([]byte(testContent)); err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}
	_ = tmpfile.Close()

	tests := []struct {
		name           string
		releaseID      int64
		filePath       string
		mockStatusCode int
		mockResponse   *github.ReleaseAsset
		wantErr        bool
	}{
		{
			name:           "successful upload",
			releaseID:      12345,
			filePath:       tmpfile.Name(),
			mockStatusCode: http.StatusCreated,
			mockResponse: &github.ReleaseAsset{
				ID:                 github.Int64(67890),
				Name:               github.String("test-asset.txt"),
				Size:               github.Int(int(len(testContent))),
				BrowserDownloadURL: github.String("https://github.com/owner/repo/releases/download/v1.0.0/test-asset.txt"),
			},
			wantErr: false,
		},
		{
			name:           "upload error - unauthorized",
			releaseID:      12345,
			filePath:       tmpfile.Name(),
			mockStatusCode: http.StatusUnauthorized,
			mockResponse:   nil,
			wantErr:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uploadReceived := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("Expected POST request, got %s", r.Method)
				}

				// Verify the upload path
				if !strings.Contains(r.URL.Path, fmt.Sprintf("/repos/owner/repo/releases/%d/assets", tt.releaseID)) {
					t.Errorf("Expected release asset upload path, got %s", r.URL.Path)
				}

				uploadReceived = true

				w.WriteHeader(tt.mockStatusCode)
			if tt.mockResponse != nil {
				_ = json.NewEncoder(w).Encode(tt.mockResponse)
				}
			}))
			defer server.Close()

			client := createTestClientWithBaseURL(t, server.URL, "test_token", "owner/repo")

			asset, err := client.UploadAsset(tt.releaseID, tt.filePath)

			if tt.wantErr {
				if err == nil {
					t.Errorf("UploadAsset() expected error, got nil")
					return
				}
				return
			}

			if err != nil {
				t.Errorf("UploadAsset() unexpected error: %v", err)
				return
			}

			if !uploadReceived {
				t.Error("Upload request was not received by mock server")
			}

			if asset == nil {
				t.Fatal("UploadAsset() returned nil asset")
			}

			// Verify download URL
			downloadURL := client.GetAssetDownloadURL(asset)
			if downloadURL != *tt.mockResponse.BrowserDownloadURL {
				t.Errorf("Download URL = %s, want %s", downloadURL, *tt.mockResponse.BrowserDownloadURL)
			}
		})
	}
}

// TestClient_GetReleaseURL_Integration tests GetReleaseURL with a real release object.
func TestClient_GetReleaseURL_Integration(t *testing.T) {
	client, err := NewClient("test_token", "owner/repo")
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}

	testURL := "https://github.com/owner/repo/releases/tag/v1.0.0"
	release := &github.RepositoryRelease{
		HTMLURL: github.String(testURL),
	}

	url := client.GetReleaseURL(release)
	if url != testURL {
		t.Errorf("GetReleaseURL() = %s, want %s", url, testURL)
	}
}

// Helper function to create a test client with custom base URL
// Following the official go-github testing pattern
func createTestClientWithBaseURL(t *testing.T, baseURL, token, repository string) *Client {
	t.Helper()

	owner, repo, err := parseRepository(repository)
	if err != nil {
		t.Fatalf("parseRepository() error: %v", err)
	}

	// Create a GitHub client that points to our mock server
	// Following official go-github pattern: https://github.com/google/go-github/blob/master/github/github_test.go
	httpClient := &http.Client{}
	ghClient := github.NewClient(httpClient).WithAuthToken(token)

	// Parse the mock server URL properly
	url, err := url.Parse(baseURL + "/")
	if err != nil {
		t.Fatalf("Failed to parse server URL: %v", err)
	}
	
	// Set both BaseURL and UploadURL to point to mock server
	ghClient.BaseURL = url
	ghClient.UploadURL = url

	return &Client{
		client: ghClient,
		owner:  owner,
		repo:   repo,
		ctx:    context.Background(),
	}
}

