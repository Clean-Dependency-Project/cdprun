package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewTempDir(t *testing.T) {
	tests := []struct {
		name        string
		runtime     string
		version     string
		wantErr     bool
		errContains string
	}{
		{
			name:    "valid nodejs",
			runtime: "nodejs",
			version: "22.15.0",
			wantErr: false,
		},
		{
			name:    "valid python",
			runtime: "python",
			version: "3.14.0",
			wantErr: false,
		},
		{
			name:        "empty runtime",
			runtime:     "",
			version:     "1.0.0",
			wantErr:     true,
			errContains: "runtime cannot be empty",
		},
		{
			name:        "empty version",
			runtime:     "nodejs",
			version:     "",
			wantErr:     true,
			errContains: "version cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			td, err := NewTempDir(tt.runtime, tt.version)
			
			if tt.wantErr {
				if err == nil {
					t.Errorf("NewTempDir() expected error containing %q, got nil", tt.errContains)
					return
				}
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("NewTempDir() error = %v, want error containing %q", err, tt.errContains)
				}
				return
			}

			if err != nil {
				t.Errorf("NewTempDir() unexpected error: %v", err)
				return
			}

			defer func() {
				if err := td.Remove(); err != nil {
					t.Errorf("Remove() error: %v", err)
				}
			}()

			// Verify root directory exists
			if _, err := os.Stat(td.Root()); os.IsNotExist(err) {
				t.Errorf("root directory does not exist: %s", td.Root())
			}

			// Verify downloads directory exists
			if _, err := os.Stat(td.Downloads()); os.IsNotExist(err) {
				t.Errorf("downloads directory does not exist: %s", td.Downloads())
			}

			// Verify signatures directory exists
			if _, err := os.Stat(td.Signatures()); os.IsNotExist(err) {
				t.Errorf("signatures directory does not exist: %s", td.Signatures())
			}

			// Verify directory name format
			dirname := filepath.Base(td.Root())
			expectedPrefix := "cdprun-" + tt.runtime + "-" + tt.version + "-"
			if !strings.HasPrefix(dirname, expectedPrefix) {
				t.Errorf("directory name = %q, want prefix %q", dirname, expectedPrefix)
			}
		})
	}
}

func TestTempDir_Remove(t *testing.T) {
	t.Run("remove existing directory", func(t *testing.T) {
		td, err := NewTempDir("nodejs", "20.0.0")
		if err != nil {
			t.Fatalf("NewTempDir() error: %v", err)
		}

		root := td.Root()

		// Verify directory exists
		if _, err := os.Stat(root); os.IsNotExist(err) {
			t.Fatalf("directory should exist before Remove()")
		}

		// Remove directory
		if err := td.Remove(); err != nil {
			t.Errorf("Remove() error: %v", err)
		}

		// Verify directory is gone
		if _, err := os.Stat(root); !os.IsNotExist(err) {
			t.Errorf("directory still exists after Remove()")
		}
	})

	t.Run("remove is idempotent", func(t *testing.T) {
		td, err := NewTempDir("nodejs", "20.0.0")
		if err != nil {
			t.Fatalf("NewTempDir() error: %v", err)
		}

		// Remove once
		if err := td.Remove(); err != nil {
			t.Errorf("first Remove() error: %v", err)
		}

		// Remove again - should not error
		if err := td.Remove(); err != nil {
			t.Errorf("second Remove() error: %v", err)
		}
	})
}

func TestTempDir_Age(t *testing.T) {
	td, err := NewTempDir("nodejs", "20.0.0")
	if err != nil {
		t.Fatalf("NewTempDir() error: %v", err)
	}
	defer func() {
		_ = td.Remove()
	}()

	// Wait a bit
	time.Sleep(100 * time.Millisecond)

	age := td.Age()
	if age < 100*time.Millisecond {
		t.Errorf("Age() = %v, want >= 100ms", age)
	}
	if age > 1*time.Second {
		t.Errorf("Age() = %v, want < 1s", age)
	}
}

func TestTempDir_ListAllFiles(t *testing.T) {
	td, err := NewTempDir("nodejs", "20.0.0")
	if err != nil {
		t.Fatalf("NewTempDir() error: %v", err)
	}
	defer func() {
		_ = td.Remove()
	}()

	t.Run("empty directories", func(t *testing.T) {
		files, err := td.ListAllFiles()
		if err != nil {
			t.Errorf("ListAllFiles() error: %v", err)
		}
		if len(files) != 0 {
			t.Errorf("ListAllFiles() = %v files, want 0", len(files))
		}
	})

	t.Run("with files in downloads", func(t *testing.T) {
		// Create test file in downloads
		testFile := filepath.Join(td.Downloads(), "test.tar.xz")
		if err := os.WriteFile(testFile, []byte("test data"), 0644); err != nil {
			t.Fatalf("WriteFile() error: %v", err)
		}

		files, err := td.ListAllFiles()
		if err != nil {
			t.Errorf("ListAllFiles() error: %v", err)
		}
		if len(files) != 1 {
			t.Errorf("ListAllFiles() = %v files, want 1", len(files))
		}
		if len(files) > 0 && !strings.HasSuffix(files[0], "test.tar.xz") {
			t.Errorf("ListAllFiles()[0] = %q, want file ending with test.tar.xz", files[0])
		}
	})

	t.Run("with files in both directories", func(t *testing.T) {
		// Create files in both downloads and signatures
		downloadFile := filepath.Join(td.Downloads(), "binary.tar.xz")
		if err := os.WriteFile(downloadFile, []byte("binary"), 0644); err != nil {
			t.Fatalf("WriteFile() error: %v", err)
		}

		sigFile := filepath.Join(td.Signatures(), "binary.tar.xz.sig")
		if err := os.WriteFile(sigFile, []byte("signature"), 0644); err != nil {
			t.Fatalf("WriteFile() error: %v", err)
		}

		certFile := filepath.Join(td.Signatures(), "binary.tar.xz.cert")
		if err := os.WriteFile(certFile, []byte("certificate"), 0644); err != nil {
			t.Fatalf("WriteFile() error: %v", err)
		}

		files, err := td.ListAllFiles()
		if err != nil {
			t.Errorf("ListAllFiles() error: %v", err)
		}

		// Should have 3 files total (1 from downloads, 2 from signatures)
		// But we created test.tar.xz earlier, so 4 total
		if len(files) != 4 {
			t.Errorf("ListAllFiles() = %v files, want 4", len(files))
		}
	})
}

func TestTempDir_Paths(t *testing.T) {
	td, err := NewTempDir("nodejs", "20.0.0")
	if err != nil {
		t.Fatalf("NewTempDir() error: %v", err)
	}
	defer func() {
		_ = td.Remove()
	}()

	// Test Root()
	root := td.Root()
	if root == "" {
		t.Error("Root() returned empty string")
	}
	if !strings.Contains(root, "cdprun-nodejs-20.0.0-") {
		t.Errorf("Root() = %q, want path containing cdprun-nodejs-20.0.0", root)
	}

	// Test Downloads()
	downloads := td.Downloads()
	if downloads == "" {
		t.Error("Downloads() returned empty string")
	}
	if !strings.HasSuffix(downloads, "downloads") {
		t.Errorf("Downloads() = %q, want path ending with downloads", downloads)
	}
	if !strings.HasPrefix(downloads, root) {
		t.Errorf("Downloads() = %q, want path under root %q", downloads, root)
	}

	// Test Signatures()
	signatures := td.Signatures()
	if signatures == "" {
		t.Error("Signatures() returned empty string")
	}
	if !strings.HasSuffix(signatures, "signatures") {
		t.Errorf("Signatures() = %q, want path ending with signatures", signatures)
	}
	if !strings.HasPrefix(signatures, root) {
		t.Errorf("Signatures() = %q, want path under root %q", signatures, root)
	}
}

