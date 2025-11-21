// Package storage provides database operations for release tracking.
package storage

import (
	"encoding/json"
	"errors"
	"fmt"

	"gorm.io/gorm"
)

// Sentinel errors for release operations.
var (
	ErrNilRelease      = errors.New("release cannot be nil")
	ErrReleaseNotFound = errors.New("release not found")
)

// CreateRelease inserts a new release record into the database.
// Returns an error if the release already exists (duplicate release_tag).
func (d *DB) CreateRelease(release *Release) error {
	if release == nil {
		return ErrNilRelease
	}

	if err := d.db.Create(release).Error; err != nil {
		return fmt.Errorf("failed to create release: %w", err)
	}

	return nil
}

// GetRelease retrieves a release by runtime and version.
// Returns ErrReleaseNotFound if no matching release exists.
func (d *DB) GetRelease(runtime, version string) (*Release, error) {
	if runtime == "" {
		return nil, fmt.Errorf("runtime cannot be empty")
	}
	if version == "" {
		return nil, fmt.Errorf("version cannot be empty")
	}

	var release Release
	if err := d.db.Where("runtime = ? AND version = ?", runtime, version).First(&release).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrReleaseNotFound
		}
		return nil, fmt.Errorf("failed to get release: %w", err)
	}

	return &release, nil
}

// GetReleaseByTag retrieves a release by its unique release tag.
// Returns ErrReleaseNotFound if no matching release exists.
func (d *DB) GetReleaseByTag(releaseTag string) (*Release, error) {
	if releaseTag == "" {
		return nil, fmt.Errorf("release tag cannot be empty")
	}

	var release Release
	if err := d.db.Where("release_tag = ?", releaseTag).First(&release).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrReleaseNotFound
		}
		return nil, fmt.Errorf("failed to get release by tag: %w", err)
	}

	return &release, nil
}

// GetReleasesByRuntime retrieves all releases for a given runtime.
// Returns an empty slice if no releases exist for the runtime.
func (d *DB) GetReleasesByRuntime(runtime string) ([]Release, error) {
	if runtime == "" {
		return nil, fmt.Errorf("runtime cannot be empty")
	}

	var releases []Release
	if err := d.db.Where("runtime = ?", runtime).Order("created_at DESC").Find(&releases).Error; err != nil {
		return nil, fmt.Errorf("failed to get releases for runtime %s: %w", runtime, err)
	}

	return releases, nil
}

// GetAllReleases retrieves all releases from the database, ordered by creation time descending.
func (d *DB) GetAllReleases() ([]Release, error) {
	var releases []Release
	if err := d.db.Order("created_at DESC").Find(&releases).Error; err != nil {
		return nil, fmt.Errorf("failed to get all releases: %w", err)
	}

	return releases, nil
}

// ExportReleasesJSON exports all releases for a runtime as JSON bytes.
// This is useful for generating web pages or APIs.
func (d *DB) ExportReleasesJSON(runtime string) ([]byte, error) {
	releases, err := d.GetReleasesByRuntime(runtime)
	if err != nil {
		return nil, err
	}

	data, err := json.MarshalIndent(releases, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal releases to JSON: %w", err)
	}

	return data, nil
}

