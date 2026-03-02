package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/clean-dependency-project/cdprun/internal/config"
	gh "github.com/clean-dependency-project/cdprun/internal/github"
	"github.com/clean-dependency-project/cdprun/internal/promotion"
	"github.com/google/go-github/v57/github"
)

// PromoteTestedPackagesFromFiles promotes tested package entries from files.
//
// It reads a manifest + test-result file, validates all entries up front, then
// updates release artifacts and package records only if all entries are eligible.
func PromoteTestedPackagesFromFiles(
	db promotion.Store,
	manifestPath string,
	testResultsPath string,
) (promotion.Summary, error) {
	return PromoteTestedPackagesFromFilesWithUploader(db, manifestPath, testResultsPath, nil)
}

// PromoteTestedPackagesFromFilesWithUploader promotes tested package entries and
// optionally uploads package assets to releases when URLs are missing.
func PromoteTestedPackagesFromFilesWithUploader(
	db promotion.Store,
	manifestPath string,
	testResultsPath string,
	uploader promotion.ReleaseAssetUploader,
) (promotion.Summary, error) {
	manifest, err := ReadPackageManifest(manifestPath)
	if err != nil {
		return promotion.Summary{}, fmt.Errorf("read package manifest: %w", err)
	}
	testResults, err := ReadPackageTestResultsFile(testResultsPath)
	if err != nil {
		return promotion.Summary{}, fmt.Errorf("read package test results: %w", err)
	}
	targets := make([]promotion.Target, 0, len(manifest.Targets))
	for _, target := range manifest.Targets {
		targets = append(targets, promotion.Target{
			Runtime:       target.Runtime,
			Version:       target.Version,
			Target:        target.Target,
			InputPlatform: target.InputPlatform,
			InputArch:     target.InputArch,
			InputSHA256:   target.InputSHA256,
			PackageName:   target.PackageName,
			InstallPrefix: target.InstallPrefix,
			Tested:        target.Status.Tested,
		})
	}
	return promotion.PromoteTestedPackages(db, uploader, manifest.RunID, targets, testResults)
}

type runtimeReleaseUploader struct {
	clientsByRuntime map[string]releaseAssetClient
}

type releaseAssetClient interface {
	GetRelease(tag string) (*github.RepositoryRelease, error)
	UploadAsset(releaseID int64, filePath string) (*github.ReleaseAsset, error)
	GetAssetDownloadURL(asset *github.ReleaseAsset) string
	FindReleaseAssetDownloadURL(releaseID int64, filename string) (string, error)
}

func (u *runtimeReleaseUploader) UploadPackageAsset(runtime, releaseTag, packagePath, _ string) (string, error) {
	client, ok := u.clientsByRuntime[strings.TrimSpace(runtime)]
	if !ok || client == nil {
		return "", fmt.Errorf("github client is not configured for runtime %q", runtime)
	}
	release, err := client.GetRelease(strings.TrimSpace(releaseTag))
	if err != nil {
		return "", fmt.Errorf("get release %q: %w", releaseTag, err)
	}

	asset, err := client.UploadAsset(release.GetID(), strings.TrimSpace(packagePath))
	if err == nil {
		url := strings.TrimSpace(client.GetAssetDownloadURL(asset))
		if url == "" {
			return "", fmt.Errorf("uploaded asset has empty download URL")
		}
		return url, nil
	}

	// Idempotent retry path: if the asset already exists on GitHub, reuse it.
	if strings.Contains(strings.ToLower(err.Error()), "already_exists") {
		filename := filepath.Base(strings.TrimSpace(packagePath))
		existingURL, lookupErr := client.FindReleaseAssetDownloadURL(release.GetID(), filename)
		if lookupErr != nil {
			return "", fmt.Errorf("lookup existing asset %q after already_exists: %w", filename, lookupErr)
		}
		if strings.TrimSpace(existingURL) == "" {
			return "", fmt.Errorf("asset %q already exists but download URL was not found", filename)
		}
		return strings.TrimSpace(existingURL), nil
	}
	return "", fmt.Errorf("upload asset %q: %w", packagePath, err)
}

func BuildPromotionUploader(configPath, token string, manifest PackageManifest) (promotion.ReleaseAssetUploader, error) {
	if strings.TrimSpace(token) == "" {
		return nil, nil
	}
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("load config for promotion uploader: %w", err)
	}

	runtimeSet := make(map[string]struct{})
	for _, target := range manifest.Targets {
		runtimeName := strings.TrimSpace(target.Runtime)
		if runtimeName == "" {
			continue
		}
		runtimeSet[runtimeName] = struct{}{}
	}

	clientsByRuntime := make(map[string]releaseAssetClient)
	for runtimeName := range runtimeSet {
		runtimeCfg, exists := cfg.GetRuntimeConfig(runtimeName)
		if !exists {
			return nil, fmt.Errorf("runtime %q not found/enabled in config", runtimeName)
		}
		repo := strings.TrimSpace(runtimeCfg.Release.GitHubRepository)
		if repo == "" {
			return nil, fmt.Errorf("runtime %q release.github_repository is required for promotion upload", runtimeName)
		}
		client, err := gh.NewClient(token, repo)
		if err != nil {
			return nil, fmt.Errorf("create github client for runtime %q: %w", runtimeName, err)
		}
		clientsByRuntime[runtimeName] = client
	}
	return &runtimeReleaseUploader{clientsByRuntime: clientsByRuntime}, nil
}

func UploadPromotionEvidenceFiles(
	uploader promotion.ReleaseAssetUploader,
	releaseTagsByRuntime map[string]string,
	testResultsPath string,
) error {
	if uploader == nil {
		return nil
	}
	if len(releaseTagsByRuntime) == 0 {
		return nil
	}

	artifactsDir := filepath.Dir(strings.TrimSpace(testResultsPath))
	evidenceFiles := []string{
		strings.TrimSpace(testResultsPath),
		filepath.Join(artifactsDir, "package-build-results.json"),
		filepath.Join(artifactsDir, "package-manifest.built.json"),
		filepath.Join(artifactsDir, "package-manifest.tested.json"),
	}
	seen := make(map[string]struct{})
	for _, runtimeName := range sortedRuntimeKeys(releaseTagsByRuntime) {
		releaseTag := releaseTagsByRuntime[runtimeName]
		for _, evidencePath := range evidenceFiles {
			cleanPath := strings.TrimSpace(evidencePath)
			if cleanPath == "" {
				continue
			}
			if _, exists := seen[cleanPath+"|"+runtimeName]; exists {
				continue
			}
			seen[cleanPath+"|"+runtimeName] = struct{}{}
			info, err := os.Stat(cleanPath)
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return fmt.Errorf("stat evidence file %q: %w", cleanPath, err)
			}
			if info.IsDir() || info.Size() == 0 {
				continue
			}
			if _, err := uploader.UploadPackageAsset(runtimeName, releaseTag, cleanPath, filepath.Base(cleanPath)); err != nil {
				return fmt.Errorf("upload promotion evidence %q for %s: %w", cleanPath, runtimeName, err)
			}
		}
	}
	return nil
}

func sortedRuntimeKeys(byRuntime map[string]string) []string {
	keys := make([]string, 0, len(byRuntime))
	for runtimeName := range byRuntime {
		keys = append(keys, runtimeName)
	}
	// Keep deterministic upload order for repeatability in logs/tests.
	for i := 0; i < len(keys)-1; i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[j] < keys[i] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	return keys
}

// ReadPackageManifest reads a package manifest JSON from disk.
func ReadPackageManifest(path string) (PackageManifest, error) {
	if strings.TrimSpace(path) == "" {
		return PackageManifest{}, fmt.Errorf("manifest path is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return PackageManifest{}, fmt.Errorf("read manifest file: %w", err)
	}
	var manifest PackageManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return PackageManifest{}, fmt.Errorf("unmarshal package manifest: %w", err)
	}
	return manifest, nil
}

// ReadPackageTestResultsFile reads package test results JSON from disk.
func ReadPackageTestResultsFile(path string) (promotion.TestResultsFile, error) {
	if strings.TrimSpace(path) == "" {
		return promotion.TestResultsFile{}, fmt.Errorf("test results path is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return promotion.TestResultsFile{}, fmt.Errorf("read test results file: %w", err)
	}
	var out promotion.TestResultsFile
	if err := json.Unmarshal(data, &out); err != nil {
		return promotion.TestResultsFile{}, fmt.Errorf("unmarshal package test results: %w", err)
	}
	return out, nil
}
