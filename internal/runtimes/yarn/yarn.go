// Package yarn provides a Yarn Classic runtime adapter for the unified runtime download system.
package yarn

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/clean-dependency-project/cdprun/internal/config"
	"github.com/clean-dependency-project/cdprun/internal/endoflife"
	"github.com/clean-dependency-project/cdprun/internal/platform"
	"github.com/clean-dependency-project/cdprun/internal/runtime"
	"github.com/clean-dependency-project/cdprun/internal/version"
)

const (
	Yarn        = "yarn"
	classicLine = "1"
)

// YarnAdapter implements runtime.RuntimeProvider for Yarn Classic (v1.x).
type YarnAdapter struct {
	endoflifeClient  endoflife.Client
	policyLoader     endoflife.PolicyLoader
	downloader       *runtime.ConcurrentDownloader
	config           *config.Runtime
	globalConfig     *config.GlobalConfig
	stdout           *slog.Logger
	stderr           *slog.Logger
	versionValidator version.Validator
}

type NoOpVerifier struct{}

func (v *NoOpVerifier) Verify(ctx context.Context, result runtime.DownloadResult) error {
	_ = ctx
	_ = result
	return nil
}

func (v *NoOpVerifier) GetType() string {
	return "none"
}

func (v *NoOpVerifier) RequiresAdditionalFiles() bool {
	return false
}

// NewAdapter creates a new Yarn runtime adapter.
func NewAdapter(eolClient endoflife.Client) runtime.RuntimeProvider {
	stdout := slog.Default()
	stderr := slog.Default()

	return &YarnAdapter{
		endoflifeClient:  eolClient,
		policyLoader:     endoflife.NewJSONPolicyLoader(),
		downloader:       runtime.NewConcurrentDownloader(5, 30*time.Second, stdout, stderr),
		stdout:           stdout,
		stderr:           stderr,
		versionValidator: version.New(),
	}
}

// NewAdapterWithConfig creates a new Yarn runtime adapter with configuration and loggers.
func NewAdapterWithConfig(eolClient endoflife.Client, cfg *config.Runtime, globalCfg *config.GlobalConfig, stdout, stderr *slog.Logger) runtime.RuntimeProvider {
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

	return &YarnAdapter{
		endoflifeClient:  eolClient,
		policyLoader:     endoflife.NewJSONPolicyLoader(),
		downloader:       runtime.NewConcurrentDownloader(5, timeout, stdout, stderr),
		config:           cfg,
		globalConfig:     globalCfg,
		stdout:           stdout,
		stderr:           stderr,
		versionValidator: version.New(),
	}
}

func (a *YarnAdapter) SetConfig(cfg *config.Runtime) {
	a.config = cfg
}

func (a *YarnAdapter) GetName() string {
	return Yarn
}

func (a *YarnAdapter) GetEndOfLifeProduct() string {
	return Yarn
}

func (a *YarnAdapter) GetSupportedPlatforms() []platform.Platform {
	if a.config != nil {
		return a.config.GetConfiguredPlatforms()
	}

	return []platform.Platform{
		{OS: "windows", Arch: "x64", FileExt: "msi", DownloadName: "windows", Classifier: "windows-x64"},
		{OS: "linux", Arch: "x64", FileExt: "tar.gz", DownloadName: "linux", Classifier: "linux-x64"},
		{OS: "mac", Arch: "x64", FileExt: "tar.gz", DownloadName: "mac", Classifier: "mac-x64"},
	}
}

// ListVersions returns Yarn Classic versions from endoflife.
func (a *YarnAdapter) ListVersions(ctx context.Context) ([]endoflife.VersionInfo, error) {
	if a.endoflifeClient == nil {
		return nil, fmt.Errorf("endoflife client is not initialized")
	}

	productInfo, err := a.endoflifeClient.GetProductInfo(ctx, Yarn)
	if err != nil {
		return nil, fmt.Errorf("failed to get Yarn product info: %w", err)
	}

	var versions []endoflife.VersionInfo
	for _, release := range productInfo.Result.Releases {
		if release.Name != classicLine {
			continue
		}

		versionInfo := endoflife.VersionInfo{
			Version:        release.Name,
			LatestPatch:    release.Latest.Name,
			IsSupported:    !release.IsEOL,
			IsRecommended:  !release.IsEOL,
			IsLTS:          false,
			IsEOL:          release.IsEOL,
			IsEOAS:         release.IsEOAS,
			IsMaintained:   release.IsMaintained,
			EOLDate:        "",
			ReleaseDate:    release.ReleaseDate,
			RuntimeName:    Yarn,
			VersionPattern: version.PatternMajor,
		}
		if release.EOLFrom != nil {
			versionInfo.EOLDate = *release.EOLFrom
		}

		versions = append(versions, versionInfo)
	}

	sort.Slice(versions, func(i, j int) bool {
		vi := versions[i].LatestPatch
		vj := versions[j].LatestPatch
		if vi == "" {
			vi = versions[i].Version
		}
		if vj == "" {
			vj = versions[j].Version
		}
		cmp, err := a.versionValidator.CompareVersions(vi, vj)
		if err != nil {
			return vi > vj
		}
		return cmp > 0
	})

	return versions, nil
}

func (a *YarnAdapter) GetLatestVersion(ctx context.Context, opts runtime.VersionOptions) (endoflife.VersionInfo, error) {
	// Allow exact mode for direct downloads.
	if opts.ExactMatch && opts.VersionPattern != "" {
		return endoflife.VersionInfo{
			Version:        classicLine,
			LatestPatch:    opts.VersionPattern,
			RuntimeName:    Yarn,
			IsSupported:    true,
			IsRecommended:  false,
			IsLTS:          false,
			IsEOL:          false,
			IsMaintained:   true,
			VersionPattern: version.PatternMajor,
		}, nil
	}

	versions, err := a.ListVersions(ctx)
	if err != nil {
		return endoflife.VersionInfo{}, fmt.Errorf("failed to list versions: %w", err)
	}
	if len(versions) == 0 {
		return endoflife.VersionInfo{}, fmt.Errorf("no Yarn versions available")
	}

	filtered := make([]endoflife.VersionInfo, 0, len(versions))
	for _, v := range versions {
		if opts.LTSOnly && !v.IsLTS {
			continue
		}
		if opts.RecommendedOnly && !v.IsRecommended {
			continue
		}
		if opts.VersionPattern != "" && v.Version != opts.VersionPattern && v.LatestPatch != opts.VersionPattern {
			continue
		}
		if !v.IsSupported && !opts.ExactMatch {
			continue
		}
		filtered = append(filtered, v)
	}

	if len(filtered) == 0 {
		return endoflife.VersionInfo{}, fmt.Errorf("no matching Yarn versions found")
	}

	return filtered[0], nil
}

func (a *YarnAdapter) CreateDownloadTasks(versionInfo endoflife.VersionInfo, platforms []platform.Platform, outputDir string) ([]runtime.DownloadTask, error) {
	userAgent := "cdprun/1.0 (Yarn)"
	if a.config != nil && a.config.Download.UserAgent != "" {
		userAgent = a.config.Download.UserAgent
	}

	requestVersion := versionInfo.LatestPatch
	if requestVersion == "" {
		requestVersion = versionInfo.Version
	}
	if requestVersion == "" {
		return nil, fmt.Errorf("empty version for Yarn download task creation")
	}

	var tasks []runtime.DownloadTask
	for _, plat := range platforms {
		filename := a.fileNameForPlatform(requestVersion, plat)
		url := a.constructDownloadURL(requestVersion, filename)
		platformDir := fmt.Sprintf("%s-%s", plat.OS, plat.Arch)
		outputPath := filepath.Join(outputDir, platformDir, filename)

		tasks = append(tasks, runtime.DownloadTask{
			URL:        url,
			OutputPath: outputPath,
			Platform:   plat,
			Runtime:    Yarn,
			Version:    requestVersion,
			FileType:   "main",
			Headers: map[string]string{
				"User-Agent": userAgent,
			},
		})

		if a.shouldDownloadChecksumFiles() {
			tasks = append(tasks, runtime.DownloadTask{
				URL:        url + ".sha256",
				OutputPath: outputPath + ".sha256",
				Platform:   plat,
				Runtime:    Yarn,
				Version:    requestVersion,
				FileType:   "checksum",
				Headers: map[string]string{
					"User-Agent": userAgent,
				},
				Optional: true,
			})
		}

		if a.shouldDownloadSignatureFiles() {
			tasks = append(tasks, runtime.DownloadTask{
				URL:        url + ".asc",
				OutputPath: outputPath + ".asc",
				Platform:   plat,
				Runtime:    Yarn,
				Version:    requestVersion,
				FileType:   "signature",
				Headers: map[string]string{
					"User-Agent": userAgent,
				},
				Optional: true,
			})
		}
	}

	return tasks, nil
}

func (a *YarnAdapter) ProcessDownloads(ctx context.Context, tasks []runtime.DownloadTask, concurrency int) ([]runtime.DownloadResult, error) {
	timeout := 30 * time.Second
	if a.globalConfig != nil {
		timeout = a.globalConfig.GetDownloadTimeout()
	}

	downloader := runtime.NewConcurrentDownloader(concurrency, timeout, a.stdout, a.stderr)
	return downloader.ProcessDownloads(ctx, tasks)
}

func (a *YarnAdapter) GetVerificationStrategy() runtime.VerificationStrategy {
	return &NoOpVerifier{}
}

func (a *YarnAdapter) LoadPolicy(filePath string) ([]endoflife.PolicyVersion, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read policy file: %w", err)
	}

	var policies []endoflife.PolicyVersion
	if err := json.Unmarshal(data, &policies); err != nil {
		return nil, fmt.Errorf("failed to parse policy file: %w", err)
	}
	return policies, nil
}

func (a *YarnAdapter) ApplyPolicy(versions []endoflife.VersionInfo, policyVersions []endoflife.PolicyVersion) ([]endoflife.VersionInfo, error) {
	if len(policyVersions) == 0 {
		return versions, nil
	}

	policyByVersion := make(map[string]endoflife.PolicyVersion, len(policyVersions))
	for _, pv := range policyVersions {
		policyByVersion[pv.Version] = pv
	}

	var filtered []endoflife.VersionInfo
	for _, v := range versions {
		policyVersion, ok := policyByVersion[v.Version]
		if !ok {
			continue
		}

		updated := v
		updated.IsSupported = policyVersion.Supported
		updated.IsRecommended = policyVersion.Recommended
		updated.IsLTS = policyVersion.LTS
		if policyVersion.LatestPatchVersion != "" {
			updated.LatestPatch = policyVersion.LatestPatchVersion
		}

		if policyVersion.Supported {
			filtered = append(filtered, updated)
		}
	}

	return filtered, nil
}

func (a *YarnAdapter) GetMaintainedVersions(ctx context.Context) ([]endoflife.VersionInfo, error) {
	if a.endoflifeClient == nil {
		return nil, fmt.Errorf("endoflife client is not initialized")
	}
	return a.endoflifeClient.GetMaintainedReleases(ctx, Yarn)
}

func (a *YarnAdapter) shouldDownloadChecksumFiles() bool {
	if a.config == nil {
		return false
	}
	return a.config.Verification.Enabled && a.config.Verification.Methods.Checksum.Enabled
}

func (a *YarnAdapter) shouldDownloadSignatureFiles() bool {
	if a.config == nil {
		return false
	}
	return a.config.Verification.Enabled && a.config.Verification.Methods.GPG.Enabled
}

func (a *YarnAdapter) constructDownloadURL(version, filename string) string {
	if a.config == nil {
		return fmt.Sprintf("https://github.com/yarnpkg/yarn/releases/download/v%s/%s", version, filename)
	}

	pattern := a.config.Download.URLPattern
	baseURL := a.config.Download.BaseURL
	if pattern == "" {
		pattern = "{base_url}/v{version}/{filename}"
	}

	url := strings.ReplaceAll(pattern, "{base_url}", baseURL)
	url = strings.ReplaceAll(url, "{version}", version)
	url = strings.ReplaceAll(url, "{filename}", filename)
	return url
}

func (a *YarnAdapter) fileNameForPlatform(version string, plat platform.Platform) string {
	// Normalize by OS first so explicit CLI platforms (e.g. windows-x64)
	// don't inherit generic extensions like .zip from shared platform maps.
	switch strings.ToLower(plat.OS) {
	case "windows":
		return fmt.Sprintf("yarn-%s.msi", version)
	case "linux", "mac", "darwin":
		return fmt.Sprintf("yarn-v%s.tar.gz", version)
	}

	ext := plat.FileExt
	if ext == "" {
		ext = "tar.gz"
	}

	switch strings.ToLower(ext) {
	case "msi":
		return fmt.Sprintf("yarn-%s.msi", version)
	case "js":
		return fmt.Sprintf("yarn-%s.js", version)
	case "rpm":
		return fmt.Sprintf("yarn-%s-1.noarch.rpm", version)
	default:
		return fmt.Sprintf("yarn-v%s.%s", version, ext)
	}
}
