package packaging

import "time"

type PackageType string

const (
	PackageTypeRPM PackageType = "rpm"
	PackageTypeAPK PackageType = "apk"
)

type InputMode string

const (
	// InputModePayloadDir packages an already-prepared payload directory representing filesystem root.
	InputModePayloadDir InputMode = "payload-dir"
	// InputModeArchiveTarball stages a tarball payload into an install prefix under a payload root.
	InputModeArchiveTarball InputMode = "archive-tarball"
)

type InputInfo struct {
	Path      string `json:"path"`
	SHA256    string `json:"sha256"`
	SourceURL string `json:"source_url,omitempty"`
	RunID     string `json:"run_id,omitempty"`
}

type BuildRequest struct {
	Runtime       string      `json:"runtime"`
	Version       string      `json:"version"`
	PackageType   PackageType `json:"package_type"`
	PackageName   string      `json:"package_name"`
	Release       string      `json:"release"`
	Arch          string      `json:"arch"`
	InstallPrefix string      `json:"install_prefix"`

	InputMode  InputMode `json:"input_mode"`
	Input      InputInfo `json:"input"`
	PayloadDir string    `json:"payload_dir,omitempty"`

	OutDir  string `json:"out_dir"`
	Summary string `json:"summary,omitempty"`
	License string `json:"license,omitempty"`
	URL     string `json:"url,omitempty"`
}

type BuildResult struct {
	Runtime       string      `json:"runtime"`
	Version       string      `json:"version"`
	PackageType   PackageType `json:"package_type"`
	PackageName   string      `json:"package_name"`
	Release       string      `json:"release"`
	Arch          string      `json:"arch"`
	InstallPrefix string      `json:"install_prefix"`
	Input         InputInfo   `json:"input"`

	PackageFilename string        `json:"package_filename"`
	PackagePath     string        `json:"package_path"`
	PackageSHA256   string        `json:"package_sha256"`
	Duration        time.Duration `json:"duration"`
}
