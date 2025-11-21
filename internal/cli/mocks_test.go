package cli

import (
	"fmt"

	"github.com/clean-dependency-project/cdprun/internal/storage"
	"github.com/google/go-github/v57/github"
)

// mockGitHubReleaser implements GitHubReleaser for testing.
type mockGitHubReleaser struct {
	createReleaseFn       func(tag, name, body string, draft bool) (*github.RepositoryRelease, error)
	uploadAssetFn         func(releaseID int64, filePath string) (*github.ReleaseAsset, error)
	getAssetDownloadURLFn func(asset *github.ReleaseAsset) string
	getReleaseURLFn       func(release *github.RepositoryRelease) string
}

// CreateRelease implements GitHubReleaser.
func (m *mockGitHubReleaser) CreateRelease(tag, name, body string, draft bool) (*github.RepositoryRelease, error) {
	if m.createReleaseFn != nil {
		return m.createReleaseFn(tag, name, body, draft)
	}
	id := int64(123)
	htmlURL := fmt.Sprintf("https://github.com/owner/repo/releases/tag/%s", tag)
	return &github.RepositoryRelease{
		ID:      &id,
		TagName: &tag,
		Name:    &name,
		Body:    &body,
		HTMLURL: &htmlURL,
	}, nil
}

// UploadAsset implements GitHubReleaser.
func (m *mockGitHubReleaser) UploadAsset(releaseID int64, filePath string) (*github.ReleaseAsset, error) {
	if m.uploadAssetFn != nil {
		return m.uploadAssetFn(releaseID, filePath)
	}
	assetID := int64(456)
	name := filePath // Use full path as name for testing
	downloadURL := fmt.Sprintf("https://github.com/owner/repo/releases/download/%s", name)
	return &github.ReleaseAsset{
		ID:                 &assetID,
		Name:               &name,
		BrowserDownloadURL: &downloadURL,
	}, nil
}

// GetAssetDownloadURL implements GitHubReleaser.
func (m *mockGitHubReleaser) GetAssetDownloadURL(asset *github.ReleaseAsset) string {
	if m.getAssetDownloadURLFn != nil {
		return m.getAssetDownloadURLFn(asset)
	}
	if asset.BrowserDownloadURL != nil {
		return *asset.BrowserDownloadURL
	}
	return ""
}

// GetReleaseURL implements GitHubReleaser.
func (m *mockGitHubReleaser) GetReleaseURL(release *github.RepositoryRelease) string {
	if m.getReleaseURLFn != nil {
		return m.getReleaseURLFn(release)
	}
	if release.HTMLURL != nil {
		return *release.HTMLURL
	}
	return ""
}

// mockDatabaseStore implements DatabaseStore for testing.
type mockDatabaseStore struct {
	createReleaseFn func(release *storage.Release) error
	getReleaseFn    func(runtime, version string) (*storage.Release, error)
	closeFn         func() error
}

// CreateRelease implements DatabaseStore.
func (m *mockDatabaseStore) CreateRelease(release *storage.Release) error {
	if m.createReleaseFn != nil {
		return m.createReleaseFn(release)
	}
	return nil
}

// GetRelease implements DatabaseStore.
func (m *mockDatabaseStore) GetRelease(runtime, version string) (*storage.Release, error) {
	if m.getReleaseFn != nil {
		return m.getReleaseFn(runtime, version)
	}
	return nil, nil
}

// Close implements DatabaseStore.
func (m *mockDatabaseStore) Close() error {
	if m.closeFn != nil {
		return m.closeFn()
	}
	return nil
}
