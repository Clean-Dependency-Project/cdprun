package storage

import (
	"testing"
	"time"
)

func TestDB_UpsertPackageRecord_AndIsPackagePromoted(t *testing.T) {
	db := newTestDB(t)

	rec := &PackageRecord{
		Runtime:         "nodejs",
		Version:         "22.22.0",
		PackageType:     "rpm",
		PlatformOS:      "linux",
		PlatformArch:    "x64",
		InputSHA256:     "sha-in-1",
		PackageName:     "OSPO-nodejs",
		InstallPrefix:   "/export/apps/citools/OSPO-nodejs/22.22.0",
		PackageFilename: "OSPO-nodejs-22.22.0-1.x86_64.rpm",
		PackageSHA256:   "sha-pkg-1",
		BuildStatus:     "success",
		TestStatus:      "success",
		Promoted:        false,
	}

	if err := db.UpsertPackageRecord(rec); err != nil {
		t.Fatalf("UpsertPackageRecord() create error: %v", err)
	}

	promoted, err := db.IsPackagePromoted(
		rec.Runtime, rec.Version, rec.PackageType, rec.PlatformOS, rec.PlatformArch, rec.InputSHA256, rec.PackageName, rec.InstallPrefix,
	)
	if err != nil {
		t.Fatalf("IsPackagePromoted() error: %v", err)
	}
	if promoted {
		t.Fatal("IsPackagePromoted() = true, want false before promotion")
	}

	now := time.Now()
	rec.Promoted = true
	rec.PromotedAt = &now
	rec.ReleaseTag = "nodejs-multi-test"
	if err := db.UpsertPackageRecord(rec); err != nil {
		t.Fatalf("UpsertPackageRecord() update error: %v", err)
	}

	promoted, err = db.IsPackagePromoted(
		rec.Runtime, rec.Version, rec.PackageType, rec.PlatformOS, rec.PlatformArch, rec.InputSHA256, rec.PackageName, rec.InstallPrefix,
	)
	if err != nil {
		t.Fatalf("IsPackagePromoted() error after update: %v", err)
	}
	if !promoted {
		t.Fatal("IsPackagePromoted() = false, want true after promotion")
	}
}
