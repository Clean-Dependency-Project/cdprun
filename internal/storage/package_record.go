package storage

import (
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrNilPackageRecord = errors.New("package record cannot be nil")
)

// PackageRecord tracks package build/test/promotion state for idempotent orchestration.
type PackageRecord struct {
	ID uint `gorm:"primaryKey"`

	Runtime       string `gorm:"not null;uniqueIndex:idx_unique_package_record"`
	Version       string `gorm:"not null;uniqueIndex:idx_unique_package_record"`
	PackageType   string `gorm:"not null;uniqueIndex:idx_unique_package_record"` // rpm|apk
	PlatformOS    string `gorm:"not null;uniqueIndex:idx_unique_package_record"` // linux
	PlatformArch  string `gorm:"not null;uniqueIndex:idx_unique_package_record"` // x64,aarch64
	InputSHA256   string `gorm:"not null;uniqueIndex:idx_unique_package_record"`
	PackageName   string `gorm:"not null;uniqueIndex:idx_unique_package_record"`
	InstallPrefix string `gorm:"not null;uniqueIndex:idx_unique_package_record"`

	PackageFilename string
	PackageSHA256   string
	BuildStatus     string // pending|success|failed
	TestStatus      string // pending|success|failed
	Promoted        bool   `gorm:"not null;default:false;index"`
	PromotedAt      *time.Time
	ReleaseTag      string

	CreatedAt time.Time
	UpdatedAt time.Time
}

func (PackageRecord) TableName() string {
	return "package_records"
}

// UpsertPackageRecord creates or updates a package record by unique identity key.
func (d *DB) UpsertPackageRecord(record *PackageRecord) error {
	if record == nil {
		return ErrNilPackageRecord
	}
	if err := d.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "runtime"},
			{Name: "version"},
			{Name: "package_type"},
			{Name: "platform_os"},
			{Name: "platform_arch"},
			{Name: "input_sha256"},
			{Name: "package_name"},
			{Name: "install_prefix"},
		},
		DoUpdates: clause.AssignmentColumns([]string{
			"package_filename",
			"package_sha256",
			"build_status",
			"test_status",
			"promoted",
			"promoted_at",
			"release_tag",
			"updated_at",
		}),
	}).Create(record).Error; err != nil {
		return fmt.Errorf("upsert package record: %w", err)
	}
	return nil
}

// IsPackagePromoted reports whether an identical package identity has already been promoted.
func (d *DB) IsPackagePromoted(
	runtimeName, version, packageType, platformOS, platformArch, inputSHA256, packageName, installPrefix string,
) (bool, error) {
	var count int64
	err := d.db.Model(&PackageRecord{}).Where(
		"runtime = ? AND version = ? AND package_type = ? AND platform_os = ? AND platform_arch = ? AND input_sha256 = ? AND package_name = ? AND install_prefix = ? AND promoted = ?",
		runtimeName, version, packageType, platformOS, platformArch, inputSHA256, packageName, installPrefix, true,
	).Count(&count).Error
	if err != nil {
		return false, fmt.Errorf("check package promoted: %w", err)
	}
	return count > 0, nil
}

// GetPackageRecord returns a package record by identity.
func (d *DB) GetPackageRecord(
	runtimeName, version, packageType, platformOS, platformArch, inputSHA256, packageName, installPrefix string,
) (*PackageRecord, error) {
	var rec PackageRecord
	err := d.db.Where(
		"runtime = ? AND version = ? AND package_type = ? AND platform_os = ? AND platform_arch = ? AND input_sha256 = ? AND package_name = ? AND install_prefix = ?",
		runtimeName, version, packageType, platformOS, platformArch, inputSHA256, packageName, installPrefix,
	).First(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get package record: %w", err)
	}
	return &rec, nil
}
