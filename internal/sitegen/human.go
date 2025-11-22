package sitegen

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"os"
	"path/filepath"

	"log/slog"
)

//go:embed templates/*.tmpl
var templateFS embed.FS

//go:embed assets/style.css
var assetsFS embed.FS

// RenderHumanPages generates human-readable HTML pages for browsing releases.
// Creates directory structure: /<runtime>/<os>/v<major>/<version>/index.html
func RenderHumanPages(model *SiteModel, outDir string, logger *slog.Logger) error {
	// Load templates
	tmpl, err := loadTemplates()
	if err != nil {
		return fmt.Errorf("failed to load templates: %w", err)
	}

	// Write shared assets (CSS)
	if err := writeSiteAssets(outDir, logger); err != nil {
		return fmt.Errorf("failed to write site assets: %w", err)
	}

	// Render root index
	if err := renderRootIndex(tmpl, model, outDir, logger); err != nil {
		return fmt.Errorf("failed to render root index: %w", err)
	}

	// Render runtime pages
	for _, runtime := range model.Runtimes {
		if err := renderRuntimePages(tmpl, runtime, outDir, logger); err != nil {
			return fmt.Errorf("failed to render runtime pages for %s: %w", runtime.Name, err)
		}
	}

	return nil
}

// writeSiteAssets writes embedded static assets (like CSS) to the output directory.
func writeSiteAssets(outDir string, logger *slog.Logger) error {
	assetsDir := filepath.Join(outDir, "assets")
	if err := os.MkdirAll(assetsDir, 0o755); err != nil {
		return fmt.Errorf("failed to create assets directory: %w", err)
	}

	data, err := fs.ReadFile(assetsFS, "assets/style.css")
	if err != nil {
		return fmt.Errorf("failed to read embedded style.css: %w", err)
	}

	path := filepath.Join(assetsDir, "style.css")
	if err := writeFileIfChanged(path, data, logger); err != nil {
		return fmt.Errorf("failed to write style.css: %w", err)
	}
	return nil
}

// loadTemplates loads all HTML templates with helper functions.
func loadTemplates() (*template.Template, error) {
	tmpl := template.New("").Funcs(template.FuncMap{
		"formatBytes": formatBytes,
	})

	// Parse all templates
	entries, err := templateFS.ReadDir("templates")
	if err != nil {
		return nil, fmt.Errorf("failed to read templates directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		// Skip PEP 503 templates (they're loaded separately)
		if entry.Name() == "simple_index.tmpl" || entry.Name() == "simple_package.tmpl" {
			continue
		}
		data, err := templateFS.ReadFile("templates/" + entry.Name())
		if err != nil {
			return nil, fmt.Errorf("failed to read template %s: %w", entry.Name(), err)
		}
		// Parse with the filename as the template name
		_, err = tmpl.New(entry.Name()).Parse(string(data))
		if err != nil {
			return nil, fmt.Errorf("failed to parse template %s: %w", entry.Name(), err)
		}
	}

	return tmpl, nil
}

// renderRootIndex renders the root index.html listing all runtimes.
func renderRootIndex(tmpl *template.Template, model *SiteModel, outDir string, logger *slog.Logger) error {
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "root.tmpl", model); err != nil {
		return fmt.Errorf("failed to execute root template: %w", err)
	}

	path := filepath.Join(outDir, "index.html")
	if err := writeFileIfChanged(path, buf.Bytes(), logger); err != nil {
		return fmt.Errorf("failed to write root index: %w", err)
	}

	logger.Info("rendered root index", "path", path)
	return nil
}

// renderRuntimePages renders all pages for a runtime.
func renderRuntimePages(tmpl *template.Template, runtime RuntimeModel, outDir string, logger *slog.Logger) error {
	// Render runtime index: /<runtime>/index.html
	runtimeDir := filepath.Join(outDir, runtime.Name)
	if err := os.MkdirAll(runtimeDir, 0755); err != nil {
		return fmt.Errorf("failed to create runtime directory: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "runtime.tmpl", runtime); err != nil {
		return fmt.Errorf("failed to execute runtime template: %w", err)
	}

	runtimeIndexPath := filepath.Join(runtimeDir, "index.html")
	if err := writeFileIfChanged(runtimeIndexPath, buf.Bytes(), logger); err != nil {
		return fmt.Errorf("failed to write runtime index: %w", err)
	}

	// Render OS pages
	for _, platform := range runtime.Platforms {
		if err := renderOSPages(tmpl, runtime.Name, platform, runtimeDir, logger); err != nil {
			return fmt.Errorf("failed to render OS pages for %s/%s: %w", runtime.Name, platform.OS, err)
		}
	}

	return nil
}

// renderOSPages renders all pages for an OS within a runtime.
func renderOSPages(tmpl *template.Template, runtimeName string, platform PlatformModel, runtimeDir string, logger *slog.Logger) error {
	osDir := filepath.Join(runtimeDir, platform.OS)
	if err := os.MkdirAll(osDir, 0755); err != nil {
		return fmt.Errorf("failed to create OS directory: %w", err)
	}

	// Group versions by major version
	type majorVersionGroup struct {
		Major    int
		Versions []VersionModel
	}
	
	majorMap := make(map[int][]VersionModel)
	for _, version := range platform.Versions {
		majorMap[version.Major] = append(majorMap[version.Major], version)
	}
	
	// Create sorted list of major versions (descending)
	var majors []int
	for major := range majorMap {
		majors = append(majors, major)
	}
	// Sort descending (newest first)
	for i := 0; i < len(majors); i++ {
		for j := i + 1; j < len(majors); j++ {
			if majors[i] < majors[j] {
				majors[i], majors[j] = majors[j], majors[i]
			}
		}
	}
	
	var versionsByMajor []majorVersionGroup
	for _, major := range majors {
		versionsByMajor = append(versionsByMajor, majorVersionGroup{
			Major:    major,
			Versions: majorMap[major],
		})
	}

	// Render OS index: /<runtime>/<os>/index.html
	osData := struct {
		Runtime         string
		OS              string
		VersionsByMajor []majorVersionGroup
	}{
		Runtime:         runtimeName,
		OS:              platform.OS,
		VersionsByMajor: versionsByMajor,
	}

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "os.tmpl", osData); err != nil {
		return fmt.Errorf("failed to execute OS template: %w", err)
	}

	osIndexPath := filepath.Join(osDir, "index.html")
	if err := writeFileIfChanged(osIndexPath, buf.Bytes(), logger); err != nil {
		return fmt.Errorf("failed to write OS index: %w", err)
	}

	// Render version pages
	for _, version := range platform.Versions {
		if err := renderVersionPages(tmpl, runtimeName, platform.OS, version, osDir, logger); err != nil {
			return fmt.Errorf("failed to render version pages for %s/%s/v%d/%s: %w", runtimeName, platform.OS, version.Major, version.Version, err)
		}
	}

	return nil
}

// renderVersionPages renders the version page with artifacts.
func renderVersionPages(tmpl *template.Template, runtimeName, osName string, version VersionModel, osDir string, logger *slog.Logger) error {
	versionDir := filepath.Join(osDir, fmt.Sprintf("v%d", version.Major), version.Version)
	if err := os.MkdirAll(versionDir, 0755); err != nil {
		return fmt.Errorf("failed to create version directory: %w", err)
	}

	versionData := struct {
		Runtime  string
		OS       string
		Major    int
		Version  string
		Releases []ReleaseModel
	}{
		Runtime:  runtimeName,
		OS:       osName,
		Major:    version.Major,
		Version:  version.Version,
		Releases: version.Releases,
	}

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "version.tmpl", versionData); err != nil {
		return fmt.Errorf("failed to execute version template: %w", err)
	}

	versionIndexPath := filepath.Join(versionDir, "index.html")
	if err := writeFileIfChanged(versionIndexPath, buf.Bytes(), logger); err != nil {
		return fmt.Errorf("failed to write version index: %w", err)
	}

	return nil
}

// formatBytes formats a byte count as a human-readable string.
func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
