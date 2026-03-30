// Package vscode provides a VSCode runtime adapter for the unified runtime download system.
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
	neturl "net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/clean-dependency-project/cdprun/internal/config"
	"github.com/clean-dependency-project/cdprun/internal/endoflife"
	"github.com/clean-dependency-project/cdprun/internal/platform"
	"github.com/clean-dependency-project/cdprun/internal/runtime"
	"github.com/clean-dependency-project/cdprun/internal/version"
)

const (
	VSCode         = "vscode"
	defaultBaseURL = "https://update.code.visualstudio.com"
)

type updateAPIResponse struct {
	URL            string `json:"url"`
	Name           string `json:"name"`
	ProductVersion string `json:"productVersion"`
	SHA256Hash     string `json:"sha256hash"`
}

// Adapter implements runtime.RuntimeProvider for VSCode latest downloads.
type Adapter struct {
	config       *config.Runtime
	globalConfig *config.GlobalConfig
	downloader   *runtime.ConcurrentDownloader
	stdout       *slog.Logger
	stderr       *slog.Logger
	httpClient   *http.Client

	mu                    sync.RWMutex
	expectedSHA256ByClass map[string]string
}

// NewAdapter creates a VSCode adapter with defaults.
func NewAdapter() runtime.RuntimeProvider {
	return NewAdapterWithConfig(nil, nil, slog.Default(), slog.Default())
}

// NewAdapterWithConfig creates a VSCode adapter with runtime config and loggers.
func NewAdapterWithConfig(cfg *config.Runtime, globalCfg *config.GlobalConfig, stdout, stderr *slog.Logger) runtime.RuntimeProvider {
	if stdout == nil {
		stdout = slog.Default()
	}
	if stderr == nil {
		stderr = slog.Default()
	}

	timeout := 30 * time.Second
	if globalCfg != nil {
		timeout = globalCfg.GetDownloadTimeout()
	}

	return &Adapter{
		config:                cfg,
		globalConfig:          globalCfg,
		downloader:            runtime.NewConcurrentDownloader(4, timeout, stdout, stderr),
		stdout:                stdout,
		stderr:                stderr,
		httpClient:            &http.Client{Timeout: timeout},
		expectedSHA256ByClass: make(map[string]string),
	}
}

func (a *Adapter) GetName() string {
	return VSCode
}

func (a *Adapter) GetEndOfLifeProduct() string {
	return VSCode
}

func (a *Adapter) GetSupportedPlatforms() []platform.Platform {
	if a.config != nil {
		return a.config.GetConfiguredPlatforms()
	}
	return []platform.Platform{
		{OS: "windows", Arch: "x64", FileExt: "exe", DownloadName: "win32-x64", Classifier: "windows-x64"},
		{OS: "windows", Arch: "aarch64", FileExt: "exe", DownloadName: "win32-arm64", Classifier: "windows-aarch64"},
		{OS: "mac", Arch: "x64", FileExt: "zip", DownloadName: "darwin-universal", Classifier: "mac-x64"},
		{OS: "mac", Arch: "aarch64", FileExt: "zip", DownloadName: "darwin-universal", Classifier: "mac-aarch64"},
	}
}

func (a *Adapter) ListVersions(ctx context.Context) ([]endoflife.VersionInfo, error) {
	latest, err := a.fetchLatestForPlatform(ctx, "darwin-universal")
	if err != nil {
		return nil, fmt.Errorf("fetch vscode latest version: %w", err)
	}
	versionValue := strings.TrimSpace(latest.ProductVersion)
	if versionValue == "" {
		versionValue = strings.TrimSpace(latest.Name)
	}
	if versionValue == "" {
		return nil, fmt.Errorf("empty version in vscode update response")
	}

	return []endoflife.VersionInfo{
		{
			Version:        versionValue,
			LatestPatch:    versionValue,
			IsSupported:    true,
			IsRecommended:  true,
			IsLTS:          false,
			IsEOL:          false,
			IsEOAS:         false,
			IsMaintained:   true,
			RuntimeName:    VSCode,
			VersionPattern: version.PatternMajor,
		},
	}, nil
}

func (a *Adapter) GetLatestVersion(ctx context.Context, opts runtime.VersionOptions) (endoflife.VersionInfo, error) {
	if opts.ExactMatch && strings.TrimSpace(opts.VersionPattern) != "" {
		v := strings.TrimSpace(opts.VersionPattern)
		return endoflife.VersionInfo{
			Version:        v,
			LatestPatch:    v,
			IsSupported:    true,
			IsRecommended:  true,
			IsLTS:          false,
			IsEOL:          false,
			IsEOAS:         false,
			IsMaintained:   true,
			RuntimeName:    VSCode,
			VersionPattern: version.PatternMajor,
		}, nil
	}

	versions, err := a.ListVersions(ctx)
	if err != nil {
		return endoflife.VersionInfo{}, err
	}
	if len(versions) == 0 {
		return endoflife.VersionInfo{}, fmt.Errorf("no vscode versions found")
	}
	return versions[0], nil
}

func (a *Adapter) CreateDownloadTasks(versionInfo endoflife.VersionInfo, platforms []platform.Platform, outputDir string) ([]runtime.DownloadTask, error) {
	baseURL := a.metadataBaseURL()
	userAgent := a.metadataUserAgent()

	tasks := make([]runtime.DownloadTask, 0, len(platforms))
	for _, plat := range platforms {
		apiPlatform, err := toVSCodeAPIPlatform(plat)
		if err != nil {
			return nil, err
		}
		resp, err := a.fetchLatestForPlatformWithBaseURL(context.Background(), baseURL, apiPlatform, userAgent)
		if err != nil {
			return nil, fmt.Errorf("fetch latest metadata for %s: %w", plat.Classifier, err)
		}
		if strings.TrimSpace(resp.URL) == "" || strings.TrimSpace(resp.SHA256Hash) == "" {
			return nil, fmt.Errorf("missing url or sha256hash in vscode update response for %s", plat.Classifier)
		}

		versionValue := strings.TrimSpace(resp.ProductVersion)
		if versionValue == "" {
			versionValue = strings.TrimSpace(resp.Name)
		}
		if versionValue == "" {
			versionValue = versionInfo.LatestPatch
		}
		if versionValue == "" {
			versionValue = versionInfo.Version
		}
		if versionValue == "" {
			return nil, fmt.Errorf("unable to determine version for %s", plat.Classifier)
		}

		ext := extensionFromDownloadURL(resp.URL)
		fileName := fmt.Sprintf("vscode-%s-%s%s", versionValue, plat.Classifier, ext)
		outputPath := filepath.Join(outputDir, plat.Classifier, fileName)

		a.setExpectedSHA256(plat.Classifier, resp.SHA256Hash)

		tasks = append(tasks, runtime.DownloadTask{
			URL:        resp.URL,
			OutputPath: outputPath,
			Platform:   plat,
			Runtime:    VSCode,
			Version:    versionValue,
			FileType:   "main",
			Headers: map[string]string{
				"User-Agent": userAgent,
				"Accept":     "application/json",
			},
		})
	}

	return tasks, nil
}

func (a *Adapter) ProcessDownloads(ctx context.Context, tasks []runtime.DownloadTask, concurrency int) ([]runtime.DownloadResult, error) {
	timeout := 30 * time.Second
	if a.globalConfig != nil {
		timeout = a.globalConfig.GetDownloadTimeout()
	}
	downloader := runtime.NewConcurrentDownloader(concurrency, timeout, a.stdout, a.stderr)
	return downloader.ProcessDownloads(ctx, tasks)
}

func (a *Adapter) GetVerificationStrategy() runtime.VerificationStrategy {
	return &sha256Verifier{adapter: a}
}

// VSCode intentionally does not use policy files.
func (a *Adapter) LoadPolicy(filePath string) ([]endoflife.PolicyVersion, error) {
	_ = filePath
	return []endoflife.PolicyVersion{}, nil
}

// VSCode intentionally does not use policy filtering.
func (a *Adapter) ApplyPolicy(versions []endoflife.VersionInfo, policy []endoflife.PolicyVersion) ([]endoflife.VersionInfo, error) {
	_ = policy
	return versions, nil
}

func (a *Adapter) GetMaintainedVersions(ctx context.Context) ([]endoflife.VersionInfo, error) {
	return a.ListVersions(ctx)
}

func (a *Adapter) fetchLatestForPlatform(ctx context.Context, platformID string) (*updateAPIResponse, error) {
	return a.fetchLatestForPlatformWithBaseURL(ctx, a.metadataBaseURL(), platformID, a.metadataUserAgent())
}

func (a *Adapter) fetchLatestForPlatformWithBaseURL(ctx context.Context, baseURL, platformID, userAgent string) (*updateAPIResponse, error) {
	urlValue := fmt.Sprintf("%s/api/update/%s/stable/latest", strings.TrimRight(baseURL, "/"), platformID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlValue, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request update api: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("update api status %d", resp.StatusCode)
	}

	var decoded updateAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("decode update api response: %w", err)
	}
	return &decoded, nil
}

func toVSCodeAPIPlatform(plat platform.Platform) (string, error) {
	osValue := strings.ToLower(strings.TrimSpace(plat.OS))
	archValue := strings.ToLower(strings.TrimSpace(plat.Arch))

	switch osValue {
	case "windows":
		if archValue == "x64" {
			return "win32-x64", nil
		}
		if archValue == "aarch64" || archValue == "arm64" {
			return "win32-arm64", nil
		}
	case "mac", "darwin":
		return "darwin-universal", nil
	}

	return "", fmt.Errorf("unsupported vscode platform %s-%s", plat.OS, plat.Arch)
}

func extensionFromDownloadURL(downloadURL string) string {
	parsed, err := neturl.Parse(downloadURL)
	if err != nil {
		return ".bin"
	}
	ext := path.Ext(parsed.Path)
	if strings.TrimSpace(ext) == "" {
		return ".bin"
	}
	return ext
}

func (a *Adapter) setExpectedSHA256(classifier, hash string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.expectedSHA256ByClass[classifier] = strings.ToLower(strings.TrimSpace(hash))
}

func (a *Adapter) expectedSHA256(classifier string) string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.expectedSHA256ByClass[classifier]
}

func (a *Adapter) metadataBaseURL() string {
	if a.config != nil && strings.TrimSpace(a.config.Download.BaseURL) != "" {
		return strings.TrimRight(strings.TrimSpace(a.config.Download.BaseURL), "/")
	}
	return defaultBaseURL
}

func (a *Adapter) metadataUserAgent() string {
	if a.config != nil && strings.TrimSpace(a.config.Download.UserAgent) != "" {
		return strings.TrimSpace(a.config.Download.UserAgent)
	}
	return "cdprun/1.0 (VSCode)"
}

type sha256Verifier struct {
	adapter *Adapter
}

func (v *sha256Verifier) Verify(ctx context.Context, result runtime.DownloadResult) error {
	_ = ctx
	expected := v.adapter.expectedSHA256(result.Platform.Classifier)
	if expected == "" {
		if err := runtime.WriteChecksumAuditRecord(result, "", "", false, "failed", "missing expected sha256 for platform"); err != nil {
			return fmt.Errorf("missing expected sha256 for platform %s and failed writing proof files: %w", result.Platform.Classifier, err)
		}
		return fmt.Errorf("missing expected sha256 for platform %s", result.Platform.Classifier)
	}

	file, err := os.Open(result.LocalPath)
	if err != nil {
		return fmt.Errorf("open downloaded file: %w", err)
	}
	defer func() { _ = file.Close() }()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		if proofErr := runtime.WriteChecksumAuditRecord(result, expected, "", false, "failed", "failed to compute sha256"); proofErr != nil {
			return fmt.Errorf("compute sha256 failed: %v; failed writing proof files: %w", err, proofErr)
		}
		return fmt.Errorf("compute sha256: %w", err)
	}
	actual := hex.EncodeToString(hash.Sum(nil))
	if actual != expected {
		if err := runtime.WriteChecksumAuditRecord(result, expected, actual, false, "failed", "sha256 mismatch"); err != nil {
			return fmt.Errorf("sha256 mismatch for %s and failed writing proof files: %w", result.Platform.Classifier, err)
		}
		return fmt.Errorf("sha256 mismatch for %s: expected %s got %s", result.Platform.Classifier, expected, actual)
	}

	if err := runtime.WriteChecksumAuditRecord(result, expected, actual, true, "success", ""); err != nil {
		return fmt.Errorf("failed writing proof files: %w", err)
	}
	return nil
}

func (v *sha256Verifier) GetType() string {
	return "checksum-sha256"
}

func (v *sha256Verifier) RequiresAdditionalFiles() bool {
	return false
}
