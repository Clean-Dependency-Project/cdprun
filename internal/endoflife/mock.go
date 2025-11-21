package endoflife

import (
	"context"
	"fmt"
	"time"
)

// MockClient implements Client interface for testing
type MockClient struct{}

// NewMockClient creates a new mock client
func NewMockClient() Client {
	return &MockClient{}
}

func (m *MockClient) GetProductInfo(ctx context.Context, product string) (*ProductInfo, error) {
	return &ProductInfo{
		SchemaVersion: "1.0",
		GeneratedAt:   time.Now().Format(time.RFC3339),
		LastModified:  time.Now().Format(time.RFC3339),
		Result: struct {
			Name           string       `json:"name"`
			Aliases        []string     `json:"aliases"`
			Label          string       `json:"label"`
			Category       string       `json:"category"`
			Tags           []string     `json:"tags"`
			VersionCommand string       `json:"versionCommand,omitempty"`
			Identifiers    []Identifier `json:"identifiers,omitempty"`
			Labels         Labels       `json:"labels,omitempty"`
			Links          Links        `json:"links,omitempty"`
			Releases       []Release    `json:"releases"`
		}{
			Name:     product,
			Label:    fmt.Sprintf("Mock %s", product),
			Category: "runtime",
			Releases: generateMockReleases(product),
		},
	}, nil
}

func (m *MockClient) GetSupportedVersions(ctx context.Context, runtime PolicyRuntime) ([]VersionInfo, error) {
	var versions []VersionInfo
	for _, pv := range runtime.Versions {
		if pv.Supported {
			versions = append(versions, VersionInfo{
				Version:       pv.Version,
				LatestPatch:   pv.LatestPatchVersion,
				IsSupported:   pv.Supported,
				IsRecommended: pv.Recommended,
				IsLTS:         pv.LTS,
				RuntimeName:   runtime.Name,
			})
		}
	}
	return versions, nil
}

func (m *MockClient) ValidatePolicy(policy *Policy) error {
	if policy == nil || len(policy.Runtimes) == 0 {
		return fmt.Errorf("invalid policy")
	}
	return nil
}

func (m *MockClient) EnrichVersionInfo(ctx context.Context, runtime PolicyRuntime, policyVersion PolicyVersion) (*VersionInfo, error) {
	return &VersionInfo{
		Version:       policyVersion.Version,
		IsSupported:   policyVersion.Supported,
		IsRecommended: policyVersion.Recommended,
		IsLTS:         policyVersion.LTS,
		RuntimeName:   runtime.Name,
	}, nil
}

func generateMockReleases(product string) []Release {
	switch product {
	case "python":
		return []Release{
			{
				Name:         "3.13",
				Label:        "3.13",
				ReleaseDate:  "2024-10-07",
				IsLTS:        false,
				IsEOL:        false,
				IsEOAS:       false,
				IsMaintained: true,
				Latest: struct {
					Name string `json:"name"`
					Date string `json:"date"`
					Link string `json:"link"`
				}{
					Name: "3.13.3",
					Date: "2025-04-08",
				},
			},
			{
				Name:         "3.12",
				Label:        "3.12",
				ReleaseDate:  "2023-10-02",
				IsLTS:        false,
				IsEOL:        false,
				IsEOAS:       true,
				IsMaintained: true,
				Latest: struct {
					Name string `json:"name"`
					Date string `json:"date"`
					Link string `json:"link"`
				}{
					Name: "3.12.10",
					Date: "2025-04-08",
				},
			},
		}
	case "nodejs":
		return []Release{
			{
				Name:         "22",
				Label:        "22",
				ReleaseDate:  "2024-04-24",
				IsLTS:        true,
				IsEOL:        false,
				IsEOAS:       false,
				IsMaintained: true,
				Latest: struct {
					Name string `json:"name"`
					Date string `json:"date"`
					Link string `json:"link"`
				}{
					Name: "22.16.0",
					Date: "2025-05-21",
				},
			},
			{
				Name:         "20",
				Label:        "20",
				ReleaseDate:  "2023-04-18",
				IsLTS:        true,
				IsEOL:        false,
				IsEOAS:       false,
				IsMaintained: true,
				Latest: struct {
					Name string `json:"name"`
					Date string `json:"date"`
					Link string `json:"link"`
				}{
					Name: "20.19.2",
					Date: "2025-05-14",
				},
			},
		}
	case "temurin":
		return []Release{
			{
				Name:         "21",
				Label:        "21",
				ReleaseDate:  "2023-09-19",
				IsLTS:        true,
				IsEOL:        false,
				IsEOAS:       false,
				IsMaintained: true,
				Latest: struct {
					Name string `json:"name"`
					Date string `json:"date"`
					Link string `json:"link"`
				}{
					Name: "21.0.4+7",
					Date: "2024-01-16",
				},
			},
			{
				Name:         "17",
				Label:        "17",
				ReleaseDate:  "2021-09-14",
				IsLTS:        true,
				IsEOL:        false,
				IsEOAS:       false,
				IsMaintained: true,
				Latest: struct {
					Name string `json:"name"`
					Date string `json:"date"`
					Link string `json:"link"`
				}{
					Name: "17.0.12+7",
					Date: "2024-07-16",
				},
			},
		}
	default:
		return []Release{
			{
				Name:         "1.0",
				Label:        "1.0",
				ReleaseDate:  "2024-01-01",
				IsLTS:        false,
				IsEOL:        false,
				IsEOAS:       false,
				IsMaintained: true,
				Latest: struct {
					Name string `json:"name"`
					Date string `json:"date"`
					Link string `json:"link"`
				}{
					Name: "1.0.1",
					Date: "2024-12-01",
				},
			},
		}
	}
}
