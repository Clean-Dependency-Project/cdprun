// Package sbom provides parsing of SBOM and pom.xml to extract PURLs for the Lineaje API.
package sbom

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// cycloneDXBOM is a minimal struct for CycloneDX JSON (components with purl).
type cycloneDXBOM struct {
	Components []cycloneDXComponent `json:"components"`
}

type cycloneDXComponent struct {
	Purl string `json:"purl"`
}

// PURLsFromCycloneDX reads a CycloneDX JSON file and returns non-empty component PURLs.
func PURLsFromCycloneDX(path string) ([]string, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve path: %w", err)
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	var bom cycloneDXBOM
	if err := json.Unmarshal(data, &bom); err != nil {
		return nil, fmt.Errorf("parse CycloneDX JSON: %w", err)
	}
	var purls []string
	seen := make(map[string]struct{})
	for _, c := range bom.Components {
		p := trimSpace(c.Purl)
		if p == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		purls = append(purls, p)
	}
	return purls, nil
}

func trimSpace(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}
