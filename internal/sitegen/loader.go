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
// For security-only releases where binaries use older patch versions (e.g., 3.12.10 for version 3.12.12),
// this function matches by major.minor prefix as a fallback.
func filterArtifactsForVersion(artifacts storage.ReleaseArtifacts, version string) storage.ReleaseArtifacts {
	filtered := storage.ReleaseArtifacts{
		Platforms:   []storage.PlatformArtifact{},
		CommonFiles: artifacts.CommonFiles, // Keep common files for all versions
		Metadata:    artifacts.Metadata,
	}

	// Extract major.minor from version for fallback matching
	// e.g., "3.12.12" -> "3.12"
	majorMinor := extractMajorMinor(version)

	// Filter platform artifacts by version in filename
	for _, platform := range artifacts.Platforms {
		// Check if any artifact in this platform matches the version
		hasMatchingArtifact := false

		if platform.Binary != nil && matchesVersion(platform.Binary.Filename, version, majorMinor) {
			hasMatchingArtifact = true
		}
		if platform.Audit != nil && matchesVersion(platform.Audit.Filename, version, majorMinor) {
			hasMatchingArtifact = true
		}

		if hasMatchingArtifact {
			// Create a filtered copy of the platform with only matching artifacts
			filteredPlatform := storage.PlatformArtifact{
				Platform:     platform.Platform,
				PlatformOS:   platform.PlatformOS,
				PlatformArch: platform.PlatformArch,
			}

			if platform.Binary != nil && matchesVersion(platform.Binary.Filename, version, majorMinor) {
				filteredPlatform.Binary = platform.Binary
			}
			if platform.Audit != nil && matchesVersion(platform.Audit.Filename, version, majorMinor) {
				filteredPlatform.Audit = platform.Audit
			}
			if platform.Signature != nil && matchesVersion(platform.Signature.Filename, version, majorMinor) {
				filteredPlatform.Signature = platform.Signature
			}
			if platform.Certificate != nil && matchesVersion(platform.Certificate.Filename, version, majorMinor) {
				filteredPlatform.Certificate = platform.Certificate
			}

			filtered.Platforms = append(filtered.Platforms, filteredPlatform)
		}
	}

	return filtered
}

// extractMajorMinor extracts the major.minor portion from a semver version.
// e.g., "3.12.12" -> "3.12", "22.15.0" -> "22.15"
func extractMajorMinor(version string) string {
	parts := strings.Split(version, ".")
	if len(parts) >= 2 {
		return parts[0] + "." + parts[1]
	}
	return version
}

// matchesVersion checks if a filename matches the given version.
// First tries exact version match, then falls back to major.minor match.
// This handles security-only releases where binaries use older patch versions.
func matchesVersion(filename, version, majorMinor string) bool {
	// First try exact version match
	if strings.Contains(filename, version) {
		return true
	}
	// Fallback to major.minor match for security-only releases
	// e.g., "python-3.12.10-amd64.exe" matches version "3.12.12" via "3.12"
	if strings.Contains(filename, majorMinor+".") {
		return true
	}
	return false
}

// ReleaseWithArtifacts combines a Release with its parsed artifacts.
type ReleaseWithArtifacts struct {
	Release   storage.Release
	Artifacts storage.ReleaseArtifacts
}

