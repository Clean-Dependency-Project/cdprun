package platform

import (
	"fmt"
	"runtime"
	"strings"
)

// Platform represents a target OS/Architecture combination
type Platform struct {
	OS           string // windows, linux, mac
	Arch         string // x64, aarch64
	FileExt      string // zip, tar.gz
	DownloadName string // Name format in download URL
	Classifier   string // Classifier for Nexus
}

// PredefinedPlatforms returns common platform combinations
func PredefinedPlatforms() []Platform {
	return []Platform{
		{OS: "windows", Arch: "x64", FileExt: "zip", DownloadName: "windows", Classifier: "windows-x64"},
		{OS: "windows", Arch: "aarch64", FileExt: "zip", DownloadName: "windows", Classifier: "windows-aarch64"},
		{OS: "mac", Arch: "x64", FileExt: "tar.gz", DownloadName: "mac", Classifier: "mac-x64"},
		{OS: "mac", Arch: "aarch64", FileExt: "tar.gz", DownloadName: "mac", Classifier: "mac-aarch64"},
		{OS: "linux", Arch: "x64", FileExt: "tar.gz", DownloadName: "linux", Classifier: "linux-x64"},
		{OS: "linux", Arch: "aarch64", FileExt: "tar.gz", DownloadName: "linux", Classifier: "linux-aarch64"},
	}
}

// FindPlatform finds a platform by its classifier or OS-Arch combination
func FindPlatform(platformStr string) (Platform, error) {
	for _, p := range PredefinedPlatforms() {
		if p.Classifier == platformStr {
			return p, nil
		}

		// Also check for OS-Arch format
		if fmt.Sprintf("%s-%s", p.OS, p.Arch) == platformStr {
			return p, nil
		}
	}

	return Platform{}, fmt.Errorf("unknown platform: %s", platformStr)
}

// CurrentPlatform returns the platform for the current system
func CurrentPlatform() Platform {
	os := mapOS(runtime.GOOS)
	arch := mapArch(runtime.GOARCH)

	// Find matching predefined platform
	for _, p := range PredefinedPlatforms() {
		if p.OS == os && p.Arch == arch {
			return p
		}
	}

	// Fallback: construct platform if not in predefined list
	return buildPlatform(os, arch)
}

// mapOS converts Go's GOOS to our platform OS naming
func mapOS(goos string) string {
	switch goos {
	case "windows":
		return "windows"
	case "darwin":
		return "mac"
	default:
		return "linux"
	}
}

// mapArch converts Go's GOARCH to our platform architecture naming
func mapArch(goarch string) string {
	switch goarch {
	case "arm64":
		return "aarch64"
	default:
		return "x64"
	}
}

// buildPlatform constructs a Platform from OS and architecture strings
func buildPlatform(os, arch string) Platform {
	fileExt := "tar.gz"
	if os == "windows" {
		fileExt = "zip"
	}

	return Platform{
		OS:           os,
		Arch:         arch,
		FileExt:      fileExt,
		DownloadName: os,
		Classifier:   fmt.Sprintf("%s-%s", os, arch),
	}
}

// ResolvePlatforms converts platform flags to actual Platform objects
func ResolvePlatforms(platformFlags []string) ([]Platform, error) {
	allPlatforms := PredefinedPlatforms()

	// Check if "all" is specified
	for _, flag := range platformFlags {
		if strings.ToLower(flag) == "all" {
			return allPlatforms, nil
		}
	}

	// If no platforms specified, use current platform
	if len(platformFlags) == 0 {
		return []Platform{CurrentPlatform()}, nil
	}

	// Otherwise, resolve each platform
	var result []Platform
	for _, flag := range platformFlags {
		if platform, err := FindPlatform(flag); err == nil {
			result = append(result, platform)
		} else {
			return nil, fmt.Errorf("invalid platform: %s", flag)
		}
	}

	return result, nil
}
