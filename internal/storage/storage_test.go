package storage

import (
	"testing"
	"time"
)

// newTestDB creates an in-memory SQLite database for testing
func newTestDB(t *testing.T) *DB {
	t.Helper()

	db, err := InitDB(Config{
		DatabasePath: ":memory:",
		LogLevel:     "silent",
	})
	if err != nil {
		t.Fatalf("failed to create test database: %v", err)
	}

	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("failed to close test database: %v", err)
		}
	})

	return db
}

// createTestDownload creates a Download with default test values
func createTestDownload(runtime, version, platform, arch string) *Download {
	return &Download{
		Runtime:            runtime,
		Version:            version,
		VersionMajor:       18,
		VersionMinor:       20,
		VersionPatch:       8,
		Platform:           platform,
		Architecture:       arch,
		Filename:           "node-v18.20.8-darwin-arm64.tar.gz",
		FileExtension:      ".tar.gz",
		FileSize:           40123456,
		SourceURL:          "https://nodejs.org/dist/v18.20.8/node-v18.20.8-darwin-arm64.tar.gz",
		DownloadedAt:       time.Now(),
		ChecksumVerified:   true,
		ChecksumAlgorithm:  "sha256",
		ChecksumValue:      "abc123def456",
		ChecksumSourceURL:  "https://nodejs.org/dist/v18.20.8/SHASUMS256.txt",
		GPGVerified:        false,
		VerificationStatus: "success",
		VerificationType:   "checksum",
	}
}

// seedTestData populates the database with test data
func seedTestData(t *testing.T, db *DB) []*Download {
	t.Helper()

	downloads := []*Download{
		createTestDownload("nodejs", "18.20.8", "darwin", "arm64"),
		{
			Runtime:            "nodejs",
			Version:            "20.10.0",
			VersionMajor:       20,
			VersionMinor:       10,
			VersionPatch:       0,
			Platform:           "linux",
			Architecture:       "x64",
			Filename:           "node-v20.10.0-linux-x64.tar.gz",
			FileExtension:      ".tar.gz",
			FileSize:           45123456,
			SourceURL:          "https://nodejs.org/dist/v20.10.0/node-v20.10.0-linux-x64.tar.gz",
			DownloadedAt:       time.Now().Add(-1 * time.Hour),
			ChecksumVerified:   true,
			ChecksumAlgorithm:  "sha256",
			ChecksumValue:      "def789ghi012",
			ChecksumSourceURL:  "https://nodejs.org/dist/v20.10.0/SHASUMS256.txt",
			GPGVerified:        true,
			GPGSignatureURL:    "https://nodejs.org/dist/v20.10.0/SHASUMS256.txt.sig",
			GPGKeyringSource:   "embedded",
			VerificationStatus: "success",
			VerificationType:   "gpg",
		},
		{
			Runtime:            "python",
			Version:            "3.11.5",
			VersionMajor:       3,
			VersionMinor:       11,
			VersionPatch:       5,
			Platform:           "darwin",
			Architecture:       "arm64",
			Filename:           "python-3.11.5-macos-arm64.pkg",
			FileExtension:      ".pkg",
			FileSize:           35000000,
			SourceURL:          "https://www.python.org/ftp/python/3.11.5/python-3.11.5-macos-arm64.pkg",
			DownloadedAt:       time.Now().Add(-2 * time.Hour),
			ChecksumVerified:   false,
			VerificationStatus: "pending",
		},
	}

	for _, dl := range downloads {
		if err := db.RecordDownload(dl); err != nil {
			t.Fatalf("failed to seed test data: %v", err)
		}
	}

	return downloads
}

// TestInitDB tests database initialization
func TestInitDB(t *testing.T) {
	tests := []struct {
		name      string
		config    Config
		wantError bool
	}{
		{
			name: "in-memory database",
			config: Config{
				DatabasePath: ":memory:",
				LogLevel:     "silent",
			},
			wantError: false,
		},
		{
			name: "in-memory with error log level",
			config: Config{
				DatabasePath: ":memory:",
				LogLevel:     "error",
			},
			wantError: false,
		},
		{
			name: "in-memory with warn log level",
			config: Config{
				DatabasePath: ":memory:",
				LogLevel:     "warn",
			},
			wantError: false,
		},
		{
			name: "in-memory with info log level",
			config: Config{
				DatabasePath: ":memory:",
				LogLevel:     "info",
			},
			wantError: false,
		},
		{
			name: "in-memory with unknown log level defaults to silent",
			config: Config{
				DatabasePath: ":memory:",
				LogLevel:     "unknown",
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, err := InitDB(tt.config)
			if (err != nil) != tt.wantError {
				t.Errorf("InitDB() error = %v, wantError %v", err, tt.wantError)
				return
			}
			if !tt.wantError && db == nil {
				t.Error("InitDB() returned nil DB without error")
				return
			}
			if db != nil {
				if err := db.Close(); err != nil {
					t.Errorf("failed to close database: %v", err)
				}
			}
		})
	}
}

// TestClose tests closing database connections
func TestClose(t *testing.T) {
	t.Run("close active connection", func(t *testing.T) {
		db, err := InitDB(Config{
			DatabasePath: ":memory:",
			LogLevel:     "silent",
		})
		if err != nil {
			t.Fatalf("InitDB() failed: %v", err)
		}

		if err := db.Close(); err != nil {
			t.Errorf("Close() error = %v, want nil", err)
		}
	})

	t.Run("close already closed connection", func(t *testing.T) {
		db, err := InitDB(Config{
			DatabasePath: ":memory:",
			LogLevel:     "silent",
		})
		if err != nil {
			t.Fatalf("InitDB() failed: %v", err)
		}

		// Close once
		if err := db.Close(); err != nil {
			t.Errorf("First Close() error = %v, want nil", err)
		}

		// Close again - SQLite may or may not return an error
		// This test just ensures it doesn't panic
		_ = db.Close()
	})
}

// TestRecordDownload tests recording download entries
func TestRecordDownload(t *testing.T) {
	tests := []struct {
		name      string
		download  *Download
		wantError bool
		errorMsg  string
	}{
		{
			name:      "successful insert",
			download:  createTestDownload("nodejs", "18.20.8", "darwin", "arm64"),
			wantError: false,
		},
		{
			name:      "nil download",
			download:  nil,
			wantError: true,
			errorMsg:  "download cannot be nil",
		},
		{
			name: "duplicate download should fail",
			download: &Download{
				Runtime:            "nodejs",
				Version:            "18.20.8",
				Platform:           "darwin",
				Architecture:       "arm64",
				Filename:           "duplicate.tar.gz",
				SourceURL:          "https://example.com/duplicate.tar.gz",
				DownloadedAt:       time.Now(),
				VerificationStatus: "success",
			},
			wantError: true, // Will fail on second insert due to unique constraint
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := newTestDB(t)

			// For duplicate test, insert once first
			if tt.name == "duplicate download should fail" {
				firstDownload := createTestDownload("nodejs", "18.20.8", "darwin", "arm64")
				if err := db.RecordDownload(firstDownload); err != nil {
					t.Fatalf("failed to insert first download: %v", err)
				}
			}

			err := db.RecordDownload(tt.download)
			if (err != nil) != tt.wantError {
				t.Errorf("RecordDownload() error = %v, wantError %v", err, tt.wantError)
				return
			}

			if tt.wantError && tt.errorMsg != "" && err != nil {
				if err.Error() != tt.errorMsg {
					t.Errorf("RecordDownload() error message = %q, want %q", err.Error(), tt.errorMsg)
				}
			}

			// Verify download was recorded
			if !tt.wantError && tt.download != nil {
				if tt.download.ID == 0 {
					t.Error("RecordDownload() did not set ID")
				}
				if tt.download.CreatedAt.IsZero() {
					t.Error("RecordDownload() did not set CreatedAt")
				}
			}
		})
	}
}

// TestGetDownload tests retrieving download entries
func TestGetDownload(t *testing.T) {
	tests := []struct {
		name      string
		runtime   string
		version   string
		platform  string
		arch      string
		wantFound bool
		wantError bool
		setupFunc func(*testing.T, *DB)
	}{
		{
			name:      "find existing download",
			runtime:   "nodejs",
			version:   "18.20.8",
			platform:  "darwin",
			arch:      "arm64",
			wantFound: true,
			wantError: false,
			setupFunc: func(t *testing.T, db *DB) {
				dl := createTestDownload("nodejs", "18.20.8", "darwin", "arm64")
				if err := db.RecordDownload(dl); err != nil {
					t.Fatalf("setup failed: %v", err)
				}
			},
		},
		{
			name:      "download not found",
			runtime:   "nodejs",
			version:   "99.99.99",
			platform:  "darwin",
			arch:      "arm64",
			wantFound: false,
			wantError: true,
			setupFunc: nil,
		},
		{
			name:      "different platform returns ErrNotFound",
			runtime:   "nodejs",
			version:   "18.20.8",
			platform:  "linux",
			arch:      "x64",
			wantFound: false,
			wantError: true,
			setupFunc: func(t *testing.T, db *DB) {
				dl := createTestDownload("nodejs", "18.20.8", "darwin", "arm64")
				if err := db.RecordDownload(dl); err != nil {
					t.Fatalf("setup failed: %v", err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := newTestDB(t)

			if tt.setupFunc != nil {
				tt.setupFunc(t, db)
			}

			got, err := db.GetDownload(tt.runtime, tt.version, tt.platform, tt.arch)
			if (err != nil) != tt.wantError {
				t.Errorf("GetDownload() error = %v, wantError %v", err, tt.wantError)
				return
			}

			if tt.wantFound && got == nil {
				t.Error("GetDownload() returned nil, expected download")
				return
			}

			if !tt.wantFound && got != nil {
				t.Errorf("GetDownload() returned download, expected nil")
				return
			}

			if tt.wantFound && got != nil {
				if got.Runtime != tt.runtime {
					t.Errorf("Runtime = %q, want %q", got.Runtime, tt.runtime)
				}
				if got.Version != tt.version {
					t.Errorf("Version = %q, want %q", got.Version, tt.version)
				}
			}
		})
	}
}

// TestUpdateVerification tests updating verification status
func TestUpdateVerification(t *testing.T) {
	tests := []struct {
		name             string
		checksumVerified bool
		gpgVerified      bool
		status           string
		errorMsg         string
		wantError        bool
	}{
		{
			name:             "update to success",
			checksumVerified: true,
			gpgVerified:      true,
			status:           "success",
			errorMsg:         "",
			wantError:        false,
		},
		{
			name:             "update to failed with error message",
			checksumVerified: false,
			gpgVerified:      false,
			status:           "failed",
			errorMsg:         "checksum mismatch",
			wantError:        false,
		},
		{
			name:             "partial verification",
			checksumVerified: true,
			gpgVerified:      false,
			status:           "partial",
			errorMsg:         "",
			wantError:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := newTestDB(t)

			// Setup: create a download
			dl := createTestDownload("nodejs", "18.20.8", "darwin", "arm64")
			if err := db.RecordDownload(dl); err != nil {
				t.Fatalf("setup failed: %v", err)
			}

			err := db.UpdateVerification(dl.ID, tt.checksumVerified, tt.gpgVerified, tt.status, tt.errorMsg)
			if (err != nil) != tt.wantError {
				t.Errorf("UpdateVerification() error = %v, wantError %v", err, tt.wantError)
				return
			}

			// Verify the update
			updated, err := db.GetDownload("nodejs", "18.20.8", "darwin", "arm64")
			if err != nil {
				t.Fatalf("GetDownload() failed: %v", err)
			}

			if updated.ChecksumVerified != tt.checksumVerified {
				t.Errorf("ChecksumVerified = %v, want %v", updated.ChecksumVerified, tt.checksumVerified)
			}
			if updated.GPGVerified != tt.gpgVerified {
				t.Errorf("GPGVerified = %v, want %v", updated.GPGVerified, tt.gpgVerified)
			}
			if updated.VerificationStatus != tt.status {
				t.Errorf("VerificationStatus = %q, want %q", updated.VerificationStatus, tt.status)
			}
			if tt.errorMsg != "" && updated.ErrorMessage != tt.errorMsg {
				t.Errorf("ErrorMessage = %q, want %q", updated.ErrorMessage, tt.errorMsg)
			}
		})
	}

	t.Run("update non-existent download", func(t *testing.T) {
		db := newTestDB(t)

		// Try to update a non-existent ID
		err := db.UpdateVerification(99999, true, true, "success", "")
		// GORM will not return an error for updating non-existent records
		// but we can verify no records were affected
		if err != nil {
			t.Errorf("UpdateVerification() unexpected error = %v", err)
		}
	})
}

// TestUpdateChecksumVerification tests updating checksum verification fields
func TestUpdateChecksumVerification(t *testing.T) {
	db := newTestDB(t)

	// Setup: create a download
	dl := createTestDownload("nodejs", "18.20.8", "darwin", "arm64")
	if err := db.RecordDownload(dl); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	tests := []struct {
		name      string
		verified  bool
		algorithm string
		value     string
		sourceURL string
	}{
		{
			name:      "update checksum verification",
			verified:  true,
			algorithm: "sha512",
			value:     "new-checksum-value",
			sourceURL: "https://example.com/new-checksums.txt",
		},
		{
			name:      "mark as not verified",
			verified:  false,
			algorithm: "",
			value:     "",
			sourceURL: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := db.UpdateChecksumVerification(dl.ID, tt.verified, tt.algorithm, tt.value, tt.sourceURL)
			if err != nil {
				t.Errorf("UpdateChecksumVerification() error = %v", err)
				return
			}

			// Verify the update
			updated, err := db.GetDownload("nodejs", "18.20.8", "darwin", "arm64")
			if err != nil {
				t.Fatalf("GetDownload() failed: %v", err)
			}

			if updated.ChecksumVerified != tt.verified {
				t.Errorf("ChecksumVerified = %v, want %v", updated.ChecksumVerified, tt.verified)
			}
			if updated.ChecksumAlgorithm != tt.algorithm {
				t.Errorf("ChecksumAlgorithm = %q, want %q", updated.ChecksumAlgorithm, tt.algorithm)
			}
			if updated.ChecksumValue != tt.value {
				t.Errorf("ChecksumValue = %q, want %q", updated.ChecksumValue, tt.value)
			}
			if updated.ChecksumSourceURL != tt.sourceURL {
				t.Errorf("ChecksumSourceURL = %q, want %q", updated.ChecksumSourceURL, tt.sourceURL)
			}
		})
	}
}

// TestUpdateGPGVerification tests updating GPG verification fields
func TestUpdateGPGVerification(t *testing.T) {
	db := newTestDB(t)

	// Setup: create a download
	dl := createTestDownload("nodejs", "18.20.8", "darwin", "arm64")
	if err := db.RecordDownload(dl); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	tests := []struct {
		name          string
		verified      bool
		signatureURL  string
		keyringSource string
	}{
		{
			name:          "update GPG verification",
			verified:      true,
			signatureURL:  "https://example.com/signature.asc",
			keyringSource: "keyserver",
		},
		{
			name:          "mark as not verified",
			verified:      false,
			signatureURL:  "",
			keyringSource: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := db.UpdateGPGVerification(dl.ID, tt.verified, tt.signatureURL, tt.keyringSource)
			if err != nil {
				t.Errorf("UpdateGPGVerification() error = %v", err)
				return
			}

			// Verify the update
			updated, err := db.GetDownload("nodejs", "18.20.8", "darwin", "arm64")
			if err != nil {
				t.Fatalf("GetDownload() failed: %v", err)
			}

			if updated.GPGVerified != tt.verified {
				t.Errorf("GPGVerified = %v, want %v", updated.GPGVerified, tt.verified)
			}
			if updated.GPGSignatureURL != tt.signatureURL {
				t.Errorf("GPGSignatureURL = %q, want %q", updated.GPGSignatureURL, tt.signatureURL)
			}
			if updated.GPGKeyringSource != tt.keyringSource {
				t.Errorf("GPGKeyringSource = %q, want %q", updated.GPGKeyringSource, tt.keyringSource)
			}
		})
	}
}

// TestIsAlreadyDownloaded tests checking if downloads are already completed
func TestIsAlreadyDownloaded(t *testing.T) {
	tests := []struct {
		name      string
		runtime   string
		version   string
		platform  string
		arch      string
		want      bool
		setupFunc func(*testing.T, *DB)
	}{
		{
			name:     "successfully verified download exists",
			runtime:  "nodejs",
			version:  "18.20.8",
			platform: "darwin",
			arch:     "arm64",
			want:     true,
			setupFunc: func(t *testing.T, db *DB) {
				dl := createTestDownload("nodejs", "18.20.8", "darwin", "arm64")
				dl.VerificationStatus = "success"
				if err := db.RecordDownload(dl); err != nil {
					t.Fatalf("setup failed: %v", err)
				}
			},
		},
		{
			name:     "pending download not counted",
			runtime:  "nodejs",
			version:  "18.20.8",
			platform: "darwin",
			arch:     "arm64",
			want:     false,
			setupFunc: func(t *testing.T, db *DB) {
				dl := createTestDownload("nodejs", "18.20.8", "darwin", "arm64")
				dl.VerificationStatus = "pending"
				if err := db.RecordDownload(dl); err != nil {
					t.Fatalf("setup failed: %v", err)
				}
			},
		},
		{
			name:     "failed download not counted",
			runtime:  "nodejs",
			version:  "18.20.8",
			platform: "darwin",
			arch:     "arm64",
			want:     false,
			setupFunc: func(t *testing.T, db *DB) {
				dl := createTestDownload("nodejs", "18.20.8", "darwin", "arm64")
				dl.VerificationStatus = "failed"
				if err := db.RecordDownload(dl); err != nil {
					t.Fatalf("setup failed: %v", err)
				}
			},
		},
		{
			name:      "non-existent download",
			runtime:   "python",
			version:   "99.99.99",
			platform:  "darwin",
			arch:      "arm64",
			want:      false,
			setupFunc: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := newTestDB(t)

			if tt.setupFunc != nil {
				tt.setupFunc(t, db)
			}

			got, err := db.IsAlreadyDownloaded(tt.runtime, tt.version, tt.platform, tt.arch)
			if err != nil {
				t.Errorf("IsAlreadyDownloaded() error = %v", err)
				return
			}

			if got != tt.want {
				t.Errorf("IsAlreadyDownloaded() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestListAll tests listing all downloads
func TestListAll(t *testing.T) {
	t.Run("empty database", func(t *testing.T) {
		db := newTestDB(t)

		downloads, err := db.ListAll()
		if err != nil {
			t.Errorf("ListAll() error = %v", err)
			return
		}

		if len(downloads) != 0 {
			t.Errorf("ListAll() returned %d downloads, want 0", len(downloads))
		}
	})

	t.Run("multiple downloads", func(t *testing.T) {
		db := newTestDB(t)
		seedTestData(t, db)

		downloads, err := db.ListAll()
		if err != nil {
			t.Errorf("ListAll() error = %v", err)
			return
		}

		if len(downloads) != 3 {
			t.Errorf("ListAll() returned %d downloads, want 3", len(downloads))
		}

		// Verify ordering (DESC by downloaded_at)
		for i := 0; i < len(downloads)-1; i++ {
			if downloads[i].DownloadedAt.Before(downloads[i+1].DownloadedAt) {
				t.Error("ListAll() downloads not ordered by downloaded_at DESC")
				break
			}
		}
	})
}

// TestListByRuntime tests listing downloads by runtime
func TestListByRuntime(t *testing.T) {
	db := newTestDB(t)
	seedTestData(t, db)

	tests := []struct {
		name      string
		runtime   string
		wantCount int
	}{
		{
			name:      "nodejs downloads",
			runtime:   "nodejs",
			wantCount: 2,
		},
		{
			name:      "python downloads",
			runtime:   "python",
			wantCount: 1,
		},
		{
			name:      "non-existent runtime",
			runtime:   "ruby",
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			downloads, err := db.ListByRuntime(tt.runtime)
			if err != nil {
				t.Errorf("ListByRuntime() error = %v", err)
				return
			}

			if len(downloads) != tt.wantCount {
				t.Errorf("ListByRuntime() returned %d downloads, want %d", len(downloads), tt.wantCount)
			}

			// Verify all downloads match the runtime
			for _, dl := range downloads {
				if dl.Runtime != tt.runtime {
					t.Errorf("ListByRuntime() returned download with runtime %q, want %q", dl.Runtime, tt.runtime)
				}
			}
		})
	}
}

// TestListByVersion tests listing downloads by version
func TestListByVersion(t *testing.T) {
	db := newTestDB(t)
	seedTestData(t, db)

	tests := []struct {
		name      string
		runtime   string
		version   string
		wantCount int
	}{
		{
			name:      "nodejs 18.20.8",
			runtime:   "nodejs",
			version:   "18.20.8",
			wantCount: 1,
		},
		{
			name:      "nodejs 20.10.0",
			runtime:   "nodejs",
			version:   "20.10.0",
			wantCount: 1,
		},
		{
			name:      "non-existent version",
			runtime:   "nodejs",
			version:   "99.99.99",
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			downloads, err := db.ListByVersion(tt.runtime, tt.version)
			if err != nil {
				t.Errorf("ListByVersion() error = %v", err)
				return
			}

			if len(downloads) != tt.wantCount {
				t.Errorf("ListByVersion() returned %d downloads, want %d", len(downloads), tt.wantCount)
			}

			// Verify all downloads match the runtime and version
			for _, dl := range downloads {
				if dl.Runtime != tt.runtime || dl.Version != tt.version {
					t.Errorf("ListByVersion() returned download with %s@%s, want %s@%s",
						dl.Runtime, dl.Version, tt.runtime, tt.version)
				}
			}
		})
	}
}

// TestListByPlatform tests listing downloads by platform
func TestListByPlatform(t *testing.T) {
	db := newTestDB(t)
	seedTestData(t, db)

	tests := []struct {
		name      string
		platform  string
		arch      string
		wantCount int
	}{
		{
			name:      "darwin arm64",
			platform:  "darwin",
			arch:      "arm64",
			wantCount: 2,
		},
		{
			name:      "linux x64",
			platform:  "linux",
			arch:      "x64",
			wantCount: 1,
		},
		{
			name:      "non-existent platform",
			platform:  "windows",
			arch:      "x64",
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			downloads, err := db.ListByPlatform(tt.platform, tt.arch)
			if err != nil {
				t.Errorf("ListByPlatform() error = %v", err)
				return
			}

			if len(downloads) != tt.wantCount {
				t.Errorf("ListByPlatform() returned %d downloads, want %d", len(downloads), tt.wantCount)
			}

			// Verify all downloads match the platform and arch
			for _, dl := range downloads {
				if dl.Platform != tt.platform || dl.Architecture != tt.arch {
					t.Errorf("ListByPlatform() returned download with %s-%s, want %s-%s",
						dl.Platform, dl.Architecture, tt.platform, tt.arch)
				}
			}
		})
	}
}

// TestListByMajorVersion tests listing downloads by major version
func TestListByMajorVersion(t *testing.T) {
	db := newTestDB(t)
	seedTestData(t, db)

	tests := []struct {
		name         string
		runtime      string
		majorVersion int
		wantCount    int
	}{
		{
			name:         "nodejs v18",
			runtime:      "nodejs",
			majorVersion: 18,
			wantCount:    1,
		},
		{
			name:         "nodejs v20",
			runtime:      "nodejs",
			majorVersion: 20,
			wantCount:    1,
		},
		{
			name:         "python v3",
			runtime:      "python",
			majorVersion: 3,
			wantCount:    1,
		},
		{
			name:         "non-existent major version",
			runtime:      "nodejs",
			majorVersion: 99,
			wantCount:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			downloads, err := db.ListByMajorVersion(tt.runtime, tt.majorVersion)
			if err != nil {
				t.Errorf("ListByMajorVersion() error = %v", err)
				return
			}

			if len(downloads) != tt.wantCount {
				t.Errorf("ListByMajorVersion() returned %d downloads, want %d", len(downloads), tt.wantCount)
			}

			// Verify all downloads match the runtime and major version
			for _, dl := range downloads {
				if dl.Runtime != tt.runtime || dl.VersionMajor != tt.majorVersion {
					t.Errorf("ListByMajorVersion() returned download with %s v%d, want %s v%d",
						dl.Runtime, dl.VersionMajor, tt.runtime, tt.majorVersion)
				}
			}
		})
	}
}

// TestGetStats tests getting download statistics
func TestGetStats(t *testing.T) {
	t.Run("empty database", func(t *testing.T) {
		db := newTestDB(t)

		stats, err := db.GetStats()
		if err != nil {
			t.Errorf("GetStats() error = %v", err)
			return
		}

		total, ok := stats["total_downloads"].(int64)
		if !ok {
			t.Error("GetStats() total_downloads not an int64")
			return
		}

		if total != 0 {
			t.Errorf("GetStats() total_downloads = %d, want 0", total)
		}
	})

	t.Run("with data", func(t *testing.T) {
		db := newTestDB(t)
		seedTestData(t, db)

		stats, err := db.GetStats()
		if err != nil {
			t.Errorf("GetStats() error = %v", err)
			return
		}

		// Check total downloads
		total, ok := stats["total_downloads"].(int64)
		if !ok {
			t.Error("GetStats() total_downloads not an int64")
			return
		}
		if total != 3 {
			t.Errorf("GetStats() total_downloads = %d, want 3", total)
		}

		// Check by_runtime
		runtimeCounts, ok := stats["by_runtime"]
		if !ok {
			t.Error("GetStats() missing by_runtime")
			return
		}

		runtimeSlice, ok := runtimeCounts.([]struct {
			Runtime string
			Count   int64
		})
		if !ok {
			t.Error("GetStats() by_runtime has wrong type")
			return
		}

		if len(runtimeSlice) != 2 {
			t.Errorf("GetStats() by_runtime has %d entries, want 2", len(runtimeSlice))
		}

		// Check by_status
		statusCounts, ok := stats["by_status"]
		if !ok {
			t.Error("GetStats() missing by_status")
			return
		}

		statusSlice, ok := statusCounts.([]struct {
			Status string
			Count  int64
		})
		if !ok {
			t.Error("GetStats() by_status has wrong type")
		}

		if len(statusSlice) == 0 {
			t.Error("GetStats() by_status is empty")
		}
	})
}

// TestParseSemver tests semantic version parsing
func TestParseSemver(t *testing.T) {
	tests := []struct {
		name      string
		version   string
		wantMajor int
		wantMinor int
		wantPatch int
		wantError bool
	}{
		{
			name:      "valid version 1.2.3",
			version:   "1.2.3",
			wantMajor: 1,
			wantMinor: 2,
			wantPatch: 3,
			wantError: false,
		},
		{
			name:      "valid version 18.20.8",
			version:   "18.20.8",
			wantMajor: 18,
			wantMinor: 20,
			wantPatch: 8,
			wantError: false,
		},
		{
			name:      "valid version 0.0.0",
			version:   "0.0.0",
			wantMajor: 0,
			wantMinor: 0,
			wantPatch: 0,
			wantError: false,
		},
		{
			name:      "valid version 100.200.300",
			version:   "100.200.300",
			wantMajor: 100,
			wantMinor: 200,
			wantPatch: 300,
			wantError: false,
		},
		{
			name:      "invalid version - missing patch",
			version:   "1.2",
			wantMajor: 0,
			wantMinor: 0,
			wantPatch: 0,
			wantError: true,
		},
		{
			name:      "invalid version - extra part",
			version:   "1.2.3.4",
			wantMajor: 1,
			wantMinor: 2,
			wantPatch: 3,
			wantError: false, // Will parse first 3 parts successfully
		},
		{
			name:      "invalid version - not numbers",
			version:   "a.b.c",
			wantMajor: 0,
			wantMinor: 0,
			wantPatch: 0,
			wantError: true,
		},
		{
			name:      "invalid version - empty string",
			version:   "",
			wantMajor: 0,
			wantMinor: 0,
			wantPatch: 0,
			wantError: true,
		},
		{
			name:      "invalid version - with v prefix",
			version:   "v1.2.3",
			wantMajor: 0,
			wantMinor: 0,
			wantPatch: 0,
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			major, minor, patch, err := ParseSemver(tt.version)

			if (err != nil) != tt.wantError {
				t.Errorf("ParseSemver() error = %v, wantError %v", err, tt.wantError)
				return
			}

			if !tt.wantError {
				if major != tt.wantMajor {
					t.Errorf("major = %d, want %d", major, tt.wantMajor)
				}
				if minor != tt.wantMinor {
					t.Errorf("minor = %d, want %d", minor, tt.wantMinor)
				}
				if patch != tt.wantPatch {
					t.Errorf("patch = %d, want %d", patch, tt.wantPatch)
				}
			}
		})
	}
}

// TestExtractFilename tests filename extraction from paths
func TestExtractFilename(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "simple filename",
			path: "file.txt",
			want: "file.txt",
		},
		{
			name: "unix path",
			path: "/usr/local/bin/file.tar.gz",
			want: "file.tar.gz",
		},
		{
			name: "relative path",
			path: "./downloads/node-v18.20.8-darwin-arm64.tar.gz",
			want: "node-v18.20.8-darwin-arm64.tar.gz",
		},
		// Note: filepath.Base handles platform-specific separators
		// On Unix/Darwin, backslash is not a separator
		{
			name: "path ending with slash",
			path: "/usr/local/bin/",
			want: "bin",
		},
		{
			name: "empty path",
			path: "",
			want: ".",
		},
		{
			name: "just slash",
			path: "/",
			want: "/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractFilename(tt.path)
			if got != tt.want {
				t.Errorf("ExtractFilename() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestExtractExtension tests extension extraction from filenames
func TestExtractExtension(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		want     string
	}{
		{
			name:     "simple extension",
			filename: "file.txt",
			want:     ".txt",
		},
		{
			name:     "double extension",
			filename: "archive.tar.gz",
			want:     ".gz",
		},
		{
			name:     "no extension",
			filename: "README",
			want:     "",
		},
		{
			name:     "hidden file",
			filename: ".gitignore",
			want:     ".gitignore",
		},
		{
			name:     "hidden file with extension",
			filename: ".config.json",
			want:     ".json",
		},
		{
			name:     "multiple dots",
			filename: "file.backup.old.txt",
			want:     ".txt",
		},
		{
			name:     "empty string",
			filename: "",
			want:     "",
		},
		{
			name:     "path with extension",
			filename: "/path/to/file.exe",
			want:     ".exe",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractExtension(tt.filename)
			if got != tt.want {
				t.Errorf("ExtractExtension() = %q, want %q", got, tt.want)
			}
		})
	}
}
