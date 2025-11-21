package gpg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEmbeddedKeysFS tests that the embedded Node.js keys are accessible
func TestEmbeddedKeysFS(t *testing.T) {
	entries, err := EmbeddedKeysFS.ReadDir("nodejs-keys")
	if err != nil {
		t.Fatalf("Failed to read embedded nodejs-keys directory: %v", err)
	}

	if len(entries) == 0 {
		t.Fatal("No embedded keys found in nodejs-keys directory")
	}

	t.Logf("Found %d embedded key files", len(entries))

	// Verify each entry is an .asc file
	for _, entry := range entries {
		if entry.IsDir() {
			t.Errorf("Unexpected directory in nodejs-keys: %s", entry.Name())
			continue
		}

		if !strings.HasSuffix(entry.Name(), ".asc") {
			t.Errorf("Non-.asc file found: %s", entry.Name())
		}

		// Verify we can read each key file
		keyPath := filepath.Join("nodejs-keys", entry.Name())
		keyData, err := EmbeddedKeysFS.ReadFile(keyPath)
		if err != nil {
			t.Errorf("Failed to read embedded key %s: %v", entry.Name(), err)
			continue
		}

		if len(keyData) == 0 {
			t.Errorf("Empty key file: %s", entry.Name())
		}
	}
}

// TestLoadKeyRingFromEmbeddedFS tests loading keys from the embedded filesystem
func TestLoadKeyRingFromEmbeddedFS(t *testing.T) {
	t.Run("load nodejs keys from embedded fs", func(t *testing.T) {
		keyRing, err := LoadKeyRingFromEmbeddedFS(EmbeddedKeysFS, "nodejs-keys")
		if err != nil {
			t.Fatalf("LoadKeyRingFromEmbeddedFS() error = %v", err)
		}

		if keyRing == nil {
			t.Fatal("LoadKeyRingFromEmbeddedFS() returned nil keyring")
		}

		// Verify it's a RealKeyRing
		_, ok := keyRing.(*RealKeyRing)
		if !ok {
			t.Errorf("LoadKeyRingFromEmbeddedFS() returned wrong type, want *RealKeyRing")
		}
	})

	t.Run("load from convenience wrapper", func(t *testing.T) {
		keyRing, err := LoadKeyRingFromEmbedFS(EmbeddedKeysFS, "nodejs-keys")
		if err != nil {
			t.Fatalf("LoadKeyRingFromEmbedFS() error = %v", err)
		}

		if keyRing == nil {
			t.Fatal("LoadKeyRingFromEmbedFS() returned nil keyring")
		}
	})

	t.Run("invalid path", func(t *testing.T) {
		_, err := LoadKeyRingFromEmbeddedFS(EmbeddedKeysFS, "nonexistent-keys")
		if err == nil {
			t.Error("LoadKeyRingFromEmbeddedFS() error = nil, want error for invalid path")
		}
	})

	t.Run("empty directory", func(t *testing.T) {
		// Create a mock embedded FS with no .asc files
		mockFS := &mockEmbeddedFS{
			files: map[string][]byte{
				"empty-keys/readme.txt": []byte("no keys here"),
			},
		}

		_, err := LoadKeyRingFromEmbeddedFS(mockFS, "empty-keys")
		if err == nil {
			t.Error("LoadKeyRingFromEmbeddedFS() error = nil, want error for empty directory")
		}
		if err != nil && !strings.Contains(err.Error(), "no .asc keys found") {
			t.Errorf("LoadKeyRingFromEmbeddedFS() error = %v, want 'no .asc keys found'", err)
		}
	})
}

// TestNewRealKeyRing tests creating a real keyring
func TestNewRealKeyRing(t *testing.T) {
	keyRing := NewRealKeyRing()
	if keyRing == nil {
		t.Fatal("NewRealKeyRing() returned nil")
	}

	if keyRing.keyRing != nil {
		t.Error("NewRealKeyRing() keyring should be nil until first key is added")
	}
}

// TestRealKeyRing_AddKey tests adding keys to a real keyring
func TestRealKeyRing_AddKey(t *testing.T) {
	// Load a real key from embedded filesystem for testing
	keyData, err := EmbeddedKeysFS.ReadFile("nodejs-keys/4ED778F539E3634C779C87C6D7062848A1AB005C.asc")
	if err != nil {
		t.Fatalf("Failed to read test key: %v", err)
	}

	tests := []struct {
		name      string
		setupFunc func() KeyRing
		key       func() Key
		wantError bool
		errorMsg  string
	}{
		{
			name: "add valid key",
			setupFunc: func() KeyRing {
				return NewRealKeyRing()
			},
			key: func() Key {
				k, err := NewRealKey(string(keyData))
				if err != nil {
					t.Fatalf("Failed to create test key: %v", err)
				}
				return k
			},
			wantError: false,
		},
		{
			name: "add nil key",
			setupFunc: func() KeyRing {
				return NewRealKeyRing()
			},
			key: func() Key {
				return nil
			},
			wantError: true,
			errorMsg:  "key cannot be nil",
		},
		{
			name: "add second key",
			setupFunc: func() KeyRing {
				kr := NewRealKeyRing()
				k, _ := NewRealKey(string(keyData))
				_ = kr.AddKey(k)
				return kr
			},
			key: func() Key {
				// Load a different key
				data, _ := EmbeddedKeysFS.ReadFile("nodejs-keys/108F52B48DB57BB0CC439B2997B01419BD92F80A.asc")
				k, _ := NewRealKey(string(data))
				return k
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keyRing := tt.setupFunc()
			key := tt.key()

			err := keyRing.AddKey(key)

			if tt.wantError {
				if err == nil {
					t.Errorf("AddKey() error = nil, want error")
				} else if tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("AddKey() error = %v, want error containing %q", err, tt.errorMsg)
				}
			} else {
				if err != nil {
					t.Errorf("AddKey() error = %v, want nil", err)
				}
			}
		})
	}
}

// TestRealKeyRing_VerifyDetached tests signature verification
func TestRealKeyRing_VerifyDetached(t *testing.T) {
	t.Run("verify with empty keyring", func(t *testing.T) {
		keyRing := NewRealKeyRing()
		err := keyRing.VerifyDetached([]byte("test message"), []byte("test signature"))
		if err == nil {
			t.Error("VerifyDetached() error = nil, want error for empty keyring")
		}
		if err != nil && !strings.Contains(err.Error(), "no keys in keyring") {
			t.Errorf("VerifyDetached() error = %v, want 'no keys in keyring'", err)
		}
	})

	// Note: Real signature verification requires actual signed data
	// which is tested in integration tests with real Node.js artifacts
}

// TestNewRealKey tests creating keys from armored data
func TestNewRealKey(t *testing.T) {
	// Load a real key from embedded filesystem
	validKeyData, err := EmbeddedKeysFS.ReadFile("nodejs-keys/4ED778F539E3634C779C87C6D7062848A1AB005C.asc")
	if err != nil {
		t.Fatalf("Failed to read test key: %v", err)
	}

	tests := []struct {
		name      string
		armored   string
		wantError bool
		errorMsg  string
	}{
		{
			name:      "valid armored key",
			armored:   string(validKeyData),
			wantError: false,
		},
		{
			name:      "empty armored data",
			armored:   "",
			wantError: true,
			errorMsg:  "armored data cannot be empty",
		},
		{
			name:      "invalid armored data",
			armored:   "not a valid PGP key",
			wantError: true,
			errorMsg:  "failed to parse PGP key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, err := NewRealKey(tt.armored)

			if tt.wantError {
				if err == nil {
					t.Errorf("NewRealKey() error = nil, want error")
				} else if tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("NewRealKey() error = %v, want error containing %q", err, tt.errorMsg)
				}
			} else {
				if err != nil {
					t.Errorf("NewRealKey() error = %v, want nil", err)
				}
				if key == nil {
					t.Error("NewRealKey() returned nil key")
				}
				if key != nil {
					if key.GetFingerprint() == "" {
						t.Error("NewRealKey() key has empty fingerprint")
					}
					if key.IsRevoked() {
						t.Error("NewRealKey() key is revoked, want not revoked")
					}
				}
			}
		})
	}
}

// TestSimpleKeyRing tests the mock/simple keyring implementation
func TestSimpleKeyRing(t *testing.T) {
	t.Run("create simple keyring", func(t *testing.T) {
		keyRing := NewSimpleKeyRing()
		if keyRing == nil {
			t.Fatal("NewSimpleKeyRing() returned nil")
		}
		if len(keyRing.keys) != 0 {
			t.Errorf("NewSimpleKeyRing() keys count = %d, want 0", len(keyRing.keys))
		}
	})

	t.Run("add key to simple keyring", func(t *testing.T) {
		keyRing := NewSimpleKeyRing()
		key, err := NewSimpleKey("test key data")
		if err != nil {
			t.Fatalf("NewSimpleKey() error = %v", err)
		}

		err = keyRing.AddKey(key)
		if err != nil {
			t.Errorf("AddKey() error = %v, want nil", err)
		}

		if len(keyRing.keys) != 1 {
			t.Errorf("AddKey() keys count = %d, want 1", len(keyRing.keys))
		}
	})

	t.Run("verify with simple keyring", func(t *testing.T) {
		keyRing := NewSimpleKeyRing()
		key, _ := NewSimpleKey("test key")
		_ = keyRing.AddKey(key)

		err := keyRing.VerifyDetached([]byte("message"), []byte("signature"))
		if err != nil {
			t.Errorf("VerifyDetached() error = %v, want nil", err)
		}
	})

	t.Run("verify with empty message", func(t *testing.T) {
		keyRing := NewSimpleKeyRing()
		key, _ := NewSimpleKey("test key")
		_ = keyRing.AddKey(key)

		err := keyRing.VerifyDetached([]byte{}, []byte("signature"))
		if err == nil {
			t.Error("VerifyDetached() error = nil, want error for empty message")
		}
	})

	t.Run("verify with no keys", func(t *testing.T) {
		keyRing := NewSimpleKeyRing()
		err := keyRing.VerifyDetached([]byte("message"), []byte("signature"))
		if err == nil {
			t.Error("VerifyDetached() error = nil, want error for no keys")
		}
	})
}

// TestSimpleKey tests the mock/simple key implementation
func TestSimpleKey(t *testing.T) {
	tests := []struct {
		name      string
		armored   string
		wantError bool
		errorMsg  string
	}{
		{
			name:      "valid key data",
			armored:   "test key data",
			wantError: false,
		},
		{
			name:      "empty key data",
			armored:   "",
			wantError: true,
			errorMsg:  "armored data cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, err := NewSimpleKey(tt.armored)

			if tt.wantError {
				if err == nil {
					t.Errorf("NewSimpleKey() error = nil, want error")
				} else if tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("NewSimpleKey() error = %v, want error containing %q", err, tt.errorMsg)
				}
			} else {
				if err != nil {
					t.Errorf("NewSimpleKey() error = %v, want nil", err)
				}
				if key == nil {
					t.Fatal("NewSimpleKey() returned nil key")
				}
				if key.GetFingerprint() == "" {
					t.Error("NewSimpleKey() fingerprint is empty")
				}
				if !strings.HasPrefix(key.GetFingerprint(), "fp_") {
					t.Errorf("NewSimpleKey() fingerprint = %s, want prefix 'fp_'", key.GetFingerprint())
				}
				if key.IsRevoked() {
					t.Error("NewSimpleKey() key is revoked, want not revoked")
				}
			}
		})
	}
}

// TestValidateKey tests key validation
func TestValidateKey(t *testing.T) {
	tests := []struct {
		name      string
		key       Key
		wantError bool
		errorMsg  string
	}{
		{
			name: "valid key",
			key: &SimpleKey{
				fingerprint: "test-fp",
				revoked:     false,
			},
			wantError: false,
		},
		{
			name:      "nil key",
			key:       nil,
			wantError: true,
			errorMsg:  "key is nil",
		},
		{
			name: "revoked key",
			key: &SimpleKey{
				fingerprint: "test-fp",
				revoked:     true,
			},
			wantError: true,
			errorMsg:  "key is revoked",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateKey(tt.key)

			if tt.wantError {
				if err == nil {
					t.Errorf("validateKey() error = nil, want error")
				} else if tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("validateKey() error = %v, want error containing %q", err, tt.errorMsg)
				}
			} else {
				if err != nil {
					t.Errorf("validateKey() error = %v, want nil", err)
				}
			}
		})
	}
}

// mockEmbeddedFS implements EmbeddedFS interface for testing
type mockEmbeddedFS struct {
	files map[string][]byte
}

func (m *mockEmbeddedFS) ReadDir(name string) ([]os.DirEntry, error) {
	var entries []os.DirEntry
	for path := range m.files {
		if strings.HasPrefix(path, name+"/") {
			filename := strings.TrimPrefix(path, name+"/")
			if !strings.Contains(filename, "/") {
				entries = append(entries, &mockDirEntry{name: filename})
			}
		}
	}
	if len(entries) == 0 {
		return nil, os.ErrNotExist
	}
	return entries, nil
}

func (m *mockEmbeddedFS) ReadFile(name string) ([]byte, error) {
	data, ok := m.files[name]
	if !ok {
		return nil, os.ErrNotExist
	}
	return data, nil
}

type mockDirEntry struct {
	name string
}

func (m *mockDirEntry) Name() string               { return m.name }
func (m *mockDirEntry) IsDir() bool                { return false }
func (m *mockDirEntry) Type() os.FileMode          { return 0 }
func (m *mockDirEntry) Info() (os.FileInfo, error) { return nil, nil }

// TestParseClearTextMessage tests parsing clear-signed messages
func TestParseClearTextMessage(t *testing.T) {
	tests := []struct {
		name      string
		armored   string
		wantError bool
		errorMsg  string
	}{
		{
			name: "valid clear-signed message",
			armored: `-----BEGIN PGP SIGNED MESSAGE-----
Hash: SHA256

This is the message content
-----BEGIN PGP SIGNATURE-----

iQEzBAEBCAAdFiEE...(signature data)...
-----END PGP SIGNATURE-----`,
			wantError: false,
		},
		{
			name: "message without hash header",
			armored: `-----BEGIN PGP SIGNED MESSAGE-----

Message without hash header
-----BEGIN PGP SIGNATURE-----

signature here
-----END PGP SIGNATURE-----`,
			wantError: false,
		},
		{
			name:      "empty armored text",
			armored:   "",
			wantError: true,
			errorMsg:  "armored text cannot be empty",
		},
		{
			name:      "invalid format - too short",
			armored:   "short\n",
			wantError: true,
			errorMsg:  "invalid clear-signed message format",
		},
		{
			name:      "missing BEGIN PGP SIGNED MESSAGE",
			armored:   "Line 1\nLine 2\nLine 3\n-----BEGIN PGP SIGNATURE-----\nsig",
			wantError: true,
			errorMsg:  "invalid clear-signed message structure",
		},
		{
			name:      "missing BEGIN PGP SIGNATURE",
			armored:   "-----BEGIN PGP SIGNED MESSAGE-----\nHash: SHA256\n\nMessage content\nMore content",
			wantError: true,
			errorMsg:  "invalid clear-signed message structure",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg, err := ParseClearTextMessage(tt.armored)

			if tt.wantError {
				if err == nil {
					t.Errorf("ParseClearTextMessage() error = nil, want error")
				} else if tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("ParseClearTextMessage() error = %v, want error containing %q", err, tt.errorMsg)
				}
			} else {
				if err != nil {
					t.Errorf("ParseClearTextMessage() error = %v, want nil", err)
				}
				if msg == nil {
					t.Fatal("ParseClearTextMessage() returned nil message")
				}
				if len(msg.Data) == 0 {
					t.Error("ParseClearTextMessage() message data is empty")
				}
				if len(msg.Signature) == 0 {
					t.Error("ParseClearTextMessage() signature is empty")
				}
			}
		})
	}
}

// TestLoadKeyRingFromStrings tests loading keys from string slices
func TestLoadKeyRingFromStrings(t *testing.T) {
	// Load a valid key for testing
	validKeyData, err := EmbeddedKeysFS.ReadFile("nodejs-keys/4ED778F539E3634C779C87C6D7062848A1AB005C.asc")
	if err != nil {
		t.Fatalf("Failed to read test key: %v", err)
	}

	tests := []struct {
		name      string
		keys      []string
		wantError bool
		errorMsg  string
	}{
		{
			name:      "single valid key",
			keys:      []string{string(validKeyData)},
			wantError: false,
		},
		{
			name:      "empty slice",
			keys:      []string{},
			wantError: true,
			errorMsg:  "no armored keys provided",
		},
		{
			name:      "nil slice",
			keys:      nil,
			wantError: true,
			errorMsg:  "no armored keys provided",
		},
		{
			name:      "invalid key in slice",
			keys:      []string{"not a valid key"},
			wantError: true,
			errorMsg:  "failed to parse armored key string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keyRing, err := LoadKeyRingFromStrings(tt.keys)

			if tt.wantError {
				if err == nil {
					t.Errorf("LoadKeyRingFromStrings() error = nil, want error")
				} else if tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("LoadKeyRingFromStrings() error = %v, want error containing %q", err, tt.errorMsg)
				}
			} else {
				if err != nil {
					t.Errorf("LoadKeyRingFromStrings() error = %v, want nil", err)
				}
				if keyRing == nil {
					t.Error("LoadKeyRingFromStrings() returned nil keyring")
				}
			}
		})
	}
}

// TestLoadKeyRingFromPath tests loading keys from filesystem
func TestLoadKeyRingFromPath(t *testing.T) {
	// Create temporary directory for test keys
	tmpDir := t.TempDir()

	// Create a valid key file
	validKeyData, err := EmbeddedKeysFS.ReadFile("nodejs-keys/4ED778F539E3634C779C87C6D7062848A1AB005C.asc")
	if err != nil {
		t.Fatalf("Failed to read test key: %v", err)
	}

	validKeyPath := filepath.Join(tmpDir, "valid-keys")
	if err := os.Mkdir(validKeyPath, 0755); err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}

	// Write key with correct permissions
	keyFile := filepath.Join(validKeyPath, "test-key.asc")
	if err := os.WriteFile(keyFile, validKeyData, 0644); err != nil {
		t.Fatalf("Failed to write test key: %v", err)
	}

	tests := []struct {
		name      string
		setupFunc func() string
		wantError bool
		errorMsg  string
	}{
		{
			name: "valid directory with keys",
			setupFunc: func() string {
				return validKeyPath
			},
			wantError: false,
		},
		{
			name: "nonexistent directory",
			setupFunc: func() string {
				return filepath.Join(tmpDir, "nonexistent")
			},
			wantError: true,
			errorMsg:  "failed to read keys directory",
		},
		{
			name: "empty directory",
			setupFunc: func() string {
				emptyDir := filepath.Join(tmpDir, "empty")
				_ = os.Mkdir(emptyDir, 0755)
				return emptyDir
			},
			wantError: true,
			errorMsg:  "no .asc keys found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := tt.setupFunc()
			keyRing, err := LoadKeyRingFromPath(path)

			if tt.wantError {
				if err == nil {
					t.Errorf("LoadKeyRingFromPath() error = nil, want error")
				} else if tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("LoadKeyRingFromPath() error = %v, want error containing %q", err, tt.errorMsg)
				}
			} else {
				if err != nil {
					t.Errorf("LoadKeyRingFromPath() error = %v, want nil", err)
				}
				if keyRing == nil {
					t.Error("LoadKeyRingFromPath() returned nil keyring")
				}
			}
		})
	}
}

// TestLoadSimpleKeyRingFromPath tests loading simple keyring from path
func TestLoadSimpleKeyRingFromPath(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a test key file
	keyDir := filepath.Join(tmpDir, "test-keys")
	if err := os.Mkdir(keyDir, 0755); err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}

	keyFile := filepath.Join(keyDir, "test.asc")
	if err := os.WriteFile(keyFile, []byte("test key data"), 0644); err != nil {
		t.Fatalf("Failed to write test key: %v", err)
	}

	t.Run("load simple keyring", func(t *testing.T) {
		keyRing, err := LoadSimpleKeyRingFromPath(keyDir)
		if err != nil {
			t.Errorf("LoadSimpleKeyRingFromPath() error = %v, want nil", err)
		}
		if keyRing == nil {
			t.Error("LoadSimpleKeyRingFromPath() returned nil keyring")
		}

		// Verify it's a SimpleKeyRing
		_, ok := keyRing.(*SimpleKeyRing)
		if !ok {
			t.Errorf("LoadSimpleKeyRingFromPath() returned wrong type, want *SimpleKeyRing")
		}
	})
}

// TestVerifyDetachedSignature tests detached signature verification
func TestVerifyDetachedSignature(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test files
	dataFile := filepath.Join(tmpDir, "data.txt")
	sigFile := filepath.Join(tmpDir, "data.txt.sig")

	if err := os.WriteFile(dataFile, []byte("test data"), 0644); err != nil {
		t.Fatalf("Failed to write data file: %v", err)
	}
	if err := os.WriteFile(sigFile, []byte("test signature"), 0644); err != nil {
		t.Fatalf("Failed to write signature file: %v", err)
	}

	tests := []struct {
		name      string
		keyRing   KeyRing
		dataPath  string
		sigPath   string
		wantError bool
		errorMsg  string
	}{
		{
			name:      "nil keyring",
			keyRing:   nil,
			dataPath:  dataFile,
			sigPath:   sigFile,
			wantError: true,
			errorMsg:  "keyring cannot be nil",
		},
		{
			name:      "nonexistent data file",
			keyRing:   NewSimpleKeyRing(),
			dataPath:  filepath.Join(tmpDir, "nonexistent.txt"),
			sigPath:   sigFile,
			wantError: true,
			errorMsg:  "failed to read data file",
		},
		{
			name:      "nonexistent signature file",
			keyRing:   NewSimpleKeyRing(),
			dataPath:  dataFile,
			sigPath:   filepath.Join(tmpDir, "nonexistent.sig"),
			wantError: true,
			errorMsg:  "failed to read signature file",
		},
		{
			name: "simple keyring verification",
			keyRing: func() KeyRing {
				kr := NewSimpleKeyRing()
				key, _ := NewSimpleKey("test key")
				_ = kr.AddKey(key)
				return kr
			}(),
			dataPath:  dataFile,
			sigPath:   sigFile,
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := VerifyDetachedSignature(tt.keyRing, tt.dataPath, tt.sigPath)

			if tt.wantError {
				if err == nil {
					t.Errorf("VerifyDetachedSignature() error = nil, want error")
				} else if tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("VerifyDetachedSignature() error = %v, want error containing %q", err, tt.errorMsg)
				}
			} else {
				if err != nil {
					t.Errorf("VerifyDetachedSignature() error = %v, want nil", err)
				}
			}
		})
	}
}

// TestVerifyClearSignedFile tests clear-signed file verification
func TestVerifyClearSignedFile(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a test clear-signed file
	ascFile := filepath.Join(tmpDir, "message.asc")
	clearSignedContent := `-----BEGIN PGP SIGNED MESSAGE-----
Hash: SHA256

This is the message
-----BEGIN PGP SIGNATURE-----

iQEzBAEBCAAdFiEE
-----END PGP SIGNATURE-----`

	if err := os.WriteFile(ascFile, []byte(clearSignedContent), 0644); err != nil {
		t.Fatalf("Failed to write asc file: %v", err)
	}

	tests := []struct {
		name      string
		keyRing   KeyRing
		ascPath   string
		wantError bool
		errorMsg  string
	}{
		{
			name:      "nil keyring",
			keyRing:   nil,
			ascPath:   ascFile,
			wantError: true,
			errorMsg:  "keyring cannot be nil",
		},
		{
			name:      "nonexistent file",
			keyRing:   NewSimpleKeyRing(),
			ascPath:   filepath.Join(tmpDir, "nonexistent.asc"),
			wantError: true,
			errorMsg:  "failed to read clear-signed file",
		},
		{
			name: "simple keyring verification",
			keyRing: func() KeyRing {
				kr := NewSimpleKeyRing()
				key, _ := NewSimpleKey("test key")
				_ = kr.AddKey(key)
				return kr
			}(),
			ascPath:   ascFile,
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plaintext, err := VerifyClearSignedFile(tt.keyRing, tt.ascPath)

			if tt.wantError {
				if err == nil {
					t.Errorf("VerifyClearSignedFile() error = nil, want error")
				} else if tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("VerifyClearSignedFile() error = %v, want error containing %q", err, tt.errorMsg)
				}
			} else {
				if err != nil {
					t.Errorf("VerifyClearSignedFile() error = %v, want nil", err)
				}
				if plaintext == "" {
					t.Error("VerifyClearSignedFile() returned empty plaintext")
				}
			}
		})
	}
}

// TestValidateKeyFile tests key file validation
func TestValidateKeyFile(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test files with different permissions
	validFile := filepath.Join(tmpDir, "valid-key.asc")
	if err := os.WriteFile(validFile, []byte("test key"), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	strictFile := filepath.Join(tmpDir, "strict-key.asc")
	if err := os.WriteFile(strictFile, []byte("test key"), 0600); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	tests := []struct {
		name      string
		filePath  string
		wantError bool
		errorMsg  string
	}{
		{
			name:      "valid file with 0644",
			filePath:  validFile,
			wantError: false,
		},
		{
			name:      "valid file with 0600",
			filePath:  strictFile,
			wantError: false,
		},
		{
			name:      "nonexistent file",
			filePath:  filepath.Join(tmpDir, "nonexistent.asc"),
			wantError: true,
			errorMsg:  "failed to access key file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateKeyFile(tt.filePath)

			if tt.wantError {
				if err == nil {
					t.Errorf("validateKeyFile() error = nil, want error")
				} else if tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("validateKeyFile() error = %v, want error containing %q", err, tt.errorMsg)
				}
			} else {
				if err != nil {
					t.Errorf("validateKeyFile() error = %v, want nil", err)
				}
			}
		})
	}
}
