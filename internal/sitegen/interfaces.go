// Package sitegen generates static HTML pages from release database for GitHub Pages.
// Following Dave Cheney's principle: "Accept interfaces, return structs"
package sitegen

import "github.com/clean-dependency-project/cdprun/internal/storage"

// ReleaseReader abstracts database operations for reading releases.
// This interface enables testability by allowing mock implementations.
type ReleaseReader interface {
	// GetAllReleases retrieves all releases from the database, ordered by creation time descending.
	GetAllReleases() ([]storage.Release, error)
}

