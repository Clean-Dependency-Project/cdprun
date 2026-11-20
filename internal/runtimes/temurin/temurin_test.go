package temurin

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/clean-dependency-project/cdprun/internal/config"
	"github.com/clean-dependency-project/cdprun/internal/endoflife"
	"github.com/clean-dependency-project/cdprun/internal/gpg"
	"github.com/clean-dependency-project/cdprun/internal/platform"
	"github.com/clean-dependency-project/cdprun/internal/runtime"
	"github.com/clean-dependency-project/cdprun/internal/storage"
)

// adoptiumFixture is one feature line served by the test Adoptium API.
type adoptiumFixture struct {
	major         int
	releaseName   string
	platforms     []string
	featureStatus int
}

type recordedRequest struct {
	method string
	path   string
	query  string
}

func newAdoptiumTestAdapter(t *testing.T, lines []adoptiumFixture) (*TemurinAdapter, *[]recordedRequest) {
	t.Helper()

	byMajor := make(map[int]adoptiumFixture, len(lines))
	majors := make([]int, 0, len(lines))
	lts := make([]int, 0, len(lines))
	for _, line := range lines {
		byMajor[line.major] = line
		majors = append(majors, line.major)
		if line.major != 26 && line.major != 27 {
			lts = append(lts, line.major)
		}
	}

	var mu sync.Mutex
	recorded := make([]recordedRequest, 0)

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		recorded = append(recorded, recordedRequest{method: r.Method, path: r.URL.Path, query: r.URL.RawQuery})
		mu.Unlock()

		switch {
		case r.URL.Path == "/v3/info/available_releases":
			body := map[string]any{
				"available_releases":          majors,
				"available_lts_releases":      lts,
				"most_recent_feature_release": 27,
			}
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(body); err != nil {
				t.Errorf("encode available releases: %v", err)
			}
		case strings.Contains(r.URL.Path, "/assets/feature_releases/"):
			major := featureMajorFromPath(t, r.URL.Path)
			line, ok := byMajor[major]
			if !ok {
				http.NotFound(w, r)
				return
			}
			page, _ := strconv.Atoi(r.URL.Query().Get("page"))
			if page > 0 {
				http.NotFound(w, r)
				return
			}
			if line.featureStatus != 0 {
				w.WriteHeader(line.featureStatus)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode([]map[string]any{featureReleaseBody(server.URL, line.major, line.releaseName, line.platforms)}); err != nil {
				t.Errorf("encode feature release: %v", err)
			}
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	adapter := NewAdapter().(*TemurinAdapter)
	adapter.adoptiumBaseURL = server.URL + "/v3"
	return adapter, &recorded
}

func featureMajorFromPath(t *testing.T, path string) int {
	t.Helper()
	const marker = "/assets/feature_releases/"
	rest := strings.TrimPrefix(path, "/v3"+marker)
	rest = strings.TrimPrefix(rest, marker)
	majorText, _, _ := strings.Cut(rest, "/")
	major, err := strconv.Atoi(majorText)
	if err != nil {
		t.Fatalf("feature release path %s: %v", path, err)
	}
	return major
}

func featureReleaseBody(serverURL string, major int, releaseName string, platforms []string) map[string]any {
	binaries := make([]map[string]any, 0, len(platforms))
	for _, plat := range platforms {
		osName, arch, _ := strings.Cut(plat, "-")
		link := fmt.Sprintf("%s/pkg/%d/%s.tar.gz", serverURL, major, plat)
		binaries = append(binaries, map[string]any{
			"os":           osName,
			"architecture": arch,
			"image_type":   "jdk",
			"jvm_impl":     "hotspot",
			"package": map[string]any{
				"name":           plat + ".tar.gz",
				"link":           link,
				"checksum_link":  link + ".sha256.txt",
				"signature_link": link + ".sig",
			},
		})
	}
	return map[string]any{
		"release_name": releaseName,
		"release_link": serverURL + "/releases/tag/" + releaseName,
		"release_type": "ga",
		"binaries":     binaries,
	}
}

func standardLines() []adoptiumFixture {
	return []adoptiumFixture{
		{major: 27, releaseName: "jdk-27.0.1.1+4", platforms: []string{"linux-x64"}},
		{major: 26, releaseName: "jdk-26.0.2.1+1", platforms: []string{"linux-x64"}},
		{major: 25, releaseName: "jdk-25.0.5.1+2", platforms: []string{"linux-x64"}},
		{major: 21, releaseName: "jdk-21.0.13.1+2", platforms: []string{"linux-x64"}},
		{major: 17, releaseName: "jdk-17.0.21.1+2", platforms: []string{"linux-x64", "linux-aarch64"}},
		{major: 11, releaseName: "jdk-11.0.33+10", platforms: []string{"linux-x64"}},
		{major: 8, releaseName: "jdk8u512-b09", platforms: []string{"linux-x64"}},
	}
}

func createTestTemurinPolicyFile(t *testing.T) string {
	tempDir := t.TempDir()
	policyPath := filepath.Join(tempDir, "temurin-policy.json")

	// Create a policy file in the format expected by the Temurin LoadPolicy method
	policyContent := `[
		{
			"version": "21",
			"supported": true,
			"recommended": true,
			"lts": true,
			"under_review": false
		},
		{
			"version": "17",
			"supported": true,
			"recommended": false,
			"lts": true,
			"under_review": false
		},
		{
			"version": "11",
			"supported": true,
			"recommended": false,
			"lts": true,
			"under_review": false
		}
	]`

	err := os.WriteFile(policyPath, []byte(policyContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test policy file: %v", err)
	}

	return policyPath
}

func TestNewAdapter(t *testing.T) {
	adapter := NewAdapter()

	if adapter == nil {
		t.Error("NewAdapter returned nil")
	}

	temurinAdapter, ok := adapter.(*TemurinAdapter)
	if !ok {
		t.Error("NewAdapter did not return a TemurinAdapter")
	}

	if temurinAdapter.adoptiumBaseURL != defaultAdoptiumBaseURL {
		t.Errorf("adoptiumBaseURL = %s, want %s", temurinAdapter.adoptiumBaseURL, defaultAdoptiumBaseURL)
	}
}

func TestTemurinAdapter_GetName(t *testing.T) {
	adapter := NewAdapter()
	name := adapter.GetName()

	if name != "temurin" {
		t.Errorf("GetName() = %s, want temurin", name)
	}
}

func TestTemurinAdapter_GetEndOfLifeProduct(t *testing.T) {
	adapter := NewAdapter()
	product := adapter.GetEndOfLifeProduct()

	if product != "" {
		t.Errorf("GetEndOfLifeProduct() = %s, want empty", product)
	}
}

func TestTemurinAdapter_GetSupportedPlatforms(t *testing.T) {
	runtimeConfig := &config.Runtime{
		SupportedArchitectures: []string{"x64", "aarch64"},
		SupportedPlatforms: []config.PlatformConfig{
			{OS: "windows", Arch: []string{"x64", "aarch64"}, FileExtension: "msi", DownloadName: "windows"},
			{OS: "linux", Arch: []string{"x64", "aarch64"}, FileExtension: "tar.gz", DownloadName: "linux"},
			{OS: "mac", Arch: []string{"x64", "aarch64"}, FileExtension: "pkg", DownloadName: "mac"},
		},
	}
	adapter := NewAdapterWithConfig(runtimeConfig, nil, slog.Default(), slog.Default())
	platforms := adapter.GetSupportedPlatforms()

	if len(platforms) == 0 {
		t.Error("GetSupportedPlatforms() returned empty list")
	}

	// Check that we have expected platforms (OS/Arch combinations)
	hasWindows := false
	hasLinux := false
	hasMac := false
	hasLinuxAArch64 := false

	for _, p := range platforms {
		switch p.OS {
		case "windows":
			hasWindows = true
		case "linux":
			hasLinux = true
		case "mac": // Temurin uses "mac" not "darwin"
			hasMac = true
		}

		if p.OS == "linux" && p.Arch == "aarch64" {
			hasLinuxAArch64 = true
		}
	}

	if !hasWindows || !hasLinux || !hasMac {
		t.Errorf("GetSupportedPlatforms() missing expected platforms. Got: %v", platforms)
	}
	if !hasLinuxAArch64 {
		t.Errorf("GetSupportedPlatforms() missing linux-aarch64. Got: %v", platforms)
	}
}

func TestTemurinAdapter_ListVersions(t *testing.T) {
	adapter, recorded := newAdoptiumTestAdapter(t, standardLines())

	versions, err := adapter.ListVersions(context.Background())
	if err != nil {
		t.Fatalf("ListVersions() error = %v", err)
	}

	byMajor := map[string]endoflife.VersionInfo{}
	for _, version := range versions {
		byMajor[version.Version] = version
		if version.RuntimeName != "temurin" {
			t.Errorf("Version %s runtime = %s, want temurin", version.Version, version.RuntimeName)
		}
	}

	checks := []struct {
		major string
		patch string
		lts   bool
		eol   bool
	}{
		{major: "17", patch: "17.0.21.1+2", lts: true},
		{major: "21", patch: "21.0.13.1+2", lts: true},
		{major: "25", patch: "25.0.5.1+2", lts: true},
		{major: "27", patch: "27.0.1.1+4", lts: false},
		{major: "26", patch: "26.0.2.1+1", lts: false, eol: true},
	}
	for _, check := range checks {
		version, ok := byMajor[check.major]
		if !ok {
			t.Errorf("missing major %s", check.major)
			continue
		}
		if version.LatestPatch != check.patch {
			t.Errorf("major %s LatestPatch = %s, want %s", check.major, version.LatestPatch, check.patch)
		}
		if version.IsLTS != check.lts {
			t.Errorf("major %s IsLTS = %v, want %v", check.major, version.IsLTS, check.lts)
		}
		if version.IsEOL != check.eol {
			t.Errorf("major %s IsEOL = %v, want %v", check.major, version.IsEOL, check.eol)
		}
		if version.EOLDate != "" {
			t.Errorf("major %s EOLDate = %s, want empty", check.major, version.EOLDate)
		}
	}

	for _, major := range []string{"17", "21", "25"} {
		path := "/v3/assets/feature_releases/" + major + "/ga"
		if !sawRequest(*recorded, http.MethodGet, path) {
			t.Errorf("expected GET %s", path)
		}
	}
	if sawRequest(*recorded, http.MethodGet, "/v3/assets/version/jdk-21.0.13.1+2") {
		t.Error("ListVersions requested /assets/version")
	}
}

func TestTemurinAdapter_ListVersions_DoesNotCallGitHub(t *testing.T) {
	adapter, recorded := newAdoptiumTestAdapter(t, standardLines())

	versions, err := adapter.ListVersions(context.Background())
	if err != nil {
		t.Fatalf("ListVersions() error = %v", err)
	}
	found := false
	for _, version := range versions {
		if version.Version == "21" && version.LatestPatch == "21.0.13.1+2" {
			found = true
		}
	}
	if !found {
		t.Fatal("ListVersions() dropped major 21")
	}
	for _, req := range *recorded {
		if req.method == http.MethodHead || strings.Contains(req.path, "/releases/tag/") {
			t.Errorf("ListVersions called %s %s", req.method, req.path)
		}
	}
}

func TestTemurinAdapter_ListVersions_FeatureLineError(t *testing.T) {
	lines := standardLines()
	for i := range lines {
		if lines[i].major == 21 {
			lines[i].featureStatus = http.StatusInternalServerError
		}
	}
	adapter, _ := newAdoptiumTestAdapter(t, lines)

	versions, err := adapter.ListVersions(context.Background())
	if err != nil {
		t.Fatalf("ListVersions() error = %v, want the other lines", err)
	}
	for _, version := range versions {
		if version.Version == "21" {
			t.Fatal("ListVersions() kept major 21 after the feature-release API failed")
		}
	}
	if len(versions) == 0 {
		t.Fatal("ListVersions() returned no versions")
	}
}

func TestTemurinAdapter_GetMaintainedVersions(t *testing.T) {
	adapter, _ := newAdoptiumTestAdapter(t, standardLines())

	versions, err := adapter.GetMaintainedVersions(context.Background())
	if err != nil {
		t.Fatalf("GetMaintainedVersions() error = %v", err)
	}

	found := map[string]bool{}
	for _, version := range versions {
		found[version.Version] = true
		if version.IsEOL {
			t.Errorf("maintained list included EOL %s", version.Version)
		}
	}
	if !found["21"] || !found["27"] {
		t.Fatalf("maintained versions = %v, want 21 and 27", found)
	}
	if found["26"] {
		t.Fatal("maintained versions included EOL 26")
	}
}

func TestTemurinAdapter_GetLatestVersion(t *testing.T) {
	adapter, _ := newAdoptiumTestAdapter(t, standardLines())

	tests := []struct {
		name    string
		opts    runtime.VersionOptions
		want    string
		wantErr bool
	}{
		{name: "default options", opts: runtime.VersionOptions{}, want: "27"},
		{name: "LTS only", opts: runtime.VersionOptions{LTSOnly: true}, want: "25"},
		{name: "recommended only", opts: runtime.VersionOptions{RecommendedOnly: true}, want: "25"},
		{name: "major 21", opts: runtime.VersionOptions{VersionPattern: "21"}, want: "21"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			version, err := adapter.GetLatestVersion(context.Background(), tt.opts)
			if (err != nil) != tt.wantErr {
				t.Fatalf("GetLatestVersion() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if version.Version != tt.want {
				t.Errorf("GetLatestVersion() version = %s, want %s", version.Version, tt.want)
			}
			if version.RuntimeName != "temurin" {
				t.Errorf("GetLatestVersion() runtime = %s, want temurin", version.RuntimeName)
			}
			if tt.opts.LTSOnly && !version.IsLTS {
				t.Error("GetLatestVersion() with LTSOnly returned non-LTS version")
			}
		})
	}
}

func TestTemurinAdapter_CreateDownloadTasks_UsesAPIPackages(t *testing.T) {
	adapter, recorded := newAdoptiumTestAdapter(t, standardLines())

	tasks, err := adapter.CreateDownloadTasks(endoflife.VersionInfo{
		Version:     "17",
		LatestPatch: "17.0.21.1+2",
		RuntimeName: "temurin",
	}, []platform.Platform{
		{OS: "linux", Arch: "x64", FileExt: "tar.gz", Classifier: "linux-x64"},
		{OS: "linux", Arch: "aarch64", FileExt: "tar.gz", Classifier: "linux-aarch64"},
	}, t.TempDir())
	if err != nil {
		t.Fatalf("CreateDownloadTasks() error = %v", err)
	}
	if len(tasks) != 6 {
		t.Fatalf("CreateDownloadTasks() returned %d tasks, want 6 from the API packages", len(tasks))
	}
	for _, req := range *recorded {
		if req.method == http.MethodHead {
			t.Errorf("CreateDownloadTasks called HEAD %s", req.path)
		}
	}
}

func TestReleaseVersion(t *testing.T) {
	tests := []struct {
		releaseName string
		want        string
	}{
		{releaseName: "jdk-21.0.13.1+2", want: "21.0.13.1+2"},
		{releaseName: "jdk-11.0.33+10", want: "11.0.33+10"},
		{releaseName: "jdk-25+36", want: "25+36"},
		{releaseName: "jdk8u512-b09", want: "8u512-b09"},
		{releaseName: "21.0.13.1+2", want: "21.0.13.1+2"},
		{releaseName: "jdk", want: "jdk"},
		{releaseName: "", want: ""},
	}
	for _, tt := range tests {
		if got := releaseVersion(tt.releaseName); got != tt.want {
			t.Errorf("releaseVersion(%q) = %q, want %q", tt.releaseName, got, tt.want)
		}
	}
}

func TestTemurinAdapter_ListVersions_PatchReadableDownstream(t *testing.T) {
	adapter, _ := newAdoptiumTestAdapter(t, standardLines())

	versions, err := adapter.ListVersions(context.Background())
	if err != nil {
		t.Fatalf("ListVersions() error = %v", err)
	}
	for _, version := range versions {
		if strings.HasPrefix(version.LatestPatch, "jdk") {
			t.Errorf("major %s LatestPatch = %s, want no jdk prefix", version.Version, version.LatestPatch)
		}
		if version.Version == "8" {
			continue
		}
		major, _, _, err := storage.ParseSemver(version.LatestPatch)
		if err != nil {
			t.Errorf("ParseSemver(%s) error = %v", version.LatestPatch, err)
			continue
		}
		if strconv.Itoa(major) != version.Version {
			t.Errorf("ParseSemver(%s) major = %d, want %s", version.LatestPatch, major, version.Version)
		}
	}
}

func sawRequest(recorded []recordedRequest, method, path string) bool {
	for _, req := range recorded {
		if req.method == method && req.path == path {
			return true
		}
	}
	return false
}

func TestTemurinAdapter_LoadPolicy(t *testing.T) {
	adapter := NewAdapter()
	policyPath := createTestTemurinPolicyFile(t)

	policy, err := adapter.LoadPolicy(policyPath)
	if err != nil {
		t.Errorf("LoadPolicy() error = %v", err)
		return
	}

	if len(policy) == 0 {
		t.Error("LoadPolicy() returned empty policy")
	}

	// Check that we have expected versions
	hasV21 := false
	hasV17 := false
	hasV11 := false

	for _, pv := range policy {
		switch pv.Version {
		case "21":
			hasV21 = true
			if !pv.Supported {
				t.Error("Temurin 21 should be supported in test policy")
			}
			if !pv.LTS {
				t.Error("Temurin 21 should be LTS in test policy")
			}
		case "17":
			hasV17 = true
			if !pv.Supported {
				t.Error("Temurin 17 should be supported in test policy")
			}
			if !pv.LTS {
				t.Error("Temurin 17 should be LTS in test policy")
			}
		case "11":
			hasV11 = true
			if !pv.Supported {
				t.Error("Temurin 11 should be supported in test policy")
			}
			if !pv.LTS {
				t.Error("Temurin 11 should be LTS in test policy")
			}
		}
	}

	if !hasV21 || !hasV17 || !hasV11 {
		t.Errorf("LoadPolicy() missing expected versions. Got: %v", policy)
	}
}

func TestTemurinAdapter_LoadPolicy_NonExistentFile(t *testing.T) {
	adapter := NewAdapter()

	_, err := adapter.LoadPolicy("/nonexistent/path/policy.json")
	if err == nil {
		t.Error("LoadPolicy() should have failed for non-existent file")
	}
}

func TestTemurinAdapter_ApplyPolicy(t *testing.T) {
	adapter := NewAdapter()

	versions := []endoflife.VersionInfo{
		{Version: "21", LatestPatch: "21.0.13.1+2", IsLTS: true},
		{Version: "17", LatestPatch: "17.0.21.1+2", IsLTS: true},
		{Version: "8", LatestPatch: "8u512-b09", IsLTS: true},
	}

	policy := []endoflife.PolicyVersion{
		{Version: "21", Supported: true, LTS: true},
		{Version: "17", Supported: true, LTS: true},
		{Version: "8", Supported: false},
	}

	filtered, err := adapter.ApplyPolicy(versions, policy)
	if err != nil {
		t.Errorf("ApplyPolicy() error = %v", err)
		return
	}

	if len(filtered) != 2 {
		t.Fatalf("ApplyPolicy() returned %d versions, want 2", len(filtered))
	}

	kept := map[string]string{}
	for _, version := range filtered {
		kept[version.Version] = version.LatestPatch
	}
	if kept["21"] != "21.0.13.1+2" {
		t.Errorf("ApplyPolicy() latest patch for 21 = %s", kept["21"])
	}
	if kept["17"] != "17.0.21.1+2" {
		t.Errorf("ApplyPolicy() latest patch for 17 = %s", kept["17"])
	}
	if _, ok := kept["8"]; ok {
		t.Error("ApplyPolicy() kept unsupported major 8")
	}
}

func TestTemurinAdapter_CreateDownloadTasks_FromFeatureRelease(t *testing.T) {
	lines := standardLines()
	for i := range lines {
		if lines[i].major == 21 {
			lines[i].platforms = []string{"linux-x64"}
		}
	}
	adapter, recorded := newAdoptiumTestAdapter(t, lines)

	if _, err := adapter.ListVersions(context.Background()); err != nil {
		t.Fatalf("ListVersions() error = %v", err)
	}

	tasks, err := adapter.CreateDownloadTasks(endoflife.VersionInfo{
		Version:     "21",
		LatestPatch: "21.0.13.1+2",
		RuntimeName: "temurin",
	}, []platform.Platform{
		{OS: "linux", Arch: "x64", FileExt: "tar.gz", Classifier: "linux-x64"},
		{OS: "windows", Arch: "x64", FileExt: "zip", Classifier: "windows-x64"},
	}, t.TempDir())
	if err != nil {
		t.Fatalf("CreateDownloadTasks() error = %v", err)
	}
	if len(tasks) != 3 {
		t.Fatalf("CreateDownloadTasks() returned %d tasks, want 3 for linux-x64", len(tasks))
	}
	for _, task := range tasks {
		if task.Platform.OS != "linux" {
			t.Errorf("task platform = %s, want linux", task.Platform.OS)
		}
		if task.Version != "21.0.13.1+2" {
			t.Errorf("task version = %s, want the release version without the jdk prefix", task.Version)
		}
	}
	for _, req := range *recorded {
		if req.method == http.MethodHead {
			t.Errorf("CreateDownloadTasks called HEAD %s", req.path)
		}
		if strings.Contains(req.path, "/assets/version/") {
			t.Errorf("requested %s", req.path)
		}
		if strings.Contains(req.path, "/feature_releases/21/") && strings.Contains(req.query, "page=1") {
			t.Error("download requested the previous GA page")
		}
	}
}

func TestTemurinAdapter_CreateDownloadTasks_NoPackages(t *testing.T) {
	adapter, _ := newAdoptiumTestAdapter(t, standardLines())

	tasks, err := adapter.CreateDownloadTasks(endoflife.VersionInfo{
		Version:     "21",
		LatestPatch: "21.0.13.1+2",
		RuntimeName: "temurin",
	}, []platform.Platform{
		{OS: "windows", Arch: "x64", FileExt: "zip", Classifier: "windows-x64"},
	}, t.TempDir())
	if err != nil {
		t.Fatalf("CreateDownloadTasks() error = %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("CreateDownloadTasks() returned %d tasks, want none", len(tasks))
	}
}

func TestTemurinAdapter_CreateDownloadTasks(t *testing.T) {
	// Skip this test in short mode as it makes real API calls
	if testing.Short() {
		t.Skip("Skipping TestTemurinAdapter_CreateDownloadTasks in short mode")
	}

	adapter := NewAdapter()

	version := endoflife.VersionInfo{
		Version:     "21",
		LatestPatch: "21",
		RuntimeName: "temurin",
		IsSupported: true,
	}

	platforms := []platform.Platform{
		{OS: "linux", Arch: "x64", FileExt: "tar.gz", Classifier: "linux-x64"},
	}

	outputDir := "/tmp/test"

	tasks, err := adapter.CreateDownloadTasks(version, platforms, outputDir)
	if err != nil {
		t.Errorf("CreateDownloadTasks() error = %v", err)
		return
	}

	// Each platform should generate 3 tasks: main, checksum, signature
	expectedTaskCount := len(platforms) * 3
	if len(tasks) != expectedTaskCount {
		t.Errorf("CreateDownloadTasks() returned %d tasks, want %d", len(tasks), expectedTaskCount)
		return
	}

	// Group tasks by platform and verify types
	tasksByPlatform := make(map[string][]runtime.DownloadTask)
	for _, task := range tasks {
		key := task.Platform.Classifier
		tasksByPlatform[key] = append(tasksByPlatform[key], task)
	}

	for platformKey, platformTasks := range tasksByPlatform {
		if len(platformTasks) != 3 {
			t.Errorf("Platform %s has %d tasks, want 3", platformKey, len(platformTasks))
			continue
		}

		// Check that we have all three file types
		hasMain := false
		hasChecksum := false
		hasSignature := false

		for _, task := range platformTasks {
			if task.Runtime != "temurin" {
				t.Errorf("Task runtime = %s, want temurin", task.Runtime)
			}

			switch task.FileType {
			case "main":
				hasMain = true
			case "checksum":
				hasChecksum = true
				if !strings.HasSuffix(task.URL, ".sha256.txt") {
					t.Errorf("Checksum task URL should end with .sha256.txt, got: %s", task.URL)
				}
			case "signature":
				hasSignature = true
				if !strings.HasSuffix(task.URL, ".sig") {
					t.Errorf("Signature task URL should end with .sig, got: %s", task.URL)
				}
			}

			// Check User-Agent header
			userAgent, exists := task.Headers["User-Agent"]
			if !exists {
				t.Errorf("Task missing User-Agent header")
			} else if userAgent != "downloadruntime/1.0 (temurin)" {
				t.Errorf("Task User-Agent = %s, want downloadruntime/1.0 (temurin)", userAgent)
			}
		}

		if !hasMain || !hasChecksum || !hasSignature {
			t.Errorf("Platform %s missing expected task types. main=%t, checksum=%t, signature=%t",
				platformKey, hasMain, hasChecksum, hasSignature)
		}
	}
}

func TestTemurinAdapter_ProcessDownloads(t *testing.T) {
	// Skip this test in short mode as it's time-consuming
	if testing.Short() {
		t.Skip("Skipping TestTemurinAdapter_ProcessDownloads in short mode")
	}

	adapter := NewAdapter()

	// Create test tasks that won't actually download (invalid URLs)
	tasks := []runtime.DownloadTask{
		{
			URL:        "http://example.com/test.tar.gz",
			OutputPath: "/tmp/test.tar.gz",
			Runtime:    "temurin",
			Version:    "21",
			FileType:   "main",
		},
	}

	// Test with different concurrency values
	tests := []struct {
		name        string
		concurrency int
		wantErr     bool
	}{
		{
			name:        "default concurrency",
			concurrency: 0, // Should default to 5
			wantErr:     false,
		},
		{
			name:        "custom concurrency",
			concurrency: 2,
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := adapter.ProcessDownloads(context.Background(), tasks, tt.concurrency)
			if (err != nil) != tt.wantErr {
				t.Errorf("ProcessDownloads() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if len(results) != len(tasks) {
				t.Errorf("ProcessDownloads() returned %d results, want %d", len(results), len(tasks))
			}
		})
	}
}

func TestTemurinAdapter_GetVerificationStrategy(t *testing.T) {
	adapter := NewAdapter()
	strategy := adapter.GetVerificationStrategy()

	if strategy == nil {
		t.Error("GetVerificationStrategy() returned nil")
		return
	}

	expectedType := "temurin-checksum-sha256"
	if strategy.GetType() != expectedType {
		t.Errorf("Verification strategy type = %s, want %s", strategy.GetType(), expectedType)
	}
}

func TestTemurinAdapter_GetAdoptiumAPI(t *testing.T) {
	// Skip this test if not in integration mode since it requires real API calls
	if testing.Short() {
		t.Skip("Skipping Adoptium API test in short mode")
	}

	adapter := NewAdapter().(*TemurinAdapter)

	release, err := adapter.fetchLatestFeatureRelease(context.Background(), 21)
	if err != nil {
		t.Fatalf("fetchLatestFeatureRelease(21) error = %v", err)
	}
	if release.ReleaseName == "" {
		t.Fatal("fetchLatestFeatureRelease(21) returned an empty release_name")
	}
	if len(release.Binaries) == 0 {
		t.Fatal("fetchLatestFeatureRelease(21) returned no binaries")
	}

	if _, err := adapter.fetchLatestFeatureRelease(context.Background(), 999); err == nil {
		t.Fatal("fetchLatestFeatureRelease(999) error = nil, want error")
	}
}

func TestTemurinAdapter_Integration(t *testing.T) {
	// Skip integration test if not in integration mode
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	adapter := NewAdapter().(*TemurinAdapter)

	// Test the complete flow
	ctx := context.Background()

	// List versions
	versions, err := adapter.ListVersions(ctx)
	if err != nil {
		t.Errorf("Integration test ListVersions() failed: %v", err)
		return
	}

	if len(versions) == 0 {
		t.Error("Integration test: no versions returned")
		return
	}

	// Verify version structure
	firstVersion := versions[0]
	if firstVersion.Version == "" {
		t.Error("Integration test: first version has empty version string")
	}

	if firstVersion.RuntimeName != "temurin" {
		t.Errorf("Integration test: version runtime name = %s, want temurin", firstVersion.RuntimeName)
	}

	// Check that we have LTS versions
	hasLTS := false
	for _, v := range versions {
		if v.IsLTS {
			hasLTS = true
			break
		}
	}

	if !hasLTS {
		t.Error("Integration test: no LTS versions found")
	}

	for _, version := range versions {
		if version.LatestPatch == "" {
			t.Errorf("version %s has an empty release name", version.Version)
			continue
		}
		major := extractMajorVersion(version.Version)
		release, err := adapter.fetchLatestFeatureRelease(ctx, major)
		if err != nil {
			t.Errorf("feature release for %s: %v", version.Version, err)
			continue
		}
		if want := releaseVersion(release.ReleaseName); version.LatestPatch != want {
			t.Errorf("version %s LatestPatch = %s, want %s from release_name %s", version.Version, version.LatestPatch, want, release.ReleaseName)
		}
	}

	t.Logf("Integration test: Found %d Temurin versions, latest: %s %s", len(versions), firstVersion.Version, firstVersion.LatestPatch)
}

func TestTemurinAdapter_LoadTemurinGPGKeys(t *testing.T) {
	adapter := NewAdapter().(*TemurinAdapter)

	keyRing, err := adapter.LoadTemurinGPGKeys()
	if err != nil {
		t.Fatalf("LoadTemurinGPGKeys() error = %v", err)
	}

	if keyRing == nil {
		t.Error("LoadTemurinGPGKeys() returned nil keyring")
	}

	// Test that the keyring has some basic functionality
	err = keyRing.VerifyDetached([]byte("test message"), []byte("test signature"))
	if err == nil {
		t.Log("Keyring verification test passed (unexpected but possible)")
	} else {
		// We expect this to fail since we're using mock data, but the keyring should be functional
		if strings.Contains(err.Error(), "no keys available") {
			t.Error("Keyring reports no keys available - key loading failed")
		} else {
			t.Logf("Keyring verification failed as expected: %v", err)
		}
	}
}

func TestTemurinVerificationStrategy(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	strategy := NewTemurinVerificationStrategy(logger, logger)
	if strategy == nil {
		t.Fatal("NewTemurinVerificationStrategy returned nil")
	}

	if strategy.GetType() != "temurin-checksum-sha256" {
		t.Errorf("Expected strategy type 'temurin-checksum-gpg', got %s", strategy.GetType())
	}

	if !strategy.RequiresAdditionalFiles() {
		t.Error("Expected strategy to require additional files")
	}

	t.Logf("Verification strategy: %s, requires additional files: %t",
		strategy.GetType(), strategy.RequiresAdditionalFiles())
}

func TestTemurinAdapter_VerifySignature(t *testing.T) {
	adapter := NewAdapter().(*TemurinAdapter)

	// Create test files
	tempDir := t.TempDir()
	dataFile := filepath.Join(tempDir, "test-data.txt")
	sigFile := filepath.Join(tempDir, "test-data.txt.sig")

	// Create a test data file
	testData := "Test content for Temurin GPG verification"
	err := os.WriteFile(dataFile, []byte(testData), 0644)
	if err != nil {
		t.Fatalf("Failed to create test data file: %v", err)
	}

	// Create a mock signature file (this will fail verification, but tests the loading)
	mockSig := "-----BEGIN PGP SIGNATURE-----\nMock signature for testing\n-----END PGP SIGNATURE-----"
	err = os.WriteFile(sigFile, []byte(mockSig), 0644)
	if err != nil {
		t.Fatalf("Failed to create test signature file: %v", err)
	}

	// Test verification (should fail due to mock signature, but should not error on key loading)
	err = adapter.VerifySignature(dataFile, sigFile)
	if err == nil {
		t.Log("Signature verification unexpectedly passed")
	} else {
		// We expect this to fail with a verification error, not a key loading error
		if strings.Contains(err.Error(), "failed to load") {
			t.Errorf("Key loading failed: %v", err)
		} else {
			t.Logf("Signature verification failed as expected: %v", err)
		}
	}
}

func TestTemurinGPGIntegration(t *testing.T) {
	// Test that embedded keys can be loaded and GPG verification system is functional
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	// Load keys directly from embedded filesystem
	keyRing, err := gpg.LoadKeyRingFromEmbedFS(embeddedTemurinKeys, "keys")
	if err != nil {
		t.Fatalf("Failed to load Temurin GPG keys from embedded filesystem: %v", err)
	}

	if keyRing == nil {
		t.Fatal("Keyring is nil")
	}

	logger.Info("Successfully loaded Temurin GPG keys for testing")
	t.Log("Temurin GPG key loading integration test passed")
}
