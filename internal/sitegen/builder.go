package sitegen

import (
	"sort"
)

// BuildModel transforms releases into a SiteModel with deterministic sorting.
// Sorting order: runtime (asc), OS (linux < mac < windows), semver (desc), filename (asc).
func BuildModel(releases []ReleaseWithArtifacts) *SiteModel {
	if len(releases) == 0 {
		return &SiteModel{Runtimes: []RuntimeModel{}}
	}

	// Build nested map structure: runtime -> os -> major -> minor -> patch -> releases
	type osMapType map[string]map[int]map[int]map[int][]ReleaseWithArtifacts
	runtimeMap := make(map[string]osMapType)

	for _, rel := range releases {
		runtime := rel.Release.Runtime
		if runtimeMap[runtime] == nil {
			runtimeMap[runtime] = make(osMapType)
		}

		// Process each platform artifact
		for _, platform := range rel.Artifacts.Platforms {
			if platform.Binary == nil {
				continue // Skip platforms without binaries
			}

			os := normalizeOS(platform.PlatformOS)
			if runtimeMap[runtime][os] == nil {
				runtimeMap[runtime][os] = make(map[int]map[int]map[int][]ReleaseWithArtifacts)
			}

			major := rel.Release.SemverMajor
			if runtimeMap[runtime][os][major] == nil {
				runtimeMap[runtime][os][major] = make(map[int]map[int][]ReleaseWithArtifacts)
			}

			minor := rel.Release.SemverMinor
			if runtimeMap[runtime][os][major][minor] == nil {
				runtimeMap[runtime][os][major][minor] = make(map[int][]ReleaseWithArtifacts)
			}

			patch := rel.Release.SemverPatch
			runtimeMap[runtime][os][major][minor][patch] = append(
				runtimeMap[runtime][os][major][minor][patch],
				rel,
			)
		}
	}

	// Convert to SiteModel with deterministic sorting
	var runtimeModels []RuntimeModel
	for _, runtimeName := range sortedStringKeys(runtimeMap) {
		runtimeModels = append(runtimeModels, buildRuntimeModel(runtimeName, runtimeMap[runtimeName]))
	}

	return &SiteModel{Runtimes: runtimeModels}
}

// buildRuntimeModel builds a RuntimeModel from the OS map.
func buildRuntimeModel(runtimeName string, osMap map[string]map[int]map[int]map[int][]ReleaseWithArtifacts) RuntimeModel {
	var platformModels []PlatformModel
	for _, osName := range sortedOSKeys(osMap) {
		platformModels = append(platformModels, buildPlatformModel(osName, osMap[osName]))
	}
	
	return RuntimeModel{
		Name:      runtimeName,
		Platforms: platformModels,
	}
}

// buildPlatformModel builds a PlatformModel from the version map.
func buildPlatformModel(osName string, majorMap map[int]map[int]map[int][]ReleaseWithArtifacts) PlatformModel {
	var versionModels []VersionModel
	
	// Sort major versions descending (newest first)
	majorVersions := sortedIntKeys(majorMap)
	for i := len(majorVersions) - 1; i >= 0; i-- {
		major := majorVersions[i]
		versionModels = append(versionModels, buildVersionModels(major, majorMap[major], osName)...)
	}
	
	return PlatformModel{
		OS:       osName,
		Versions: versionModels,
	}
}

// buildVersionModels builds VersionModels for a major version.
func buildVersionModels(major int, minorMap map[int]map[int][]ReleaseWithArtifacts, osName string) []VersionModel {
	var versionModels []VersionModel
	
	// Sort minor versions descending (newest first)
	minorVersions := sortedIntKeys(minorMap)
	for i := len(minorVersions) - 1; i >= 0; i-- {
		minor := minorVersions[i]
		versionModels = append(versionModels, buildPatchVersionModels(major, minor, minorMap[minor], osName)...)
	}
	
	return versionModels
}

// buildPatchVersionModels builds VersionModels for a major.minor version.
func buildPatchVersionModels(major, minor int, patchMap map[int][]ReleaseWithArtifacts, osName string) []VersionModel {
	var versionModels []VersionModel
	
	// Sort patch versions descending (newest first)
	patchVersions := sortedIntKeys(patchMap)
	for i := len(patchVersions) - 1; i >= 0; i-- {
		patch := patchVersions[i]
		releases := patchMap[patch]
		
		// Sort releases by created_at descending
		sort.Slice(releases, func(i, j int) bool {
			return releases[i].Release.CreatedAt.After(releases[j].Release.CreatedAt)
		})
		
		var releaseModels []ReleaseModel
		for _, rel := range releases {
			// Filter artifacts to only include those matching this OS
			releaseModels = append(releaseModels, buildReleaseModelForOS(rel, osName))
		}
		
		versionModels = append(versionModels, VersionModel{
			Major:    major,
			Minor:    minor,
			Patch:    patch,
			Version:  releases[0].Release.Version,
			Releases: releaseModels,
		})
	}
	
	return versionModels
}

// buildReleaseModelForOS converts a ReleaseWithArtifacts into a ReleaseModel,
// filtering artifacts to only include those matching the specified OS.
func buildReleaseModelForOS(rel ReleaseWithArtifacts, osFilter string) ReleaseModel {
	var artifacts []ArtifactModel
	for _, platform := range rel.Artifacts.Platforms {
		if platform.Binary == nil {
			continue
		}

		// Filter by OS - only include artifacts for this OS
		normalizedOS := normalizeOS(platform.PlatformOS)
		if normalizedOS != osFilter {
			continue
		}

		artifact := ArtifactModel{
			Platform:     platform.Platform,
			PlatformOS:   platform.PlatformOS,
			PlatformArch: platform.PlatformArch,
		}

		if platform.Binary != nil {
			artifact.Binary = &FileModel{
				Filename: platform.Binary.Filename,
				Size:     platform.Binary.Size,
				SHA256:   platform.Binary.SHA256,
				URL:      platform.Binary.URL,
			}
		}

		if platform.Audit != nil {
			artifact.Audit = &FileModel{
				Filename: platform.Audit.Filename,
				Size:     platform.Audit.Size,
				URL:      platform.Audit.URL,
			}
		}

		if platform.Signature != nil {
			artifact.Signature = &FileModel{
				Filename: platform.Signature.Filename,
				Size:     platform.Signature.Size,
				SHA256:   platform.Signature.SHA256,
				URL:      platform.Signature.URL,
			}
		}

		if platform.Certificate != nil {
			artifact.Certificate = &FileModel{
				Filename: platform.Certificate.Filename,
				Size:     platform.Certificate.Size,
				SHA256:   platform.Certificate.SHA256,
				URL:      platform.Certificate.URL,
			}
		}

		artifacts = append(artifacts, artifact)
	}

	return ReleaseModel{
		ReleaseTag: rel.Release.ReleaseTag,
		ReleaseURL: rel.Release.ReleaseURL,
		CreatedAt:  rel.Release.CreatedAt,
		Artifacts:  artifacts,
	}
}

// normalizeOS normalizes OS names for consistent sorting.
// Maps: "darwin" -> "mac", others unchanged.
func normalizeOS(os string) string {
	if os == "darwin" {
		return "mac"
	}
	return os
}

// sortedStringKeys returns sorted keys from a map[string]T.
func sortedStringKeys[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// sortedOSKeys returns OS keys sorted in order: linux, mac, windows.
func sortedOSKeys[T any](m map[string]T) []string {
	order := []string{"linux", "mac", "windows"}
	var result []string
	for _, os := range order {
		if _, exists := m[os]; exists {
			result = append(result, os)
		}
	}
	// Add any other OS names not in the standard list
	for k := range m {
		found := false
		for _, o := range order {
			if k == o {
				found = true
				break
			}
		}
		if !found {
			result = append(result, k)
		}
	}
	sort.Strings(result[len(result)-(len(m)-len(result)):])
	return result
}

// sortedIntKeys returns sorted integer keys from a map[int]T.
func sortedIntKeys[T any](m map[int]T) []int {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	return keys
}

