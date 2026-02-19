package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/clean-dependency-project/cdprun/internal/endoflife"
	"github.com/clean-dependency-project/cdprun/internal/runtime"
)

// mockProvider for discovery tests
type mockDiscoveryProvider struct {
	runtime.RuntimeProvider
	policyLoaded bool
	apiCalled    bool
	versions     []runtime.VersionInfo
}

func (m *mockDiscoveryProvider) LoadPolicy(filePath string) ([]endoflife.PolicyVersion, error) {
	m.policyLoaded = true
	return []endoflife.PolicyVersion{{Version: "22", Supported: true}}, nil
}

func (m *mockDiscoveryProvider) ApplyPolicy(versions []endoflife.VersionInfo, policy []endoflife.PolicyVersion) ([]endoflife.VersionInfo, error) {
	return []endoflife.VersionInfo{{Version: "22"}}, nil
}

func (m *mockDiscoveryProvider) GetMaintainedVersions(ctx context.Context) ([]endoflife.VersionInfo, error) {
	m.apiCalled = true
	return m.versions, nil
}

func (m *mockDiscoveryProvider) GetEndOfLifeProduct() string {
	return "nodejs"
}

func TestDownloadVersionDiscovery(t *testing.T) {
	// This test simulates the logic in downloadSingleRuntime for version discovery

	tests := []struct {
		name           string
		setupPolicy    func(tempDir string) string
		expectedPolicy bool
		expectedAPI    bool
	}{
		{
			name: "uses policy file when present",
			setupPolicy: func(tempDir string) string {
				policyDir := filepath.Join(tempDir, "policies")
				os.MkdirAll(policyDir, 0755)
				policyPath := filepath.Join(policyDir, "nodejs-policy.json")
				os.WriteFile(policyPath, []byte(`[]`), 0644)
				return policyPath
			},
			expectedPolicy: true,
			expectedAPI:    false,
		},
		{
			name: "falls back to API when policy missing",
			setupPolicy: func(tempDir string) string {
				return filepath.Join(tempDir, "nonexistent-policy.json")
			},
			expectedPolicy: false,
			expectedAPI:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			policyPath := tt.setupPolicy(tmpDir)

			provider := &mockDiscoveryProvider{
				versions: []runtime.VersionInfo{{Version: "22"}},
			}

			// Simulate the discovery logic from cli.go
			var versions []runtime.VersionInfo
			var err error

			if _, statErr := os.Stat(policyPath); statErr == nil {
				_, err = provider.LoadPolicy(policyPath)
				if err == nil {
					versions, err = provider.ApplyPolicy(nil, nil)
				}
			} else {
				versions, err = provider.GetMaintainedVersions(context.Background())
			}

			if err != nil {
				t.Fatalf("Discovery logic failed: %v", err)
			}

			if len(versions) == 0 {
				t.Error("Expected versions to be found")
			}

			if provider.policyLoaded != tt.expectedPolicy {
				t.Errorf("policyLoaded = %v, want %v", provider.policyLoaded, tt.expectedPolicy)
			}

			if provider.apiCalled != tt.expectedAPI {
				t.Errorf("apiCalled = %v, want %v", provider.apiCalled, tt.expectedAPI)
			}
		})
	}
}
