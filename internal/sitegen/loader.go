package sitegen

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/clean-dependency-project/cdprun/internal/storage"
)

// LoadReleases loads all releases from the ReleaseReader and parses their artifact JSON.
// Returns releases with parsed artifacts, or an error if loading or parsing fails.
// For aggregated releases (version contains commas), splits into individual version entries.
func LoadReleases(reader ReleaseReader) ([]ReleaseWithArtifacts, error) {
	releases, err := reader.GetAllReleases()
	if err != nil {
		return nil, fmt.Errorf("failed to load releases: %w", err)
	}

	result := make([]ReleaseWithArtifacts, 0, len(releases))
	for _, release := range releases {
		var artifacts storage.ReleaseArtifacts
		if err := json.Unmarshal([]byte(release.Artifacts), &artifacts); err != nil {
			return nil, fmt.Errorf("failed to parse artifacts JSON for release %s: %w", release.ReleaseTag, err)
		}

		// Check if this is an aggregated release (version contains commas)
		if strings.Contains(release.Version, ",") {
			// Split aggregated release into individual versions
			versions := strings.Split(release.Version, ",")
			for _, version := range versions {
				version = strings.TrimSpace(version)
				// Create filtered artifacts for this specific version
				filteredArtifacts := filterArtifactsForVersion(artifacts, version)
				
				// Create a copy of the release with single version
				individualRelease := release
				individualRelease.Version = version
				
				// Parse semver for individual version
				major, minor, patch, err := storage.ParseSemver(version)
				if err != nil {
					// If semver parsing fails, log and continue with zeros
					// This can happen with non-standard version formats
					major, minor, patch = 0, 0, 0
				}
				individualRelease.SemverMajor = major
				individualRelease.SemverMinor = minor
				individualRelease.SemverPatch = patch
				
				result = append(result, ReleaseWithArtifacts{
					Release:   individualRelease,
					Artifacts: filteredArtifacts,
				})
			}
		} else {
			// Regular single-version release
			result = append(result, ReleaseWithArtifacts{
				Release:   release,
				Artifacts: artifacts,
			})
		}
	}

	return result, nil
}

// filterArtifactsForVersion filters artifacts to only include those matching the specified version.
func filterArtifactsForVersion(artifacts storage.ReleaseArtifacts, version string) storage.ReleaseArtifacts {
	filtered := storage.ReleaseArtifacts{
		Platforms:   []storage.PlatformArtifact{},
		CommonFiles: artifacts.CommonFiles, // Keep common files for all versions
		Metadata:    artifacts.Metadata,
	}
	
	// Filter platform artifacts by version in filename
	for _, platform := range artifacts.Platforms {
		// Check if any artifact in this platform matches the version
		hasMatchingArtifact := false
		
		if platform.Binary != nil && strings.Contains(platform.Binary.Filename, version) {
			hasMatchingArtifact = true
		}
		if platform.Audit != nil && strings.Contains(platform.Audit.Filename, version) {
			hasMatchingArtifact = true
		}
		
		if hasMatchingArtifact {
			// Create a filtered copy of the platform with only matching artifacts
			filteredPlatform := storage.PlatformArtifact{
				Platform:     platform.Platform,
				PlatformOS:   platform.PlatformOS,
				PlatformArch: platform.PlatformArch,
			}
			
			if platform.Binary != nil && strings.Contains(platform.Binary.Filename, version) {
				filteredPlatform.Binary = platform.Binary
			}
			if platform.Audit != nil && strings.Contains(platform.Audit.Filename, version) {
				filteredPlatform.Audit = platform.Audit
			}
			if platform.Signature != nil && strings.Contains(platform.Signature.Filename, version) {
				filteredPlatform.Signature = platform.Signature
			}
			if platform.Certificate != nil && strings.Contains(platform.Certificate.Filename, version) {
				filteredPlatform.Certificate = platform.Certificate
			}
			
			filtered.Platforms = append(filtered.Platforms, filteredPlatform)
		}
	}
	
	return filtered
}

// ReleaseWithArtifacts combines a Release with its parsed artifacts.
type ReleaseWithArtifacts struct {
	Release   storage.Release
	Artifacts storage.ReleaseArtifacts
}

