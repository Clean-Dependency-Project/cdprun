package sitegen

import (
	"strings"
	"unicode"
)

// NormalizePackageName normalizes a package name according to PEP 503.
// PEP 503 requires: lowercase, replace runs of non-alphanumeric with single hyphen,
// remove leading/trailing hyphens/underscores.
func NormalizePackageName(name string) string {
	if name == "" {
		return ""
	}

	var result strings.Builder
	prevWasSeparator := false

	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			result.WriteRune(unicode.ToLower(r))
			prevWasSeparator = false
		} else if !prevWasSeparator {
			result.WriteRune('-')
			prevWasSeparator = true
		}
	}

	normalized := result.String()
	
	// Remove leading and trailing hyphens/underscores
	normalized = strings.Trim(normalized, "-_")
	
	return normalized
}

