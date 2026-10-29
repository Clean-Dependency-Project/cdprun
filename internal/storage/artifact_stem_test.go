package storage

import "testing"

func TestArtifactStem(t *testing.T) {
	tests := []struct {
		filename string
		want     string
	}{
		{
			filename: "vscode-1.141.0-mac-aarch64.zip",
			want:     "vscode-1.141.0-mac-aarch64",
		},
		{
			filename: "vscode-1.141.0-mac-aarch64.audit.json",
			want:     "vscode-1.141.0-mac-aarch64",
		},
		{
			filename: "vscode-1.141.0-windows-x64.exe",
			want:     "vscode-1.141.0-windows-x64",
		},
		{
			filename: "vscode-1.141.0-windows-x64.metadata.json",
			want:     "vscode-1.141.0-windows-x64",
		},
		{
			filename: "node-v22.15.0-linux-x64.tar.xz",
			want:     "node-v22.15.0-linux-x64",
		},
		{
			filename: "node-v22.15.0-linux-x64.tar.xz.audit.json",
			want:     "node-v22.15.0-linux-x64",
		},
		{
			filename: "apache-tomcat-10.1.60-windows-x64.zip",
			want:     "apache-tomcat-10.1.60-windows-x64",
		},
		{
			filename: "apache-tomcat-10.1.60-windows-x64.zip.audit.json",
			want:     "apache-tomcat-10.1.60-windows-x64",
		},
		{
			filename: "apache-tomcat-10.1.60.exe",
			want:     "apache-tomcat-10.1.60",
		},
		{
			filename: "apache-tomcat-10.1.60.exe.asc",
			want:     "apache-tomcat-10.1.60",
		},
		{
			filename: "OSPO-nodejs-24.15.0-1.amzn2023.x86_64.rpm.audit.json",
			want:     "OSPO-nodejs-24.15.0-1.amzn2023.x86_64",
		},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			got := ArtifactStem(tt.filename)
			if got != tt.want {
				t.Errorf("ArtifactStem(%q) = %q, want %q", tt.filename, got, tt.want)
			}
		})
	}
}

func TestAttachSidecars_PairsVSCodeAudit(t *testing.T) {
	artifacts := ReleaseArtifacts{
		Platforms: []PlatformArtifact{
			{
				Platform:     "mac-aarch64",
				PlatformOS:   "mac",
				PlatformArch: "aarch64",
				Binary: &ArtifactFile{
					Filename: "vscode-1.141.0-mac-aarch64.zip",
					URL:      "https://example.com/vscode.zip",
				},
			},
			{
				Platform:     "mac-aarch64",
				PlatformOS:   "mac",
				PlatformArch: "aarch64",
				Audit: &AuditArtifact{
					Filename: "vscode-1.141.0-mac-aarch64.audit.json",
					URL:      "https://example.com/vscode.audit.json",
				},
			},
			{
				Platform:     "mac-x64",
				PlatformOS:   "mac",
				PlatformArch: "x64",
				Binary: &ArtifactFile{
					Filename: "vscode-1.141.0-mac-x64.zip",
					URL:      "https://example.com/vscode-x64.zip",
				},
			},
		},
	}

	got := AttachSidecars(artifacts)
	if len(got.Platforms) != 2 {
		t.Fatalf("platforms = %d, want 2", len(got.Platforms))
	}

	var aarch64, x64 *PlatformArtifact
	for i := range got.Platforms {
		switch got.Platforms[i].PlatformArch {
		case "aarch64":
			aarch64 = &got.Platforms[i]
		case "x64":
			x64 = &got.Platforms[i]
		}
	}
	if aarch64 == nil || aarch64.Binary == nil || aarch64.Audit == nil {
		t.Fatalf("aarch64 artifact = %+v, want binary and audit", aarch64)
	}
	if aarch64.Audit.Filename != "vscode-1.141.0-mac-aarch64.audit.json" {
		t.Errorf("audit filename = %q", aarch64.Audit.Filename)
	}
	if x64 == nil || x64.Audit != nil {
		t.Fatalf("x64 artifact = %+v, want binary without the aarch64 audit", x64)
	}
}

func TestAttachSidecars_KeepsTomcatZipAndExeApart(t *testing.T) {
	artifacts := ReleaseArtifacts{
		Platforms: []PlatformArtifact{
			{
				Platform:     "windows-x64",
				PlatformOS:   "windows",
				PlatformArch: "x64",
				Binary:       &ArtifactFile{Filename: "apache-tomcat-10.1.60-windows-x64.zip"},
				Audit:        &AuditArtifact{Filename: "apache-tomcat-10.1.60-windows-x64.zip.audit.json"},
			},
			{
				Platform:     "windows-x64",
				PlatformOS:   "windows",
				PlatformArch: "x64",
				Binary:       &ArtifactFile{Filename: "apache-tomcat-10.1.60.exe"},
				Audit:        &AuditArtifact{Filename: "apache-tomcat-10.1.60.exe.audit.json"},
			},
		},
	}

	got := AttachSidecars(artifacts)
	if len(got.Platforms) != 2 {
		t.Fatalf("platforms = %d, want 2", len(got.Platforms))
	}
	for _, platform := range got.Platforms {
		if platform.Binary == nil || platform.Audit == nil {
			t.Fatalf("platform %+v lost its own binary or audit", platform)
		}
		if ArtifactStem(platform.Binary.Filename) != ArtifactStem(platform.Audit.Filename) {
			t.Errorf("binary %q paired with audit %q", platform.Binary.Filename, platform.Audit.Filename)
		}
	}
}
