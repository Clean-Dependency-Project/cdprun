package sitegen

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

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
						Runtime:     "nodejs",
						Version:     "22.15.0",
						ReleaseTag:  "nodejs-v22.15.0",
						Artifacts:   `invalid json`,
						CreatedAt:   now,
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

			// Capture file info before write
			infoBefore, _ := os.Stat(filePath)
			modTimeBefore := time.Time{}
			if infoBefore != nil {
				modTimeBefore = infoBefore.ModTime()
			}

			// Use a simple logger for testing
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))

			// Write file
			err := writeFileIfChanged(filePath, tt.newData, logger)
			if err != nil {
				t.Fatalf("writeFileIfChanged() error = %v", err)
			}

			// Check if file was written
			infoAfter, err := os.Stat(filePath)
			if err != nil {
				t.Fatalf("Failed to stat file after write: %v", err)
			}

			if tt.shouldWrite {
				if infoBefore == nil || infoAfter.ModTime().After(modTimeBefore) {
					// File was written
					content, err := os.ReadFile(filePath)
					if err != nil {
						t.Fatalf("Failed to read file: %v", err)
					}
					if string(content) != string(tt.newData) {
						t.Errorf("File content = %q, want %q", string(content), string(tt.newData))
					}
				} else {
					t.Error("Expected file to be written, but mod time unchanged")
				}
			} else {
				if infoBefore != nil && infoAfter.ModTime().Equal(modTimeBefore) {
					// File was not written (unchanged)
					content, err := os.ReadFile(filePath)
					if err != nil {
						t.Fatalf("Failed to read file: %v", err)
					}
					if string(content) != string(tt.initialData) {
						t.Errorf("File content changed unexpectedly: got %q, want %q", string(content), string(tt.initialData))
					}
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
	if gen == nil {
		t.Error("NewGenerator() returned nil")
	}
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
		name     string
		reader   ReleaseReader
		opts     GenerateOptions
		wantErr  bool
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
			err := gen.Generate(nil, tt.opts)
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

