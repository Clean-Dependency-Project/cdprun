// Package storage provides database operations for release tracking.
package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"
)

// Sentinel errors for release operations.
var (
	ErrNilRelease      = errors.New("release cannot be nil")
	ErrReleaseNotFound = errors.New("release not found")
	ErrNilPlatformArtifact = errors.New("platform artifact cannot be nil")
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

// AppendOrMergePlatformArtifact appends a platform artifact to an existing release
// or replaces the existing matching entry (idempotent update behavior).
//
// Identity key is: normalized platform + binary filename.
// This allows reruns to refresh URL/SHA metadata without creating duplicates.
func (d *DB) AppendOrMergePlatformArtifact(runtime, version string, artifact PlatformArtifact) error {
	if runtime == "" {
		return fmt.Errorf("runtime cannot be empty")
	}
	if version == "" {
		return fmt.Errorf("version cannot be empty")
	}
	if artifact.Binary == nil {
		return ErrNilPlatformArtifact
	}
	if strings.TrimSpace(artifact.Binary.Filename) == "" {
		return fmt.Errorf("platform artifact binary filename is required")
	}

	release, err := d.GetRelease(runtime, version)
	if err != nil {
		return err
	}

	artifacts, err := parseReleaseArtifacts(release.Artifacts)
	if err != nil {
		return fmt.Errorf("parse release artifacts: %w", err)
	}

	artifacts.Platforms = mergePlatformArtifacts(artifacts.Platforms, artifact)
	artifacts.Metadata.PlatformCount = len(artifacts.Platforms)
	artifacts.Metadata.TotalArtifacts = countReleaseArtifacts(artifacts)

	encoded, err := json.Marshal(artifacts)
	if err != nil {
		return fmt.Errorf("marshal release artifacts: %w", err)
	}

	if err := d.db.Model(&Release{}).Where("id = ?", release.ID).Update("artifacts", string(encoded)).Error; err != nil {
		return fmt.Errorf("update release artifacts: %w", err)
	}

	return nil
}

func parseReleaseArtifacts(raw string) (ReleaseArtifacts, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ReleaseArtifacts{}, nil
	}
	var artifacts ReleaseArtifacts
	if err := json.Unmarshal([]byte(trimmed), &artifacts); err != nil {
		return ReleaseArtifacts{}, err
	}
	return artifacts, nil
}

func mergePlatformArtifacts(existing []PlatformArtifact, incoming PlatformArtifact) []PlatformArtifact {
	out := make([]PlatformArtifact, len(existing))
	copy(out, existing)

	incomingKey := platformArtifactIdentityKey(incoming)
	for i := range out {
		if platformArtifactIdentityKey(out[i]) == incomingKey {
			out[i] = incoming
			return out
		}
	}

	out = append(out, incoming)
	return out
}

func platformArtifactIdentityKey(a PlatformArtifact) string {
	platformKey := strings.ToLower(strings.TrimSpace(a.Platform))
	filename := ""
	if a.Binary != nil {
		filename = strings.ToLower(strings.TrimSpace(a.Binary.Filename))
	}
	return platformKey + "|" + filename
}

func countReleaseArtifacts(a ReleaseArtifacts) int {
	total := 0
	for _, p := range a.Platforms {
		if p.Binary != nil {
			total++
		}
		if p.Audit != nil {
			total++
		}
		if p.Signature != nil {
			total++
		}
		if p.Certificate != nil {
			total++
		}
	}
	total += len(a.CommonFiles)
	return total
}

