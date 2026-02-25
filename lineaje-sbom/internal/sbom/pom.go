package sbom

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"deps.dev/util/maven"
)

// PURLsFromPOM reads a Maven pom.xml file and returns PURLs for dependencies with resolved versions.
// It uses deps.dev/util/maven to parse and interpolate; ProcessDependencies is called with a no-op
// callback so only in-POM dependency management is applied (no remote BOM imports).
func PURLsFromPOM(path string) ([]string, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve path: %w", err)
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	var proj maven.Project
	if err := xml.Unmarshal(data, &proj); err != nil {
		return nil, fmt.Errorf("parse pom.xml: %w", err)
	}
	if err := proj.Interpolate(); err != nil {
		return nil, fmt.Errorf("interpolate: %w", err)
	}
	noOpGetDM := func(_, _, _ maven.String) (maven.DependencyManagement, error) {
		return maven.DependencyManagement{}, nil
	}
	proj.ProcessDependencies(noOpGetDM)

	var purls []string
	seen := make(map[string]struct{})
	for _, dep := range proj.Dependencies {
		version := strings.TrimSpace(string(dep.Version))
		if version == "" || strings.Contains(version, "${") {
			continue
		}
		group := strings.TrimSpace(string(dep.GroupID))
		artifact := strings.TrimSpace(string(dep.ArtifactID))
		if group == "" || artifact == "" {
			continue
		}
		purl := fmt.Sprintf("pkg:maven/%s/%s@%s", group, artifact, version)
		if _, ok := seen[purl]; ok {
			continue
		}
		seen[purl] = struct{}{}
		purls = append(purls, purl)
	}
	return purls, nil
}
