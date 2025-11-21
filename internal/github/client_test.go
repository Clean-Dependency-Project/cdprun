package github

import (
	"errors"
	"strings"
	"testing"
)

func TestNewClient(t *testing.T) {
	tests := []struct {
		name       string
		token      string
		repository string
		wantErr    bool
		errType    error
	}{
		{
			name:       "valid client",
			token:      "ghp_test_token_123",
			repository: "owner/repo",
			wantErr:    false,
		},
		{
			name:       "empty token",
			token:      "",
			repository: "owner/repo",
			wantErr:    true,
			errType:    ErrEmptyToken,
		},
		{
			name:       "invalid repository format - no slash",
			token:      "ghp_test_token_123",
			repository: "ownerrepo",
			wantErr:    true,
			errType:    ErrInvalidRepo,
		},
		{
			name:       "invalid repository format - too many parts",
			token:      "ghp_test_token_123",
			repository: "owner/repo/extra",
			wantErr:    true,
			errType:    ErrInvalidRepo,
		},
		{
			name:       "invalid repository format - empty owner",
			token:      "ghp_test_token_123",
			repository: "/repo",
			wantErr:    true,
			errType:    ErrInvalidRepo,
		},
		{
			name:       "invalid repository format - empty repo",
			token:      "ghp_test_token_123",
			repository: "owner/",
			wantErr:    true,
			errType:    ErrInvalidRepo,
		},
		{
			name:       "empty repository",
			token:      "ghp_test_token_123",
			repository: "",
			wantErr:    true,
			errType:    ErrInvalidRepo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewClient(tt.token, tt.repository)

			if tt.wantErr {
				if err == nil {
					t.Errorf("NewClient() expected error, got nil")
					return
				}
				if tt.errType != nil && !errors.Is(err, tt.errType) {
					t.Errorf("NewClient() error = %v, want error type %v", err, tt.errType)
				}
				return
			}

			if err != nil {
				t.Errorf("NewClient() unexpected error: %v", err)
				return
			}

			if client == nil {
				t.Error("NewClient() returned nil client")
				return
			}

			// Verify owner and repo are set correctly
			expectedOwner, expectedRepo, _ := parseRepository(tt.repository)
			if client.owner != expectedOwner {
				t.Errorf("NewClient() owner = %q, want %q", client.owner, expectedOwner)
			}
			if client.repo != expectedRepo {
				t.Errorf("NewClient() repo = %q, want %q", client.repo, expectedRepo)
			}
		})
	}
}

func TestParseRepository(t *testing.T) {
	tests := []struct {
		name      string
		repo      string
		wantOwner string
		wantRepo  string
		wantErr   bool
	}{
		{
			name:      "valid repository",
			repo:      "owner/repo",
			wantOwner: "owner",
			wantRepo:  "repo",
			wantErr:   false,
		},
		{
			name:      "valid with whitespace",
			repo:      " owner / repo ",
			wantOwner: "owner",
			wantRepo:  "repo",
			wantErr:   false,
		},
		{
			name:    "invalid - no slash",
			repo:    "ownerrepo",
			wantErr: true,
		},
		{
			name:    "invalid - multiple slashes",
			repo:    "owner/repo/extra",
			wantErr: true,
		},
		{
			name:    "invalid - empty owner",
			repo:    "/repo",
			wantErr: true,
		},
		{
			name:    "invalid - empty repo",
			repo:    "owner/",
			wantErr: true,
		},
		{
			name:    "invalid - empty string",
			repo:    "",
			wantErr: true,
		},
		{
			name:    "invalid - only slash",
			repo:    "/",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, err := parseRepository(tt.repo)

			if tt.wantErr {
				if err == nil {
					t.Errorf("parseRepository() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("parseRepository() unexpected error: %v", err)
				return
			}

			if owner != tt.wantOwner {
				t.Errorf("parseRepository() owner = %q, want %q", owner, tt.wantOwner)
			}
			if repo != tt.wantRepo {
				t.Errorf("parseRepository() repo = %q, want %q", repo, tt.wantRepo)
			}
		})
	}
}

func TestClient_CreateRelease_Validation(t *testing.T) {
	// Create client for validation tests (won't actually call API)
	client, err := NewClient("test_token", "owner/repo")
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}

	tests := []struct {
		name        string
		tag         string
		releaseName string
		body        string
		draft       bool
		wantErr     bool
		errContains string
	}{
		{
			name:        "empty tag",
			tag:         "",
			releaseName: "Release Name",
			body:        "Release body",
			draft:       false,
			wantErr:     true,
			errContains: "tag cannot be empty",
		},
		{
			name:        "empty name",
			tag:         "v1.0.0",
			releaseName: "",
			body:        "Release body",
			draft:       false,
			wantErr:     true,
			errContains: "name cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.CreateRelease(tt.tag, tt.releaseName, tt.body, tt.draft)

			if tt.wantErr {
				if err == nil {
					t.Errorf("CreateRelease() expected error containing %q, got nil", tt.errContains)
					return
				}
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("CreateRelease() error = %v, want error containing %q", err, tt.errContains)
				}
				return
			}

			// Note: We can't test successful creation without mocking or hitting real API
		})
	}
}

func TestClient_GetRelease_Validation(t *testing.T) {
	client, err := NewClient("test_token", "owner/repo")
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}

	t.Run("empty tag", func(t *testing.T) {
		_, err := client.GetRelease("")
		if err == nil {
			t.Error("GetRelease() expected error for empty tag, got nil")
		}
		if !strings.Contains(err.Error(), "tag cannot be empty") {
			t.Errorf("GetRelease() error = %v, want error containing 'tag cannot be empty'", err)
		}
	})
}

func TestClient_UploadAsset_Validation(t *testing.T) {
	client, err := NewClient("test_token", "owner/repo")
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}

	tests := []struct {
		name        string
		releaseID   int64
		filePath    string
		wantErr     bool
		errContains string
	}{
		{
			name:        "zero release ID",
			releaseID:   0,
			filePath:    "/tmp/test.txt",
			wantErr:     true,
			errContains: "release ID cannot be zero",
		},
		{
			name:        "empty file path",
			releaseID:   12345,
			filePath:    "",
			wantErr:     true,
			errContains: "file path cannot be empty",
		},
		{
			name:        "non-existent file",
			releaseID:   12345,
			filePath:    "/nonexistent/file.txt",
			wantErr:     true,
			errContains: "failed to open file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.UploadAsset(tt.releaseID, tt.filePath)

			if tt.wantErr {
				if err == nil {
					t.Errorf("UploadAsset() expected error containing %q, got nil", tt.errContains)
					return
				}
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("UploadAsset() error = %v, want error containing %q", err, tt.errContains)
				}
				return
			}
		})
	}
}

func TestClient_GetAssetDownloadURL(t *testing.T) {
	client, err := NewClient("test_token", "owner/repo")
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}

	t.Run("nil asset", func(t *testing.T) {
		url := client.GetAssetDownloadURL(nil)
		if url != "" {
			t.Errorf("GetAssetDownloadURL(nil) = %q, want empty string", url)
		}
	})

	// Note: Testing with actual asset would require either mocking or integration test
}

func TestClient_GetReleaseURL(t *testing.T) {
	client, err := NewClient("test_token", "owner/repo")
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}

	t.Run("nil release", func(t *testing.T) {
		url := client.GetReleaseURL(nil)
		if url != "" {
			t.Errorf("GetReleaseURL(nil) = %q, want empty string", url)
		}
	})

	// Note: Testing with actual release would require either mocking or integration test
}

