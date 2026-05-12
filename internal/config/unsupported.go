// Package config provides configuration management for the unified runtime download system.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// UnsupportedVersionInput represents a single unsupported version rule as declared
// by the operator in the unsupported-versions YAML/JSON input file.
// The Version field uses prefix matching (same semantics as ignore-versions.yaml).
// Reason and EOLDate are author-friendly names; they are mapped to PolicyVersion
// fields (notes, eol) when serialised into the sitegen JSON output.
type UnsupportedVersionInput struct {
	Version string `yaml:"version" json:"version"`
	Reason  string `yaml:"reason,omitempty" json:"reason,omitempty"`
	EOLDate string `yaml:"eol_date,omitempty" json:"eol_date,omitempty"`
}

// UnsupportedConfig maps runtime names to their list of unsupported version rules.
// Example:
//
//	nodejs:
//	  - version: "16"
//	    reason: "EOL since 2023-09-11"
//	    eol_date: "2023-09-11"
type UnsupportedConfig map[string][]UnsupportedVersionInput

// LoadUnsupportedConfig loads an unsupported-versions configuration file.
// Supports both YAML (.yaml/.yml) and JSON (.json) formats, detected by extension.
// Returns an empty config if filePath is empty.
func LoadUnsupportedConfig(filePath string) (UnsupportedConfig, error) {
	if filePath == "" {
		return UnsupportedConfig{}, nil
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read unsupported versions file %s: %w", filePath, err)
	}
	var raw map[string][]UnsupportedVersionInput
	switch ext := filepath.Ext(filePath); ext {
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, &raw); err != nil {
			return nil, fmt.Errorf("failed to parse YAML unsupported versions file %s: %w", filePath, err)
		}
	default:
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, fmt.Errorf("failed to parse JSON unsupported versions file %s: %w", filePath, err)
		}
	}
	return UnsupportedConfig(raw), nil
}

// IsVersionUnsupported returns true if the given version matches any unsupported
// rule for the runtime using prefix matching.
// "16" matches "16.20.2" but not "160.0.1" — the prefix must be followed by a
// non-digit character or be an exact match, mirroring the semantics of IgnoreConfig.
func (uc UnsupportedConfig) IsVersionUnsupported(runtime, version string) bool {
	return uc.FindMatchingRule(runtime, version) != nil
}

// FindMatchingRule returns the first rule whose Version prefix matches the given
// concrete version for the runtime, or nil if no rule matches.
func (uc UnsupportedConfig) FindMatchingRule(runtime, version string) *UnsupportedVersionInput {
	rules, ok := uc[runtime]
	if !ok {
		return nil
	}
	for i := range rules {
		if matchUnsupportedPrefix(rules[i].Version, version) {
			return &rules[i]
		}
	}
	return nil
}

// matchUnsupportedPrefix reports whether candidate starts with prefix and the
// next character (if any) is not a digit, preventing "16" from matching "160.0.1".
func matchUnsupportedPrefix(prefix, candidate string) bool {
	if prefix == "" {
		return false
	}
	if candidate == prefix {
		return true
	}
	if len(candidate) > len(prefix) && candidate[:len(prefix)] == prefix {
		next := candidate[len(prefix)]
		return next < '0' || next > '9'
	}
	return false
}
