// Package github provides testing helpers for GitHub client.
package github

import (
	"context"
	"net/http"
	"net/url"

	"github.com/google/go-github/v57/github"
)

// NewTestClient creates a GitHub client for testing with a custom HTTP client and base URL.
// This allows tests to use httptest.Server for mocking GitHub API responses.
// The baseURL should be the URL of your httptest.NewServer().
func NewTestClient(httpClient *http.Client, baseURL, repository string) (*Client, error) {
	if repository == "" {
		return nil, ErrInvalidRepo
	}

	owner, repo, err := parseRepository(repository)
	if err != nil {
		return nil, err
	}

	// Create GitHub client with custom HTTP client
	ghClient := github.NewClient(httpClient)

	// Parse and set the base URL (following official go-github pattern)
	parsedURL, err := url.Parse(baseURL + "/")
	if err != nil {
		return nil, err
	}
	
	ghClient.BaseURL = parsedURL
	ghClient.UploadURL = parsedURL

	return &Client{
		client: ghClient,
		owner:  owner,
		repo:   repo,
		ctx:    context.Background(),
	}, nil
}

