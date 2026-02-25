package cli

import (
	"testing"

	"github.com/clean-dependency-project/cdprun/internal/packageexec"
	"github.com/clean-dependency-project/cdprun/internal/promotion"
)

func TestManifestWithBuildStatus(t *testing.T) {
	in := PackageManifest{
		RunID: "run-1",
		Stage: "resolved",
		Targets: []PackageManifestEntry{
			{
				Runtime:       "nodejs",
				Version:       "22.22.0",
				Target:        "rpm",
				InputSHA256:   "sha-a",
				PackageName:   "OSPO-nodejs",
				InstallPrefix: "/export/apps/citools/OSPO-nodejs/22.22.0",
				Status:        PackageManifestStatus{Resolved: true},
			},
			{
				Runtime:       "nodejs",
				Version:       "22.23.0",
				Target:        "rpm",
				InputSHA256:   "sha-b",
				PackageName:   "OSPO-nodejs",
				InstallPrefix: "/export/apps/citools/OSPO-nodejs/22.23.0",
				Status:        PackageManifestStatus{Resolved: true},
			},
		},
	}
	builds := packageexec.BuildResultsFile{
		RunID: "run-1",
		Results: []packageexec.BuildResultRecord{
			{Target: packageexec.Target{Runtime: "nodejs", Version: "22.22.0", Target: "rpm", InputSHA256: "sha-a", PackageName: "OSPO-nodejs", InstallPrefix: "/export/apps/citools/OSPO-nodejs/22.22.0"}, Success: true},
			{Target: packageexec.Target{Runtime: "nodejs", Version: "22.23.0", Target: "rpm", InputSHA256: "sha-b", PackageName: "OSPO-nodejs", InstallPrefix: "/export/apps/citools/OSPO-nodejs/22.23.0"}, Success: false},
		},
	}

	out := manifestWithBuildStatus(in, builds)
	if out.Stage != "built" {
		t.Fatalf("stage = %q, want built", out.Stage)
	}
	if !out.Targets[0].Status.Built {
		t.Fatal("target[0].built = false, want true")
	}
	if out.Targets[1].Status.Built {
		t.Fatal("target[1].built = true, want false")
	}
}

func TestManifestWithTestStatus(t *testing.T) {
	in := PackageManifest{
		RunID: "run-1",
		Stage: "built",
		Targets: []PackageManifestEntry{
			{
				Runtime:       "nodejs",
				Version:       "22.22.0",
				Target:        "rpm",
				InputSHA256:   "sha-a",
				PackageName:   "OSPO-nodejs",
				InstallPrefix: "/export/apps/citools/OSPO-nodejs/22.22.0",
				Status:        PackageManifestStatus{Resolved: true, Built: true},
			},
			{
				Runtime:       "nodejs",
				Version:       "22.23.0",
				Target:        "rpm",
				InputSHA256:   "sha-b",
				PackageName:   "OSPO-nodejs",
				InstallPrefix: "/export/apps/citools/OSPO-nodejs/22.23.0",
				Status:        PackageManifestStatus{Resolved: true, Built: true},
			},
		},
	}
	tests := promotion.TestResultsFile{
		RunID: "run-1",
		Results: []promotion.TestResult{
			{Runtime: "nodejs", Version: "22.22.0", Target: "rpm", InputSHA256: "sha-a", PackageName: "OSPO-nodejs", InstallPrefix: "/export/apps/citools/OSPO-nodejs/22.22.0", Passed: true},
			{Runtime: "nodejs", Version: "22.23.0", Target: "rpm", InputSHA256: "sha-b", PackageName: "OSPO-nodejs", InstallPrefix: "/export/apps/citools/OSPO-nodejs/22.23.0", Passed: false},
		},
	}

	out := manifestWithTestStatus(in, tests)
	if out.Stage != "tested" {
		t.Fatalf("stage = %q, want tested", out.Stage)
	}
	if !out.Targets[0].Status.Tested {
		t.Fatal("target[0].tested = false, want true")
	}
	if out.Targets[1].Status.Tested {
		t.Fatal("target[1].tested = true, want false")
	}
}
