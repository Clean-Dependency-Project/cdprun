package vscode

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/clean-dependency-project/cdprun/internal/config"
	"github.com/clean-dependency-project/cdprun/internal/endoflife"
	"github.com/clean-dependency-project/cdprun/internal/platform"
	"github.com/clean-dependency-project/cdprun/internal/runtime"
)

func TestAdapter_CreateDownloadTasks_StoresExpectedSHA(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/update/win32-x64/stable/latest":
			_, _ = io.WriteString(w, `{"url":"https://example.com/VSCodeSetup-x64-1.111.0.exe","name":"1.111.0","productVersion":"1.111.0","sha256hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`)
		case "/api/update/darwin-universal/stable/latest":
			_, _ = io.WriteString(w, `{"url":"https://example.com/VSCode-darwin-universal.zip","name":"1.111.0","productVersion":"1.111.0","sha256hash":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := &config.Runtime{
		Download: config.DownloadConfig{
			BaseURL:   server.URL,
			UserAgent: "test-agent",
		},
	}
	adapter := NewAdapterWithConfig(cfg, &config.GlobalConfig{}, slog.Default(), slog.Default()).(*Adapter)
	versionInfo := endoflife.VersionInfo{Version: "1.111.0", LatestPatch: "1.111.0"}
	platforms := []platform.Platform{
		{OS: "windows", Arch: "x64", Classifier: "windows-x64"},
		{OS: "mac", Arch: "aarch64", Classifier: "mac-aarch64"},
	}

	tasks, err := adapter.CreateDownloadTasks(versionInfo, platforms, "/tmp/out")
	if err != nil {
		t.Fatalf("CreateDownloadTasks() error = %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("CreateDownloadTasks() len = %d, want 2", len(tasks))
	}
	if !strings.Contains(tasks[0].URL, "VSCodeSetup-x64-1.111.0.exe") {
		t.Fatalf("unexpected windows URL: %s", tasks[0].URL)
	}
	if !strings.Contains(tasks[1].URL, "VSCode-darwin-universal.zip") {
		t.Fatalf("unexpected mac URL: %s", tasks[1].URL)
	}
	if got := adapter.expectedSHA256("windows-x64"); got == "" {
		t.Fatalf("expected windows sha to be stored")
	}
	if got := adapter.expectedSHA256("mac-aarch64"); got == "" {
		t.Fatalf("expected mac sha to be stored")
	}
}

func TestAdapter_GetLatestVersion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/update/darwin-universal/stable/latest" {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, `{"url":"https://example.com/VSCode-darwin-universal.zip","name":"1.111.0","productVersion":"1.111.0","sha256hash":"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"}`)
	}))
	defer server.Close()

	adapter := NewAdapterWithConfig(&config.Runtime{
		Download: config.DownloadConfig{BaseURL: server.URL},
	}, &config.GlobalConfig{}, slog.Default(), slog.Default()).(*Adapter)

	latest, err := adapter.GetLatestVersion(context.Background(), runtime.VersionOptions{})
	if err != nil {
		t.Fatalf("GetLatestVersion() error = %v", err)
	}
	if latest.LatestPatch != "1.111.0" {
		t.Fatalf("GetLatestVersion() latest = %s, want 1.111.0", latest.LatestPatch)
	}
}

func TestSHA256Verifier_Verify(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "artifact.zip")
	content := []byte("hello vscode artifact")
	if err := os.WriteFile(filePath, content, 0644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	sum := sha256.Sum256(content)
	expected := hex.EncodeToString(sum[:])

	adapter := NewAdapter().(*Adapter)
	adapter.setExpectedSHA256("windows-x64", expected)
	verifier := adapter.GetVerificationStrategy()

	err := verifier.Verify(context.Background(), runtime.DownloadResult{
		LocalPath: filePath,
		Platform:  platform.Platform{Classifier: "windows-x64"},
		Runtime:   "vscode",
		Version:   "1.111.0",
		URL:       "https://example.com/artifact.zip",
	})
	if err != nil {
		t.Fatalf("Verify() expected success, got error: %v", err)
	}
	assertProofFiles(t, filePath, true)

	adapter.setExpectedSHA256("windows-x64", strings.Repeat("0", 64))
	err = verifier.Verify(context.Background(), runtime.DownloadResult{
		LocalPath: filePath,
		Platform:  platform.Platform{Classifier: "windows-x64"},
		Runtime:   "vscode",
		Version:   "1.111.0",
		URL:       "https://example.com/artifact.zip",
	})
	if err == nil {
		t.Fatalf("Verify() expected mismatch error")
	}
	assertProofFiles(t, filePath, false)
}

func TestToVSCodeAPIPlatform(t *testing.T) {
	tests := []struct {
		name    string
		plat    platform.Platform
		want    string
		wantErr bool
	}{
		{name: "windows-x64", plat: platform.Platform{OS: "windows", Arch: "x64"}, want: "win32-x64"},
		{name: "windows-aarch64", plat: platform.Platform{OS: "windows", Arch: "aarch64"}, want: "win32-arm64"},
		{name: "mac-x64", plat: platform.Platform{OS: "mac", Arch: "x64"}, want: "darwin-universal"},
		{name: "unsupported-linux", plat: platform.Platform{OS: "linux", Arch: "x64"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := toVSCodeAPIPlatform(tt.plat)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %q want %q", got, tt.want)
			}
		})
	}
}

func TestExtensionFromDownloadURL(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{url: "https://example.com/VSCodeSetup-x64-1.111.0.exe", want: ".exe"},
		{url: "https://example.com/VSCode-darwin-universal.zip", want: ".zip"},
		{url: "bad-url", want: ".bin"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("url=%s", tt.url), func(t *testing.T) {
			got := extensionFromDownloadURL(tt.url)
			if got != tt.want {
				t.Fatalf("got %q want %q", got, tt.want)
			}
		})
	}
}

func assertProofFiles(t *testing.T, filePath string, expectedVerified bool) {
	t.Helper()

	auditPath := runtime.ProofArtifactPath(filePath, "audit.json")
	data, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("expected audit proof file %s: %v", auditPath, err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("invalid JSON in %s: %v", auditPath, err)
	}
	got, ok := decoded["checksum_verified"].(bool)
	if !ok {
		t.Fatalf("missing checksum_verified in %s", auditPath)
	}
	if got != expectedVerified {
		t.Fatalf("checksum_verified in %s = %v, want %v", auditPath, got, expectedVerified)
	}

	metadataPath := runtime.ProofArtifactPath(filePath, "metadata.json")
	if _, err := os.Stat(metadataPath); !os.IsNotExist(err) {
		t.Fatalf("metadata proof file should not be created, found %s", metadataPath)
	}
}
