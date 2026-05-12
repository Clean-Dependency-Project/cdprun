package sitegen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/clean-dependency-project/cdprun/internal/config"
	"github.com/clean-dependency-project/cdprun/internal/storage"
	"log/slog"
)

// SimpleArtifactEntry represents a single artifact in the JSON index.
type SimpleArtifactEntry struct {
	Platform string `json:"platform"`
	Type     string `json:"type"`
	Binary   string `json:"binary"`
	// SourcePath keeps the pre-reorg layout (<release_tag>/<filename>) so consumers
	// can still locate artifacts by GitHub release tag if needed.
	SourcePath string `json:"source_path"`
	// DownloadURL is the full GitHub release download URL for this artifact.
	DownloadURL string `json:"download_url"`
	SHA256      string `json:"sha256,omitempty"`
	Audit       string `json:"audit,omitempty"`
	Metadata    string `json:"metadata,omitempty"`
	Version     string `json:"version"`
}

// UnsupportedEntry is a single item in the "unsupported" JSON key.
// Kind distinguishes two roles:
//
//   - "line"     — the entire version line is EOL (e.g. version "3.9" covers all Python 3.9.x).
//     Clients should block every artifact whose version starts with this prefix.
//   - "artifact" — this specific version is present in the artifact store and is EOL.
//     Clients should remove or quarantine that specific artifact.
type UnsupportedEntry struct {
	Version   string `json:"version"`
	EOL       string `json:"eol,omitempty"`
	Notes     string `json:"notes,omitempty"`
	Supported bool   `json:"supported"` // always false
	Kind      string `json:"kind"`       // "line" | "artifact"
}

func artifactTypeFromFilename(filename string) string {
	lower := strings.ToLower(filename)
	// Multi-part extensions first.
	for _, ext := range []string{
		".tar.xz",
		".tar.gz",
		".tar.bz2",
		".tar.zst",
		".tgz",
	} {
		if strings.HasSuffix(lower, ext) {
			return strings.TrimPrefix(ext, ".")
		}
	}
	ext := path.Ext(lower)
	return strings.TrimPrefix(ext, ".")
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
func RenderSimpleIndex(model *SiteModel, outDir string, unsupported config.UnsupportedConfig, logger *slog.Logger) error {
	simpleDir := filepath.Join(outDir, "simple")

	// Render /simple/index.html (list of runtimes)
	if err := renderSimpleRootIndex(model, unsupported, simpleDir, logger); err != nil {
		return fmt.Errorf("failed to render simple root index: %w", err)
	}

	// Collect scripts index once (searches all runtimes).
	scriptsIndex := collectScriptsIndex(model)

	// Render pages for each runtime
	for _, runtime := range model.Runtimes {
		if err := renderSimpleRuntimePages(runtime, scriptsIndex, unsupported, simpleDir, logger); err != nil {
			return fmt.Errorf("failed to render pages for %s: %w", runtime.Name, err)
		}
	}

	return nil
}

// renderSimpleRuntimePages renders /simple/<runtime>/index.html and version pages.
// scriptsIndex is pre-collected across all runtimes so every runtime index includes it.
func renderSimpleRuntimePages(runtime RuntimeModel, scriptsIndex []ScriptsArtifactEntry, unsupported config.UnsupportedConfig, simpleDir string, logger *slog.Logger) error {
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
	out := make(map[string]any, len(runtimeIndex)+2)
	for k, v := range runtimeIndex {
		out[k] = v
	}
	out["scripts"] = scriptsIndex
	out["unsupported"] = expandUnsupportedVersions(runtime, unsupported)

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
		if err := renderVersionPage(runtime, major, unsupported, runtimeDir, logger); err != nil {
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
		fmt.Fprintf(&buf, "<a href=\"v%d/\">v%d</a><br/>\n", major, major)
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
func renderVersionPage(runtime RuntimeModel, major int, unsupported config.UnsupportedConfig, runtimeDir string, logger *slog.Logger) error {
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
	fmt.Fprintf(&buf, "%s v%d", runtime.Name, major)
	buf.WriteString("</title></head>\n<body>\n<h1>")
	fmt.Fprintf(&buf, "%s v%d binaries", runtime.Name, major)
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

	// Render JSON index: artifact entries + unsupported list filtered to this major.
	artifactIndex := collectArtifactIndexByMajor(runtime, major)
	allUnsupported := expandUnsupportedVersions(runtime, unsupported)
	majorUnsupported := []UnsupportedEntry{}
	for _, pv := range allUnsupported {
		if versionBelongsToMajor(pv.Version, major) {
			majorUnsupported = append(majorUnsupported, pv)
		}
	}

	versionOut := make(map[string]any, len(artifactIndex)+1)
	for k, v := range artifactIndex {
		versionOut[k] = v
	}
	versionOut["unsupported"] = majorUnsupported

	jsonData, err := json.MarshalIndent(versionOut, "", "  ")
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
	seen := make(map[string]bool)

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

					destBinary := fmt.Sprintf(
						"%s/%s/%d.%d/%s/%s",
						os,
						runtime.Name,
						version.Major,
						version.Minor,
						version.Version,
						artifact.Binary.Filename,
					)
					if seen[destBinary] {
						continue
					}
					seen[destBinary] = true

					sourcePath := fmt.Sprintf("%s/%s", release.ReleaseTag, artifact.Binary.Filename)
					entry := SimpleArtifactEntry{
						Platform:    artifact.Platform,
						Type:        artifactTypeFromFilename(artifact.Binary.Filename),
						Binary:      destBinary,
						SourcePath:  sourcePath,
						DownloadURL: artifact.Binary.URL,
						SHA256:      artifact.Binary.SHA256,
						Version:     version.Version,
					}
					if artifact.Audit != nil {
						entry.Audit = fmt.Sprintf("%s/%s", release.ReleaseTag, artifact.Audit.Filename)
					}
					if artifact.MetadataFile != nil {
						entry.Metadata = fmt.Sprintf("%s/%s", release.ReleaseTag, artifact.MetadataFile.Filename)
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

					destBinary := fmt.Sprintf(
						"%s/%s/%d.%d/%s/%s",
						os,
						runtime.Name,
						version.Major,
						version.Minor,
						version.Version,
						artifact.Binary.Filename,
					)
					if seen[destBinary] {
						continue
					}
					seen[destBinary] = true

					sourcePath := fmt.Sprintf("%s/%s", release.ReleaseTag, artifact.Binary.Filename)
					entry := SimpleArtifactEntry{
						Platform:    artifact.Platform,
						Type:        artifactTypeFromFilename(artifact.Binary.Filename),
						Binary:      destBinary,
						SourcePath:  sourcePath,
						DownloadURL: artifact.Binary.URL,
						SHA256:      artifact.Binary.SHA256,
						Version:     version.Version,
					}
					if artifact.Audit != nil {
						entry.Audit = fmt.Sprintf("%s/%s", release.ReleaseTag, artifact.Audit.Filename)
					}
					if artifact.MetadataFile != nil {
						entry.Metadata = fmt.Sprintf("%s/%s", release.ReleaseTag, artifact.MetadataFile.Filename)
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

			// Add metadata/proof files
			if artifact.MetadataFile != nil {
				distKey := artifact.MetadataFile.Filename + "|" + artifact.MetadataFile.URL
				if _, exists := distMap[distKey]; !exists {
					distMap[distKey] = DistributionModel{
						Filename: artifact.MetadataFile.Filename,
						URL:      artifact.MetadataFile.URL,
						SHA256:   artifact.MetadataFile.SHA256,
					}
				}
			}
		}
	}
}

// renderSimpleRootIndex renders /simple/index.html listing all runtimes.
func renderSimpleRootIndex(model *SiteModel, unsupported config.UnsupportedConfig, simpleDir string, logger *slog.Logger) error {
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
		fmt.Fprintf(&buf, "<a href=\"%s/\">%s</a><br/>\n", name, name)
	}

	buf.WriteString("\n</body>\n</html>\n")

	path := filepath.Join(simpleDir, "index.html")
	if err := writeFileIfChanged(path, buf.Bytes(), logger); err != nil {
		return fmt.Errorf("failed to write simple root index: %w", err)
	}

	// Render consolidated JSON index: all artifact paths keyed by OS, plus
	// an "unsupported" key mapping runtime name → []PolicyVersion.
	allIndex := collectAllArtifactIndex(model)
	out := make(map[string]any, len(allIndex)+1)
	for k, v := range allIndex {
		out[k] = v
	}
	unsupportedByRuntime := make(map[string][]UnsupportedEntry, len(model.Runtimes))
	for _, rt := range model.Runtimes {
		unsupportedByRuntime[rt.Name] = expandUnsupportedVersions(rt, unsupported)
	}
	out["unsupported"] = unsupportedByRuntime

	jsonData, err := json.MarshalIndent(out, "", "  ")
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

						destBinary := fmt.Sprintf(
							"%s/%s/%d.%d/%s/%s",
							os,
							runtime.Name,
							version.Major,
							version.Minor,
							version.Version,
							artifact.Binary.Filename,
						)
						if seen[destBinary] {
							continue
						}
						seen[destBinary] = true

						sourcePath := fmt.Sprintf("%s/%s", release.ReleaseTag, artifact.Binary.Filename)
						entry := SimpleArtifactEntry{
							Platform:    artifact.Platform,
							Type:        artifactTypeFromFilename(artifact.Binary.Filename),
							Binary:      destBinary,
							SourcePath:  sourcePath,
							DownloadURL: artifact.Binary.URL,
							SHA256:      artifact.Binary.SHA256,
							Version:     version.Version,
						}
						if artifact.Audit != nil {
							entry.Audit = fmt.Sprintf("%s/%s", release.ReleaseTag, artifact.Audit.Filename)
						}
						if artifact.MetadataFile != nil {
							entry.Metadata = fmt.Sprintf("%s/%s", release.ReleaseTag, artifact.MetadataFile.Filename)
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

// versionBelongsToMajor reports whether version string v has the given major version.
// Handles 1-component prefixes (e.g. "16"), 2-component (e.g. "3.9"), and full semver.
func versionBelongsToMajor(v string, major int) bool {
	parsedMajor, _, _, err := storage.ParseSemver(v)
	if err == nil {
		return parsedMajor == major
	}
	// Single-component prefix like "16"
	var m int
	if n, scanErr := fmt.Sscanf(v, "%d", &m); n == 1 && scanErr == nil {
		return m == major
	}
	return false
}

// parseSemverFull parses any version string (1, 2, or 3 components) into a
// comparable numeric tuple. Single-component strings like "8" or "16" return
// (major, 0, 0). This is used by the sort in expandUnsupportedVersions so that
// single-digit major prefixes ("8") sort correctly before double-digit ones ("10").
func parseSemverFull(v string) (major, minor, patch int, err error) {
	maj, min, pat, e := storage.ParseSemver(v)
	if e == nil {
		return maj, min, pat, nil
	}
	// Single-component (e.g. "8", "16")
	var m int
	if n, scanErr := fmt.Sscanf(v, "%d", &m); n == 1 && scanErr == nil {
		return m, 0, 0, nil
	}
	return 0, 0, 0, fmt.Errorf("cannot parse version %q", v)
}

// expandUnsupportedVersions builds the list of concrete unsupported versions for a runtime
// by walking every version present in the model and prefix-matching against unsupported rules.
// For each rule that matches at least one DB version, the rule's prefix (e.g. "3.9", "16") is
// also included so downstream clients can block the entire version line, not just the specific
// patches present in this artifact store.
// Duplicate concrete versions across platforms are deduplicated before matching.
// Always returns a non-nil slice so the JSON output is [] rather than null.
func expandUnsupportedVersions(rt RuntimeModel, uc config.UnsupportedConfig) []UnsupportedEntry {
	result := []UnsupportedEntry{}
	if len(uc) == 0 {
		return result
	}

	seenConcrete := make(map[string]struct{})
	seenPrefix := make(map[string]struct{})

	for _, platform := range rt.Platforms {
		for _, version := range platform.Versions {
			v := version.Version
			if _, already := seenConcrete[v]; already {
				continue
			}
			seenConcrete[v] = struct{}{}

			rule := uc.FindMatchingRule(rt.Name, v)
			if rule == nil {
				continue
			}

			// Emit the rule prefix once (e.g. "3.9", "16") so clients can block
			// the entire version line regardless of which patches they have installed.
			if _, prefixSeen := seenPrefix[rule.Version]; !prefixSeen && rule.Version != v {
				seenPrefix[rule.Version] = struct{}{}
				entry := UnsupportedEntry{
					Version:   rule.Version,
					Supported: false,
					Kind:      "line",
				}
				if rule.EOLDate != "" {
					entry.EOL = rule.EOLDate
				}
				if rule.Reason != "" {
					entry.Notes = rule.Reason
				}
				result = append(result, entry)
			}

			// Emit the concrete patch version (e.g. "3.9.25") present in the artifact store.
			entry := UnsupportedEntry{
				Version:   v,
				Supported: false,
				Kind:      "artifact",
			}
			if rule.EOLDate != "" {
				entry.EOL = rule.EOLDate
			}
			if rule.Reason != "" {
				entry.Notes = rule.Reason
			}
			result = append(result, entry)
		}
	}

	sort.Slice(result, func(i, j int) bool {
		iMaj, iMin, iPat, iErr := parseSemverFull(result[i].Version)
		jMaj, jMin, jPat, jErr := parseSemverFull(result[j].Version)
		if iErr != nil || jErr != nil {
			return result[i].Version < result[j].Version
		}
		if iMaj != jMaj {
			return iMaj < jMaj
		}
		if iMin != jMin {
			return iMin < jMin
		}
		return iPat < jPat
	})
	return result
}
