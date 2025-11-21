package sitegen

import "time"

// SiteModel represents the complete site structure for HTML generation.
type SiteModel struct {
	Runtimes []RuntimeModel
}

// RuntimeModel represents all data for a specific runtime (e.g., nodejs, python).
type RuntimeModel struct {
	Name     string
	Platforms []PlatformModel
}

// PlatformModel represents all versions for a specific OS (linux, mac, windows).
type PlatformModel struct {
	OS       string // "linux", "mac", "windows"
	Versions []VersionModel
}

// VersionModel represents all artifacts for a specific version.
type VersionModel struct {
	Major    int
	Minor    int
	Patch    int
	Version  string // Full version string (e.g., "22.15.0")
	Releases []ReleaseModel
}

// ReleaseModel represents a single release with its artifacts.
type ReleaseModel struct {
	ReleaseTag  string
	ReleaseURL  string
	CreatedAt   time.Time
	Artifacts   []ArtifactModel
}

// ArtifactModel represents a single downloadable artifact.
type ArtifactModel struct {
	Platform     string // e.g., "linux-x64"
	PlatformOS   string // e.g., "linux"
	PlatformArch string // e.g., "x64"
	Binary       *FileModel
	Audit        *FileModel
	Signature    *FileModel
	Certificate  *FileModel
}

// FileModel represents a single file with its metadata.
type FileModel struct {
	Filename string
	Size     int64
	SHA256   string
	URL      string
}

// SimplePackageModel represents a PEP 503 package with all its distributions.
type SimplePackageModel struct {
	Name         string // Normalized package name (e.g., "nodejs-linux-x64")
	Distributions []DistributionModel
}

// DistributionModel represents a single distribution file for PEP 503.
type DistributionModel struct {
	Filename string
	URL      string
	SHA256   string // Included in URL fragment as #sha256=<hash>
}

