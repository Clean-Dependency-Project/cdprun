package storage

import (
	"encoding/json"
	"testing"
	"time"
)

func TestDB_AppendOrMergePlatformArtifact(t *testing.T) {
	db := newTestDB(t)

	release := &Release{
		Runtime:     "nodejs",
		Version:     "22.22.0",
		SemverMajor: 22,
		SemverMinor: 22,
		SemverPatch: 0,
		ReleaseTag:  "nodejs-multi-test",
		ReleaseURL:  "https://example.com/release",
		Artifacts:   `{"platforms":[],"common_files":[],"metadata":{}}`,
		CreatedAt:   time.Now(),
	}
	if err := db.CreateRelease(release); err != nil {
		t.Fatalf("CreateRelease() error: %v", err)
	}

	artifact := PlatformArtifact{
		Platform:     "linux-rpm-x64",
		PlatformOS:   "linux",
		PlatformArch: "x64",
		Binary: &ArtifactFile{
			Filename:   "OSPO-nodejs-22.22.0-1.x86_64.rpm",
			Size:       1234,
			SHA256:     "sha-old",
			URL:        "https://example.com/old.rpm",
			UploadedAt: time.Now(),
		},
	}

	if err := db.AppendOrMergePlatformArtifact("nodejs", "22.22.0", artifact); err != nil {
		t.Fatalf("AppendOrMergePlatformArtifact() append error: %v", err)
	}

	gotRelease, err := db.GetRelease("nodejs", "22.22.0")
	if err != nil {
		t.Fatalf("GetRelease() error: %v", err)
	}
	parsed, err := parseReleaseArtifacts(gotRelease.Artifacts)
	if err != nil {
		t.Fatalf("parseReleaseArtifacts() error: %v", err)
	}
	if len(parsed.Platforms) != 1 {
		t.Fatalf("platforms len = %d, want 1", len(parsed.Platforms))
	}
	if parsed.Metadata.PlatformCount != 1 {
		t.Fatalf("metadata.platform_count = %d, want 1", parsed.Metadata.PlatformCount)
	}

	artifact.Binary.SHA256 = "sha-new"
	artifact.Binary.URL = "https://example.com/new.rpm"
	if err := db.AppendOrMergePlatformArtifact("nodejs", "22.22.0", artifact); err != nil {
		t.Fatalf("AppendOrMergePlatformArtifact() merge error: %v", err)
	}

	gotRelease, err = db.GetRelease("nodejs", "22.22.0")
	if err != nil {
		t.Fatalf("GetRelease() error after merge: %v", err)
	}
	parsed, err = parseReleaseArtifacts(gotRelease.Artifacts)
	if err != nil {
		t.Fatalf("parseReleaseArtifacts() error after merge: %v", err)
	}
	if len(parsed.Platforms) != 1 {
		t.Fatalf("platforms len after merge = %d, want 1", len(parsed.Platforms))
	}
	if parsed.Platforms[0].Binary == nil || parsed.Platforms[0].Binary.SHA256 != "sha-new" {
		t.Fatalf("merged binary sha = %+v, want sha-new", parsed.Platforms[0].Binary)
	}
}

func TestDB_AppendOrMergePlatformArtifact_InvalidArtifactsJSON(t *testing.T) {
	db := newTestDB(t)

	release := &Release{
		Runtime:     "nodejs",
		Version:     "22.22.0",
		SemverMajor: 22,
		SemverMinor: 22,
		SemverPatch: 0,
		ReleaseTag:  "nodejs-multi-invalid-json",
		ReleaseURL:  "https://example.com/release",
		Artifacts:   `{"platforms":[`, // malformed
		CreatedAt:   time.Now(),
	}
	if err := db.CreateRelease(release); err != nil {
		t.Fatalf("CreateRelease() error: %v", err)
	}

	err := db.AppendOrMergePlatformArtifact("nodejs", "22.22.0", PlatformArtifact{
		Platform: "linux-rpm-x64",
		Binary: &ArtifactFile{
			Filename: "pkg.rpm",
		},
	})
	if err == nil {
		t.Fatal("AppendOrMergePlatformArtifact() error = nil, want parse error")
	}
}

func TestDB_AppendOrMergePlatformArtifact_Validation(t *testing.T) {
	db := newTestDB(t)

	if err := db.AppendOrMergePlatformArtifact("", "1.0.0", PlatformArtifact{}); err == nil {
		t.Fatal("expected error for empty runtime")
	}
	if err := db.AppendOrMergePlatformArtifact("nodejs", "", PlatformArtifact{}); err == nil {
		t.Fatal("expected error for empty version")
	}
	if err := db.AppendOrMergePlatformArtifact("nodejs", "1.0.0", PlatformArtifact{}); err == nil {
		t.Fatal("expected error for nil binary")
	}
}

func TestDB_AppendOrMergePlatformArtifact_PreservesCommonFiles(t *testing.T) {
	db := newTestDB(t)

	seed := ReleaseArtifacts{
		CommonFiles: []CommonFile{
			{
				Type:       "checksum_file",
				Filename:   "SHASUMS256.txt",
				Size:       2048,
				SHA256:     "sum-file-sha",
				URL:        "https://example.com/SHASUMS256.txt",
				UploadedAt: time.Now(),
			},
		},
	}
	data, err := json.Marshal(seed)
	if err != nil {
		t.Fatalf("marshal seed: %v", err)
	}

	release := &Release{
		Runtime:     "nodejs",
		Version:     "22.23.0",
		SemverMajor: 22,
		SemverMinor: 23,
		SemverPatch: 0,
		ReleaseTag:  "nodejs-common-files-preserve",
		ReleaseURL:  "https://example.com/release",
		Artifacts:   string(data),
		CreatedAt:   time.Now(),
	}
	if err := db.CreateRelease(release); err != nil {
		t.Fatalf("CreateRelease() error: %v", err)
	}

	err = db.AppendOrMergePlatformArtifact("nodejs", "22.23.0", PlatformArtifact{
		Platform:     "linux-rpm-x64",
		PlatformOS:   "linux",
		PlatformArch: "x64",
		Binary: &ArtifactFile{
			Filename: "OSPO-nodejs-22.23.0-1.x86_64.rpm",
			Size:     1000,
			SHA256:   "rpm-sha",
			URL:      "https://example.com/pkg.rpm",
		},
	})
	if err != nil {
		t.Fatalf("AppendOrMergePlatformArtifact() error: %v", err)
	}

	gotRelease, err := db.GetRelease("nodejs", "22.23.0")
	if err != nil {
		t.Fatalf("GetRelease() error: %v", err)
	}
	var gotArtifacts ReleaseArtifacts
	if err := json.Unmarshal([]byte(gotRelease.Artifacts), &gotArtifacts); err != nil {
		t.Fatalf("unmarshal artifacts: %v", err)
	}
	if len(gotArtifacts.CommonFiles) != 1 {
		t.Fatalf("common files len = %d, want 1", len(gotArtifacts.CommonFiles))
	}
	if gotArtifacts.CommonFiles[0].Filename != "SHASUMS256.txt" {
		t.Fatalf("common file = %q, want SHASUMS256.txt", gotArtifacts.CommonFiles[0].Filename)
	}
}

