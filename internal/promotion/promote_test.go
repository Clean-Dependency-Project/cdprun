package promotion

import (
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/clean-dependency-project/cdprun/internal/storage"
)

func TestPromoteTestedPackages_SuccessAndIdempotent(t *testing.T) {
	db, cleanup := createTestDB(t)
	defer cleanup()

	if err := seedRelease(t, db, "nodejs", "22.22.0", "nodejs-v22.22.0-test"); err != nil {
		t.Fatalf("seedRelease() error: %v", err)
	}

	targets := []Target{
		{
			Runtime:       "nodejs",
			Version:       "22.22.0",
			Target:        "rpm",
			InputPlatform: "linux",
			InputArch:     "x64",
			InputSHA256:   "in-sha-1",
			PackageName:   "OSPO-nodejs",
			InstallPrefix: "/export/apps/citools/OSPO-nodejs/22.22.0",
			Tested:        true,
		},
	}
	results := TestResultsFile{
		RunID: "run-1",
		Results: []TestResult{
			{
				Runtime:         "nodejs",
				Version:         "22.22.0",
				Target:          "rpm",
				InputSHA256:     "in-sha-1",
				PackageName:     "OSPO-nodejs",
				InstallPrefix:   "/export/apps/citools/OSPO-nodejs/22.22.0",
				Passed:          true,
				PackageFilename: "OSPO-nodejs-22.22.0-1.x86_64.rpm",
				PackageSHA256:   "pkg-sha-1",
				PackageSize:     2048,
				PackageURL:      "https://example.com/OSPO-nodejs-22.22.0-1.x86_64.rpm",
			},
		},
	}

	summary, err := PromoteTestedPackages(db, nil, "run-1", targets, results)
	if err != nil {
		t.Fatalf("PromoteTestedPackages() error: %v", err)
	}
	if summary.EligibleEntries != 1 || summary.PromotedEntries != 1 || summary.BlockedEntries != 0 {
		t.Fatalf("unexpected summary after first run: %+v", summary)
	}

	summary, err = PromoteTestedPackages(db, nil, "run-1", targets, results)
	if err != nil {
		t.Fatalf("PromoteTestedPackages() second run error: %v", err)
	}
	if summary.PromotedEntries != 1 || summary.BlockedEntries != 0 {
		t.Fatalf("unexpected summary after second run: %+v", summary)
	}

	release, err := db.GetRelease("nodejs", "22.22.0")
	if err != nil {
		t.Fatalf("GetRelease() error: %v", err)
	}
	var artifacts storage.ReleaseArtifacts
	if err := json.Unmarshal([]byte(release.Artifacts), &artifacts); err != nil {
		t.Fatalf("unmarshal release artifacts: %v", err)
	}
	if len(artifacts.Platforms) != 1 {
		t.Fatalf("platform artifacts len = %d, want 1", len(artifacts.Platforms))
	}
	if artifacts.Platforms[0].Binary == nil || artifacts.Platforms[0].Binary.Filename != "OSPO-nodejs-22.22.0-1.x86_64.rpm" {
		t.Fatalf("unexpected promoted artifact: %+v", artifacts.Platforms[0].Binary)
	}

	record, err := db.GetPackageRecord(
		"nodejs", "22.22.0", "rpm", "linux", "x64", "in-sha-1", "OSPO-nodejs", "/export/apps/citools/OSPO-nodejs/22.22.0",
	)
	if err != nil {
		t.Fatalf("GetPackageRecord() error: %v", err)
	}
	if !record.Promoted || record.TestStatus != "success" || record.BuildStatus != "success" {
		t.Fatalf("unexpected package record after promotion: %+v", record)
	}
	if record.PromotedAt == nil {
		t.Fatal("PromotedAt = nil, want non-nil")
	}
}

func TestPromoteTestedPackages_BlockedOnMissingOrFailedResults_NoMutation(t *testing.T) {
	tests := []struct {
		name      string
		results   TestResultsFile
		errReason BlockedReason
	}{
		{
			name: "missing test result",
			results: TestResultsFile{
				RunID:   "run-1",
				Results: []TestResult{},
			},
			errReason: BlockedReasonMissingTestResult,
		},
		{
			name: "failed test result",
			results: TestResultsFile{
				RunID: "run-1",
				Results: []TestResult{
					{
						Runtime:       "nodejs",
						Version:       "22.22.0",
						Target:        "rpm",
						InputSHA256:   "in-sha-2",
						PackageName:   "OSPO-nodejs",
						InstallPrefix: "/export/apps/citools/OSPO-nodejs/22.22.0",
						Passed:        false,
					},
				},
			},
			errReason: BlockedReasonTestFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, cleanup := createTestDB(t)
			defer cleanup()
			if err := seedRelease(t, db, "nodejs", "22.22.0", "nodejs-v22.22.0-test"); err != nil {
				t.Fatalf("seedRelease() error: %v", err)
			}

			targets := []Target{
				{
					Runtime:       "nodejs",
					Version:       "22.22.0",
					Target:        "rpm",
					InputPlatform: "linux",
					InputArch:     "x64",
					InputSHA256:   "in-sha-2",
					PackageName:   "OSPO-nodejs",
					InstallPrefix: "/export/apps/citools/OSPO-nodejs/22.22.0",
					Tested:        true,
				},
			}

			summary, err := PromoteTestedPackages(db, nil, "run-1", targets, tt.results)
			if !errors.Is(err, ErrBlocked) {
				t.Fatalf("PromoteTestedPackages() error = %v, want ErrBlocked", err)
			}
			if summary.BlockedEntries != 1 || len(summary.Blocked) != 1 {
				t.Fatalf("blocked summary = %+v, want 1 blocked entry", summary)
			}
			if summary.Blocked[0].Reason != tt.errReason {
				t.Fatalf("blocked reason = %q, want %q", summary.Blocked[0].Reason, tt.errReason)
			}

			release, err := db.GetRelease("nodejs", "22.22.0")
			if err != nil {
				t.Fatalf("GetRelease() error: %v", err)
			}
			var artifacts storage.ReleaseArtifacts
			if err := json.Unmarshal([]byte(release.Artifacts), &artifacts); err != nil {
				t.Fatalf("unmarshal release artifacts: %v", err)
			}
			if len(artifacts.Platforms) != 0 {
				t.Fatalf("platform artifacts len = %d, want 0 on blocked promotion", len(artifacts.Platforms))
			}
		})
	}
}

func TestPromoteTestedPackages_BlockedOnMissingRelease_NoMutation(t *testing.T) {
	db, cleanup := createTestDB(t)
	defer cleanup()

	targets := []Target{
		{
			Runtime:       "nodejs",
			Version:       "22.22.0",
			Target:        "rpm",
			InputPlatform: "linux",
			InputArch:     "x64",
			InputSHA256:   "in-sha-3",
			PackageName:   "OSPO-nodejs",
			InstallPrefix: "/export/apps/citools/OSPO-nodejs/22.22.0",
			Tested:        true,
		},
	}
	results := TestResultsFile{
		RunID: "run-1",
		Results: []TestResult{
			{
				Runtime:         "nodejs",
				Version:         "22.22.0",
				Target:          "rpm",
				InputSHA256:     "in-sha-3",
				PackageName:     "OSPO-nodejs",
				InstallPrefix:   "/export/apps/citools/OSPO-nodejs/22.22.0",
				Passed:          true,
				PackageFilename: "OSPO-nodejs-22.22.0-1.x86_64.rpm",
				PackageSHA256:   "pkg-sha-3",
			},
		},
	}

	summary, err := PromoteTestedPackages(db, nil, "run-1", targets, results)
	if !errors.Is(err, ErrBlocked) {
		t.Fatalf("PromoteTestedPackages() error = %v, want ErrBlocked", err)
	}
	if summary.BlockedEntries != 1 || summary.Blocked[0].Reason != BlockedReasonReleaseNotFound {
		t.Fatalf("unexpected blocked summary: %+v", summary)
	}
}

func TestPromoteTestedPackages_ResolvesURLFromReleaseArtifactsByPackagePath(t *testing.T) {
	db, cleanup := createTestDB(t)
	defer cleanup()
	if err := seedRelease(t, db, "nodejs", "22.22.0", "nodejs-v22.22.0-test"); err != nil {
		t.Fatalf("seedRelease() error: %v", err)
	}
	if err := db.AppendOrMergePlatformArtifact("nodejs", "22.22.0", storage.PlatformArtifact{
		Platform:     "linux-x64-rpm",
		PlatformOS:   "linux",
		PlatformArch: "x64",
		Binary: &storage.ArtifactFile{
			Filename: "OSPO-nodejs-22.22.0-1.x86_64.rpm",
			SHA256:   "pkg-sha-4",
			Size:     1024,
			URL:      "https://example.com/releases/download/nodejs-v22.22.0-test/OSPO-nodejs-22.22.0-1.x86_64.rpm",
		},
	}); err != nil {
		t.Fatalf("AppendOrMergePlatformArtifact() error: %v", err)
	}

	targets := []Target{
		{
			Runtime:       "nodejs",
			Version:       "22.22.0",
			Target:        "rpm",
			InputPlatform: "linux",
			InputArch:     "x64",
			InputSHA256:   "in-sha-4",
			PackageName:   "OSPO-nodejs",
			InstallPrefix: "/export/apps/citools/OSPO-nodejs/22.22.0",
			Tested:        true,
		},
	}
	results := TestResultsFile{
		RunID: "run-1",
		Results: []TestResult{
			{
				Runtime:       "nodejs",
				Version:       "22.22.0",
				Target:        "rpm",
				InputSHA256:   "in-sha-4",
				PackageName:   "OSPO-nodejs",
				InstallPrefix: "/export/apps/citools/OSPO-nodejs/22.22.0",
				Passed:        true,
				PackagePath:   "/workspace/packages/OSPO-nodejs-22.22.0-1.x86_64.rpm",
				PackageSHA256: "pkg-sha-4",
				PackageSize:   1024,
			},
		},
	}

	summary, err := PromoteTestedPackages(db, nil, "run-1", targets, results)
	if err != nil {
		t.Fatalf("PromoteTestedPackages() error: %v", err)
	}
	if summary.PromotedEntries != 1 || summary.BlockedEntries != 0 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
}

func TestPromoteTestedPackages_UploadsPackageAssetWhenURLMissing(t *testing.T) {
	db, cleanup := createTestDB(t)
	defer cleanup()
	if err := seedRelease(t, db, "nodejs", "22.22.0", "nodejs-v22.22.0-test"); err != nil {
		t.Fatalf("seedRelease() error: %v", err)
	}

	targets := []Target{{
		Runtime:       "nodejs",
		Version:       "22.22.0",
		Target:        "rpm",
		InputPlatform: "linux",
		InputArch:     "x64",
		InputSHA256:   "in-sha-upload",
		PackageName:   "OSPO-nodejs",
		InstallPrefix: "/export/apps/citools/OSPO-nodejs/22.22.0",
		Tested:        true,
	}}
	results := TestResultsFile{
		RunID: "run-1",
		Results: []TestResult{{
			Runtime:         "nodejs",
			Version:         "22.22.0",
			Target:          "rpm",
			InputSHA256:     "in-sha-upload",
			PackageName:     "OSPO-nodejs",
			InstallPrefix:   "/export/apps/citools/OSPO-nodejs/22.22.0",
			Passed:          true,
			PackagePath:     "packages/OSPO-nodejs-22.22.0-1.x86_64.rpm",
			PackageFilename: "OSPO-nodejs-22.22.0-1.x86_64.rpm",
			PackageSHA256:   "pkg-sha-upload",
			PackageSize:     4096,
		}},
	}

	uploader := &mockReleaseAssetUploader{
		uploadFn: func(runtime, releaseTag, packagePath, uploadFilename string) (string, error) {
			if runtime != "nodejs" || releaseTag != "nodejs-v22.22.0-test" {
				t.Fatalf("unexpected uploader context: runtime=%q releaseTag=%q", runtime, releaseTag)
			}
			if uploadFilename != "OSPO-nodejs-22.22.0-1.x86_64.rpm" {
				t.Fatalf("unexpected upload filename: %q", uploadFilename)
			}
			return "https://example.com/releases/download/nodejs-v22.22.0-test/OSPO-nodejs-22.22.0-1.x86_64.rpm", nil
		},
	}

	summary, err := PromoteTestedPackages(db, uploader, "run-1", targets, results)
	if err != nil {
		t.Fatalf("PromoteTestedPackages() error: %v", err)
	}
	if summary.PromotedEntries != 1 || summary.BlockedEntries != 0 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if uploader.calls != 1 {
		t.Fatalf("uploader calls = %d, want 1", uploader.calls)
	}
}

func TestPromoteTestedPackages_UploadFailureReturnsError(t *testing.T) {
	db, cleanup := createTestDB(t)
	defer cleanup()
	if err := seedRelease(t, db, "nodejs", "22.22.0", "nodejs-v22.22.0-test"); err != nil {
		t.Fatalf("seedRelease() error: %v", err)
	}

	targets := []Target{{
		Runtime:       "nodejs",
		Version:       "22.22.0",
		Target:        "rpm",
		InputPlatform: "linux",
		InputArch:     "x64",
		InputSHA256:   "in-sha-upload-fail",
		PackageName:   "OSPO-nodejs",
		InstallPrefix: "/export/apps/citools/OSPO-nodejs/22.22.0",
		Tested:        true,
	}}
	results := TestResultsFile{
		RunID: "run-1",
		Results: []TestResult{{
			Runtime:         "nodejs",
			Version:         "22.22.0",
			Target:          "rpm",
			InputSHA256:     "in-sha-upload-fail",
			PackageName:     "OSPO-nodejs",
			InstallPrefix:   "/export/apps/citools/OSPO-nodejs/22.22.0",
			Passed:          true,
			PackagePath:     "packages/OSPO-nodejs-22.22.0-1.x86_64.rpm",
			PackageFilename: "OSPO-nodejs-22.22.0-1.x86_64.rpm",
			PackageSHA256:   "pkg-sha-upload-fail",
			PackageSize:     4096,
		}},
	}

	uploader := &mockReleaseAssetUploader{
		uploadFn: func(runtime, releaseTag, packagePath, uploadFilename string) (string, error) {
			return "", errors.New("upload failed")
		},
	}

	if _, err := PromoteTestedPackages(db, uploader, "run-1", targets, results); err == nil {
		t.Fatal("PromoteTestedPackages() error = nil, want non-nil")
	}
}

type mockReleaseAssetUploader struct {
	uploadFn func(runtime, releaseTag, packagePath, uploadFilename string) (string, error)
	calls    int
}

func (m *mockReleaseAssetUploader) UploadPackageAsset(runtime, releaseTag, packagePath, uploadFilename string) (string, error) {
	m.calls++
	if m.uploadFn == nil {
		return "", nil
	}
	return m.uploadFn(runtime, releaseTag, packagePath, uploadFilename)
}

func createTestDB(t *testing.T) (*storage.DB, func()) {
	t.Helper()

	tmpfile, err := os.CreateTemp("", "test-promotion-*.db")
	if err != nil {
		t.Fatalf("create temp db file: %v", err)
	}
	dbPath := tmpfile.Name()
	_ = tmpfile.Close()

	db, err := storage.InitDB(storage.Config{
		DatabasePath: dbPath,
		LogLevel:     "silent",
	})
	if err != nil {
		_ = os.Remove(dbPath)
		t.Fatalf("initialize test database: %v", err)
	}

	cleanup := func() {
		_ = db.Close()
		_ = os.Remove(dbPath)
	}
	return db, cleanup
}

func seedRelease(t *testing.T, db *storage.DB, runtime, version, releaseTag string) error {
	t.Helper()
	return db.CreateRelease(&storage.Release{
		Runtime:     runtime,
		Version:     version,
		SemverMajor: 22,
		SemverMinor: 22,
		SemverPatch: 0,
		ReleaseTag:  releaseTag,
		ReleaseURL:  "https://example.com/releases/" + releaseTag,
		Artifacts:   `{"platforms":[],"common_files":[{"type":"checksum_file","filename":"SHASUMS256.txt","size":10,"url":"https://example.com/SHASUMS256.txt","uploaded_at":"2026-01-01T00:00:00Z"}],"metadata":{}}`,
		CreatedAt:   time.Now().UTC(),
	})
}
