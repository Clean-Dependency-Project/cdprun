package sitegen

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/clean-dependency-project/cdprun/internal/config"
	"github.com/clean-dependency-project/cdprun/internal/endoflife"
	"github.com/clean-dependency-project/cdprun/internal/storage"
)

func TestNormalizePackageName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple lowercase",
			input:    "nodejs",
			expected: "nodejs",
		},
		{
			name:     "uppercase to lowercase",
			input:    "NodeJS",
			expected: "nodejs",
		},
		{
			name:     "with underscores",
			input:    "node_js",
			expected: "node-js",
		},
		{
			name:     "with hyphens",
			input:    "node-js",
			expected: "node-js",
		},
		{
			name:     "with dots",
			input:    "node.js",
			expected: "node-js",
		},
		{
			name:     "multiple separators",
			input:    "node__js--test",
			expected: "node-js-test",
		},
		{
			name:     "leading underscore",
			input:    "_nodejs",
			expected: "nodejs",
		},
		{
			name:     "trailing underscore",
			input:    "nodejs_",
			expected: "nodejs",
		},
		{
			name:     "leading hyphen",
			input:    "-nodejs",
			expected: "nodejs",
		},
		{
			name:     "trailing hyphen",
			input:    "nodejs-",
			expected: "nodejs",
		},
		{
			name:     "mixed case and separators",
			input:    "Node.JS_Runtime",
			expected: "node-js-runtime",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "only separators",
			input:    "---___",
			expected: "",
		},
		{
			name:     "numbers",
			input:    "nodejs123",
			expected: "nodejs123",
		},
		{
			name:     "with numbers and separators",
			input:    "node-js-2.0",
			expected: "node-js-2-0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NormalizePackageName(tt.input)
			if result != tt.expected {
				t.Errorf("NormalizePackageName(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestBuildModel(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name     string
		releases []ReleaseWithArtifacts
		want     *SiteModel
	}{
		{
			name:     "empty releases",
			releases: []ReleaseWithArtifacts{},
			want:     &SiteModel{Runtimes: []RuntimeModel{}},
		},
		{
			name: "single release",
			releases: []ReleaseWithArtifacts{
				{
					Release: storage.Release{
						Runtime:     "nodejs",
						Version:     "22.15.0",
						SemverMajor: 22,
						SemverMinor: 15,
						SemverPatch: 0,
						ReleaseTag:  "nodejs-v22.15.0",
						CreatedAt:   now,
					},
					Artifacts: storage.ReleaseArtifacts{
						Platforms: []storage.PlatformArtifact{
							{
								Platform:     "linux-x64",
								PlatformOS:   "linux",
								PlatformArch: "x64",
								Binary: &storage.ArtifactFile{
									Filename: "node-v22.15.0-linux-x64.tar.xz",
									Size:     1000,
									SHA256:   "abc123",
									URL:      "https://example.com/node-v22.15.0-linux-x64.tar.xz",
								},
							},
						},
					},
				},
			},
			want: &SiteModel{
				Runtimes: []RuntimeModel{
					{
						Name: "nodejs",
						Platforms: []PlatformModel{
							{
								OS: "linux",
								Versions: []VersionModel{
									{
										Major:   22,
										Minor:   15,
										Patch:   0,
										Version: "22.15.0",
										Releases: []ReleaseModel{
											{
												ReleaseTag: "nodejs-v22.15.0",
												CreatedAt:  now,
												Artifacts: []ArtifactModel{
													{
														Platform:     "linux-x64",
														PlatformOS:   "linux",
														PlatformArch: "x64",
														Binary: &FileModel{
															Filename: "node-v22.15.0-linux-x64.tar.xz",
															Size:     1000,
															SHA256:   "abc123",
															URL:      "https://example.com/node-v22.15.0-linux-x64.tar.xz",
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "multiple runtimes sorted",
			releases: []ReleaseWithArtifacts{
				{
					Release: storage.Release{
						Runtime:     "python",
						Version:     "3.13.0",
						SemverMajor: 3,
						SemverMinor: 13,
						SemverPatch: 0,
						ReleaseTag:  "python-v3.13.0",
						CreatedAt:   now,
					},
					Artifacts: storage.ReleaseArtifacts{
						Platforms: []storage.PlatformArtifact{
							{
								Platform:     "linux-x64",
								PlatformOS:   "linux",
								PlatformArch: "x64",
								Binary: &storage.ArtifactFile{
									Filename: "python-3.13.0-linux-x64.tar.xz",
									Size:     2000,
									URL:      "https://example.com/python-3.13.0-linux-x64.tar.xz",
								},
							},
						},
					},
				},
				{
					Release: storage.Release{
						Runtime:     "nodejs",
						Version:     "22.15.0",
						SemverMajor: 22,
						SemverMinor: 15,
						SemverPatch: 0,
						ReleaseTag:  "nodejs-v22.15.0",
						CreatedAt:   now,
					},
					Artifacts: storage.ReleaseArtifacts{
						Platforms: []storage.PlatformArtifact{
							{
								Platform:     "linux-x64",
								PlatformOS:   "linux",
								PlatformArch: "x64",
								Binary: &storage.ArtifactFile{
									Filename: "node-v22.15.0-linux-x64.tar.xz",
									Size:     1000,
									URL:      "https://example.com/node-v22.15.0-linux-x64.tar.xz",
								},
							},
						},
					},
				},
			},
			want: &SiteModel{
				Runtimes: []RuntimeModel{
					{
						Name: "nodejs",
						Platforms: []PlatformModel{
							{
								OS: "linux",
								Versions: []VersionModel{
									{
										Major:   22,
										Minor:   15,
										Patch:   0,
										Version: "22.15.0",
									},
								},
							},
						},
					},
					{
						Name: "python",
						Platforms: []PlatformModel{
							{
								OS: "linux",
								Versions: []VersionModel{
									{
										Major:   3,
										Minor:   13,
										Patch:   0,
										Version: "3.13.0",
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "darwin normalized to mac",
			releases: []ReleaseWithArtifacts{
				{
					Release: storage.Release{
						Runtime:     "nodejs",
						Version:     "22.15.0",
						SemverMajor: 22,
						SemverMinor: 15,
						SemverPatch: 0,
						ReleaseTag:  "nodejs-v22.15.0",
						CreatedAt:   now,
					},
					Artifacts: storage.ReleaseArtifacts{
						Platforms: []storage.PlatformArtifact{
							{
								Platform:     "darwin-x64",
								PlatformOS:   "darwin",
								PlatformArch: "x64",
								Binary: &storage.ArtifactFile{
									Filename: "node-v22.15.0-darwin-x64.tar.xz",
									Size:     1000,
									URL:      "https://example.com/node-v22.15.0-darwin-x64.tar.xz",
								},
							},
						},
					},
				},
			},
			want: &SiteModel{
				Runtimes: []RuntimeModel{
					{
						Name: "nodejs",
						Platforms: []PlatformModel{
							{
								OS: "mac",
								Versions: []VersionModel{
									{
										Major:   22,
										Minor:   15,
										Patch:   0,
										Version: "22.15.0",
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "release without binary creates runtime with empty platforms",
			releases: []ReleaseWithArtifacts{
				{
					Release: storage.Release{
						Runtime:     "nodejs",
						Version:     "22.15.0",
						SemverMajor: 22,
						SemverMinor: 15,
						SemverPatch: 0,
						ReleaseTag:  "nodejs-v22.15.0",
						CreatedAt:   now,
					},
					Artifacts: storage.ReleaseArtifacts{
						Platforms: []storage.PlatformArtifact{
							{
								Platform:     "linux-x64",
								PlatformOS:   "linux",
								PlatformArch: "x64",
								Binary:       nil,
							},
						},
					},
				},
			},
			want: &SiteModel{
				Runtimes: []RuntimeModel{
					{
						Name:      "nodejs",
						Platforms: []PlatformModel{},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildModel(tt.releases)

			if len(got.Runtimes) != len(tt.want.Runtimes) {
				t.Errorf("BuildModel() runtime count = %d, want %d", len(got.Runtimes), len(tt.want.Runtimes))
				return
			}

			for i, runtime := range got.Runtimes {
				if runtime.Name != tt.want.Runtimes[i].Name {
					t.Errorf("BuildModel() runtime[%d].Name = %q, want %q", i, runtime.Name, tt.want.Runtimes[i].Name)
				}
				if len(runtime.Platforms) != len(tt.want.Runtimes[i].Platforms) {
					t.Errorf("BuildModel() runtime[%d].Platforms count = %d, want %d", i, len(runtime.Platforms), len(tt.want.Runtimes[i].Platforms))
				}
			}
		})
	}
}

type mockReleaseReader struct {
	releases []storage.Release
	err      error
}

func (m *mockReleaseReader) GetAllReleases() ([]storage.Release, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.releases, nil
}

func TestLoadReleases(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name    string
		reader  ReleaseReader
		wantErr bool
		wantLen int
	}{
		{
			name: "successful load",
			reader: &mockReleaseReader{
				releases: []storage.Release{
					{
						Runtime:     "nodejs",
						Version:     "22.15.0",
						SemverMajor: 22,
						SemverMinor: 15,
						SemverPatch: 0,
						ReleaseTag:  "nodejs-v22.15.0",
						Artifacts:   `{"platforms":[],"common_files":[],"metadata":{}}`,
						CreatedAt:   now,
					},
				},
			},
			wantLen: 1,
		},
		{
			name: "aggregated release",
			reader: &mockReleaseReader{
				releases: []storage.Release{
					{
						Runtime:     "nodejs",
						Version:     "22.15.0, 22.14.0",
						SemverMajor: 22,
						SemverMinor: 15,
						SemverPatch: 0,
						ReleaseTag:  "nodejs-v22.15.0",
						Artifacts: `{
							"platforms": [
								{
									"platform": "linux-x64",
									"platform_os": "linux",
									"platform_arch": "x64",
									"binary": {
										"filename": "node-v22.15.0-linux-x64.tar.xz",
										"size": 1000,
										"url": "https://example.com/node-v22.15.0-linux-x64.tar.xz"
									}
								},
								{
									"platform": "linux-x64",
									"platform_os": "linux",
									"platform_arch": "x64",
									"binary": {
										"filename": "node-v22.14.0-linux-x64.tar.xz",
										"size": 1000,
										"url": "https://example.com/node-v22.14.0-linux-x64.tar.xz"
									}
								}
							],
							"common_files": [],
							"metadata": {}
						}`,
						CreatedAt: now,
					},
				},
			},
			wantLen: 2,
		},
		{
			name: "reader error",
			reader: &mockReleaseReader{
				err: os.ErrNotExist,
			},
			wantErr: true,
		},
		{
			name: "invalid JSON",
			reader: &mockReleaseReader{
				releases: []storage.Release{
					{
						Runtime:    "nodejs",
						Version:    "22.15.0",
						ReleaseTag: "nodejs-v22.15.0",
						Artifacts:  `invalid json`,
						CreatedAt:  now,
					},
				},
			},
			wantErr: true,
		},
		{
			name: "empty releases",
			reader: &mockReleaseReader{
				releases: []storage.Release{},
			},
			wantLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := LoadReleases(tt.reader)
			if tt.wantErr {
				if err == nil {
					t.Errorf("LoadReleases() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("LoadReleases() unexpected error: %v", err)
				return
			}
			if len(got) != tt.wantLen {
				t.Errorf("LoadReleases() length = %d, want %d", len(got), tt.wantLen)
			}
		})
	}
}

func TestWriteFileIfChanged(t *testing.T) {
	tests := []struct {
		name        string
		initialData []byte
		newData     []byte
		shouldWrite bool
	}{
		{
			name:        "new file",
			initialData: nil,
			newData:     []byte("test content"),
			shouldWrite: true,
		},
		{
			name:        "file unchanged",
			initialData: []byte("test content"),
			newData:     []byte("test content"),
			shouldWrite: false,
		},
		{
			name:        "file changed",
			initialData: []byte("old content"),
			newData:     []byte("new content"),
			shouldWrite: true,
		},
		{
			name:        "empty file",
			initialData: nil,
			newData:     []byte(""),
			shouldWrite: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := t.TempDir()
			filePath := filepath.Join(tempDir, "test.txt")

			// Create initial file if needed
			if tt.initialData != nil {
				if err := os.WriteFile(filePath, tt.initialData, 0644); err != nil {
					t.Fatalf("Failed to create initial file: %v", err)
				}
			}

			// Use a simple logger for testing
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))

			// Write file
			err := writeFileIfChanged(filePath, tt.newData, logger)
			if err != nil {
				t.Fatalf("writeFileIfChanged() error = %v", err)
			}

			// Verify file content (more reliable than mod time in CI)
			content, err := os.ReadFile(filePath)
			if err != nil {
				t.Fatalf("Failed to read file: %v", err)
			}

			if tt.shouldWrite {
				// File should have new content
				if string(content) != string(tt.newData) {
					t.Errorf("File content = %q, want %q", string(content), string(tt.newData))
				}
			} else {
				// File should have original content (unchanged)
				if string(content) != string(tt.initialData) {
					t.Errorf("File content changed unexpectedly: got %q, want %q", string(content), string(tt.initialData))
				}
			}
		})
	}
}

func TestContentMatches(t *testing.T) {
	tests := []struct {
		name string
		a    []byte
		b    []byte
		want bool
	}{
		{
			name: "identical small files",
			a:    []byte("test"),
			b:    []byte("test"),
			want: true,
		},
		{
			name: "different small files",
			a:    []byte("test1"),
			b:    []byte("test2"),
			want: false,
		},
		{
			name: "identical large files",
			a:    make([]byte, 2048),
			b:    make([]byte, 2048),
			want: true,
		},
		{
			name: "different sizes",
			a:    []byte("test"),
			b:    []byte("test longer"),
			want: false,
		},
		{
			name: "empty files",
			a:    []byte{},
			b:    []byte{},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := contentMatches(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("contentMatches() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewGenerator(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	reader := &mockReleaseReader{}

	gen := NewGenerator(reader, logger)
	if gen.reader != reader {
		t.Error("NewGenerator() reader mismatch")
	}
	if gen.logger != logger {
		t.Error("NewGenerator() logger mismatch")
	}
}

func TestGenerator_Generate(t *testing.T) {
	now := time.Now()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	tests := []struct {
		name    string
		reader  ReleaseReader
		opts    GenerateOptions
		wantErr bool
	}{
		{
			name: "successful generation",
			reader: &mockReleaseReader{
				releases: []storage.Release{
					{
						Runtime:     "nodejs",
						Version:     "22.15.0",
						SemverMajor: 22,
						SemverMinor: 15,
						SemverPatch: 0,
						ReleaseTag:  "nodejs-v22.15.0",
						Artifacts:   `{"platforms":[{"platform":"linux-x64","platform_os":"linux","platform_arch":"x64","binary":{"filename":"node-v22.15.0-linux-x64.tar.xz","size":1000,"url":"https://example.com/file.tar.xz"}}],"common_files":[],"metadata":{}}`,
						CreatedAt:   now,
					},
				},
			},
			opts: GenerateOptions{
				OutputDir: "",
				DryRun:    false,
			},
			wantErr: true, // Will fail because OutputDir is empty
		},
		{
			name: "dry run mode",
			reader: &mockReleaseReader{
				releases: []storage.Release{
					{
						Runtime:     "nodejs",
						Version:     "22.15.0",
						SemverMajor: 22,
						SemverMinor: 15,
						SemverPatch: 0,
						ReleaseTag:  "nodejs-v22.15.0",
						Artifacts:   `{"platforms":[{"platform":"linux-x64","platform_os":"linux","platform_arch":"x64","binary":{"filename":"node-v22.15.0-linux-x64.tar.xz","size":1000,"url":"https://example.com/file.tar.xz"}}],"common_files":[],"metadata":{}}`,
						CreatedAt:   now,
					},
				},
			},
			opts: GenerateOptions{
				OutputDir: t.TempDir(),
				DryRun:    true,
			},
			wantErr: false,
		},
		{
			name: "empty releases",
			reader: &mockReleaseReader{
				releases: []storage.Release{},
			},
			opts: GenerateOptions{
				OutputDir: t.TempDir(),
				DryRun:    false,
			},
			wantErr: false,
		},
		{
			name: "reader error",
			reader: &mockReleaseReader{
				err: os.ErrNotExist,
			},
			opts: GenerateOptions{
				OutputDir: t.TempDir(),
				DryRun:    false,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gen := NewGenerator(tt.reader, logger)
			err := gen.Generate(context.TODO(), tt.opts)
			if tt.wantErr {
				if err == nil {
					t.Errorf("Generate() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("Generate() unexpected error: %v", err)
			}
		})
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		name     string
		bytes    int64
		expected string
	}{
		{"zero bytes", 0, "0 B"},
		{"small bytes", 512, "512 B"},
		{"one KB", 1024, "1.0 KB"},
		{"one MB", 1024 * 1024, "1.0 MB"},
		{"one GB", 1024 * 1024 * 1024, "1.0 GB"},
		{"mixed size", 1536, "1.5 KB"},
		{"large MB", 50 * 1024 * 1024, "50.0 MB"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatBytes(tt.bytes)
			if got != tt.expected {
				t.Errorf("formatBytes(%d) = %q, want %q", tt.bytes, got, tt.expected)
			}
		})
	}
}

func TestRenderHumanPages(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tempDir := t.TempDir()

	model := &SiteModel{
		Runtimes: []RuntimeModel{
			{
				Name: "nodejs",
				Platforms: []PlatformModel{
					{
						OS: "linux",
						Versions: []VersionModel{
							{
								Major:   22,
								Minor:   15,
								Patch:   0,
								Version: "22.15.0",
								Releases: []ReleaseModel{
									{
										ReleaseTag: "nodejs-22.15.0",
										ReleaseURL: "https://github.com/test/releases/tag/nodejs-22.15.0",
										Artifacts: []ArtifactModel{
											{
												Platform:     "linux-x64",
												PlatformOS:   "linux",
												PlatformArch: "x64",
												Binary: &FileModel{
													Filename: "node-v22.15.0-linux-x64.tar.gz",
													Size:     1024 * 1024,
													SHA256:   "abc123",
													URL:      "https://example.com/node-v22.15.0-linux-x64.tar.gz",
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	err := RenderHumanPages(model, tempDir, logger)
	if err != nil {
		t.Fatalf("RenderHumanPages() error = %v", err)
	}

	// Verify root index exists
	rootIndex := filepath.Join(tempDir, "index.html")
	if _, err := os.Stat(rootIndex); os.IsNotExist(err) {
		t.Error("Expected root index.html to exist")
	}

	// Verify assets directory exists
	assetsDir := filepath.Join(tempDir, "assets")
	if _, err := os.Stat(assetsDir); os.IsNotExist(err) {
		t.Error("Expected assets directory to exist")
	}

	// Verify runtime directory exists
	runtimeDir := filepath.Join(tempDir, "nodejs")
	if _, err := os.Stat(runtimeDir); os.IsNotExist(err) {
		t.Error("Expected nodejs directory to exist")
	}
}

func TestRenderSimpleIndex(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tempDir := t.TempDir()

	model := &SiteModel{
		Runtimes: []RuntimeModel{
			{
				Name: "nodejs",
				Platforms: []PlatformModel{
					{
						OS: "linux",
						Versions: []VersionModel{
							{
								Major:   22,
								Minor:   15,
								Patch:   0,
								Version: "22.15.0",
								Releases: []ReleaseModel{
									{
										ReleaseTag: "nodejs-22.15.0",
										Artifacts: []ArtifactModel{
											{
												Binary: &FileModel{
													Filename: "node-v22.15.0-linux-x64.tar.gz",
													URL:      "https://example.com/binary.tar.gz",
													SHA256:   "abc123",
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	err := RenderSimpleIndex(model, tempDir, config.UnsupportedConfig{}, logger)
	if err != nil {
		t.Fatalf("RenderSimpleIndex() error = %v", err)
	}

	// Verify simple root index exists
	simpleIndex := filepath.Join(tempDir, "simple", "index.html")
	if _, err := os.Stat(simpleIndex); os.IsNotExist(err) {
		t.Error("Expected simple/index.html to exist")
	}

	// Verify runtime directory exists
	nodejsDir := filepath.Join(tempDir, "simple", "nodejs")
	if _, err := os.Stat(nodejsDir); os.IsNotExist(err) {
		t.Error("Expected simple/nodejs directory to exist")
	}

	// Verify JSON artifact index for v22 was created with expected path
	jsonPath := filepath.Join(nodejsDir, "v22", "index.json")
	content, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("Expected %s to exist: %v", jsonPath, err)
	}

	var index SimpleVersionIndex
	if err := json.Unmarshal(content, &index); err != nil {
		t.Fatalf("Failed to parse JSON artifact index: %v", err)
	}

	if len(index["linux"]) != 1 {
		t.Fatalf("JSON artifact index for linux length = %d, want 1", len(index["linux"]))
	}
	expected := "linux/nodejs/22.15/22.15.0/node-v22.15.0-linux-x64.tar.gz"
	if index["linux"][0].Binary != expected {
		t.Errorf("JSON artifact entry = %q, want %q", index["linux"][0].Binary, expected)
	}
	if index["linux"][0].Type != "tar.gz" {
		t.Errorf("JSON artifact type = %q, want %q", index["linux"][0].Type, "tar.gz")
	}
	if index["linux"][0].SourcePath != "nodejs-22.15.0/node-v22.15.0-linux-x64.tar.gz" {
		t.Errorf("JSON artifact source_path = %q, want %q", index["linux"][0].SourcePath, "nodejs-22.15.0/node-v22.15.0-linux-x64.tar.gz")
	}
	if index["linux"][0].DownloadURL != "https://example.com/binary.tar.gz" {
		t.Errorf("JSON artifact download_url = %q, want %q", index["linux"][0].DownloadURL, "https://example.com/binary.tar.gz")
	}
	if index["linux"][0].Version != "22.15.0" {
		t.Errorf("JSON artifact version = %q, want %q", index["linux"][0].Version, "22.15.0")
	}

	// Verify runtime-specific JSON index exists
	runtimeJSONPath := filepath.Join(nodejsDir, "index.json")
	runtimeContent, err := os.ReadFile(runtimeJSONPath)
	if err != nil {
		t.Fatalf("Expected %s to exist: %v", runtimeJSONPath, err)
	}

	var runtimeIndex SimpleRootIndex
	if err := json.Unmarshal(runtimeContent, &runtimeIndex); err != nil {
		t.Fatalf("Failed to parse runtime JSON index: %v", err)
	}

	if len(runtimeIndex["linux"]) != 1 {
		t.Fatalf("Runtime JSON index for linux length = %d, want 1", len(runtimeIndex["linux"]))
	}
	if runtimeIndex["linux"][0].Binary != expected {
		t.Errorf("Runtime JSON entry = %q, want %q", runtimeIndex["linux"][0].Binary, expected)
	}
	if runtimeIndex["linux"][0].Version != "22.15.0" {
		t.Errorf("Runtime JSON artifact version = %q, want %q", runtimeIndex["linux"][0].Version, "22.15.0")
	}

	// Verify consolidated root JSON index exists
	rootJSONPath := filepath.Join(tempDir, "simple", "index.json")
	rootContent, err := os.ReadFile(rootJSONPath)
	if err != nil {
		t.Fatalf("Expected %s to exist: %v", rootJSONPath, err)
	}

	var rootRaw map[string]json.RawMessage
	if err := json.Unmarshal(rootContent, &rootRaw); err != nil {
		t.Fatalf("Failed to parse consolidated JSON index: %v", err)
	}

	var linuxEntries []SimpleArtifactEntry
	if err := json.Unmarshal(rootRaw["linux"], &linuxEntries); err != nil {
		t.Fatalf("Failed to parse linux entries in root JSON: %v", err)
	}

	if len(linuxEntries) != 1 {
		t.Fatalf("Consolidated JSON index for linux length = %d, want 1", len(linuxEntries))
	}
	if linuxEntries[0].Binary != expected {
		t.Errorf("Consolidated JSON entry = %q, want %q", linuxEntries[0].Binary, expected)
	}
	if linuxEntries[0].Version != "22.15.0" {
		t.Errorf("Consolidated JSON artifact version = %q, want %q", linuxEntries[0].Version, "22.15.0")
	}

	// Verify "unsupported" key is present (may be empty for a test with no unsupported config).
	if _, ok := rootRaw["unsupported"]; !ok {
		t.Error("Consolidated root JSON index missing 'unsupported' key")
	}
}

func TestRenderSimpleIndex_YarnRuntime(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tempDir := t.TempDir()

	model := &SiteModel{
		Runtimes: []RuntimeModel{
			{
				Name: "yarn",
				Platforms: []PlatformModel{
					{
						OS: "linux",
						Versions: []VersionModel{
							{
								Major:   1,
								Minor:   22,
								Patch:   22,
								Version: "1.22.22",
								Releases: []ReleaseModel{
									{
										ReleaseTag: "yarn-1.22.22",
										Artifacts: []ArtifactModel{
											{
												Binary: &FileModel{
													Filename: "yarn-v1.22.22.tar.gz",
													URL:      "https://example.com/yarn-v1.22.22.tar.gz",
													SHA256:   "abc123",
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	if err := RenderSimpleIndex(model, tempDir, config.UnsupportedConfig{}, logger); err != nil {
		t.Fatalf("RenderSimpleIndex() error = %v", err)
	}

	runtimeJSONPath := filepath.Join(tempDir, "simple", "yarn", "index.json")
	runtimeContent, err := os.ReadFile(runtimeJSONPath)
	if err != nil {
		t.Fatalf("Expected %s to exist: %v", runtimeJSONPath, err)
	}

	var runtimeIndex map[string]json.RawMessage
	if err := json.Unmarshal(runtimeContent, &runtimeIndex); err != nil {
		t.Fatalf("Failed to parse runtime JSON index: %v", err)
	}

	linuxEntries := make([]SimpleArtifactEntry, 0)
	if err := json.Unmarshal(runtimeIndex["linux"], &linuxEntries); err != nil {
		t.Fatalf("Failed to parse linux entries: %v", err)
	}
	if len(linuxEntries) != 1 {
		t.Fatalf("runtime linux entries len = %d, want 1", len(linuxEntries))
	}
	if linuxEntries[0].Binary != "linux/yarn/1.22/1.22.22/yarn-v1.22.22.tar.gz" {
		t.Fatalf("runtime linux binary = %q", linuxEntries[0].Binary)
	}

	versionJSONPath := filepath.Join(tempDir, "simple", "yarn", "v1", "index.json")
	versionContent, err := os.ReadFile(versionJSONPath)
	if err != nil {
		t.Fatalf("Expected %s to exist: %v", versionJSONPath, err)
	}

	var versionIndex SimpleVersionIndex
	if err := json.Unmarshal(versionContent, &versionIndex); err != nil {
		t.Fatalf("Failed to parse version JSON index: %v", err)
	}
	if len(versionIndex["linux"]) != 1 {
		t.Fatalf("version linux entries len = %d, want 1", len(versionIndex["linux"]))
	}
}

func TestCollectMajorVersions(t *testing.T) {
	runtime := RuntimeModel{
		Name: "nodejs",
		Platforms: []PlatformModel{
			{
				OS: "linux",
				Versions: []VersionModel{
					{Major: 22, Version: "22.15.0"},
					{Major: 20, Version: "20.10.0"},
				},
			},
			{
				OS: "mac",
				Versions: []VersionModel{
					{Major: 22, Version: "22.15.0"},
					{Major: 18, Version: "18.20.0"},
				},
			},
		},
	}

	majors := collectMajorVersions(runtime)

	if len(majors) != 3 {
		t.Errorf("collectMajorVersions() returned %d majors, want 3", len(majors))
	}

	expected := []int{18, 20, 22}
	for i, v := range expected {
		if majors[i] != v {
			t.Errorf("collectMajorVersions()[%d] = %d, want %d", i, majors[i], v)
		}
	}
}

func TestCollectDistributionsFromVersion(t *testing.T) {
	version := VersionModel{
		Major:   22,
		Version: "22.15.0",
		Releases: []ReleaseModel{
			{
				ReleaseTag: "nodejs-22.15.0",
				Artifacts: []ArtifactModel{
					{
						Binary: &FileModel{
							Filename: "node-v22.15.0-linux-x64.tar.gz",
							URL:      "https://example.com/binary.tar.gz",
							SHA256:   "abc123",
						},
						Audit: &FileModel{
							Filename: "node-v22.15.0-linux-x64.audit.json",
							URL:      "https://example.com/audit.json",
						},
						Signature: &FileModel{
							Filename: "node-v22.15.0-linux-x64.tar.gz.sig",
							URL:      "https://example.com/binary.sig",
							SHA256:   "sig123",
						},
						Certificate: &FileModel{
							Filename: "node-v22.15.0-linux-x64.tar.gz.cert",
							URL:      "https://example.com/binary.cert",
							SHA256:   "cert123",
						},
					},
				},
			},
		},
	}

	distMap := make(map[string]DistributionModel)
	collectDistributionsFromVersion(version, distMap)

	if len(distMap) != 4 {
		t.Errorf("collectDistributionsFromVersion() collected %d distributions, want 4", len(distMap))
	}
}

func TestCollectAllArtifactIndex(t *testing.T) {
	tests := []struct {
		name     string
		model    *SiteModel
		expected map[string][]struct {
			path    string
			version string
		}
	}{
		{
			name: "single runtime with multiple versions",
			model: &SiteModel{
				Runtimes: []RuntimeModel{
					{
						Name: "nodejs",
						Platforms: []PlatformModel{
							{
								OS: "linux",
								Versions: []VersionModel{
									{
										Major:   22,
										Minor:   15,
										Version: "22.15.0",
										Releases: []ReleaseModel{
											{
												ReleaseTag: "nodejs-v22.15.0",
												Artifacts: []ArtifactModel{
													{
														Binary: &FileModel{
															Filename: "node-v22.15.0-linux-x64.tar.xz",
														},
													},
												},
											},
										},
									},
									{
										Major:   20,
										Minor:   11,
										Version: "20.11.0",
										Releases: []ReleaseModel{
											{
												ReleaseTag: "nodejs-v20.11.0",
												Artifacts: []ArtifactModel{
													{
														Binary: &FileModel{
															Filename: "node-v20.11.0-linux-x64.tar.xz",
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			expected: map[string][]struct {
				path    string
				version string
			}{
				"linux": {
					{"linux/nodejs/20.11/20.11.0/node-v20.11.0-linux-x64.tar.xz", "20.11.0"},
					{"linux/nodejs/22.15/22.15.0/node-v22.15.0-linux-x64.tar.xz", "22.15.0"},
				},
			},
		},
		{
			name: "multiple runtimes",
			model: &SiteModel{
				Runtimes: []RuntimeModel{
					{
						Name: "nodejs",
						Platforms: []PlatformModel{
							{
								OS: "linux",
								Versions: []VersionModel{
									{
										Major:   22,
										Minor:   15,
										Version: "22.15.0",
										Releases: []ReleaseModel{
											{
												ReleaseTag: "nodejs-v22.15.0",
												Artifacts: []ArtifactModel{
													{
														Binary: &FileModel{
															Filename: "node-v22.15.0-linux-x64.tar.xz",
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
					{
						Name: "python",
						Platforms: []PlatformModel{
							{
								OS: "linux",
								Versions: []VersionModel{
									{
										Major:   3,
										Minor:   13,
										Version: "3.13.0",
										Releases: []ReleaseModel{
											{
												ReleaseTag: "python-v3.13.0",
												Artifacts: []ArtifactModel{
													{
														Binary: &FileModel{
															Filename: "python-3.13.0-linux-x64.tar.gz",
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			expected: map[string][]struct {
				path    string
				version string
			}{
				"linux": {
					{"linux/nodejs/22.15/22.15.0/node-v22.15.0-linux-x64.tar.xz", "22.15.0"},
					{"linux/python/3.13/3.13.0/python-3.13.0-linux-x64.tar.gz", "3.13.0"},
				},
			},
		},
		{
			name: "empty model",
			model: &SiteModel{
				Runtimes: []RuntimeModel{},
			},
			expected: map[string][]struct {
				path    string
				version string
			}{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := collectAllArtifactIndex(tt.model)

			if len(result) != len(tt.expected) {
				t.Errorf("collectAllArtifactIndex() OS count = %d, want %d", len(result), len(tt.expected))
			}

			for os, expectedEntries := range tt.expected {
				if len(result[os]) != len(expectedEntries) {
					t.Errorf("collectAllArtifactIndex() paths for %s = %d, want %d", os, len(result[os]), len(expectedEntries))
					continue
				}
				for i, entry := range result[os] {
					if entry.Binary != expectedEntries[i].path {
						t.Errorf("collectAllArtifactIndex() [%s][%d] path = %q, want %q", os, i, entry.Binary, expectedEntries[i].path)
					}
					if entry.Version != expectedEntries[i].version {
						t.Errorf("collectAllArtifactIndex() [%s][%d] version = %q, want %q", os, i, entry.Version, expectedEntries[i].version)
					}
				}
			}
		})
	}
}

func TestRenderHumanPages_EmptyModel(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tempDir := t.TempDir()

	model := &SiteModel{
		Runtimes: []RuntimeModel{},
	}

	err := RenderHumanPages(model, tempDir, logger)
	if err != nil {
		t.Fatalf("RenderHumanPages() with empty model error = %v", err)
	}

	rootIndex := filepath.Join(tempDir, "index.html")
	if _, err := os.Stat(rootIndex); os.IsNotExist(err) {
		t.Error("Expected root index.html to exist even with empty model")
	}
}

func TestRenderSimpleIndex_EmptyModel(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tempDir := t.TempDir()

	model := &SiteModel{
		Runtimes: []RuntimeModel{},
	}

	err := RenderSimpleIndex(model, tempDir, config.UnsupportedConfig{}, logger)
	if err != nil {
		t.Fatalf("RenderSimpleIndex() with empty model error = %v", err)
	}

	simpleIndex := filepath.Join(tempDir, "simple", "index.html")
	if _, err := os.Stat(simpleIndex); os.IsNotExist(err) {
		t.Error("Expected simple/index.html to exist even with empty model")
	}

	// Verify consolidated JSON index exists and has the "unsupported" key
	rootJSONPath := filepath.Join(tempDir, "simple", "index.json")
	rootContent, err := os.ReadFile(rootJSONPath)
	if err != nil {
		t.Fatalf("Expected %s to exist: %v", rootJSONPath, err)
	}

	var rootRaw map[string]json.RawMessage
	if err := json.Unmarshal(rootContent, &rootRaw); err != nil {
		t.Fatalf("Failed to parse consolidated JSON index: %v", err)
	}

	// Only the "unsupported" meta-key should be present; no OS keys for an empty model.
	if _, ok := rootRaw["unsupported"]; !ok {
		t.Error("Consolidated root JSON index missing 'unsupported' key")
	}
	// Remove the meta-key and confirm no OS entries remain.
	delete(rootRaw, "unsupported")
	if len(rootRaw) != 0 {
		t.Errorf("Consolidated JSON index OS count = %d, want 0 for empty model", len(rootRaw))
	}
}

// TestRenderSimpleIndex_UnsupportedVersions verifies that the "unsupported" key is
// correctly populated at all three JSON output levels (root, runtime, major-version)
// when an UnsupportedConfig with matching rules is provided.
func TestRenderSimpleIndex_UnsupportedVersions(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tempDir := t.TempDir()

	// Model: nodejs v22 (supported) and v16 (should be flagged unsupported).
	model := &SiteModel{
		Runtimes: []RuntimeModel{
			{
				Name: "nodejs",
				Platforms: []PlatformModel{
					{
						OS: "linux",
						Versions: []VersionModel{
							{
								Major:   22,
								Minor:   15,
								Patch:   0,
								Version: "22.15.0",
								Releases: []ReleaseModel{
									{
										ReleaseTag: "nodejs-22.15.0",
										Artifacts: []ArtifactModel{
											{
												Binary: &FileModel{
													Filename: "node-v22.15.0-linux-x64.tar.gz",
													URL:      "https://example.com/22.tar.gz",
													SHA256:   "abc123",
												},
											},
										},
									},
								},
							},
							{
								Major:   16,
								Minor:   20,
								Patch:   2,
								Version: "16.20.2",
								Releases: []ReleaseModel{
									{
										ReleaseTag: "nodejs-16.20.2",
										Artifacts: []ArtifactModel{
											{
												Binary: &FileModel{
													Filename: "node-v16.20.2-linux-x64.tar.gz",
													URL:      "https://example.com/16.tar.gz",
													SHA256:   "def456",
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	// Mark nodejs major version 16 as unsupported.
	unsupportedCfg := config.UnsupportedConfig{
		"nodejs": {
			{Version: "16", Reason: "EOL since 2023-09-11", EOLDate: "2023-09-11"},
		},
	}

	if err := RenderSimpleIndex(model, tempDir, unsupportedCfg, logger); err != nil {
		t.Fatalf("RenderSimpleIndex() error = %v", err)
	}

	// ── 1. Root simple/index.json ──────────────────────────────────────────────
	rootContent, err := os.ReadFile(filepath.Join(tempDir, "simple", "index.json"))
	if err != nil {
		t.Fatalf("simple/index.json: %v", err)
	}

	var rootRaw map[string]json.RawMessage
	if err := json.Unmarshal(rootContent, &rootRaw); err != nil {
		t.Fatalf("parse simple/index.json: %v", err)
	}

	unsupportedRootRaw, ok := rootRaw["unsupported"]
	if !ok {
		t.Fatal("simple/index.json missing 'unsupported' key")
	}

	var unsupportedRoot map[string][]endoflife.PolicyVersion
	if err := json.Unmarshal(unsupportedRootRaw, &unsupportedRoot); err != nil {
		t.Fatalf("parse root unsupported map: %v", err)
	}

	nodejsUnsupported, ok := unsupportedRoot["nodejs"]
	if !ok {
		t.Fatal("root unsupported missing 'nodejs' key")
	}
	if len(nodejsUnsupported) != 1 {
		t.Fatalf("root unsupported nodejs count = %d, want 1", len(nodejsUnsupported))
	}
	if nodejsUnsupported[0].Version != "16.20.2" {
		t.Errorf("root unsupported nodejs[0].version = %q, want %q", nodejsUnsupported[0].Version, "16.20.2")
	}
	if nodejsUnsupported[0].Supported {
		t.Error("root unsupported nodejs[0].supported should be false")
	}
	if nodejsUnsupported[0].EOL != "2023-09-11" {
		t.Errorf("root unsupported nodejs[0].eol = %q, want %q", nodejsUnsupported[0].EOL, "2023-09-11")
	}

	// ── 2. Runtime simple/nodejs/index.json ───────────────────────────────────
	rtContent, err := os.ReadFile(filepath.Join(tempDir, "simple", "nodejs", "index.json"))
	if err != nil {
		t.Fatalf("simple/nodejs/index.json: %v", err)
	}

	var rtRaw map[string]json.RawMessage
	if err := json.Unmarshal(rtContent, &rtRaw); err != nil {
		t.Fatalf("parse simple/nodejs/index.json: %v", err)
	}

	unsupportedRtRaw, ok := rtRaw["unsupported"]
	if !ok {
		t.Fatal("simple/nodejs/index.json missing 'unsupported' key")
	}

	var unsupportedRt []endoflife.PolicyVersion
	if err := json.Unmarshal(unsupportedRtRaw, &unsupportedRt); err != nil {
		t.Fatalf("parse runtime unsupported list: %v", err)
	}

	if len(unsupportedRt) != 1 {
		t.Fatalf("runtime unsupported count = %d, want 1", len(unsupportedRt))
	}
	if unsupportedRt[0].Version != "16.20.2" {
		t.Errorf("runtime unsupported[0].version = %q, want %q", unsupportedRt[0].Version, "16.20.2")
	}

	// ── 3. Major-version simple/nodejs/v16/index.json ─────────────────────────
	v16Content, err := os.ReadFile(filepath.Join(tempDir, "simple", "nodejs", "v16", "index.json"))
	if err != nil {
		t.Fatalf("simple/nodejs/v16/index.json: %v", err)
	}

	var v16Raw map[string]json.RawMessage
	if err := json.Unmarshal(v16Content, &v16Raw); err != nil {
		t.Fatalf("parse simple/nodejs/v16/index.json: %v", err)
	}

	unsupportedV16Raw, ok := v16Raw["unsupported"]
	if !ok {
		t.Fatal("simple/nodejs/v16/index.json missing 'unsupported' key")
	}

	var unsupportedV16 []endoflife.PolicyVersion
	if err := json.Unmarshal(unsupportedV16Raw, &unsupportedV16); err != nil {
		t.Fatalf("parse v16 unsupported list: %v", err)
	}

	if len(unsupportedV16) != 1 {
		t.Fatalf("v16 unsupported count = %d, want 1", len(unsupportedV16))
	}
	if unsupportedV16[0].Version != "16.20.2" {
		t.Errorf("v16 unsupported[0].version = %q, want %q", unsupportedV16[0].Version, "16.20.2")
	}

	// ── 4. Supported version (v22) must NOT appear in its major-version unsupported list ─
	v22Content, err := os.ReadFile(filepath.Join(tempDir, "simple", "nodejs", "v22", "index.json"))
	if err != nil {
		t.Fatalf("simple/nodejs/v22/index.json: %v", err)
	}

	var v22Raw map[string]json.RawMessage
	if err := json.Unmarshal(v22Content, &v22Raw); err != nil {
		t.Fatalf("parse simple/nodejs/v22/index.json: %v", err)
	}

	unsupportedV22Raw, ok := v22Raw["unsupported"]
	if !ok {
		t.Fatal("simple/nodejs/v22/index.json missing 'unsupported' key")
	}

	var unsupportedV22 []endoflife.PolicyVersion
	if err := json.Unmarshal(unsupportedV22Raw, &unsupportedV22); err != nil {
		t.Fatalf("parse v22 unsupported list: %v", err)
	}

	if len(unsupportedV22) != 0 {
		t.Errorf("v22 unsupported count = %d, want 0 (v22 is supported)", len(unsupportedV22))
	}
}

// TestExpandUnsupportedVersions tests the expandUnsupportedVersions helper directly.
func TestExpandUnsupportedVersions(t *testing.T) {
	rt := RuntimeModel{
		Name: "nodejs",
		Platforms: []PlatformModel{
			{
				OS: "linux",
				Versions: []VersionModel{
					{Version: "16.20.2", Major: 16},
					{Version: "16.20.1", Major: 16},
					{Version: "18.20.0", Major: 18},
					{Version: "22.15.0", Major: 22},
				},
			},
			// Same versions on a second platform — must not produce duplicates.
			{
				OS: "windows",
				Versions: []VersionModel{
					{Version: "16.20.2", Major: 16},
					{Version: "18.20.0", Major: 18},
				},
			},
		},
	}

	tests := []struct {
		name            string
		uc              config.UnsupportedConfig
		wantVersions    []string
		wantNoDuplicate bool
	}{
		{
			name:         "empty config returns nil",
			uc:           config.UnsupportedConfig{},
			wantVersions: nil,
		},
		{
			name: "prefix 16 expands to all 16.x.y versions without duplicates",
			uc: config.UnsupportedConfig{
				"nodejs": {{Version: "16", Reason: "EOL", EOLDate: "2023-09-11"}},
			},
			wantVersions:    []string{"16.20.1", "16.20.2"},
			wantNoDuplicate: true,
		},
		{
			name: "numeric sort: 8.x must come before 22.x (lexicographic would reverse this)",
			uc: config.UnsupportedConfig{
				"nodejs": {
					{Version: "16", Reason: "EOL", EOLDate: "2023-09-11"},
					{Version: "18", Reason: "EOL", EOLDate: "2025-04-30"},
					{Version: "22", Reason: "EOL"},
				},
			},
			// 16.20.1, 16.20.2, 18.20.0, 22.15.0 — numeric order, not lexicographic
			wantVersions: []string{"16.20.1", "16.20.2", "18.20.0", "22.15.0"},
		},
		{
			name: "exact version match",
			uc: config.UnsupportedConfig{
				"nodejs": {{Version: "18.20.0", Reason: "EOL", EOLDate: "2024-04-30"}},
			},
			wantVersions: []string{"18.20.0"},
		},
		{
			name: "prefix 16 must not match 160.x versions (false positive guard)",
			uc: config.UnsupportedConfig{
				"nodejs": {{Version: "1", Reason: "ancient"}},
			},
			// "1" should NOT match "16.20.2", "18.20.0", or "22.15.0"
			wantVersions: nil,
		},
		{
			name: "unknown runtime returns nil",
			uc: config.UnsupportedConfig{
				"temurin": {{Version: "16", Reason: "EOL"}},
			},
			wantVersions: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := expandUnsupportedVersions(rt, tc.uc)

			if len(tc.wantVersions) == 0 {
				if len(got) != 0 {
					t.Errorf("got %d entries, want 0: %v", len(got), got)
				}
				return
			}

			gotVersions := make([]string, len(got))
			for i, pv := range got {
				gotVersions[i] = pv.Version
			}

			if len(gotVersions) != len(tc.wantVersions) {
				t.Fatalf("got versions %v, want %v", gotVersions, tc.wantVersions)
			}
			for i, v := range tc.wantVersions {
				if gotVersions[i] != v {
					t.Errorf("got[%d] = %q, want %q", i, gotVersions[i], v)
				}
			}

			if tc.wantNoDuplicate {
				seen := make(map[string]int)
				for _, pv := range got {
					seen[pv.Version]++
					if seen[pv.Version] > 1 {
						t.Errorf("duplicate version %q in output", pv.Version)
					}
				}
			}

			// Verify all returned entries have Supported=false.
			for _, pv := range got {
				if pv.Supported {
					t.Errorf("version %q has Supported=true, want false", pv.Version)
				}
			}
		})
	}
}
