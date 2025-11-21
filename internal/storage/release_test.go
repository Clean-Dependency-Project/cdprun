package storage

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestDB_CreateRelease(t *testing.T) {
	db, err := InitDB(Config{DatabasePath: ":memory:", LogLevel: "silent"})
	if err != nil {
		t.Fatalf("InitDB() error: %v", err)
	}
	defer func() { _ = db.Close() }()

	tests := []struct {
		name    string
		release *Release
		wantErr bool
		errType error
	}{
		{
			name: "valid release",
			release: &Release{
				Runtime:     "nodejs",
				Version:     "22.15.0",
				SemverMajor: 22,
				SemverMinor: 15,
				SemverPatch: 0,
				ReleaseTag:  "nodejs-v22.15.0-20251109T120000Z",
				ReleaseURL:  "https://github.com/owner/repo/releases/tag/nodejs-v22.15.0-20251109T120000Z",
				Artifacts:   `{"platforms":[]}`,
				CreatedAt:   time.Now(),
			},
			wantErr: false,
		},
		{
			name:    "nil release",
			release: nil,
			wantErr: true,
			errType: ErrNilRelease,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := db.CreateRelease(tt.release)
			if (err != nil) != tt.wantErr {
				t.Errorf("CreateRelease() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && tt.errType != nil && !errors.Is(err, tt.errType) {
				t.Errorf("CreateRelease() error = %v, want error type %v", err, tt.errType)
			}
		})
	}
}

func TestDB_GetRelease(t *testing.T) {
	db, err := InitDB(Config{DatabasePath: ":memory:", LogLevel: "silent"})
	if err != nil {
		t.Fatalf("InitDB() error: %v", err)
	}
	defer func() { _ = db.Close() }()

	// Insert test release
	testRelease := &Release{
		Runtime:     "nodejs",
		Version:     "22.15.0",
		SemverMajor: 22,
		SemverMinor: 15,
		SemverPatch: 0,
		ReleaseTag:  "nodejs-v22.15.0-20251109T120000Z",
		ReleaseURL:  "https://github.com/owner/repo/releases/tag/nodejs-v22.15.0-20251109T120000Z",
		Artifacts:   `{"platforms":[]}`,
		CreatedAt:   time.Now(),
	}
	if err := db.CreateRelease(testRelease); err != nil {
		t.Fatalf("CreateRelease() error: %v", err)
	}

	tests := []struct {
		name    string
		runtime string
		version string
		wantErr bool
		errType error
	}{
		{
			name:    "existing release",
			runtime: "nodejs",
			version: "22.15.0",
			wantErr: false,
		},
		{
			name:    "non-existent release",
			runtime: "nodejs",
			version: "99.99.99",
			wantErr: true,
			errType: ErrReleaseNotFound,
		},
		{
			name:    "empty runtime",
			runtime: "",
			version: "22.15.0",
			wantErr: true,
		},
		{
			name:    "empty version",
			runtime: "nodejs",
			version: "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			release, err := db.GetRelease(tt.runtime, tt.version)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetRelease() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				if tt.errType != nil && !errors.Is(err, tt.errType) {
					t.Errorf("GetRelease() error = %v, want error type %v", err, tt.errType)
				}
				return
			}
			if release.Runtime != tt.runtime {
				t.Errorf("GetRelease() runtime = %v, want %v", release.Runtime, tt.runtime)
			}
			if release.Version != tt.version {
				t.Errorf("GetRelease() version = %v, want %v", release.Version, tt.version)
			}
		})
	}
}

func TestDB_GetReleaseByTag(t *testing.T) {
	db, err := InitDB(Config{DatabasePath: ":memory:", LogLevel: "silent"})
	if err != nil {
		t.Fatalf("InitDB() error: %v", err)
	}
	defer func() { _ = db.Close() }()

	// Insert test release
	testTag := "nodejs-v22.15.0-20251109T120000Z"
	testRelease := &Release{
		Runtime:     "nodejs",
		Version:     "22.15.0",
		SemverMajor: 22,
		SemverMinor: 15,
		SemverPatch: 0,
		ReleaseTag:  testTag,
		ReleaseURL:  "https://github.com/owner/repo/releases/tag/" + testTag,
		Artifacts:   `{"platforms":[]}`,
		CreatedAt:   time.Now(),
	}
	if err := db.CreateRelease(testRelease); err != nil {
		t.Fatalf("CreateRelease() error: %v", err)
	}

	tests := []struct {
		name       string
		releaseTag string
		wantErr    bool
		errType    error
	}{
		{
			name:       "existing tag",
			releaseTag: testTag,
			wantErr:    false,
		},
		{
			name:       "non-existent tag",
			releaseTag: "nodejs-v99.99.99-20251109T120000Z",
			wantErr:    true,
			errType:    ErrReleaseNotFound,
		},
		{
			name:       "empty tag",
			releaseTag: "",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			release, err := db.GetReleaseByTag(tt.releaseTag)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetReleaseByTag() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				if tt.errType != nil && !errors.Is(err, tt.errType) {
					t.Errorf("GetReleaseByTag() error = %v, want error type %v", err, tt.errType)
				}
				return
			}
			if release.ReleaseTag != tt.releaseTag {
				t.Errorf("GetReleaseByTag() tag = %v, want %v", release.ReleaseTag, tt.releaseTag)
			}
		})
	}
}

func TestDB_GetReleasesByRuntime(t *testing.T) {
	db, err := InitDB(Config{DatabasePath: ":memory:", LogLevel: "silent"})
	if err != nil {
		t.Fatalf("InitDB() error: %v", err)
	}
	defer func() { _ = db.Close() }()

	// Insert multiple test releases
	releases := []Release{
		{
			Runtime:     "nodejs",
			Version:     "22.15.0",
			SemverMajor: 22,
			SemverMinor: 15,
			SemverPatch: 0,
			ReleaseTag:  "nodejs-v22.15.0-20251109T120000Z",
			ReleaseURL:  "https://github.com/owner/repo/releases/tag/nodejs-v22.15.0",
			Artifacts:   `{}`,
			CreatedAt:   time.Now(),
		},
		{
			Runtime:     "nodejs",
			Version:     "20.11.0",
			SemverMajor: 20,
			SemverMinor: 11,
			SemverPatch: 0,
			ReleaseTag:  "nodejs-v20.11.0-20251109T120000Z",
			ReleaseURL:  "https://github.com/owner/repo/releases/tag/nodejs-v20.11.0",
			Artifacts:   `{}`,
			CreatedAt:   time.Now().Add(-1 * time.Hour),
		},
		{
			Runtime:     "python",
			Version:     "3.14.0",
			SemverMajor: 3,
			SemverMinor: 14,
			SemverPatch: 0,
			ReleaseTag:  "python-v3.14.0-20251109T120000Z",
			ReleaseURL:  "https://github.com/owner/repo/releases/tag/python-v3.14.0",
			Artifacts:   `{}`,
			CreatedAt:   time.Now(),
		},
	}

	for _, release := range releases {
		if err := db.CreateRelease(&release); err != nil {
			t.Fatalf("CreateRelease() error: %v", err)
		}
	}

	tests := []struct {
		name      string
		runtime   string
		wantCount int
		wantErr   bool
	}{
		{
			name:      "nodejs releases",
			runtime:   "nodejs",
			wantCount: 2,
			wantErr:   false,
		},
		{
			name:      "python releases",
			runtime:   "python",
			wantCount: 1,
			wantErr:   false,
		},
		{
			name:      "non-existent runtime",
			runtime:   "ruby",
			wantCount: 0,
			wantErr:   false,
		},
		{
			name:    "empty runtime",
			runtime: "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			releases, err := db.GetReleasesByRuntime(tt.runtime)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetReleasesByRuntime() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}
			if len(releases) != tt.wantCount {
				t.Errorf("GetReleasesByRuntime() got %d releases, want %d", len(releases), tt.wantCount)
			}
		})
	}
}

func TestDB_GetAllReleases(t *testing.T) {
	db, err := InitDB(Config{DatabasePath: ":memory:", LogLevel: "silent"})
	if err != nil {
		t.Fatalf("InitDB() error: %v", err)
	}
	defer func() { _ = db.Close() }()

	// Insert test releases
	releases := []Release{
		{
			Runtime:     "nodejs",
			Version:     "22.15.0",
			SemverMajor: 22,
			SemverMinor: 15,
			SemverPatch: 0,
			ReleaseTag:  "nodejs-v22.15.0-20251109T120000Z",
			ReleaseURL:  "https://github.com/owner/repo/releases/tag/nodejs-v22.15.0",
			Artifacts:   `{}`,
			CreatedAt:   time.Now(),
		},
		{
			Runtime:     "python",
			Version:     "3.14.0",
			SemverMajor: 3,
			SemverMinor: 14,
			SemverPatch: 0,
			ReleaseTag:  "python-v3.14.0-20251109T120000Z",
			ReleaseURL:  "https://github.com/owner/repo/releases/tag/python-v3.14.0",
			Artifacts:   `{}`,
			CreatedAt:   time.Now(),
		},
	}

	for _, release := range releases {
		if err := db.CreateRelease(&release); err != nil {
			t.Fatalf("CreateRelease() error: %v", err)
		}
	}

	allReleases, err := db.GetAllReleases()
	if err != nil {
		t.Errorf("GetAllReleases() error: %v", err)
	}
	if len(allReleases) != 2 {
		t.Errorf("GetAllReleases() got %d releases, want 2", len(allReleases))
	}
}

func TestDB_ExportReleasesJSON(t *testing.T) {
	db, err := InitDB(Config{DatabasePath: ":memory:", LogLevel: "silent"})
	if err != nil {
		t.Fatalf("InitDB() error: %v", err)
	}
	defer func() { _ = db.Close() }()

	// Insert test release
	testRelease := &Release{
		Runtime:     "nodejs",
		Version:     "22.15.0",
		SemverMajor: 22,
		SemverMinor: 15,
		SemverPatch: 0,
		ReleaseTag:  "nodejs-v22.15.0-20251109T120000Z",
		ReleaseURL:  "https://github.com/owner/repo/releases/tag/nodejs-v22.15.0",
		Artifacts:   `{"platforms":[]}`,
		CreatedAt:   time.Now(),
	}
	if err := db.CreateRelease(testRelease); err != nil {
		t.Fatalf("CreateRelease() error: %v", err)
	}

	jsonData, err := db.ExportReleasesJSON("nodejs")
	if err != nil {
		t.Errorf("ExportReleasesJSON() error: %v", err)
		return
	}

	// Verify JSON is valid
	var releases []Release
	if err := json.Unmarshal(jsonData, &releases); err != nil {
		t.Errorf("ExportReleasesJSON() produced invalid JSON: %v", err)
		return
	}

	if len(releases) != 1 {
		t.Errorf("ExportReleasesJSON() got %d releases, want 1", len(releases))
	}
}

