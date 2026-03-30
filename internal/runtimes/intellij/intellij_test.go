package intellij

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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

func TestParseChecksumLine(t *testing.T) {
	line := strings.Repeat("ab", 32) + " *file.tar.gz"
	got, err := parseChecksumLine(line)
	if err != nil {
		t.Fatalf("parseChecksumLine: %v", err)
	}
	want := strings.Repeat("ab", 32)
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func newJetBrainsTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	var ts *httptest.Server
	ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/products/releases":
			if r.URL.Query().Get("code") != "IIU" {
				http.Error(w, "bad code", http.StatusBadRequest)
				return
			}
			body := fmt.Sprintf(
				`{"IIU":[{"version":"2099.1.1","downloads":{"windows":{"link":"%s/file.exe","checksumLink":"%s/file.exe.sha256"}}}]}`,
				ts.URL, ts.URL,
			)
			_, _ = w.Write([]byte(body))
		case "/file.exe.sha256":
			_, _ = io.WriteString(w, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa *file.exe\n")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(ts.Close)
	return ts
}

func TestAdapter_CreateDownloadTasks_StoresExpectedSHA(t *testing.T) {
	ts := newJetBrainsTestServer(t)

	cfg := &config.Runtime{
		Download: config.DownloadConfig{
			BaseURL:       ts.URL + "/products/releases",
			JetBrainsCode: "IIU",
			UserAgent:     "test",
		},
	}
	adapter := NewAdapterWithConfig(cfg, &config.GlobalConfig{}, slog.Default(), slog.Default()).(*Adapter)
	versionInfo := endoflife.VersionInfo{Version: "2099.1.1", LatestPatch: "2099.1.1"}
	platforms := []platform.Platform{
		{OS: "windows", Arch: "x64", DownloadName: "windows", Classifier: "windows-x64"},
	}

	tasks, err := adapter.CreateDownloadTasks(versionInfo, platforms, t.TempDir())
	if err != nil {
		t.Fatalf("CreateDownloadTasks() error = %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("len tasks = %d, want 1", len(tasks))
	}
	if !strings.HasSuffix(tasks[0].URL, "/file.exe") {
		t.Fatalf("unexpected URL: %s", tasks[0].URL)
	}
	if got := adapter.expectedSHA256("windows-x64"); got == "" {
		t.Fatal("expected sha stored")
	}
}

func TestAdapter_GetLatestVersion(t *testing.T) {
	ts := newJetBrainsTestServer(t)

	adapter := NewAdapterWithConfig(&config.Runtime{
		Download: config.DownloadConfig{
			BaseURL:       ts.URL + "/products/releases",
			JetBrainsCode: "IIU",
		},
	}, &config.GlobalConfig{}, slog.Default(), slog.Default()).(*Adapter)

	latest, err := adapter.GetLatestVersion(context.Background(), runtime.VersionOptions{})
	if err != nil {
		t.Fatalf("GetLatestVersion() error = %v", err)
	}
	if latest.LatestPatch != "2099.1.1" {
		t.Fatalf("LatestPatch = %s, want 2099.1.1", latest.LatestPatch)
	}
}

func TestAdapter_GetEndOfLifeProduct_Empty(t *testing.T) {
	a := NewAdapterWithConfig(nil, nil, slog.Default(), slog.Default()).(*Adapter)
	if got := a.GetEndOfLifeProduct(); got != "" {
		t.Fatalf("GetEndOfLifeProduct() = %q, want empty (not on endoflife.date)", got)
	}
}

func TestSHA256Verifier_Verify(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "artifact.exe")
	content := []byte("hello intellij artifact")
	if err := os.WriteFile(filePath, content, 0644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	sum := sha256.Sum256(content)
	expected := hex.EncodeToString(sum[:])

	adapter := NewAdapterWithConfig(nil, nil, slog.Default(), slog.Default()).(*Adapter)
	adapter.setExpectedSHA256("windows-x64", expected)
	verifier := adapter.GetVerificationStrategy()

	err := verifier.Verify(context.Background(), runtime.DownloadResult{
		LocalPath: filePath,
		Platform:  platform.Platform{Classifier: "windows-x64"},
		Runtime:   IntelliJUltimateRuntime,
		Version:   "2099.1.1",
		URL:       "https://example.com/artifact.exe",
	})
	if err != nil {
		t.Fatalf("Verify() expected success, got error: %v", err)
	}
}
