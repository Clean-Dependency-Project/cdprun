// Package intellij provides an IntelliJ IDEA runtime adapter using JetBrains product releases JSON.
// Versions come only from data.services.jetbrains.com — there is no endoflife.date product for this IDE.
package intellij

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
	// IntelliJUltimateRuntime is the registry key for IntelliJ IDEA Ultimate (IIU).
	IntelliJUltimateRuntime = "intellij_idea_ultimate"
	defaultReleasesURL      = "https://data.services.jetbrains.com/products/releases"
	defaultProductCode      = "IIU"
)

type downloadMeta struct {
	Link         string `json:"link"`
	ChecksumLink string `json:"checksumLink"`
}

type releaseEntry struct {
	Version   string                  `json:"version"`
	Downloads map[string]downloadMeta `json:"downloads"`
}

// Adapter implements runtime.RuntimeProvider for JetBrains IntelliJ IDEA downloads.
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

// NewAdapterWithConfig builds an IntelliJ adapter (Ultimate by default when jetbrains_code is empty).
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
	return IntelliJUltimateRuntime
}

// GetEndOfLifeProduct returns empty: IntelliJ is not tracked on endoflife.date.
func (a *Adapter) GetEndOfLifeProduct() string {
	return ""
}

func (a *Adapter) productCode() string {
	if a.config != nil && strings.TrimSpace(a.config.Download.JetBrainsCode) != "" {
		return strings.TrimSpace(a.config.Download.JetBrainsCode)
	}
	return defaultProductCode
}

func (a *Adapter) releaseType() string {
	if a.config != nil && strings.TrimSpace(a.config.Download.JetBrainsType) != "" {
		return strings.TrimSpace(a.config.Download.JetBrainsType)
	}
	return "release"
}

func (a *Adapter) releasesBaseURL() string {
	if a.config != nil && strings.TrimSpace(a.config.Download.BaseURL) != "" {
		return strings.TrimRight(strings.TrimSpace(a.config.Download.BaseURL), "/")
	}
	return defaultReleasesURL
}

func (a *Adapter) metadataUserAgent() string {
	if a.config != nil && strings.TrimSpace(a.config.Download.UserAgent) != "" {
		return strings.TrimSpace(a.config.Download.UserAgent)
	}
	return "cdprun/1.0 (IntelliJ)"
}

func (a *Adapter) GetSupportedPlatforms() []platform.Platform {
	if a.config != nil {
		return a.config.GetConfiguredPlatforms()
	}
	return []platform.Platform{
		{OS: "windows", Arch: "x64", FileExt: "exe", DownloadName: "windows", Classifier: "windows-x64"},
		{OS: "windows", Arch: "aarch64", FileExt: "exe", DownloadName: "windowsARM64", Classifier: "windows-aarch64"},
		{OS: "mac", Arch: "aarch64", FileExt: "dmg", DownloadName: "macM1", Classifier: "mac-aarch64"},
	}
}

func (a *Adapter) ListVersions(ctx context.Context) ([]endoflife.VersionInfo, error) {
	rel, err := a.fetchLatestRelease(ctx, "")
	if err != nil {
		return nil, err
	}
	return []endoflife.VersionInfo{a.releaseToVersionInfo(rel)}, nil
}

func (a *Adapter) releaseToVersionInfo(rel *releaseEntry) endoflife.VersionInfo {
	v := strings.TrimSpace(rel.Version)
	return endoflife.VersionInfo{
		Version:        v,
		LatestPatch:    v,
		IsSupported:    true,
		IsRecommended:  true,
		IsLTS:          false,
		IsEOL:          false,
		IsEOAS:         false,
		IsMaintained:   true,
		RuntimeName:    IntelliJUltimateRuntime,
		VersionPattern: version.PatternMajor,
	}
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
			RuntimeName:    IntelliJUltimateRuntime,
			VersionPattern: version.PatternMajor,
		}, nil
	}

	pattern := strings.TrimSpace(opts.VersionPattern)
	var major string
	if isTwoSegmentMajor(pattern) {
		major = pattern
	}

	rel, err := a.fetchLatestRelease(ctx, major)
	if err != nil {
		return endoflife.VersionInfo{}, err
	}
	vi := a.releaseToVersionInfo(rel)
	return vi, nil
}

func (a *Adapter) CreateDownloadTasks(versionInfo endoflife.VersionInfo, platforms []platform.Platform, outputDir string) ([]runtime.DownloadTask, error) {
	major := majorVersionFromFullVersion(strings.TrimSpace(versionInfo.LatestPatch))
	if major == "" {
		major = majorVersionFromFullVersion(strings.TrimSpace(versionInfo.Version))
	}

	rel, err := a.fetchLatestRelease(context.Background(), major)
	if err != nil {
		return nil, err
	}

	userAgent := a.metadataUserAgent()

	tasks := make([]runtime.DownloadTask, 0, len(platforms))
	for _, plat := range platforms {
		key := strings.TrimSpace(plat.DownloadName)
		if key == "" {
			return nil, fmt.Errorf("empty download_name for platform %s", plat.Classifier)
		}
		artifact, ok := rel.Downloads[key]
		if !ok {
			return nil, fmt.Errorf("no %s download for JetBrains key %q", plat.Classifier, key)
		}
		if strings.TrimSpace(artifact.Link) == "" {
			return nil, fmt.Errorf("empty download link for %s (key %q)", plat.Classifier, key)
		}
		if strings.TrimSpace(artifact.ChecksumLink) == "" {
			return nil, fmt.Errorf("empty checksum link for %s (key %q)", plat.Classifier, key)
		}

		sum, err := a.fetchChecksumHex(context.Background(), artifact.ChecksumLink, userAgent)
		if err != nil {
			return nil, fmt.Errorf("checksum for %s: %w", plat.Classifier, err)
		}

		ver := strings.TrimSpace(rel.Version)
		if ver == "" {
			ver = strings.TrimSpace(versionInfo.LatestPatch)
		}
		if ver == "" {
			ver = strings.TrimSpace(versionInfo.Version)
		}
		if ver == "" {
			return nil, fmt.Errorf("unable to determine IntelliJ version for %s", plat.Classifier)
		}

		ext := extensionFromDownloadURL(artifact.Link)
		fileName := fmt.Sprintf("intellij-idea-%s-%s%s", ver, plat.Classifier, ext)
		outputPath := filepath.Join(outputDir, plat.Classifier, fileName)

		a.setExpectedSHA256(plat.Classifier, sum)

		tasks = append(tasks, runtime.DownloadTask{
			URL:        artifact.Link,
			OutputPath: outputPath,
			Platform:   plat,
			Runtime:    IntelliJUltimateRuntime,
			Version:    ver,
			FileType:   "main",
			Headers: map[string]string{
				"User-Agent": userAgent,
				"Accept":     "*/*",
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

// LoadPolicy is a no-op; IntelliJ versions come only from JetBrains releases JSON.
func (a *Adapter) LoadPolicy(filePath string) ([]endoflife.PolicyVersion, error) {
	_ = filePath
	return []endoflife.PolicyVersion{}, nil
}

// ApplyPolicy is a no-op.
func (a *Adapter) ApplyPolicy(versions []endoflife.VersionInfo, policy []endoflife.PolicyVersion) ([]endoflife.VersionInfo, error) {
	_ = policy
	return versions, nil
}

func (a *Adapter) GetMaintainedVersions(ctx context.Context) ([]endoflife.VersionInfo, error) {
	return a.ListVersions(ctx)
}

func (a *Adapter) fetchLatestRelease(ctx context.Context, majorVersion string) (*releaseEntry, error) {
	base := a.releasesBaseURL()
	q := neturl.Values{}
	q.Set("code", a.productCode())
	q.Set("latest", "true")
	q.Set("type", a.releaseType())
	if strings.TrimSpace(majorVersion) != "" {
		q.Set("majorVersion", strings.TrimSpace(majorVersion))
	}
	url := base + "?" + q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build jetbrains releases request: %w", err)
	}
	req.Header.Set("User-Agent", a.metadataUserAgent())
	req.Header.Set("Accept", "application/json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("jetbrains releases request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("jetbrains releases status %d", resp.StatusCode)
	}

	var decoded map[string][]releaseEntry
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("decode jetbrains releases: %w", err)
	}

	code := a.productCode()
	list := decoded[code]
	if len(list) == 0 {
		return nil, fmt.Errorf("jetbrains releases: empty list for code %q", code)
	}
	return &list[0], nil
}

func (a *Adapter) fetchChecksumHex(ctx context.Context, checksumURL, userAgent string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, checksumURL, nil)
	if err != nil {
		return "", fmt.Errorf("build checksum request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("checksum request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("checksum status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read checksum body: %w", err)
	}
	return parseChecksumLine(string(body))
}

func parseChecksumLine(body string) (string, error) {
	line := strings.TrimSpace(body)
	line = strings.TrimPrefix(line, "\uFEFF")
	fields := strings.Fields(line)
	if len(fields) < 1 {
		return "", fmt.Errorf("empty checksum file")
	}
	h := strings.ToLower(strings.TrimSpace(fields[0]))
	if len(h) != 64 {
		return "", fmt.Errorf("invalid sha256 in checksum file (len=%d)", len(h))
	}
	return h, nil
}

func majorVersionFromFullVersion(ver string) string {
	ver = strings.TrimSpace(ver)
	parts := strings.Split(ver, ".")
	if len(parts) >= 2 {
		return parts[0] + "." + parts[1]
	}
	return ""
}

func isTwoSegmentMajor(s string) bool {
	parts := strings.Split(strings.TrimSpace(s), ".")
	return len(parts) == 2 && parts[0] != "" && parts[1] != ""
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
