package hfpoc

import "testing"
import "time"

func TestExtractLicense(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		mi         ModelInfo
		wantLic    string
		wantSource string
	}{
		{
			name: "cardData_string",
			mi: ModelInfo{
				CardData: map[string]any{"license": "mit"},
				Tags:     []string{"license:apache-2.0"},
			},
			wantLic:    "mit",
			wantSource: "cardData.license",
		},
		{
			name: "tags_license",
			mi: ModelInfo{
				Tags: []string{"transformers", "license:apache-2.0"},
			},
			wantLic:    "apache-2.0",
			wantSource: "tags.license:*",
		},
		{
			name: "missing",
			mi:   ModelInfo{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lic, src := extractLicense(tt.mi)
			if lic != tt.wantLic {
				t.Fatalf("license: want %q got %q", tt.wantLic, lic)
			}
			if src != tt.wantSource {
				t.Fatalf("source: want %q got %q", tt.wantSource, src)
			}
		})
	}
}

func TestEvaluateModel_MetadataChecks(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 2, 18, 0, 0, 0, 0, time.UTC)
	mi := ModelInfo{
		Tags:      []string{"license:mit", "region:us"},
		Downloads: 10,
		CreatedAt: "2026-02-17T00:00:00.000Z",
		CardData:  map[string]any{"license": []any{"mit"}},
	}

	criteria := IntakeCriteria{
		AllowedLicenses:     []string{"apache-2.0"},
		AllowMissingLicense: false,
		RequiredCardFields:  []string{"license", "nonexistent"},
		DenyTags:            []string{"region:*"},
		MinDownloads30d:     100,
		MinAgeDays:          7,
	}

	_, _, reasons := evaluateModel(mi, criteria, nil, now)
	if len(reasons) == 0 {
		t.Fatalf("expected reasons, got none")
	}
}
