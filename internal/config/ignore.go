package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// IgnoreConfig supports runtime-level and platform-specific ignore rules.
// Structure examples:
//
//		{
//		  "tomcat": ["10.0", "8.0.53"],
//		  "nodejs": {"all": ["22"], "windows": ["20"], "mac": {"all": ["18"], "aarch64": ["20.11"]} }
//		}
//
//	  - Runtime value can be an array (applies to all platforms) or an object with
//	    keys: "all" (array), and OS keys ("windows", "linux", "mac").
//	  - OS value can be an array (applies to all arches) or an object with keys
//	    "all" (array) and arch keys (e.g., "x64", "aarch64").
type IgnoreConfig map[string]any

// LoadIgnoreConfig loads an ignore configuration file if provided.
// Returns an empty config if filePath is empty.
func LoadIgnoreConfig(filePath string) (IgnoreConfig, error) {
	if filePath == "" {
		return IgnoreConfig{}, nil
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read ignore file %s: %w", filePath, err)
	}
	var raw map[string]any
	switch ext := filepath.Ext(filePath); ext {
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, &raw); err != nil {
			return nil, fmt.Errorf("failed to parse YAML ignore file %s: %w", filePath, err)
		}
	default:
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, fmt.Errorf("failed to parse JSON ignore file %s: %w", filePath, err)
		}
	}
	return IgnoreConfig(raw), nil
}

// IsVersionIgnored returns true if a version should be ignored for the runtime across all platforms.
// Global (runtime-wide) ignores are not supported. Only platform-specific ignores apply.
func (ic IgnoreConfig) IsVersionIgnored(runtimeName, version string) bool { return false }

// IsPlatformIgnored returns true if a version should be ignored for the runtime on a specific OS/arch.
func (ic IgnoreConfig) IsPlatformIgnored(runtimeName, version, osName, arch string) bool {
	v, ok := ic[runtimeName]
	if !ok {
		return false
	}

	// If entire version is ignored globally, then platform is ignored as well
	if ic.IsVersionIgnored(runtimeName, version) {
		return true
	}

	rules, ok := v.(map[string]any)
	if !ok {
		return false
	}

	// OS-level rules
	osVal, ok := rules[osName]
	if !ok {
		return false
	}

	switch osRules := osVal.(type) {
	case []any:
		return matchPatterns(osRules, version)
	case map[string]any:
		// OS-wide patterns
		if all, ok := osRules["all"]; ok {
			if arr, ok := all.([]any); ok && matchPatterns(arr, version) {
				return true
			}
		}
		// Arch-specific patterns
		if archArr, ok := osRules[arch]; ok {
			if arr, ok := archArr.([]any); ok && matchPatterns(arr, version) {
				return true
			}
		}
	}

	return false
}

func matchPatterns(patterns []any, version string) bool {
	for _, p := range patterns {
		s, ok := p.(string)
		if !ok || s == "" {
			continue
		}
		if s == version {
			return true
		}
		if len(version) > len(s) && version[:len(s)] == s {
			return true
		}
	}
	return false
}
