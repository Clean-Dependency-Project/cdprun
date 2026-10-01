package tomcat

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path"
	"testing"

	"github.com/clean-dependency-project/cdprun/internal/config"
	"github.com/clean-dependency-project/cdprun/internal/endoflife"
	"github.com/clean-dependency-project/cdprun/internal/platform"
	"github.com/clean-dependency-project/cdprun/internal/runtime"
)

func TestCreateDownloadTasks_IncludesWindowsInstallerAndZip(t *testing.T) {
	const version = "10.1.60"
	available := map[string]bool{
		"apache-tomcat-10.1.60-windows-x64.zip": true,
		"apache-tomcat-10.1.60-windows-x86.zip": true,
		"apache-tomcat-10.1.60.tar.gz":          true,
		"apache-tomcat-10.1.60.exe":             true,
	}
	adapter := newArchiveAdapter(t, available)

	tasks, err := adapter.CreateDownloadTasks(endoflife.VersionInfo{Version: version}, []platform.Platform{
		{OS: "windows", Arch: "x64"},
		{OS: "windows", Arch: "x86"},
		{OS: "linux", Arch: "x64"},
	}, t.TempDir())
	if err != nil {
		t.Fatalf("CreateDownloadTasks() error = %v", err)
	}

	mains := mainFilenames(tasks)
	for name := range available {
		if mains[name] != 1 {
			t.Errorf("main task %s count = %d, want 1", name, mains[name])
		}
	}
	if mains["apache-tomcat-10.1.60.exe"] != 1 {
		t.Fatalf("installer downloaded %d times, want 1", mains["apache-tomcat-10.1.60.exe"])
	}

	for _, task := range tasks {
		if task.FileType != "main" || path.Base(task.OutputPath) != "apache-tomcat-10.1.60.exe" {
			continue
		}
		if task.Platform.OS != "windows" || task.Platform.Arch != "x64" {
			t.Errorf("installer platform = %s-%s, want windows-x64", task.Platform.OS, task.Platform.Arch)
		}
		if !hasSidecar(tasks, task.OutputPath+".sha512", "checksum") || !hasSidecar(tasks, task.OutputPath+".asc", "signature") {
			t.Error("installer is missing checksum or signature task")
		}
	}
}

func TestCreateDownloadTasks_MissingInstallerKeepsZip(t *testing.T) {
	available := map[string]bool{
		"apache-tomcat-11.0.26-windows-x64.zip": true,
		"apache-tomcat-11.0.26.tar.gz":          true,
	}
	adapter := newArchiveAdapter(t, available)

	tasks, err := adapter.CreateDownloadTasks(endoflife.VersionInfo{Version: "11.0.26"}, []platform.Platform{
		{OS: "windows", Arch: "x64"},
		{OS: "linux", Arch: "x64"},
	}, t.TempDir())
	if err != nil {
		t.Fatalf("CreateDownloadTasks() error = %v", err)
	}

	mains := mainFilenames(tasks)
	if mains["apache-tomcat-11.0.26-windows-x64.zip"] != 1 {
		t.Errorf("windows zip count = %d, want 1", mains["apache-tomcat-11.0.26-windows-x64.zip"])
	}
	if mains["apache-tomcat-11.0.26.tar.gz"] != 1 {
		t.Errorf("tar.gz count = %d, want 1", mains["apache-tomcat-11.0.26.tar.gz"])
	}
	if mains["apache-tomcat-11.0.26.exe"] != 0 {
		t.Errorf("installer count = %d, want 0", mains["apache-tomcat-11.0.26.exe"])
	}
}

func TestCreateDownloadTasks_LinuxOnlySkipsInstaller(t *testing.T) {
	var sawExe bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if path.Base(r.URL.Path) == "apache-tomcat-9.0.122.exe" {
			sawExe = true
		}
		if path.Base(r.URL.Path) == "apache-tomcat-9.0.122.tar.gz" {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)

	adapter := NewAdapterWithConfig(nil, &config.Runtime{
		Download: config.DownloadConfig{BaseURL: server.URL},
	}, nil, discardLogger(), discardLogger())

	tasks, err := adapter.CreateDownloadTasks(endoflife.VersionInfo{Version: "9.0.122"}, []platform.Platform{
		{OS: "linux", Arch: "x64"},
	}, t.TempDir())
	if err != nil {
		t.Fatalf("CreateDownloadTasks() error = %v", err)
	}
	if sawExe {
		t.Error("linux-only download probed the Windows installer")
	}
	if mainFilenames(tasks)["apache-tomcat-9.0.122.tar.gz"] != 1 {
		t.Error("linux tar.gz task missing")
	}
}

func TestCreateDownloadTasks_InstallerUsesX86WhenX64NotRequested(t *testing.T) {
	available := map[string]bool{
		"apache-tomcat-9.0.122-windows-x86.zip": true,
		"apache-tomcat-9.0.122.exe":             true,
	}
	adapter := newArchiveAdapter(t, available)

	tasks, err := adapter.CreateDownloadTasks(endoflife.VersionInfo{Version: "9.0.122"}, []platform.Platform{
		{OS: "windows", Arch: "x86"},
	}, t.TempDir())
	if err != nil {
		t.Fatalf("CreateDownloadTasks() error = %v", err)
	}

	for _, task := range tasks {
		if task.FileType == "main" && path.Base(task.OutputPath) == "apache-tomcat-9.0.122.exe" {
			if task.Platform.Arch != "x86" {
				t.Errorf("installer arch = %s, want x86", task.Platform.Arch)
			}
			return
		}
	}
	t.Fatal("installer task not created")
}

func newArchiveAdapter(t *testing.T, available map[string]bool) *TomcatAdapter {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if available[path.Base(r.URL.Path)] {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)

	return NewAdapterWithConfig(nil, &config.Runtime{
		Download: config.DownloadConfig{BaseURL: server.URL},
	}, nil, discardLogger(), discardLogger())
}

func mainFilenames(tasks []runtime.DownloadTask) map[string]int {
	counts := make(map[string]int)
	for _, task := range tasks {
		if task.FileType != "main" {
			continue
		}
		counts[path.Base(task.OutputPath)]++
	}
	return counts
}

func hasSidecar(tasks []runtime.DownloadTask, outputPath, fileType string) bool {
	for _, task := range tasks {
		if task.FileType == fileType && task.OutputPath == outputPath {
			return true
		}
	}
	return false
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}
