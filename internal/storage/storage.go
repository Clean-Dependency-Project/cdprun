// Package storage provides download tracking using GORM and SQLite
package storage

import (
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Sentinel errors following Dave Cheney's principle: define errors as values
var (
	ErrNilDownload       = errors.New("download cannot be nil")
	ErrNotFound          = errors.New("download not found")
	ErrInvalidVersionFmt = errors.New("invalid version format: expected major.minor.patch")
)

// Download represents a downloaded runtime file with verification status
type Download struct {
	ID uint `gorm:"primaryKey"`

	// What was downloaded
	Runtime       string `gorm:"not null;index:idx_runtime_version;uniqueIndex:idx_unique_download"`
	Version       string `gorm:"not null;index:idx_runtime_version,idx_version;uniqueIndex:idx_unique_download"`
	VersionMajor  int    `gorm:"index"`
	VersionMinor  int
	VersionPatch  int
	Platform      string `gorm:"not null;index:idx_platform;uniqueIndex:idx_unique_download"`
	Architecture  string `gorm:"not null;index:idx_platform;uniqueIndex:idx_unique_download"`
	Filename      string `gorm:"not null"`
	FileExtension string
	FileSize      int64
	SourceURL     string `gorm:"not null"`

	// When
	DownloadedAt time.Time `gorm:"not null"`

	// Checksum verification
	ChecksumVerified  bool `gorm:"not null;default:false"`
	ChecksumAlgorithm string
	ChecksumValue     string
	ChecksumSourceURL string

	// GPG verification
	GPGVerified      bool `gorm:"not null;default:false"`
	GPGSignatureURL  string
	GPGKeyringSource string

	// Attestation (SLSA provenance with Sigstore)
	Attested          bool      `gorm:"not null;default:false"`
	AttestationFile   string    `gorm:"type:text"`
	AttestedAt        time.Time `gorm:"type:datetime"`
	AttestationIssuer string    `gorm:"type:varchar(50)"` // "github-actions" or "local"
	RekorLogIndex     int64     `gorm:"default:0"`        // Rekor transparency log index
	RekorLogID        string    `gorm:"type:varchar(128)"` // Rekor log entry ID

	// Status
	VerificationStatus string `gorm:"not null"`
	VerificationType   string
	ErrorMessage       string

	CreatedAt time.Time
	UpdatedAt time.Time
}

// Store defines the interface for download storage operations
type Store interface {
	Close() error
	RecordDownload(*Download) error
	GetDownload(runtime, version, platform, arch string) (*Download, error)
	IsAlreadyDownloaded(runtime, version, platform, arch string) (bool, error)
	UpdateVerification(id uint, checksumVerified, gpgVerified bool, status, errorMsg string) error
	UpdateChecksumVerification(id uint, verified bool, algorithm, value, sourceURL string) error
	UpdateGPGVerification(id uint, verified bool, signatureURL, keyringSource string) error
	UpdateAttestation(id uint, attested bool, attestationFile, issuer string, attestedAt time.Time) error
	UpdateAttestationWithRekor(id uint, attested bool, attestationFile, issuer string, attestedAt time.Time, rekorIndex int64, rekorID string) error
	ListAll() ([]*Download, error)
	ListByRuntime(runtime string) ([]*Download, error)
	ListByVersion(runtime, version string) ([]*Download, error)
	ListByPlatform(platform, arch string) ([]*Download, error)
	ListByMajorVersion(runtime string, majorVersion int) ([]*Download, error)
	GetStats() (map[string]interface{}, error)
}

// DB wraps gorm.DB with our download operations
type DB struct {
	db *gorm.DB
}

// Config holds database configuration
type Config struct {
	DatabasePath string
	LogLevel     string // silent, error, warn, info
}

// InitDB initializes the database connection and runs migrations
func InitDB(cfg Config) (*DB, error) {
	logLevel := logger.Silent
	switch cfg.LogLevel {
	case "error":
		logLevel = logger.Error
	case "warn":
		logLevel = logger.Warn
	case "info":
		logLevel = logger.Info
	}

	db, err := gorm.Open(sqlite.Open(cfg.DatabasePath), &gorm.Config{
		Logger: logger.Default.LogMode(logLevel),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Auto-migrate schema
	if err := db.AutoMigrate(&Download{}, &Release{}); err != nil {
		return nil, fmt.Errorf("failed to migrate schema: %w", err)
	}

	return &DB{db: db}, nil
}

// Close closes the database connection
func (d *DB) Close() error {
	sqlDB, err := d.db.DB()
	if err != nil {
		return fmt.Errorf("failed to get underlying SQL DB: %w", err)
	}
	if err := sqlDB.Close(); err != nil {
		return fmt.Errorf("failed to close database connection: %w", err)
	}
	return nil
}

// RecordDownload creates a new download record
func (d *DB) RecordDownload(download *Download) error {
	if download == nil {
		return ErrNilDownload
	}
	if err := d.db.Create(download).Error; err != nil {
		return fmt.Errorf("failed to record download: %w", err)
	}
	return nil
}

// GetDownload retrieves a download by runtime, version, platform, and architecture
func (d *DB) GetDownload(runtime, version, platform, arch string) (*Download, error) {
	var download Download
	err := d.db.Where("runtime = ? AND version = ? AND platform = ? AND architecture = ?",
		runtime, version, platform, arch).First(&download).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get download: %w", err)
	}
	return &download, nil
}

// IsAlreadyDownloaded checks if a file was successfully downloaded and verified
func (d *DB) IsAlreadyDownloaded(runtime, version, platform, arch string) (bool, error) {
	var count int64
	err := d.db.Model(&Download{}).Where(
		"runtime = ? AND version = ? AND platform = ? AND architecture = ? AND verification_status = ?",
		runtime, version, platform, arch, "success").Count(&count).Error
	if err != nil {
		return false, fmt.Errorf("failed to check if already downloaded: %w", err)
	}
	return count > 0, nil
}

// UpdateVerification updates verification status for a download
func (d *DB) UpdateVerification(id uint, checksumVerified, gpgVerified bool, status, errorMsg string) error {
	updates := map[string]interface{}{
		"checksum_verified":   checksumVerified,
		"gpg_verified":        gpgVerified,
		"verification_status": status,
	}
	if errorMsg != "" {
		updates["error_message"] = errorMsg
	}
	if err := d.db.Model(&Download{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return fmt.Errorf("failed to update verification for download %d: %w", id, err)
	}
	return nil
}

// UpdateChecksumVerification updates only checksum-related fields
func (d *DB) UpdateChecksumVerification(id uint, verified bool, algorithm, value, sourceURL string) error {
	if err := d.db.Model(&Download{}).Where("id = ?", id).Updates(map[string]interface{}{
		"checksum_verified":   verified,
		"checksum_algorithm":  algorithm,
		"checksum_value":      value,
		"checksum_source_url": sourceURL,
	}).Error; err != nil {
		return fmt.Errorf("failed to update checksum verification for download %d: %w", id, err)
	}
	return nil
}

// UpdateGPGVerification updates only GPG-related fields
func (d *DB) UpdateGPGVerification(id uint, verified bool, signatureURL, keyringSource string) error {
	if err := d.db.Model(&Download{}).Where("id = ?", id).Updates(map[string]interface{}{
		"gpg_verified":       verified,
		"gpg_signature_url":  signatureURL,
		"gpg_keyring_source": keyringSource,
	}).Error; err != nil {
		return fmt.Errorf("failed to update GPG verification for download %d: %w", id, err)
	}
	return nil
}

// UpdateVerificationType updates the verification_type field
func (d *DB) UpdateVerificationType(runtime, version, platform, arch, verificationType string) error {
	if err := d.db.Model(&Download{}).
		Where("runtime = ? AND version = ? AND platform = ? AND architecture = ?",
			runtime, version, platform, arch).
		Update("verification_type", verificationType).Error; err != nil {
		return fmt.Errorf("failed to update verification type: %w", err)
	}
	return nil
}

// UpdateAttestation updates attestation fields for a download
func (d *DB) UpdateAttestation(id uint, attested bool, attestationFile, issuer string, attestedAt time.Time) error {
	if err := d.db.Model(&Download{}).Where("id = ?", id).Updates(map[string]interface{}{
		"attested":           attested,
		"attestation_file":   attestationFile,
		"attestation_issuer": issuer,
		"attested_at":        attestedAt,
	}).Error; err != nil {
		return fmt.Errorf("failed to update attestation for download %d: %w", id, err)
	}
	return nil
}

// UpdateAttestationWithRekor updates attestation fields including Rekor transparency log information
func (d *DB) UpdateAttestationWithRekor(id uint, attested bool, attestationFile, issuer string, attestedAt time.Time, rekorIndex int64, rekorID string) error {
	if err := d.db.Model(&Download{}).Where("id = ?", id).Updates(map[string]interface{}{
		"attested":           attested,
		"attestation_file":   attestationFile,
		"attestation_issuer": issuer,
		"attested_at":        attestedAt,
		"rekor_log_index":    rekorIndex,
		"rekor_log_id":       rekorID,
	}).Error; err != nil {
		return fmt.Errorf("failed to update attestation with Rekor info for download %d: %w", id, err)
	}
	return nil
}

// ListAll returns all downloads
func (d *DB) ListAll() ([]*Download, error) {
	var downloads []*Download
	if err := d.db.Order("downloaded_at DESC").Find(&downloads).Error; err != nil {
		return nil, fmt.Errorf("failed to list all downloads: %w", err)
	}
	return downloads, nil
}

// ListByRuntime returns all downloads for a specific runtime
func (d *DB) ListByRuntime(runtime string) ([]*Download, error) {
	var downloads []*Download
	if err := d.db.Where("runtime = ?", runtime).Order("downloaded_at DESC").Find(&downloads).Error; err != nil {
		return nil, fmt.Errorf("failed to list downloads for runtime %s: %w", runtime, err)
	}
	return downloads, nil
}

// ListByVersion returns all downloads for a specific runtime and version
func (d *DB) ListByVersion(runtime, version string) ([]*Download, error) {
	var downloads []*Download
	if err := d.db.Where("runtime = ? AND version = ?", runtime, version).
		Order("downloaded_at DESC").Find(&downloads).Error; err != nil {
		return nil, fmt.Errorf("failed to list downloads for %s@%s: %w", runtime, version, err)
	}
	return downloads, nil
}

// ListByPlatform returns all downloads for a specific platform and architecture
func (d *DB) ListByPlatform(platform, arch string) ([]*Download, error) {
	var downloads []*Download
	if err := d.db.Where("platform = ? AND architecture = ?", platform, arch).
		Order("downloaded_at DESC").Find(&downloads).Error; err != nil {
		return nil, fmt.Errorf("failed to list downloads for platform %s-%s: %w", platform, arch, err)
	}
	return downloads, nil
}

// ListByMajorVersion returns all downloads matching a major version
func (d *DB) ListByMajorVersion(runtime string, majorVersion int) ([]*Download, error) {
	var downloads []*Download
	if err := d.db.Where("runtime = ? AND version_major = ?", runtime, majorVersion).
		Order("downloaded_at DESC").Find(&downloads).Error; err != nil {
		return nil, fmt.Errorf("failed to list downloads for %s v%d: %w", runtime, majorVersion, err)
	}
	return downloads, nil
}

// GetStats returns download statistics
func (d *DB) GetStats() (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	// Total downloads
	var total int64
	if err := d.db.Model(&Download{}).Count(&total).Error; err != nil {
		return nil, fmt.Errorf("failed to count total downloads: %w", err)
	}
	stats["total_downloads"] = total

	// By runtime
	var runtimeCounts []struct {
		Runtime string
		Count   int64
	}
	if err := d.db.Model(&Download{}).Select("runtime, COUNT(*) as count").
		Group("runtime").Scan(&runtimeCounts).Error; err != nil {
		return nil, fmt.Errorf("failed to get runtime counts: %w", err)
	}
	stats["by_runtime"] = runtimeCounts

	// Verification status
	var statusCounts []struct {
		Status string
		Count  int64
	}
	if err := d.db.Model(&Download{}).Select("verification_status, COUNT(*) as count").
		Group("verification_status").Scan(&statusCounts).Error; err != nil {
		return nil, fmt.Errorf("failed to get status counts: %w", err)
	}
	stats["by_status"] = statusCounts

	return stats, nil
}

// ParseSemver parses a semantic version string and returns major, minor, patch components.
// It expects versions in the format "major.minor.patch" (e.g., "1.2.3").
// Returns an error if the version string doesn't match the expected format.
func ParseSemver(version string) (major, minor, patch int, err error) {
	n, err := fmt.Sscanf(version, "%d.%d.%d", &major, &minor, &patch)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to parse version %q: %w", version, err)
	}
	if n != 3 {
		return 0, 0, 0, fmt.Errorf("%w: %q", ErrInvalidVersionFmt, version)
	}
	return major, minor, patch, nil
}

// ExtractFilename extracts the filename from a file path using filepath.Base.
func ExtractFilename(path string) string {
	return filepath.Base(path)
}

// ExtractExtension extracts the file extension from a filename using filepath.Ext.
func ExtractExtension(filename string) string {
	return filepath.Ext(filename)
}
