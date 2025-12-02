package sitegen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"

	"log/slog"
)

// RenderSimpleIndex generates hierarchical Simple index pages.
// Creates: /simple/index.html (runtimes)
//
//	/simple/<runtime>/index.html (versions)
//	/simple/<runtime>/v<major>/index.html (binaries)
func RenderSimpleIndex(model *SiteModel, outDir string, logger *slog.Logger) error {
	simpleDir := filepath.Join(outDir, "simple")

	// Render /simple/index.html (list of runtimes)
	if err := renderSimpleRootIndex(model, simpleDir, logger); err != nil {
		return fmt.Errorf("failed to render simple root index: %w", err)
	}

	// Render pages for each runtime
	for _, runtime := range model.Runtimes {
		if err := renderSimpleRuntimePages(runtime, simpleDir, logger); err != nil {
			return fmt.Errorf("failed to render pages for %s: %w", runtime.Name, err)
		}
	}

	return nil
}

// renderSimpleRuntimePages renders /simple/<runtime>/index.html and version pages.
func renderSimpleRuntimePages(runtime RuntimeModel, simpleDir string, logger *slog.Logger) error {
	runtimeDir := filepath.Join(simpleDir, runtime.Name)

	// Collect unique major versions
	majorVersions := collectMajorVersions(runtime)

	// Render /simple/<runtime>/index.html (list of major versions)
	if err := renderRuntimeIndex(runtime.Name, majorVersions, runtimeDir, logger); err != nil {
		return err
	}

	// Render version pages for each major version
	for _, major := range majorVersions {
		if err := renderVersionPage(runtime, major, runtimeDir, logger); err != nil {
			return err
		}
	}

	return nil
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
	artifactPaths := collectArtifactPathsByMajor(runtime, major)
	jsonData, err := json.MarshalIndent(artifactPaths, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize artifact index: %w", err)
	}

	jsonPath := filepath.Join(versionDir, "index.json")
	if err := writeFileIfChanged(jsonPath, jsonData, logger); err != nil {
		return fmt.Errorf("failed to write version JSON index: %w", err)
	}

	logger.Debug("rendered version page", "runtime", runtime.Name, "major", major, "distributions", len(distributions), "artifact_paths", len(artifactPaths))
	return nil
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

// collectArtifactPathsByMajor returns sorted tag-relative artifact paths for the given runtime/major.
func collectArtifactPathsByMajor(runtime RuntimeModel, major int) []string {
	paths := make(map[string]struct{})

	for _, platform := range runtime.Platforms {
		for _, version := range platform.Versions {
			if version.Major != major {
				continue
			}

			for _, release := range version.Releases {
				for _, artifact := range release.Artifacts {
					if artifact.Binary == nil || release.ReleaseTag == "" {
						continue
					}
					path := fmt.Sprintf("%s/%s", release.ReleaseTag, artifact.Binary.Filename)
					paths[path] = struct{}{}
				}
			}
		}
	}

	result := make([]string, 0, len(paths))
	for path := range paths {
		result = append(result, path)
	}
	sort.Strings(result)
	return result
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

	logger.Info("rendered simple root index", "path", path, "runtimes", len(runtimeNames))
	return nil
}
