package sitegen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"

	"log/slog"
)

// SimpleArtifactEntry represents a single artifact in the JSON index.
type SimpleArtifactEntry struct {
	Platform string `json:"platform"`
	Binary   string `json:"binary"`
	SHA256   string `json:"sha256,omitempty"`
	Audit    string `json:"audit,omitempty"`
	Version  string `json:"version"`
}

// ScriptsArtifactEntry represents a non-platform-specific scripts archive included in every runtime index.json.
type ScriptsArtifactEntry struct {
	ReleaseTag string `json:"release_tag"`
	Binary     string `json:"binary"`
	SHA256     string `json:"sha256,omitempty"`
}

// SimpleVersionIndex represents a version-specific JSON index nested by OS.
type SimpleVersionIndex map[string][]SimpleArtifactEntry

// SimpleRootIndex represents the root JSON index nested by OS.
type SimpleRootIndex map[string][]SimpleArtifactEntry

// RenderSimpleIndex generates hierarchical Simple index pages.
func RenderSimpleIndex(model *SiteModel, outDir string, logger *slog.Logger) error {
	simpleDir := filepath.Join(outDir, "simple")

	// Render /simple/index.html (list of runtimes)
	if err := renderSimpleRootIndex(model, simpleDir, logger); err != nil {
		return fmt.Errorf("failed to render simple root index: %w", err)
	}

	// Collect scripts index once (searches all runtimes).
	scriptsIndex := collectScriptsIndex(model)

	// Render pages for each runtime
	for _, runtime := range model.Runtimes {
		if err := renderSimpleRuntimePages(runtime, scriptsIndex, simpleDir, logger); err != nil {
			return fmt.Errorf("failed to render pages for %s: %w", runtime.Name, err)
		}
	}

	return nil
}

// renderSimpleRuntimePages renders /simple/<runtime>/index.html and version pages.
// scriptsIndex is pre-collected across all runtimes so every runtime index includes it.
func renderSimpleRuntimePages(runtime RuntimeModel, scriptsIndex []ScriptsArtifactEntry, simpleDir string, logger *slog.Logger) error {
	runtimeDir := filepath.Join(simpleDir, runtime.Name)

	// Collect unique major versions
	majorVersions := collectMajorVersions(runtime)

	// Render /simple/<runtime>/index.html (list of major versions)
	if err := renderRuntimeIndex(runtime.Name, majorVersions, runtimeDir, logger); err != nil {
		return err
	}

	// Render runtime-level JSON index containing all versions for this runtime.
	// Every runtime gets a "scripts" key so consumers can discover scripts.zip
	// regardless of which runtime index they query.
	runtimeIndex := collectRuntimeArtifactIndex(runtime)
	out := make(map[string]any, len(runtimeIndex)+1)
	for k, v := range runtimeIndex {
		out[k] = v
	}
	out["scripts"] = scriptsIndex

	jsonData, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize runtime artifact index: %w", err)
	}

	jsonPath := filepath.Join(runtimeDir, "index.json")
	if err := writeFileIfChanged(jsonPath, jsonData, logger); err != nil {
		return fmt.Errorf("failed to write runtime JSON index: %w", err)
	}

	// Render version pages for each major version
	for _, major := range majorVersions {
		if err := renderVersionPage(runtime, major, runtimeDir, logger); err != nil {
			return err
		}
	}

	logger.Debug("rendered runtime pages", "runtime", runtime.Name, "major_versions", len(majorVersions), "os_groups", len(runtimeIndex))
	return nil
}

// collectScriptsIndex searches all runtimes for scripts common files.
// scripts.zip is attached to whichever release runs first (could be any runtime),
// so we must scan globally to find them all.
func collectScriptsIndex(model *SiteModel) []ScriptsArtifactEntry {
	seen := make(map[string]bool)
	var entries []ScriptsArtifactEntry

	for _, rt := range model.Runtimes {
		for _, platform := range rt.Platforms {
			for _, version := range platform.Versions {
				for _, release := range version.Releases {
					if release.ReleaseTag == "" {
						continue
					}
					for _, cf := range release.CommonFiles {
						if cf.Type != "scripts" || cf.Filename == "" {
							continue
						}
						binary := fmt.Sprintf("%s/%s", release.ReleaseTag, cf.Filename)
						if seen[binary] {
							continue
						}
						seen[binary] = true

						entries = append(entries, ScriptsArtifactEntry{
							ReleaseTag: release.ReleaseTag,
							Binary:     binary,
							SHA256:     cf.SHA256,
						})
					}
				}
			}
		}
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Binary < entries[j].Binary
	})

	return entries
}

// collectMajorVersions collects unique major versions from a runtime.
func collectMajorVersions(runtime RuntimeModel) []int {
	majorMap := make(map[int]bool)

	for _, platform := range runtime.Platforms {
		for _, version := range platform.Versions {
			majorMap[version.Major] = true
		}
	}

	majors := make([]int, 0, len(majorMap))
	for major := range majorMap {
		majors = append(majors, major)
	}

	sort.Ints(majors)
	return majors
}

// renderRuntimeIndex renders /simple/<runtime>/index.html listing major versions.
func renderRuntimeIndex(runtimeName string, majors []int, runtimeDir string, logger *slog.Logger) error {
	var buf bytes.Buffer
	buf.WriteString("<!DOCTYPE html>\n<html>\n<head><title>")
	buf.WriteString(runtimeName)
	buf.WriteString(" versions</title></head>\n<body>\n<h1>")
	buf.WriteString(runtimeName)
	buf.WriteString("</h1>\n\n")

	for _, major := range majors {
		buf.WriteString(fmt.Sprintf("<a href=\"v%d/\">v%d</a><br/>\n", major, major))
	}

	buf.WriteString("\n</body>\n</html>\n")

	path := filepath.Join(runtimeDir, "index.html")
	if err := writeFileIfChanged(path, buf.Bytes(), logger); err != nil {
		return fmt.Errorf("failed to write runtime index: %w", err)
	}

	logger.Debug("rendered runtime index", "runtime", runtimeName, "versions", len(majors))
	return nil
}

// renderVersionPage renders /simple/<runtime>/v<major>/index.html with all binaries.
func renderVersionPage(runtime RuntimeModel, major int, runtimeDir string, logger *slog.Logger) error {
	// Collect all distributions for this major version
	distMap := make(map[string]DistributionModel)

	for _, platform := range runtime.Platforms {
		for _, version := range platform.Versions {
			if version.Major == major {
				collectDistributionsFromVersion(version, distMap)
			}
		}
	}

	// Convert to sorted slice
	distributions := make([]DistributionModel, 0, len(distMap))
	for _, dist := range distMap {
		distributions = append(distributions, dist)
	}

	sort.Slice(distributions, func(i, j int) bool {
		return distributions[i].Filename < distributions[j].Filename
	})

	// Render HTML
	var buf bytes.Buffer
	buf.WriteString("<!DOCTYPE html>\n<html>\n<head><title>")
	buf.WriteString(fmt.Sprintf("%s v%d", runtime.Name, major))
	buf.WriteString("</title></head>\n<body>\n<h1>")
	buf.WriteString(fmt.Sprintf("%s v%d binaries", runtime.Name, major))
	buf.WriteString("</h1>\n\n")

	for _, dist := range distributions {
		buf.WriteString("<a href=\"")
		buf.WriteString(dist.URL)
		buf.WriteString("\"")
		if dist.SHA256 != "" {
			buf.WriteString("#sha256=")
			buf.WriteString(dist.SHA256)
		}
		buf.WriteString(">")
		buf.WriteString(dist.Filename)
		buf.WriteString("</a><br/>\n")
	}

	buf.WriteString("\n</body>\n</html>\n")

	versionDir := filepath.Join(runtimeDir, fmt.Sprintf("v%d", major))
	path := filepath.Join(versionDir, "index.html")
	if err := writeFileIfChanged(path, buf.Bytes(), logger); err != nil {
		return fmt.Errorf("failed to write version page: %w", err)
	}

	// Also render JSON index for automation tooling (e.g., Nexus proxy discovery)
	artifactIndex := collectArtifactIndexByMajor(runtime, major)
	jsonData, err := json.MarshalIndent(artifactIndex, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize artifact index: %w", err)
	}

	jsonPath := filepath.Join(versionDir, "index.json")
	if err := writeFileIfChanged(jsonPath, jsonData, logger); err != nil {
		return fmt.Errorf("failed to write version JSON index: %w", err)
	}

	logger.Debug("rendered version page", "runtime", runtime.Name, "major", major, "distributions", len(distributions), "artifact_entries", len(artifactIndex))
	return nil
}

// collectArtifactIndexByMajor returns nested artifact index for the given runtime/major.
func collectArtifactIndexByMajor(runtime RuntimeModel, major int) SimpleVersionIndex {
	index := make(SimpleVersionIndex)

	for _, platform := range runtime.Platforms {
		os := normalizeOS(platform.OS)
		for _, version := range platform.Versions {
			if version.Major != major {
				continue
			}

			for _, release := range version.Releases {
				for _, artifact := range release.Artifacts {
					if artifact.Binary == nil || release.ReleaseTag == "" {
						continue
					}

					entry := SimpleArtifactEntry{
						Platform: artifact.Platform,
						Binary:   fmt.Sprintf("%s/%s", release.ReleaseTag, artifact.Binary.Filename),
						SHA256:   artifact.Binary.SHA256,
						Version:  version.Version,
					}
					if artifact.Audit != nil {
						entry.Audit = fmt.Sprintf("%s/%s", release.ReleaseTag, artifact.Audit.Filename)
					}

					index[os] = append(index[os], entry)
				}
			}
		}
	}

	// Sort entries within each OS for determinism
	for os := range index {
		sort.Slice(index[os], func(i, j int) bool {
			return index[os][i].Binary < index[os][j].Binary
		})
	}

	return index
}

// collectRuntimeArtifactIndex returns nested artifact index for a single runtime across all versions.
func collectRuntimeArtifactIndex(runtime RuntimeModel) SimpleRootIndex {
	index := make(SimpleRootIndex)
	seen := make(map[string]bool)

	for _, platform := range runtime.Platforms {
		os := normalizeOS(platform.OS)
		for _, version := range platform.Versions {
			for _, release := range version.Releases {
				if release.ReleaseTag == "" {
					continue
				}
				for _, artifact := range release.Artifacts {
					if artifact.Binary == nil {
						continue
					}

					binaryPath := fmt.Sprintf("%s/%s", release.ReleaseTag, artifact.Binary.Filename)
					if seen[binaryPath] {
						continue
					}
					seen[binaryPath] = true

					entry := SimpleArtifactEntry{
						Platform: artifact.Platform,
						Binary:   binaryPath,
						SHA256:   artifact.Binary.SHA256,
						Version:  version.Version,
					}
					if artifact.Audit != nil {
						entry.Audit = fmt.Sprintf("%s/%s", release.ReleaseTag, artifact.Audit.Filename)
					}

					index[os] = append(index[os], entry)
				}
			}
		}
	}

	// Sort entries within each OS for determinism
	for os := range index {
		sort.Slice(index[os], func(i, j int) bool {
			return index[os][i].Binary < index[os][j].Binary
		})
	}

	return index
}

// collectDistributionsFromVersion collects all distributions from a version model.
func collectDistributionsFromVersion(version VersionModel, distMap map[string]DistributionModel) {
	for _, release := range version.Releases {
		for _, artifact := range release.Artifacts {
			// Add binary
			if artifact.Binary != nil {
				distKey := artifact.Binary.Filename + "|" + artifact.Binary.URL
				if _, exists := distMap[distKey]; !exists {
					distMap[distKey] = DistributionModel{
						Filename: artifact.Binary.Filename,
						URL:      artifact.Binary.URL,
						SHA256:   artifact.Binary.SHA256,
					}
				}
			}

			// Add audit.json
			if artifact.Audit != nil {
				distKey := artifact.Audit.Filename + "|" + artifact.Audit.URL
				if _, exists := distMap[distKey]; !exists {
					distMap[distKey] = DistributionModel{
						Filename: artifact.Audit.Filename,
						URL:      artifact.Audit.URL,
						SHA256:   "", // audit.json files don't have SHA256
					}
				}
			}

			// Add signature files
			if artifact.Signature != nil {
				distKey := artifact.Signature.Filename + "|" + artifact.Signature.URL
				if _, exists := distMap[distKey]; !exists {
					distMap[distKey] = DistributionModel{
						Filename: artifact.Signature.Filename,
						URL:      artifact.Signature.URL,
						SHA256:   artifact.Signature.SHA256,
					}
				}
			}

			// Add certificate files
			if artifact.Certificate != nil {
				distKey := artifact.Certificate.Filename + "|" + artifact.Certificate.URL
				if _, exists := distMap[distKey]; !exists {
					distMap[distKey] = DistributionModel{
						Filename: artifact.Certificate.Filename,
						URL:      artifact.Certificate.URL,
						SHA256:   artifact.Certificate.SHA256,
					}
				}
			}
		}
	}
}

// renderSimpleRootIndex renders /simple/index.html listing all runtimes.
func renderSimpleRootIndex(model *SiteModel, simpleDir string, logger *slog.Logger) error {
	// Extract runtime names
	runtimeNames := make([]string, 0, len(model.Runtimes))
	for _, runtime := range model.Runtimes {
		runtimeNames = append(runtimeNames, runtime.Name)
	}
	sort.Strings(runtimeNames)

	// Render HTML
	var buf bytes.Buffer
	buf.WriteString("<!DOCTYPE html>\n<html>\n<head><title>Simple Index</title></head>\n<body>\n<h1>Available Runtimes</h1>\n\n")

	for _, name := range runtimeNames {
		buf.WriteString(fmt.Sprintf("<a href=\"%s/\">%s</a><br/>\n", name, name))
	}

	buf.WriteString("\n</body>\n</html>\n")

	path := filepath.Join(simpleDir, "index.html")
	if err := writeFileIfChanged(path, buf.Bytes(), logger); err != nil {
		return fmt.Errorf("failed to write simple root index: %w", err)
	}

	// Also render consolidated JSON index with all artifact paths across all runtimes
	allIndex := collectAllArtifactIndex(model)
	jsonData, err := json.MarshalIndent(allIndex, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize consolidated artifact index: %w", err)
	}

	jsonPath := filepath.Join(simpleDir, "index.json")
	if err := writeFileIfChanged(jsonPath, jsonData, logger); err != nil {
		return fmt.Errorf("failed to write consolidated JSON index: %w", err)
	}

	logger.Info("rendered simple root index", "path", path, "runtimes", len(runtimeNames), "total_os_groups", len(allIndex))
	return nil
}

// collectAllArtifactIndex returns nested artifact index across all runtimes in the model.
func collectAllArtifactIndex(model *SiteModel) SimpleRootIndex {
	index := make(SimpleRootIndex)
	seen := make(map[string]bool)

	for _, runtime := range model.Runtimes {
		for _, platform := range runtime.Platforms {
			os := normalizeOS(platform.OS)
			for _, version := range platform.Versions {
				for _, release := range version.Releases {
					if release.ReleaseTag == "" {
						continue
					}
					for _, artifact := range release.Artifacts {
						if artifact.Binary == nil {
							continue
						}

						binaryPath := fmt.Sprintf("%s/%s", release.ReleaseTag, artifact.Binary.Filename)
						if seen[binaryPath] {
							continue
						}
						seen[binaryPath] = true

						entry := SimpleArtifactEntry{
							Platform: artifact.Platform,
							Binary:   binaryPath,
							SHA256:   artifact.Binary.SHA256,
							Version:  version.Version,
						}
						if artifact.Audit != nil {
							entry.Audit = fmt.Sprintf("%s/%s", release.ReleaseTag, artifact.Audit.Filename)
						}

						index[os] = append(index[os], entry)
					}
				}
			}
		}
	}

	// Sort entries within each OS for determinism
	for os := range index {
		sort.Slice(index[os], func(i, j int) bool {
			return index[os][i].Binary < index[os][j].Binary
		})
	}

	return index
}
