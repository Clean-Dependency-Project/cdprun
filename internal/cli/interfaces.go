// Package cli provides command-line interface components with testable abstractions.
package cli

import (
	"context"

	"github.com/clean-dependency-project/cdprun/internal/endoflife"
	"github.com/clean-dependency-project/cdprun/internal/platform"
	"github.com/clean-dependency-project/cdprun/internal/runtime"
	"github.com/clean-dependency-project/cdprun/internal/storage"
	"github.com/google/go-github/v57/github"
)

// GitHubReleaser abstracts GitHub release operations for testing.
// Following Dave Cheney's principle: "Accept interfaces, return structs"
type GitHubReleaser interface {
	// CreateRelease creates a new GitHub release with the given parameters.
	CreateRelease(tag, name, body string, draft bool) (*github.RepositoryRelease, error)

	// UploadAsset uploads a file to an existing GitHub release.
	UploadAsset(releaseID int64, filePath string) (*github.ReleaseAsset, error)

	// GetAssetDownloadURL returns the public download URL for a release asset.
	GetAssetDownloadURL(asset *github.ReleaseAsset) string

	// GetReleaseURL returns the HTML URL for a GitHub release.
	GetReleaseURL(release *github.RepositoryRelease) string
}

// DatabaseStore abstracts database operations for testing.
type DatabaseStore interface {
	// CreateRelease inserts a new release record into the database.
	CreateRelease(release *storage.Release) error

	// GetRelease retrieves a release by runtime name and version.
	GetRelease(runtime, version string) (*storage.Release, error)

	// Close closes the database connection.
	Close() error
}

// RuntimeManager abstracts runtime download operations for testing.
// This interface mirrors the key methods from runtime.Manager that the CLI needs.
type RuntimeManager interface {
	// GetProvider returns the runtime provider for the given runtime name.
	GetProvider(name string) (runtime.RuntimeProvider, error)

	// DownloadRuntime downloads runtime binaries for the specified platforms.
	DownloadRuntime(
		ctx context.Context,
		runtimeName string,
		version endoflife.VersionInfo,
		platforms []platform.Platform,
		outputDir string,
		concurrency int,
	) ([]runtime.DownloadResult, error)
}
