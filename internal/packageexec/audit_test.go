package packageexec

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/clean-dependency-project/cdprun/internal/packaging"
)

func TestLoadDownloadAuditObject_MissingFallback(t *testing.T) {
	tmp := t.TempDir()
	target := Target{
		InputPath:   "downloads/missing.tar.xz",
		InputSHA256: "abc",
	}
	_, err := loadDownloadAuditObject(tmp, target.InputPath)
	if err == nil {
		t.Fatalf("expected error for missing file")
	}
	fb := fallbackDownloadObject(target)
	if fb["input_sha256"] != "abc" {
		t.Fatalf("fallback sha = %v", fb["input_sha256"])
	}
}

func TestLoadDownloadAuditObject_ReadsFile(t *testing.T) {
	tmp := t.TempDir()
	dlDir := filepath.Join(tmp, "downloads")
	if err := os.MkdirAll(dlDir, 0755); err != nil {
		t.Fatal(err)
	}
	tarball := filepath.Join(dlDir, "node-v1.0.0-linux-x64.tar.xz")
	if err := os.WriteFile(tarball, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	auditPath := tarball + ".audit.json"
	payload := map[string]interface{}{
		"checksum_verified": true,
		"file_name":         "node-v1.0.0-linux-x64.tar.xz",
	}
	b, _ := json.Marshal(payload)
	if err := os.WriteFile(auditPath, b, 0644); err != nil {
		t.Fatal(err)
	}

	got, err := loadDownloadAuditObject(tmp, "downloads/node-v1.0.0-linux-x64.tar.xz")
	if err != nil {
		t.Fatalf("loadDownloadAuditObject: %v", err)
	}
	if got["checksum_verified"] != true {
		t.Fatalf("got %#v", got)
	}
}

func TestParseTestOutputJSON(t *testing.T) {
	raw := `{"success":true,"passed":2,"failed":0,"tests":[]}`
	got, err := parseTestOutputJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got["success"] != true {
		t.Fatalf("got %#v", got)
	}
	_, err = parseTestOutputJSON("not json")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestWriteRPMAuditRecord(t *testing.T) {
	tmp := t.TempDir()
	pkgDir := filepath.Join(tmp, "packages")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	rpmPath := filepath.Join(pkgDir, "OSPO-nodejs-1.0.0-1.amzn2023.x86_64.rpm")
	if err := os.WriteFile(rpmPath, []byte("fake-rpm"), 0644); err != nil {
		t.Fatal(err)
	}

	dlDir := filepath.Join(tmp, "downloads")
	if err := os.MkdirAll(dlDir, 0755); err != nil {
		t.Fatal(err)
	}
	tarball := filepath.Join(dlDir, "node-v1.0.0-linux-x64.tar.xz")
	if err := os.WriteFile(tarball, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	dlAudit := tarball + ".audit.json"
	if err := os.WriteFile(dlAudit, []byte(`{"verification_status":"success","checksum_verified":true}`), 0644); err != nil {
		t.Fatal(err)
	}

	execSpec := TargetExecutorSpec{
		Build: ContainerSpec{Image: "amazonlinux:2023", Shell: "/bin/bash", Script: "build.sh"},
		Test:  ContainerSpec{Image: "amazonlinux:2023", Shell: "/bin/bash", Script: "test.sh"},
	}
	build := packaging.BuildResult{
		Runtime:         "nodejs",
		Version:         "1.0.0",
		PackageType:     packaging.PackageTypeRPM,
		PackageName:     "OSPO-nodejs",
		Release:         "1",
		Arch:            "x86_64",
		InstallPrefix:   "/export/apps/citools/OSPO-nodejs/1.0.0",
		PackageFilename: filepath.Base(rpmPath),
		PackagePath:     "packages/" + filepath.Base(rpmPath),
		PackageSHA256:   "deadbeef",
		Duration:        2 * time.Second,
		Input: packaging.InputInfo{
			Path:   "downloads/node-v1.0.0-linux-x64.tar.xz",
			SHA256: "abc",
		},
	}
	target := Target{
		Runtime:       "nodejs",
		Version:       "1.0.0",
		Target:        "rpm",
		InputPath:     "downloads/node-v1.0.0-linux-x64.tar.xz",
		InputSHA256:   "abc",
		PackageName:   "OSPO-nodejs",
		InstallPrefix: "/export/apps/citools/OSPO-nodejs/1.0.0",
	}

	testOut := `{"success":true,"passed":1,"failed":0}`
	if err := WriteRPMAuditRecord(tmp, "run-1", target, build, execSpec, testOut, "", nil); err != nil {
		t.Fatal(err)
	}
	auditData, err := os.ReadFile(rpmPath + ".audit.json")
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(auditData, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["verification_status"] != "success" {
		t.Fatalf("status = %v", decoded["verification_status"])
	}
	dl := decoded["download"].(map[string]interface{})
	if dl["checksum_verified"] != true {
		t.Fatalf("download embed = %#v", dl)
	}
}

func TestWriteRPMAuditRecord_WrongTarget(t *testing.T) {
	err := WriteRPMAuditRecord(t.TempDir(), "r", Target{Target: "apk"}, packaging.BuildResult{}, TargetExecutorSpec{}, "", "", nil)
	if err == nil {
		t.Fatal("expected error")
	}
}
