package python

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/clean-dependency-project/cdprun/internal/config"
	"github.com/clean-dependency-project/cdprun/internal/platform"
	"github.com/clean-dependency-project/cdprun/internal/runtime"
)

func sigstoreRuntime(identities ...config.SigstoreIdentityEntry) *config.Runtime {
	return &config.Runtime{
		Enabled: true,
		Verification: config.Verification{
			Enabled: true,
			Methods: config.VerificationMethods{
				GPG:      config.GPGVerification{Enabled: false},
				Sigstore: config.SigstoreVerification{Enabled: true, BundleSuffix: ".sigstore", Identities: identities},
			},
		},
	}
}

func TestPythonSigstoreVerifier_SkipWhenBundleMissing(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "Python-3.15.0.tar.xz")
	if err := os.WriteFile(mainPath, []byte("artifact-bytes"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := sigstoreRuntime(config.SigstoreIdentityEntry{
		VersionPrefix: "3.15", CertIdentity: "hugo@python.org", OIDCIssuer: "https://github.com/login/oauth",
	})
	v := NewPythonSigstoreVerifier(cfg, nil).(*PythonSigstoreVerifier)

	result := runtime.DownloadResult{
		LocalPath: mainPath,
		Runtime:   "python",
		Version:   "3.15.0",
		Success:   true,
	}
	if err := v.Verify(context.Background(), result); err != nil {
		t.Fatalf("expected skip (nil error) when bundle missing, got: %v", err)
	}
	auditBytes, err := os.ReadFile(mainPath + ".audit.json")
	if err != nil {
		t.Fatalf("audit file not written: %v", err)
	}
	if !strings.Contains(string(auditBytes), `"sigstore_verified": false`) {
		t.Errorf("audit should record sigstore_verified false, got: %s", auditBytes)
	}
	if !strings.Contains(string(auditBytes), `"bundle_missing"`) {
		t.Errorf("audit should record bundle_missing status, got: %s", auditBytes)
	}
}

func TestPythonSigstoreVerifier_SkipsOldReleaseWithNoIdentity(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "Python-2.7.0.tar.xz")
	if err := os.WriteFile(mainPath, []byte("artifact-bytes"), 0644); err != nil {
		t.Fatal(err)
	}
	// A bundle sidecar exists, but config has no identity row for 2.7 (old
	// release). Sigstore should SKIP rather than fail closed.
	if err := os.WriteFile(mainPath+".sigstore", []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := sigstoreRuntime(config.SigstoreIdentityEntry{
		VersionPrefix: "3.15", CertIdentity: "hugo@python.org", OIDCIssuer: "https://github.com/login/oauth",
	})
	v := NewPythonSigstoreVerifier(cfg, nil).(*PythonSigstoreVerifier)

	result := runtime.DownloadResult{
		LocalPath: mainPath,
		Runtime:   "python",
		Version:   "2.7.0",
		Success:   true,
	}
	if err := v.Verify(context.Background(), result); err != nil {
		t.Fatalf("expected skip (nil error) for old release with no identity, got: %v", err)
	}
	auditBytes, err := os.ReadFile(mainPath + ".audit.json")
	if err != nil {
		t.Fatalf("audit file not written: %v", err)
	}
	if !strings.Contains(string(auditBytes), `"identity_not_configured"`) {
		t.Errorf("audit should record identity_not_configured status, got: %s", auditBytes)
	}
}

func TestPythonSigstoreVerifier_SkipsBundleSidecarItself(t *testing.T) {
	dir := t.TempDir()
	bundlePath := filepath.Join(dir, "Python-3.15.0.tar.xz.sigstore")
	if err := os.WriteFile(bundlePath, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	cfg := sigstoreRuntime(config.SigstoreIdentityEntry{
		VersionPrefix: "3.15", CertIdentity: "hugo@python.org", OIDCIssuer: "https://github.com/login/oauth",
	})
	v := NewPythonSigstoreVerifier(cfg, nil).(*PythonSigstoreVerifier)

	result := runtime.DownloadResult{LocalPath: bundlePath, Runtime: "python", Version: "3.15.0", Success: true}
	if err := v.Verify(context.Background(), result); err != nil {
		t.Fatalf("expected nil when verifying bundle sidecar itself, got: %v", err)
	}
}

func TestPythonAdapter_GetVerificationStrategy_SigstorePrecedence(t *testing.T) {
	// Sigstore enabled -> sigstore verifier.
	a := &PythonAdapter{config: sigstoreRuntime(config.SigstoreIdentityEntry{
		VersionPrefix: "3.15", CertIdentity: "hugo@python.org", OIDCIssuer: "https://github.com/login/oauth",
	})}
	if got := a.GetVerificationStrategy().GetType(); got != "python-sigstore" {
		t.Errorf("expected python-sigstore, got %q", got)
	}

	// Sigstore disabled, GPG enabled -> gpg verifier.
	gpgCfg := &config.Runtime{Enabled: true, Verification: config.Verification{
		Enabled: true,
		Methods: config.VerificationMethods{GPG: config.GPGVerification{Enabled: true, SignaturePattern: ".asc"}},
	}}
	a2 := &PythonAdapter{config: gpgCfg}
	if got := a2.GetVerificationStrategy().GetType(); got != "python-gpg" {
		t.Errorf("expected python-gpg, got %q", got)
	}

	// No config -> gpg verifier (legacy default).
	a3 := &PythonAdapter{}
	if got := a3.GetVerificationStrategy().GetType(); got != "python-gpg" {
		t.Errorf("expected python-gpg default, got %q", got)
	}
}

func TestPythonAdapter_SigstoreDownloadTasks_NoAscWhenSigstoreOn(t *testing.T) {
	cfg := &config.Runtime{
		Enabled: true,
		Download: config.DownloadConfig{
			BaseURL:    "https://www.python.org/ftp/python",
			URLPattern: "{base_url}/{version}/python-{version}-{arch}.{ext}",
		},
		Verification: config.Verification{
			Enabled: true,
			Methods: config.VerificationMethods{
				GPG:      config.GPGVerification{Enabled: true, SignaturePattern: ".asc"},
				Sigstore: config.SigstoreVerification{Enabled: true, BundleSuffix: ".sigstore", Identities: []config.SigstoreIdentityEntry{{VersionPrefix: "3.15", CertIdentity: "hugo@python.org", OIDCIssuer: "https://github.com/login/oauth"}}},
			},
		},
	}
	a := &PythonAdapter{config: cfg, stdout: nil, stderr: nil}

	plat := platform.Platform{OS: "linux", Arch: "x64", FileExt: "tar.xz"}
	tasks := a.createVerificationTasksWithVersions([]platformVersion{{plat: plat, version: "3.15.0"}}, "/tmp/out", "cdprun/1.0")

	if len(tasks) != 1 {
		t.Fatalf("expected 1 sigstore task, got %d", len(tasks))
	}
	if tasks[0].FileType != "sigstore" {
		t.Errorf("expected sigstore file type, got %q", tasks[0].FileType)
	}
	if !strings.HasSuffix(tasks[0].URL, ".sigstore") {
		t.Errorf("expected .sigstore URL, got %q", tasks[0].URL)
	}
}

func TestPythonAdapter_GPGDownloadTasks_WhenSigstoreOff(t *testing.T) {
	cfg := &config.Runtime{
		Enabled: true,
		Download: config.DownloadConfig{
			BaseURL:    "https://www.python.org/ftp/python",
			URLPattern: "{base_url}/{version}/python-{version}-{arch}.{ext}",
		},
		Verification: config.Verification{
			Enabled: true,
			Methods: config.VerificationMethods{
				GPG: config.GPGVerification{Enabled: true, SignaturePattern: ".asc"},
			},
		},
	}
	a := &PythonAdapter{config: cfg}

	plat := platform.Platform{OS: "linux", Arch: "x64", FileExt: "tar.xz"}
	tasks := a.createVerificationTasksWithVersions([]platformVersion{{plat: plat, version: "3.13.0"}}, "/tmp/out", "cdprun/1.0")

	if len(tasks) != 1 {
		t.Fatalf("expected 1 gpg task, got %d", len(tasks))
	}
	if tasks[0].FileType != "signature" {
		t.Errorf("expected signature file type, got %q", tasks[0].FileType)
	}
	if !strings.HasSuffix(tasks[0].URL, ".asc") {
		t.Errorf("expected .asc URL, got %q", tasks[0].URL)
	}
}
