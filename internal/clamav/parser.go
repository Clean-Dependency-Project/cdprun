package clamav

import (
	"errors"
	"regexp"
	"strings"
)

// Sentinel errors
var (
	ErrNoThreatsInOutput = errors.New("malware detected but no threats found in output")
)

// parseResult extracts scan results from clamscan output.
func parseResult(output []byte, exitCode int, version string) (Result, error) {
	outputStr := string(output)

	result := Result{
		Clean: isClean(exitCode),
		Metadata: Metadata{
			EngineVersion: version,
			DatabaseDate:  extractDatabaseDate(version),
		},
	}

	if !result.Clean {
		result.Threats = extractThreats(outputStr)
		if len(result.Threats) == 0 && exitCode == 1 {
			return result, ErrNoThreatsInOutput
		}
	}

	return result, nil
}

// isClean determines if the file is clean based on exit code.
// Exit code 0 = clean, 1 = infected, 2+ = error
func isClean(exitCode int) bool {
	return exitCode == 0
}

// extractThreats finds all "FOUND" lines and extracts threat names.
func extractThreats(output string) []string {
	var threats []string
	lines := strings.Split(output, "\n")

	for _, line := range lines {
		if strings.Contains(line, " FOUND") {
			// Format: "/scan/file: Threat-Name FOUND"
			parts := strings.Split(line, ":")
			if len(parts) >= 2 {
				threatPart := strings.TrimSpace(parts[1])
				threatName := strings.TrimSuffix(threatPart, " FOUND")
				threats = append(threats, strings.TrimSpace(threatName))
			}
		}
	}

	return threats
}

// extractDatabaseDate parses the virus database date from version string.
// Example version: "ClamAV 1.5.1/27805/Mon Oct 27 09:50:30 2025"
func extractDatabaseDate(version string) string {
	// Look for date pattern after version info
	// Example: "Mon Oct 27 09:50:30 2025"
	re := regexp.MustCompile(`ClamAV \d+\.\d+\.\d+/\d+/([A-Za-z]{3} [A-Za-z]{3}\s+\d+\s+\d+:\d+:\d+ \d{4})`)
	matches := re.FindStringSubmatch(version)

	if len(matches) >= 2 {
		return matches[1]
	}

	return "unknown"
}

