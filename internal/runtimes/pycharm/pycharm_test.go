package pycharm

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/clean-dependency-project/cdprun/internal/config"
	"github.com/clean-dependency-project/cdprun/internal/endoflife"
	"github.com/clean-dependency-project/cdprun/internal/platform"
	"github.com/clean-dependency-project/cdprun/internal/runtime"
)

func newJetBrainsTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	var ts *httptest.Server
	ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/products/releases":
			if r.URL.Query().Get("code") != "PCP" {
				http.Error(w, "bad code", http.StatusBadRequest)
				return
			}
			body := fmt.Sprintf(
				`{"PCP":[{"version":"2099.1.1","downloads":{"windows":{"link":"%s/file.exe","checksumLink":"%s/file.exe.sha256"}}}]}`,
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
			JetBrainsCode: "PCP",
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
	if got := adapter.ExpectedSHA256("windows-x64"); got == "" {
		t.Fatal("expected sha stored")
	}
}

func TestAdapter_GetLatestVersion(t *testing.T) {
	ts := newJetBrainsTestServer(t)

	adapter := NewAdapterWithConfig(&config.Runtime{
		Download: config.DownloadConfig{
			BaseURL:       ts.URL + "/products/releases",
			JetBrainsCode: "PCP",
		},
	}, &config.GlobalConfig{}, slog.Default(), slog.Default()).(*Adapter)

	latest, err := adapter.GetLatestVersion(context.Background(), runtime.VersionOptions{})
	if err != nil {
		t.Fatalf("GetLatestVersion() error = %v", err)
	}
	if latest.LatestPatch != "2099.1.1" {
		t.Fatalf("LatestPatch = %s, want 2099.1.1", latest.LatestPatch)
	}
	if latest.RuntimeName != PyCharmRuntime {
		t.Fatalf("RuntimeName = %s, want %s", latest.RuntimeName, PyCharmRuntime)
	}
}

func TestAdapter_GetEndOfLifeProduct_Empty(t *testing.T) {
	a := NewAdapterWithConfig(nil, nil, slog.Default(), slog.Default()).(*Adapter)
	if got := a.GetEndOfLifeProduct(); got != "" {
		t.Fatalf("GetEndOfLifeProduct() = %q, want empty (not on endoflife.date)", got)
	}
}
