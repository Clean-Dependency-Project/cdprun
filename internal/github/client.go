// Package github provides a client for interacting with GitHub Releases API.
package github

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/google/go-github/v57/github"
)

// Sentinel errors for GitHub operations.
var (
	ErrEmptyToken      = errors.New("github token cannot be empty")
	ErrInvalidRepo     = errors.New("repository must be in format 'owner/repo'")
	ErrNilRelease      = errors.New("github release cannot be nil")
	ErrReleaseNotFound = errors.New("release not found")
)

// Client wraps the GitHub API client for release operations.
type Client struct {
	client *github.Client
	owner  string
	repo   string
	ctx    context.Context
}

// NewClient creates a new GitHub API client for the specified repository.
// Token should be a personal access token or GitHub Actions token with repo permissions.
// Repository must be in the format "owner/repo".
func NewClient(token, repository string) (*Client, error) {
	if token == "" {
		return nil, ErrEmptyToken
	}

	owner, repo, err := parseRepository(repository)
	if err != nil {
		return nil, err
	}

	// Create authenticated client
	client := github.NewClient(nil).WithAuthToken(token)

	return &Client{
		client: client,
		owner:  owner,
		repo:   repo,
		ctx:    context.Background(),
	}, nil
}

// CreateRelease creates a new GitHub release.
// Returns the created release metadata including the HTML URL and upload URL.
func (c *Client) CreateRelease(tag, name, body string, draft bool) (*github.RepositoryRelease, error) {
	if tag == "" {
		return nil, fmt.Errorf("release tag cannot be empty")
	}
	if name == "" {
		return nil, fmt.Errorf("release name cannot be empty")
	}

	if c.client == nil || c.owner == "" || c.repo == "" {
		return nil, fmt.Errorf("client not initialized: use NewClient to create instances")
	}

	release := &github.RepositoryRelease{
		TagName: github.String(tag),
		Name:    github.String(name),
		Body:    github.String(body),
		Draft:   github.Bool(draft),
	}

	created, _, err := c.client.Repositories.CreateRelease(c.ctx, c.owner, c.repo, release)
	if err != nil {
		return nil, fmt.Errorf("failed to create release %s: %w", tag, err)
	}

	return created, nil
}

// GetRelease retrieves an existing release by tag name.
// Returns ErrReleaseNotFound if the release doesn't exist.
func (c *Client) GetRelease(tag string) (*github.RepositoryRelease, error) {
	if tag == "" {
		return nil, fmt.Errorf("release tag cannot be empty")
	}

	if c.client == nil || c.owner == "" || c.repo == "" {
		return nil, fmt.Errorf("client not initialized: use NewClient to create instances")
	}

	release, resp, err := c.client.Repositories.GetReleaseByTag(c.ctx, c.owner, c.repo, tag)
	if err != nil {
		if resp != nil && resp.StatusCode == 404 {
			return nil, ErrReleaseNotFound
		}
		return nil, fmt.Errorf("failed to get release %s: %w", tag, err)
	}

	return release, nil
}

// UploadAsset uploads a file as a release asset.
// The file will be read from the provided path and uploaded to the specified release.
// Returns the created asset with its download URL.
func (c *Client) UploadAsset(releaseID int64, filePath string) (*github.ReleaseAsset, error) {
	if releaseID == 0 {
		return nil, fmt.Errorf("release ID cannot be zero")
	}
	if filePath == "" {
		return nil, fmt.Errorf("file path cannot be empty")
	}

	if c.client == nil || c.owner == "" || c.repo == "" {
		return nil, fmt.Errorf("client not initialized: use NewClient to create instances")
	}

	// Open file
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file %s: %w", filePath, err)
	}
	defer func() { _ = file.Close() }()

	// Get file info for size
	fileInfo, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to stat file %s: %w", filePath, err)
	}

	// Extract filename from path
	filename := fileInfo.Name()

	// Create upload options
	opts := &github.UploadOptions{
		Name: filename,
	}

	// Upload asset
	asset, _, err := c.client.Repositories.UploadReleaseAsset(c.ctx, c.owner, c.repo, releaseID, opts, file)
	if err != nil {
		return nil, fmt.Errorf("failed to upload asset %s: %w", filename, err)
	}

	return asset, nil
}

// GetAssetDownloadURL returns the direct download URL for an asset.
// This URL can be used to download the asset publicly.
func (c *Client) GetAssetDownloadURL(asset *github.ReleaseAsset) string {
	if asset == nil || asset.BrowserDownloadURL == nil {
		return ""
	}
	return *asset.BrowserDownloadURL
}

// GetReleaseURL returns the HTML URL for a release.
func (c *Client) GetReleaseURL(release *github.RepositoryRelease) string {
	if release == nil || release.HTMLURL == nil {
		return ""
	}
	return *release.HTMLURL
}

// parseRepository splits a repository string into owner and repo.
// Returns an error if the format is invalid.
func parseRepository(repository string) (owner, repo string, err error) {
	if repository == "" {
		return "", "", ErrInvalidRepo
	}

	parts := strings.Split(repository, "/")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("%w: got %s", ErrInvalidRepo, repository)
	}

	owner = strings.TrimSpace(parts[0])
	repo = strings.TrimSpace(parts[1])

	if owner == "" || repo == "" {
		return "", "", fmt.Errorf("%w: owner or repo is empty", ErrInvalidRepo)
	}

	return owner, repo, nil
}

