package cli

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/clean-dependency-project/cdprun/internal/config"
	"github.com/clean-dependency-project/cdprun/internal/runtime"
)

func makeVersionInfos(latestPatches ...string) []runtime.VersionInfo {
	vis := make([]runtime.VersionInfo, len(latestPatches))
	for i, lp := range latestPatches {
		vis[i] = runtime.VersionInfo{Version: lp, LatestPatch: lp}
	}
	return vis
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
}

func TestApplyUnsupportedFilter_BatchSkip(t *testing.T) {
	uc := config.UnsupportedConfig{
		"nodejs": {
			{Version: "16", EOLDate: "2023-09-11"},
			{Version: "14", EOLDate: "2023-04-30"},
		},
	}
	versions := makeVersionInfos("22.15.0", "20.19.1", "16.20.2", "14.21.3")

	got := applyUnsupportedFilter("nodejs", "", versions, uc, discardLogger())

	if len(got) != 2 {
		t.Fatalf("expected 2 versions after batch filter, got %d: %v", len(got), got)
	}
	for _, vi := range got {
		if vi.LatestPatch == "16.20.2" || vi.LatestPatch == "14.21.3" {
			t.Errorf("unsupported version %q should have been filtered out", vi.LatestPatch)
		}
	}
}

func TestApplyUnsupportedFilter_ExplicitWarnOnly(t *testing.T) {
	uc := config.UnsupportedConfig{
		"nodejs": {{Version: "16", EOLDate: "2023-09-11"}},
	}
	// Explicit --version path: version is NOT empty.
	versions := makeVersionInfos("16.20.2")

	got := applyUnsupportedFilter("nodejs", "16", versions, uc, discardLogger())

	// Version must be KEPT (warn only, not removed).
	if len(got) != 1 {
		t.Fatalf("expected 1 version (kept) for explicit path, got %d", len(got))
	}
	if got[0].LatestPatch != "16.20.2" {
		t.Errorf("expected 16.20.2 to be kept, got %q", got[0].LatestPatch)
	}
}

func TestApplyUnsupportedFilter_EmptyConfig(t *testing.T) {
	versions := makeVersionInfos("22.15.0", "20.19.1", "16.20.2")

	got := applyUnsupportedFilter("nodejs", "", versions, config.UnsupportedConfig{}, discardLogger())

	if len(got) != 3 {
		t.Errorf("expected all 3 versions to pass through empty config, got %d", len(got))
	}
}

func TestApplyUnsupportedFilter_UnknownRuntime(t *testing.T) {
	uc := config.UnsupportedConfig{
		"nodejs": {{Version: "16", EOLDate: "2023-09-11"}},
	}
	versions := makeVersionInfos("8.11.0") // "tomcat" has no rules

	got := applyUnsupportedFilter("tomcat", "", versions, uc, discardLogger())

	if len(got) != 1 {
		t.Errorf("expected 1 version for unknown runtime, got %d", len(got))
	}
}

func TestApplyUnsupportedFilter_FalsePositive(t *testing.T) {
	uc := config.UnsupportedConfig{
		"nodejs": {{Version: "16", EOLDate: "2023-09-11"}},
	}
	// "160.0.1" must NOT be filtered by prefix "16"
	versions := makeVersionInfos("160.0.1", "22.15.0")

	got := applyUnsupportedFilter("nodejs", "", versions, uc, discardLogger())

	if len(got) != 2 {
		t.Errorf("expected 160.0.1 to NOT be filtered by prefix '16', got %v", got)
	}
}
