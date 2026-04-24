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

// filterArtifactsForVersion filters artifacts to only include those whose
// filenames contain an exact match for the specified version.
//
// For aggregated security-only releases, upstream sometimes only ships a binary
// for the latest patch in a minor line (e.g., python.org publishes
// python-3.12.10-macos11.pkg as the macOS installer for the entire 3.12.x line).
// We deliberately do NOT attach such binaries to other advertised patch
// versions (e.g., 3.12.12), because doing so produced misleading entries like
// `mac/python/3.12/3.12.12/python-3.12.10-macos11.pkg` with version "3.12.12".
// If no binary in the release exactly matches the advertised version for a
// platform, that platform is simply omitted from this version's entries.
func filterArtifactsForVersion(artifacts storage.ReleaseArtifacts, version string) storage.ReleaseArtifacts {
	filtered := storage.ReleaseArtifacts{
		Platforms:   []storage.PlatformArtifact{},
		CommonFiles: artifacts.CommonFiles, // Keep common files for all versions
		Metadata:    artifacts.Metadata,
	}

	for _, platform := range artifacts.Platforms {
		hasMatchingArtifact := false

		if platform.Binary != nil && matchesVersion(platform.Binary.Filename, version) {
			hasMatchingArtifact = true
		}
		if platform.Audit != nil && matchesVersion(platform.Audit.Filename, version) {
			hasMatchingArtifact = true
		}
		if platform.MetadataFile != nil && matchesVersion(platform.MetadataFile.Filename, version) {
			hasMatchingArtifact = true
		}

		if !hasMatchingArtifact {
			continue
		}

		filteredPlatform := storage.PlatformArtifact{
			Platform:     platform.Platform,
			PlatformOS:   platform.PlatformOS,
			PlatformArch: platform.PlatformArch,
		}

		if platform.Binary != nil && matchesVersion(platform.Binary.Filename, version) {
			filteredPlatform.Binary = platform.Binary
		}
		if platform.Audit != nil && matchesVersion(platform.Audit.Filename, version) {
			filteredPlatform.Audit = platform.Audit
		}
		if platform.Signature != nil && matchesVersion(platform.Signature.Filename, version) {
			filteredPlatform.Signature = platform.Signature
		}
		if platform.Certificate != nil && matchesVersion(platform.Certificate.Filename, version) {
			filteredPlatform.Certificate = platform.Certificate
		}
		if platform.MetadataFile != nil && matchesVersion(platform.MetadataFile.Filename, version) {
			filteredPlatform.MetadataFile = platform.MetadataFile
		}

		filtered.Platforms = append(filtered.Platforms, filteredPlatform)
	}

	return filtered
}

// matchesVersion reports whether a filename advertises the given exact version.
//
// Matching is intentionally strict: only an exact substring match on the full
// version string counts. We previously fell back to a major.minor prefix
// match, but that caused mac/windows installers from one patch version to be
// re-published under unrelated patch versions in the same security-only
// aggregated release (see filterArtifactsForVersion for context).
//
// To avoid false positives such as filenames containing "3.12.123" matching
// version "3.12.12", the version must be bounded on both sides by either a
// non-digit character or the start/end of the filename.
func matchesVersion(filename, version string) bool {
	idx := 0
	for {
		i := strings.Index(filename[idx:], version)
		if i < 0 {
			return false
		}
		start := idx + i
		end := start + len(version)
		if !isVersionDigit(filename, start-1) && !isVersionDigit(filename, end) {
			return true
		}
		idx = start + 1
		if idx >= len(filename) {
			return false
		}
	}
}

// isVersionDigit reports whether the byte at position i in s is an ASCII digit.
// Out-of-bounds positions return false (treated as a non-digit boundary).
func isVersionDigit(s string, i int) bool {
	if i < 0 || i >= len(s) {
		return false
	}
	c := s[i]
	return c >= '0' && c <= '9'
}

// ReleaseWithArtifacts combines a Release with its parsed artifacts.
type ReleaseWithArtifacts struct {
	Release   storage.Release
	Artifacts storage.ReleaseArtifacts
}
