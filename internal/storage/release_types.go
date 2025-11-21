// Package storage provides database models and operations for release tracking.
package storage

import "time"

// Release represents a GitHub release of a runtime version with all its artifacts.
type Release struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	Runtime     string    `gorm:"not null;index:idx_release_runtime_version" json:"runtime"`
	Version     string    `gorm:"not null;index:idx_release_runtime_version" json:"version"`
	SemverMajor int       `gorm:"not null" json:"semver_major"`
	SemverMinor int       `gorm:"not null" json:"semver_minor"`
	SemverPatch int       `gorm:"not null" json:"semver_patch"`
	ReleaseTag  string    `gorm:"not null;unique" json:"release_tag"`
	ReleaseURL  string    `gorm:"not null" json:"release_url"`
	Artifacts   string    `gorm:"type:json" json:"artifacts"` // JSON blob
	CreatedAt   time.Time `gorm:"not null" json:"created_at"`
}

// TableName overrides the table name for GORM.
func (Release) TableName() string {
	return "releases"
}

// ReleaseArtifacts represents the complete artifacts structure stored in the JSON column.
// This structure is stored as JSON in the Release.Artifacts field.
type ReleaseArtifacts struct {
	Platforms   []PlatformArtifact `json:"platforms"`
	CommonFiles []CommonFile       `json:"common_files"`
	Metadata    ArtifactsMetadata  `json:"metadata"`
}

// PlatformArtifact represents all artifacts for a specific platform (e.g., linux-x64).
type PlatformArtifact struct {
	Platform     string            `json:"platform"`      // e.g., "linux-x64"
	PlatformOS   string            `json:"platform_os"`   // e.g., "linux"
	PlatformArch string            `json:"platform_arch"` // e.g., "x64"
	Binary       *ArtifactFile     `json:"binary"`
	Audit        *AuditArtifact    `json:"audit"`
	Signature    *ArtifactFile     `json:"signature,omitempty"`    // Cosign .sig file
	Certificate  *ArtifactFile     `json:"certificate,omitempty"`  // Cosign .cert file
}

// ArtifactFile represents a single file artifact (binary, signature, certificate).
type ArtifactFile struct {
	Filename   string    `json:"filename"`
	Size       int64     `json:"size"`
	SHA256     string    `json:"sha256,omitempty"`
	URL        string    `json:"url"`
	UploadedAt time.Time `json:"uploaded_at"`
}

// AuditArtifact represents an audit.json file with additional verification metadata.
type AuditArtifact struct {
	Filename         string    `json:"filename"`
	Size             int64     `json:"size"`
	URL              string    `json:"url"`
	ClamAVClean      bool      `json:"clamav_clean"`
	ChecksumVerified bool      `json:"checksum_verified"`
	GPGVerified      bool      `json:"gpg_verified"`
	UploadedAt       time.Time `json:"uploaded_at"`
}

// CommonFile represents release-level files (not platform-specific).
type CommonFile struct {
	Type       string    `json:"type"` // e.g., "checksum_file", "checksum_signature"
	Filename   string    `json:"filename"`
	Size       int64     `json:"size"`
	URL        string    `json:"url"`
	UploadedAt time.Time `json:"uploaded_at"`
}

// ArtifactsMetadata contains summary information about all artifacts.
type ArtifactsMetadata struct {
	TotalArtifacts       int   `json:"total_artifacts"`
	TotalSizeBytes       int64 `json:"total_size_bytes"`
	UploadDurationSecs   int   `json:"upload_duration_seconds"`
	PlatformCount        int   `json:"platform_count"`
	HasSignatures        bool  `json:"has_signatures"`
	HasCertificates      bool  `json:"has_certificates"`
	AllClamAVClean       bool  `json:"all_clamav_clean"`
	AllChecksumsVerified bool  `json:"all_checksums_verified"`
}

// Example JSON structure stored in Release.Artifacts:
//
// {
//   "platforms": [
//     {
//       "platform": "linux-x64",
//       "platform_os": "linux",
//       "platform_arch": "x64",
//       "binary": {
//         "filename": "node-v22.15.0-linux-x64.tar.xz",
//         "size": 30023544,
//         "sha256": "dafe2e8f82cb97de1bd10db9e2ec4c07bbf53389b0799b1e095a918951e78fd4",
//         "url": "https://github.com/owner/repo/releases/download/nodejs-v22.15.0-20251109T120000Z/node-v22.15.0-linux-x64.tar.xz",
//         "uploaded_at": "2025-11-09T12:00:00Z"
//       },
//       "audit": {
//         "filename": "node-v22.15.0-linux-x64.tar.xz.audit.json",
//         "size": 1024,
//         "url": "https://github.com/owner/repo/releases/download/nodejs-v22.15.0-20251109T120000Z/node-v22.15.0-linux-x64.tar.xz.audit.json",
//         "clamav_clean": true,
//         "checksum_verified": true,
//         "gpg_verified": false,
//         "uploaded_at": "2025-11-09T12:00:01Z"
//       },
//       "signature": {
//         "filename": "node-v22.15.0-linux-x64.tar.xz.sig",
//         "size": 256,
//         "url": "https://github.com/owner/repo/releases/download/nodejs-v22.15.0-20251109T120000Z/node-v22.15.0-linux-x64.tar.xz.sig",
//         "uploaded_at": "2025-11-09T12:00:02Z"
//       },
//       "certificate": {
//         "filename": "node-v22.15.0-linux-x64.tar.xz.cert",
//         "size": 512,
//         "url": "https://github.com/owner/repo/releases/download/nodejs-v22.15.0-20251109T120000Z/node-v22.15.0-linux-x64.tar.xz.cert",
//         "uploaded_at": "2025-11-09T12:00:03Z"
//       }
//     },
//     {
//       "platform": "windows-x64",
//       "platform_os": "windows",
//       "platform_arch": "x64",
//       "binary": {
//         "filename": "node-v22.15.0-x64.msi",
//         "size": 28000000,
//         "sha256": "abc123...",
//         "url": "https://github.com/owner/repo/releases/download/nodejs-v22.15.0-20251109T120000Z/node-v22.15.0-x64.msi",
//         "uploaded_at": "2025-11-09T12:00:10Z"
//       },
//       "audit": {
//         "filename": "node-v22.15.0-x64.msi.audit.json",
//         "size": 1024,
//         "url": "https://github.com/owner/repo/releases/download/nodejs-v22.15.0-20251109T120000Z/node-v22.15.0-x64.msi.audit.json",
//         "clamav_clean": true,
//         "checksum_verified": true,
//         "gpg_verified": false,
//         "uploaded_at": "2025-11-09T12:00:11Z"
//       }
//     }
//   ],
//   "common_files": [
//     {
//       "type": "checksum_file",
//       "filename": "SHASUMS256.txt",
//       "size": 2048,
//       "url": "https://github.com/owner/repo/releases/download/nodejs-v22.15.0-20251109T120000Z/SHASUMS256.txt",
//       "uploaded_at": "2025-11-09T12:00:20Z"
//     },
//     {
//       "type": "checksum_signature",
//       "filename": "SHASUMS256.txt.sig",
//       "size": 256,
//       "url": "https://github.com/owner/repo/releases/download/nodejs-v22.15.0-20251109T120000Z/SHASUMS256.txt.sig",
//       "uploaded_at": "2025-11-09T12:00:21Z"
//     }
//   ],
//   "metadata": {
//     "total_artifacts": 10,
//     "total_size_bytes": 120000000,
//     "upload_duration_seconds": 45,
//     "platform_count": 3,
//     "has_signatures": true,
//     "has_certificates": true,
//     "all_clamav_clean": true,
//     "all_checksums_verified": true
//   }
// }

