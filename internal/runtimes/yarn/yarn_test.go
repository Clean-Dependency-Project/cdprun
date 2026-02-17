package yarn

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/clean-dependency-project/cdprun/internal/config"
	"github.com/clean-dependency-project/cdprun/internal/endoflife"
	"github.com/clean-dependency-project/cdprun/internal/platform"
	"github.com/clean-dependency-project/cdprun/internal/runtime"
)

type mockEndOfLifeClient struct {
	productInfo        *endoflife.ProductInfo
	maintainedReleases []endoflife.VersionInfo
	shouldError        bool
}

func (m *mockEndOfLifeClient) GetProductInfo(ctx context.Context, product string) (*endoflife.ProductInfo, error) {
	if m.shouldError {
		return nil, fmt.Errorf("mock error")
	}
	if m.productInfo != nil {
		return m.productInfo, nil
	}
	return &endoflife.ProductInfo{}, nil
}

func (m *mockEndOfLifeClient) GetSupportedVersions(ctx context.Context, runtime endoflife.PolicyRuntime) ([]endoflife.VersionInfo, error) {
	if m.shouldError {
		return nil, fmt.Errorf("mock error")
	}
	return nil, nil
}

func (m *mockEndOfLifeClient) ValidatePolicy(policy *endoflife.Policy) error {
	if m.shouldError {
		return fmt.Errorf("mock error")
	}
	return nil
}

func (m *mockEndOfLifeClient) EnrichVersionInfo(ctx context.Context, runtime endoflife.PolicyRuntime, policyVersion endoflife.PolicyVersion) (*endoflife.VersionInfo, error) {
	if m.shouldError {
		return nil, fmt.Errorf("mock error")
	}
	return &endoflife.VersionInfo{Version: policyVersion.Version}, nil
}

func (m *mockEndOfLifeClient) GetMaintainedReleases(ctx context.Context, product string) ([]endoflife.VersionInfo, error) {
	if m.shouldError {
		return nil, fmt.Errorf("mock error")
	}
	return m.maintainedReleases, nil
}

func TestYarnAdapter_ListVersions_ClassicOnly(t *testing.T) {
	mockClient := &mockEndOfLifeClient{
		productInfo: &endoflife.ProductInfo{
			Result: struct {
				Name           string                 `json:"name"`
				Aliases        []string               `json:"aliases"`
				Label          string                 `json:"label"`
				Category       string                 `json:"category"`
				Tags           []string               `json:"tags"`
				VersionCommand string                 `json:"versionCommand,omitempty"`
				Identifiers    []endoflife.Identifier `json:"identifiers,omitempty"`
				Labels         endoflife.Labels       `json:"labels,omitempty"`
				Links          endoflife.Links        `json:"links,omitempty"`
				Releases       []endoflife.Release    `json:"releases"`
			}{
				Name: "yarn",
				Releases: []endoflife.Release{
					{
						Name:         "4",
						IsEOL:        false,
						IsMaintained: true,
						Latest: struct {
							Name string `json:"name"`
							Date string `json:"date"`
							Link string `json:"link"`
						}{Name: "4.10.3"},
					},
					{
						Name:         "1",
						IsEOL:        false,
						IsMaintained: true,
						Latest: struct {
							Name string `json:"name"`
							Date string `json:"date"`
							Link string `json:"link"`
						}{Name: "1.22.22"},
					},
				},
			},
		},
	}

	adapter := NewAdapter(mockClient)
	versions, err := adapter.ListVersions(context.Background())
	if err != nil {
		t.Fatalf("ListVersions() error = %v", err)
	}
	if len(versions) != 1 {
		t.Fatalf("ListVersions() len = %d, want 1", len(versions))
	}
	if versions[0].Version != "1" {
		t.Fatalf("ListVersions() version = %q, want %q", versions[0].Version, "1")
	}
	if versions[0].LatestPatch != "1.22.22" {
		t.Fatalf("ListVersions() latest_patch = %q, want %q", versions[0].LatestPatch, "1.22.22")
	}
}

func TestYarnAdapter_CreateDownloadTasks(t *testing.T) {
	cfg := &config.Runtime{
		Download: config.DownloadConfig{
			BaseURL:    "https://github.com/yarnpkg/yarn/releases/download",
			URLPattern: "{base_url}/v{version}/{filename}",
		},
		Verification: config.Verification{
			Enabled: true,
			Methods: config.VerificationMethods{
				GPG: config.GPGVerification{
					Enabled: true,
				},
			},
		},
	}
	adapter := NewAdapterWithConfig(&mockEndOfLifeClient{}, cfg, &config.GlobalConfig{}, slog.Default(), slog.Default())
	yarnAdapter := adapter.(*YarnAdapter)

	tasks, err := yarnAdapter.CreateDownloadTasks(endoflife.VersionInfo{
		Version:     "1",
		LatestPatch: "1.22.22",
	}, []platform.Platform{
		{OS: "linux", Arch: "x64", FileExt: "tar.gz"},
		{OS: "windows", Arch: "x64", FileExt: "msi"},
	}, "/tmp/out")
	if err != nil {
		t.Fatalf("CreateDownloadTasks() error = %v", err)
	}

	if len(tasks) != 4 {
		t.Fatalf("CreateDownloadTasks() len = %d, want 4", len(tasks))
	}

	if tasks[0].URL != "https://github.com/yarnpkg/yarn/releases/download/v1.22.22/yarn-v1.22.22.tar.gz" {
		t.Fatalf("linux task URL = %q", tasks[0].URL)
	}
	if tasks[2].URL != "https://github.com/yarnpkg/yarn/releases/download/v1.22.22/yarn-1.22.22.msi" {
		t.Fatalf("windows task URL = %q", tasks[2].URL)
	}
}

func TestYarnAdapter_CreateDownloadTasks_WindowsZipPlatformStillUsesMSI(t *testing.T) {
	cfg := &config.Runtime{
		Download: config.DownloadConfig{
			BaseURL:    "https://github.com/yarnpkg/yarn/releases/download",
			URLPattern: "{base_url}/v{version}/{filename}",
		},
		Verification: config.Verification{
			Enabled: true,
			Methods: config.VerificationMethods{
				GPG: config.GPGVerification{
					Enabled: true,
				},
			},
		},
	}
	adapter := NewAdapterWithConfig(&mockEndOfLifeClient{}, cfg, &config.GlobalConfig{}, slog.Default(), slog.Default())
	yarnAdapter := adapter.(*YarnAdapter)

	tasks, err := yarnAdapter.CreateDownloadTasks(endoflife.VersionInfo{
		Version:     "1",
		LatestPatch: "1.22.22",
	}, []platform.Platform{
		// Simulates explicit CLI platform mapping that may carry generic zip extension.
		{OS: "windows", Arch: "x64", FileExt: "zip"},
	}, "/tmp/out")
	if err != nil {
		t.Fatalf("CreateDownloadTasks() error = %v", err)
	}
	if len(tasks) == 0 {
		t.Fatalf("CreateDownloadTasks() returned no tasks")
	}

	if tasks[0].URL != "https://github.com/yarnpkg/yarn/releases/download/v1.22.22/yarn-1.22.22.msi" {
		t.Fatalf("windows URL = %q, want msi URL", tasks[0].URL)
	}
	if filepath.Base(tasks[0].OutputPath) != "yarn-1.22.22.msi" {
		t.Fatalf("windows output = %q, want msi filename", filepath.Base(tasks[0].OutputPath))
	}
}

func TestYarnAdapter_LoadAndApplyPolicy(t *testing.T) {
	adapter := NewAdapter(&mockEndOfLifeClient{})
	tempDir := t.TempDir()
	policyPath := filepath.Join(tempDir, "yarn-policy.json")
	content := `[
		{"version":"1","supported":true,"recommended":true,"lts":false,"latest_patch_version":"1.22.22"},
		{"version":"4","supported":false,"recommended":false,"lts":false}
	]`
	if err := os.WriteFile(policyPath, []byte(content), 0644); err != nil {
		t.Fatalf("write policy: %v", err)
	}

	policy, err := adapter.LoadPolicy(policyPath)
	if err != nil {
		t.Fatalf("LoadPolicy() error = %v", err)
	}
	if len(policy) != 2 {
		t.Fatalf("LoadPolicy() len = %d, want 2", len(policy))
	}

	filtered, err := adapter.ApplyPolicy([]endoflife.VersionInfo{
		{Version: "1", LatestPatch: "1.22.20"},
		{Version: "4", LatestPatch: "4.10.3"},
	}, policy)
	if err != nil {
		t.Fatalf("ApplyPolicy() error = %v", err)
	}
	if len(filtered) != 1 || filtered[0].Version != "1" {
		t.Fatalf("ApplyPolicy() unexpected result = %+v", filtered)
	}
	if filtered[0].LatestPatch != "1.22.22" {
		t.Fatalf("ApplyPolicy() latest_patch = %q, want %q", filtered[0].LatestPatch, "1.22.22")
	}
}

func TestYarnAdapter_GetMaintainedVersions(t *testing.T) {
	adapter := NewAdapter(&mockEndOfLifeClient{
		maintainedReleases: []endoflife.VersionInfo{
			{Version: "1", LatestPatch: "1.22.22", IsMaintained: true},
		},
	})
	versions, err := adapter.GetMaintainedVersions(context.Background())
	if err != nil {
		t.Fatalf("GetMaintainedVersions() error = %v", err)
	}
	if len(versions) != 1 || versions[0].Version != "1" {
		t.Fatalf("GetMaintainedVersions() unexpected versions = %+v", versions)
	}
}

func TestYarnAdapter_GetLatestVersion_Exact(t *testing.T) {
	adapter := NewAdapter(&mockEndOfLifeClient{})
	v, err := adapter.GetLatestVersion(context.Background(), runtime.VersionOptions{
		ExactMatch:     true,
		VersionPattern: "1.22.22",
	})
	if err != nil {
		t.Fatalf("GetLatestVersion() error = %v", err)
	}
	if v.LatestPatch != "1.22.22" {
		t.Fatalf("GetLatestVersion() latest_patch = %q", v.LatestPatch)
	}
}
